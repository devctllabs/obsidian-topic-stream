package digests_test

import (
	"context"
	"testing"
	"time"

	"github.com/ethernity/obsidian-topic-stream/internal/domain"
	"github.com/ethernity/obsidian-topic-stream/internal/service/digests"
	"github.com/ethernity/obsidian-topic-stream/internal/service/digests/mocks"
	"github.com/ethernity/obsidian-topic-stream/internal/service/events"
	"github.com/ethernity/obsidian-topic-stream/internal/service/ranking"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

func TestGenerationCapturesInputsOnceAtEachAttemptStart(t *testing.T) {
	t.Parallel()
	workspaceID, first, second := uuid.New(), uuid.New(), uuid.New()
	definition := domain.DigestDefinition{ID: uuid.New(), WorkspaceID: workspaceID, Name: "Queued", Topics: []domain.DigestTopic{{TopicID: first, ItemLimit: 1}, {TopicID: second, ItemLimit: 1}}}
	repository := mocks.NewMockRepository(gomock.NewController(t))
	settings := defaultDigestSettings()
	ranker := mocks.NewMockTopicRanker(gomock.NewController(t))
	queue := newJobQueue(t)
	newDigestService(t, digests.Config{WorkspaceID: workspaceID, Repository: repository, Settings: settings,
		Topics: digestTopics(t, []domain.Topic{{ID: first, WorkspaceID: workspaceID, Enabled: true}, {ID: second, WorkspaceID: workspaceID, Enabled: true}}),
		Items:  digestItems(t, nil), Ranker: ranker, Jobs: queue, Events: events.NewForWorkspace(uuid.Nil, nil), Now: time.Now})
	require.NoError(t, queue.seedDigest(t, workspaceID, definition.ID))
	// Edits after admission, before execution, belong to the first attempt.
	definition.Name = "At attempt start"
	settings.value.Retrieval.CandidateLimit = 77
	repository.EXPECT().GetDigest(gomock.Any(), workspaceID, definition.ID).DoAndReturn(func(context.Context, uuid.UUID, uuid.UUID) (domain.DigestDefinition, bool, error) {
		return definition, true, nil
	}).Times(2)
	for attempt, limit := range []int{77, 99} {
		ranker.EXPECT().RankTopic(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, request ranking.TopicRequest) (ranking.TopicResult, error) {
			require.Equal(t, limit, request.Limit)
			require.Equal(t, limit, request.WorkspaceSettings.Retrieval.CandidateLimit)
			// Concurrently changed settings and Definition apply only to the next attempt.
			settings.value.Retrieval.CandidateLimit = 99
			definition.Name = "Next attempt"
			definition.Topics = []domain.DigestTopic{{TopicID: first, ItemLimit: 2}, {TopicID: second, ItemLimit: 2}}
			return ranking.TopicResult{}, nil
		}).Times(2)
		title := "At attempt start"
		if attempt > 0 {
			title = "Next attempt"
		}
		repository.EXPECT().PublishDigestIssue(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, command domain.DigestPublication) (domain.DigestPublicationResult, error) {
			issue := command.Issue
			require.NotEmpty(t, command.LeaseOwner)
			require.Positive(t, command.Attempt)

			require.Contains(t, issue.Title, title)
			return domain.DigestPublicationResult{Issue: issue, Created: true}, nil
		})
		require.NoError(t, queue.run(t, 0))
		require.Equal(t, attempt+1, settings.calls)
	}
}
