package generation_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ethernity/obsidian-topic-stream/internal/platform/generation"
	"github.com/stretchr/testify/require"
)

func TestHealthCheckUsesOpenAIModelsEndpoint(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		require.Equal(t, http.MethodGet, request.Method)
		require.Equal(t, "/v1/models", request.URL.Path)
		response.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(server.Close)

	err := generation.HealthCheck(t.Context(), server.URL+"/v1", server.Client())

	require.NoError(t, err)
}

func TestHealthCheckRejectsProviderFailureWithoutReadingSensitiveBody(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		http.Error(response, "secret provider details", http.StatusUnauthorized)
	}))
	t.Cleanup(server.Close)

	err := generation.HealthCheck(t.Context(), server.URL+"/v1", server.Client())

	require.ErrorContains(t, err, "status 401")
	require.NotContains(t, err.Error(), "secret provider details")
}
