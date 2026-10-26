//go:build integration && performance

package vectorindex_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ethernity/obsidian-topic-stream/internal/platform/vectorindex"
	"github.com/ethernity/obsidian-topic-stream/internal/testutil"
	"github.com/google/uuid"
	"github.com/qdrant/go-client/qdrant"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type syntheticSearchSample struct {
	Case        string
	Phase       string
	Round       int
	Concurrency int
	Query       int
	StartedAt   time.Time
	ElapsedMs   float64
	Results     int
	Error       string
}

type syntheticSearchCase struct {
	name   string
	filter *qdrant.Filter
	accept func(int) bool
}

type syntheticSearchRun struct {
	scenario    syntheticSearchCase
	phase       string
	round       int
	concurrency int
	count       int
}

func TestSyntheticQdrantSearchCapacity(t *testing.T) {
	testutil.RequireOwnedQdrant(t)
	count := 100000
	if text := os.Getenv("TOPIC_STREAM_SYNTHETIC_POINTS"); text != "" {
		var err error
		count, err = strconv.Atoi(text)
		require.NoError(t, err)
	}
	require.Contains(t, []int{1024, 100000, 1000000}, count)
	require.Contains(t, []string{"", "balanced", "dominant"}, os.Getenv("TOPIC_STREAM_SYNTHETIC_DISTRIBUTION"))
	path := os.Getenv("TOPIC_STREAM_PERF_REPORT")
	require.NotEmpty(t, path, "use the tracked capped local runner")
	fixture := newSyntheticCapacityFixture(t, count, path)
	fixture.load(t)
	fixture.reads(t)
	fixture.report["Recall"] = syntheticRecall(t, fixture.client, fixture.collection, fixture.cases)
	fixture.report["FinalCollection"] = waitSyntheticIndex(t, fixture.client, fixture.collection, count)
	fixture.report["ResourcesAfterReads"] = syntheticResources(t, fixture.collection)
	fixture.report["Status"] = "complete"
	fixture.save(t, "complete")
}

type syntheticCapacityFixture struct {
	client     *qdrant.Client
	collection string
	count      int
	path       string
	cases      []syntheticSearchCase
	report     map[string]any
}

func newSyntheticCapacityFixture(t *testing.T, count int, path string) *syntheticCapacityFixture {
	t.Helper()
	client, err := vectorindex.NewClient(testutil.QdrantURL(t))
	require.NoError(t, err)
	collection := "synthetic_capacity_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	f := &syntheticCapacityFixture{client: client, collection: collection, count: count, path: path, cases: syntheticCapacityCases()}
	t.Cleanup(func() { require.NoError(t, f.client.Close()) })
	policy := vectorindex.SearchPolicy{FullScanThresholdKB: 100000, HNSWEf: 256, Acorn: true, MaxSelectivity: .4}
	index, err := vectorindex.NewWithSearchPolicy(client, collection, 1024, policy)
	require.NoError(t, err)
	require.NoError(t, index.Ensure(t.Context()))
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		require.NoError(t, f.client.DeleteCollection(ctx, collection))
	})
	require.NoError(t, client.UpdateCollection(t.Context(), &qdrant.UpdateCollection{CollectionName: collection,
		OptimizersConfig: &qdrant.OptimizersConfigDiff{DefaultSegmentNumber: qdrant.PtrOf(uint64(2)), IndexingThreshold: qdrant.PtrOf(uint64(1024))}}))
	f.report = map[string]any{
		"Status": "in_progress", "Points": count, "Dimensions": 1024, "DenseBytes": int64(count) * 4096, "Policy": policy,
		"Dataset":      "64 normalized Gaussian clusters; independent document/query noise streams; deterministic UUID identities and payloads",
		"Distribution": os.Getenv("TOPIC_STREAM_SYNTHETIC_DISTRIBUTION"), "BatchSize": 256, "Indexing": "online, strong wait=true; no quantization; 2 target segments; index threshold 1024 KiB",
		"Scope":        "raw dense Qdrant policy/latency/recall; full payload and dense vectors returned; no model calls, sparse corpus, PostgreSQL ACK or API SLO claim",
		"Cold":         "process restart only; host/VM filesystem caches are not flushed; post_restart samples are not steady-state p99",
		"RecallTarget": .99, "AllowedFallback": .95, "Percentiles": "nearest rank; warm rounds exclude references, startup and warmup",
	}
	t.Cleanup(func() {
		if t.Failed() {
			f.report["Status"] = "failed"
			f.save(t, "failed")
		}
	})
	return f
}

