package embeddings_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ethernity/obsidian-topic-stream/internal/platform/embeddings"
	"github.com/stretchr/testify/require"
)

func TestOpenAIClientEmbedsTextUsingConfiguredModelAndDimensions(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		require.Equal(t, "/v1/embeddings", request.URL.Path)
		require.Equal(t, http.MethodPost, request.Method)
		require.Equal(t, "application/json", request.Header.Get("Content-Type"))

		var body struct {
			Input      string `json:"input"`
			Model      string `json:"model"`
			Dimensions int    `json:"dimensions"`
		}
		require.NoError(t, json.NewDecoder(request.Body).Decode(&body))
		require.Equal(t, "A PostgreSQL query planner article", body.Input)
		require.Equal(t, "qwen3-embedding:0.6b", body.Model)
		require.Equal(t, 3, body.Dimensions)

		response.Header().Set("Content-Type", "application/json")
		_, _ = response.Write([]byte(`{"data":[{"embedding":[0.1,0.2,0.3],"index":0}],"model":"qwen3-embedding:0.6b"}`))
	}))
	t.Cleanup(server.Close)

	client, err := embeddings.NewOpenAI(server.URL+"/v1", "qwen3-embedding:0.6b", 3, server.Client())
	require.NoError(t, err)

	vector, err := client.Embed(context.Background(), "A PostgreSQL query planner article")

	require.NoError(t, err)
	require.Equal(t, []float32{0.1, 0.2, 0.3}, vector)
}

func TestOpenAIClientRejectsAnUnexpectedVectorSize(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		response.Header().Set("Content-Type", "application/json")
		_, _ = response.Write([]byte(`{"data":[{"embedding":[0.1,0.2],"index":0}]}`))
	}))
	t.Cleanup(server.Close)
	client, err := embeddings.NewOpenAI(server.URL, "model", 3, server.Client())
	require.NoError(t, err)

	_, err = client.Embed(context.Background(), "text")

	require.ErrorContains(t, err, "dimensions")
}

func TestOpenAIClientReturnsProviderErrorWithoutLeakingResponseBody(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		http.Error(response, `{"error":{"message":"secret provider detail"}}`, http.StatusUnauthorized)
	}))
	t.Cleanup(server.Close)
	client, err := embeddings.NewOpenAI(server.URL, "model", 3, server.Client())
	require.NoError(t, err)

	_, err = client.Embed(context.Background(), "text")

	require.ErrorContains(t, err, "status 401")
	require.NotContains(t, err.Error(), "secret provider detail")
}
