package config

import "fmt"

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
			// Compound index validation
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
			// Single field index validation
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
				errs = append(errs, fmt.Errorf("schedule.startAt must be RFC3339 format: %w", err))
			}
		}
		if c.Schedule.StopAt != "" {
			if _, err := c.Schedule.ParseStopAt(); err != nil {
				errs = append(errs, fmt.Errorf("schedule.stopAt must be RFC3339 format: %w", err))
			}
		}
		if c.Schedule.RunFor != "" {
			if _, err := c.Schedule.ParseRunFor(); err != nil {
				errs = append(errs, fmt.Errorf("schedule.runFor is invalid: %w", err))
			}
		}
	}

	// ── Results ─────────────────────────────────────────────────────────────
	if c.Results.MongoDB.Enabled {
		if c.Results.MongoDB.URI == "" {
			errs = append(errs, fmt.Errorf("results.mongodb.uri is required when enabled"))
		}
		if c.Results.MongoDB.Database == "" {
			errs = append(errs, fmt.Errorf("results.mongodb.database is required when enabled"))
		}
		if c.Results.MongoDB.Collection == "" {
			errs = append(errs, fmt.Errorf("results.mongodb.collection is required when enabled"))
		}
	}

	return errs
}
