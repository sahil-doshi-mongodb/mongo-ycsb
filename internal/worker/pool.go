package worker

import (
	"context"
	"sync"

	"github.com/yourusername/mongo-ycsb/internal/config"
	"github.com/yourusername/mongo-ycsb/internal/datagen"
	"github.com/yourusername/mongo-ycsb/internal/metrics"
	"github.com/yourusername/mongo-ycsb/internal/workloads"
	"go.mongodb.org/mongo-driver/mongo"
	"go.uber.org/zap"
)

// Pool manages a group of Worker goroutines.
type Pool struct {
	cfg      *config.ExecutionConfig
	coll     *mongo.Collection
	selector *workloads.Selector
	gen      *datagen.Generator
	recorder metrics.Recorder
	log      *zap.Logger
}

// NewPool creates a Pool.
func NewPool(
	cfg *config.ExecutionConfig,
	coll *mongo.Collection,
	selector *workloads.Selector,
	gen *datagen.Generator,
	recorder metrics.Recorder,
	log *zap.Logger,
) *Pool {
	return &Pool{cfg: cfg, coll: coll, selector: selector, gen: gen, recorder: recorder, log: log}
}

// Run dispatches to the correct execution strategy based on config mode.
func (p *Pool) Run(ctx context.Context) error {
	switch p.cfg.Mode {
	case config.ModeOps:
		return p.runOpsBound(ctx)
	case config.ModeRampup:
		return p.runRampup(ctx)
	default: // ModeTime
		return p.runTimeBound(ctx)
	}
}

// runTimeBound runs all workers for a fixed duration.
func (p *Pool) runTimeBound(ctx context.Context) error {
	dur, err := p.cfg.ParseDuration()
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, dur)
	defer cancel()
	p.spawnAndWait(ctx, p.cfg.Threads, func(ctx context.Context, w *Worker) {
		w.Run(ctx)
	})
	return nil
}

// runOpsBound distributes a fixed operation count across workers.
func (p *Pool) runOpsBound(ctx context.Context) error {
	n := p.cfg.Threads
	base := p.cfg.OperationCount / int64(n)
	remainder := p.cfg.OperationCount % int64(n)

	// Pre-compute per-worker op counts
	opCounts := make([]int64, n)
	for i := range opCounts {
		opCounts[i] = base
	}
	opCounts[n-1] += remainder // last worker gets the leftover

	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		w := New(i, p.coll, p.selector, p.gen, p.recorder)
		ops := opCounts[i]
		go func(w *Worker, ops int64) {
			defer wg.Done()
			w.RunN(ctx, ops)
		}(w, ops)
	}
	wg.Wait()
	return nil
}

// runRampup gradually increases concurrency to find the saturation point.
// Full metrics reset per step is implemented in Step 5.
func (p *Pool) runRampup(ctx context.Context) error {
	r := &p.cfg.Rampup
	stepDur, err := r.ParseStepDuration()
	if err != nil {
		return err
	}

	for threads := r.InitialThreads; threads <= r.MaxThreads; threads += r.StepSize {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		p.log.Info("ramp-up step",
			zap.Int("threads", threads),
			zap.String("step_duration", r.StepDuration),
		)

		stepCtx, cancel := context.WithTimeout(ctx, stepDur)
		p.spawnAndWait(stepCtx, threads, func(ctx context.Context, w *Worker) {
			w.Run(ctx)
		})
		cancel()
	}
	return nil
}

// spawnAndWait launches n workers, runs fn on each, and blocks until all finish.
func (p *Pool) spawnAndWait(ctx context.Context, n int, fn func(context.Context, *Worker)) {
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		w := New(i, p.coll, p.selector, p.gen, p.recorder)
		go func(w *Worker) {
			defer wg.Done()
			fn(ctx, w)
		}(w)
	}
	wg.Wait()
}
