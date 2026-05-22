package orchestrator

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/sahil-doshi-mongodb/mongo-ycsb/internal/config"
	"github.com/sahil-doshi-mongodb/mongo-ycsb/internal/datagen"
	"github.com/sahil-doshi-mongodb/mongo-ycsb/internal/db"
	"github.com/sahil-doshi-mongodb/mongo-ycsb/internal/loader"
	"github.com/sahil-doshi-mongodb/mongo-ycsb/internal/metrics"
	"github.com/sahil-doshi-mongodb/mongo-ycsb/internal/models"
	"github.com/sahil-doshi-mongodb/mongo-ycsb/internal/reporter"
	"github.com/sahil-doshi-mongodb/mongo-ycsb/internal/worker"
	"github.com/sahil-doshi-mongodb/mongo-ycsb/internal/workloads"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
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

// Run executes: connect → preload → indexes → warmup → benchmark → report.
func (o *Orchestrator) Run(ctx context.Context) (*models.RunResult, error) {
	fmt.Printf("\n🔑 Run ID : %s\n\n", o.runID)
	o.log.Info("benchmark starting", zap.String("run_id", o.runID))

	// ── 1. Benchmark client ──────────────────────────────────────────────────
	fmt.Printf("🔌 Pre-warming %d benchmark connections...\n", o.cfg.Execution.Threads)
	benchClient, err := db.NewBenchmarkClient(ctx, &o.cfg.Connection)
	if err != nil {
		return nil, fmt.Errorf("benchmark connection failed: %w", err)
	}
	defer benchClient.Disconnect(ctx)
	db.WarmUpPool(ctx, benchClient, o.cfg.Execution.Threads)
	fmt.Printf("✅ Connected to MongoDB\n\n")

	// Capture cluster info — MongoDB version, host, storage engine
	fmt.Printf("🔍 Capturing cluster info...\n")
	clusterInfo, err := captureClusterInfo(ctx, benchClient)
	if err != nil {
		fmt.Printf("⚠️  Could not capture cluster info: %v\n", err)
	} else {
		fmt.Printf("   MongoDB version  : %s\n", clusterInfo.MongoVersion)
		fmt.Printf("   Host             : %s\n", clusterInfo.Host)
		fmt.Printf("   Storage engine   : %s\n\n", clusterInfo.StorageEngine)
	}

	benchColl := benchClient.
		Database(o.cfg.Connection.Database).
		Collection(o.cfg.Connection.Collection)

	// ── 2. Preload / skip-preload ────────────────────────────────────────────
	var startingCount int64

	if o.skipPreload {
		fmt.Printf("⏭️  Skipping preload — using existing collection data\n")
		startingCount, err = getHighestKeyNumber(ctx, benchColl, o.cfg.Execution.EffectiveKeyPrefix())
		if err != nil {
			return nil, fmt.Errorf("failed to determine highest existing key: %w", err)
		}
		fmt.Printf("   ↳ Highest existing key : %s%d\n", o.cfg.Execution.EffectiveKeyPrefix(), startingCount-1)
		fmt.Printf("   ↳ Next insert will use : %s%d\n\n", o.cfg.Execution.EffectiveKeyPrefix(), startingCount)
	} else {
		preloadClient, err := db.NewPreloadClient(ctx, &o.cfg.Connection)
		if err != nil {
			return nil, fmt.Errorf("preload connection failed: %w", err)
		}
		preloadColl := preloadClient.
			Database(o.cfg.Connection.Database).
			Collection(o.cfg.Connection.Collection)

		// Build a temporary generator for preload phase (no distribution needed)
		preloadGen := datagen.New(
			&o.cfg.DocumentShape,
			&o.cfg.Execution,
			&o.cfg.Workload,
			0,
		)
		ldr := loader.New(&o.cfg.Phases, preloadColl, preloadGen, o.log)

		if err := ldr.Preload(ctx); err != nil {
			_ = preloadClient.Disconnect(ctx)
			return nil, err
		}

		fmt.Printf("🔧 Creating indexes...\n")
		if err := ldr.CreateIndexes(ctx, o.cfg.Indexes); err != nil {
			_ = preloadClient.Disconnect(ctx)
			return nil, err
		}
		_ = preloadClient.Disconnect(ctx)

		startingCount = o.cfg.Phases.Preload.DocumentCount
		fmt.Printf("✅ Indexes ready\n\n")
	}

	// ── Workload D: auto-switch to Latest distribution ───────────────────────────
	// Original YCSB always uses Latest distribution for Workload D regardless of
	// the global keyDistribution setting. Replicate that behaviour here.
	if o.cfg.Workload.Type == config.WorkloadD &&
		o.cfg.Execution.KeyDistribution != "latest" {
		fmt.Printf("ℹ️  Workload D: auto-switching key distribution to 'latest'\n")
		fmt.Printf("   (original YCSB always uses Latest distribution for Workload D)\n\n")
		o.cfg.Execution.KeyDistribution = "latest"
	}

	// ── 3. Generator with distribution ──────────────────────────────────────
	// Created AFTER preload so recordCount is known and Zipfian can be initialised.
	gen := datagen.New(
		&o.cfg.DocumentShape,
		&o.cfg.Execution,
		&o.cfg.Workload,
		startingCount,
	)

	// ── 4. Workload selector ─────────────────────────────────────────────────
	selector, err := workloads.NewSelector(&o.cfg.Workload)
	if err != nil {
		return nil, err
	}

	// ── 5. Warmup ───────────────────────────────────────────────────────────
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
			&warmupExecCfg,
			&o.cfg.Workload,
			benchColl,
			selector,
			gen,
			metrics.NewHdrRecorder(),
			o.log,
		)
		_ = warmupPool.Run(warmupCtx)
		cancel()
	}

	// ── 6. Capture server opcounters before benchmark ────────────────────────
	statsBefore, err := captureOpcounters(ctx, benchClient)
	if err != nil {
		o.log.Warn("failed to capture pre-benchmark server stats", zap.Error(err))
	}

	// ── 7. Benchmark ─────────────────────────────────────────────────────────
	recorder := metrics.NewHdrRecorder()
	sampler := metrics.NewSystemSampler(o.cfg.Reporting.Console.RefreshIntervalMs)
	sampler.Start()
	ticker := metrics.NewTicker(
		recorder,
		o.cfg.Reporting.Console.RefreshIntervalMs,
		o.cfg.Reporting.Console.Enabled,
	)
	ticker.Start()

	dist := o.cfg.Execution.KeyDistribution
	if dist == "" {
		dist = "uniform"
	}
	fmt.Printf("🚀 Benchmark running — workload %s | %d threads | mode: %s | distribution: %s\n",
		o.cfg.Workload.Type, o.cfg.Execution.Threads, o.cfg.Execution.Mode, dist)
	if o.cfg.Execution.TargetOpsPerSec > 0 {
		fmt.Printf("   Target: %d ops/sec\n", o.cfg.Execution.TargetOpsPerSec)
	}

	start := time.Now()
	pool := worker.NewPool(
		&o.cfg.Execution,
		&o.cfg.Workload,
		benchColl,
		selector,
		gen,
		recorder,
		o.log,
	)
	if err := pool.Run(ctx); err != nil {
		ticker.Stop()
		sampler.Stop()
		return nil, fmt.Errorf("benchmark failed: %w", err)
	}
	elapsed := time.Since(start)

	ticker.Stop()
	sampler.Stop()

	// ── 8. Capture server opcounters after benchmark ─────────────────────────
	statsAfter, err := captureOpcounters(ctx, benchClient)
	if err != nil {
		o.log.Warn("failed to capture post-benchmark server stats", zap.Error(err))
	}

	// ── 9. Build result ──────────────────────────────────────────────────────
	snap := recorder.Snapshot()

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

	sysSamples := sampler.Samples()
	modelSysSamples := make([]models.SystemSample, len(sysSamples))
	for i, s := range sysSamples {
		modelSysSamples[i] = models.SystemSample{
			OffsetSeconds: s.OffsetSeconds,
			CPUPercent:    s.CPUPercent,
			MemoryMB:      s.MemoryMB,
		}
	}

	// Build server stats delta
	var serverStats *models.ServerStats
	if statsBefore != nil && statsAfter != nil {
		serverStats = &models.ServerStats{
			Before:     *statsBefore,
			After:      *statsAfter,
			CapturedAt: time.Now().UTC(),
			Delta: models.OpcounterSnapshot{
				Insert:  statsAfter.Insert - statsBefore.Insert,
				Query:   statsAfter.Query - statsBefore.Query,
				Update:  statsAfter.Update - statsBefore.Update,
				Delete:  statsAfter.Delete - statsBefore.Delete,
				GetMore: statsAfter.GetMore - statsBefore.GetMore,
				Command: statsAfter.Command - statsBefore.Command,
			},
		}
	}

	// Build error samples from recorder
	var errorSamples []models.ErrorSample
	for op, msgs := range recorder.ErrorMessages() {
		for _, msg := range msgs {
			errorSamples = append(errorSamples, models.ErrorSample{
				Operation: op,
				Message:   msg,
			})
		}
	}

	result := &models.RunResult{
		RunID:         o.runID,
		Timestamp:     time.Now().UTC(),
		Tags:          o.cfg.Results.Tags,
		Config:        models.FromConfig(o.cfg),
		ClusterInfo:   clusterInfo,
		Delta:         modelDeltas,
		SystemSamples: modelSysSamples,
		ServerStats:   serverStats,
		ErrorSamples:  errorSamples,
		Summary: models.RunSummary{
			DurationSeconds: elapsed.Seconds(),
			TotalOps:        snap.TotalOps,
			TotalErrors:     snap.TotalErrors,
			OpsPerSec:       float64(snap.TotalOps) / elapsed.Seconds(),
			ByOperation:     buildOpMetrics(snap),
		},
	}

	printSummary(result)

	// ── 10. Persist & report ─────────────────────────────────────────────────
	fmt.Printf("\n📤 Saving results...\n")

	mongoRep := reporter.NewMongoReporter(&o.cfg.Results.MongoDB)
	if err := mongoRep.Save(ctx, result); err != nil {
		o.log.Warn("MongoDB reporter failed", zap.Error(err))
		fmt.Printf("⚠️  MongoDB save failed: %v\n", err)
	}

	localRep := reporter.NewLocalReporter(&o.cfg.Results.Local, &o.cfg.Reporting.CSV)
	if err := localRep.Save(result); err != nil {
		o.log.Warn("local reporter failed", zap.Error(err))
		fmt.Printf("⚠️  Local save failed: %v\n", err)
	}

	htmlRep := reporter.NewHTMLReporter(&o.cfg.Reporting.HTML)
	if err := htmlRep.Save(result); err != nil {
		o.log.Warn("HTML reporter failed", zap.Error(err))
		fmt.Printf("⚠️  HTML report failed: %v\n", err)
	}

	return result, nil
}

