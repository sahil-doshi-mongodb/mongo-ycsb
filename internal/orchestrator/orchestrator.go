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

	// ── 1. Benchmark client — established first, reused throughout ───────────
	fmt.Printf("🔌 Pre-warming %d benchmark connections...\n", o.cfg.Execution.Threads)
	benchClient, err := db.NewBenchmarkClient(ctx, &o.cfg.Connection)
	if err != nil {
		return nil, fmt.Errorf("benchmark connection failed: %w", err)
	}
	defer benchClient.Disconnect(ctx)
	db.WarmUpPool(ctx, benchClient, o.cfg.Execution.Threads)
	fmt.Printf("✅ Connected to MongoDB\n\n")

	benchColl := benchClient.
		Database(o.cfg.Connection.Database).
		Collection(o.cfg.Connection.Collection)

	// ── 2. Generator seed ────────────────────────────────────────────────────
	gen := datagen.New(&o.cfg.DocumentShape, 0)

	if o.skipPreload {
		// Reuse the already-established bench client to count docs —
		// no extra connection needed.
		fmt.Printf("⏭️  Skipping preload — using existing collection data\n")
		count, err := benchColl.EstimatedDocumentCount(ctx)
		if err != nil {
			return nil, fmt.Errorf("failed to count existing documents: %w", err)
		}
		gen = datagen.New(&o.cfg.DocumentShape, count)
		fmt.Printf("   ↳ Found %d existing documents\n\n", count)
	} else {
		// Preload uses its own client with retries enabled so transient
		// connection hiccups during bulk inserts don't abort the preload.
		preloadClient, err := db.NewPreloadClient(ctx, &o.cfg.Connection)
		if err != nil {
			return nil, fmt.Errorf("preload connection failed: %w", err)
		}
		preloadColl := preloadClient.
			Database(o.cfg.Connection.Database).
			Collection(o.cfg.Connection.Collection)

		ldr := loader.New(&o.cfg.Phases, preloadColl, gen, o.log)

		// Preload BEFORE indexes so Drop() doesn't wipe them.
		if err := ldr.Preload(ctx); err != nil {
			_ = preloadClient.Disconnect(ctx)
			return nil, err
		}

		// Create indexes AFTER preload.
		fmt.Printf("🔧 Creating indexes...\n")
		if err := ldr.CreateIndexes(ctx, o.cfg.Indexes); err != nil {
			_ = preloadClient.Disconnect(ctx)
			return nil, err
		}
		_ = preloadClient.Disconnect(ctx)

		gen = datagen.New(&o.cfg.DocumentShape, o.cfg.Phases.Preload.DocumentCount)
		fmt.Printf("✅ Indexes ready\n\n")
	}

	// ── 3. Workload selector ─────────────────────────────────────────────────
	selector, err := workloads.NewSelector(&o.cfg.Workload)
	if err != nil {
		return nil, err
	}

	// ── 4. Warmup — metrics discarded, no ticker ─────────────────────────────
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
			metrics.NewHdrRecorder(), // discarded — not attached to ticker
			o.log,
		)
		_ = warmupPool.Run(warmupCtx)
		cancel()
	}

	// ── 5. Benchmark — HDR recorder + live ticker + system sampler ───────────
	recorder := metrics.NewHdrRecorder()

	// System sampler — always runs, samples CPU/memory every interval.
	sampler := metrics.NewSystemSampler(o.cfg.Reporting.Console.RefreshIntervalMs)
	sampler.Start()

	// Ticker — always runs for delta recording; only prints if console enabled.
	ticker := metrics.NewTicker(
		recorder,
		o.cfg.Reporting.Console.RefreshIntervalMs,
		o.cfg.Reporting.Console.Enabled,
	)
	ticker.Start()

	fmt.Printf("🚀 Benchmark running — workload %s | %d threads | mode: %s\n",
		o.cfg.Workload.Type, o.cfg.Execution.Threads, o.cfg.Execution.Mode)

	start := time.Now()
	pool := worker.NewPool(&o.cfg.Execution, benchColl, selector, gen, recorder, o.log)
	if err := pool.Run(ctx); err != nil {
		ticker.Stop()
		sampler.Stop()
		return nil, fmt.Errorf("benchmark failed: %w", err)
	}
	elapsed := time.Since(start)

	// Stop observability before reading final metrics so we get a clean snapshot.
	ticker.Stop()
	sampler.Stop()

	// ── 6. Build result ──────────────────────────────────────────────────────
	snap := recorder.Snapshot()

	// Convert metrics.DeltaPoint → models.DeltaPoint
	metricDeltas := recorder.Deltas()
	modelDeltas := make([]models.DeltaPoint, len(metricDeltas))
	for i, d := range metricDeltas {
		modelDeltas[i] = models.DeltaPoint{
			OffsetSeconds: d.OffsetSeconds,
			OpsPerSec:     d.OpsPerSec,
			ErrorRate:     d.ErrorRate,
			P99Ms:         d.P99Ms,
		}
	}

	// Convert metrics.SystemSample → models.SystemSample
	sysSamples := sampler.Samples()
	modelSysSamples := make([]models.SystemSample, len(sysSamples))
	for i, s := range sysSamples {
		modelSysSamples[i] = models.SystemSample{
			OffsetSeconds: s.OffsetSeconds,
			CPUPercent:    s.CPUPercent,
			MemoryMB:      s.MemoryMB,
		}
	}

	result := &models.RunResult{
		RunID:         o.runID,
		Timestamp:     time.Now().UTC(),
		Tags:          o.cfg.Results.Tags,
		Config:        models.FromConfig(o.cfg),
		Delta:         modelDeltas,
		SystemSamples: modelSysSamples,
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
		out[k] = models.OpMetric{
			Count:  v.Count,
			Errors: v.Errors,
			MeanMs: v.MeanMs,
			P50Ms:  v.P50Ms,
			P95Ms:  v.P95Ms,
			P99Ms:  v.P99Ms,
			P999Ms: v.P999Ms,
		}
	}
	return out
}

