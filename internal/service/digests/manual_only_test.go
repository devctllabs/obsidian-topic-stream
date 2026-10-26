package digests_test

import (
	"context"
	"testing"
	"time"

	"github.com/ethernity/obsidian-topic-stream/internal/domain"
	"github.com/ethernity/obsidian-topic-stream/internal/service/digests"
	"github.com/ethernity/obsidian-topic-stream/internal/service/digests/mocks"
	"github.com/ethernity/obsidian-topic-stream/internal/service/events"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

func TestDisablingSchedulingKeepsRecurrenceAndManualBuildAndCanReenable(t *testing.T) {
	t.Parallel()
	workspaceID, topicID := uuid.New(), uuid.New()
	now := time.Date(2026, time.October, 23, 10, 0, 0, 0, time.UTC)
	current := domain.DigestDefinition{ID: uuid.New(), WorkspaceID: workspaceID, Name: "Daily", Enabled: true, Version: 1, Timezone: "UTC",
		Topics: []domain.DigestTopic{{TopicID: topicID, ItemLimit: 5}}, Recurrence: domain.DigestRecurrence{Frequency: domain.RecurrenceDaily, Hour: 12}}
	original := current.Recurrence
	repository := mocks.NewMockRepository(gomock.NewController(t))
	repository.EXPECT().GetDigest(gomock.Any(), workspaceID, current.ID).DoAndReturn(func(context.Context, uuid.UUID, uuid.UUID) (domain.DigestDefinition, bool, error) {
		return current, true, nil
	}).Times(2)
	repository.EXPECT().UpdateDigest(gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, value domain.DigestDefinition, expected int) (bool, error) {
		require.Equal(t, current.Version, expected)
		current = value
		return true, nil
	}).Times(2)
	repository.EXPECT().AdmitDigestBuild(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, request domain.DigestBuildRequest) (domain.DigestBuildAdmission, error) {
		require.False(t, current.Enabled)
		return domain.DigestBuildAdmission{Job: request.Job, Created: true}, nil
	})
	queue := newJobQueue(t)
	service := newDigestService(t, digests.Config{WorkspaceID: workspaceID, Repository: repository, Topics: digestTopics(t, []domain.Topic{{ID: topicID, WorkspaceID: workspaceID, Enabled: true}}), Items: digestItems(t, nil), Ranker: &digestRankerStub{}, Jobs: queue, Events: events.NewForWorkspace(uuid.Nil, nil), Now: func() time.Time { return now }})
	enabled := false
	disabled, err := service.Update(t.Context(), current.ID, 1, digests.Patch{Enabled: &enabled})
	require.NoError(t, err)
	require.Nil(t, disabled.NextRunAt)
	require.Equal(t, original, disabled.Recurrence)
	_, err = service.Generate(t.Context(), current.ID, uuid.New())
	require.NoError(t, err)
	enabled = true
	reenabled, err := service.Update(t.Context(), current.ID, 2, digests.Patch{Enabled: &enabled})
	require.NoError(t, err)
	require.Equal(t, original, reenabled.Recurrence)
	require.NotNil(t, reenabled.NextRunAt)
	require.True(t, reenabled.NextRunAt.After(now))
	require.Empty(t, queue.requests)
}
