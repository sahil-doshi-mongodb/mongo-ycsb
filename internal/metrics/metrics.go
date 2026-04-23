package metrics

// This is a simple placeholder for Step 3, which will replace the
// SimpleRecorder implementation with HDR histograms while keeping
// the Recorder interface identical — no other packages need to change.

import (
	"sync"
	"sync/atomic"
	"time"

	"github.com/yourusername/mongo-ycsb/internal/workloads"
)

// Recorder is the interface all metric backends must satisfy.
type Recorder interface {
	Record(op workloads.OpType, latency time.Duration, err error)
	Snapshot() Snapshot
	Reset()
}

// Snapshot is a point-in-time view of recorded metrics.
type Snapshot struct {
	TotalOps    int64
	TotalErrors int64
	ByOperation map[string]OpSnapshot
}

// OpSnapshot holds stats for one operation type.
// Step 3 will extend this with accurate HDR percentiles.
type OpSnapshot struct {
	Count      int64
	Errors     int64
	TotalLatMs int64 // used to compute mean; replaced by histogram in Step 3
}

// ── SimpleRecorder ────────────────────────────────────────────────────────────

type SimpleRecorder struct {
	mu    sync.Mutex
	ops   map[string]*opStat
	total atomic.Int64
	errs  atomic.Int64
}

type opStat struct {
	count   int64
	errors  int64
	totalMs int64
}

func NewSimpleRecorder() *SimpleRecorder {
	return &SimpleRecorder{ops: make(map[string]*opStat)}
}

func (r *SimpleRecorder) Record(op workloads.OpType, latency time.Duration, err error) {
	key := string(op)
	ms := latency.Milliseconds()

	r.mu.Lock()
	s, ok := r.ops[key]
	if !ok {
		s = &opStat{}
		r.ops[key] = s
	}
	s.count++
	s.totalMs += ms
	if err != nil {
		s.errors++
	}
	r.mu.Unlock()

	r.total.Add(1)
	if err != nil {
		r.errs.Add(1)
	}
}

func (r *SimpleRecorder) Snapshot() Snapshot {
	r.mu.Lock()
	defer r.mu.Unlock()

	snap := Snapshot{
		TotalOps:    r.total.Load(),
		TotalErrors: r.errs.Load(),
		ByOperation: make(map[string]OpSnapshot, len(r.ops)),
	}
	for k, s := range r.ops {
		snap.ByOperation[k] = OpSnapshot{
			Count:      s.count,
			Errors:     s.errors,
			TotalLatMs: s.totalMs,
		}
	}
	return snap
}

func (r *SimpleRecorder) Reset() {
	r.mu.Lock()
	r.ops = make(map[string]*opStat)
	r.mu.Unlock()
	r.total.Store(0)
	r.errs.Store(0)
}
