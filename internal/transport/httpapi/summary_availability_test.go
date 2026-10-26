package httpapi_test

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/ethernity/obsidian-topic-stream/internal/domain"
	"github.com/ethernity/obsidian-topic-stream/internal/transport/httpapi"
	"github.com/ethernity/obsidian-topic-stream/internal/transport/httpapi/mocks"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

func TestItemSummaryAvailabilityUsesContentProvenance(t *testing.T) {
	for _, origin := range []domain.ContentOrigin{domain.ContentOriginArticleBody, domain.ContentOriginArticleDescription} {
		t.Run(string(origin), func(t *testing.T) {
			item := domain.Item{ID: uuid.New(), WorkspaceID: uuid.New(), SourceIDs: []uuid.UUID{uuid.New()}, Kind: domain.ItemKindArticle, Details: domain.ItemDetails{Type: domain.ItemKindArticle, Article: &domain.ArticleDetails{}}, Title: "Title", URL: "https://fixture.example/item", CanonicalURL: "https://fixture.example/item", ContentMarkdown: "Nonempty canonical text", ContentOrigin: origin, ReadingState: domain.ReadingStateUnread, Visibility: domain.ItemVisibilityVisible, DiscoveredAt: time.Now().UTC()}
			service := mocks.NewMockItems(gomock.NewController(t))
			service.EXPECT().Get(gomock.Any(), item.ID).Return(item, nil)
			router := newTestRouter(t, httpapi.Dependencies{Items: service})
			response := performRequest(router, http.MethodGet, "/api/v1/items/"+item.ID.String(), "")
			require.Equal(t, http.StatusOK, response.Code, response.Body.String())
			var result map[string]any
			require.NoError(t, json.Unmarshal(response.Body.Bytes(), &result))
			require.Equal(t, string(origin), result["contentOrigin"])
			require.Equal(t, origin == domain.ContentOriginArticleBody, result["summarySupported"])
			version, ok := result["summarySourceVersion"].(string)
			require.True(t, ok)
			require.Len(t, version, 64)
		})
	}
}
