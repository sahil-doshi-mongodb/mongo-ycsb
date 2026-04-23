package cmd

import (
	"fmt"
	"os"
	"time"

	"github.com/robfig/cron/v3"
	"github.com/spf13/cobra"
	"github.com/yourusername/mongo-ycsb/internal/config"
)

// skipPreloadFlag is set by runCmd — used by printConfigSummary to avoid
// a circular reference back to runCmd.
var skipPreloadFlag bool

var dryRunCmd = &cobra.Command{
	Use:   "dry-run",
	Short: "Validate config and show benchmark plan without running",
	Long:  `Parse and validate the configuration, then print what would be executed — without touching MongoDB.`,
	RunE:  dryRun,
}

func dryRun(cmd *cobra.Command, args []string) error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	errs := cfg.Validate()
	if len(errs) > 0 {
		fmt.Fprintln(os.Stderr, "❌ Configuration errors found:")
		for _, e := range errs {
			fmt.Fprintf(os.Stderr, "   • %s\n", e)
		}
		os.Exit(1)
	}

	fmt.Println("✅ Configuration is valid!\n")
	printConfigSummary(cfg)
	return nil
}

func printConfigSummary(cfg *config.Config) {
	fmt.Println("📋 Benchmark Plan:")

	// ── Connection ───────────────────────────────────────────────────────────
	fmt.Printf("   Connection   : %s\n", cfg.Connection.URI)
	fmt.Printf("   Target       : %s.%s\n", cfg.Connection.Database, cfg.Connection.Collection)
	fmt.Printf("   Read Pref    : %s | Read Concern: %s | Write Concern: %s\n",
		cfg.Connection.ReadPreference, cfg.Connection.ReadConcern, cfg.Connection.WriteConcern)
	fmt.Printf("   Pool Size    : %d\n\n", cfg.Connection.ConnectionPoolSize)

	// ── Workload ─────────────────────────────────────────────────────────────
	fmt.Printf("   Workload     : %s\n", cfg.Workload.Type)
	if cfg.Workload.Type == config.WorkloadCustom {
		w := cfg.Workload.Custom
		fmt.Printf("   Mix          : Read=%.0f%% Insert=%.0f%% Update=%.0f%% Delete=%.0f%% Scan=%.0f%% RMW=%.0f%%\n",
			w.Read, w.Insert, w.Update, w.Delete, w.Scan, w.ReadModifyWrite)
	}
	fmt.Printf("   Write All Fields : %v | Read All Fields : %v\n",
		cfg.Workload.WriteAllFields, cfg.Workload.ReadAllFields)
	fmt.Printf("   Scan Length  : %d–%d (%s distribution)\n",
		effectiveScanMin(cfg), effectiveScanMax(cfg), effectiveScanDist(cfg))

	// ── Execution ─────────────────────────────────────────────────────────────
	fmt.Printf("\n   Mode         : %s\n", cfg.Execution.Mode)
	switch cfg.Execution.Mode {
	case config.ModeTime:
		fmt.Printf("   Duration     : %s\n", cfg.Execution.Duration)
	case config.ModeOps:
		fmt.Printf("   Operations   : %d\n", cfg.Execution.OperationCount)
	case config.ModeRampup:
		r := cfg.Execution.Rampup
		fmt.Printf("   Ramp-up      : %d → %d threads (step +%d every %s)\n",
			r.InitialThreads, r.MaxThreads, r.StepSize, r.StepDuration)
	}
	// Don't show threads for rampup — it's controlled by the rampup config, not execution.threads
	if cfg.Execution.Mode != config.ModeRampup {
		fmt.Printf("   Threads      : %d\n", cfg.Execution.Threads)
	}
	if cfg.Execution.TargetOpsPerSec > 0 {
		fmt.Printf("   Target Rate  : %d ops/sec (token-bucket throttle)\n",
			cfg.Execution.TargetOpsPerSec)
	}

	// ── Key space ─────────────────────────────────────────────────────────────
	dist := cfg.Execution.KeyDistribution
	if dist == "" {
		dist = "uniform"
	}
	fmt.Printf("\n   Distribution : %s", dist)
	if dist == "zipfian" {
		fmt.Printf(" (θ=%.3f)", cfg.Execution.EffectiveZipfianConstant())
	}
	fmt.Println()

	rc := cfg.Execution.RecordCount
	if rc == 0 {
		rc = cfg.Phases.Preload.DocumentCount
	}
	fmt.Printf("   Record Count : %d\n", rc)

	keyFmt := cfg.Execution.EffectiveKeyPrefix() + "<N>"
	if cfg.Execution.KeyZeroPadding > 0 {
		keyFmt = fmt.Sprintf("%s<N> (zero-padded to %d digits)",
			cfg.Execution.EffectiveKeyPrefix(), cfg.Execution.KeyZeroPadding)
	}
	fmt.Printf("   Key Format   : %s\n", keyFmt)

	insertOrder := cfg.Execution.InsertOrdering
	if insertOrder == "" {
		insertOrder = "ordered"
	}
	fmt.Printf("   Insert Order : %s\n", insertOrder)

	// ── Phases ────────────────────────────────────────────────────────────────
	fmt.Printf("\n")
	if skipPreloadFlag {
		fmt.Printf("   Preload      : ⏭️  skipped (--skip-preload)\n")
	} else if !cfg.Phases.Preload.Enabled {
		fmt.Printf("   Preload      : disabled\n")
	} else {
		fmt.Printf("   Preload      : %d docs | %d threads\n",
			cfg.Phases.Preload.DocumentCount, cfg.Phases.Preload.Threads)
		if cfg.Phases.Preload.SkipIfExists {
			fmt.Printf("                  ↳ skipIfExists = true (skips if collection has data)\n")
		}
	}
	fmt.Printf("   Warmup       : %v (%s)\n", cfg.Phases.Warmup.Enabled, cfg.Phases.Warmup.Duration)

	// ── Indexes ───────────────────────────────────────────────────────────────
	if len(cfg.Indexes) == 0 {
		fmt.Printf("   Indexes      : none — only default _id index\n")
		fmt.Printf("                  ↳ matches original YCSB behaviour (no secondary indexes)\n")
	} else {
		fmt.Printf("   Indexes      : %d defined\n", len(cfg.Indexes))
		for i, idx := range cfg.Indexes {
			if len(idx.Fields) > 0 {
				fmt.Printf("      [%d] compound:", i)
				for _, f := range idx.Fields {
					fmt.Printf(" %s(%s)", f.Field, f.Type)
				}
				fmt.Printf(" sparse=%v unique=%v\n", idx.Sparse, idx.Unique)
			} else {
				fmt.Printf("      [%d] %s (%s) sparse=%v unique=%v\n",
					i, idx.Field, idx.Type, idx.Sparse, idx.Unique)
			}
		}
	}

	// ── Storage & Reporting ───────────────────────────────────────────────────
	fmt.Printf("\n   Store → MongoDB : %v | Local JSON : %v\n",
		cfg.Results.MongoDB.Enabled, cfg.Results.Local.Enabled)
	fmt.Printf("   Report → HTML   : %v | CSV        : %v\n",
		cfg.Reporting.HTML.Enabled, cfg.Reporting.CSV.Enabled)
	fmt.Printf("   Tags            : %v\n", cfg.Results.Tags)

	// ── Schedule ──────────────────────────────────────────────────────────────
	if cfg.Schedule.Enabled {
		printSchedulePlan(cfg)
	}
}

