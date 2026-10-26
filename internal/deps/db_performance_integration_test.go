//go:build integration && performance

package deps

import (
	"context"
	"crypto/md5"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	generated "github.com/ethernity/obsidian-topic-stream/gen/config"
	"github.com/ethernity/obsidian-topic-stream/gen/serverhttp"
	"github.com/ethernity/obsidian-topic-stream/internal/domain"
	"github.com/ethernity/obsidian-topic-stream/internal/repository/postgres"
	"github.com/ethernity/obsidian-topic-stream/internal/service/settings"
	"github.com/ethernity/obsidian-topic-stream/internal/testutil"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"
)

// Opt-in, synthetic capacity characterization. Run with a disposable PostgreSQL constrained to
// 2 CPU / 4 GiB; see docs/performance.md. This never migrates the public schema.
func TestDatabasePerformanceHTTP(t *testing.T) {
	require.NotEmpty(t, os.Getenv("TOPIC_STREAM_TEST_POSTGRES_DSN"), "supply a disposable, resource-constrained PostgreSQL")
	fixture, conn, sourceID := newPerformanceFixture(t)
	workspaceID, runtime, store := fixture.workspaceID, fixture.runtime, fixture.store
	measureFeedDisplayCosts(t, fixture, conn)
	var err error
	var results []performanceResult
	for _, topics := range []int{100, 500} {
		_, err = conn.Exec(t.Context(), `INSERT INTO topics(id,workspace_id,name,include_text)
SELECT md5('topic:'||n)::uuid,$1,'Topic '||n,'' FROM generate_series(1,$2::integer) n ON CONFLICT DO NOTHING`, workspaceID, topics)
		require.NoError(t, err)
		_, err = conn.Exec(t.Context(), `INSERT INTO topic_candidate_state(workspace_id,topic_id)
SELECT workspace_id,id FROM topics WHERE workspace_id=$1 ON CONFLICT DO NOTHING`, workspaceID)
		require.NoError(t, err)
		for _, hidden := range []int{0, 50, 90} {
			_, err = conn.Exec(t.Context(), `DELETE FROM item_state WHERE workspace_id=$1`, workspaceID)
			require.NoError(t, err)
			_, err = conn.Exec(t.Context(), `INSERT INTO item_state(workspace_id,item_id,visibility)
SELECT $1,id,'hidden' FROM perf_seed WHERE abs(hashtext(id::text)::bigint)%100<$2`, workspaceID, hidden)
			require.NoError(t, err)
			_, err = conn.Exec(t.Context(), `TRUNCATE feed_display_entries`)
			require.NoError(t, err)
			_, err = conn.Exec(t.Context(), `INSERT INTO feed_display_entries(workspace_id,item_id,ordinal,topic_ids)
SELECT p.workspace_id,p.item_id,p.ordinal,p.matched_topic_ids FROM feed_publications p
WHERE p.workspace_id=$1 AND NOT EXISTS(SELECT 1 FROM item_state s WHERE s.workspace_id=p.workspace_id AND s.item_id=p.item_id AND s.visibility='hidden')`, workspaceID)
			require.NoError(t, err)
			_, err = conn.Exec(t.Context(), "ANALYZE")
			require.NoError(t, err)
			for _, mixed := range []bool{false, true} {
				results = append(results, measurePerformanceScenario(t, stableAPIFixture{runtime: runtime, store: store, workspaceID: workspaceID}, sourceID, performanceResult{Topics: topics, HiddenPercent: hidden, Background: mixed})...)
			}
		}
	}
	encoded, err := json.MarshalIndent(results, "", "  ")
	require.NoError(t, err)
	if path := os.Getenv("TOPIC_STREAM_PERF_REPORT"); path != "" {
		require.NoError(t, os.WriteFile(path, append(encoded, '\n'), 0o600))
	}
	for _, result := range results {
		if result.Topics == 100 && result.Background {
			require.LessOrEqual(t, result.P95, 200.0, "%+v", result)
		}
	}
}

