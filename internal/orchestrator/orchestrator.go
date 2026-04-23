package orchestrator

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/yourusername/mongo-ycsb/internal/config"
	"github.com/yourusername/mongo-ycsb/internal/datagen"
	"github.com/yourusername/mongo-ycsb/internal/db"
	"github.com/yourusername/mongo-ycsb/internal/loader"
	"github.com/yourusername/mongo-ycsb/internal/metrics"
	"github.com/yourusername/mongo-ycsb/internal/models"
	"github.com/yourusername/mongo-ycsb/internal/worker"
	"github.com/yourusername/mongo-ycsb/internal/workloads"
	"go.uber.org/zap"
)

// Orchestrator runs all benchmark phases in sequence.
type Orchestrator struct {
	cfg         *config.Config
	log         *zap.Logger
	runID       string
	skipPreload bool
}

// New creates an Orchestrator with a fresh run ID.
func New(cfg *config.Config, log *zap.Logger, skipPreload bool) *Orchestrator {
	return &Orchestrator{
		cfg:         cfg,
		log:         log,
		runID:       uuid.New().String(),
		skipPreload: skipPreload,
	}
}

// Run executes: connect → preload → indexes → warmup → benchmark → summary.
func (o *Orchestrator) Run(ctx context.Context) (*models.RunResult, error) {
	fmt.Printf("\n🔑 Run ID : %s\n\n", o.runID)
	o.log.Info("benchmark starting", zap.String("run_id", o.runID))

	// ── 1. Preload phase — uses its own client with retries enabled ──────────
	// Kept separate from the benchmark client so transient connection hiccups
	// during long bulk inserts don't abort the entire preload, while the
	// benchmark client stays retry-free for accurate latency measurement.
	gen := datagen.New(&o.cfg.DocumentShape, 0)

	if o.skipPreload {
		fmt.Printf("⏭️  Skipping preload — using existing collection data\n")
		preloadClient, err := db.NewPreloadClient(ctx, &o.cfg.Connection)
		if err != nil {
			return nil, fmt.Errorf("preload connection failed: %w", err)
		}
		coll := preloadClient.
			Database(o.cfg.Connection.Database).
			Collection(o.cfg.Connection.Collection)
		count, err := coll.EstimatedDocumentCount(ctx)
		_ = preloadClient.Disconnect(ctx)
		if err != nil {
			return nil, fmt.Errorf("failed to count existing documents: %w", err)
		}
		gen = datagen.New(&o.cfg.DocumentShape, count)
		fmt.Printf("   ↳ Found %d existing documents\n\n", count)
	} else {
		preloadClient, err := db.NewPreloadClient(ctx, &o.cfg.Connection)
		if err != nil {
			return nil, fmt.Errorf("preload connection failed: %w", err)
		}
		preloadColl := preloadClient.
			Database(o.cfg.Connection.Database).
			Collection(o.cfg.Connection.Collection)

		ldr := loader.New(&o.cfg.Phases, preloadColl, gen, o.log)

		// Preload first — Drop() inside Preload() would wipe indexes
		// if we created them beforehand.
		if err := ldr.Preload(ctx); err != nil {
			_ = preloadClient.Disconnect(ctx)
			return nil, err
		}

		// Create indexes after preload so they survive the Drop().
		fmt.Printf("🔧 Creating indexes...\n")
		if err := ldr.CreateIndexes(ctx, o.cfg.Indexes); err != nil {
			_ = preloadClient.Disconnect(ctx)
			return nil, err
		}

		_ = preloadClient.Disconnect(ctx)
		gen = datagen.New(&o.cfg.DocumentShape, o.cfg.Phases.Preload.DocumentCount)
		fmt.Printf("✅ Indexes ready\n\n")
	}

	// ── 2. Benchmark client — retries disabled for accurate measurement ──────
	fmt.Printf("🔌 Pre-warming %d benchmark connections...\n", o.cfg.Execution.Threads)
	benchClient, err := db.NewBenchmarkClient(ctx, &o.cfg.Connection)
	if err != nil {
		return nil, fmt.Errorf("benchmark connection failed: %w", err)
	}
	defer benchClient.Disconnect(ctx)

	// Open connections in controlled batches before the clock starts.
	db.WarmUpPool(ctx, benchClient, o.cfg.Execution.Threads)
	fmt.Printf("✅ Connected to MongoDB\n\n")

	benchColl := benchClient.
		Database(o.cfg.Connection.Database).
		Collection(o.cfg.Connection.Collection)

	// ── 3. Shared benchmark components ──────────────────────────────────────
	selector, err := workloads.NewSelector(&o.cfg.Workload)
	if err != nil {
		return nil, err
	}
	recorder := metrics.NewSimpleRecorder()

	// ── 4. Warmup ───────────────────────────────────────────────────────────
	if o.cfg.Phases.Warmup.Enabled {
		warmupDur, err := time.ParseDuration(o.cfg.Phases.Warmup.Duration)
		if err != nil {
			return nil, fmt.Errorf("invalid warmup duration: %w", err)
		}

		fmt.Printf("🔥 Warming up for %s (metrics discarded)...\n\n", warmupDur)

		warmupExecCfg := o.cfg.Execution
		warmupExecCfg.Mode = config.ModeTime
		warmupExecCfg.Duration = o.cfg.Phases.Warmup.Duration

		warmupCtx, cancel := context.WithTimeout(ctx, warmupDur)
		warmupPool := worker.NewPool(
			&warmupExecCfg, benchColl, selector, gen,
			metrics.NewSimpleRecorder(), // discarded
			o.log,
		)
		_ = warmupPool.Run(warmupCtx)
		cancel()
	}

	// ── 5. Benchmark ────────────────────────────────────────────────────────
	fmt.Printf("🚀 Benchmark running — workload %s | %d threads | mode: %s\n",
		o.cfg.Workload.Type, o.cfg.Execution.Threads, o.cfg.Execution.Mode)

	start := time.Now()
	pool := worker.NewPool(&o.cfg.Execution, benchColl, selector, gen, recorder, o.log)
	if err := pool.Run(ctx); err != nil {
		return nil, fmt.Errorf("benchmark failed: %w", err)
	}
	elapsed := time.Since(start)

	// ── 6. Build and print result ───────────────────────────────────────────
	snap := recorder.Snapshot()
	result := &models.RunResult{
		RunID:     o.runID,
		Timestamp: time.Now().UTC(),
		Tags:      o.cfg.Results.Tags,
		Config:    models.FromConfig(o.cfg),
		Summary: models.RunSummary{
			DurationSeconds: elapsed.Seconds(),
			TotalOps:        snap.TotalOps,
			TotalErrors:     snap.TotalErrors,
			OpsPerSec:       float64(snap.TotalOps) / elapsed.Seconds(),
			ByOperation:     buildOpMetrics(snap),
		},
	}

	printSummary(result)
	return result, nil
}

