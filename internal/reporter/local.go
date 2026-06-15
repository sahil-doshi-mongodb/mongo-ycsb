package reporter

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/sahil-doshi-mongodb/mongo-ycsb/internal/config"
	"github.com/sahil-doshi-mongodb/mongo-ycsb/internal/models"
)

// LocalReporter writes results to local JSON and CSV files.
type LocalReporter struct {
	jsonCfg *config.LocalResultsConfig
	csvCfg  *config.CSVConfig
}

// NewLocalReporter creates a LocalReporter.
func NewLocalReporter(jsonCfg *config.LocalResultsConfig, csvCfg *config.CSVConfig) *LocalReporter {
	return &LocalReporter{jsonCfg: jsonCfg, csvCfg: csvCfg}
}

// Save writes JSON and CSV files if enabled.
func (r *LocalReporter) Save(result *models.RunResult) error {
	if r.jsonCfg.Enabled {
		if err := r.saveJSON(result); err != nil {
			return err
		}
	}
	if r.csvCfg.Enabled {
		if err := r.saveCSV(result); err != nil {
			return err
		}
	}
	return nil
}

// saveJSON writes the full RunResult as a pretty-printed JSON file.
func (r *LocalReporter) saveJSON(result *models.RunResult) error {
	if err := os.MkdirAll(r.jsonCfg.Path, 0755); err != nil {
		return fmt.Errorf("create results dir: %w", err)
	}

	path := filepath.Join(r.jsonCfg.Path, result.RunID+".json")
	f, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("create JSON file: %w", err)
	}
	defer f.Close()

	enc := json.NewEncoder(f)
	enc.SetIndent("", "  ")
	if err := enc.Encode(result); err != nil {
		return fmt.Errorf("encode JSON: %w", err)
	}

	fmt.Printf("💾 JSON saved → %s\n", path)
	return nil
}

// saveCSV writes a flat per-operation metrics CSV.
func (r *LocalReporter) saveCSV(result *models.RunResult) error {
	if err := os.MkdirAll(r.csvCfg.OutputPath, 0755); err != nil {
		return fmt.Errorf("create CSV dir: %w", err)
	}

	path := filepath.Join(r.csvCfg.OutputPath, result.RunID+".csv")
	f, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("create CSV file: %w", err)
	}
	defer f.Close()

	w := csv.NewWriter(f)
	defer w.Flush()

	// Header
	_ = w.Write([]string{
		"run_id", "timestamp",
		"run_start_time", "run_end_time",
		"benchmark_start_time", "benchmark_end_time",
		"workload", "mode", "threads",
		"duration_seconds", "total_ops", "total_errors", "ops_per_sec",
		"operation", "count", "errors", "mean_ms",
		"p50_ms", "p95_ms", "p99_ms", "p999_ms",
	})

	// One row per operation type
	for op, m := range result.Summary.ByOperation {
		_ = w.Write([]string{
			result.RunID,
			result.Timestamp.Format("2006-01-02T15:04:05Z"),
			result.RunStartTime.UTC().Format(time.RFC3339),
			result.RunEndTime.UTC().Format(time.RFC3339),
			result.BenchmarkStartTime.UTC().Format(time.RFC3339),
			result.BenchmarkEndTime.UTC().Format(time.RFC3339),
			result.Config.Workload,
			result.Config.Mode,
			strconv.Itoa(result.Config.Threads),
			strconv.FormatFloat(result.Summary.DurationSeconds, 'f', 2, 64),
			strconv.FormatInt(result.Summary.TotalOps, 10),
			strconv.FormatInt(result.Summary.TotalErrors, 10),
			strconv.FormatFloat(result.Summary.OpsPerSec, 'f', 2, 64),
			op,
			strconv.FormatInt(m.Count, 10),
			strconv.FormatInt(m.Errors, 10),
			strconv.FormatFloat(m.MeanMs, 'f', 3, 64),
			strconv.FormatFloat(m.P50Ms, 'f', 3, 64),
			strconv.FormatFloat(m.P95Ms, 'f', 3, 64),
			strconv.FormatFloat(m.P99Ms, 'f', 3, 64),
			strconv.FormatFloat(m.P999Ms, 'f', 3, 64),
		})
	}

	fmt.Printf("💾 CSV  saved → %s\n", path)
	return nil
}
