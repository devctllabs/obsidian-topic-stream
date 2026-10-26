//go:build integration && live

package deps

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/ethernity/obsidian-topic-stream/internal/platform/embeddings"
	"github.com/stretchr/testify/require"
)

func TestLivePilotEmbeddingObserverCountsBatchInputsSeparatelyFromRequests(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		texts, err := fixtureEmbeddingInputs(r)
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		fixtureEmbeddingResponse(w, texts, make([]float32, 1024))
	}))
	t.Cleanup(upstream.Close)
	t.Setenv("TOPIC_STREAM_PILOT_OLLAMA", upstream.URL)
	t.Setenv("TOPIC_STREAM_PERF_REPORT", filepath.Join(t.TempDir(), "report.json"))
	recorder := newLivePilotRecorder(t)
	client, err := embeddings.NewOpenAI(livePilotEmbeddingProxy(t, recorder), "fixture", 1024, http.DefaultClient)
	require.NoError(t, err)
	texts := []string{"Pilot item 1: русский", "Pilot item 2: English"}
	vectors, err := client.EmbedBatch(t.Context(), texts)
	require.NoError(t, err)
	require.Len(t, vectors, 2)
	samples := recorder.snapshot()
	require.Len(t, samples, 1, "one latency sample is one provider request")
	require.Equal(t, 2, samples[0].Inputs)
	require.Equal(t, 2, samples[0].Results)
	require.Equal(t, len(texts[0])+len(texts[1]), samples[0].InputBytes)
	require.Equal(t, "document", samples[0].Operation)
	require.Equal(t, 1024, samples[0].Dimensions)
}
