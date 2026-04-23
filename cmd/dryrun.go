package cmd

import (
    "fmt"
    "os"

    "github.com/yourusername/mongo-ycsb/internal/config"
    "github.com/spf13/cobra"
)

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
    fmt.Printf("   Mode         : %s\n", cfg.Execution.Mode)
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
    fmt.Printf("   Threads      : %d\n\n", cfg.Execution.Threads)

    fmt.Printf("   Preload      : %v (%d docs, %d threads)\n",
        cfg.Phases.Preload.Enabled, cfg.Phases.Preload.DocumentCount, cfg.Phases.Preload.Threads)
    fmt.Printf("   Warmup       : %v (%s)\n", cfg.Phases.Warmup.Enabled, cfg.Phases.Warmup.Duration)
    fmt.Printf("   Indexes      : %d defined\n\n", len(cfg.Indexes))

    fmt.Printf("   Store → MongoDB : %v | Local JSON : %v\n",
        cfg.Results.MongoDB.Enabled, cfg.Results.Local.Enabled)
    fmt.Printf("   Report → HTML   : %v | CSV        : %v\n",
        cfg.Reporting.HTML.Enabled, cfg.Reporting.CSV.Enabled)
    fmt.Printf("   Tags         : %v\n", cfg.Results.Tags)

    if cfg.Schedule.Enabled {
        fmt.Printf("\n   ⏰ CRON Schedule: %s\n", cfg.Schedule.Cron)
    }
}