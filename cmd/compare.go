package cmd

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"
	"github.com/yourusername/mongo-ycsb/internal/comparer"
	"github.com/yourusername/mongo-ycsb/internal/config"
)

var compareCmd = &cobra.Command{
	Use:   "compare <run-id-1> <run-id-2>",
	Short: "Diff two benchmark runs side by side",
	Long: `Compare latency, throughput, and error metrics between two stored benchmark runs.

Examples:
  # Compare by run ID
  mongo-ycsb compare <run-id-1> <run-id-2>

  # Compare by tag (most recent run per tag)
  mongo-ycsb compare --tag-a baseline --tag-b after-index

  # Output HTML report
  mongo-ycsb compare <run-id-1> <run-id-2> --output html`,
	RunE: compareRuns,
}

func init() {
	compareCmd.Flags().String("output", "console", "Output format: console | html | both")
	compareCmd.Flags().String("tag-a", "", "Tag for Run A (uses most recent run with this tag)")
	compareCmd.Flags().String("tag-b", "", "Tag for Run B (uses most recent run with this tag)")
	compareCmd.Flags().String("html-path", "./reports", "Directory for HTML comparison report")
}

func compareRuns(cmd *cobra.Command, args []string) error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	output, _ := cmd.Flags().GetString("output")
	tagA, _ := cmd.Flags().GetString("tag-a")
	tagB, _ := cmd.Flags().GetString("tag-b")
	htmlPath, _ := cmd.Flags().GetString("html-path")

	cmp := comparer.New(&cfg.Results)
	ctx := context.Background()

	var diff *comparer.Diff

	if tagA != "" && tagB != "" {
		// Tag-based comparison
		fmt.Printf("🔍 Comparing by tags: %q vs %q\n\n", tagA, tagB)
		diff, err = cmp.CompareByTags(ctx, tagA, tagB)
	} else if len(args) == 2 {
		// Run ID comparison
		fmt.Printf("🔍 Comparing runs:\n   A: %s\n   B: %s\n\n", args[0], args[1])
		diff, err = cmp.Compare(ctx, args[0], args[1])
	} else {
		return fmt.Errorf("provide either two run IDs or --tag-a and --tag-b flags")
	}

	if err != nil {
		return err
	}

	switch output {
	case "html":
		if err := diff.SaveHTML(htmlPath); err != nil {
			return err
		}
	case "both":
		diff.PrintConsole()
		if err := diff.SaveHTML(htmlPath); err != nil {
			return err
		}
	default: // console
		diff.PrintConsole()
	}

	return nil
}
