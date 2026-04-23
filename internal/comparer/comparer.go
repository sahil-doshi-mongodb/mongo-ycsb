package comparer

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/yourusername/mongo-ycsb/internal/config"
	"github.com/yourusername/mongo-ycsb/internal/models"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// Diff holds the side-by-side comparison of two runs.
type Diff struct {
	RunA *models.RunResult
	RunB *models.RunResult
}

// Comparer loads two RunResult documents and builds a Diff.
type Comparer struct {
	cfg *config.ResultsConfig
}

// New creates a Comparer.
func New(cfg *config.ResultsConfig) *Comparer {
	return &Comparer{cfg: cfg}
}

// Compare loads two runs by ID and returns a Diff.
// It tries MongoDB first, then falls back to local JSON.
func (c *Comparer) Compare(ctx context.Context, idA, idB string) (*Diff, error) {
	runA, err := c.load(ctx, idA)
	if err != nil {
		return nil, fmt.Errorf("failed to load run %s: %w", idA, err)
	}
	runB, err := c.load(ctx, idB)
	if err != nil {
		return nil, fmt.Errorf("failed to load run %s: %w", idB, err)
	}
	return &Diff{RunA: runA, RunB: runB}, nil
}

// CompareByTags loads the most recent run per tag and returns a Diff.
func (c *Comparer) CompareByTags(ctx context.Context, tagA, tagB string) (*Diff, error) {
	runA, err := c.loadByTag(ctx, tagA)
	if err != nil {
		return nil, fmt.Errorf("failed to load run with tag %q: %w", tagA, err)
	}
	runB, err := c.loadByTag(ctx, tagB)
	if err != nil {
		return nil, fmt.Errorf("failed to load run with tag %q: %w", tagB, err)
	}
	return &Diff{RunA: runA, RunB: runB}, nil
}

// PrintConsole renders the diff as a formatted console table.
func (d *Diff) PrintConsole() {
	a, b := d.RunA, d.RunB

	fmt.Printf("\n══════════════════════════════════════════════════════════════════════════════\n")
	fmt.Printf("🔍 Benchmark Comparison\n")
	fmt.Printf("══════════════════════════════════════════════════════════════════════════════\n\n")

	fmt.Printf("  %-20s  %-38s  %-38s\n", "", "Run A", "Run B")
	fmt.Printf("  %-20s  %-38s  %-38s\n", "────────────────────",
		"──────────────────────────────────────",
		"──────────────────────────────────────")
	fmt.Printf("  %-20s  %-38s  %-38s\n", "Run ID",
		truncate(a.RunID, 36), truncate(b.RunID, 36))
	fmt.Printf("  %-20s  %-38s  %-38s\n", "Timestamp",
		a.Timestamp.Format("2006-01-02 15:04:05"),
		b.Timestamp.Format("2006-01-02 15:04:05"))
	fmt.Printf("  %-20s  %-38s  %-38s\n", "Workload",
		a.Config.Workload, b.Config.Workload)
	fmt.Printf("  %-20s  %-38d  %-38d\n", "Threads",
		a.Config.Threads, b.Config.Threads)
	fmt.Printf("  %-20s  %-38s  %-38s\n", "Tags",
		joinTags(a.Tags), joinTags(b.Tags))

	fmt.Printf("\n  %-20s  %-38s  %-38s  %s\n",
		"Metric", "Run A", "Run B", "Delta")
	fmt.Printf("  %-20s  %-38s  %-38s  %s\n",
		"──────────────────", "──────────────────────────────────────",
		"──────────────────────────────────────", "───────────────")

	printMetricRow("Throughput (ops/s)",
		fmt.Sprintf("%.0f", a.Summary.OpsPerSec),
		fmt.Sprintf("%.0f", b.Summary.OpsPerSec),
		pctDelta(a.Summary.OpsPerSec, b.Summary.OpsPerSec), true)

	printMetricRow("Total Ops",
		fmt.Sprintf("%d", a.Summary.TotalOps),
		fmt.Sprintf("%d", b.Summary.TotalOps), "", false)

	printMetricRow("Errors",
		fmt.Sprintf("%d", a.Summary.TotalErrors),
		fmt.Sprintf("%d", b.Summary.TotalErrors), "", false)

	// Per-operation percentile comparison
	allOps := unionOps(a.Summary.ByOperation, b.Summary.ByOperation)
	for _, op := range allOps {
		ma, hasA := a.Summary.ByOperation[op]
		mb, hasB := b.Summary.ByOperation[op]

		fmt.Printf("\n  [%s]\n", op)

		valA := func(v float64, ok bool) string {
			if !ok {
				return "—"
			}
			return fmt.Sprintf("%.2f ms", v)
		}

		printMetricRow("  Mean",
			valA(ma.MeanMs, hasA), valA(mb.MeanMs, hasB),
			pctDeltaOpt(ma.MeanMs, mb.MeanMs, hasA && hasB), false)
		printMetricRow("  p50",
			valA(ma.P50Ms, hasA), valA(mb.P50Ms, hasB),
			pctDeltaOpt(ma.P50Ms, mb.P50Ms, hasA && hasB), false)
		printMetricRow("  p95",
			valA(ma.P95Ms, hasA), valA(mb.P95Ms, hasB),
			pctDeltaOpt(ma.P95Ms, mb.P95Ms, hasA && hasB), false)
		printMetricRow("  p99",
			valA(ma.P99Ms, hasA), valA(mb.P99Ms, hasB),
			pctDeltaOpt(ma.P99Ms, mb.P99Ms, hasA && hasB), false)
		printMetricRow("  p999",
			valA(ma.P999Ms, hasA), valA(mb.P999Ms, hasB),
			pctDeltaOpt(ma.P999Ms, mb.P999Ms, hasA && hasB), false)
	}

	fmt.Printf("\n══════════════════════════════════════════════════════════════════════════════\n")
}