func measurePerformanceScenario(t *testing.T, fixture stableAPIFixture, sourceID uuid.UUID, scenario performanceResult) []performanceResult {
	t.Helper()
	stop := func() {}
	if scenario.Background {
		stop = performanceBackground(t, fixture.store, fixture.workspaceID, sourceID)
	}
	defer stop()
	var results []performanceResult
	for _, route := range performanceRoutes(t, fixture.runtime.server.Handler, fixture.workspaceID) {
		result := measurePerformanceRoute(t, fixture.runtime.server.Handler, route)
		result.Topics, result.HiddenPercent, result.Background = scenario.Topics, scenario.HiddenPercent, scenario.Background
		results = append(results, result)
		t.Logf("topics=%d hidden=%d background=%t route=%s p50=%.2f p95=%.2f p99=%.2f ms", result.Topics, result.HiddenPercent, result.Background, route, result.P50, result.P95, result.P99)
	}
	return results
}

func seedPerformanceData(t *testing.T, conn *pgx.Conn, workspaceID, sourceID uuid.UUID) {
	t.Helper()
	queries := []struct {
		sql  string
		args []any
	}{
		{`CREATE UNLOGGED TABLE perf_seed AS SELECT n,md5('item:'||n)::uuid id FROM generate_series(1,1000000) n`, nil},
		{`INSERT INTO items(id,workspace_id,details,title,url,canonical_url,excerpt,content_markdown)
SELECT seed.id,$1,'{"type":"article","article":{"description":"Synthetic","content":"Body"}}','Item '||n,
'https://perf.example/'||n,'https://perf.example/'||n,'Synthetic article',
md5(n::text)||E'\n\n'||body.text||E'\n\n![Illustration](https://perf.example/image/'||n||'.png)'
FROM perf_seed seed CROSS JOIN (SELECT string_agg(md5(k::text),' ') text FROM generate_series(1,128) k) body`, []any{workspaceID}},
		{`INSERT INTO source_entries(source_id,external_id,item_id,url,details,discovered_at,updated_at)
SELECT $1,n::text,id,'https://perf.example/'||n,'{"type":"article","article":{"description":"Synthetic"}}',now(),now() FROM perf_seed`, []any{sourceID}},
		{`INSERT INTO feed_publications(workspace_id,item_id,batch_id,published_at,ordinal,matched_topic_ids,score,pipeline,model_id)
SELECT $1,id,$2,now(),n,
ARRAY[md5('topic:'||(3+n%98))::uuid] ||
CASE WHEN n%2=0 THEN ARRAY[md5('topic:1')::uuid] ELSE '{}'::uuid[] END ||
CASE WHEN n%10000=0 THEN ARRAY[md5('topic:2')::uuid] ELSE '{}'::uuid[] END,
0.8,'synthetic',$3 FROM perf_seed`, []any{workspaceID, uuid.New(), domain.DefaultEmbeddingModelID}},
		{`INSERT INTO feed_display_entries(workspace_id,item_id,ordinal,topic_ids)
SELECT workspace_id,item_id,ordinal,matched_topic_ids FROM feed_publications WHERE workspace_id=$1`, []any{workspaceID}},
		{`INSERT INTO feed_state(workspace_id,head_ordinal) VALUES ($1,1000000)
ON CONFLICT(workspace_id) DO UPDATE SET head_ordinal=EXCLUDED.head_ordinal`, []any{workspaceID}},
		{`INSERT INTO jobs(id,workspace_id,kind,queue,status,created_at,available_at,finished_at)
SELECT md5('job:'||n)::uuid,$1,'synthetic','maintenance',
CASE WHEN n<=100 THEN 'dead' WHEN n<=200 THEN 'queued' ELSE 'succeeded' END,
now()-interval '1000000 seconds'+n*interval '1 second',now()+interval '1 day',
CASE WHEN n<=200 THEN NULL ELSE now() END FROM generate_series(1,1000000) n`, []any{workspaceID}},
		{`INSERT INTO item_projection_state(workspace_id,item_id,revision,updated_at)
SELECT $1,id,md5('revision:'||n)::uuid,now() FROM perf_seed`, []any{workspaceID}},
		{`INSERT INTO retrieval_projection_pending(workspace_id,item_id,embedding_model_id,revision,updated_at)
SELECT workspace_id,item_id,$2,revision,updated_at FROM item_projection_state WHERE workspace_id=$1`, []any{workspaceID, domain.DefaultEmbeddingModelID}},
	}
	for index, query := range queries {
		start := time.Now()
		_, err := conn.Exec(t.Context(), query.sql, query.args...)
		require.NoError(t, err)
		t.Logf("seed %d completed in %s", index, time.Since(start))
	}
}