func (f *syntheticCapacityFixture) save(t *testing.T, stage string) {
	t.Helper()
	f.report["Stage"] = stage
	writeSyntheticReport(t, f.path, f.report)
}

func (f *syntheticCapacityFixture) load(t *testing.T) {
	t.Helper()
	f.save(t, "uploading")
	started := time.Now()
	f.report["LogicalPayloadJSONBytes"] = uploadSyntheticPoints(t, f.client, f.collection, f.count)
	f.report["UploadMs"] = float64(time.Since(started)) / float64(time.Millisecond)
	f.report["Ready"] = waitSyntheticIndex(t, f.client, f.collection, f.count)
	f.report["UploadAndIndexMs"] = float64(time.Since(started)) / float64(time.Millisecond)
	exact, err := f.client.Count(t.Context(), &qdrant.CountPoints{CollectionName: f.collection, Exact: qdrant.PtrOf(true)})
	require.NoError(t, err)
	require.EqualValues(t, f.count, exact)
	scopes := map[string]any{}
	for _, scenario := range f.cases {
		expected := 0
		for n := range f.count {
			if scenario.accept(n) {
				expected++
			}
		}
		actual, err := f.client.Count(t.Context(), &qdrant.CountPoints{CollectionName: f.collection, Filter: scenario.filter, Exact: qdrant.PtrOf(true)})
		require.NoError(t, err)
		require.EqualValues(t, expected, actual, scenario.name)
		scopes[scenario.name] = map[string]any{"Count": actual, "Fraction": float64(actual) / float64(f.count)}
	}
	f.report["ExactCount"], f.report["FilterSelectivity"] = exact, scopes
	f.report["ResourcesBeforeReads"] = syntheticResources(t, f.collection)
	f.save(t, "ready")
}

func (f *syntheticCapacityFixture) reads(t *testing.T) {
	t.Helper()
	container := os.Getenv("TOPIC_STREAM_SYNTHETIC_QDRANT_CONTAINER")
	require.True(t, strings.HasPrefix(container, "topic-stream-qdrant-search-perf-"), "runner-owned Qdrant is required")
	output, err := exec.CommandContext(t.Context(), "docker", "restart", container).CombinedOutput()
	require.NoError(t, err, string(output))
	httpURL, grpcURL := syntheticRestartEndpoint(t, container, "6333/tcp"), syntheticRestartEndpoint(t, container, "6334/tcp")
	waitSyntheticHTTP(t, httpURL)
	t.Setenv("TOPIC_STREAM_SYNTHETIC_QDRANT_HTTP", httpURL)
	replacement, err := vectorindex.NewClient(grpcURL)
	require.NoError(t, err)
	require.NoError(t, f.client.Close())
	f.client = replacement
	f.report["RestartEndpoints"] = map[string]string{"HTTP": httpURL, "GRPC": grpcURL}
	var samples []syntheticSearchSample
	queryCount := 100
	if f.count == 1024 {
		queryCount = 8
	}
	for _, scenario := range f.cases {
		samples = append(samples, runSyntheticQueries(t, f.client, f.collection, syntheticSearchRun{scenario: scenario, phase: "post_restart", concurrency: 1, count: 1})...)
		samples = append(samples, runSyntheticQueries(t, f.client, f.collection, syntheticSearchRun{scenario: scenario, phase: "warmup", concurrency: 1, count: 8})...)
		for _, concurrency := range []int{1, 4} {
			for round := range 2 {
				samples = append(samples, runSyntheticQueries(t, f.client, f.collection, syntheticSearchRun{scenario: scenario, phase: "warm", round: round, concurrency: concurrency, count: queryCount})...)
			}
		}
		f.report["Samples"] = samples
		f.save(t, "reading_"+scenario.name)
	}
	groups := map[string][]float64{}
	for _, sample := range samples {
		require.Empty(t, sample.Error, "%+v", sample)
		if sample.Phase == "warm" {
			key := fmt.Sprintf("%s/c%d/r%d", sample.Case, sample.Concurrency, sample.Round)
			groups[key] = append(groups[key], sample.ElapsedMs)
		}
	}
	distributions := map[string]map[string]float64{}
	for name, values := range groups {
		distributions[name] = testutil.Percentiles(values)
		distributions[name]["n"] = float64(len(values))
	}
	f.report["LatencyMs"] = distributions
}

