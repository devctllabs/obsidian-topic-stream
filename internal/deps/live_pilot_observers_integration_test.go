//go:build integration && live

package deps

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/stretchr/testify/require"
)

type livePilotSample struct {
	Phase           string
	Kind            string
	Operation       string
	Request         int64
	ElapsedMs       float64
	Status          string
	InputBytes      int
	Inputs          int
	InputRunes      int
	Tokens          int
	Dimensions      int
	StartedUnixNs   int64
	CompletedUnixNs int64
	Results         int
	ResponseBytes   int
}

type livePilotRecorder struct {
	mu           sync.Mutex
	phase        string
	samples      []livePilotSample
	stream       *os.File
	writeError   error
	activeSearch atomic.Int64
}

func newLivePilotRecorder(t *testing.T) *livePilotRecorder {
	t.Helper()
	stream, err := os.OpenFile(filepath.Join(filepath.Dir(os.Getenv("TOPIC_STREAM_PERF_REPORT")), "raw-calls.jsonl"), os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, stream.Close()) })
	return &livePilotRecorder{phase: "setup", stream: stream}
}

func (recorder *livePilotRecorder) setPhase(phase string) {
	recorder.mu.Lock()
	defer recorder.mu.Unlock()
	recorder.phase = phase
}

func (recorder *livePilotRecorder) record(sample livePilotSample) {
	recorder.mu.Lock()
	defer recorder.mu.Unlock()
	sample.Phase = recorder.phase
	sample.CompletedUnixNs = time.Now().UnixNano()
	if sample.StartedUnixNs == 0 {
		sample.StartedUnixNs = sample.CompletedUnixNs - int64(sample.ElapsedMs*float64(time.Millisecond))
	}
	recorder.samples = append(recorder.samples, sample)
	if err := json.NewEncoder(recorder.stream).Encode(sample); recorder.writeError == nil {
		recorder.writeError = err
	}
}

func (recorder *livePilotRecorder) snapshot() []livePilotSample {
	recorder.mu.Lock()
	defer recorder.mu.Unlock()
	return append([]livePilotSample(nil), recorder.samples...)
}

func livePilotEmbeddingInputs(body []byte) ([]string, error) {
	var envelope struct {
		Input json.RawMessage `json:"input"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		return nil, err
	}
	var texts []string
	if len(envelope.Input) > 0 && envelope.Input[0] == '[' {
		err := json.Unmarshal(envelope.Input, &texts)
		return texts, err
	}
	var text string
	if err := json.Unmarshal(envelope.Input, &text); err != nil {
		return nil, err
	}
	return []string{text}, nil
}

type livePilotEmbeddingResponse struct {
	Data []struct {
		Embedding []float32 `json:"embedding"`
	} `json:"data"`
	Usage struct {
		PromptTokens int `json:"prompt_tokens"`
	} `json:"usage"`
}

func (r livePilotEmbeddingResponse) dimensions() int {
	if len(r.Data) == 0 {
		return 0
	}
	dimensions := len(r.Data[0].Embedding)
	for _, vector := range r.Data {
		if len(vector.Embedding) != dimensions {
			return 0
		}
	}
	return dimensions
}

func livePilotEmbeddingSample(texts []string, requestID int64) livePilotSample {
	sample := livePilotSample{Kind: "embedding", Operation: "document", Inputs: len(texts)}
	for _, text := range texts {
		sample.InputBytes += len(text)
		sample.InputRunes += utf8.RuneCountInString(text)
		if !bytes.HasPrefix([]byte(text), []byte("Pilot item ")) {
			sample.Operation = "query_or_probe"
		}
	}
	if sample.Operation != "document" {
		sample.Request = requestID
	}
	return sample
}

func livePilotEmbeddingProxy(t *testing.T, recorder *livePilotRecorder) string {
	t.Helper()
	upstream := os.Getenv("TOPIC_STREAM_PILOT_OLLAMA") + "/v1/embeddings"
	client := &http.Client{Timeout: time.Minute}
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		body, err := io.ReadAll(io.LimitReader(request.Body, 4<<20))
		if err != nil {
			http.Error(writer, "read benchmark request", http.StatusBadRequest)
			return
		}
		texts, err := livePilotEmbeddingInputs(body)
		if err != nil {
			http.Error(writer, "decode benchmark request", http.StatusBadRequest)
			return
		}
		forward, err := http.NewRequestWithContext(request.Context(), http.MethodPost, upstream, bytes.NewReader(body))
		if err != nil {
			http.Error(writer, "construct benchmark request", http.StatusBadGateway)
			return
		}
		forward.Header.Set("Content-Type", "application/json")
		sample := livePilotEmbeddingSample(texts, recorder.activeSearch.Load())
		started := time.Now()
		sample.StartedUnixNs = started.UnixNano()
		response, err := client.Do(forward)
		if err != nil {
			sample.ElapsedMs = float64(time.Since(started)) / float64(time.Millisecond)
			sample.Status = "transport_error"
			recorder.record(sample)
			http.Error(writer, "embedding transport failed", http.StatusBadGateway)
			return
		}
		defer func() { _ = response.Body.Close() }()
		payload, readErr := io.ReadAll(io.LimitReader(response.Body, 16<<20))
		sample.ElapsedMs = float64(time.Since(started)) / float64(time.Millisecond)
		var decoded livePilotEmbeddingResponse
		_ = json.Unmarshal(payload, &decoded)
		sample.Dimensions = decoded.dimensions()
		sample.Results = len(decoded.Data)
		sample.Tokens = decoded.Usage.PromptTokens
		sample.Status = fmt.Sprint(response.StatusCode)
		if readErr != nil {
			sample.Status = "read_error"
		}
		recorder.record(sample)
		writer.Header().Set("Content-Type", "application/json")
		writer.WriteHeader(response.StatusCode)
		_, _ = writer.Write(payload)
	}))
	t.Cleanup(server.Close)
	return server.URL + "/v1"
}

type livePilotRequestKey struct{}

func livePilotHTTPObserver(handler http.Handler, recorder *livePilotRecorder) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		started := time.Now()
		var id int64
		_, _ = fmt.Sscan(request.Header.Get("X-Pilot-Request"), &id)
		handler.ServeHTTP(writer, request.WithContext(context.WithValue(request.Context(), livePilotRequestKey{}, id)))
		recorder.record(livePilotSample{Kind: "handler", Operation: request.URL.Path, Request: id, ElapsedMs: float64(time.Since(started)) / float64(time.Millisecond), Status: "complete"})
	})
}
