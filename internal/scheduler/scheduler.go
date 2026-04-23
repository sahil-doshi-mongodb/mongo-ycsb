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

// Scheduler wraps robfig/cron and enforces all four bounding rules:
//  1. startAt  — don't fire before this timestamp
//  2. stopAt   — shut down after this timestamp
//  3. runFor   — shut down after this wall-clock duration from first start
//  4. maxRuns  — shut down after N completed runs (0 = unlimited)
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

// Start begins the CRON scheduler and blocks until all bounds are satisfied
// or the context is cancelled. It returns the total number of completed runs.
func (s *Scheduler) Start(ctx context.Context) (int64, error) {
	s.startedAt = time.Now()

	// Parse optional bounds — zero values mean "no limit"
	startAt, err := s.cfg.ParseStartAt()
	if err != nil {
		return 0, fmt.Errorf("invalid schedule.startAt: %w", err)
	}
	stopAt, err := s.cfg.ParseStopAt()
	if err != nil {
		return 0, fmt.Errorf("invalid schedule.stopAt: %w", err)
	}
	runFor, err := s.cfg.ParseRunFor()
	if err != nil {
		return 0, fmt.Errorf("invalid schedule.runFor: %w", err)
	}

	// If startAt is set and we're before it, wait until then
	if !startAt.IsZero() && time.Now().Before(startAt) {
		waitDur := time.Until(startAt)
		fmt.Printf("⏳ Waiting until %s to start (%.0f seconds)...\n",
			startAt.Format(time.RFC3339), waitDur.Seconds())
		select {
		case <-ctx.Done():
			return s.completed.Load(), ctx.Err()
		case <-time.After(waitDur):
		}
		s.startedAt = time.Now()
	}

	// Apply runFor from the moment we actually start
	if runFor > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, runFor)
		defer cancel()
		fmt.Printf("⏰ Scheduler will run for %s\n", runFor)
	}

	c := cron.New(cron.WithSeconds()) // with-seconds for sub-minute scheduling
	_, err = c.AddFunc(s.cfg.Cron, func() {
		// Check stopAt on every trigger
		if !stopAt.IsZero() && time.Now().After(stopAt) {
			s.log.Info("stopAt reached — shutting down scheduler")
			fmt.Printf("\n🛑 schedule.stopAt reached — stopping scheduler\n")
			close(s.stop)
			return
		}

		// Check maxRuns on every trigger
		if s.cfg.MaxRuns > 0 && s.completed.Load() >= int64(s.cfg.MaxRuns) {
			s.log.Info("maxRuns reached — shutting down scheduler",
				zap.Int64("completed", s.completed.Load()))
			fmt.Printf("\n🛑 schedule.maxRuns (%d) reached — stopping scheduler\n",
				s.cfg.MaxRuns)
			close(s.stop)
			return
		}

		runNum := s.completed.Load() + 1
		fmt.Printf("\n▶️  Scheduled run #%d starting at %s\n",
			runNum, time.Now().Format("2006-01-02 15:04:05"))

		if err := s.runFn(ctx); err != nil {
			s.log.Error("scheduled run failed", zap.Int64("run", runNum), zap.Error(err))
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

	fmt.Printf("📅 Scheduler started — cron: %q\n", s.cfg.Cron)
	if !stopAt.IsZero() {
		fmt.Printf("   Stops at  : %s\n", stopAt.Format(time.RFC3339))
	}
	if runFor > 0 {
		fmt.Printf("   Runs for  : %s\n", runFor)
	}
	if s.cfg.MaxRuns > 0 {
		fmt.Printf("   Max runs  : %d\n", s.cfg.MaxRuns)
	}
	fmt.Println()

	// Block until one of: context done, runFor exceeded, stopAt hit, maxRuns hit
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