func printSummary(r *models.RunResult) {
	fmt.Printf("\n──────────────────────────────────────────────────────────────────────────\n")
	fmt.Printf("✅ Benchmark Complete\n")
	fmt.Printf("   Run ID      : %s\n", r.RunID)
	fmt.Printf("   Duration    : %.2fs\n", r.Summary.DurationSeconds)
	fmt.Printf("   Total Ops   : %d\n", r.Summary.TotalOps)
	fmt.Printf("   Errors      : %d\n", r.Summary.TotalErrors)
	fmt.Printf("   Throughput  : %.0f ops/sec\n\n", r.Summary.OpsPerSec)

	fmt.Printf("   %-18s %8s %8s %8s %8s %8s %8s %8s\n",
		"Operation", "Count", "Errors", "Mean ms", "p50 ms", "p95 ms", "p99 ms", "p999 ms")
	fmt.Printf("   %-18s %8s %8s %8s %8s %8s %8s %8s\n",
		"─────────", "─────", "──────", "───────", "──────", "──────", "──────", "───────")
	for op, m := range r.Summary.ByOperation {
		fmt.Printf("   %-18s %8d %8d %8.2f %8.2f %8.2f %8.2f %8.2f\n",
			op, m.Count, m.Errors, m.MeanMs, m.P50Ms, m.P95Ms, m.P99Ms, m.P999Ms)
	}

	fmt.Printf("\n   Delta snapshots   : %d\n", len(r.Delta))
	fmt.Printf("   System samples    : %d\n", len(r.SystemSamples))

	if len(r.SystemSamples) > 0 {
		totalCPU, peakMem := 0.0, 0.0
		for _, s := range r.SystemSamples {
			totalCPU += s.CPUPercent
			if s.MemoryMB > peakMem {
				peakMem = s.MemoryMB
			}
		}
		fmt.Printf("   Avg CPU           : %.1f%%\n", totalCPU/float64(len(r.SystemSamples)))
		fmt.Printf("   Peak Memory       : %.0f MB\n", peakMem)
	}
	fmt.Printf("──────────────────────────────────────────────────────────────────────────\n")
}
