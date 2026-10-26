package feed_test

import (
	"context"
	"testing"
	"time"

	"github.com/ethernity/obsidian-topic-stream/internal/domain"
	"github.com/ethernity/obsidian-topic-stream/internal/service/feed"
	"github.com/ethernity/obsidian-topic-stream/internal/service/feed/mocks"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

func TestRefreshWithoutTopicsPublishesChronologicalFallbackAndReturnsSavedPage(t *testing.T) {
	controller := gomock.NewController(t)
	admission, history, topics, settings := mocks.NewMockAdmission(controller), mocks.NewMockHistory(controller), mocks.NewMockTopics(controller), mocks.NewMockWorkspaceSettings(controller)
	workspaceID, itemID := uuid.New(), uuid.New()
	now := time.Now().UTC()
	settings.EXPECT().GetWorkspace(gomock.Any()).Return(domain.WorkspaceSettings{Version: 3, Retrieval: domain.RetrievalSettings{CandidateLimit: 50, RRFK: 60}}, nil)
	admission.EXPECT().HasEnabledFeedTopics(gomock.Any(), workspaceID).Return(false, nil)
	admission.EXPECT().RecentUnpublishedFeed(gomock.Any(), domain.FeedCandidateQuery{WorkspaceID: workspaceID, At: now, CandidateLimit: 50}).Return([]domain.Item{{ID: itemID}}, nil)
	admission.EXPECT().GetActive(gomock.Any(), workspaceID).Return(domain.RetrievalIndex{}, false, nil)
	admission.EXPECT().PublishFeed(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, command domain.FeedPublishCommand) (domain.FeedPublishResult, error) {
		require.Equal(t, workspaceID, command.WorkspaceID)
		require.Len(t, command.Publications, 1)
		require.Equal(t, itemID, command.Publications[0].ItemID)
		require.Empty(t, command.Publications[0].MatchedTopicIDs)
		return domain.FeedPublishResult{HeadOrdinal: 1}, nil
	})
	history.EXPECT().FeedHead(gomock.Any(), workspaceID).Return(int64(1), nil)
	history.EXPECT().ReadFeedPublications(gomock.Any(), domain.FeedPublicationQuery{WorkspaceID: workspaceID, HeadOrdinal: 1, Limit: 26}).Return([]domain.FeedPublishedItem{{Item: domain.Item{ID: itemID}, Publication: domain.FeedPublication{Ordinal: 1}}}, nil)
	service := feed.New(feed.Config{Admission: admission, History: history, Topics: topics, Settings: settings, WorkspaceID: workspaceID, Now: func() time.Time { return now }})
	page, err := service.Refresh(t.Context(), domain.FeedRefreshCommand{})
	require.NoError(t, err)
	require.Len(t, page.Items, 1)
	require.Equal(t, itemID, page.Items[0].Item.ID)
}

