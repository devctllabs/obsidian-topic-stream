//go:build integration

package deps

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/devctllabs/go-libs/di"
	generated "github.com/ethernity/obsidian-topic-stream/gen/config"
	"github.com/ethernity/obsidian-topic-stream/gen/serverhttp"
	"github.com/ethernity/obsidian-topic-stream/internal/domain"
	"github.com/ethernity/obsidian-topic-stream/internal/platform/vectorindex"
	"github.com/ethernity/obsidian-topic-stream/internal/repository/postgres"
	"github.com/ethernity/obsidian-topic-stream/internal/service/settings"
	"github.com/ethernity/obsidian-topic-stream/internal/testutil"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"
)

type stableAPIFixture struct {
	runtime     *API
	store       *postgres.Store
	dsn         string
	workspaceID uuid.UUID
	topics      []uuid.UUID
	embeddings  *atomic.Int64
	stopWorkers func()
	workers     *stableAPIWorkers
}

type stableAPIWorkers struct {
	done chan struct{}
	err  error // Published by closing done; readers wait for that close.
}
type apiExchange struct {
	method, path string
	status       int
	body         any
}

func (f stableAPIFixture) exchange(t *testing.T, x apiExchange, result any) {
	t.Helper()
	var body bytes.Buffer
	if x.body != nil {
		require.NoError(t, json.NewEncoder(&body).Encode(x.body))
	}
	request := httptest.NewRequest(x.method, x.path, &body)
	if x.body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	response := httptest.NewRecorder()
	f.runtime.server.Handler.ServeHTTP(response, request)
	require.Equal(t, x.status, response.Code, response.Body.String())
	if result != nil {
		require.NoError(t, json.Unmarshal(response.Body.Bytes(), result))
	}
}
func (f stableAPIFixture) feed(t *testing.T, method, path string) serverhttp.FeedPage {
	t.Helper()
	x := apiExchange{method: method, path: path, status: 200}
	if method == http.MethodPost {
		x.body = map[string]any{}
	}
	var page serverhttp.FeedPage
	f.exchange(t, x, &page)
	return page
}
func (f stableAPIFixture) prepare(t *testing.T) {
	t.Helper()
	conn, err := pgx.Connect(t.Context(), f.dsn)
	require.NoError(t, err)
	defer func() { require.NoError(t, conn.Close(context.Background())) }()
	f.awaitReady(t, "projection", 10*time.Second, func(ctx context.Context) (bool, error) {
		if err := f.runtime.projectionReconciler.Reconcile(ctx); err != nil {
			return false, err
		}
		var count int
		err := conn.QueryRow(ctx, `SELECT count(*) FROM retrieval_projection_pending WHERE workspace_id=$1`, f.workspaceID).Scan(&count)
		return count == 0, err
	})
	_, err = conn.Exec(t.Context(), `UPDATE topic_candidate_state SET first_dirty_at=clock_timestamp()-interval '2 minutes' WHERE workspace_id=$1 AND requested_version>ready_version`, f.workspaceID)
	require.NoError(t, err)
	f.awaitReady(t, "candidate preparation", 15*time.Second, func(ctx context.Context) (bool, error) {
		if err := f.runtime.preparationReconciler.Reconcile(ctx); err != nil {
			return false, err
		}
		var count int
		err := conn.QueryRow(ctx, `SELECT count(*) FROM topic_candidate_state WHERE workspace_id=$1 AND (ready_version=0 OR requested_version>ready_version)`, f.workspaceID).Scan(&count)
		return count == 0, err
	})
}

func (f stableAPIFixture) awaitReady(t *testing.T, phase string, budget time.Duration, check func(context.Context) (bool, error)) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), budget)
	defer cancel()
	ticker := time.NewTicker(20 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-f.workers.done:
			f.logPendingJobs(t)
			t.Fatalf("workers stopped during %s readiness: %v", phase, f.workers.err)
		default:
		}
		ready, err := check(ctx)
		if err != nil {
			f.logPendingJobs(t)
			require.NoError(t, err, "%s readiness", phase)
		}
		if ready {
			return
		}
		select {
		case <-ctx.Done():
			f.logPendingJobs(t)
			t.Fatalf("%s readiness: %v", phase, ctx.Err())
		case <-ticker.C:
		}
	}
}

func (f stableAPIFixture) logPendingJobs(t *testing.T) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	jobs, err := f.store.Jobs().List(ctx, domain.JobList{WorkspaceID: f.workspaceID, Limit: 20})
	if err != nil {
		t.Logf("readiness Job diagnostics unavailable: %v", err)
		return
	}
	for _, job := range jobs {
		reason := ""
		if job.Reason != nil {
			reason = *job.Reason
		}
		t.Logf("readiness Job %s kind=%s status=%s attempt=%d reason=%q", job.ID, job.Kind, job.Status, job.Attempt, reason)
	}
}
func (f stableAPIFixture) correct(t *testing.T, itemID, topicID uuid.UUID, membership string) {
	t.Helper()
	f.exchange(t, apiExchange{method: http.MethodPut, path: "/api/v1/items/" + itemID.String() + "/topic-corrections", status: 204, body: map[string]any{"topicId": topicID, "membership": membership}}, nil)
}

