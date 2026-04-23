package metrics

import (
	"sync"
	"time"

	"github.com/shirou/gopsutil/v3/cpu"
	"github.com/shirou/gopsutil/v3/mem"
)

// SystemSample captures a single point-in-time CPU and memory reading.
type SystemSample struct {
	OffsetSeconds float64
	CPUPercent    float64
	MemoryMB      float64
}

// SystemSampler polls CPU and memory usage at a fixed interval.
// Samples are collected in the background and retrieved after the benchmark.
type SystemSampler struct {
	interval  time.Duration
	startTime time.Time
	stop      chan struct{}
	done      chan struct{}
	mu        sync.Mutex
	samples   []SystemSample
}

// NewSystemSampler creates a SystemSampler.
func NewSystemSampler(intervalMs int) *SystemSampler {
	if intervalMs <= 0 {
		intervalMs = 1000
	}
	return &SystemSampler{
		interval:  time.Duration(intervalMs) * time.Millisecond,
		startTime: time.Now(),
		stop:      make(chan struct{}),
		done:      make(chan struct{}),
	}
}

// Start launches the sampler in a background goroutine.
func (s *SystemSampler) Start() {
	go func() {
		defer close(s.done)
		ticker := time.NewTicker(s.interval)
		defer ticker.Stop()

		for {
			select {
			case <-s.stop:
				return
			case <-ticker.C:
				sample := s.collect()
				s.mu.Lock()
				s.samples = append(s.samples, sample)
				s.mu.Unlock()
			}
		}
	}()
}

// Stop signals the sampler to stop and waits for it to finish.
func (s *SystemSampler) Stop() {
	close(s.stop)
	<-s.done
}

// Samples returns a copy of all collected system samples.
func (s *SystemSampler) Samples() []SystemSample {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]SystemSample, len(s.samples))
	copy(out, s.samples)
	return out
}

func (s *SystemSampler) collect() SystemSample {
	offset := time.Since(s.startTime).Seconds()

	// CPU — non-blocking (0 interval = instantaneous reading)
	cpuPct := 0.0
	if percents, err := cpu.Percent(0, false); err == nil && len(percents) > 0 {
		cpuPct = percents[0]
	}

	// Memory
	memMB := 0.0
	if vmStat, err := mem.VirtualMemory(); err == nil {
		memMB = float64(vmStat.Used) / 1024 / 1024
	}

	return SystemSample{
		OffsetSeconds: offset,
		CPUPercent:    cpuPct,
		MemoryMB:      memMB,
	}
}
