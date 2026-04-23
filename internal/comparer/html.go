package comparer

import (
	"encoding/json"
	"fmt"
	"html/template"
	"os"
	"path/filepath"
)

// SaveHTML writes a self-contained HTML comparison report.
func (d *Diff) SaveHTML(outputPath string) error {
	if err := os.MkdirAll(outputPath, 0755); err != nil {
		return fmt.Errorf("create reports dir: %w", err)
	}

	filename := fmt.Sprintf("compare_%s_vs_%s.html",
		d.RunA.RunID[:8], d.RunB.RunID[:8])
	path := filepath.Join(outputPath, filename)

	f, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("create HTML file: %w", err)
	}
	defer f.Close()

	tmpl, err := template.New("compare").Parse(compareTemplate)
	if err != nil {
		return fmt.Errorf("parse template: %w", err)
	}

	data, err := d.buildCompareData()
	if err != nil {
		return fmt.Errorf("build compare data: %w", err)
	}

	if err := tmpl.Execute(f, data); err != nil {
		return fmt.Errorf("render template: %w", err)
	}

	fmt.Printf("📊 HTML comparison report → %s\n", path)
	return nil
}

type compareData struct {
	RunA        runMeta
	RunB        runMeta
	Ops         []opCompareRow
	DeltaLabels template.JS
	AOpsSeries  template.JS
	BOpsSeries  template.JS
	AP99Series  template.JS
	BP99Series  template.JS
}

type runMeta struct {
	RunID     string
	Timestamp string
	Workload  string
	Threads   int
	Tags      string
	OpsPerSec float64
	TotalOps  int64
	Errors    int64
}

type opCompareRow struct {
	Op      string
	AMean   float64
	BMean   float64
	AP50    float64
	BP50    float64
	AP95    float64
	BP95    float64
	AP99    float64
	BP99    float64
	AP999   float64
	BP999   float64
	PctDiff float64 // p99 B vs A
}

func (d *Diff) buildCompareData() (*compareData, error) {
	toJS := func(v interface{}) (template.JS, error) {
		b, err := json.Marshal(v)
		return template.JS(b), err
	}

	// Delta time-series — align by index, pad shorter series
	maxLen := len(d.RunA.Delta)
	if len(d.RunB.Delta) > maxLen {
		maxLen = len(d.RunB.Delta)
	}

	labels := make([]string, maxLen)
	aOps := make([]float64, maxLen)
	bOps := make([]float64, maxLen)
	aP99 := make([]float64, maxLen)
	bP99 := make([]float64, maxLen)

	for i := 0; i < maxLen; i++ {
		labels[i] = fmt.Sprintf("%ds", i+1)
		if i < len(d.RunA.Delta) {
			aOps[i] = d.RunA.Delta[i].OpsPerSec
			aP99[i] = d.RunA.Delta[i].P99Ms
		}
		if i < len(d.RunB.Delta) {
			bOps[i] = d.RunB.Delta[i].OpsPerSec
			bP99[i] = d.RunB.Delta[i].P99Ms
		}
	}

	// Operation rows
	allOps := unionOps(d.RunA.Summary.ByOperation, d.RunB.Summary.ByOperation)
	var rows []opCompareRow
	for _, op := range allOps {
		ma := d.RunA.Summary.ByOperation[op]
		mb := d.RunB.Summary.ByOperation[op]
		pct := 0.0
		if ma.P99Ms > 0 {
			pct = (mb.P99Ms - ma.P99Ms) / ma.P99Ms * 100
		}
		rows = append(rows, opCompareRow{
			Op:    op,
			AMean: ma.MeanMs, BMean: mb.MeanMs,
			AP50: ma.P50Ms, BP50: mb.P50Ms,
			AP95: ma.P95Ms, BP95: mb.P95Ms,
			AP99: ma.P99Ms, BP99: mb.P99Ms,
			AP999: ma.P999Ms, BP999: mb.P999Ms,
			PctDiff: pct,
		})
	}

	dlJS, _ := toJS(labels)
	aoJS, _ := toJS(aOps)
	boJS, _ := toJS(bOps)
	apJS, _ := toJS(aP99)
	bpJS, err := toJS(bP99)
	if err != nil {
		return nil, err
	}

	return &compareData{
		RunA: runMeta{
			RunID:     d.RunA.RunID,
			Timestamp: d.RunA.Timestamp.Format("2006-01-02 15:04:05"),
			Workload:  d.RunA.Config.Workload,
			Threads:   d.RunA.Config.Threads,
			Tags:      joinTags(d.RunA.Tags),
			OpsPerSec: d.RunA.Summary.OpsPerSec,
			TotalOps:  d.RunA.Summary.TotalOps,
			Errors:    d.RunA.Summary.TotalErrors,
		},
		RunB: runMeta{
			RunID:     d.RunB.RunID,
			Timestamp: d.RunB.Timestamp.Format("2006-01-02 15:04:05"),
			Workload:  d.RunB.Config.Workload,
			Threads:   d.RunB.Config.Threads,
			Tags:      joinTags(d.RunB.Tags),
			OpsPerSec: d.RunB.Summary.OpsPerSec,
			TotalOps:  d.RunB.Summary.TotalOps,
			Errors:    d.RunB.Summary.TotalErrors,
		},
		Ops:         rows,
		DeltaLabels: dlJS,
		AOpsSeries:  aoJS,
		BOpsSeries:  boJS,
		AP99Series:  apJS,
		BP99Series:  bpJS,
	}, nil
}