func uploadSyntheticPoints(t *testing.T, client *qdrant.Client, collection string, count int) int {
	t.Helper()
	payloadBytes := 0
	for offset := 0; offset < count; offset += 256 {
		var points []*qdrant.PointStruct
		for n := offset; n < min(offset+256, count); n++ {
			payload := syntheticCapacityPayload(n)
			encoded, err := json.Marshal(payload)
			require.NoError(t, err)
			payloadBytes += len(encoded)
			points = append(points, &qdrant.PointStruct{Id: qdrant.NewIDUUID(syntheticCapacityID("item", n)), Payload: qdrant.NewValueMap(payload),
				Vectors: qdrant.NewVectorsMap(map[string]*qdrant.Vector{vectorindex.DenseVectorName: qdrant.NewVector(syntheticCapacityVector(uint64(n), false)...)})})
		}
		_, err := client.Upsert(t.Context(), &qdrant.UpsertPoints{CollectionName: collection, Points: points, Wait: qdrant.PtrOf(true), Ordering: &qdrant.WriteOrdering{Type: qdrant.WriteOrderingType_Strong}})
		require.NoError(t, err)
	}
	return payloadBytes
}

func syntheticCapacityID(kind string, n int) string {
	return uuid.NewSHA1(uuid.NameSpaceOID, []byte(fmt.Sprintf("synthetic:%s:%d", kind, n))).String()
}

func syntheticCapacityPayload(n int) map[string]any {
	at := time.Date(2026, time.September, 1, 0, 0, 0, 0, time.UTC).AddDate(0, 0, n%60).Format(time.RFC3339)
	return map[string]any{
		"itemId": syntheticCapacityID("item", n), "workspaceId": syntheticCapacityID("workspace", syntheticWorkspace(n)),
		"sourceIds": []any{syntheticCapacityID("source", n/256%10)}, "kind": []string{"article", "paper", "video", "repository"}[n/2560%4],
		"publishedAt": at, "discoveredAt": at, "contentAt": at,
		"published": n%5 != 0, "hidden": n%11 == 0, "projectionVersion": 1,
		"projectionRevision": syntheticCapacityID("revision", n), "textFingerprint": strings.Repeat("a", 64),
		"topicCorrections": []any{}, "fixtureOrdinal": n,
	}
}

func syntheticCapacityCases() []syntheticSearchCase {
	workspace := qdrant.NewMatch("workspaceId", syntheticCapacityID("workspace", 0))
	source := qdrant.NewMatch("sourceIds", syntheticCapacityID("source", 0))
	date := time.Date(2026, time.September, 30, 0, 0, 0, 0, time.UTC)
	return []syntheticSearchCase{
		{name: "all", accept: func(int) bool { return true }},
		{name: "workspace", filter: &qdrant.Filter{Must: []*qdrant.Condition{workspace}}, accept: func(n int) bool { return syntheticWorkspace(n) == 0 }},
		{name: "workspace_source", filter: &qdrant.Filter{Must: []*qdrant.Condition{workspace, source}}, accept: func(n int) bool { return syntheticWorkspace(n) == 0 && n/256%10 == 0 }},
		{name: "combined", filter: &qdrant.Filter{Must: []*qdrant.Condition{workspace, source, qdrant.NewMatch("kind", "article"), qdrant.NewMatchBool("published", true), qdrant.NewMatchBool("hidden", false), qdrant.NewDatetimeRange("publishedAt", &qdrant.DatetimeRange{Gte: timestamppb.New(date)})}},
			accept: func(n int) bool {
				return syntheticWorkspace(n) == 0 && n/256%10 == 0 && n/2560%4 == 0 && n%5 != 0 && n%11 != 0 && n%60 >= 29
			}},
	}
}

