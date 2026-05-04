package worker

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"time"

	"github.com/brianvoe/gofakeit/v6"
	"github.com/sahil-doshi-mongodb/mongo-ycsb/internal/config"
	"github.com/sahil-doshi-mongodb/mongo-ycsb/internal/datagen"
	"github.com/sahil-doshi-mongodb/mongo-ycsb/internal/metrics"
	"github.com/sahil-doshi-mongodb/mongo-ycsb/internal/workloads"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// Worker executes benchmark operations against MongoDB.
// Each worker owns its own RNG and Faker to eliminate shared-mutex contention.
type Worker struct {
	id          int
	coll        *mongo.Collection
	selector    *workloads.Selector
	gen         *datagen.Generator
	recorder    metrics.Recorder
	workloadCfg *config.WorkloadConfig
	rng         *rand.Rand
	faker       *gofakeit.Faker
}

// New creates a Worker with goroutine-local RNG and Faker.
func New(
	id int,
	coll *mongo.Collection,
	selector *workloads.Selector,
	gen *datagen.Generator,
	recorder metrics.Recorder,
	workloadCfg *config.WorkloadConfig,
) *Worker {
	return &Worker{
		id:          id,
		coll:        coll,
		selector:    selector,
		gen:         gen,
		recorder:    recorder,
		workloadCfg: workloadCfg,
		rng:         datagen.NewWorkerRNG(),
		faker:       datagen.NewWorkerFaker(),
	}
}

// Run executes operations until ctx is cancelled — time-bound mode.
func (w *Worker) Run(ctx context.Context, limiter *rateLimiter) {
	for {
		select {
		case <-ctx.Done():
			return
		default:
			if limiter != nil {
				if err := limiter.wait(ctx); err != nil {
					return
				}
			}
			op := w.selector.Next(w.rng)
			start := time.Now()
			err := w.execute(ctx, op)
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				return
			}
			w.recorder.Record(op, time.Since(start), err)
		}
	}
}

// RunN executes exactly n operations — ops-bound mode.
func (w *Worker) RunN(ctx context.Context, n int64, limiter *rateLimiter) {
	for i := int64(0); i < n; i++ {
		select {
		case <-ctx.Done():
			return
		default:
			if limiter != nil {
				if err := limiter.wait(ctx); err != nil {
					return
				}
			}
			op := w.selector.Next(w.rng)
			start := time.Now()
			err := w.execute(ctx, op)
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				return
			}
			w.recorder.Record(op, time.Since(start), err)
		}
	}
}

func (w *Worker) execute(ctx context.Context, op workloads.OpType) error {
	switch op {
	case workloads.OpRead:
		return w.doRead(ctx)
	case workloads.OpInsert:
		return w.doInsert(ctx)
	case workloads.OpUpdate:
		return w.doUpdate(ctx)
	case workloads.OpDelete:
		return w.doDelete(ctx)
	case workloads.OpScan:
		return w.doScan(ctx)
	case workloads.OpReadModifyWrite:
		return w.doReadModifyWrite(ctx)
	default:
		return fmt.Errorf("unknown operation: %s", op)
	}
}

func (w *Worker) doRead(ctx context.Context) error {
	key := w.gen.NextExistingKey(w.rng)
	if key == "" {
		return nil
	}
	var opts *options.FindOneOptions
	// readAllFields=false: project only one field (saves network bandwidth)
	if w.workloadCfg != nil && !w.workloadCfg.ReadAllFields {
		opts = options.FindOne().SetProjection(bson.M{"field0": 1})
	}
	result := w.coll.FindOne(ctx, bson.M{"_id": key}, opts)
	if errors.Is(result.Err(), mongo.ErrNoDocuments) {
		return nil // key space is probabilistic — not a real error
	}
	return result.Err()
}

func (w *Worker) doInsert(ctx context.Context) error {
	key := w.gen.ReserveInsertKey()
	doc := w.gen.BuildDocument(key, w.rng, w.faker)
	_, err := w.coll.InsertOne(ctx, doc)
	if err == nil {
		// Only acknowledge after confirmed insert — matches YCSB AcknowledgedCounterGenerator
		w.gen.AcknowledgeInsert()
	}
	return err
}

func (w *Worker) doUpdate(ctx context.Context) error {
	key := w.gen.NextExistingKey(w.rng)
	if key == "" {
		return nil
	}
	_, err := w.coll.UpdateOne(ctx, bson.M{"_id": key}, w.gen.BuildUpdateDoc(w.rng, w.faker))
	return err
}

func (w *Worker) doDelete(ctx context.Context) error {
	key := w.gen.NextExistingKey(w.rng)
	if key == "" {
		return nil
	}
	_, err := w.coll.DeleteOne(ctx, bson.M{"_id": key})
	return err
}

func (w *Worker) doScan(ctx context.Context) error {
	key := w.gen.NextExistingKey(w.rng)
	if key == "" {
		return nil
	}

	// Variable scan length — matches YCSB Workload E (1–100 docs)
	scanLen := int64(w.effectiveScanMin())
	maxLen := w.effectiveScanMax()
	if maxLen > int(scanLen) {
		scanLen += int64(w.rng.Intn(maxLen - int(scanLen) + 1))
	}

	filter := bson.M{"_id": bson.M{"$gte": key}}
	findOpts := options.Find().SetLimit(scanLen)

	start := time.Now()
	cursor, err := w.coll.Find(ctx, filter, findOpts)
	if err != nil {
		return err
	}
	defer cursor.Close(ctx)

	var recordCount int64
	for cursor.Next(ctx) {
		recordCount++
	}
	elapsed := time.Since(start)

	if err := cursor.Err(); err != nil {
		return err
	}

	// Record per-record scan latency (total ÷ records) — matches YCSB SCAN-LATENCY-PER-RECORD
	if recordCount > 0 {
		perRecord := time.Duration(int64(elapsed) / recordCount)
		w.recorder.Record(workloads.OpScanPerRecord, perRecord, nil)
	}

	return nil
}

func (w *Worker) doReadModifyWrite(ctx context.Context) error {
	key := w.gen.NextExistingKey(w.rng)
	if key == "" {
		return nil
	}
	var existing bson.M
	if err := w.coll.FindOne(ctx, bson.M{"_id": key}).Decode(&existing); err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return nil
		}
		return err
	}
	_, err := w.coll.UpdateOne(ctx, bson.M{"_id": key}, w.gen.BuildUpdateDoc(w.rng, w.faker))
	return err
}

// ── Scan length helpers ───────────────────────────────────────────────────────

func (w *Worker) effectiveScanMin() int {
	if w.workloadCfg == nil || w.workloadCfg.Scan.MinLength <= 0 {
		return 1 // YCSB default
	}
	return w.workloadCfg.Scan.MinLength
}

func (w *Worker) effectiveScanMax() int {
	if w.workloadCfg == nil || w.workloadCfg.Scan.MaxLength <= 0 {
		return 1000 // YCSB default; Workload E uses 100
	}
	return w.workloadCfg.Scan.MaxLength
}
