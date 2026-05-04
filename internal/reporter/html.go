package reporter

import (
	"encoding/json"
	"fmt"
	"html/template"
	"os"
	"path/filepath"

	"github.com/sahil-doshi-mongodb/mongo-ycsb/internal/config"
	"github.com/sahil-doshi-mongodb/mongo-ycsb/internal/models"
)

// HTMLReporter generates a self-contained HTML report with Chart.js charts.
type HTMLReporter struct {
	cfg *config.HTMLConfig
}

// NewHTMLReporter creates an HTMLReporter.
func NewHTMLReporter(cfg *config.HTMLConfig) *HTMLReporter {
	return &HTMLReporter{cfg: cfg}
}

// Save generates the HTML report file.
func (r *HTMLReporter) Save(result *models.RunResult) error {
	if !r.cfg.Enabled {
		return nil
	}

	if err := os.MkdirAll(r.cfg.OutputPath, 0755); err != nil {
		return fmt.Errorf("create reports dir: %w", err)
	}

	path := filepath.Join(r.cfg.OutputPath, result.RunID+".html")
	f, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("create HTML file: %w", err)
	}
	defer f.Close()

	tmpl, err := template.New("report").Parse(htmlTemplate)
	if err != nil {
		return fmt.Errorf("parse HTML template: %w", err)
	}

	data, err := buildTemplateData(result)
	if err != nil {
		return fmt.Errorf("build template data: %w", err)
	}

	if err := tmpl.Execute(f, data); err != nil {
		return fmt.Errorf("render HTML: %w", err)
	}

	fmt.Printf("📊 HTML report → %s\n", path)
	return nil
}

// templateData holds all values injected into the HTML template.
type templateData struct {
	RunID           string
	Timestamp       string
	Workload        string
	Mode            string
	Threads         int
	DurationSeconds float64
	TotalOps        int64
	TotalErrors     int64
	OpsPerSec       float64
	Tags            string
	Operations      []opRow

	// JSON-encoded arrays for Chart.js
	DeltaLabels   template.JS
	DeltaOpsData  template.JS
	DeltaP99Data  template.JS
	SysTimeLabels template.JS
	SysCPUData    template.JS
	SysMemData    template.JS
}

type opRow struct {
	Name   string
	Count  int64
	Errors int64
	MeanMs float64
	P50Ms  float64
	P95Ms  float64
	P99Ms  float64
	P999Ms float64
}

func buildTemplateData(r *models.RunResult) (*templateData, error) {
	// Operation rows
	var ops []opRow
	for name, m := range r.Summary.ByOperation {
		ops = append(ops, opRow{
			Name:   name,
			Count:  m.Count,
			Errors: m.Errors,
			MeanMs: m.MeanMs,
			P50Ms:  m.P50Ms,
			P95Ms:  m.P95Ms,
			P99Ms:  m.P99Ms,
			P999Ms: m.P999Ms,
		})
	}

	// Delta chart data
	deltaLabels := make([]string, len(r.Delta))
	deltaOps := make([]float64, len(r.Delta))
	deltaP99 := make([]float64, len(r.Delta))
	for i, d := range r.Delta {
		deltaLabels[i] = fmt.Sprintf("%.0fs", d.OffsetSeconds)
		deltaOps[i] = d.OpsPerSec
		deltaP99[i] = d.P99Ms
	}

	// System chart data
	sysLabels := make([]string, len(r.SystemSamples))
	sysCPU := make([]float64, len(r.SystemSamples))
	sysMem := make([]float64, len(r.SystemSamples))
	for i, s := range r.SystemSamples {
		sysLabels[i] = fmt.Sprintf("%.0fs", s.OffsetSeconds)
		sysCPU[i] = s.CPUPercent
		sysMem[i] = s.MemoryMB
	}

	toJS := func(v interface{}) (template.JS, error) {
		b, err := json.Marshal(v)
		if err != nil {
			return "", err
		}
		return template.JS(b), nil
	}

	dlJS, err := toJS(deltaLabels)
	if err != nil {
		return nil, err
	}
	doJS, err := toJS(deltaOps)
	if err != nil {
		return nil, err
	}
	dpJS, err := toJS(deltaP99)
	if err != nil {
		return nil, err
	}
	slJS, err := toJS(sysLabels)
	if err != nil {
		return nil, err
	}
	scJS, err := toJS(sysCPU)
	if err != nil {
		return nil, err
	}
	smJS, err := toJS(sysMem)
	if err != nil {
		return nil, err
	}

	tags := ""
	for i, t := range r.Tags {
		if i > 0 {
			tags += ", "
		}
		tags += t
	}

	return &templateData{
		RunID:           r.RunID,
		Timestamp:       r.Timestamp.Format("2006-01-02 15:04:05 UTC"),
		Workload:        r.Config.Workload,
		Mode:            r.Config.Mode,
		Threads:         r.Config.Threads,
		DurationSeconds: r.Summary.DurationSeconds,
		TotalOps:        r.Summary.TotalOps,
		TotalErrors:     r.Summary.TotalErrors,
		OpsPerSec:       r.Summary.OpsPerSec,
		Tags:            tags,
		Operations:      ops,
		DeltaLabels:     dlJS,
		DeltaOpsData:    doJS,
		DeltaP99Data:    dpJS,
		SysTimeLabels:   slJS,
		SysCPUData:      scJS,
		SysMemData:      smJS,
	}, nil
}

