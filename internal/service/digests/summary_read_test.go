package digests_test

import (
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

func TestGetIssueAddsSeparateReadOnlySummaryStates(t *testing.T) {
	controller := gomock.NewController(t)
	repository := mocks.NewMockRepository(controller)
	annotations := mocks.NewMockItemSummaries(controller)
	workspaceID, itemID := uuid.New(), uuid.New()
	issue := domain.DigestIssue{ID: uuid.New(), WorkspaceID: workspaceID, Status: domain.DigestIssueComplete, Sections: []domain.DigestSection{{Items: []domain.DigestItem{{Item: domain.Item{ID: itemID}, Summary: "Ordinary excerpt"}}}}}
	repository.EXPECT().GetDigestIssue(gomock.Any(), workspaceID, issue.ID).Return(issue, true, nil)
	state := domain.ItemSummary{State: domain.ItemSummaryReady, Result: &domain.AISummary{ID: uuid.New(), Text: "Separate AI text"}}
	annotations.EXPECT().ReadDigest(gomock.Any(), issue).Return(map[uuid.UUID]domain.ItemSummary{itemID: state}, nil)
	service := newDigestService(t, digests.Config{WorkspaceID: workspaceID, Repository: repository, Summaries: annotations, Topics: digestTopics(t, nil), Items: digestItems(t, nil), Ranker: &digestRankerStub{}, Jobs: newJobQueue(t), Events: events.NewForWorkspace(uuid.Nil, nil), Now: time.Now})
	read, err := service.GetIssue(t.Context(), issue.ID)
	require.NoError(t, err)
	require.Equal(t, "Ordinary excerpt", read.Sections[0].Items[0].Summary)
	require.Equal(t, &state, read.Sections[0].Items[0].AISummary)
	require.Equal(t, domain.DigestIssueComplete, read.Status)
}
