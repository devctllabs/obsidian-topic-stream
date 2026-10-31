package httpapi_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ethernity/obsidian-topic-stream/internal/transport/httpapi"
	"github.com/ethernity/obsidian-topic-stream/internal/transport/httpapi/mocks"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

func corsRouter(t *testing.T, origins []string) http.Handler {
	t.Helper()
	policy, err := httpapi.NewCORSMiddleware(origins)
	require.NoError(t, err)
	dependencies := completeTestDependencies(t)
	dependencies.CORS = policy
	router, err := httpapi.NewRouter(dependencies)
	require.NoError(t, err)
	return router
}

func TestCORSPreflightCoversProductAndAdminRoutesBeforeValidation(t *testing.T) {
	router := corsRouter(t, []string{"app://obsidian.md", "https://reader.example"})
	for _, path := range []string{"/api/v1/events", "/api/v1/topics", "/api/v1/admin/embedding-models"} {
		t.Run(path, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodOptions, path, nil)
			request.Header.Set("Origin", "https://reader.example")
			request.Header.Set("Access-Control-Request-Method", http.MethodPost)
			request.Header.Set("Access-Control-Request-Headers", "Content-Type,If-Match,Idempotency-Key")
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)
			require.Equal(t, http.StatusNoContent, response.Code)
			require.Equal(t, "https://reader.example", response.Header().Get("Access-Control-Allow-Origin"))
			require.Equal(t, "GET,POST,PUT,PATCH,DELETE", response.Header().Get("Access-Control-Allow-Methods"))
			require.Equal(t, "Content-Type,If-Match,Idempotency-Key", response.Header().Get("Access-Control-Allow-Headers"))
			require.Empty(t, response.Header().Get("Access-Control-Allow-Credentials"))
			require.Empty(t, response.Header().Get("Access-Control-Max-Age"))
			require.Contains(t, response.Header().Values("Vary"), "Origin")
		})
	}
}

func TestCORSPreservesOrdinaryRESTAndOnlyGrantsListedOrigins(t *testing.T) {
	topics := mocks.NewMockTopics(gomock.NewController(t))
	topics.EXPECT().List(gomock.Any()).Return(nil, nil).Times(3)
	policy, err := httpapi.NewCORSMiddleware([]string{"app://obsidian.md"})
	require.NoError(t, err)
	dependencies := completeTestDependencies(t)
	dependencies.Topics, dependencies.CORS = topics, policy
	router, err := httpapi.NewRouter(dependencies)
	require.NoError(t, err)
	for _, origin := range []string{"app://obsidian.md", "https://other.example", ""} {
		request := httptest.NewRequest(http.MethodGet, "/api/v1/topics", nil)
		request.Header.Set("Origin", origin)
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)
		require.Equal(t, http.StatusOK, response.Code)
		if origin == "app://obsidian.md" {
			require.Equal(t, origin, response.Header().Get("Access-Control-Allow-Origin"))
			require.Equal(t, "ETag", response.Header().Get("Access-Control-Expose-Headers"))
		} else {
			require.Empty(t, response.Header().Get("Access-Control-Allow-Origin"))
		}
	}
}

func TestCORSHeadersReachSSEBeforeFirstFlushAndProblemResponses(t *testing.T) {
	router := corsRouter(t, []string{"app://obsidian.md"})
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	request := httptest.NewRequest(http.MethodGet, "/api/v1/events", nil).WithContext(ctx)
	request.Header.Set("Origin", "app://obsidian.md")
	stream := &eventResponseWriter{ResponseRecorder: httptest.NewRecorder(), cancel: cancel}
	router.ServeHTTP(stream, request)
	require.Equal(t, ": connected\n\n", stream.Body.String())
	require.Equal(t, "app://obsidian.md", stream.Result().Header.Get("Access-Control-Allow-Origin"))
	require.Equal(t, 1, stream.flushes)

	request = httptest.NewRequest(http.MethodGet, "/api/v1/not-an-operation", nil)
	request.Header.Set("Origin", "app://obsidian.md")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	require.Equal(t, http.StatusNotFound, response.Code)
	require.Equal(t, "app://obsidian.md", response.Header().Get("Access-Control-Allow-Origin"))
}

func TestCORSEmptyPolicyAndDisallowedPreflightNeverGrantAccess(t *testing.T) {
	for _, origins := range [][]string{nil, {"app://obsidian.md"}} {
		router := corsRouter(t, origins)
		request := httptest.NewRequest(http.MethodOptions, "/api/v1/events", nil)
		request.Header.Set("Origin", "https://other.example")
		request.Header.Set("Access-Control-Request-Method", http.MethodGet)
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)
		require.Empty(t, response.Header().Get("Access-Control-Allow-Origin"))
		if len(origins) > 0 {
			require.Equal(t, http.StatusNoContent, response.Code)
		}
	}
}

func TestCORSPolicyRejectsWildcardAndMalformedOrigins(t *testing.T) {
	for _, origin := range []string{"*", "https://*.example.com", "null", "not-an-origin", "https://example.com/path", "https://example.com?token=value", "https://example.com#fragment"} {
		t.Run(origin, func(t *testing.T) {
			policy, err := httpapi.NewCORSMiddleware([]string{origin})
			require.Error(t, err)
			require.Nil(t, policy)
		})
	}
}
