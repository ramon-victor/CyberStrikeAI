package storage

import (
	"errors"
	"time"

	"cyberstrike-ai/internal/config"

	"go.uber.org/zap"
)

// minSweepInterval is the floor for background cleanup, so a tiny interval_minutes cannot keep traversing the disk continuously.
const minSweepInterval = 5 * time.Minute

// sweepCheckInterval is the wake-up granularity of the background loop.
// It cannot sleep a whole interval: after changing interval_minutes from 60 to 5, the change
// would only apply once the ongoing 60-minute sleep ended. Fine-grained wake-ups plus an
// expiry check bound the delay to one granularity step.
const sweepCheckInterval = time.Minute

// Service drives background automatic cleanup.
type Service struct {
	cleaner *Cleaner
	cfg     *config.Config
	logger  *zap.Logger
}

// NewService creates the background cleanup service.
func NewService(cleaner *Cleaner, cfg *config.Config, logger *zap.Logger) *Service {
	return &Service{cleaner: cleaner, cfg: cfg, logger: logger}
}

// AutoCleanEnabled reports whether background automatic cleanup is on (off by default).
func (s *Service) AutoCleanEnabled() bool {
	if s == nil || s.cfg == nil {
		return false
	}
	return s.cfg.Storage.AutoCleanEffective()
}

// Interval returns the cleanup interval; re-read every round, so config changes need no restart.
func (s *Service) Interval() time.Duration {
	if s == nil || s.cfg == nil {
		return minSweepInterval
	}
	d := time.Duration(s.cfg.Storage.IntervalMinutesEffective()) * time.Minute
	if d < minSweepInterval {
		return minSweepInterval
	}
	return d
}

// PurgeExpired runs one automatic cleanup round; returns immediately when auto cleanup is off.
func (s *Service) PurgeExpired() {
	if s == nil || s.cleaner == nil || !s.AutoCleanEnabled() {
		return
	}
	if _, err := s.cleaner.Clean(CleanRequest{Trigger: "schedule"}); err != nil {
		if s.logger != nil {
			// A concurrency conflict is not a failure: a round is already running, just skip.
			if errors.Is(err, ErrCleanupInProgress) {
				s.logger.Debug("A storage cleanup is already running, skipping this round")
				return
			}
			s.logger.Warn("Runtime storage automatic cleanup failed", zap.Error(err))
		}
	}
}

// StartRetentionLoop periodically cleans runtime-space garbage at the configured interval.
// It wakes at sweepCheckInterval granularity and runs only when due, so an interval_minutes
// change takes effect after at most one granularity step.
func StartRetentionLoop(s *Service, logger *zap.Logger) {
	if s == nil || s.cleaner == nil {
		return
	}
	// Run one round right after startup, so a fresh configuration does not wait a full interval;
	// it runs in a goroutine so the full directory traversal does not block startup.
	go func() {
		s.PurgeExpired()
		last := time.Now()
		ticker := time.NewTicker(sweepCheckInterval)
		defer ticker.Stop()
		for range ticker.C {
			if time.Since(last) < s.Interval() {
				continue
			}
			last = time.Now()
			s.PurgeExpired()
			if logger != nil {
				logger.Debug("storage retention tick completed")
			}
		}
	}()
}
