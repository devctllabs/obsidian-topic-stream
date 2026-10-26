//go:build integration && live

package deps

import (
	"context"
	"crypto/md5"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ethernity/obsidian-topic-stream/internal/service/search"
	"github.com/ethernity/obsidian-topic-stream/internal/testutil"
	"github.com/ethernity/obsidian-topic-stream/internal/transport/httpapi"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

type livePilotSearch struct {
	delegate httpapi.Search
	recorder *livePilotRecorder
}

// Find measures the unchanged application operation independently of its HTTP mapping.
func (observer livePilotSearch) Find(ctx context.Context, request search.Request) (search.Page, error) {
	started := time.Now()
	page, err := observer.delegate.Find(ctx, request)
	id, _ := ctx.Value(livePilotRequestKey{}).(int64)
	state := "ok"
	if err != nil {
		state = "error"
	}
	observer.recorder.record(livePilotSample{Kind: "application", Operation: "Search.Find", Request: id, ElapsedMs: float64(time.Since(started)) / float64(time.Millisecond), Status: state})
	return page, err
}

func TestLiveRetrievalPilotCapacity(t *testing.T) {
	testutil.RequireOwnedQdrant(t)
	require.NotEmpty(t, os.Getenv("TOPIC_STREAM_PILOT_OLLAMA"), "explicit live embedding endpoint is required")
	require.NotEmpty(t, os.Getenv("TOPIC_STREAM_TEST_POSTGRES_DSN"))
	require.NotEmpty(t, os.Getenv("TOPIC_STREAM_PERF_REPORT"))
	items, err := strconv.Atoi(os.Getenv("TOPIC_STREAM_PILOT_ITEMS"))
	require.NoError(t, err)
	require.Contains(t, []int{3, 500}, items)
	recorder := newLivePilotRecorder(t)
	f := newLivePilotFixture(t, items, recorder)
	report := map[string]any{"Status": "in_progress", "SyntheticSmoke": os.Getenv("TOPIC_STREAM_LIVE_SMOKE") == "true", "Dataset": f.dataset, "Model": livePilotModel(t), "Dimensions": livePilotDimensions(t),
		"Resources": "PostgreSQL 2 CPU / 4 GiB, Qdrant 2 CPU / 6 GiB; native local Ollama uncapped; detailed resource samples separate",
		"Timing":    "test-owned HTTP provider observer; direct SDK to Qdrant unless explicitly opted into gRPC observer; upstream socket and handler/application timing distinct; no TLS; SQL server profiles are not SQL client durations",
		"Scope":     "bounded 500-Item real-provider indexing and payload rebuild, or explicit 3-Item synthetic smoke; no million-provider run"}
	writeLivePilotReport(t, report, recorder)
	report["EmbeddingBatchSize"] = livePilotInt(t, "TOPIC_STREAM_PILOT_BATCH_SIZE", 8)
	report["EmbeddingConcurrency"] = livePilotInt(t, "TOPIC_STREAM_PILOT_CONCURRENCY", 1)
	report["EmbeddingMeasurementUnits"] = "one sample per HTTP request; bytes/runes/tokens sum the batch; Inputs and Results count documents/vectors; RPC latency is not individual Item latency"
	handler := livePilotSearchHandler(t, f)
	server := httptest.NewServer(livePilotHTTPObserver(handler, recorder))
	t.Cleanup(server.Close)
	var requestID atomic.Int64
	profile := capacitySQLProfile{conn: f.conn}
	profile.resetStatements(t)
	f.qdrantMetrics(t, "before-index")
	recorder.setPhase("initial_index")
	stopInitialReads := livePilotReadLoad(t, server.URL, recorder, &requestID)
	started := time.Now()
	job, err := f.maintenance.Rebuild(t.Context())
	require.NoError(t, err)
	f.startWorkers(t)
	jobInfo := f.waitRebuild(t, job.ID)
	stopInitialReads()
	jobInfo["WallMs"] = float64(time.Since(started)) / float64(time.Millisecond)
	f.assertComplete(t)
	f.qdrantMetrics(t, "after-index")
	jobInfo["ServerSQL"] = profile.statements(t)
	report["InitialIndex"] = jobInfo
	writeLivePilotReport(t, report, recorder)
	t.Logf("live initial index: items=%d elapsed_ms=%.3f", items, jobInfo["WallMs"])
	for _, topics := range []int{100, 500} {
		f.addTopics(t, topics)
		recorder.setPhase(fmt.Sprintf("search_%d", topics))
		profile.resetStatements(t)
		stop := livePilotReadLoad(t, server.URL, recorder, &requestID)
		f.runSearch(t, server.URL, topics, &requestID)
		stop()
		f.qdrantMetrics(t, fmt.Sprintf("after-search-%d", topics))
		report[fmt.Sprintf("Search%dSQL", topics)] = profile.statements(t)
		writeLivePilotReport(t, report, recorder)
	}
	recorder.setPhase("payload_rebuild_with_search")
	profile.resetStatements(t)
	started = time.Now()
	job, err = f.maintenance.Rebuild(t.Context())
	require.NoError(t, err)
	f.runSearch(t, server.URL, 500, &requestID)
	jobInfo = f.waitRebuild(t, job.ID)
	jobInfo["WallMs"] = float64(time.Since(started)) / float64(time.Millisecond)
	f.assertComplete(t)
	f.qdrantMetrics(t, "after-payload-rebuild")
	jobInfo["ServerSQL"] = profile.statements(t)
	report["PayloadOnlyRebuild"] = jobInfo
	report["Status"] = "complete"
	writeLivePilotReport(t, report, recorder)
	assertLivePilotMeasurements(t, recorder, items)
}

func livePilotSearchHandler(t *testing.T, f livePilotFixture) http.Handler {
	t.Helper()
	dependencies, err := resolveRouterCore(f.runtime.container)
	require.NoError(t, err)
	require.NoError(t, resolveRouterFeatures(f.runtime.container, &dependencies))
	dependencies.Search = livePilotSearch{delegate: dependencies.Search, recorder: f.recorder}
	handler, err := httpapi.NewRouter(dependencies)
	require.NoError(t, err)
	return handler
}

func (f livePilotFixture) runSearch(t *testing.T, base string, topics int, sequence *atomic.Int64) {
	t.Helper()
	client := livePilotHTTPClient{base: base, recorder: f.recorder, sequence: sequence, client: &http.Client{Timeout: time.Minute}}
	samples := 10
	if f.items == 3 {
		samples = 1
	}
	for _, mode := range []string{"lexical", "semantic", "hybrid"} {
		for _, filter := range []string{"all", "source", "date", "topic"} {
			values := url.Values{"q": {"PostgreSQL distributed systems"}, "mode": {mode}, "limit": {"20"}}
			switch filter {
			case "source":
				values.Set("sourceId", f.sources[0].String())
			case "date":
				values.Set("publishedAfter", time.Now().UTC().Add(-15*24*time.Hour).Format(time.RFC3339))
			case "topic":
				values.Set("topicId", livePilotTopicID())
			}
			for range samples {
				client.search(t, values, fmt.Sprintf("%d/%s/%s", topics, mode, filter))
			}
		}
	}
}

func livePilotTopicID() string {
	// Same deterministic MD5 as the SQL fixture, independent of process-random Workspace IDs.
	return uuid.UUID(md5.Sum([]byte("pilot:topic:1"))).String()
}

func livePilotReadLoad(t *testing.T, base string, recorder *livePilotRecorder, sequence *atomic.Int64) func() {
	t.Helper()
	ctx, cancel := context.WithCancel(t.Context())
	var workers sync.WaitGroup
	reader := livePilotHTTPClient{base: base, recorder: recorder, sequence: sequence, client: &http.Client{Timeout: time.Minute}}
	for _, path := range []string{"/api/v1/feed?limit=20", "/api/v1/jobs?limit=20"} {
		workers.Go(func() {
			ticker := time.NewTicker(100 * time.Millisecond)
			defer ticker.Stop()
			for {
				select {
				case <-ctx.Done():
					return
				case <-ticker.C:
				}
				if !reader.read(ctx, path) {
					return
				}
			}
		})
	}
	var once sync.Once
	stop := func() { once.Do(func() { cancel(); workers.Wait() }) }
	t.Cleanup(stop)
	return stop
}

type livePilotHTTPClient struct {
	base     string
	recorder *livePilotRecorder
	sequence *atomic.Int64
	client   *http.Client
}

func (reader livePilotHTTPClient) read(ctx context.Context, path string) bool {
	id := reader.sequence.Add(1)
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, reader.base+path, nil)
	if err != nil {
		return false
	}
	request.Header.Set("X-Pilot-Request", strconv.FormatInt(id, 10))
	started := time.Now()
	response, err := reader.client.Do(request)
	if err != nil {
		if ctx.Err() == nil {
			reader.recorder.record(livePilotSample{Kind: "read_client", Operation: path, Status: "transport_error"})
		}
		return false
	}
	_, readErr := io.Copy(io.Discard, response.Body)
	closeErr := response.Body.Close()
	if ctx.Err() != nil {
		return false
	}
	state := strconv.Itoa(response.StatusCode)
	if readErr != nil || closeErr != nil {
		state = "read_error"
	}
	reader.recorder.record(livePilotSample{Kind: "read_client", Operation: path, Request: id, ElapsedMs: float64(time.Since(started)) / float64(time.Millisecond), Status: state})
	return state == "200"
}

