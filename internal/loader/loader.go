package loader

import (
	"context"
	"fmt"
	"sync"

	"github.com/yourusername/mongo-ycsb/internal/config"
	"github.com/yourusername/mongo-ycsb/internal/datagen"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
	"go.uber.org/zap"
)

const batchSize = 500

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

	total := l.cfg.Preload.DocumentCount
	threads := l.cfg.Preload.Threads

	l.log.Info("preload starting", zap.Int64("documents", total), zap.Int("threads", threads))
	fmt.Printf("📦 Preloading %d documents (%d threads)...\n", total, threads)

	// Start clean
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

	// Progress reporter runs independently — waits for done to be closed
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
			inserted, err := l.insertN(ctx, n)
			done <- inserted
			if err != nil {
				errCh <- err
			}
		}(count)
	}

	// Wait for all inserts to finish, then signal the reporter to exit
	workerWg.Wait()
	close(done) // unblocks the range loop in the reporter goroutine
	close(errCh)

	// Wait for the reporter to print the final 100% line before continuing
	progressWg.Wait()

	if err := <-errCh; err != nil {
		return fmt.Errorf("preload error: %w", err)
	}

	l.log.Info("preload complete", zap.Int64("documents", total))
	fmt.Printf("✅ Preload complete\n\n")
	return nil
}

// insertN inserts n documents in batches and returns the total inserted.
func (l *Loader) insertN(ctx context.Context, n int64) (int64, error) {
	var inserted int64

	for inserted < n {
		batch := int64(batchSize)
		if inserted+batch > n {
			batch = n - inserted
		}

		docs := make([]interface{}, batch)
		for i := int64(0); i < batch; i++ {
			key := l.gen.NextInsertKey()
			docs[i] = l.gen.BuildDocument(key)
		}

		opts := options.InsertMany().SetOrdered(false)
		if _, err := l.coll.InsertMany(ctx, docs, opts); err != nil {
			return inserted, err
		}
		inserted += batch
	}
	return inserted, nil
}

// CreateIndexes creates indexes defined in config as part of the setup phase.
func (l *Loader) CreateIndexes(ctx context.Context, indexes []config.IndexConfig) error {
	if len(indexes) == 0 {
		return nil
	}

	models := make([]mongo.IndexModel, 0, len(indexes))
	for _, idx := range indexes {
		key := bson.D{{Key: idx.Field, Value: indexValue(idx.Type)}}
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
	default: // "asc"
		return 1
	}
}
