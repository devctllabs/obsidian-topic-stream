package httpapi_test

import (
	"net/http"
	"testing"
	"time"

	"github.com/ethernity/obsidian-topic-stream/internal/service/feed"
	"github.com/ethernity/obsidian-topic-stream/internal/transport/httpapi"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestFeedReadWithoutPublicationStorageDoesNotRunLegacyRanking(t *testing.T) {
	t.Parallel()
	service := feed.New(feed.Config{WorkspaceID: uuid.New(), Now: time.Now})
	router := newTestRouter(t, httpapi.Dependencies{Feed: &service})
	response := performRequest(router, http.MethodGet, "/api/v1/feed", "")
	require.Equal(t, http.StatusNotImplemented, response.Code, response.Body.String())
}

func TestRefreshFeedContract(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name   string
		body   string
		status int
	}{
		{"default", `{}`, http.StatusNotImplemented},
		{"topic", `{"topicId":"018fda3a-4e36-7d1d-8835-45165d3f3596","limit":1}`, http.StatusNotImplemented},
		{"missing body", ``, http.StatusBadRequest},
		{"null", `null`, http.StatusBadRequest},
		{"zero limit", `{"limit":0}`, http.StatusBadRequest},
		{"large limit", `{"limit":101}`, http.StatusBadRequest},
		{"bad topic", `{"topicId":"invalid"}`, http.StatusBadRequest},
		{"unknown field", `{"wait":true}`, http.StatusBadRequest},
	} {
		t.Run(test.name, func(t *testing.T) {
			router := newTestRouter(t, httpapi.Dependencies{})
			response := performRequest(router, http.MethodPost, "/api/v1/feed/refresh", test.body)
			require.Equal(t, test.status, response.Code, response.Body.String())
			require.Contains(t, response.Header().Get("Content-Type"), "application/problem+json")
		})
	}
}