func buildOpMetrics(snap metrics.Snapshot) map[string]models.OpMetric {
	out := make(map[string]models.OpMetric, len(snap.ByOperation))
	for k, v := range snap.ByOperation {
		mean := 0.0
		if v.Count > 0 {
			mean = float64(v.TotalLatMs) / float64(v.Count)
		}
		out[k] = models.OpMetric{
			Count:  v.Count,
			Errors: v.Errors,
			MeanMs: mean,
		}
	}
	return out
}

func printSummary(r *models.RunResult) {
	fmt.Printf("\n─────────────────────────────────────────────────\n")
	fmt.Printf("✅ Benchmark Complete\n")
	fmt.Printf("   Run ID      : %s\n", r.RunID)
	fmt.Printf("   Duration    : %.2fs\n", r.Summary.DurationSeconds)
	fmt.Printf("   Total Ops   : %d\n", r.Summary.TotalOps)
	fmt.Printf("   Errors      : %d\n", r.Summary.TotalErrors)
	fmt.Printf("   Throughput  : %.0f ops/sec\n\n", r.Summary.OpsPerSec)
	fmt.Printf("   %-20s %10s %10s %10s\n", "Operation", "Count", "Errors", "Mean (ms)")
	fmt.Printf("   %-20s %10s %10s %10s\n", "─────────", "─────", "──────", "─────────")
	for op, m := range r.Summary.ByOperation {
		fmt.Printf("   %-20s %10d %10d %10.2f\n", op, m.Count, m.Errors, m.MeanMs)
	}
	fmt.Printf("─────────────────────────────────────────────────\n")
}