// ── Cluster info capture ──────────────────────────────────────────────────────
// captureClusterInfo queries the MongoDB server for version and host details.
// Called once at benchmark start — adds no meaningful latency.
func captureClusterInfo(ctx context.Context, client *mongo.Client) (*models.ClusterInfo, error) {
	var buildInfo bson.M
	if err := client.Database("admin").
		RunCommand(ctx, bson.D{{Key: "buildInfo", Value: 1}}).
		Decode(&buildInfo); err != nil {
		return nil, fmt.Errorf("buildInfo command failed: %w", err)
	}
	var serverStatus bson.M
	if err := client.Database("admin").
		RunCommand(ctx, bson.D{{Key: "serverStatus", Value: 1}}).
		Decode(&serverStatus); err != nil {
		return nil, fmt.Errorf("serverStatus command failed: %w", err)
	}
	info := &models.ClusterInfo{}
	if v, ok := buildInfo["version"].(string); ok {
		info.MongoVersion = v
	}
	if v, ok := buildInfo["gitVersion"].(string); ok {
		info.GitVersion = v
	}
	if v, ok := serverStatus["host"].(string); ok {
		info.Host = v
	}
	if se, ok := serverStatus["storageEngine"].(bson.M); ok {
		if name, ok := se["name"].(string); ok {
			info.StorageEngine = name
		}
	}
	return info, nil
}

