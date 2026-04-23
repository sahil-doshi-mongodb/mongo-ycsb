package cmd

import (
    "fmt"

    "github.com/spf13/cobra"
)

var compareCmd = &cobra.Command{
    Use:   "compare <run-id-1> <run-id-2>",
    Short: "Diff two benchmark runs side by side",
    Long: `Compare latency, throughput, and error metrics between two stored benchmark runs.
Run IDs are printed at the end of each benchmark run and stored in MongoDB or local JSON.`,
    Args: cobra.ExactArgs(2),
    RunE: compareRuns,
}

func init() {
    compareCmd.Flags().String("output", "console", "Output format: console | html | json")
    compareCmd.Flags().String("results-uri", "", "MongoDB URI for the results DB (overrides config)")
    compareCmd.Flags().String("results-path", "./results", "Path to local JSON results directory")
}

func compareRuns(cmd *cobra.Command, args []string) error {
    runID1, runID2 := args[0], args[1]
    output, _ := cmd.Flags().GetString("output")

    fmt.Printf("🔍 Comparing runs:\n")
    fmt.Printf("   [1] %s\n", runID1)
    fmt.Printf("   [2] %s\n", runID2)
    fmt.Printf("   Output: %s\n", output)

    // TODO (Step 5): Load both run documents and render comparison
    fmt.Println("\n🚧 Comparison engine coming in Step 5...")
    return nil
}