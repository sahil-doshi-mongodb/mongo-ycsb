package config

import (
	"fmt"
	"os"
)

// Validate checks all fields and returns a slice of errors (empty = valid).
func (c *Config) Validate() []error {
	var errs []error

	// ── Connection ──────────────────────────────────────────────────────────
	if c.Connection.URI == "" {
		errs = append(errs, fmt.Errorf("connection.uri is required"))
	}
	if c.Connection.Database == "" {
		errs = append(errs, fmt.Errorf("connection.database is required"))
	}
	if c.Connection.Collection == "" {
		errs = append(errs, fmt.Errorf("connection.collection is required"))
	}
	validReadPrefs := map[string]bool{
		"primary": true, "primaryPreferred": true,
		"secondary": true, "secondaryPreferred": true, "nearest": true,
	}
	if c.Connection.ReadPreference != "" && !validReadPrefs[c.Connection.ReadPreference] {
		errs = append(errs, fmt.Errorf(
			"connection.readPreference must be one of: primary, primaryPreferred, secondary, secondaryPreferred, nearest",
		))
	}

	// ── Workload ────────────────────────────────────────────────────────────
	validWorkloads := map[WorkloadType]bool{
		WorkloadA: true, WorkloadB: true, WorkloadC: true,
		WorkloadD: true, WorkloadE: true, WorkloadF: true,
		WorkloadCustom: true,
	}
	if !validWorkloads[c.Workload.Type] {
		errs = append(errs, fmt.Errorf("workload.type must be one of: A, B, C, D, E, F, custom"))
	}
	if c.Workload.Type == WorkloadCustom {
		w := c.Workload.Custom
		total := w.Read + w.Insert + w.Update + w.Delete + w.Scan + w.ReadModifyWrite
		if total != 100.0 {
			errs = append(errs, fmt.Errorf(
				"custom workload percentages must sum to 100 (got %.1f)", total,
			))
		}
	}

	// Scan config
	if c.Workload.Scan.MinLength < 0 {
		errs = append(errs, fmt.Errorf("workload.scan.minLength must be >= 0"))
	}
	if c.Workload.Scan.MaxLength > 0 && c.Workload.Scan.MaxLength < c.Workload.Scan.MinLength {
		errs = append(errs, fmt.Errorf("workload.scan.maxLength must be >= minLength"))
	}
	validScanDist := map[string]bool{"": true, "uniform": true, "zipfian": true}
	if !validScanDist[c.Workload.Scan.Distribution] {
		errs = append(errs, fmt.Errorf("workload.scan.distribution must be uniform or zipfian"))
	}

	// ── Execution ───────────────────────────────────────────────────────────
	validModes := map[ExecutionMode]bool{
		ModeTime: true, ModeOps: true, ModeRampup: true,
	}
	if !validModes[c.Execution.Mode] {
		errs = append(errs, fmt.Errorf("execution.mode must be one of: time, ops, rampup"))
	}
	if c.Execution.Mode == ModeTime {
		if _, err := c.Execution.ParseDuration(); err != nil {
			errs = append(errs, fmt.Errorf("execution.duration is invalid: %w", err))
		}
	}
	if c.Execution.Mode == ModeOps && c.Execution.OperationCount <= 0 {
		errs = append(errs, fmt.Errorf("execution.operationCount must be > 0 when mode is ops"))
	}
	if c.Execution.Threads <= 0 {
		errs = append(errs, fmt.Errorf("execution.threads must be > 0"))
	}
	if c.Execution.Mode == ModeRampup {
		r := c.Execution.Rampup
		if r.InitialThreads <= 0 {
			errs = append(errs, fmt.Errorf("execution.rampup.initialThreads must be > 0"))
		}
		if r.MaxThreads <= r.InitialThreads {
			errs = append(errs, fmt.Errorf("execution.rampup.maxThreads must be > initialThreads"))
		}
		if r.StepSize <= 0 {
			errs = append(errs, fmt.Errorf("execution.rampup.stepSize must be > 0"))
		}
		if _, err := r.ParseStepDuration(); err != nil {
			errs = append(errs, fmt.Errorf("execution.rampup.stepDuration is invalid: %w", err))
		}
	}

	// Key distribution
	validDist := map[string]bool{
		"": true, "uniform": true, "zipfian": true, "latest": true, "sequential": true,
	}
	if !validDist[c.Execution.KeyDistribution] {
		errs = append(errs, fmt.Errorf(
			"execution.keyDistribution must be one of: uniform, zipfian, latest, sequential",
		))
	}
	if c.Execution.ZipfianConstant != 0 &&
		(c.Execution.ZipfianConstant <= 0 || c.Execution.ZipfianConstant >= 1) {
		errs = append(errs, fmt.Errorf("execution.zipfianConstant must be in (0, 1)"))
	}
	validOrdering := map[string]bool{"": true, "ordered": true, "hashed": true}
	if !validOrdering[c.Execution.InsertOrdering] {
		errs = append(errs, fmt.Errorf("execution.insertOrdering must be ordered or hashed"))
	}

	// ── Phases ──────────────────────────────────────────────────────────────
	if c.Phases.Preload.Enabled && c.Phases.Preload.DocumentCount <= 0 {
		errs = append(errs, fmt.Errorf("phases.preload.documentCount must be > 0 when preload is enabled"))
	}
	if c.Phases.Preload.Enabled && c.Phases.Preload.Threads <= 0 {
		errs = append(errs, fmt.Errorf("phases.preload.threads must be > 0 when preload is enabled"))
	}

	// ── Indexes ─────────────────────────────────────────────────────────────
	validIndexTypes := map[string]bool{
		"asc": true, "desc": true, "text": true, "geo2dsphere": true,
	}
	for i, idx := range c.Indexes {
		if len(idx.Fields) > 0 {
			for j, f := range idx.Fields {
				if f.Field == "" {
					errs = append(errs, fmt.Errorf("indexes[%d].fields[%d].field is required", i, j))
				}
				if f.Type != "" && !validIndexTypes[f.Type] {
					errs = append(errs, fmt.Errorf(
						"indexes[%d].fields[%d].type must be one of: asc, desc, text, geo2dsphere", i, j,
					))
				}
			}
		} else {
			if idx.Field == "" {
				errs = append(errs, fmt.Errorf("indexes[%d].field is required", i))
			}
			if idx.Type != "" && !validIndexTypes[idx.Type] {
				errs = append(errs, fmt.Errorf(
					"indexes[%d].type must be one of: asc, desc, text, geo2dsphere", i,
				))
			}
		}
	}

	// ── Schedule ────────────────────────────────────────────────────────────
	if c.Schedule.Enabled {
		if c.Schedule.Cron == "" {
			errs = append(errs, fmt.Errorf("schedule.cron is required when schedule.enabled is true"))
		}
		if c.Schedule.StartAt != "" {
			if _, err := c.Schedule.ParseStartAt(); err != nil {
				errs = append(errs, fmt.Errorf("schedule.startAt must be RFC3339: %w", err))
			}
		}
		if c.Schedule.StopAt != "" {
			if _, err := c.Schedule.ParseStopAt(); err != nil {
				errs = append(errs, fmt.Errorf("schedule.stopAt must be RFC3339: %w", err))
			}
		}
		if c.Schedule.RunFor != "" {
			if _, err := c.Schedule.ParseRunFor(); err != nil {
				errs = append(errs, fmt.Errorf("schedule.runFor is invalid: %w", err))
			}
		}
	}

	// ── Results — MongoDB ────────────────────────────────────────────────────
	// If enabled, validate URI, database, collection AND verify connectivity
	// is possible by checking the URI is well-formed.
	if c.Results.MongoDB.Enabled {
		if c.Results.MongoDB.URI == "" {
			errs = append(errs, fmt.Errorf(
				"results.mongodb.uri is required when results.mongodb.enabled is true — "+
					"set the URI or disable results.mongodb.enabled",
			))
		} else if isPlaceholderURI(c.Results.MongoDB.URI) {
			errs = append(errs, fmt.Errorf(
				"results.mongodb.uri looks like a placeholder (%q) — "+
					"replace it with a real MongoDB connection string or disable results.mongodb.enabled",
				c.Results.MongoDB.URI,
			))
		}
		if c.Results.MongoDB.Database == "" {
			errs = append(errs, fmt.Errorf(
				"results.mongodb.database is required when results.mongodb.enabled is true",
			))
		}
		if c.Results.MongoDB.Collection == "" {
			errs = append(errs, fmt.Errorf(
				"results.mongodb.collection is required when results.mongodb.enabled is true",
			))
		}
	}

	// ── Results — Local JSON ─────────────────────────────────────────────────
	// If enabled, check the path exists and is writable. Create it if missing
	// so a first run doesn't fail silently after the benchmark completes.
	if c.Results.Local.Enabled {
		if c.Results.Local.Path == "" {
			errs = append(errs, fmt.Errorf(
				"results.local.path is required when results.local.enabled is true",
			))
		} else {
			if dirErr := ensureWritableDir(c.Results.Local.Path); dirErr != nil {
				errs = append(errs, fmt.Errorf(
					"results.local.path %q is not writable: %w — "+
						"fix the path/permissions or disable results.local.enabled",
					c.Results.Local.Path, dirErr,
				))
			}
		}
	}

	// ── Reporting — HTML ─────────────────────────────────────────────────────
	if c.Reporting.HTML.Enabled {
		if c.Reporting.HTML.OutputPath == "" {
			errs = append(errs, fmt.Errorf(
				"reporting.html.outputPath is required when reporting.html.enabled is true",
			))
		} else {
			if dirErr := ensureWritableDir(c.Reporting.HTML.OutputPath); dirErr != nil {
				errs = append(errs, fmt.Errorf(
					"reporting.html.outputPath %q is not writable: %w — "+
						"fix the path/permissions or disable reporting.html.enabled",
					c.Reporting.HTML.OutputPath, dirErr,
				))
			}
		}
	}

	// ── Reporting — CSV ──────────────────────────────────────────────────────
	if c.Reporting.CSV.Enabled {
		if c.Reporting.CSV.OutputPath == "" {
			errs = append(errs, fmt.Errorf(
				"reporting.csv.outputPath is required when reporting.csv.enabled is true",
			))
		} else {
			if dirErr := ensureWritableDir(c.Reporting.CSV.OutputPath); dirErr != nil {
				errs = append(errs, fmt.Errorf(
					"reporting.csv.outputPath %q is not writable: %w — "+
						"fix the path/permissions or disable reporting.csv.enabled",
					c.Reporting.CSV.OutputPath, dirErr,
				))
			}
		}
	}

	return errs
}