// ── Server opcounter capture ──────────────────────────────────────────────────

// captureOpcounters reads db.serverStatus().opcounters from the server.
// This is used to verify that both clusters received equal workload volumes —
// the root cause of Zepto's Round 1 failure where v8 received 4× more queries.
func captureOpcounters(ctx context.Context, client *mongo.Client) (*models.OpcounterSnapshot, error) {
	result := client.Database("admin").RunCommand(ctx, bson.D{{Key: "serverStatus", Value: 1}})
	if result.Err() != nil {
		return nil, result.Err()
	}

	var status struct {
		Opcounters struct {
			Insert  int64 `bson:"insert"`
			Query   int64 `bson:"query"`
			Update  int64 `bson:"update"`
			Delete  int64 `bson:"delete"`
			GetMore int64 `bson:"getmore"`
			Command int64 `bson:"command"`
		} `bson:"opcounters"`
	}

	if err := result.Decode(&status); err != nil {
		return nil, err
	}

	return &models.OpcounterSnapshot{
		Insert:  status.Opcounters.Insert,
		Query:   status.Opcounters.Query,
		Update:  status.Opcounters.Update,
		Delete:  status.Opcounters.Delete,
		GetMore: status.Opcounters.GetMore,
		Command: status.Opcounters.Command,
	}, nil
}