func waitSyntheticIndex(t *testing.T, client *qdrant.Client, collection string, count int) *qdrant.CollectionInfo {
	t.Helper()
	var info *qdrant.CollectionInfo
	stable := 0
	budget := 15 * time.Minute
	if count == 1000000 {
		budget = 45 * time.Minute
	}
	deadline := time.Now().Add(budget)
	for time.Now().Before(deadline) {
		var err error
		info, err = client.GetCollectionInfo(t.Context(), collection)
		require.NoError(t, err)
		queue := syntheticHTTP(t, os.Getenv("TOPIC_STREAM_SYNTHETIC_QDRANT_HTTP"), "/collections/"+collection+"/optimizations?with=queued")
		idle, err := syntheticOptimizationIdle(queue)
		require.NoError(t, err)
		// At most two sub-threshold tails may be plain; green alone is not readiness.
		if info.Status == qdrant.CollectionStatus_Green && info.GetIndexedVectorsCount() >= uint64(count)*95/100 && info.GetOptimizerStatus().GetOk() && idle {
			stable++
			if stable == 3 {
				return info
			}
		} else {
			stable = 0
		}
		t.Logf("synthetic index status=%s indexed=%d points=%d", info.Status, info.GetIndexedVectorsCount(), info.GetPointsCount())
		select {
		case <-t.Context().Done():
			t.Fatal(t.Context().Err())
		case <-time.After(2 * time.Second):
		}
	}
	t.Fatalf("index not ready: %v", info)
	return nil
}

func syntheticRestartEndpoint(t *testing.T, container, port string) string {
	t.Helper()
	output, err := exec.CommandContext(t.Context(), "docker", "port", container, port).CombinedOutput()
	require.NoError(t, err, string(output))
	return "http://" + strings.TrimSpace(string(output))
}

func waitSyntheticHTTP(t *testing.T, endpoint string) {
	t.Helper()
	client := &http.Client{Timeout: time.Second}
	require.Eventually(t, func() bool {
		response, err := client.Get(endpoint + "/readyz")
		if err != nil {
			return false
		}
		_, _ = io.Copy(io.Discard, response.Body)
		_ = response.Body.Close()
		return response.StatusCode == http.StatusOK
	}, time.Minute, 100*time.Millisecond)
}

func syntheticResources(t *testing.T, collection string) map[string]string {
	t.Helper()
	container := os.Getenv("TOPIC_STREAM_SYNTHETIC_QDRANT_CONTAINER")
	require.True(t, strings.HasPrefix(container, "topic-stream-qdrant-search-perf-"))
	result := map[string]string{}
	commands := map[string][]string{
		"StorageKiB":                 {"du", "-sk", "/qdrant/storage"},
		"CollectionDiskBreakdownKiB": {"du", "-ak", "/qdrant/storage/collections/" + collection},
		"CgroupCurrentBytes":         {"cat", "/sys/fs/cgroup/memory.current"},
		"CgroupPeakBytes":            {"cat", "/sys/fs/cgroup/memory.peak"},
	}
	for name, arguments := range commands {
		output, err := exec.CommandContext(t.Context(), "docker", append([]string{"exec", container}, arguments...)...).CombinedOutput()
		require.NoError(t, err, string(output))
		result[name] = string(output)
	}
	processes, err := exec.CommandContext(t.Context(), "docker", "top", container, "-eo", "pid,ppid,rss,comm").CombinedOutput()
	require.NoError(t, err, string(processes))
	result["ProcessTable"] = string(processes)
	return result
}

func syntheticQuery(collection string, scenario syntheticSearchCase, n int, exact bool) *qdrant.QueryPoints {
	params := &qdrant.SearchParams{HnswEf: qdrant.PtrOf(uint64(256)), Acorn: &qdrant.AcornSearchParams{Enable: qdrant.PtrOf(true), MaxSelectivity: qdrant.PtrOf(.4)}}
	if exact {
		params = &qdrant.SearchParams{Exact: qdrant.PtrOf(true)}
	}
	return &qdrant.QueryPoints{CollectionName: collection, Query: qdrant.NewQuery(syntheticCapacityVector(uint64(n), true)...),
		Using: qdrant.PtrOf(vectorindex.DenseVectorName), Filter: scenario.filter, Limit: qdrant.PtrOf(uint64(20)), Params: params,
		WithPayload: qdrant.NewWithPayload(true), WithVectors: qdrant.NewWithVectors(true)}
}

