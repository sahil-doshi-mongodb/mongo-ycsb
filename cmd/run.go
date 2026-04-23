package cmd

import (
	"context"
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"github.com/yourusername/mongo-ycsb/internal/config"
	"github.com/yourusername/mongo-ycsb/internal/orchestrator"
	"go.uber.org/zap"
)

var runCmd = &cobra.Command{
	Use:   "run",
	Short: "Run the benchmark",
	Long:  `Execute a MongoDB benchmark with the given configuration.`,
	RunE:  runBenchmark,
}

func init() {
	runCmd.Flags().String("uri", "", "MongoDB connection URI (overrides config)")
	runCmd.Flags().String("database", "", "Database name (overrides config)")
	runCmd.Flags().String("collection", "", "Collection name (overrides config)")
	runCmd.Flags().String("workload", "", "Workload type: A–F or custom (overrides config)")
	runCmd.Flags().Int("threads", 0, "Number of worker goroutines (overrides config)")
	runCmd.Flags().String("duration", "", "Benchmark duration e.g. 5m, 1h (overrides config)")
	runCmd.Flags().Int64("ops", 0, "Total operations to run (overrides config)")
	runCmd.Flags().StringSlice("tags", nil, "Tags to label this run e.g. baseline,after-index")
	runCmd.Flags().Bool("skip-preload", false, "Skip preload and use existing collection data")
}

func runBenchmark(cmd *cobra.Command, args []string) error {
	applyRunOverrides(cmd)

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

	printConfigSummary(cfg)

	log, err := zap.NewProduction()
	if err != nil {
		return fmt.Errorf("failed to create logger: %w", err)
	}
	defer log.Sync()

	skipPreload, _ := cmd.Flags().GetBool("skip-preload")

	orch := orchestrator.New(cfg, log, skipPreload)
	_, err = orch.Run(context.Background())
	return err
}

func applyRunOverrides(cmd *cobra.Command) {
	if v, _ := cmd.Flags().GetString("uri"); v != "" {
		viper.Set("connection.uri", v)
	}
	if v, _ := cmd.Flags().GetString("database"); v != "" {
		viper.Set("connection.database", v)
	}
	if v, _ := cmd.Flags().GetString("collection"); v != "" {
		viper.Set("connection.collection", v)
	}
	if v, _ := cmd.Flags().GetString("workload"); v != "" {
		viper.Set("workload.type", v)
	}
	if v, _ := cmd.Flags().GetInt("threads"); v > 0 {
		viper.Set("execution.threads", v)
	}
	if v, _ := cmd.Flags().GetString("duration"); v != "" {
		viper.Set("execution.duration", v)
		viper.Set("execution.mode", "time")
	}
	if v, _ := cmd.Flags().GetInt64("ops"); v > 0 {
		viper.Set("execution.operationCount", v)
		viper.Set("execution.mode", "ops")
	}
	if v, _ := cmd.Flags().GetStringSlice("tags"); len(v) > 0 {
		viper.Set("results.tags", v)
	}
}
