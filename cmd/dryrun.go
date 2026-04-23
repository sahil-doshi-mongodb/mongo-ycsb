package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"github.com/yourusername/mongo-ycsb/internal/config"
)

// skipPreloadFlag is set by runCmd and scheduleCmd — used by printConfigSummary
// to show the correct preload status without creating a circular reference.
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
	fmt.Printf("   Connection   : %s\n", cfg.Connection.URI)
	fmt.Printf("   Target       : %s.%s\n", cfg.Connection.Database, cfg.Connection.Collection)
	fmt.Printf("   Read Pref    : %s | Read Concern: %s | Write Concern: %s\n",
		cfg.Connection.ReadPreference, cfg.Connection.ReadConcern, cfg.Connection.WriteConcern)
	fmt.Printf("   Pool Size    : %d\n\n", cfg.Connection.ConnectionPoolSize)

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
	fmt.Printf("   Threads      : %d\n", cfg.Execution.Threads)
	if cfg.Execution.TargetOpsPerSec > 0 {
		fmt.Printf("   Target Rate  : %d ops/sec\n", cfg.Execution.TargetOpsPerSec)
	}

	// Key distribution
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

	// Phases
	fmt.Printf("\n")
	if skipPreloadFlag {
		fmt.Printf("   Preload      : ⏭️  skipped (--skip-preload)\n")
	} else {
		fmt.Printf("   Preload      : %v (%d docs, %d threads)\n",
			cfg.Phases.Preload.Enabled, cfg.Phases.Preload.DocumentCount, cfg.Phases.Preload.Threads)
		if cfg.Phases.Preload.SkipIfExists {
			fmt.Printf("                  (skipIfExists = true)\n")
		}
	}
	fmt.Printf("   Warmup       : %v (%s)\n", cfg.Phases.Warmup.Enabled, cfg.Phases.Warmup.Duration)
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

	// Storage & reporting
	fmt.Printf("\n   Store → MongoDB : %v | Local JSON : %v\n",
		cfg.Results.MongoDB.Enabled, cfg.Results.Local.Enabled)
	fmt.Printf("   Report → HTML   : %v | CSV        : %v\n",
		cfg.Reporting.HTML.Enabled, cfg.Reporting.CSV.Enabled)
	fmt.Printf("   Tags            : %v\n", cfg.Results.Tags)

	// Schedule
	if cfg.Schedule.Enabled {
		fmt.Printf("\n   ⏰ CRON Schedule : %s\n", cfg.Schedule.Cron)
		if cfg.Schedule.RunFor != "" {
			fmt.Printf("      Run For      : %s\n", cfg.Schedule.RunFor)
		}
		if cfg.Schedule.MaxRuns > 0 {
			fmt.Printf("      Max Runs     : %d\n", cfg.Schedule.MaxRuns)
		}
		if cfg.Schedule.StartAt != "" {
			fmt.Printf("      Start At     : %s\n", cfg.Schedule.StartAt)
		}
		if cfg.Schedule.StopAt != "" {
			fmt.Printf("      Stop At      : %s\n", cfg.Schedule.StopAt)
		}
	}
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
