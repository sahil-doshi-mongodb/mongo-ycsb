package metrics

import (
	"fmt"
	"time"

	"github.com/sahil-doshi-mongodb/mongo-ycsb/internal/workloads"
)

// Ticker prints live benchmark metrics to the console at a fixed interval
// and calls RecordDelta at each tick to build time-series data.
type Ticker struct {
	recorder     *HdrRecorder
	interval     time.Duration
	printEnabled bool
	stop         chan struct{}
	done         chan struct{}
}

func NewTicker(recorder *HdrRecorder, intervalMs int, printEnabled bool) *Ticker {
	if intervalMs <= 0 {
		intervalMs = 1000
	}
	return &Ticker{
		recorder:     recorder,
		interval:     time.Duration(intervalMs) * time.Millisecond,
		printEnabled: printEnabled,
		stop:         make(chan struct{}),
		done:         make(chan struct{}),
	}
}

func (t *Ticker) Start() {
	go func() {
		defer close(t.done)
		ticker := time.NewTicker(t.interval)
		defer ticker.Stop()

		if t.printEnabled {
			printTickerHeader()
		}
		for {
			select {
			case <-t.stop:
				return
			case <-ticker.C:
				snap := t.recorder.Snapshot()
				t.recorder.RecordDelta()
				if t.printEnabled {
					printTickerLine(snap)
				}
			}
		}
	}()
}

func (t *Ticker) Stop() {
	close(t.stop)
	<-t.done
}

func printTickerHeader() {
	fmt.Printf("\n")
	fmt.Printf("  %-8s  %-10s  %-10s  %-9s  %-9s  %-10s  %-10s  %-10s\n",
		"Elapsed", "Ops/sec", "Total Ops", "p50 (ms)", "p99 (ms)", "p999 (ms)", "p9999 (ms)", "Errors")
	fmt.Printf("  %-8s  %-10s  %-10s  %-9s  %-9s  %-10s  %-10s  %-10s\n",
		"───────", "─────────", "─────────", "────────", "────────", "─────────", "──────────", "──────")
}

func printTickerLine(snap Snapshot) {
	opsPerSec := 0.0
	if snap.ElapsedSec > 0 {
		opsPerSec = float64(snap.TotalOps) / snap.ElapsedSec
	}

	p50, p99, p999, p9999 := 0.0, 0.0, 0.0, 0.0
	for k, op := range snap.ByOperation {
		if k == string(workloads.OpScanPerRecord) {
			continue
		}
		if op.P50Ms > p50 {
			p50 = op.P50Ms
		}
		if op.P99Ms > p99 {
			p99 = op.P99Ms
		}
		if op.P999Ms > p999 {
			p999 = op.P999Ms
		}
		if op.P9999Ms > p9999 {
			p9999 = op.P9999Ms
		}
	}

	fmt.Printf("  %-8s  %-10.0f  %-10d  %-9.2f  %-9.2f  %-10.2f  %-10.2f  %-10d\n",
		formatElapsed(snap.ElapsedSec),
		opsPerSec,
		snap.TotalOps,
		p50, p99, p999, p9999,
		snap.TotalErrors,
	)
}

func formatElapsed(sec float64) string {
	total := int(sec)
	m := total / 60
	s := total % 60
	if m > 0 {
		return fmt.Sprintf("%dm%02ds", m, s)
	}
	return fmt.Sprintf("%ds", s)
}
