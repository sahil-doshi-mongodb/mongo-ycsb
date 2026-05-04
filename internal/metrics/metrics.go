package metrics

import (
	"sync"
	"sync/atomic"
	"time"

	hdrhistogram "github.com/HdrHistogram/hdrhistogram-go"
	"github.com/sahil-doshi-mongodb/mongo-ycsb/internal/workloads"
)

// Recorder is the interface all metric backends must satisfy.
type Recorder interface {
	Record(op workloads.OpType, latency time.Duration, err error)
	Snapshot() Snapshot
	Reset()
}

// Snapshot is a point-in-time view of all recorded metrics.
type Snapshot struct {
	TotalOps    int64
	TotalErrors int64
	ElapsedSec  float64
	ByOperation map[string]OpSnapshot
}

// OpSnapshot holds per-operation stats with full HDR percentiles.
type OpSnapshot struct {
	Count    int64
	Errors   int64
	MeanMs   float64
	P50Ms    float64
	P95Ms    float64
	P99Ms    float64
	P999Ms   float64
	P9999Ms  float64 // p99.99 — matches original YCSB output
	P99999Ms float64 // p99.999 — matches original YCSB output
}

// DeltaPoint is a per-interval time-series sample captured during a run.
type DeltaPoint struct {
	OffsetSeconds float64
	OpsPerSec     float64
	ErrorRate     float64
	P99Ms         float64
}

// ── HdrRecorder ───────────────────────────────────────────────────────────────

// HdrRecorder records latencies using HDR histograms for accurate percentiles.
// Latencies stored in microseconds internally, reported in milliseconds.
// Safe for concurrent use.
type HdrRecorder struct {
	mu        sync.Mutex
	hists     map[string]*hdrhistogram.Histogram
	opErrs    map[string]int64
	errorMsgs map[string][]string
	totalOps  atomic.Int64
	totalErrs atomic.Int64
	startTime time.Time

	deltasMu sync.Mutex
	deltas   []DeltaPoint
	lastOps  int64
	lastTime time.Time
}

// NewHdrRecorder creates a new HDR-backed recorder ready to use.
func NewHdrRecorder() *HdrRecorder {
	now := time.Now()
	return &HdrRecorder{
		hists:     make(map[string]*hdrhistogram.Histogram),
		opErrs:    make(map[string]int64),
		errorMsgs: make(map[string][]string),
		startTime: now,
		lastTime:  now,
	}
}

// Record records a single operation latency and optional error.
func (r *HdrRecorder) Record(op workloads.OpType, latency time.Duration, err error) {
	key := string(op)
	us := latency.Microseconds()
	if us < 1 {
		us = 1
	}

	r.mu.Lock()
	h, ok := r.hists[key]
	if !ok {
		h = hdrhistogram.New(1, 60_000_000, 3)
		r.hists[key] = h
	}
	_ = h.RecordValue(us)
	if err != nil {
		r.opErrs[key]++
		// Capture up to 5 unique error messages per operation type
		if len(r.errorMsgs[key]) < 5 {
			msg := err.Error()
			r.errorMsgs[key] = append(r.errorMsgs[key], msg)
		}
	}
	r.mu.Unlock()

	r.totalOps.Add(1)
	if err != nil {
		r.totalErrs.Add(1)
	}
}

// Snapshot returns a point-in-time copy of all metrics.
func (r *HdrRecorder) Snapshot() Snapshot {
	r.mu.Lock()
	defer r.mu.Unlock()

	snap := Snapshot{
		TotalOps:    r.totalOps.Load(),
		TotalErrors: r.totalErrs.Load(),
		ElapsedSec:  time.Since(r.startTime).Seconds(),
		ByOperation: make(map[string]OpSnapshot, len(r.hists)),
	}

	for k, h := range r.hists {
		count := h.TotalCount()
		mean := 0.0
		if count > 0 {
			mean = h.Mean() / 1000.0
		}
		snap.ByOperation[k] = OpSnapshot{
			Count:    count,
			Errors:   r.opErrs[k],
			MeanMs:   mean,
			P50Ms:    float64(h.ValueAtQuantile(50)) / 1000.0,
			P95Ms:    float64(h.ValueAtQuantile(95)) / 1000.0,
			P99Ms:    float64(h.ValueAtQuantile(99)) / 1000.0,
			P999Ms:   float64(h.ValueAtQuantile(99.9)) / 1000.0,
			P9999Ms:  float64(h.ValueAtQuantile(99.99)) / 1000.0,
			P99999Ms: float64(h.ValueAtQuantile(99.999)) / 1000.0,
		}
	}

	return snap
}

// Reset clears all recorded data.
func (r *HdrRecorder) Reset() {
	r.mu.Lock()
	r.hists = make(map[string]*hdrhistogram.Histogram)
	r.opErrs = make(map[string]int64)
	r.mu.Unlock()
	r.totalOps.Store(0)
	r.totalErrs.Store(0)
}

// RecordDelta captures a per-interval snapshot for time-series storage.
func (r *HdrRecorder) RecordDelta() {
	now := time.Now()
	snap := r.Snapshot()

	r.deltasMu.Lock()
	defer r.deltasMu.Unlock()

	intervalOps := float64(snap.TotalOps - r.lastOps)
	intervalSec := now.Sub(r.lastTime).Seconds()

	opsPerSec := 0.0
	if intervalSec > 0 {
		opsPerSec = intervalOps / intervalSec
	}

	errRate := 0.0
	if snap.TotalOps > 0 {
		errRate = float64(snap.TotalErrors) / float64(snap.TotalOps) * 100
	}

	p99 := 0.0
	for k, op := range snap.ByOperation {
		// Exclude scan_per_record from aggregate p99 — it's a derived metric
		if k == string(workloads.OpScanPerRecord) {
			continue
		}
		if op.P99Ms > p99 {
			p99 = op.P99Ms
		}
	}

	r.deltas = append(r.deltas, DeltaPoint{
		OffsetSeconds: snap.ElapsedSec,
		OpsPerSec:     opsPerSec,
		ErrorRate:     errRate,
		P99Ms:         p99,
	})

	r.lastOps = snap.TotalOps
	r.lastTime = now
}

// Deltas returns a copy of all recorded delta points.
func (r *HdrRecorder) Deltas() []DeltaPoint {
	r.deltasMu.Lock()
	defer r.deltasMu.Unlock()
	out := make([]DeltaPoint, len(r.deltas))
	copy(out, r.deltas)
	return out
}

// ErrorMessages returns captured error message samples per operation type.
func (r *HdrRecorder) ErrorMessages() map[string][]string {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make(map[string][]string, len(r.errorMsgs))
	for k, v := range r.errorMsgs {
		cp := make([]string, len(v))
		copy(cp, v)
		out[k] = cp
	}
	return out
}