type performanceResult struct {
	Route                 string
	Topics, HiddenPercent int
	Background            bool
	Samples               int
	P50, P95, P99         float64
	ResponseBytes         int
}

func performanceRoutes(t *testing.T, handler http.Handler, workspaceID uuid.UUID) []string {
	t.Helper()
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/v1/feed", nil))
	require.Equal(t, 200, response.Code, response.Body.String())
	var page serverhttp.FeedPage
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &page))
	var snapshot struct{ Snapshot domain.FeedSnapshot }
	raw, err := base64.RawURLEncoding.DecodeString(page.Snapshot)
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(raw, &snapshot))
	require.Equal(t, workspaceID, snapshot.Snapshot.WorkspaceID)
	cursor, err := json.Marshal(struct {
		Kind     string
		Snapshot domain.FeedSnapshot
		After    int64
	}{"cursor", snapshot.Snapshot, 10000})
	require.NoError(t, err)
	return []string{
		"/api/v1/feed",
		"/api/v1/feed?topicId=" + uuid.UUID(md5.Sum([]byte("topic:1"))).String(),
		"/api/v1/feed?topicId=" + uuid.UUID(md5.Sum([]byte("topic:2"))).String(),
		"/api/v1/feed?snapshot=" + url.QueryEscape(page.Snapshot) + "&cursor=" + base64.RawURLEncoding.EncodeToString(cursor),
		"/api/v1/jobs", "/api/v1/jobs?status=queued", "/api/v1/jobs?status=dead",
	}
}

func measurePerformanceRoute(t *testing.T, handler http.Handler, route string) performanceResult {
	t.Helper()
	const samples = 200
	const concurrency = 4
	type sample struct {
		millis        float64
		status, bytes int
	}
	values := make(chan sample, samples)
	var workers sync.WaitGroup
	for range concurrency {
		workers.Go(func() {
			for range samples / concurrency {
				request := httptest.NewRequest(http.MethodGet, route, nil)
				response := httptest.NewRecorder()
				start := time.Now()
				handler.ServeHTTP(response, request)
				values <- sample{float64(time.Since(start)) / float64(time.Millisecond), response.Code, response.Body.Len()}
			}
		})
	}
	workers.Wait()
	close(values)
	durations := make([]float64, 0, samples)
	var bytes int
	for value := range values {
		require.Equal(t, 200, value.status)
		durations = append(durations, value.millis)
		bytes = max(bytes, value.bytes)
	}
	quantiles := testutil.Percentiles(durations)
	return performanceResult{Route: route, Samples: samples, P50: quantiles["p50"], P95: quantiles["p95"], P99: quantiles["p99"], ResponseBytes: bytes}
}