// ── Result building ───────────────────────────────────────────────────────────

func buildOpMetrics(snap metrics.Snapshot) map[string]models.OpMetric {
	out := make(map[string]models.OpMetric, len(snap.ByOperation))
	for k, v := range snap.ByOperation {
		out[k] = models.OpMetric{
			Count:    v.Count,
			Errors:   v.Errors,
			MeanMs:   v.MeanMs,
			P50Ms:    v.P50Ms,
			P95Ms:    v.P95Ms,
			P99Ms:    v.P99Ms,
			P999Ms:   v.P999Ms,
			P9999Ms:  v.P9999Ms,
			P99999Ms: v.P99999Ms,
		}
	}
	return out
}

func printSummary(r *models.RunResult) {
	fmt.Printf("\n──────────────────────────────────────────────────────────────────────────────────────\n")
	fmt.Printf("✅ Benchmark Complete\n")
	fmt.Printf("   Run ID           : %s\n", r.RunID)
	fmt.Printf("   Duration         : %.2fs\n", r.Summary.DurationSeconds)
	fmt.Printf("   Total Ops        : %d\n", r.Summary.TotalOps)
	fmt.Printf("   Errors           : %d\n", r.Summary.TotalErrors)
	fmt.Printf("   Throughput       : %.0f ops/sec\n", r.Summary.OpsPerSec)
	fmt.Printf("   Key Distribution : %s\n", r.Config.KeyDistribution)
	if r.ClusterInfo != nil {
		fmt.Printf("   MongoDB Version  : %s\n", r.ClusterInfo.MongoVersion)
		fmt.Printf("   Host             : %s\n", r.ClusterInfo.Host)
		fmt.Printf("   Storage Engine   : %s\n", r.ClusterInfo.StorageEngine)
	}
	fmt.Printf("\n")

	fmt.Printf("   %-18s %8s %8s %8s %8s %8s %8s %9s %10s\n",
		"Operation", "Count", "Errors", "Mean ms",
		"p50 ms", "p99 ms", "p999 ms", "p9999 ms", "p99999 ms")
	fmt.Printf("   %-18s %8s %8s %8s %8s %8s %8s %9s %10s\n",
		"─────────", "─────", "──────", "───────",
		"──────", "──────", "───────", "────────", "─────────")
	for op, m := range r.Summary.ByOperation {
		fmt.Printf("   %-18s %8d %8d %8.2f %8.2f %8.2f %8.2f %9.2f %10.2f\n",
			op, m.Count, m.Errors, m.MeanMs,
			m.P50Ms, m.P99Ms, m.P999Ms, m.P9999Ms, m.P99999Ms)
	}

	if len(r.SystemSamples) > 0 {
		totalCPU, peakMem := 0.0, 0.0
		for _, s := range r.SystemSamples {
			totalCPU += s.CPUPercent
			if s.MemoryMB > peakMem {
				peakMem = s.MemoryMB
			}
		}
		fmt.Printf("\n   Avg CPU          : %.1f%%\n", totalCPU/float64(len(r.SystemSamples)))
		fmt.Printf("   Peak Memory      : %.0f MB\n", peakMem)
	}

	if r.ServerStats != nil {
		d := r.ServerStats.Delta
		fmt.Printf("\n   Server Opcounters (delta during benchmark):\n")
		fmt.Printf("   %-10s  insert=%-10d  query=%-10d  update=%-10d  delete=%-10d\n",
			"", d.Insert, d.Query, d.Update, d.Delete)
	}

	if len(r.ErrorSamples) > 0 {
		fmt.Printf("\n   ⚠️  Error Samples:\n")
		for _, e := range r.ErrorSamples {
			fmt.Printf("      [%s] %s\n", e.Operation, e.Message)
		}
	}

	fmt.Printf("──────────────────────────────────────────────────────────────────────────────────────\n")
}

