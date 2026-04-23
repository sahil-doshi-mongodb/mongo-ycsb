package worker

import (
	"context"
	"fmt"
	"time"

	"github.com/yourusername/mongo-ycsb/internal/datagen"
	"github.com/yourusername/mongo-ycsb/internal/metrics"
	"github.com/yourusername/mongo-ycsb/internal/workloads"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

const defaultScanLimit = 20

// Worker executes benchmark operations against MongoDB.
type Worker struct {
	id       int
	coll     *mongo.Collection
	selector *workloads.Selector
	gen      *datagen.Generator
	recorder metrics.Recorder
}

// New creates a Worker.
func New(
	id int,
	coll *mongo.Collection,
	selector *workloads.Selector,
	gen *datagen.Generator,
	recorder metrics.Recorder,
) *Worker {
	return &Worker{id: id, coll: coll, selector: selector, gen: gen, recorder: recorder}
}

// Run executes operations until ctx is cancelled — used for time-bound mode.
func (w *Worker) Run(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		default:
			op := w.selector.Next()
			start := time.Now()
			err := w.execute(ctx, op)
			w.recorder.Record(op, time.Since(start), err)
		}
	}
}

// RunN executes exactly n operations — used for ops-bound mode.
func (w *Worker) RunN(ctx context.Context, n int64) {
	for i := int64(0); i < n; i++ {
		select {
		case <-ctx.Done():
			return
		default:
			op := w.selector.Next()
			start := time.Now()
			err := w.execute(ctx, op)
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
	key := w.gen.RandomExistingKey()
	if key == "" {
		return nil
	}
	return w.coll.FindOne(ctx, bson.M{"_id": key}).Err()
}

func (w *Worker) doInsert(ctx context.Context) error {
	key := w.gen.NextInsertKey()
	doc := w.gen.BuildDocument(key)
	_, err := w.coll.InsertOne(ctx, doc)
	return err
}

func (w *Worker) doUpdate(ctx context.Context) error {
	key := w.gen.RandomExistingKey()
	if key == "" {
		return nil
	}
	_, err := w.coll.UpdateOne(ctx, bson.M{"_id": key}, w.gen.BuildUpdateDoc())
	return err
}

func (w *Worker) doDelete(ctx context.Context) error {
	key := w.gen.RandomExistingKey()
	if key == "" {
		return nil
	}
	_, err := w.coll.DeleteOne(ctx, bson.M{"_id": key})
	return err
}

func (w *Worker) doScan(ctx context.Context) error {
	key := w.gen.RandomExistingKey()
	if key == "" {
		return nil
	}
	filter := bson.M{"_id": bson.M{"$gte": key}}
	opts := options.Find().SetLimit(int64(defaultScanLimit))

	cursor, err := w.coll.Find(ctx, filter, opts)
	if err != nil {
		return err
	}
	defer cursor.Close(ctx)
	// Drain cursor so the full scan round-trip is measured
	for cursor.Next(ctx) {
	}
	return cursor.Err()
}

func (w *Worker) doReadModifyWrite(ctx context.Context) error {
	key := w.gen.RandomExistingKey()
	if key == "" {
		return nil
	}
	// Read
	var existing bson.M
	if err := w.coll.FindOne(ctx, bson.M{"_id": key}).Decode(&existing); err != nil {
		return err
	}
	// Write back a modified field
	_, err := w.coll.UpdateOne(ctx, bson.M{"_id": key}, w.gen.BuildUpdateDoc())
	return err
}
