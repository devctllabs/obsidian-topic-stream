package digests_test

import (
	"context"
	"strings"
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

func TestIssueHistoryCursorKeepsScopeAndPageBoundary(t *testing.T) {
	t.Parallel()
	workspaceID, digestID := uuid.New(), uuid.New()
	now := time.Now().UTC()
	first := domain.DigestIssueSummary{ID: uuid.New(), GeneratedAt: now}
	second := domain.DigestIssueSummary{ID: uuid.New(), GeneratedAt: now.Add(-time.Second)}
	repository := mocks.NewMockRepository(gomock.NewController(t))
	service := newDigestService(t, digests.Config{WorkspaceID: workspaceID, Repository: repository,
		Items: digestItems(t, nil), Topics: digestTopics(t, nil), Ranker: &digestRankerStub{},
		Jobs: newJobQueue(t), Events: events.NewForWorkspace(uuid.Nil, nil), Now: time.Now})
	repository.EXPECT().ListDigestIssues(gomock.Any(), domain.DigestIssueList{
		WorkspaceID: workspaceID, DigestID: &digestID, Limit: 2,
	}).Return([]domain.DigestIssueSummary{first, second}, nil)
	page, err := service.ListIssues(t.Context(), digests.IssueListRequest{DigestID: &digestID, Limit: 1})
	require.NoError(t, err)
	require.Equal(t, []domain.DigestIssueSummary{first}, page.Items)
	require.NotNil(t, page.NextCursor)
	repository.EXPECT().ListDigestIssues(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, query domain.DigestIssueList) ([]domain.DigestIssueSummary, error) {
		require.Equal(t, domain.DigestIssueList{WorkspaceID: workspaceID, DigestID: &digestID, Limit: 2,
			Cursor: &domain.DigestIssueCursor{GeneratedAt: first.GeneratedAt, ID: first.ID}}, query)
		return []domain.DigestIssueSummary{second}, nil
	})
	next, err := service.ListIssues(t.Context(), digests.IssueListRequest{DigestID: &digestID, Limit: 1, Cursor: *page.NextCursor})
	require.NoError(t, err)
	require.Equal(t, []domain.DigestIssueSummary{second}, next.Items)
	require.Nil(t, next.NextCursor)
	for _, request := range []digests.IssueListRequest{
		{Limit: 1, Cursor: *page.NextCursor},
		{Limit: 0}, {Limit: 101}, {Limit: 1, Cursor: "invalid"},
		{Limit: 1, Cursor: strings.Repeat("a", 2049)},
	} {
		_, err := service.ListIssues(t.Context(), request)
		require.ErrorIs(t, err, digests.ErrInvalid)
	}
}
