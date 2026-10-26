package digests_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/ethernity/obsidian-topic-stream/internal/domain"
	"github.com/ethernity/obsidian-topic-stream/internal/service/digests"
	"github.com/ethernity/obsidian-topic-stream/internal/service/digests/mocks"
	"github.com/ethernity/obsidian-topic-stream/internal/service/ranking"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

func TestGenerationRecoversSavedResultBeforeReadingDeletedDefinition(t *testing.T) {
	t.Parallel()
	workspaceID, digestID := uuid.New(), uuid.New()
	repository := mocks.NewMockRepository(gomock.NewController(t))
	repository.EXPECT().GetDigestIssueForJob(gomock.Any(), workspaceID, gomock.Any()).DoAndReturn(func(_ context.Context, _ uuid.UUID, jobID uuid.UUID) (domain.DigestIssue, bool, error) {
		require.NotEqual(t, uuid.Nil, jobID)
		return domain.DigestIssue{ID: uuid.New(), WorkspaceID: workspaceID, DigestID: digestID, GenerationJobID: jobID}, true, nil
	})
	queue := newJobQueue(t)
	newDigestService(t, digests.Config{WorkspaceID: workspaceID, Repository: repository, Topics: mocks.NewMockTopics(gomock.NewController(t)), Items: mocks.NewMockItems(gomock.NewController(t)), Settings: mocks.NewMockWorkspaceSettings(gomock.NewController(t)),
		Ranker: mocks.NewMockTopicRanker(gomock.NewController(t)), Jobs: queue, Events: mocks.NewMockDigestNotifier(gomock.NewController(t)), Now: time.Now})
	require.NoError(t, queue.seedDigest(t, workspaceID, digestID))
	finish := runDigestAttempt(t, queue, 2)
	require.Equal(t, domain.JobStatusSucceeded, finish.Status)
}

func TestGenerationRejectsMissingExecutionBeforeReadingInputs(t *testing.T) {
	t.Parallel()
	workspaceID, digestID := uuid.New(), uuid.New()
	queue := newJobQueue(t)
	newDigestService(t, digests.Config{WorkspaceID: workspaceID, Repository: mocks.NewMockRepository(gomock.NewController(t)), Topics: mocks.NewMockTopics(gomock.NewController(t)), Items: mocks.NewMockItems(gomock.NewController(t)), Settings: mocks.NewMockWorkspaceSettings(gomock.NewController(t)), Ranker: mocks.NewMockTopicRanker(gomock.NewController(t)), Jobs: queue, Events: mocks.NewMockDigestNotifier(gomock.NewController(t)), Now: time.Now})
	payload, err := json.Marshal(map[string]any{"digestId": digestID})
	require.NoError(t, err)
	require.ErrorIs(t, queue.handlers[digests.GenerationJobKind](t.Context(), payload), domain.ErrDigestPublicationRejected)
}

func TestGenerationTotalFailureNeverPublishesAnIssue(t *testing.T) {
	t.Parallel()
	workspaceID, topicID := uuid.New(), uuid.New()
	definition := domain.DigestDefinition{ID: uuid.New(), WorkspaceID: workspaceID, Name: "Failed", Topics: []domain.DigestTopic{{TopicID: topicID, ItemLimit: 1}}}
	repository := mocks.NewMockRepository(gomock.NewController(t))
	repository.EXPECT().GetDigest(gomock.Any(), workspaceID, definition.ID).Return(definition, true, nil)
	ranker := mocks.NewMockTopicRanker(gomock.NewController(t))
	ranker.EXPECT().RankTopic(gomock.Any(), gomock.Any()).Return(ranking.TopicResult{}, errors.New("unavailable"))
	queue := newJobQueue(t)
	newDigestService(t, digests.Config{WorkspaceID: workspaceID, Repository: repository, Topics: digestTopics(t, []domain.Topic{{ID: topicID, WorkspaceID: workspaceID, Enabled: true}}), Items: digestItems(t, nil), Ranker: ranker, Jobs: queue, Events: mocks.NewMockDigestNotifier(gomock.NewController(t)), Now: time.Now})
	require.NoError(t, queue.seedDigest(t, workspaceID, definition.ID))
	finish := runDigestAttempt(t, queue, 3)
	require.Equal(t, domain.JobStatusDead, finish.Status)
}
