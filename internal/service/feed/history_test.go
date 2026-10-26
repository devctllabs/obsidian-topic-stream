package feed_test

import (
	"encoding/base64"
	"encoding/json"
	"testing"
	"time"

	"github.com/ethernity/obsidian-topic-stream/internal/domain"
	"github.com/ethernity/obsidian-topic-stream/internal/service/feed"
	"github.com/ethernity/obsidian-topic-stream/internal/service/feed/mocks"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

func TestReadFeedContinuesOriginalHeadAfterConcurrentPublication(t *testing.T) {
	t.Parallel()
	workspaceID := uuid.New()
	now := time.Now().UTC()
	history := mocks.NewMockHistory(gomock.NewController(t))
	first := domain.FeedPublishedItem{Publication: domain.FeedPublication{Ordinal: 20}, Item: domain.Item{Title: "First"}}
	second := domain.FeedPublishedItem{Publication: domain.FeedPublication{Ordinal: 19}, Item: domain.Item{Title: "Second"}}
	gomock.InOrder(
		history.EXPECT().FeedHead(gomock.Any(), workspaceID).Return(int64(20), nil),
		history.EXPECT().ReadFeedPublications(gomock.Any(), domain.FeedPublicationQuery{WorkspaceID: workspaceID, HeadOrdinal: 20, Limit: 2}).Return([]domain.FeedPublishedItem{first, second}, nil),
		history.EXPECT().FeedHead(gomock.Any(), workspaceID).Return(int64(21), nil),
		history.EXPECT().ReadFeedPublications(gomock.Any(), domain.FeedPublicationQuery{WorkspaceID: workspaceID, HeadOrdinal: 20, BeforeOrdinal: 20, Limit: 2}).Return([]domain.FeedPublishedItem{second}, nil),
	)
	service := feed.New(feed.Config{History: history, WorkspaceID: workspaceID, Now: func() time.Time { return now }})
	page, err := service.Read(t.Context(), domain.FeedReadQuery{Limit: 1})
	require.NoError(t, err)
	require.NotNil(t, page.NextCursor)
	now = now.Add(time.Hour)
	older, err := service.Read(t.Context(), domain.FeedReadQuery{Limit: 1, Snapshot: page.Snapshot, Cursor: *page.NextCursor})
	require.NoError(t, err)
	require.Equal(t, page.Snapshot, older.Snapshot)
	require.Equal(t, page.GeneratedAt, older.GeneratedAt)
	require.Equal(t, "Second", older.Items[0].Item.Title)
	require.Nil(t, older.NextCursor)
}

func TestReadFeedRejectsLegacyAndForeignTraversalTokens(t *testing.T) {
	t.Parallel()
	workspaceID := uuid.New()
	now := time.Now().UTC()
	encode := func(value any) string {
		data, err := json.Marshal(value)
		require.NoError(t, err)
		return base64.RawURLEncoding.EncodeToString(data)
	}
	for _, test := range []struct {
		name  string
		query domain.FeedReadQuery
	}{
		{"legacy", domain.FeedReadQuery{Snapshot: encode(now)}},
		{"foreign workspace", domain.FeedReadQuery{Snapshot: encode(map[string]any{"Kind": "snapshot", "Snapshot": domain.FeedSnapshot{Version: 1, WorkspaceID: uuid.New(), CreatedAt: now}})}},
		{"future head", domain.FeedReadQuery{Snapshot: encode(map[string]any{"Kind": "snapshot", "Snapshot": domain.FeedSnapshot{Version: 1, WorkspaceID: workspaceID, HeadOrdinal: 100, CreatedAt: now}})}},
		{"cursor without snapshot", domain.FeedReadQuery{Cursor: "older"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			history := mocks.NewMockHistory(gomock.NewController(t))
			history.EXPECT().FeedHead(gomock.Any(), workspaceID).Return(int64(20), nil)
			service := feed.New(feed.Config{History: history, WorkspaceID: workspaceID, Now: func() time.Time { return now }})
			_, err := service.Read(t.Context(), test.query)
			var validation *domain.ValidationError
			require.ErrorAs(t, err, &validation)
			require.Len(t, validation.Violations, 1)
			require.Equal(t, domain.ValidationFormat, validation.Violations[0].Rule)
			require.ErrorIs(t, err, feed.ErrInvalidPageToken)
		})
	}
}

func TestReadFeedUsesSavedHeadAndNeverExpiresOldPublications(t *testing.T) {
	t.Parallel()
	workspaceID := uuid.New()
	now := time.Date(2026, 10, 21, 0, 0, 0, 0, time.UTC)
	history := mocks.NewMockHistory(gomock.NewController(t))
	history.EXPECT().FeedHead(gomock.Any(), workspaceID).Return(int64(20), nil)
	history.EXPECT().ReadFeedPublications(gomock.Any(), domain.FeedPublicationQuery{
		WorkspaceID: workspaceID, HeadOrdinal: 20, Limit: 26,
	}).Return([]domain.FeedPublishedItem{{Publication: domain.FeedPublication{
		Ordinal: 20, PublishedAt: now.AddDate(-1, 0, 0), Score: 0.9, Pipeline: "original",
	}, Item: domain.Item{ID: uuid.New(), Title: "Old publication"}}}, nil)
	service := feed.New(feed.Config{History: history, WorkspaceID: workspaceID, Now: func() time.Time { return now }})
	page, err := service.Read(t.Context(), domain.FeedReadQuery{Limit: 25})
	require.NoError(t, err)
	require.Len(t, page.Items, 1)
	require.Equal(t, "Old publication", page.Items[0].Item.Title)
	require.Equal(t, 0.9, page.Items[0].Score)
	require.NotEmpty(t, page.Snapshot)
}