func (client livePilotHTTPClient) search(t *testing.T, values url.Values, label string) {
	t.Helper()
	id := client.sequence.Add(1)
	client.recorder.activeSearch.Store(id)
	request, err := http.NewRequestWithContext(t.Context(), http.MethodGet, client.base+"/api/v1/search?"+values.Encode(), nil)
	require.NoError(t, err)
	request.Header.Set("X-Pilot-Request", strconv.FormatInt(id, 10))
	started := time.Now()
	response, err := client.client.Do(request)
	require.NoError(t, err)
	body, err := io.ReadAll(response.Body)
	require.NoError(t, err)
	require.NoError(t, response.Body.Close())
	var page struct {
		Items []json.RawMessage `json:"items"`
	}
	if response.StatusCode == http.StatusOK {
		require.NoError(t, json.Unmarshal(body, &page))
	}
	client.recorder.record(livePilotSample{Kind: "search_client", Operation: label, Request: id, ElapsedMs: float64(time.Since(started)) / float64(time.Millisecond), Status: strconv.Itoa(response.StatusCode), StartedUnixNs: started.UnixNano(), Results: len(page.Items), ResponseBytes: len(body)})
	client.recorder.activeSearch.Store(0)
	if response.StatusCode != http.StatusOK {
		t.Errorf("Search %s status=%d body=%s", label, response.StatusCode, body)
	}
}

