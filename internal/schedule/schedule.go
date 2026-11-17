// Package schedule enqueues board and mailbox polls. It does not fetch them itself.
package schedule

import (
	"context"
	"hash/fnv"
	"log/slog"
	"time"

	"github.com/Lazzar19/ScoutAI/internal/domain"
)

// Store lists sources and inserts poll tasks.
type Store interface {
	ListSources(ctx context.Context) ([]domain.JobSource, error)
	EnqueuePoll(ctx context.Context, sourceID string) error
}

// Scheduler enqueues a poll when the last check is older than that source's interval plus jitter.
// MailboxInterval is separate from the Greenhouse hour. Zero means mail is not scheduled.
type Scheduler struct {
	Store           Store
	Interval        time.Duration
	Jitter          time.Duration
	MailboxInterval time.Duration
	MailboxJitter   time.Duration
	Wake            time.Duration
	Now             func() time.Time
	Logger          *slog.Logger
}

// Run enqueues due polls until ctx is cancelled.
func (s Scheduler) Run(ctx context.Context) error {
	if s.Logger == nil {
		s.Logger = slog.New(slog.DiscardHandler)
	}
	wake := s.Wake
	if wake <= 0 {
		wake = 30 * time.Second
	}
	if err := s.EnqueueDue(ctx); err != nil && ctx.Err() == nil {
		s.Logger.Error("enqueue polls", "err", err)
	}
	ticker := time.NewTicker(wake)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			if err := s.EnqueueDue(ctx); err != nil && ctx.Err() == nil {
				s.Logger.Error("enqueue polls", "err", err)
			}
		}
	}
}

// EnqueueDue inserts poll tasks for sources whose check interval has elapsed.
// An active poll is left alone. The queue unique key makes the insert a no-op.
func (s Scheduler) EnqueueDue(ctx context.Context) error {
	if s.Store == nil {
		return nil
	}
	now := time.Now()
	if s.Now != nil {
		now = s.Now()
	}
	sources, err := s.Store.ListSources(ctx)
	if err != nil {
		return err
	}
	for _, src := range sources {
		if !src.Enabled {
			continue
		}
		interval, jitter, ok := s.cadence(src)
		if !ok || !Due(src.LastChecked, now, interval, jitter) {
			continue
		}
		if err := s.Store.EnqueuePoll(ctx, src.ID); err != nil {
			return err
		}
	}
	return nil
}

func (s Scheduler) cadence(src domain.JobSource) (time.Duration, time.Duration, bool) {
	switch src.Type {
	case domain.SourceGreenhouse:
		return s.Interval, Jitter(src.ID, s.Jitter), true
	case domain.SourceEmail:
		if s.MailboxInterval <= 0 {
			return 0, 0, false
		}
		return s.MailboxInterval, Jitter(src.ID, s.MailboxJitter), true
	default:
		return 0, 0, false
	}
}

// Due reports whether a source should be polled. A source that has never been checked is due.
func Due(lastChecked *time.Time, now time.Time, interval, jitter time.Duration) bool {
	if interval <= 0 {
		interval = time.Hour
	}
	if lastChecked == nil {
		return true
	}
	return !now.Before(lastChecked.Add(interval + jitter))
}

// Jitter is a stable offset for a source id, from zero up to max.
func Jitter(id string, max time.Duration) time.Duration {
	if max <= 0 {
		return 0
	}
	hash := fnv.New64a()
	_, _ = hash.Write([]byte(id))
	return time.Duration(hash.Sum64() % uint64(max))
}
