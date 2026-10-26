package embeddings_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ethernity/obsidian-topic-stream/internal/domain"
	"github.com/ethernity/obsidian-topic-stream/internal/platform/embeddings"
	"github.com/stretchr/testify/require"
)

func TestBatchEmbeddingPreservesTextAndRestoresResponseOrder(t *testing.T) {
	t.Parallel()
	texts := []string{"  Title\n\nРусский текст\"\u0001", "second article"}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			Input      []string `json:"input"`
			Model      string   `json:"model"`
			Dimensions int      `json:"dimensions"`
		}
		require.NoError(t, json.NewDecoder(r.Body).Decode(&request))
		require.Equal(t, texts, request.Input)
		require.Equal(t, "model", request.Model)
		require.Equal(t, 2, request.Dimensions)
		_, _ = w.Write([]byte(`{"data":[{"index":1,"embedding":[0,1]},{"index":0,"embedding":[1,0]}]}`))
	}))
	t.Cleanup(server.Close)
	client, err := embeddings.NewOpenAI(server.URL, "model", 2, server.Client())
	require.NoError(t, err)
	result, err := client.EmbedBatch(t.Context(), texts)
	require.NoError(t, err)
	require.Equal(t, [][]float32{{1, 0}, {0, 1}}, result)
}

func TestBatchEmbeddingRejectsMalformedCompleteResponse(t *testing.T) {
	t.Parallel()
	for name, body := range map[string]string{
		"missing vector":    `{"data":[{"index":0,"embedding":[1,0]}]}`,
		"extra vector":      `{"data":[{"index":0,"embedding":[1,0]},{"index":1,"embedding":[0,1]},{"index":2,"embedding":[0,1]}]}`,
		"duplicate index":   `{"data":[{"index":0,"embedding":[1,0]},{"index":0,"embedding":[0,1]}]}`,
		"negative index":    `{"data":[{"index":-1,"embedding":[1,0]},{"index":1,"embedding":[0,1]}]}`,
		"out of range":      `{"data":[{"index":2,"embedding":[1,0]},{"index":1,"embedding":[0,1]}]}`,
		"missing index":     `{"data":[{"embedding":[1,0]},{"index":1,"embedding":[0,1]}]}`,
		"wrong dimensions":  `{"data":[{"index":0,"embedding":[1]},{"index":1,"embedding":[0,1]}]}`,
		"overflow":          `{"data":[{"index":0,"embedding":[1e99,0]},{"index":1,"embedding":[0,1]}]}`,
		"NaN":               `{"data":[{"index":0,"embedding":[NaN,0]},{"index":1,"embedding":[0,1]}]}`,
		"trailing response": `{"data":[{"index":0,"embedding":[1,0]},{"index":1,"embedding":[0,1]}]} {"secret":"provider details"}`,
		"invalid JSON":      `provider secret details`,
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(body)) }))
			t.Cleanup(server.Close)
			client, err := embeddings.NewOpenAI(server.URL, "model", 2, server.Client())
			require.NoError(t, err)
			result, err := client.EmbedBatch(t.Context(), []string{"one", "two"})
			require.Error(t, err)
			require.Nil(t, result, "partial results must not escape a rejected batch")
			require.NotContains(t, err.Error(), "secret")
		})
	}
}

func TestBatchEmbeddingBoundsAndEmptyInputDoNotCallProvider(t *testing.T) {
	t.Parallel()
	var calls atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { calls.Add(1); w.WriteHeader(500) }))
	t.Cleanup(server.Close)
	client, err := embeddings.NewOpenAI(server.URL, "model", 2, server.Client())
	require.NoError(t, err)
	result, err := client.EmbedBatch(t.Context(), nil)
	require.NoError(t, err)
	require.Empty(t, result)
	for _, texts := range [][]string{make([]string, 129), {strings.Repeat("x", 4<<20)}, {strings.Repeat("\u0001", 1<<20)}} {
		_, err := client.EmbedBatch(t.Context(), texts)
		require.Error(t, err)
	}
	require.Zero(t, calls.Load())
}

func TestBatchEmbeddingRejectsOversizedSuccessfulBody(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"data":[{"index":0,"embedding":[1,0]}]}`))
		_, _ = w.Write([]byte(strings.Repeat(" ", 16<<20)))
	}))
	t.Cleanup(server.Close)
	client, err := embeddings.NewOpenAI(server.URL, "model", 2, server.Client())
	require.NoError(t, err)
	_, err = client.EmbedBatch(t.Context(), []string{"one"})
	require.ErrorContains(t, err, "size")
}

func TestBatchEmbeddingPropagatesCancellation(t *testing.T) {
	t.Parallel()
	started := make(chan struct{})
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		close(started)
		select {
		case <-r.Context().Done():
		case <-release:
		}
	}))
	t.Cleanup(server.Close)
	t.Cleanup(func() { close(release) })
	client, err := embeddings.NewOpenAI(server.URL, "model", 2, server.Client())
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(t.Context())
	t.Cleanup(cancel)
	done := make(chan error, 1)
	go func() { _, err := client.EmbedBatch(ctx, []string{"one"}); done <- err }()
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("provider call did not start")
	}
	cancel()
	select {
	case err := <-done:
		require.True(t, errors.Is(err, context.Canceled))
	case <-time.After(5 * time.Second):
		t.Fatal("request did not cancel")
	}
}

func TestBatchEmbeddingClassifiesInputErrorsWithoutRetryOrLeakingBody(t *testing.T) {
	t.Parallel()
	for _, code := range []int{400, 413, 422, 401, 429, 500, 503} {
		t.Run(http.StatusText(code), func(t *testing.T) {
			t.Parallel()
			var calls atomic.Int64
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				calls.Add(1)
				http.Error(w, "secret provider response", code)
			}))
			t.Cleanup(server.Close)
			client, err := embeddings.NewOpenAI(server.URL, "model", 2, server.Client())
			require.NoError(t, err)
			_, err = client.EmbedBatch(t.Context(), []string{"one", "two"})
			require.Error(t, err)
			require.NotContains(t, err.Error(), "secret")
			require.Equal(t, code == 400 || code == 413 || code == 422, errors.Is(err, domain.ErrEmbeddingInputRejected))
			require.EqualValues(t, 1, calls.Load())
		})
	}
}
