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

	// ── Schedule ────────────────────────────────────────────────────────────
	if c.Schedule.Enabled && c.Schedule.Cron == "" {
		errs = append(errs, fmt.Errorf("schedule.cron is required when schedule.enabled is true"))
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
