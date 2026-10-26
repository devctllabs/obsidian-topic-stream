package sourcepoller

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/ethernity/obsidian-topic-stream/internal/domain"
	"github.com/google/uuid"
)

// Sources lists Sources and submits their synchronization Jobs.
type Sources interface {
	// List returns the matching Source records.
	List(ctx context.Context) ([]domain.Source, error)
	// Sync submits durable synchronization work for the selected Source.
	Sync(ctx context.Context, sourceID uuid.UUID) (domain.Job, error)
}

// Service periodically enqueues synchronization for due enabled Sources.
type Service struct {
	sources  Sources
	interval time.Duration
	now      func() time.Time
}

// New binds Source polling dependencies; Run validates the polling interval before starting its loop.
func New(sources Sources, interval time.Duration, now func() time.Time) Service {
	return Service{sources: sources, interval: interval, now: now}
}

// Run periodically polls due Sources until ctx is cancelled.
func (s Service) Run(ctx context.Context) error {
	if s.interval <= 0 {
		return errors.New("source polling interval must be positive")
	}
	if err := s.PollOnce(ctx); err != nil {
		return err
	}
	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			if err := s.PollOnce(ctx); err != nil {
				return err
			}
		}
	}
}

// PollOnce submits synchronization for enabled Sources whose polling interval has elapsed.
func (s Service) PollOnce(ctx context.Context) error {
	configured, err := s.sources.List(ctx)
	if err != nil {
		return fmt.Errorf("list sources for polling: %w", err)
	}
	now := s.now()
	for _, source := range configured {
		if !due(source, now) {
			continue
		}
		if _, err := s.sources.Sync(ctx, source.ID); err != nil {
			return fmt.Errorf("synchronize due source %s: %w", source.ID, err)
		}
	}
	return nil
}

func due(source domain.Source, now time.Time) bool {
	if !source.Enabled {
		return false
	}
	if source.LastSyncedAt == nil {
		return true
	}
	interval := time.Duration(source.PollIntervalMinutes) * time.Minute
	return !source.LastSyncedAt.Add(interval).After(now)
}
