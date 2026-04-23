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
)

// spec defines the operation mix as percentages (must sum to 100).
type spec struct {
	Read            float64
	Insert          float64
	Update          float64
	Delete          float64
	Scan            float64
	ReadModifyWrite float64
}

// Standard YCSB workloads A–F
var standard = map[string]spec{
	"A": {Read: 50, Update: 50},
	"B": {Read: 95, Update: 5},
	"C": {Read: 100},
	"D": {Read: 95, Insert: 5},
	"E": {Scan: 95, Insert: 5},
	"F": {Read: 50, ReadModifyWrite: 50},
}

// threshold pairs a cumulative probability ceiling with its operation.
type threshold struct {
	limit float64
	op    OpType
}

// Selector picks operations randomly according to a workload's distribution.
type Selector struct {
	thresholds []threshold
}

// NewSelector builds a Selector from config.
func NewSelector(cfg *config.WorkloadConfig) (*Selector, error) {
	var s spec

	if cfg.Type == config.WorkloadCustom {
		c := cfg.Custom
		s = spec{
			Read:            c.Read,
			Insert:          c.Insert,
			Update:          c.Update,
			Delete:          c.Delete,
			Scan:            c.Scan,
			ReadModifyWrite: c.ReadModifyWrite,
		}
	} else {
		found, ok := standard[string(cfg.Type)]
		if !ok {
			return nil, fmt.Errorf("unknown workload type: %s", cfg.Type)
		}
		s = found
	}

	sel := &Selector{}
	cumulative := 0.0
	add := func(pct float64, op OpType) {
		if pct > 0 {
			cumulative += pct
			sel.thresholds = append(sel.thresholds, threshold{limit: cumulative, op: op})
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

// Next returns a random operation based on the workload distribution.
func (s *Selector) Next() OpType {
	r := rand.Float64() * 100
	for _, t := range s.thresholds {
		if r < t.limit {
			return t.op
		}
	}
	// Fallback: handles floating-point rounding at the 100% boundary
	return s.thresholds[len(s.thresholds)-1].op
}
