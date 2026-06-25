package explorer

import (
	"fmt"
	"sort"

	"github.com/xuri/excelize/v2"

	"github.com/sahil-doshi-mongodb/mongo-ycsb/internal/models"
)

const tsLayout = "2006-01-02 15:04:05 UTC"

// BuildExcel produces a multi-sheet .xlsx workbook comparing the given runs:
//   - Overview    : metadata + summary, one column per run
//   - Latency     : per-operation percentiles, one column per run
//   - Opcounters  : server opcounter deltas, one column per run
//   - TS_<n>_<id> : per-run time series (ops/sec, error rate, p99, CPU, memory)
func BuildExcel(runs []*models.RunResult) ([]byte, error) {
	f := excelize.NewFile()
	defer f.Close()

	overview := "Overview"
	f.SetSheetName("Sheet1", overview)

	if err := writeOverview(f, overview, runs); err != nil {
		return nil, err
	}
	if err := writeLatency(f, "Latency", runs); err != nil {
		return nil, err
	}
	if err := writeOpcounters(f, "Opcounters", runs); err != nil {
		return nil, err
	}
	for i, rr := range runs {
		name := fmt.Sprintf("TS_%d_%s", i+1, shortID(rr.RunID))
		if err := writeTimeSeries(f, name, rr); err != nil {
			return nil, err
		}
	}

	buf, err := f.WriteToBuffer()
	if err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func writeOverview(f *excelize.File, sheet string, runs []*models.RunResult) error {
	setCell(f, sheet, 1, 1, "Metric")
	for j, rr := range runs {
		setCell(f, sheet, 1, j+2, shortID(rr.RunID))
	}
	rows := []struct {
		label string
		val   func(rr *models.RunResult) interface{}
	}{
		{"Run ID", func(rr *models.RunResult) interface{} { return rr.RunID }},
		{"Timestamp (UTC)", func(rr *models.RunResult) interface{} { return rr.Timestamp.UTC().Format(tsLayout) }},
		{"Benchmark Start (UTC)", func(rr *models.RunResult) interface{} { return rr.BenchmarkStartTime.UTC().Format(tsLayout) }},
		{"Benchmark End (UTC)", func(rr *models.RunResult) interface{} { return rr.BenchmarkEndTime.UTC().Format(tsLayout) }},
		{"Run Start (UTC)", func(rr *models.RunResult) interface{} { return rr.RunStartTime.UTC().Format(tsLayout) }},
		{"Run End (UTC)", func(rr *models.RunResult) interface{} { return rr.RunEndTime.UTC().Format(tsLayout) }},
		{"Tags", func(rr *models.RunResult) interface{} { return joinStrings(rr.Tags) }},
		{"Workload", func(rr *models.RunResult) interface{} { return rr.Config.Workload }},
		{"Mode", func(rr *models.RunResult) interface{} { return rr.Config.Mode }},
		{"Threads", func(rr *models.RunResult) interface{} { return rr.Config.Threads }},
		{"Duration", func(rr *models.RunResult) interface{} { return rr.Config.Duration }},
		{"Key Distribution", func(rr *models.RunResult) interface{} { return rr.Config.KeyDistribution }},
		{"Record Count", func(rr *models.RunResult) interface{} { return rr.Config.RecordCount }},
		{"Database", func(rr *models.RunResult) interface{} { return rr.Config.Database }},
		{"Collection", func(rr *models.RunResult) interface{} { return rr.Config.Collection }},
		{"Duration (s)", func(rr *models.RunResult) interface{} { return rr.Summary.DurationSeconds }},
		{"Throughput (ops/s)", func(rr *models.RunResult) interface{} { return rr.Summary.OpsPerSec }},
		{"Total Ops", func(rr *models.RunResult) interface{} { return rr.Summary.TotalOps }},
		{"Total Errors", func(rr *models.RunResult) interface{} { return rr.Summary.TotalErrors }},
		{"MongoDB Version", func(rr *models.RunResult) interface{} { return clusterField(rr, "version") }},
		{"Host", func(rr *models.RunResult) interface{} { return clusterField(rr, "host") }},
		{"Storage Engine", func(rr *models.RunResult) interface{} { return clusterField(rr, "engine") }},
	}
	for i, row := range rows {
		setCell(f, sheet, i+2, 1, row.label)
		for j, rr := range runs {
			setCell(f, sheet, i+2, j+2, row.val(rr))
		}
	}
	return nil
}

func writeLatency(f *excelize.File, sheet string, runs []*models.RunResult) error {
	if _, err := f.NewSheet(sheet); err != nil {
		return err
	}
	setCell(f, sheet, 1, 1, "Operation")
	setCell(f, sheet, 1, 2, "Metric")
	for j, rr := range runs {
		setCell(f, sheet, 1, j+3, shortID(rr.RunID))
	}
	metrics := []struct {
		label string
		get   func(m models.OpMetric) interface{}
	}{
		{"Count", func(m models.OpMetric) interface{} { return m.Count }},
		{"Errors", func(m models.OpMetric) interface{} { return m.Errors }},
		{"Mean (ms)", func(m models.OpMetric) interface{} { return m.MeanMs }},
		{"p50 (ms)", func(m models.OpMetric) interface{} { return m.P50Ms }},
		{"p95 (ms)", func(m models.OpMetric) interface{} { return m.P95Ms }},
		{"p99 (ms)", func(m models.OpMetric) interface{} { return m.P99Ms }},
		{"p99.9 (ms)", func(m models.OpMetric) interface{} { return m.P999Ms }},
		{"p99.99 (ms)", func(m models.OpMetric) interface{} { return m.P9999Ms }},
		{"p99.999 (ms)", func(m models.OpMetric) interface{} { return m.P99999Ms }},
	}
	r := 2
	for _, op := range unionOperations(runs) {
		for _, mt := range metrics {
			setCell(f, sheet, r, 1, op)
			setCell(f, sheet, r, 2, mt.label)
			for j, rr := range runs {
				if m, ok := rr.Summary.ByOperation[op]; ok {
					setCell(f, sheet, r, j+3, mt.get(m))
				} else {
					setCell(f, sheet, r, j+3, "—")
				}
			}
			r++
		}
	}
	return nil
}

func writeOpcounters(f *excelize.File, sheet string, runs []*models.RunResult) error {
	if _, err := f.NewSheet(sheet); err != nil {
		return err
	}
	setCell(f, sheet, 1, 1, "Opcounter Delta")
	for j, rr := range runs {
		setCell(f, sheet, 1, j+2, shortID(rr.RunID))
	}
	rows := []struct {
		label string
		get   func(o models.OpcounterSnapshot) int64
	}{
		{"Insert", func(o models.OpcounterSnapshot) int64 { return o.Insert }},
		{"Query", func(o models.OpcounterSnapshot) int64 { return o.Query }},
		{"Update", func(o models.OpcounterSnapshot) int64 { return o.Update }},
		{"Delete", func(o models.OpcounterSnapshot) int64 { return o.Delete }},
		{"GetMore", func(o models.OpcounterSnapshot) int64 { return o.GetMore }},
		{"Command", func(o models.OpcounterSnapshot) int64 { return o.Command }},
	}
	for i, row := range rows {
		setCell(f, sheet, i+2, 1, row.label)
		for j, rr := range runs {
			if rr.ServerStats != nil {
				setCell(f, sheet, i+2, j+2, row.get(rr.ServerStats.Delta))
			} else {
				setCell(f, sheet, i+2, j+2, "—")
			}
		}
	}
	return nil
}

func writeTimeSeries(f *excelize.File, sheet string, rr *models.RunResult) error {
	if _, err := f.NewSheet(sheet); err != nil {
		return err
	}
	headers := []string{"Offset (s)", "Ops/sec", "Error Rate", "p99 (ms)", "CPU (%)", "Memory (MB)"}
	for c, h := range headers {
		setCell(f, sheet, 1, c+1, h)
	}
	type tsRow struct{ ops, errRate, p99, cpu, mem float64 }
	byOffset := map[float64]*tsRow{}
	var offsets []float64
	get := func(off float64) *tsRow {
		if byOffset[off] == nil {
			byOffset[off] = &tsRow{}
			offsets = append(offsets, off)
		}
		return byOffset[off]
	}
	for _, d := range rr.Delta {
		t := get(d.OffsetSeconds)
		t.ops, t.errRate, t.p99 = d.OpsPerSec, d.ErrorRate, d.P99Ms
	}
	for _, s := range rr.SystemSamples {
		t := get(s.OffsetSeconds)
		t.cpu, t.mem = s.CPUPercent, s.MemoryMB
	}
	sort.Float64s(offsets)
	r := 2
	for _, off := range offsets {
		t := byOffset[off]
		setCell(f, sheet, r, 1, off)
		setCell(f, sheet, r, 2, t.ops)
		setCell(f, sheet, r, 3, t.errRate)
		setCell(f, sheet, r, 4, t.p99)
		setCell(f, sheet, r, 5, t.cpu)
		setCell(f, sheet, r, 6, t.mem)
		r++
	}
	return nil
}

// ── helpers ───────────────────────────────────────────────────────────────────

func setCell(f *excelize.File, sheet string, row, col int, val interface{}) {
	cell, err := excelize.CoordinatesToCellName(col, row)
	if err != nil {
		return
	}
	_ = f.SetCellValue(sheet, cell, val)
}

func shortID(id string) string {
	if len(id) > 8 {
		return id[:8]
	}
	return id
}

func unionOperations(runs []*models.RunResult) []string {
	seen := map[string]bool{}
	var out []string
	for _, rr := range runs {
		for op := range rr.Summary.ByOperation {
			if !seen[op] {
				seen[op] = true
				out = append(out, op)
			}
		}
	}
	sort.Strings(out)
	return out
}

func joinStrings(s []string) string {
	out := ""
	for i, v := range s {
		if i > 0 {
			out += ", "
		}
		out += v
	}
	if out == "" {
		return "—"
	}
	return out
}

func clusterField(rr *models.RunResult, which string) string {
	if rr.ClusterInfo == nil {
		return "N/A"
	}
	switch which {
	case "version":
		return rr.ClusterInfo.MongoVersion
	case "host":
		return rr.ClusterInfo.Host
	case "engine":
		return rr.ClusterInfo.StorageEngine
	}
	return ""
}