func newStableAPIFixture(t *testing.T) stableAPIFixture {
	t.Helper()
	f := newStableHTTPFixture(t)
	ctx, cancel := context.WithCancel(t.Context())
	f.workers = &stableAPIWorkers{done: make(chan struct{})}
	go func() { f.workers.err = f.runtime.jobs.Run(ctx); close(f.workers.done) }()
	var stopOnce sync.Once
	f.stopWorkers = func() {
		stopOnce.Do(func() { cancel(); testutil.Receive(t, f.workers.done); require.NoError(t, f.workers.err) })
	}
	t.Cleanup(f.stopWorkers)
	return f
}

// newStableHTTPFixture owns the real HTTP graph without executing admitted Jobs.
// Admission-only cases must not cancel an unrelated collection creation during cleanup.
func newStableHTTPFixture(t *testing.T) stableAPIFixture {
	t.Helper()
	count := &atomic.Int64{}
	embeddingServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		texts, err := fixtureEmbeddingInputs(r)
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		count.Add(int64(len(texts)))
		fixtureEmbeddingResponse(w, texts, []float32{1, 0, 0})
	}))
	t.Cleanup(embeddingServer.Close)
	f := stableAPIFixture{dsn: testutil.MigratedPostgresSchema(t), workspaceID: uuid.New(), embeddings: count}
	store, err := postgres.Open(t.Context(), f.dsn)
	require.NoError(t, err)
	t.Cleanup(store.Close)
	f.store = store
	old := time.Now().UTC().Add(-2 * time.Minute)
	require.NoError(t, store.EnsureWorkspace(t.Context(), f.workspaceID))
	model := domain.EmbeddingModel{ID: uuid.New(), EmbeddingModelDescriptor: domain.EmbeddingModelDescriptor{Name: "HTTP stable feed fixture", Endpoint: embeddingServer.URL, ProviderModel: "synthetic", Dimensions: 3}, Enabled: true, InitializationStatus: domain.EmbeddingModelReady, Version: 1, CreatedAt: old, UpdatedAt: old}
	require.NoError(t, store.EmbeddingModels().Create(t.Context(), model))
	cfg := settings.DefaultWorkspaceSettings(old)
	cfg.EmbeddingModelID = model.ID
	cfg.Retrieval.CandidateLimit = 2
	_, err = store.GetOrCreateWorkspaceSettings(t.Context(), f.workspaceID, cfg)
	require.NoError(t, err)
	client, err := vectorindex.NewClient(testutil.QdrantURL(t))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, client.Close()) })
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		collection := vectorindex.CollectionName(model.ID)
		exists, existsErr := client.CollectionExists(ctx, collection)
		require.NoError(t, existsErr)
		t.Logf("integration owned_collection=%s phase=collection_cleanup exists=%t", collection, exists)
		if exists {
			require.NoError(t, client.DeleteCollection(ctx, collection))
		}
	})
	runtime, err := NewAPI(t.Context(), generated.Config{Bootstrap: generated.BootstrapConfig{BootstrapEmbeddingEndpoint: "http://fixture.example/v1", BootstrapEmbeddingModel: "synthetic", BootstrapEmbeddingDimensions: 3}, DBPrimary: generated.DBPrimaryConfig{Kind: "postgres", PostgresDSN: f.dsn}, HTTP: generated.HTTPConfig{Enabled: true, Address: "127.0.0.1:0"}, Runtime: generated.RuntimeConfig{QdrantFullScanThresholdKb: 100000, QdrantHnswEf: 256, QdrantAcornEnabled: true, QdrantAcornMaxSelectivityPercent: 40, EmbeddingBatchSize: 8, EmbeddingBatchInputBytes: 1 << 20, EmbeddingConcurrency: 1, WorkspaceId: f.workspaceID.String(), WorkersEnabled: true, SchedulersEnabled: false, QdrantUrl: testutil.QdrantURL(t), ShutdownTimeout: time.Second, WorkerFetchConcurrency: 2, WorkerFetchPerHostConcurrency: 1, WorkerGenerationConcurrency: 1, WorkerRetrievalConcurrency: 2, WorkerFeedPreparationConcurrency: 2, WorkerMaintenanceConcurrency: 1}}, "stable-feed-test")
	require.NoError(t, err)
	f.runtime = runtime
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		require.NoError(t, runtime.Shutdown(ctx))
	})
	for _, name := range []string{"Systems", "Engineering"} {
		var topic serverhttp.Topic
		f.exchange(t, apiExchange{method: http.MethodPost, path: "/api/v1/topics", status: 201, body: map[string]any{"name": name, "sourceIds": []uuid.UUID{}, "itemKinds": []string{}, "exclude": "", "include": "systems", "mode": "ordinary", "enabled": true}}, &topic)
		f.topics = append(f.topics, topic.Id)
	}
	sourceID := uuid.New()
	require.NoError(t, store.Create(t.Context(), domain.Source{ID: sourceID, WorkspaceID: f.workspaceID, Name: "Synthetic source", Config: domain.SourceConfig{Kind: domain.SourceKindRSS, URL: "https://fixture.example/feed"}, Enabled: true, PollIntervalMinutes: 60, CreatedAt: old, UpdatedAt: old}))
	for _, name := range []string{"Alpha systems", "Beta systems", "Gamma systems"} {
		id := uuid.New()
		canonical := "https://fixture.example/" + id.String()
		_, _, err := store.UpsertItem(t.Context(), domain.Item{ID: id, WorkspaceID: f.workspaceID, Kind: domain.ItemKindArticle, Title: name, URL: canonical, CanonicalURL: canonical, DiscoveredAt: old, ContentMarkdown: "Synthetic systems article", Details: domain.ItemDetails{Type: domain.ItemKindArticle, Article: &domain.ArticleDetails{Description: "Fixture", Content: "Synthetic systems article"}}}, domain.SourceEntry{SourceID: sourceID, ExternalID: id.String(), URL: canonical, DiscoveredAt: old, UpdatedAt: old, Details: domain.SourceEntryDetails{Type: domain.ItemKindArticle, Article: &domain.SourceArticleDetails{Description: "Fixture"}}})
		require.NoError(t, err)
	}
	return f
}

