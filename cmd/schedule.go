package cmd

import (
	"context"
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"github.com/sahil-doshi-mongodb/mongo-ycsb/internal/config"
	"github.com/sahil-doshi-mongodb/mongo-ycsb/internal/orchestrator"
	"github.com/sahil-doshi-mongodb/mongo-ycsb/internal/scheduler"
	"go.uber.org/zap"
)

var scheduleCmd = &cobra.Command{
	Use:   "schedule",
	Short: "Run benchmarks on a CRON schedule",
	Long: `Run benchmarks repeatedly according to the schedule defined in config.

The scheduler respects four optional bounding rules (first limit hit wins):
  schedule.startAt  — don't fire before this RFC3339 timestamp
  schedule.stopAt   — stop firing after this RFC3339 timestamp
  schedule.runFor   — stop after this total wall-clock duration (e.g. "600s", "2h")
  schedule.maxRuns  — stop after N completed runs

Example config:
  schedule:
    enabled: true
    cron: "*/5 * * * *"   # every 5 minutes
    runFor: "1h"           # for 1 hour total
    maxRuns: 10            # or at most 10 runs`,
	RunE: runSchedule,
}

func init() {
	scheduleCmd.Flags().Bool("skip-preload", false, "Skip preload on every scheduled run")
}

func runSchedule(cmd *cobra.Command, args []string) error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	if errs := cfg.Validate(); len(errs) > 0 {
		fmt.Fprintln(os.Stderr, "❌ Configuration errors:")
		for _, e := range errs {
			fmt.Fprintf(os.Stderr, "   • %s\n", e)
		}
		return fmt.Errorf("fix the above errors and try again")
	}

	if !cfg.Schedule.Enabled {
		return fmt.Errorf("schedule.enabled is false — set it to true in your config")
	}

	skipPreload, _ := cmd.Flags().GetBool("skip-preload")

	log, err := zap.NewProduction()
	if err != nil {
		return fmt.Errorf("failed to create logger: %w", err)
	}
	defer log.Sync()

	// The run function is called on each CRON trigger.
	// A fresh orchestrator (and run ID) is created per trigger.
	runFn := func(ctx context.Context) error {
		orch := orchestrator.New(cfg, log, skipPreload)
		_, err := orch.Run(ctx)
		return err
	}

	sched := scheduler.New(&cfg.Schedule, log, runFn)
	total, err := sched.Start(context.Background())
	if err != nil && err != context.DeadlineExceeded && err != context.Canceled {
		return err
	}

	fmt.Printf("\n✅ Schedule complete — %d run(s) completed\n", total)
	return nil
}