// ── Helpers ───────────────────────────────────────────────────────────────────

// ensureWritableDir creates the directory if it doesn't exist, then verifies
// the process can write to it by creating and removing a temp file.
// This catches permission issues before the benchmark runs rather than after.
func ensureWritableDir(path string) error {
	// Create directory (and all parents) if it doesn't exist
	if err := os.MkdirAll(path, 0755); err != nil {
		return fmt.Errorf("cannot create directory: %w", err)
	}

	// Verify write access by creating a temp probe file
	probe, err := os.CreateTemp(path, ".mongo-ycsb-probe-*")
	if err != nil {
		return fmt.Errorf("directory exists but is not writable: %w", err)
	}
	probe.Close()
	os.Remove(probe.Name())

	return nil
}

// isPlaceholderURI returns true if the URI still contains template placeholders
// that a user forgot to replace — catches the most common config mistake.
func isPlaceholderURI(uri string) bool {
	placeholders := []string{
		"<user>", "<password>", "<cluster>", "<host>",
		"<username>", "<your-cluster>", "example.com",
	}
	for _, p := range placeholders {
		for _, c := range uri {
			_ = c
			break
		}
		if len(uri) >= len(p) {
			for i := 0; i <= len(uri)-len(p); i++ {
				if uri[i:i+len(p)] == p {
					return true
				}
			}
		}
	}
	return false
}