func TestStableFeedHTTPPipelinePaginationAndSearchCorrections(t *testing.T) {
	f := newStableAPIFixture(t)
	cold := f.feed(t, http.MethodGet, "/api/v1/feed")
	require.Empty(t, cold.Items)
	f.prepare(t)
	ready := f.feed(t, http.MethodGet, "/api/v1/feed")
	require.Empty(t, ready.Items, "preparation never publishes")
	embeddings := f.embeddings.Load()
	first := f.feed(t, http.MethodPost, "/api/v1/feed/refresh")
	require.Len(t, first.Items, 2)
	require.Equal(t, embeddings, f.embeddings.Load(), "Refresh reuses ready lists without Topic embeddings")
	target := first.Items[0].Item.Id
	before, err := f.store.ReadFeedPublications(t.Context(), domain.FeedPublicationQuery{WorkspaceID: f.workspaceID, HeadOrdinal: 2, Limit: 25})
	require.NoError(t, err)
	var one serverhttp.FeedPage
	f.exchange(t, apiExchange{method: http.MethodPost, path: "/api/v1/feed/refresh", status: 200, body: map[string]any{"limit": 1}}, &one)
	require.NotNil(t, one.NextCursor)
	oldSnapshot := one.Snapshot
	f.prepare(t)
	f.exchange(t, apiExchange{method: http.MethodPost, path: "/api/v1/feed/refresh", status: 200, body: map[string]any{"topicId": f.topics[0]}}, nil)
	old := f.feed(t, http.MethodGet, "/api/v1/feed?snapshot="+url.QueryEscape(oldSnapshot))
	require.Len(t, old.Items, 2)
	older := f.feed(t, http.MethodGet, "/api/v1/feed?limit=1&snapshot="+url.QueryEscape(oldSnapshot)+"&cursor="+url.QueryEscape(*one.NextCursor))
	require.Len(t, older.Items, 1)
	require.Equal(t, first.Items[1].Item.Id, older.Items[0].Item.Id)
	latest := f.feed(t, http.MethodPost, "/api/v1/feed/refresh")
	require.Len(t, latest.Items, 3)
	f.correct(t, target, f.topics[0], "exclude")
	f.correct(t, target, f.topics[1], "exclude")
	hiddenByCorrections := f.feed(t, http.MethodGet, "/api/v1/feed")
	require.Len(t, hiddenByCorrections.Items, 2)
	var search serverhttp.SearchPage
	f.exchange(t, apiExchange{method: http.MethodGet, path: "/api/v1/search?q=" + url.QueryEscape(first.Items[0].Item.Title) + "&mode=lexical", status: 200}, &search)
	require.NotEmpty(t, search.Items)
	var added serverhttp.Topic
	f.exchange(t, apiExchange{method: http.MethodPost, path: "/api/v1/topics", status: 201, body: map[string]any{"name": "Manual", "sourceIds": []uuid.UUID{}, "itemKinds": []string{}, "include": "", "exclude": "", "enabled": true, "mode": "ordinary"}}, &added)
	f.correct(t, target, added.Id, "include")
	restored := f.feed(t, http.MethodGet, "/api/v1/feed")
	require.Len(t, restored.Items, 3)
	f.correct(t, target, added.Id, "automatic")
	require.Len(t, f.feed(t, http.MethodGet, "/api/v1/feed").Items, 2)
	f.correct(t, target, f.topics[0], "automatic")
	require.Len(t, f.feed(t, http.MethodGet, "/api/v1/feed").Items, 3)
	f.exchange(t, apiExchange{method: http.MethodPut, path: "/api/v1/items/" + target.String() + "/visibility", status: 204, body: map[string]any{"visibility": "hidden"}}, nil)
	require.Len(t, f.feed(t, http.MethodGet, "/api/v1/feed").Items, 2)
	f.exchange(t, apiExchange{method: http.MethodGet, path: "/api/v1/search?q=" + url.QueryEscape(first.Items[0].Item.Title) + "&mode=lexical", status: 200}, &search)
	found := false
	for _, item := range search.Items {
		found = found || item.Item.Id == target
	}
	require.True(t, found, "Hide preserves Search access")
	f.exchange(t, apiExchange{method: http.MethodPut, path: "/api/v1/items/" + target.String() + "/visibility", status: 204, body: map[string]any{"visibility": "visible"}}, nil)
	require.Len(t, f.feed(t, http.MethodGet, "/api/v1/feed").Items, 3)
	after, err := f.store.ReadFeedPublications(t.Context(), domain.FeedPublicationQuery{WorkspaceID: f.workspaceID, HeadOrdinal: 3, Limit: 25})
	require.NoError(t, err)
	original := before[0].Publication
	for _, row := range after {
		if row.Item.ID == target {
			require.Equal(t, original.Ordinal, row.Publication.Ordinal)
			require.WithinDuration(t, original.PublishedAt, row.Publication.PublishedAt, 0)
			require.Equal(t, original.Signals, row.Publication.Signals)
			require.ElementsMatch(t, f.topics, row.Publication.MatchedTopicIDs)
		}
	}
	legacy := base64.RawURLEncoding.EncodeToString([]byte(`{"generatedAt":"2026-10-01T00:00:00Z"}`))
	f.exchange(t, apiExchange{method: http.MethodGet, path: "/api/v1/feed?snapshot=" + legacy, status: 400}, nil)
}