func writeLivePilotReport(t *testing.T, report map[string]any, recorder *livePilotRecorder) {
	t.Helper()
	samples := recorder.snapshot()
	groups := make(map[string][]float64)
	windows := make(map[string][]float64)
	statuses := make(map[string]int)
	inputs := make(map[string][]float64)
	for _, sample := range samples {
		key := sample.Phase + "/" + sample.Kind + "/" + sample.Operation
		groups[key] = append(groups[key], sample.ElapsedMs)
		window := fmt.Sprintf("%s/%d", key, sample.StartedUnixNs/int64(10*time.Second))
		windows[window] = append(windows[window], sample.ElapsedMs)
		statuses[key+"/"+sample.Status]++
		if sample.Kind == "embedding" {
			inputs[key+"/bytes"] = append(inputs[key+"/bytes"], float64(sample.InputBytes))
			inputs[key+"/runes"] = append(inputs[key+"/runes"], float64(sample.InputRunes))
			inputs[key+"/tokens"] = append(inputs[key+"/tokens"], float64(sample.Tokens))
		}
	}
	report["Distributions"] = livePilotDistributions(groups)
	report["TenSecondWindows"] = livePilotDistributions(windows)
	report["InputDistributions"] = livePilotDistributions(inputs)
	report["ProtocolStatuses"] = statuses
	report["RawCalls"] = "raw-calls.jsonl"
	report["SearchArrival"] = "one sequential Search client; simultaneous Feed and Jobs clients each paced at 10 requests/second; request IDs identify application/handler/client samples"
	report["Percentiles"] = "pooled per labelled operation; raw samples retained for windows and per-request contribution reconstruction"
	recorder.mu.Lock()
	writeError := recorder.writeError
	recorder.mu.Unlock()
	require.NoError(t, writeError, "raw samples must remain complete")
	encoded, err := json.MarshalIndent(report, "", "  ")
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(os.Getenv("TOPIC_STREAM_PERF_REPORT"), append(encoded, '\n'), 0o600))
}

func livePilotDistributions(groups map[string][]float64) map[string]map[string]float64 {
	result := make(map[string]map[string]float64)
	for key, values := range groups {
		result[key] = testutil.Percentiles(values)
		result[key]["samples"] = float64(len(values))
		var sum float64
		for _, value := range values {
			sum += value
		}
		result[key]["mean"] = sum / float64(len(values))
	}
	return result
}

func assertLivePilotMeasurements(t *testing.T, recorder *livePilotRecorder, items int) {
	t.Helper()
	documents := make(map[string]int)
	samples := recorder.snapshot()
	for _, sample := range samples {
		if sample.Kind == "embedding" {
			require.Equal(t, "200", sample.Status)
			require.Equal(t, livePilotDimensions(t), sample.Dimensions)
			require.Equal(t, sample.Inputs, sample.Results)
			if sample.Operation == "document" {
				documents[sample.Phase] += sample.Inputs
			}
		}
		if sample.Kind == "search_client" || sample.Kind == "read_client" {
			require.Equal(t, "200", sample.Status)
		}
	}
	require.Equal(t, items, documents["initial_index"], "initial indexing performs real embeddings for every canonical Item")
	require.Zero(t, documents["payload_rebuild_with_search"], "unchanged text reuses real vectors")
	assertPilotReadSLO(t, samples)
}

func assertPilotReadSLO(t *testing.T, samples []livePilotSample) {
	t.Helper()
	var feed, jobs []float64
	for _, sample := range samples {
		if sample.Kind != "handler" {
			continue
		}
		switch sample.Operation {
		case "/api/v1/feed":
			feed = append(feed, sample.ElapsedMs)
		case "/api/v1/jobs":
			jobs = append(jobs, sample.ElapsedMs)
		}
	}
	for name, values := range map[string][]float64{"Feed": feed, "Jobs": jobs} {
		require.NotEmpty(t, values)
		require.LessOrEqual(t, testutil.Percentiles(values)["p95"], 200.0, "%s handler read SLO under declared pilot load", name)
	}
}
