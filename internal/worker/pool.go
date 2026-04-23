package worker

import (
	"context"
	"sync"
	"time"

	"github.com/yourusername/mongo-ycsb/internal/config"
	"github.com/yourusername/mongo-ycsb/internal/datagen"
	"github.com/yourusername/mongo-ycsb/internal/metrics"
	"github.com/yourusername/mongo-ycsb/internal/workloads"
	"go.mongodb.org/mongo-driver/mongo"
	"go.uber.org/zap"
)

// Pool manages a group of Worker goroutines.
type Pool struct {
	cfg         *config.ExecutionConfig
	workloadCfg *config.WorkloadConfig
	coll        *mongo.Collection
	selector    *workloads.Selector
	gen         *datagen.Generator
	recorder    metrics.Recorder
	log         *zap.Logger
}

// NewPool creates a Pool.
func NewPool(
	cfg *config.ExecutionConfig,
	workloadCfg *config.WorkloadConfig,
	coll *mongo.Collection,
	selector *workloads.Selector,
	gen *datagen.Generator,
	recorder metrics.Recorder,
	log *zap.Logger,
) *Pool {
	return &Pool{
		cfg:         cfg,
		workloadCfg: workloadCfg,
		coll:        coll,
		selector:    selector,
		gen:         gen,
		recorder:    recorder,
		log:         log,
	}
}

// Run dispatches to the correct execution strategy.
func (p *Pool) Run(ctx context.Context) error {
	// Build rate limiter if targetOpsPerSec is configured
	var limiter *rateLimiter
	if p.cfg.TargetOpsPerSec > 0 {
		limiter = newRateLimiter(p.cfg.TargetOpsPerSec)
		defer limiter.stop()
	}

	switch p.cfg.Mode {
	case config.ModeOps:
		return p.runOpsBound(ctx, limiter)
	case config.ModeRampup:
		return p.runRampup(ctx)
	default: // ModeTime
		return p.runTimeBound(ctx, limiter)
	}
}

func (p *Pool) runTimeBound(ctx context.Context, limiter *rateLimiter) error {
	dur, err := p.cfg.ParseDuration()
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, dur)
	defer cancel()
	p.spawnAndWait(ctx, p.cfg.Threads, func(ctx context.Context, w *Worker) {
		w.Run(ctx, limiter)
	})
	return nil
}

func (p *Pool) runOpsBound(ctx context.Context, limiter *rateLimiter) error {
	n := p.cfg.Threads
	base := p.cfg.OperationCount / int64(n)
	remainder := p.cfg.OperationCount % int64(n)

	opCounts := make([]int64, n)
	for i := range opCounts {
		opCounts[i] = base
	}
	opCounts[n-1] += remainder

	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		w := p.newWorker(i)
		ops := opCounts[i]
		go func(w *Worker, ops int64) {
			defer wg.Done()
			w.RunN(ctx, ops, limiter)
		}(w, ops)
	}
	wg.Wait()
	return nil
}

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
			w.Run(ctx, nil)
		})
		cancel()
	}
	return nil
}

func (p *Pool) spawnAndWait(ctx context.Context, n int, fn func(context.Context, *Worker)) {
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		w := p.newWorker(i)
		go func(w *Worker) {
			defer wg.Done()
			fn(ctx, w)
		}(w)
	}
	wg.Wait()
}

func (p *Pool) newWorker(id int) *Worker {
	return New(id, p.coll, p.selector, p.gen, p.recorder, p.workloadCfg)
}

// ── Rate limiter ──────────────────────────────────────────────────────────────

// rateLimiter is a token-bucket that caps ops/sec across all workers.
type rateLimiter struct {
	tokens chan struct{}
	done   chan struct{}
}

func newRateLimiter(opsPerSec int) *rateLimiter {
	r := &rateLimiter{
		tokens: make(chan struct{}, opsPerSec),
		done:   make(chan struct{}),
	}
	go func() {
		// Refill 10× per second; each tick deposits opsPerSec/10 tokens.
		const ticks = 10
		tokensPerTick := opsPerSec / ticks
		if tokensPerTick < 1 {
			tokensPerTick = 1
		}
		ticker := time.NewTicker(time.Second / ticks)
		defer ticker.Stop()
		for {
			select {
			case <-r.done:
				return
			case <-ticker.C:
				for i := 0; i < tokensPerTick; i++ {
					select {
					case r.tokens <- struct{}{}:
					default: // bucket full — discard token
					}
				}
			}
		}
	}()
	return r
}

func (r *rateLimiter) wait(ctx context.Context) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-r.tokens:
		return nil
	}
}

func (r *rateLimiter) stop() {
	close(r.done)
}