func performanceBackground(t *testing.T, store *postgres.Store, workspaceID, sourceID uuid.UUID) func() {
	t.Helper()
	ctx, cancel := context.WithCancel(t.Context())
	var workers sync.WaitGroup
	errors := make(chan error, 16)
	start := func(interval time.Duration, operation func(context.Context) error) {
		workers.Go(func() { runPerformanceOperation(ctx, interval, operation, errors) })
	}
	for worker := range 10 {
		start(time.Second, func(ctx context.Context) error {
			now := time.Now()
			_, err := store.Jobs().Claim(ctx, domain.JobClaimRequest{Queue: domain.JobQueueMaintenance, Owner: fmt.Sprint(worker), Now: now, LeaseExpiresAt: now.Add(time.Minute)})
			return err
		})
	}
	start(100*time.Millisecond, func(ctx context.Context) error {
		pending, err := store.ListPendingModelProjections(ctx, domain.ModelProjectionQuery{WorkspaceID: workspaceID, ModelID: domain.DefaultEmbeddingModelID, Limit: 100})
		if err != nil || len(pending) == 0 {
			return err
		}
		_, err = store.CompleteItemProjection(ctx, domain.ItemProjectionDelivery{WorkspaceID: workspaceID, ItemID: pending[0].ItemID, Revision: pending[0].ProjectionRevision, ModelID: domain.DefaultEmbeddingModelID})
		return err
	})
	start(time.Second, func(ctx context.Context) error {
		id, now := uuid.New(), time.Now()
		url := "https://perf.example/new/" + id.String()
		_, _, err := store.UpsertItem(ctx, domain.Item{ID: id, WorkspaceID: workspaceID, Kind: domain.ItemKindArticle, Details: domain.ItemDetails{Type: domain.ItemKindArticle, Article: &domain.ArticleDetails{}},
			Title: "Background ingestion", URL: url, CanonicalURL: url, ContentMarkdown: "Body", DiscoveredAt: now},
			domain.SourceEntry{SourceID: sourceID, ExternalID: id.String(), ItemID: id, URL: url, DiscoveredAt: now, UpdatedAt: now, Details: domain.SourceEntryDetails{Type: domain.ItemKindArticle, Article: &domain.SourceArticleDetails{}}})
		return err
	})
	start(time.Second, func(ctx context.Context) error {
		_, err := store.LoadFeedPreparationWork(ctx, domain.FeedPreparationQuery{WorkspaceID: workspaceID, At: time.Now(), Limit: 8})
		return err
	})
	var once sync.Once
	stop := func() {
		once.Do(func() {
			cancel()
			workers.Wait()
			close(errors)
			for err := range errors {
				require.NoError(t, err)
			}
		})
	}
	t.Cleanup(stop)
	return stop
}

func runPerformanceOperation(ctx context.Context, interval time.Duration, operation func(context.Context) error, errors chan<- error) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		if err := operation(ctx); err != nil {
			if ctx.Err() == nil {
				errors <- err
			}
			return
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func newPerformanceFixture(t *testing.T) (stableAPIFixture, *pgx.Conn, uuid.UUID) {
	t.Helper()
	dsn := testutil.MigratedPostgresSchema(t)
	store, err := postgres.Open(t.Context(), dsn)
	require.NoError(t, err)
	t.Cleanup(store.Close)
	conn, err := pgx.Connect(t.Context(), dsn)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, conn.Close(context.Background())) })
	workspaceID, sourceID := uuid.New(), uuid.New()
	now := time.Now().UTC().Truncate(time.Microsecond)
	require.NoError(t, store.EnsureWorkspace(t.Context(), workspaceID))
	require.NoError(t, store.EnsureDefaultEmbeddingModel(t.Context(), testutil.DefaultEmbeddingDescriptor(), now))
	// This fixture declares synthetic external delivery; it must never probe fixture.example.
	_, err = conn.Exec(t.Context(), `UPDATE embedding_models SET initialization_status='ready' WHERE id=$1`, domain.DefaultEmbeddingModelID)
	require.NoError(t, err)
	_, err = store.GetOrCreateWorkspaceSettings(t.Context(), workspaceID, settings.DefaultWorkspaceSettings(now))
	require.NoError(t, err)
	require.NoError(t, store.EmbeddingModels().EnsureWorkspaceState(t.Context(), workspaceID, domain.DefaultEmbeddingModelID, now))
	// Prior derived delivery is synthetic in this PostgreSQL-only workload.
	require.NoError(t, store.ObserveModelCollection(t.Context(), domain.ModelCollectionObservation{
		WorkspaceID: workspaceID, ModelID: domain.DefaultEmbeddingModelID, Incarnation: uuid.New(),
	}))
	require.NoError(t, store.Create(t.Context(), domain.Source{ID: sourceID, WorkspaceID: workspaceID, Name: "Performance", Enabled: true,
		Config: domain.SourceConfig{Kind: domain.SourceKindRSS, URL: "https://perf.example/feed"}, PollIntervalMinutes: 60, CreatedAt: now, UpdatedAt: now}))
	// Prove HTTP graph readiness with the full pending backlog already durable.
	seedPerformanceData(t, conn, workspaceID, sourceID)
	if os.Getenv("TOPIC_STREAM_CAPTURE_STATEMENTS") == "true" {
		_, err = conn.Exec(t.Context(), `CREATE EXTENSION IF NOT EXISTS pg_stat_statements`)
		require.NoError(t, err)
		capacitySQLProfile{conn: conn}.resetStatements(t)
	}
	startup := time.Now()
	runtime, err := NewAPI(t.Context(), generated.Config{
		Bootstrap: generated.BootstrapConfig{BootstrapEmbeddingEndpoint: "http://fixture.example/v1", BootstrapEmbeddingModel: "synthetic", BootstrapEmbeddingDimensions: 3},
		DBPrimary: generated.DBPrimaryConfig{Kind: "postgres", PostgresDSN: dsn},
		HTTP:      generated.HTTPConfig{Enabled: true, Address: "127.0.0.1:0"},
		Runtime:   generated.RuntimeConfig{EmbeddingBatchSize: 8, EmbeddingBatchInputBytes: 1 << 20, EmbeddingConcurrency: 1, WorkspaceId: workspaceID.String(), QdrantUrl: testutil.QdrantURL(t), ShutdownTimeout: 5 * time.Second},
	}, "db-performance")
	startupMS := float64(time.Since(startup)) / float64(time.Millisecond)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, runtime.Shutdown(context.Background())) })
	startupSQL := capacitySQLProfile{conn: conn}.statements(t)
	var queued int
	require.NoError(t, conn.QueryRow(t.Context(), `SELECT count(*) FROM jobs WHERE kind='index-item'`).Scan(&queued))
	ready, encodeErr := json.MarshalIndent(map[string]any{"pendingItems": 1000000, "startupMilliseconds": startupMS, "startupIndexJobs": queued, "ServerSQL": startupSQL}, "", "  ")
	require.NoError(t, encodeErr)
	if path := os.Getenv("TOPIC_STREAM_PERF_REPORT"); path != "" {
		require.NoError(t, os.WriteFile(filepath.Join(filepath.Dir(path), "startup-readiness.json"), append(ready, '\n'), 0o600))
	}
	t.Log(string(ready))
	require.Zero(t, queued)
	require.Less(t, startupMS, 5000.0, "graph construction must not drain the backlog")
	return stableAPIFixture{runtime: runtime, store: store, workspaceID: workspaceID}, conn, sourceID
}

