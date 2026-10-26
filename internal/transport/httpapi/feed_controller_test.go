package httpapi_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/ethernity/obsidian-topic-stream/gen/serverhttp"
	"github.com/ethernity/obsidian-topic-stream/internal/domain"
	"github.com/ethernity/obsidian-topic-stream/internal/transport/httpapi"
	"github.com/ethernity/obsidian-topic-stream/internal/transport/httpapi/mocks"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

func TestFeedReadMapsSelectionWithoutPublishing(t *testing.T) {
	t.Parallel()
	topicID := uuid.New()
	for _, test := range []struct {
		name, path string
		query      domain.FeedReadQuery
	}{
		{"default", "/api/v1/feed", domain.FeedReadQuery{Limit: 25}},
		{"scoped continuation", "/api/v1/feed?limit=1&topicId=" + topicID.String() + "&snapshot=head&cursor=older", domain.FeedReadQuery{Limit: 1, TopicID: &topicID, Snapshot: "head", Cursor: "older"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			capability := mocks.NewMockFeed(gomock.NewController(t))
			now := time.Date(2026, 10, 21, 0, 0, 0, 0, time.UTC)
			value := item(uuid.NewString(), uuid.New(), feedItem{sourceID: uuid.New(), title: "Saved", discoveredAt: now})
			capability.EXPECT().Read(gomock.Any(), test.query).Return(domain.FeedPage{
				Items: []domain.FeedEntry{{Item: value, Score: 0.8, Pipeline: "saved"}}, Snapshot: "head", GeneratedAt: now,
			}, nil)
			page := getFeedPage(t, newTestRouter(t, httpapi.Dependencies{Feed: capability}), test.path)
			require.Len(t, page.Items, 1)
			require.Equal(t, value.ID, page.Items[0].Item.Id)
			require.Equal(t, "head", page.Snapshot)
			require.Equal(t, now, page.GeneratedAt)
		})
	}
}

func TestRefreshFeedMapsDefaultsAndSelection(t *testing.T) {
	t.Parallel()
	topicID := uuid.New()
	for _, test := range []struct {
		name, body string
		command    domain.FeedRefreshCommand
	}{
		{"default", `{}`, domain.FeedRefreshCommand{Limit: 25}},
		{"topic", `{"topicId":"` + topicID.String() + `","limit":100}`, domain.FeedRefreshCommand{Limit: 100, TopicID: &topicID}},
	} {
		t.Run(test.name, func(t *testing.T) {
			capability := mocks.NewMockFeed(gomock.NewController(t))
			capability.EXPECT().Refresh(gomock.Any(), test.command).Return(domain.FeedPage{Items: []domain.FeedEntry{}, Snapshot: "latest", GeneratedAt: time.Now()}, nil)
			response := performRequest(newTestRouter(t, httpapi.Dependencies{Feed: capability}), http.MethodPost, "/api/v1/feed/refresh", test.body)
			require.Equal(t, http.StatusOK, response.Code, response.Body.String())
			var page serverhttp.FeedPage
			require.NoError(t, json.Unmarshal(response.Body.Bytes(), &page))
			require.Equal(t, "latest", page.Snapshot)
			require.Empty(t, page.Items)
		})
	}
}

func TestFeedReadRejectsInvalidProtocolInput(t *testing.T) {
	t.Parallel()
	for _, query := range []string{"limit=0", "limit=101", "topicId=invalid"} {
		t.Run(query, func(t *testing.T) {
			capability := mocks.NewMockFeed(gomock.NewController(t))
			response := performRequest(newTestRouter(t, httpapi.Dependencies{Feed: capability}), http.MethodGet, "/api/v1/feed?"+query, "")
			require.Equal(t, http.StatusBadRequest, response.Code, response.Body.String())
		})
	}
}

type feedItem struct {
	sourceID     uuid.UUID
	title        string
	discoveredAt time.Time
}

func item(id string, workspaceID uuid.UUID, input feedItem) domain.Item {
	return domain.Item{
		ID:          uuid.MustParse(id),
		WorkspaceID: workspaceID,
		SourceIDs:   []uuid.UUID{input.sourceID},
		Kind:        domain.ItemKindArticle,
		Details: domain.ItemDetails{Type: domain.ItemKindArticle, Article: &domain.ArticleDetails{
			Description: input.title + " description", Content: "# " + input.title,
		}},
		Title:           input.title,
		URL:             "https://example.com/" + url.PathEscape(input.title),
		CanonicalURL:    "https://example.com/" + url.PathEscape(input.title),
		Excerpt:         input.title + " excerpt",
		ContentMarkdown: "# " + input.title,
		DiscoveredAt:    input.discoveredAt,
		ReadingState:    domain.ReadingStateUnread,
		Visibility:      domain.ItemVisibilityVisible,
	}
}

func getFeedPage(t *testing.T, handler http.Handler, path string) serverhttp.FeedPage {
	t.Helper()
	request := httptest.NewRequest(http.MethodGet, path, nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	var page serverhttp.FeedPage
	require.NoError(t, json.NewDecoder(response.Body).Decode(&page))
	return page
}