// getHighestKeyNumber finds the highest numeric suffix of existing _id values
// in the collection. This is used to correctly set the nextKey counter when
// --skip-preload is used, avoiding duplicate key errors caused by deletions
// lowering EstimatedDocumentCount below the actual highest inserted key.
func getHighestKeyNumber(ctx context.Context, coll *mongo.Collection, keyPrefix string) (int64, error) {
	// Fast path — if collection is empty, start from 0
	est, err := coll.EstimatedDocumentCount(ctx)
	if err != nil {
		return 0, err
	}
	if est == 0 {
		return 0, nil
	}

	// Aggregate to find the maximum numeric suffix across all documents.
	// $substrCP strips the key prefix, $toLong converts the suffix to integer.
	pipeline := bson.A{
		bson.D{{Key: "$match", Value: bson.D{
			{Key: "_id", Value: bson.D{{Key: "$regex", Value: "^" + keyPrefix}}},
		}}},
		bson.D{{Key: "$group", Value: bson.D{
			{Key: "_id", Value: nil},
			{Key: "maxKey", Value: bson.D{{Key: "$max", Value: bson.D{
				{Key: "$toLong", Value: bson.D{
					{Key: "$substrCP", Value: bson.A{"$_id", len(keyPrefix), 20}},
				}},
			}}}},
		}}},
	}

	cursor, err := coll.Aggregate(ctx, pipeline)
	if err != nil {
		// Fallback to estimated count if aggregation fails
		fmt.Printf("⚠️  Could not determine highest key via aggregation, falling back to EstimatedDocumentCount\n")
		return est, nil
	}
	defer cursor.Close(ctx)

	var result struct {
		MaxKey int64 `bson:"maxKey"`
	}
	if cursor.Next(ctx) {
		if err := cursor.Decode(&result); err != nil {
			return est, nil
		}
		// Return max + 1 so next insert uses the first truly free key
		return result.MaxKey + 1, nil
	}

	return est, nil
}
