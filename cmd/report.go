package cmd

import (
    "fmt"

    "github.com/spf13/cobra"
)

var reportCmd = &cobra.Command{
    Use:   "report <run-id>",
    Short: "Generate an HTML report for a completed benchmark run",
    Long:  `Generate a self-contained HTML file with latency, throughput, and system metrics charts.`,
    Args:  cobra.ExactArgs(1),
    RunE:  generateReport,
}

func init() {
    reportCmd.Flags().String("output", "./reports", "Directory to write the HTML report")
    reportCmd.Flags().String("results-uri", "", "MongoDB URI for the results DB (overrides config)")
    reportCmd.Flags().String("results-path", "./results", "Path to local JSON results directory")
}

func generateReport(cmd *cobra.Command, args []string) error {
    runID := args[0]
    outputPath, _ := cmd.Flags().GetString("output")

    fmt.Printf("📊 Generating HTML report\n")
    fmt.Printf("   Run ID : %s\n", runID)
    fmt.Printf("   Output : %s/%s.html\n", outputPath, runID)

    // TODO (Step 4): Load result doc, render HTML template with Chart.js
    fmt.Println("\n🚧 HTML reporter coming in Step 4...")
    return nil
}