const compareTemplate = `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width, initial-scale=1.0">
<title>mongo-ycsb Comparison</title>
<script src="https://cdn.jsdelivr.net/npm/chart.js@4.4.0/dist/chart.umd.min.js"></script>
<style>
  * { box-sizing: border-box; margin: 0; padding: 0; }
  body { font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif;
         background: #f4f6f9; color: #1a1a2e; padding: 24px; }
  h1 { font-size: 1.5rem; font-weight: 700; color: #00684a; margin-bottom: 4px; }
  .subtitle { font-size: 0.85rem; color: #888; margin-bottom: 24px; }
  .run-grid { display: grid; grid-template-columns: 1fr 1fr; gap: 16px; margin-bottom: 24px; }
  .run-card { background: #fff; border-radius: 10px; padding: 20px;
              box-shadow: 0 1px 4px rgba(0,0,0,.08); }
  .run-card.a { border-top: 4px solid #00684a; }
  .run-card.b { border-top: 4px solid #ff6b35; }
  .run-card h2 { font-size: 0.9rem; font-weight: 700; margin-bottom: 12px; }
  .run-card .metric { display: flex; justify-content: space-between;
                      font-size: 0.85rem; padding: 4px 0;
                      border-bottom: 1px solid #f5f5f5; }
  .run-card .metric:last-child { border-bottom: none; }
  .run-card .metric .val { font-weight: 600; }
  .section { background: #fff; border-radius: 10px; padding: 24px;
             box-shadow: 0 1px 4px rgba(0,0,0,.08); margin-bottom: 24px; }
  .section h2 { font-size: 1rem; font-weight: 600; margin-bottom: 16px;
                color: #1a1a2e; border-bottom: 2px solid #f0f0f0; padding-bottom: 8px; }
  table { width: 100%; border-collapse: collapse; font-size: 0.82rem; }
  th { text-align: right; padding: 8px 10px; background: #f8f9fa;
       color: #555; font-weight: 600; border-bottom: 2px solid #e0e0e0; }
  th:first-child { text-align: left; }
  td { text-align: right; padding: 8px 10px; border-bottom: 1px solid #f0f0f0; }
  td:first-child { text-align: left; font-weight: 600; }
  .better { color: #00684a; font-weight: 700; }
  .worse  { color: #e63946; font-weight: 700; }
  .chart-row { display: grid; grid-template-columns: 1fr 1fr; gap: 24px; margin-bottom: 24px; }
  @media (max-width: 768px) {
    .run-grid, .chart-row { grid-template-columns: 1fr; }
  }
  .legend { display: flex; gap: 16px; font-size: 0.8rem; margin-bottom: 8px; }
  .legend-dot { width: 12px; height: 12px; border-radius: 50%;
                display: inline-block; margin-right: 4px; }
</style>
</head>
<body>

<h1>mongo-ycsb Benchmark Comparison</h1>
<div class="subtitle">Run A vs Run B — side-by-side analysis</div>

<!-- Run cards -->
<div class="run-grid">
  <div class="run-card a">
    <h2>🟢 Run A</h2>
    <div class="metric"><span>Run ID</span><span class="val" style="font-size:0.75rem">{{.RunA.RunID}}</span></div>
    <div class="metric"><span>Timestamp</span><span class="val">{{.RunA.Timestamp}}</span></div>
    <div class="metric"><span>Workload</span><span class="val">{{.RunA.Workload}}</span></div>
    <div class="metric"><span>Threads</span><span class="val">{{.RunA.Threads}}</span></div>
    <div class="metric"><span>Tags</span><span class="val">{{.RunA.Tags}}</span></div>
    <div class="metric"><span>Throughput</span><span class="val">{{printf "%.0f" .RunA.OpsPerSec}} ops/s</span></div>
    <div class="metric"><span>Total Ops</span><span class="val">{{.RunA.TotalOps}}</span></div>
    <div class="metric"><span>Errors</span><span class="val">{{.RunA.Errors}}</span></div>
  </div>
  <div class="run-card b">
    <h2>🟠 Run B</h2>
    <div class="metric"><span>Run ID</span><span class="val" style="font-size:0.75rem">{{.RunB.RunID}}</span></div>
    <div class="metric"><span>Timestamp</span><span class="val">{{.RunB.Timestamp}}</span></div>
    <div class="metric"><span>Workload</span><span class="val">{{.RunB.Workload}}</span></div>
    <div class="metric"><span>Threads</span><span class="val">{{.RunB.Threads}}</span></div>
    <div class="metric"><span>Tags</span><span class="val">{{.RunB.Tags}}</span></div>
    <div class="metric"><span>Throughput</span><span class="val">{{printf "%.0f" .RunB.OpsPerSec}} ops/s</span></div>
    <div class="metric"><span>Total Ops</span><span class="val">{{.RunB.TotalOps}}</span></div>
    <div class="metric"><span>Errors</span><span class="val">{{.RunB.Errors}}</span></div>
  </div>
</div>

<!-- Percentile comparison table -->
<div class="section">
  <h2>Latency Comparison</h2>
  <table>
    <thead>
      <tr>
        <th>Operation</th>
        <th>A Mean</th><th>B Mean</th>
        <th>A p50</th><th>B p50</th>
        <th>A p95</th><th>B p95</th>
        <th>A p99</th><th>B p99</th>
        <th>A p999</th><th>B p999</th>
        <th>p99 Δ</th>
      </tr>
    </thead>
    <tbody>
      {{range .Ops}}
      <tr>
        <td>{{.Op}}</td>
        <td>{{printf "%.2f" .AMean}}</td><td>{{printf "%.2f" .BMean}}</td>
        <td>{{printf "%.2f" .AP50}}</td><td>{{printf "%.2f" .BP50}}</td>
        <td>{{printf "%.2f" .AP95}}</td><td>{{printf "%.2f" .BP95}}</td>
        <td>{{printf "%.2f" .AP99}}</td><td>{{printf "%.2f" .BP99}}</td>
        <td>{{printf "%.2f" .AP999}}</td><td>{{printf "%.2f" .BP999}}</td>
        <td>{{if lt .PctDiff 0.0}}
              <span class="better">▼ {{printf "%.1f" .PctDiff}}%</span>
            {{else if gt .PctDiff 0.0}}
              <span class="worse">▲ +{{printf "%.1f" .PctDiff}}%</span>
            {{else}}={{end}}</td>
      </tr>
      {{end}}
    </tbody>
  </table>
</div>

<!-- Charts -->
<div class="chart-row">
  <div class="section">
    <h2>Throughput Over Time (ops/sec)</h2>
    <div class="legend">
      <span><span class="legend-dot" style="background:#00684a"></span>Run A</span>
      <span><span class="legend-dot" style="background:#ff6b35"></span>Run B</span>
    </div>
    <canvas id="opsChart"></canvas>
  </div>
  <div class="section">
    <h2>p99 Latency Over Time (ms)</h2>
    <div class="legend">
      <span><span class="legend-dot" style="background:#00684a"></span>Run A</span>
      <span><span class="legend-dot" style="background:#ff6b35"></span>Run B</span>
    </div>
    <canvas id="p99Chart"></canvas>
  </div>
</div>

<script>
const labels   = {{.DeltaLabels}};
const aOps     = {{.AOpsSeries}};
const bOps     = {{.BOpsSeries}};
const aP99     = {{.AP99Series}};
const bP99     = {{.BP99Series}};

function twoSeries(canvasId, label1, data1, label2, data2, color1, color2) {
  new Chart(document.getElementById(canvasId), {
    type: 'line',
    data: {
      labels,
      datasets: [
        { label: label1, data: data1, borderColor: color1,
          backgroundColor: color1+'22', borderWidth: 2,
          pointRadius: 2, fill: true, tension: 0.3 },
        { label: label2, data: data2, borderColor: color2,
          backgroundColor: color2+'22', borderWidth: 2,
          pointRadius: 2, fill: true, tension: 0.3 },
      ]
    },
    options: {
      responsive: true,
      plugins: { legend: { display: false } },
      scales: { x: { ticks: { maxTicksLimit: 10 } }, y: { beginAtZero: true } }
    }
  });
}

twoSeries('opsChart', 'Run A ops/s', aOps, 'Run B ops/s', bOps, '#00684a', '#ff6b35');
twoSeries('p99Chart', 'Run A p99',   aP99, 'Run B p99',   bP99, '#00684a', '#ff6b35');
</script>

</body>
</html>`
