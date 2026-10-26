package sourcepoller_test

import (
	"context"
	"testing"
	"time"

	"github.com/ethernity/obsidian-topic-stream/internal/domain"
	"github.com/ethernity/obsidian-topic-stream/internal/service/sourcepoller"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestPollOnceSynchronizesOnlyEnabledDueSources(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.September, 27, 12, 0, 0, 0, time.UTC)
	recent := now.Add(-10 * time.Minute)
	old := now.Add(-2 * time.Hour)
	dueNeverSynced := uuid.New()
	dueByInterval := uuid.New()
	sources := &sourceCapability{configured: []domain.Source{
		{ID: dueNeverSynced, Enabled: true, PollIntervalMinutes: 60},
		{ID: dueByInterval, Enabled: true, PollIntervalMinutes: 60, LastSyncedAt: &old},
		{ID: uuid.New(), Enabled: true, PollIntervalMinutes: 60, LastSyncedAt: &recent},
		{ID: uuid.New(), Enabled: false, PollIntervalMinutes: 60, LastSyncedAt: &old},
	}}
	service := sourcepoller.New(sources, time.Minute, func() time.Time { return now })

	err := service.PollOnce(context.Background())

	require.NoError(t, err)
	require.ElementsMatch(t, []uuid.UUID{dueNeverSynced, dueByInterval}, sources.synced)
}

type sourceCapability struct {
	configured []domain.Source
	synced     []uuid.UUID
}

// List returns records stored by the fixture.
func (s *sourceCapability) List(context.Context) ([]domain.Source, error) {
	return s.configured, nil
}

// Sync records synchronization submission without polling the Source.
func (s *sourceCapability) Sync(_ context.Context, sourceID uuid.UUID) (domain.Job, error) {
	s.synced = append(s.synced, sourceID)
	return domain.Job{}, nil
}