func runSyntheticQueries(t *testing.T, client *qdrant.Client, collection string, run syntheticSearchRun) []syntheticSearchSample {
	t.Helper()
	samples := make([]syntheticSearchSample, run.count)
	var workers sync.WaitGroup
	for worker := range run.concurrency {
		workers.Go(func() {
			for n := worker; n < run.count; n += run.concurrency {
				request := syntheticQuery(collection, run.scenario, n+run.round*200, false)
				started := time.Now()
				result, err := client.Query(t.Context(), request)
				sample := syntheticSearchSample{Case: run.scenario.name, Phase: run.phase, Round: run.round, Concurrency: run.concurrency, Query: n + run.round*200, StartedAt: started, ElapsedMs: float64(time.Since(started)) / float64(time.Millisecond), Results: len(result)}
				if err != nil {
					sample.Error = err.Error()
				}
				if violation := syntheticFilterViolation(result, run.scenario); violation != "" {
					sample.Error = violation
				}
				samples[n] = sample
			}
		})
	}
	workers.Wait()
	return samples
}

func syntheticFilterViolation(result []*qdrant.ScoredPoint, scenario syntheticSearchCase) string {
	seen := map[string]bool{}
	for _, hit := range result {
		ordinal := hit.Payload["fixtureOrdinal"]
		if ordinal == nil || ordinal.GetIntegerValue() < 0 || !scenario.accept(int(ordinal.GetIntegerValue())) {
			return "scope/filter violation"
		}
		expected := syntheticCapacityID("item", int(ordinal.GetIntegerValue()))
		if hit.Id.GetUuid() != expected || hit.Payload["itemId"].GetStringValue() != expected || seen[expected] {
			return "point identity/duplicate violation"
		}
		seen[expected] = true
	}
	return ""
}

func syntheticRecall(t *testing.T, client *qdrant.Client, collection string, cases []syntheticSearchCase) map[string]any {
	t.Helper()
	report := map[string]any{}
	for _, scenario := range cases {
		result := syntheticCaseRecall(t, client, collection, scenario)
		report[scenario.name] = result
	}
	return report
}

func syntheticCaseRecall(t *testing.T, client *qdrant.Client, collection string, scenario syntheticSearchCase) map[string]any {
	t.Helper()
	values := map[string][]float64{}
	for _, start := range []int{0, 10000} {
		for n := range 20 {
			exact, err := client.Query(t.Context(), syntheticQuery(collection, scenario, start+n, true))
			require.NoError(t, err)
			approximate, err := client.Query(t.Context(), syntheticQuery(collection, scenario, start+n, false))
			require.NoError(t, err)
			require.Empty(t, syntheticFilterViolation(exact, scenario))
			require.Empty(t, syntheticFilterViolation(approximate, scenario))
			require.Len(t, exact, 20, "exact reference must be complete")
			require.LessOrEqual(t, len(approximate), 20)
			truth, ids := []string{}, []string{}
			for _, hit := range exact {
				truth = append(truth, hit.Id.GetUuid())
			}
			for _, hit := range approximate {
				ids = append(ids, hit.Id.GetUuid())
			}
			bank := "calibration"
			if start != 0 {
				bank = "heldout"
			}
			values[bank] = append(values[bank], syntheticRecallIDs(truth, ids))
		}
	}
	summary := map[string]any{}
	for bank, recalls := range values {
		total := 0.0
		for _, recall := range recalls {
			total += recall
		}
		mean := total / float64(len(recalls))
		target := syntheticRecallTarget([]float64{mean})
		summary[bank] = map[string]any{"Queries": len(recalls), "Values": recalls, "Mean": mean, "Minimum": slices.Min(recalls), "AcceptedTarget": target}
		require.GreaterOrEqual(t, mean, .95, "current policy recall below the accepted fallback: case=%s bank=%s", scenario.name, bank)
	}
	return summary
}

func syntheticWorkspace(n int) int {
	if os.Getenv("TOPIC_STREAM_SYNTHETIC_DISTRIBUTION") == "balanced" {
		return n / 64 % 4
	}
	if n%10 < 8 {
		return 0
	}
	return n%10 - 7
}

func writeSyntheticReport(t *testing.T, path string, report map[string]any) {
	t.Helper()
	encoded, err := json.MarshalIndent(report, "", "  ")
	require.NoError(t, err)
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o700))
	require.NoError(t, os.WriteFile(path, append(encoded, '\n'), 0o600))
}
