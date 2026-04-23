package loader

import (
	"context"
	"fmt"
	"math/rand"
	"sync"

	"github.com/brianvoe/gofakeit/v6"
	"github.com/yourusername/mongo-ycsb/internal/config"
	"github.com/yourusername/mongo-ycsb/internal/datagen"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
	"go.uber.org/zap"
)

const batchSize = 100

// Loader handles the setup and preload phases.
type Loader struct {
	cfg  *config.PhasesConfig
	coll *mongo.Collection
	gen  *datagen.Generator
	log  *zap.Logger
}

// New creates a Loader.
func New(cfg *config.PhasesConfig, coll *mongo.Collection, gen *datagen.Generator, log *zap.Logger) *Loader {
	return &Loader{cfg: cfg, coll: coll, gen: gen, log: log}
}

// Preload drops the collection and bulk-inserts N documents using multiple goroutines.
func (l *Loader) Preload(ctx context.Context) error {
	if !l.cfg.Preload.Enabled {
		return nil
	}

	// skipIfExists — count docs and skip if already populated
	if l.cfg.Preload.SkipIfExists {
		count, err := l.coll.EstimatedDocumentCount(ctx)
		if err != nil {
			return fmt.Errorf("failed to count documents: %w", err)
		}
		if count > 0 {
			l.log.Info("preload skipped — collection already has data",
				zap.Int64("existing_docs", count))
			fmt.Printf("⏭️  Preload skipped — collection already has %d documents\n\n", count)
			return nil
		}
	}

	total := l.cfg.Preload.DocumentCount
	threads := l.cfg.Preload.Threads

	l.log.Info("preload starting", zap.Int64("documents", total), zap.Int("threads", threads))
	fmt.Printf("📦 Preloading %d documents (%d threads, batch size %d)...\n", total, threads, batchSize)

	if err := l.coll.Drop(ctx); err != nil {
		return fmt.Errorf("drop before preload: %w", err)
	}

	docsPerThread := total / int64(threads)
	remainder := total % int64(threads)

	var (
		workerWg   sync.WaitGroup
		progressWg sync.WaitGroup
		errCh      = make(chan error, threads)
		done       = make(chan int64, threads)
	)

	progressWg.Add(1)
	go func() {
		defer progressWg.Done()
		var soFar int64
		for n := range done {
			soFar += n
			pct := float64(soFar) / float64(total) * 100
			fmt.Printf("   ↳ %.0f%% (%d / %d)\n", pct, soFar, total)
		}
	}()

	for t := 0; t < threads; t++ {
		count := docsPerThread
		if t == threads-1 {
			count += remainder
		}
		workerWg.Add(1)
		go func(n int64) {
			defer workerWg.Done()
			rng := datagen.NewWorkerRNG()
			faker := datagen.NewWorkerFaker()
			inserted, err := l.insertN(ctx, n, rng, faker)
			done <- inserted
			if err != nil {
				errCh <- err
			}
		}(count)
	}

	workerWg.Wait()
	close(done)
	close(errCh)
	progressWg.Wait()

	if err := <-errCh; err != nil {
		return fmt.Errorf("preload error: %w", err)
	}

	l.log.Info("preload complete", zap.Int64("documents", total))
	fmt.Printf("✅ Preload complete\n\n")
	return nil
}

// insertN inserts n documents in batches and returns the total inserted.
func (l *Loader) insertN(ctx context.Context, n int64, rng *rand.Rand, faker *gofakeit.Faker) (int64, error) {
	var inserted int64
	for inserted < n {
		batch := int64(batchSize)
		if inserted+batch > n {
			batch = n - inserted
		}
		docs := make([]interface{}, batch)
		for i := int64(0); i < batch; i++ {
			key := l.gen.NextInsertKey()
			docs[i] = l.gen.BuildDocument(key, rng, faker)
		}
		opts := options.InsertMany().SetOrdered(false)
		if _, err := l.coll.InsertMany(ctx, docs, opts); err != nil {
			return inserted, err
		}
		inserted += batch
	}
	return inserted, nil
}

// CreateIndexes supports both single-field and compound indexes.
func (l *Loader) CreateIndexes(ctx context.Context, indexes []config.IndexConfig) error {
	if len(indexes) == 0 {
		return nil
	}

	models := make([]mongo.IndexModel, 0, len(indexes))
	for _, idx := range indexes {
		var key bson.D

		if len(idx.Fields) > 0 {
			// Compound index — build multi-key bson.D
			for _, f := range idx.Fields {
				key = append(key, bson.E{Key: f.Field, Value: indexValue(f.Type)})
			}
		} else {
			// Single field index
			key = bson.D{{Key: idx.Field, Value: indexValue(idx.Type)}}
		}

		opts := options.Index().SetSparse(idx.Sparse).SetUnique(idx.Unique)
		models = append(models, mongo.IndexModel{Keys: key, Options: opts})
	}

	if _, err := l.coll.Indexes().CreateMany(ctx, models); err != nil {
		return fmt.Errorf("create indexes: %w", err)
	}

	l.log.Info("indexes created", zap.Int("count", len(models)))
	return nil
}

func indexValue(t string) interface{} {
	switch t {
	case "desc":
		return -1
	case "text":
		return "text"
	case "geo2dsphere":
		return "2dsphere"
	default:
		return 1
	}
}
