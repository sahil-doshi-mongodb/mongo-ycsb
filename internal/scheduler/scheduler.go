package scheduler

import (
	"context"
	"fmt"
	"sync/atomic"
	"time"

	"github.com/robfig/cron/v3"
	"github.com/yourusername/mongo-ycsb/internal/config"
	"go.uber.org/zap"
)

// RunFunc is the function called on each scheduled trigger.
type RunFunc func(ctx context.Context) error

// Scheduler wraps robfig/cron and enforces the configured bound type.
// Only one bound type is active per run — set via schedule.bounds.type.
type Scheduler struct {
	cfg       *config.ScheduleConfig
	log       *zap.Logger
	runFn     RunFunc
	completed atomic.Int64
	startedAt time.Time
	stop      chan struct{}
}

// New creates a Scheduler.
func New(cfg *config.ScheduleConfig, log *zap.Logger, fn RunFunc) *Scheduler {
	return &Scheduler{
		cfg:   cfg,
		log:   log,
		runFn: fn,
		stop:  make(chan struct{}),
	}
}

// Start begins the CRON scheduler and blocks until the configured bound is
// satisfied or the context is cancelled. Returns total completed runs.
func (s *Scheduler) Start(ctx context.Context) (int64, error) {
	s.startedAt = time.Now()
	bounds := s.cfg.Bounds

	// ── Apply runFor bound via context timeout ───────────────────────────────
	if bounds.Type == "runFor" {
		runFor, err := bounds.ParseRunFor()
		if err != nil {
			return 0, fmt.Errorf("invalid schedule.bounds.runFor: %w", err)
		}
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, runFor)
		defer cancel()
		fmt.Printf("   Stops after  : %s (at ~%s)\n\n",
			bounds.RunFor,
			time.Now().Add(runFor).Format("2006-01-02 15:04:05 UTC"),
		)
	}

	// ── Apply timeWindow startAt bound — wait if before window ──────────────
	if bounds.Type == "timeWindow" && bounds.StartAt != "" {
		startAt, err := bounds.ParseStartAt()
		if err != nil {
			return 0, fmt.Errorf("invalid schedule.bounds.startAt: %w", err)
		}
		if time.Now().Before(startAt) {
			waitDur := time.Until(startAt)
			fmt.Printf("⏳ Waiting %s until window opens at %s...\n\n",
				waitDur.Round(time.Second), startAt.Format("2006-01-02 15:04:05 UTC"))
			select {
			case <-ctx.Done():
				return s.completed.Load(), ctx.Err()
			case <-time.After(waitDur):
			}
		}
	}

	c := cron.New()
	_, err := c.AddFunc(s.cfg.Cron, func() {
		switch bounds.Type {

		case "timeWindow":
			// Check stopAt on every trigger
			if bounds.StopAt != "" {
				stopAt, _ := bounds.ParseStopAt()
				if time.Now().After(stopAt) {
					s.log.Info("timeWindow stopAt reached — shutting down scheduler")
					fmt.Printf("\n🛑 schedule.bounds.stopAt reached — stopping scheduler\n")
					select {
					case <-s.stop:
					default:
						close(s.stop)
					}
					return
				}
			}

		case "maxRuns":
			// Check run count on every trigger
			if s.completed.Load() >= int64(bounds.MaxRuns) {
				s.log.Info("maxRuns reached — shutting down scheduler",
					zap.Int64("completed", s.completed.Load()))
				fmt.Printf("\n🛑 schedule.bounds.maxRuns (%d) reached — stopping scheduler\n",
					bounds.MaxRuns)
				select {
				case <-s.stop:
				default:
					close(s.stop)
				}
				return
			}
		}

		runNum := s.completed.Load() + 1
		fmt.Printf("\n▶️  Scheduled run #%d starting at %s\n",
			runNum, time.Now().Format("2006-01-02 15:04:05 UTC"))

		if err := s.runFn(ctx); err != nil {
			s.log.Error("scheduled run failed",
				zap.Int64("run", runNum), zap.Error(err))
			fmt.Printf("❌ Run #%d failed: %v\n", runNum, err)
		} else {
			s.completed.Add(1)
			fmt.Printf("✅ Run #%d complete — total completed: %d\n",
				runNum, s.completed.Load())
		}
	})
	if err != nil {
		return 0, fmt.Errorf("invalid cron expression %q: %w", s.cfg.Cron, err)
	}

	c.Start()
	defer c.Stop()

	fmt.Printf("📅 Scheduler running — press Ctrl+C to stop\n\n")

	select {
	case <-ctx.Done():
		fmt.Printf("\n🛑 Scheduler context cancelled — stopping\n")
	case <-s.stop:
		// already printed inside the cron func
	}

	total := s.completed.Load()
	fmt.Printf("📊 Scheduler finished — %d run(s) completed\n", total)
	return total, nil
}
