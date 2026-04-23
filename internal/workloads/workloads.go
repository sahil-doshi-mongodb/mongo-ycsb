package workloads

import (
	"fmt"
	"math/rand"

	"github.com/yourusername/mongo-ycsb/internal/config"
)

// OpType represents a single YCSB operation.
type OpType string

const (
	OpRead            OpType = "read"
	OpInsert          OpType = "insert"
	OpUpdate          OpType = "update"
	OpDelete          OpType = "delete"
	OpScan            OpType = "scan"
	OpReadModifyWrite OpType = "readModifyWrite"
	OpScanPerRecord   OpType = "scan_per_record" // normalised scan latency
)

type spec struct {
	Read, Insert, Update, Delete, Scan, ReadModifyWrite float64
}

// Standard YCSB workloads A–F.
// Workload E scan length is controlled by workload.scan.maxLength (default 100).
var standard = map[string]spec{
	"A": {Read: 50, Update: 50},
	"B": {Read: 95, Update: 5},
	"C": {Read: 100},
	"D": {Read: 95, Insert: 5},
	"E": {Scan: 95, Insert: 5},
	"F": {Read: 50, ReadModifyWrite: 50},
}

type threshold struct {
	limit float64
	op    OpType
}

// Selector picks operations randomly according to a workload's distribution.
type Selector struct {
	thresholds []threshold
}

func NewSelector(cfg *config.WorkloadConfig) (*Selector, error) {
	var s spec
	if cfg.Type == config.WorkloadCustom {
		c := cfg.Custom
		s = spec{
			Read: c.Read, Insert: c.Insert, Update: c.Update,
			Delete: c.Delete, Scan: c.Scan, ReadModifyWrite: c.ReadModifyWrite,
		}
	} else {
		found, ok := standard[string(cfg.Type)]
		if !ok {
			return nil, fmt.Errorf("unknown workload type: %s", cfg.Type)
		}
		s = found
	}

	sel := &Selector{}
	cum := 0.0
	add := func(pct float64, op OpType) {
		if pct > 0 {
			cum += pct
			sel.thresholds = append(sel.thresholds, threshold{limit: cum, op: op})
		}
	}
	add(s.Read, OpRead)
	add(s.Insert, OpInsert)
	add(s.Update, OpUpdate)
	add(s.Delete, OpDelete)
	add(s.Scan, OpScan)
	add(s.ReadModifyWrite, OpReadModifyWrite)
	return sel, nil
}

func (s *Selector) Next(rng *rand.Rand) OpType {
	r := rng.Float64() * 100
	for _, t := range s.thresholds {
		if r < t.limit {
			return t.op
		}
	}
	return s.thresholds[len(s.thresholds)-1].op
}