// htmlTemplate is the self-contained HTML report template.
const htmlTemplate = `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width, initial-scale=1.0">
<title>mongo-ycsb Report — {{.RunID}}</title>
<script src="https://cdn.jsdelivr.net/npm/chart.js@4.4.0/dist/chart.umd.min.js"></script>
<style>
  * { box-sizing: border-box; margin: 0; padding: 0; }
  body { font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif;
         background: #f4f6f9; color: #1a1a2e; padding: 24px; }
  h1 { font-size: 1.6rem; font-weight: 700; color: #00684a; margin-bottom: 4px; }
  .subtitle { font-size: 0.85rem; color: #666; margin-bottom: 24px; }
  .grid { display: grid; grid-template-columns: repeat(auto-fit, minmax(160px, 1fr));
          gap: 16px; margin-bottom: 24px; }
  .card { background: #fff; border-radius: 10px; padding: 20px;
          box-shadow: 0 1px 4px rgba(0,0,0,.08); }
  .card .label { font-size: 0.75rem; color: #888; text-transform: uppercase;
                 letter-spacing: .05em; margin-bottom: 6px; }
  .card .value { font-size: 1.5rem; font-weight: 700; color: #00684a; }
  .card .unit  { font-size: 0.75rem; color: #aaa; margin-left: 2px; }
  .section { background: #fff; border-radius: 10px; padding: 24px;
             box-shadow: 0 1px 4px rgba(0,0,0,.08); margin-bottom: 24px; }
  .section h2 { font-size: 1rem; font-weight: 600; margin-bottom: 16px;
                color: #1a1a2e; border-bottom: 2px solid #f0f0f0; padding-bottom: 8px; }
  table { width: 100%; border-collapse: collapse; font-size: 0.875rem; }
  th { text-align: left; padding: 8px 12px; background: #f8f9fa;
       color: #555; font-weight: 600; border-bottom: 2px solid #e0e0e0; }
  td { padding: 8px 12px; border-bottom: 1px solid #f0f0f0; }
  tr:last-child td { border-bottom: none; }
  .chart-row { display: grid; grid-template-columns: 1fr 1fr; gap: 24px;
               margin-bottom: 24px; }
  @media (max-width: 768px) { .chart-row { grid-template-columns: 1fr; } }
  .tag { display: inline-block; background: #e8f5f0; color: #00684a;
         border-radius: 4px; padding: 2px 8px; font-size: 0.75rem;
         margin-right: 4px; margin-top: 4px; }
  .run-id { font-family: monospace; font-size: 0.8rem; color: #888; }
</style>
</head>
<body>

<h1>mongo-ycsb Benchmark Report</h1>
<div class="subtitle">
  <span class="run-id">{{.RunID}}</span> &nbsp;·&nbsp; {{.Timestamp}}
  {{if .Tags}}&nbsp;·&nbsp;
    {{range $t := .Operations}}{{end}}
    <span class="tag">{{.Tags}}</span>
  {{end}}
</div>

<!-- Summary cards -->
<div class="grid">
  <div class="card">
    <div class="label">Throughput</div>
    <div class="value">{{printf "%.0f" .OpsPerSec}}<span class="unit">ops/s</span></div>
  </div>
  <div class="card">
    <div class="label">Total Ops</div>
    <div class="value">{{.TotalOps}}</div>
  </div>
  <div class="card">
    <div class="label">Duration</div>
    <div class="value">{{printf "%.1f" .DurationSeconds}}<span class="unit">s</span></div>
  </div>
  <div class="card">
    <div class="label">Workload</div>
    <div class="value">{{.Workload}}</div>
  </div>
  <div class="card">
    <div class="label">Threads</div>
    <div class="value">{{.Threads}}</div>
  </div>
  <div class="card">
    <div class="label">Errors</div>
    <div class="value">{{.TotalErrors}}</div>
  </div>
</div>

<!-- Operation metrics table -->
<div class="section">
  <h2>Operation Metrics</h2>
  <table>
    <thead>
      <tr>
        <th>Operation</th><th>Count</th><th>Errors</th>
        <th>Mean (ms)</th><th>p50 (ms)</th><th>p95 (ms)</th>
        <th>p99 (ms)</th><th>p999 (ms)</th>
      </tr>
    </thead>
    <tbody>
      {{range .Operations}}
      <tr>
        <td>{{.Name}}</td>
        <td>{{.Count}}</td>
        <td>{{.Errors}}</td>
        <td>{{printf "%.2f" .MeanMs}}</td>
        <td>{{printf "%.2f" .P50Ms}}</td>
        <td>{{printf "%.2f" .P95Ms}}</td>
        <td>{{printf "%.2f" .P99Ms}}</td>
        <td>{{printf "%.2f" .P999Ms}}</td>
      </tr>
      {{end}}
    </tbody>
  </table>
</div>

<!-- Charts -->
<div class="chart-row">
  <div class="section">
    <h2>Throughput Over Time (ops/sec)</h2>
    <canvas id="opsChart"></canvas>
  </div>
  <div class="section">
    <h2>p99 Latency Over Time (ms)</h2>
    <canvas id="p99Chart"></canvas>
  </div>
</div>

<div class="chart-row">
  <div class="section">
    <h2>CPU Usage (%)</h2>
    <canvas id="cpuChart"></canvas>
  </div>
  <div class="section">
    <h2>Memory Usage (MB)</h2>
    <canvas id="memChart"></canvas>
  </div>
</div>

<script>
const deltaLabels  = {{.DeltaLabels}};
const deltaOps     = {{.DeltaOpsData}};
const deltaP99     = {{.DeltaP99Data}};
const sysLabels    = {{.SysTimeLabels}};
const sysCPU       = {{.SysCPUData}};
const sysMem       = {{.SysMemData}};

const lineOpts = (label, data, labels, color) => ({
  type: 'line',
  data: {
    labels,
    datasets: [{
      label,
      data,
      borderColor: color,
      backgroundColor: color + '22',
      borderWidth: 2,
      pointRadius: 2,
      fill: true,
      tension: 0.3,
    }]
  },
  options: {
    responsive: true,
    plugins: { legend: { display: false } },
    scales: {
      x: { ticks: { maxTicksLimit: 10 } },
      y: { beginAtZero: true }
    }
  }
});

new Chart(document.getElementById('opsChart'),
  lineOpts('ops/sec', deltaOps, deltaLabels, '#00684a'));
new Chart(document.getElementById('p99Chart'),
  lineOpts('p99 ms', deltaP99, deltaLabels, '#ff6b35'));
new Chart(document.getElementById('cpuChart'),
  lineOpts('CPU %', sysCPU, sysLabels, '#4361ee'));
new Chart(document.getElementById('memChart'),
  lineOpts('Memory MB', sysMem, sysLabels, '#7209b7'));
</script>

</body>
</html>`