// printSchedulePlan prints the full schedule configuration and the next
// 5 trigger times so the user can verify the cron expression is correct.
func printSchedulePlan(cfg *config.Config) {
	bounds := cfg.Schedule.Bounds

	fmt.Printf("\n   ⏰ CRON Schedule\n")
	fmt.Printf("      Expression : %q\n", cfg.Schedule.Cron)
	fmt.Printf("      Bound Type : %s\n", bounds.Type)

	switch bounds.Type {
	case "runFor":
		fmt.Printf("      Run For    : %s\n", bounds.RunFor)
		if d, err := bounds.ParseRunFor(); err == nil {
			fmt.Printf("      Stops at   : ~%s\n",
				time.Now().Add(d).Format("2006-01-02 15:04:05 UTC"))
		}
	case "maxRuns":
		fmt.Printf("      Max Runs   : %d\n", bounds.MaxRuns)
	case "timeWindow":
		fmt.Printf("      Start At   : %s\n", bounds.StartAt)
		fmt.Printf("      Stop At    : %s\n", bounds.StopAt)
		if startAt, err := bounds.ParseStartAt(); err == nil && time.Now().Before(startAt) {
			fmt.Printf("      Wait Time  : %s until window opens\n",
				time.Until(startAt).Round(time.Second))
		}
	case "unlimited":
		fmt.Printf("      Runs indefinitely — stop with Ctrl+C\n")
	}

	// Compute next 5 trigger times
	triggers, err := nextCronTriggers(cfg.Schedule.Cron, 5, time.Now())
	if err != nil {
		fmt.Printf("\n      ⚠️  Could not parse cron expression: %v\n", err)
		return
	}

	fmt.Printf("\n      Next 5 trigger times:\n")
	for i, t := range triggers {
		marker := ""
		// For timeWindow, mark triggers that fall outside the window
		if bounds.Type == "timeWindow" {
			startAt, _ := bounds.ParseStartAt()
			stopAt, _ := bounds.ParseStopAt()
			if t.Before(startAt) {
				marker = "  ⏭️  (before window — will be skipped)"
			} else if t.After(stopAt) {
				marker = "  🛑 (after window — scheduler stopped)"
			} else {
				marker = "  ✅"
			}
		}
		fmt.Printf("         %d. %s%s\n", i+1,
			t.Format("2006-01-02 15:04:05 UTC"), marker)
	}

	// Estimated run count
	switch bounds.Type {
	case "runFor":
		if d, err := bounds.ParseRunFor(); err == nil {
			deadline := time.Now().Add(d)
			all, _ := nextCronTriggers(cfg.Schedule.Cron, 10000, time.Now())
			count := 0
			for _, t := range all {
				if t.After(deadline) {
					break
				}
				count++
			}
			fmt.Printf("\n      📊 Estimated runs in %s: %d\n", bounds.RunFor, count)
		}
	case "maxRuns":
		fmt.Printf("\n      📊 Will stop after exactly %d completed runs\n", bounds.MaxRuns)
	case "timeWindow":
		startAt, _ := bounds.ParseStartAt()
		stopAt, _ := bounds.ParseStopAt()
		all, _ := nextCronTriggers(cfg.Schedule.Cron, 10000, startAt)
		count := 0
		for _, t := range all {
			if t.After(stopAt) {
				break
			}
			count++
		}
		fmt.Printf("\n      📊 Estimated runs in window: %d\n", count)
	}
}

// nextCronTriggers returns the next n trigger times from the given start time.
func nextCronTriggers(expr string, n int, from time.Time) ([]time.Time, error) {
	parser := cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow)
	schedule, err := parser.Parse(expr)
	if err != nil {
		return nil, err
	}
	times := make([]time.Time, n)
	t := from
	for i := 0; i < n; i++ {
		t = schedule.Next(t)
		times[i] = t
	}
	return times, nil
}

// ── Scan config helpers ───────────────────────────────────────────────────────

func effectiveScanMin(cfg *config.Config) int {
	if cfg.Workload.Scan.MinLength <= 0 {
		return 1
	}
	return cfg.Workload.Scan.MinLength
}

func effectiveScanMax(cfg *config.Config) int {
	if cfg.Workload.Scan.MaxLength <= 0 {
		return 1000
	}
	return cfg.Workload.Scan.MaxLength
}

func effectiveScanDist(cfg *config.Config) string {
	if cfg.Workload.Scan.Distribution == "" {
		return "uniform"
	}
	return cfg.Workload.Scan.Distribution
}