// ── Internal helpers ──────────────────────────────────────────────────────────

func (c *Comparer) load(ctx context.Context, id string) (*models.RunResult, error) {
	// Try MongoDB first
	if c.cfg.MongoDB.Enabled && c.cfg.MongoDB.URI != "" {
		result, err := c.loadFromMongo(ctx, id)
		if err == nil {
			return result, nil
		}
		fmt.Printf("⚠️  MongoDB lookup failed for %s, trying local JSON: %v\n", id, err)
	}
	// Fall back to local JSON
	return c.loadFromJSON(id)
}

func (c *Comparer) loadByTag(ctx context.Context, tag string) (*models.RunResult, error) {
	if c.cfg.MongoDB.Enabled && c.cfg.MongoDB.URI != "" {
		result, err := c.loadByTagFromMongo(ctx, tag)
		if err == nil {
			return result, nil
		}
		fmt.Printf("⚠️  MongoDB tag lookup failed for %q: %v\n", tag, err)
	}
	return c.loadByTagFromJSON(tag)
}

func (c *Comparer) loadFromMongo(ctx context.Context, id string) (*models.RunResult, error) {
	client, err := mongo.Connect(ctx, options.Client().ApplyURI(c.cfg.MongoDB.URI))
	if err != nil {
		return nil, err
	}
	defer client.Disconnect(ctx)

	coll := client.Database(c.cfg.MongoDB.Database).Collection(c.cfg.MongoDB.Collection)
	var result models.RunResult
	err = coll.FindOne(ctx, bson.M{"run_id": id}).Decode(&result)
	if err != nil {
		return nil, err
	}
	return &result, nil
}

func (c *Comparer) loadByTagFromMongo(ctx context.Context, tag string) (*models.RunResult, error) {
	client, err := mongo.Connect(ctx, options.Client().ApplyURI(c.cfg.MongoDB.URI))
	if err != nil {
		return nil, err
	}
	defer client.Disconnect(ctx)

	coll := client.Database(c.cfg.MongoDB.Database).Collection(c.cfg.MongoDB.Collection)

	// Find most recent run with this tag
	opts := options.FindOne().SetSort(bson.D{{Key: "timestamp", Value: -1}})
	var result models.RunResult
	err = coll.FindOne(ctx, bson.M{"tags": tag}, opts).Decode(&result)
	if err != nil {
		return nil, err
	}
	return &result, nil
}

func (c *Comparer) loadFromJSON(id string) (*models.RunResult, error) {
	path := filepath.Join(c.cfg.Local.Path, id+".json")
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("local JSON not found at %s: %w", path, err)
	}
	defer f.Close()

	var result models.RunResult
	if err := json.NewDecoder(f).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode JSON: %w", err)
	}
	return &result, nil
}

func (c *Comparer) loadByTagFromJSON(tag string) (*models.RunResult, error) {
	// Scan all JSON files in the results dir and find most recent with this tag
	entries, err := os.ReadDir(c.cfg.Local.Path)
	if err != nil {
		return nil, fmt.Errorf("cannot read results dir %s: %w", c.cfg.Local.Path, err)
	}

	var best *models.RunResult
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".json" {
			continue
		}
		path := filepath.Join(c.cfg.Local.Path, e.Name())
		f, err := os.Open(path)
		if err != nil {
			continue
		}
		var r models.RunResult
		if err := json.NewDecoder(f).Decode(&r); err != nil {
			f.Close()
			continue
		}
		f.Close()

		for _, t := range r.Tags {
			if t == tag {
				if best == nil || r.Timestamp.After(best.Timestamp) {
					best = &r
				}
				break
			}
		}
	}

	if best == nil {
		return nil, fmt.Errorf("no run found with tag %q in %s", tag, c.cfg.Local.Path)
	}
	return best, nil
}

// ── Formatting helpers ────────────────────────────────────────────────────────

func printMetricRow(label, a, b, delta string, higherIsBetter bool) {
	_ = higherIsBetter // reserved for colour coding in future
	fmt.Printf("  %-20s  %-38s  %-38s  %s\n", label, a, b, delta)
}

func pctDelta(a, b float64) string {
	if a == 0 {
		return "—"
	}
	d := (b - a) / a * 100
	if d > 0 {
		return fmt.Sprintf("▲ +%.1f%%", d)
	}
	if d < 0 {
		return fmt.Sprintf("▼ %.1f%%", d)
	}
	return "= 0%"
}

func pctDeltaOpt(a, b float64, valid bool) string {
	if !valid {
		return "—"
	}
	return pctDelta(a, b)
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n-3] + "..."
}

func joinTags(tags []string) string {
	if len(tags) == 0 {
		return "—"
	}
	out := ""
	for i, t := range tags {
		if i > 0 {
			out += ", "
		}
		out += t
	}
	return out
}

func unionOps(a, b map[string]models.OpMetric) []string {
	seen := make(map[string]bool)
	var out []string
	for k := range a {
		if !seen[k] {
			seen[k] = true
			out = append(out, k)
		}
	}
	for k := range b {
		if !seen[k] {
			seen[k] = true
			out = append(out, k)
		}
	}
	return out
}
