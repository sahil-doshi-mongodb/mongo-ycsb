package metrics

import (
	"fmt"
	"time"
)

// Ticker prints live benchmark metrics to the console at a fixed interval
// and calls RecordDelta on the recorder at each tick to build time-series data.
type Ticker struct {
	recorder     *HdrRecorder
	interval     time.Duration
	printEnabled bool
	stop         chan struct{}
	done         chan struct{}
}

// NewTicker creates a Ticker. printEnabled controls whether output is written
// to the console — delta recording always runs regardless.
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

// Start launches the ticker in a background goroutine.
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

// Stop signals the ticker to stop and waits for the goroutine to exit.
func (t *Ticker) Stop() {
	close(t.stop)
	<-t.done
}

func printTickerHeader() {
	fmt.Printf("\n")
	fmt.Printf("  %-8s  %-12s  %-12s  %-10s  %-10s  %-10s  %-10s\n",
		"Elapsed", "Ops/sec", "Total Ops", "p50 (ms)", "p99 (ms)", "p999 (ms)", "Errors")
	fmt.Printf("  %-8s  %-12s  %-12s  %-10s  %-10s  %-10s  %-10s\n",
		"───────", "───────────", "─────────", "────────", "────────", "─────────", "──────")
}

func printTickerLine(snap Snapshot) {
	opsPerSec := 0.0
	if snap.ElapsedSec > 0 {
		opsPerSec = float64(snap.TotalOps) / snap.ElapsedSec
	}

	// Aggregate percentiles across all operation types (take highest)
	p50, p99, p999 := 0.0, 0.0, 0.0
	for _, op := range snap.ByOperation {
		if op.P50Ms > p50 {
			p50 = op.P50Ms
		}
		if op.P99Ms > p99 {
			p99 = op.P99Ms
		}
		if op.P999Ms > p999 {
			p999 = op.P999Ms
		}
	}

	fmt.Printf("  %-8s  %-12.0f  %-12d  %-10.2f  %-10.2f  %-10.2f  %-10d\n",
		formatElapsed(snap.ElapsedSec),
		opsPerSec,
		snap.TotalOps,
		p50,
		p99,
		p999,
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
