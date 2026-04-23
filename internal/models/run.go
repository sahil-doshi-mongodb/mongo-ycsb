package models

import (
	"time"

	"github.com/yourusername/mongo-ycsb/internal/config"
)

// RunResult is the top-level document stored per benchmark run.
type RunResult struct {
	RunID         string         `bson:"run_id"         json:"run_id"`
	Timestamp     time.Time      `bson:"timestamp"      json:"timestamp"`
	Tags          []string       `bson:"tags"           json:"tags"`
	Config        RunConfig      `bson:"config"         json:"config"`
	Summary       RunSummary     `bson:"summary"        json:"summary"`
	Delta         []DeltaPoint   `bson:"delta"          json:"delta"`
	SystemSamples []SystemSample `bson:"system_samples" json:"system_samples"`
	ServerStats   *ServerStats   `bson:"server_stats,omitempty" json:"server_stats,omitempty"`
	ErrorSamples  []ErrorSample  `bson:"error_samples,omitempty" json:"error_samples,omitempty"`
}

// RunConfig is a snapshot of the config used for this run.
type RunConfig struct {
	Workload        string `bson:"workload"                   json:"workload"`
	Mode            string `bson:"mode"                       json:"mode"`
	Threads         int    `bson:"threads"                    json:"threads"`
	Duration        string `bson:"duration,omitempty"         json:"duration,omitempty"`
	OpCount         int64  `bson:"op_count,omitempty"         json:"op_count,omitempty"`
	URI             string `bson:"uri"                        json:"uri"`
	Database        string `bson:"database"                   json:"database"`
	Collection      string `bson:"collection"                 json:"collection"`
	KeyDistribution string `bson:"key_distribution"           json:"key_distribution"`
	RecordCount     int64  `bson:"record_count,omitempty"     json:"record_count,omitempty"`
	WriteAllFields  bool   `bson:"write_all_fields"           json:"write_all_fields"`
	TargetOpsPerSec int    `bson:"target_ops_per_sec,omitempty" json:"target_ops_per_sec,omitempty"`
}

// RunSummary holds the final aggregated metrics for a run.
type RunSummary struct {
	DurationSeconds float64             `bson:"duration_seconds" json:"duration_seconds"`
	TotalOps        int64               `bson:"total_ops"        json:"total_ops"`
	TotalErrors     int64               `bson:"total_errors"     json:"total_errors"`
	OpsPerSec       float64             `bson:"ops_per_sec"      json:"ops_per_sec"`
	ByOperation     map[string]OpMetric `bson:"by_operation"     json:"by_operation"`
}

// OpMetric holds per-operation-type stats with full HDR percentiles.
// Matches original YCSB output columns.
type OpMetric struct {
	Count    int64   `bson:"count"      json:"count"`
	Errors   int64   `bson:"errors"     json:"errors"`
	MeanMs   float64 `bson:"mean_ms"    json:"mean_ms"`
	P50Ms    float64 `bson:"p50_ms"     json:"p50_ms"`
	P95Ms    float64 `bson:"p95_ms"     json:"p95_ms"`
	P99Ms    float64 `bson:"p99_ms"     json:"p99_ms"`
	P999Ms   float64 `bson:"p999_ms"    json:"p999_ms"`
	P9999Ms  float64 `bson:"p9999_ms"   json:"p9999_ms"`
	P99999Ms float64 `bson:"p99999_ms"  json:"p99999_ms"`
}

// DeltaPoint is a per-second time-series sample.
type DeltaPoint struct {
	OffsetSeconds float64 `bson:"offset_seconds" json:"offset_seconds"`
	OpsPerSec     float64 `bson:"ops_per_sec"    json:"ops_per_sec"`
	ErrorRate     float64 `bson:"error_rate"     json:"error_rate"`
	P99Ms         float64 `bson:"p99_ms"         json:"p99_ms"`
}

// SystemSample is a point-in-time CPU and memory reading.
type SystemSample struct {
	OffsetSeconds float64 `bson:"offset_seconds" json:"offset_seconds"`
	CPUPercent    float64 `bson:"cpu_percent"    json:"cpu_percent"`
	MemoryMB      float64 `bson:"memory_mb"      json:"memory_mb"`
}

// ServerStats holds server-side opcounters captured before and after the run.
// The Delta is what the server actually processed — used to verify that both
// clusters under comparison received equal workload volumes (Zepto Round 1 failure).
type ServerStats struct {
	Before     OpcounterSnapshot `bson:"before"  json:"before"`
	After      OpcounterSnapshot `bson:"after"   json:"after"`
	Delta      OpcounterSnapshot `bson:"delta"   json:"delta"`
	CapturedAt time.Time         `bson:"captured_at" json:"captured_at"`
}

// OpcounterSnapshot mirrors MongoDB's db.serverStatus().opcounters.
type OpcounterSnapshot struct {
	Insert  int64 `bson:"insert"  json:"insert"`
	Query   int64 `bson:"query"   json:"query"`
	Update  int64 `bson:"update"  json:"update"`
	Delete  int64 `bson:"delete"  json:"delete"`
	GetMore int64 `bson:"getmore" json:"getmore"`
	Command int64 `bson:"command" json:"command"`
}

// ErrorSample holds a sample error message captured during the run.
type ErrorSample struct {
	Operation string `bson:"operation" json:"operation"`
	Message   string `bson:"message"   json:"message"`
}

// FromConfig builds a RunConfig snapshot from the full benchmark config.
func FromConfig(cfg *config.Config) RunConfig {
	dist := cfg.Execution.KeyDistribution
	if dist == "" {
		dist = "uniform"
	}
	return RunConfig{
		Workload:        string(cfg.Workload.Type),
		Mode:            string(cfg.Execution.Mode),
		Threads:         cfg.Execution.Threads,
		Duration:        cfg.Execution.Duration,
		OpCount:         cfg.Execution.OperationCount,
		URI:             cfg.Connection.URI,
		Database:        cfg.Connection.Database,
		Collection:      cfg.Connection.Collection,
		KeyDistribution: dist,
		RecordCount:     cfg.Execution.RecordCount,
		WriteAllFields:  cfg.Workload.WriteAllFields,
		TargetOpsPerSec: cfg.Execution.TargetOpsPerSec,
	}
}
