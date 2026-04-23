package models

import (
	"time"

	"github.com/yourusername/mongo-ycsb/internal/config"
)

// RunResult is the top-level document stored per benchmark run.
type RunResult struct {
	RunID     string       `bson:"run_id"    json:"run_id"`
	Timestamp time.Time    `bson:"timestamp" json:"timestamp"`
	Tags      []string     `bson:"tags"      json:"tags"`
	Config    RunConfig    `bson:"config"    json:"config"`
	Summary   RunSummary   `bson:"summary"   json:"summary"`
	Delta     []DeltaPoint `bson:"delta"    json:"delta"` // populated in Step 3
}

// RunConfig is a snapshot of the config used for this run.
type RunConfig struct {
	Workload   string `bson:"workload"             json:"workload"`
	Mode       string `bson:"mode"                 json:"mode"`
	Threads    int    `bson:"threads"              json:"threads"`
	Duration   string `bson:"duration,omitempty"   json:"duration,omitempty"`
	OpCount    int64  `bson:"op_count,omitempty"   json:"op_count,omitempty"`
	URI        string `bson:"uri"                  json:"uri"`
	Database   string `bson:"database"             json:"database"`
	Collection string `bson:"collection"           json:"collection"`
}

// RunSummary holds the final aggregated metrics for a run.
type RunSummary struct {
	DurationSeconds float64             `bson:"duration_seconds" json:"duration_seconds"`
	TotalOps        int64               `bson:"total_ops"        json:"total_ops"`
	TotalErrors     int64               `bson:"total_errors"     json:"total_errors"`
	OpsPerSec       float64             `bson:"ops_per_sec"      json:"ops_per_sec"`
	ByOperation     map[string]OpMetric `bson:"by_operation"     json:"by_operation"`
}

// OpMetric holds per-operation-type stats.
// P50/P95/P99/P999 are populated in Step 3 (HDR histograms).
type OpMetric struct {
	Count  int64   `bson:"count"   json:"count"`
	Errors int64   `bson:"errors"  json:"errors"`
	MeanMs float64 `bson:"mean_ms" json:"mean_ms"`
	P50Ms  float64 `bson:"p50_ms"  json:"p50_ms"`
	P95Ms  float64 `bson:"p95_ms"  json:"p95_ms"`
	P99Ms  float64 `bson:"p99_ms"  json:"p99_ms"`
	P999Ms float64 `bson:"p999_ms" json:"p999_ms"`
}

// DeltaPoint is a time-series sample captured during a run (Step 3).
type DeltaPoint struct {
	OffsetSeconds float64 `bson:"offset_seconds" json:"offset_seconds"`
	OpsPerSec     float64 `bson:"ops_per_sec"    json:"ops_per_sec"`
	ErrorRate     float64 `bson:"error_rate"     json:"error_rate"`
	P99Ms         float64 `bson:"p99_ms"         json:"p99_ms"`
}

// FromConfig builds a RunConfig snapshot from the full benchmark config.
func FromConfig(cfg *config.Config) RunConfig {
	return RunConfig{
		Workload:   string(cfg.Workload.Type),
		Mode:       string(cfg.Execution.Mode),
		Threads:    cfg.Execution.Threads,
		Duration:   cfg.Execution.Duration,
		OpCount:    cfg.Execution.OperationCount,
		URI:        cfg.Connection.URI,
		Database:   cfg.Connection.Database,
		Collection: cfg.Connection.Collection,
	}
}