func TestRefreshDeduplicatesBeforeCommonCapAndFreezesAllKnownMatches(t *testing.T) {
	for _, view := range []string{"all", "topic", "all-start-second"} {
		t.Run(view, func(t *testing.T) {
			controller := gomock.NewController(t)
			admission, history, topics, settings, ranker := mocks.NewMockAdmission(controller), mocks.NewMockHistory(controller), mocks.NewMockTopics(controller), mocks.NewMockWorkspaceSettings(controller), mocks.NewMockPersonalRanker(controller)
			workspaceID, modelID, a, b, c := uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New()
			first, second := uuid.MustParse("00000000-0000-4000-8000-000000000001"), uuid.MustParse("00000000-0000-4000-8000-000000000002")
			now := time.Now().UTC()
			snapshot := domain.WorkspaceSettings{Version: 5, Retrieval: domain.RetrievalSettings{CandidateLimit: 2, RRFK: 60}}
			settings.EXPECT().GetWorkspace(gomock.Any()).Return(snapshot, nil)
			var topicID *uuid.UUID
			expected := []uuid.UUID{a, c}
			start := 0
			if view == "all-start-second" {
				start = 1
				expected = []uuid.UUID{a, b}
			}
			if view == "topic" {
				topicID = &first
				expected = []uuid.UUID{a, b}
				topics.EXPECT().GetTopic(gomock.Any(), workspaceID, first).Return(domain.Topic{ID: first, Enabled: true}, true, nil).AnyTimes()
			} else {
				admission.EXPECT().HasEnabledFeedTopics(gomock.Any(), workspaceID).Return(true, nil)
			}
			inputs := domain.FeedCandidateInputs{CandidateLimit: 2, RRFK: 60, ModelID: modelID, GateVersion: "gate"}
			rows := []domain.FeedCandidate{{TopicID: first, ItemID: a, Position: 1, Score: 1, Inputs: inputs}, {TopicID: first, ItemID: b, Position: 2, Score: 0.8, Inputs: inputs}, {TopicID: second, ItemID: a, Position: 1, Score: 1, Inputs: inputs}, {TopicID: second, ItemID: c, Position: 2, Score: 0.7, Inputs: inputs}}
			admission.EXPECT().ReadFeedCandidates(gomock.Any(), domain.FeedCandidateQuery{WorkspaceID: workspaceID, At: now, CandidateLimit: 2, GateVersion: "gate"}).Return(rows, nil)
			admission.EXPECT().GetActive(gomock.Any(), workspaceID).Return(domain.RetrievalIndex{ModelID: modelID}, true, nil)
			ranker.EXPECT().RankFeed(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, request domain.FeedRankingRequest) ([]domain.FeedCandidate, error) {
				require.Equal(t, snapshot, request.Settings)
				require.Equal(t, modelID, request.Generation.ModelID)
				require.Len(t, request.Candidates, 2)
				require.Equal(t, expected, []uuid.UUID{request.Candidates[0].ItemID, request.Candidates[1].ItemID})
				return request.Candidates, nil
			})
			admission.EXPECT().PublishFeed(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, command domain.FeedPublishCommand) (domain.FeedPublishResult, error) {
				require.Equal(t, topicID, command.TopicID)
				require.Len(t, command.Publications, 2, "page limit does not reduce the common admission budget")
				require.Equal(t, a, command.Publications[0].ItemID)
				require.ElementsMatch(t, []uuid.UUID{first, second}, command.Publications[0].MatchedTopicIDs)
				require.Equal(t, inputs, command.Publications[0].TopicInputs[second])
				require.Equal(t, modelID, command.Publications[0].ModelID)
				return domain.FeedPublishResult{}, nil
			})
			history.EXPECT().FeedHead(gomock.Any(), workspaceID).Return(int64(2), nil)
			history.EXPECT().ReadFeedPublications(gomock.Any(), domain.FeedPublicationQuery{WorkspaceID: workspaceID, HeadOrdinal: 2, TopicID: topicID, Limit: 2}).Return([]domain.FeedPublishedItem{{Item: domain.Item{ID: a}, Publication: domain.FeedPublication{Ordinal: 2}}, {Item: domain.Item{ID: expected[1]}, Publication: domain.FeedPublication{Ordinal: 1}}}, nil)
			service := feed.New(feed.Config{Admission: admission, History: history, Topics: topics, Settings: settings, Ranker: ranker, WorkspaceID: workspaceID, Now: func() time.Time { return now }, SelectionStart: func(int) int { return start }, GateVersion: "gate"})
			page, err := service.Refresh(t.Context(), domain.FeedRefreshCommand{TopicID: topicID, Limit: 1})
			require.NoError(t, err)
			require.Len(t, page.Items, 1)
			require.NotNil(t, page.NextCursor)
		})
	}
}

func TestColdEnabledTopicsReturnSavedHistoryWithoutFallbackOrPersonalWork(t *testing.T) {
	controller := gomock.NewController(t)
	admission, history, topics, settings := mocks.NewMockAdmission(controller), mocks.NewMockHistory(controller), mocks.NewMockTopics(controller), mocks.NewMockWorkspaceSettings(controller)
	workspaceID, itemID := uuid.New(), uuid.New()
	now := time.Now().UTC()
	settings.EXPECT().GetWorkspace(gomock.Any()).Return(domain.WorkspaceSettings{Retrieval: domain.RetrievalSettings{CandidateLimit: 50, RRFK: 60}}, nil)
	admission.EXPECT().HasEnabledFeedTopics(gomock.Any(), workspaceID).Return(true, nil)
	admission.EXPECT().ReadFeedCandidates(gomock.Any(), gomock.Any()).Return([]domain.FeedCandidate{}, nil)
	history.EXPECT().FeedHead(gomock.Any(), workspaceID).Return(int64(1), nil)
	history.EXPECT().ReadFeedPublications(gomock.Any(), gomock.Any()).Return([]domain.FeedPublishedItem{{Item: domain.Item{ID: itemID}, Publication: domain.FeedPublication{Ordinal: 1}}}, nil)
	service := feed.New(feed.Config{Admission: admission, History: history, Topics: topics, Settings: settings, WorkspaceID: workspaceID, Now: func() time.Time { return now }})
	page, err := service.Refresh(t.Context(), domain.FeedRefreshCommand{})
	require.NoError(t, err)
	require.Equal(t, itemID, page.Items[0].Item.ID)
}
