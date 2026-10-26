package httpapi_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/ethernity/obsidian-topic-stream/gen/serverhttp"
	"github.com/ethernity/obsidian-topic-stream/internal/domain"
	"github.com/ethernity/obsidian-topic-stream/internal/service/search"
	"github.com/ethernity/obsidian-topic-stream/internal/transport/httpapi"
	"github.com/ethernity/obsidian-topic-stream/internal/transport/httpapi/mocks"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

func TestSearchRouteReturnsTypedHybridResults(t *testing.T) {
	t.Parallel()

	workspaceID, sourceID := uuid.New(), uuid.New()
	now := time.Date(2026, time.September, 27, 12, 0, 0, 0, time.UTC)
	item := domain.Item{
		ID: uuid.New(), WorkspaceID: workspaceID, SourceIDs: []uuid.UUID{sourceID}, Kind: domain.ItemKindPaper,
		Details: domain.ItemDetails{Type: domain.ItemKindPaper, Paper: &domain.PaperDetails{Abstract: "A retrieval paper", Categories: []string{"cs.IR"}}},
		Title:   "Hybrid retrieval", URL: "https://arxiv.org/abs/2609.12345", CanonicalURL: "https://arxiv.org/abs/2609.12345",
		Excerpt: "A retrieval paper", PublishedAt: &now, DiscoveredAt: now,
		ReadingState: domain.ReadingStateUnread, Visibility: domain.ItemVisibilityVisible,
	}
	service := mocks.NewMockSearch(gomock.NewController(t))
	service.EXPECT().Find(gomock.Any(), search.Request{Query: "hybrid retrieval", Mode: search.ModeHybrid, Limit: 10}).Return(search.Page{Items: []search.Entry{{Item: item, Signals: map[string]float64{"rrf.dense": 1, "rrf.sparse": 1}}}}, nil)
	router := newTestRouter(t, httpapi.Dependencies{Search: service})
	request := httptest.NewRequest(http.MethodGet, "/api/v1/search?q=hybrid+retrieval&mode=hybrid&limit=10", nil)
	response := httptest.NewRecorder()

	router.ServeHTTP(response, request)

	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	var page serverhttp.SearchPage
	require.NoError(t, json.NewDecoder(response.Body).Decode(&page))
	require.Len(t, page.Items, 1)
	require.Equal(t, item.ID, page.Items[0].Item.Id)
	details, err := page.Items[0].Item.Details.AsPaperDetails()
	require.NoError(t, err)
	require.Equal(t, []string{"cs.IR"}, details.Categories)
	require.Contains(t, page.Items[0].Explanation.Signals, "rrf.dense")
	require.Contains(t, page.Items[0].Explanation.Signals, "rrf.sparse")
}

func TestSearchRouteRejectsInvalidMode(t *testing.T) {
	t.Parallel()
	service := mocks.NewMockSearch(gomock.NewController(t))
	router := newTestRouter(t, httpapi.Dependencies{Search: service})
	request := httptest.NewRequest(http.MethodGet, "/api/v1/search?q=query&mode=magic", nil)
	response := httptest.NewRecorder()

	router.ServeHTTP(response, request)

	require.Equal(t, http.StatusBadRequest, response.Code)
	require.Equal(t, "application/problem+json", response.Header().Get("Content-Type"))
}