func measureFeedDisplayCosts(t *testing.T, f stableAPIFixture, conn *pgx.Conn) {
	t.Helper()
	var heap, indexes, count int64
	require.NoError(t, conn.QueryRow(t.Context(), `SELECT pg_table_size('feed_display_entries'),pg_indexes_size('feed_display_entries'),count(*) FROM feed_display_entries`).Scan(&heap, &indexes, &count))
	results := map[string]any{"heapBytes": heap, "indexBytes": indexes, "entries": count}
	itemID := uuid.UUID(md5.Sum([]byte("item:1")))
	durations := map[domain.ItemVisibility][]float64{}
	wal := map[domain.ItemVisibility]int64{}
	for range 120 {
		for _, visibility := range []domain.ItemVisibility{domain.ItemVisibilityHidden, domain.ItemVisibilityVisible} {
			var lsn string
			require.NoError(t, conn.QueryRow(t.Context(), `SELECT pg_current_wal_insert_lsn()::text`).Scan(&lsn))
			started := time.Now()
			found, err := f.store.SetVisibility(t.Context(), f.workspaceID, itemID, visibility)
			durations[visibility] = append(durations[visibility], float64(time.Since(started))/float64(time.Millisecond))
			require.NoError(t, err)
			require.True(t, found)
			var bytes int64
			require.NoError(t, conn.QueryRow(t.Context(), `SELECT pg_wal_lsn_diff(pg_current_wal_insert_lsn(),$1::pg_lsn)::bigint`, lsn).Scan(&bytes))
			wal[visibility] += bytes
		}
	}
	for visibility, values := range durations {
		quantiles := testutil.Percentiles(values)
		results[string(visibility)] = map[string]any{"p50Milliseconds": quantiles["p50"], "p95Milliseconds": quantiles["p95"], "p99Milliseconds": quantiles["p99"], "averageWALBytes": wal[visibility] / 120}
	}
	encoded, err := json.MarshalIndent(results, "", "  ")
	require.NoError(t, err)
	if path := os.Getenv("TOPIC_STREAM_PERF_REPORT"); path != "" {
		require.NoError(t, os.WriteFile(filepath.Join(filepath.Dir(path), "feed-display-costs.json"), append(encoded, '\n'), 0o600))
	}
	t.Log(string(encoded))
}
