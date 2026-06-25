package cmd

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/sahil-doshi-mongodb/mongo-ycsb/internal/explorer"
)

var exploreCmd = &cobra.Command{
	Use:   "explore",
	Short: "Launch an interactive web UI to browse and compare benchmark results",
	Long: `explore connects to a results cluster and starts a local web server that lists
all stored benchmark runs and lets you select and compare up to 5 of them live,
with one-click PDF and Excel export.

Examples:
  # Explicit flags
  mongo-ycsb explore \
    --results-uri "mongodb+srv://user:pass@cluster.mongodb.net/" \
    --results-db ycsb_results \
    --results-collection runs

  # Fall back to results.mongodb.* from a config file
  mongo-ycsb explore --config configs/mytest.yaml`,

	// Override the root PersistentPreRunE so explore does NOT require a config
	// file. We load config only if one is available, and never error if absent.
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		return initConfigOptional()
	},
	RunE: runExplore,
}

func init() {
	exploreCmd.Flags().String("results-uri", "", "MongoDB connection string for the results cluster (overrides config)")
	exploreCmd.Flags().String("results-db", "", "Results database name (overrides config)")
	exploreCmd.Flags().String("results-collection", "", "Results collection name (overrides config)")
	exploreCmd.Flags().Int("port", 7070, "Local port for the web UI")
	exploreCmd.Flags().Bool("no-open", false, "Do not auto-open the browser")
}

func runExplore(cmd *cobra.Command, args []string) error {
	uri, _ := cmd.Flags().GetString("results-uri")
	db, _ := cmd.Flags().GetString("results-db")
	coll, _ := cmd.Flags().GetString("results-collection")
	port, _ := cmd.Flags().GetInt("port")
	noOpen, _ := cmd.Flags().GetBool("no-open")

	// Fall back to config (results.mongodb.*) when flags are not provided.
	if uri == "" {
		uri = viper.GetString("results.mongodb.uri")
	}
	if db == "" {
		db = viper.GetString("results.mongodb.database")
	}
	if coll == "" {
		coll = viper.GetString("results.mongodb.collection")
	}

	if uri == "" || db == "" || coll == "" {
		return fmt.Errorf("results connection incomplete: provide --results-uri, --results-db and " +
			"--results-collection (or set results.mongodb.* in a config file)")
	}

	return explorer.Serve(context.Background(), explorer.Options{
		URI:         uri,
		Database:    db,
		Collection:  coll,
		Port:        port,
		OpenBrowser: !noOpen,
	})
}
