package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var cfgFile string

var rootCmd = &cobra.Command{
	Use:   "mongo-ycsb",
	Short: "A MongoDB YCSB-compatible benchmarking tool",
	Long: `mongo-ycsb is a comprehensive benchmarking tool for MongoDB.
It supports standard YCSB workloads (A–F), custom workloads, ramp-up
concurrency testing, rich reporting, and CRON scheduling.`,

	// PersistentPreRunE runs for ALL subcommands, AFTER flags are parsed.
	// This replaces cobra.OnInitialize which ran too early (before flag parsing).
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		return initConfig()
	},
}

func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func init() {
	rootCmd.PersistentFlags().StringVar(
		&cfgFile, "config", "",
		"config file path (default: ./configs/default.yaml)",
	)

	rootCmd.AddCommand(runCmd)
	rootCmd.AddCommand(dryRunCmd)
	rootCmd.AddCommand(compareCmd)
	rootCmd.AddCommand(reportCmd)
}

// initConfig is called by PersistentPreRunE — flags are guaranteed to be
// parsed at this point, so cfgFile will have the correct value.
func initConfig() error {
	if cfgFile != "" {
		viper.SetConfigFile(cfgFile)
	} else {
		viper.AddConfigPath("./configs")
		viper.SetConfigName("default")
		viper.SetConfigType("yaml")
	}

	// Allow env var overrides: MONGOYCSB_CONNECTION_URI, etc.
	viper.SetEnvPrefix("MONGOYCSB")
	viper.AutomaticEnv()

	if err := viper.ReadInConfig(); err != nil {
		// Provide a clear, actionable error message
		return fmt.Errorf(
			"❌ Could not read config file %q — does the file exist?\n   Error: %w",
			viper.ConfigFileUsed(),
			err,
		)
	}

	fmt.Println("📄 Using config:", viper.ConfigFileUsed())
	return nil
}