func TestStableFeedHTTPReplicasSharePublicationHeadAcrossRestart(t *testing.T) {
	f := newStableAPIFixture(t)
	f.prepare(t)
	cfg, err := di.Resolve[generated.Config](f.runtime.container)
	require.NoError(t, err)
	cfg.Runtime.WorkersEnabled = false
	replica, err := NewAPI(t.Context(), cfg, "stable-feed-replica")
	require.NoError(t, err)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		require.NoError(t, replica.Shutdown(ctx))
	})
	type response struct {
		status int
		body   []byte
	}
	outcomes := make(chan response, 2)
	start := make(chan struct{})
	ctx, cancel := context.WithCancel(t.Context())
	var requests sync.WaitGroup
	t.Cleanup(func() { cancel(); requests.Wait() })
	for _, handler := range []http.Handler{f.runtime.server.Handler, replica.server.Handler} {
		requests.Add(1)
		go func() {
			defer requests.Done()
			<-start
			request := httptest.NewRequestWithContext(ctx, http.MethodPost, "/api/v1/feed/refresh", bytes.NewBufferString(`{}`))
			request.Header.Set("Content-Type", "application/json")
			recorder := httptest.NewRecorder()
			handler.ServeHTTP(recorder, request)
			outcomes <- response{status: recorder.Code, body: recorder.Body.Bytes()}
		}()
	}
	close(start)
	for range 2 {
		result := testutil.Receive(t, outcomes)
		require.Equal(t, 200, result.status, string(result.body))
		var page serverhttp.FeedPage
		require.NoError(t, json.Unmarshal(result.body, &page))
		require.Len(t, page.Items, 2)
	}
	head, err := f.store.FeedHead(t.Context(), f.workspaceID)
	require.NoError(t, err)
	require.Equal(t, int64(2), head)
	expected := f.feed(t, http.MethodGet, "/api/v1/feed")
	require.NoError(t, replica.Shutdown(t.Context()))
	replica, err = NewAPI(t.Context(), cfg, "stable-feed-restarted")
	require.NoError(t, err)
	restarted := stableAPIFixture{runtime: replica}.feed(t, http.MethodGet, "/api/v1/feed")
	require.Equal(t, expected.Items, restarted.Items)
}
