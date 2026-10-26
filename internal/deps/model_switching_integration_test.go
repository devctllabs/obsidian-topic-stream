//go:build integration

package deps

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/devctllabs/go-libs/di"
	generated "github.com/ethernity/obsidian-topic-stream/gen/config"
	"github.com/ethernity/obsidian-topic-stream/internal/domain"
	"github.com/ethernity/obsidian-topic-stream/internal/platform/vectorindex"
	"github.com/ethernity/obsidian-topic-stream/internal/service/ranking"
	"github.com/ethernity/obsidian-topic-stream/internal/service/retrieval"
	"github.com/ethernity/obsidian-topic-stream/internal/testutil"
	"github.com/google/uuid"
	"github.com/qdrant/go-client/qdrant"
	"github.com/stretchr/testify/require"
)

func TestLatestModelSelectionHTTPWhilePreviousProviderIsBlocked(t *testing.T) {
	for _, returnToServing := range []bool{false, true} {
		t.Run(strconv.FormatBool(returnToServing), func(t *testing.T) {
			f := newStableAPIFixture(t)
			f.prepare(t)
			published := f.feed(t, http.MethodPost, "/api/v1/feed/refresh")
			require.NotEmpty(t, published.Items)
			stored, _, err := f.store.GetWorkspaceSettings(t.Context(), f.workspaceID)
			require.NoError(t, err)
			serving, err := f.store.ResolveModel(t.Context(), stored.EmbeddingModelID)
			require.NoError(t, err)
			entered, release := make(chan struct{}), make(chan struct{})
			var enterOnce, releaseOnce sync.Once
			provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				texts, err := fixtureEmbeddingInputs(r)
				if err != nil {
					w.WriteHeader(http.StatusBadRequest)
					return
				}
				enterOnce.Do(func() { close(entered) })
				select {
				case <-release:
					fixtureEmbeddingResponse(w, texts, []float32{1, 0, 0})
				case <-r.Context().Done():
				}
			}))
			t.Cleanup(provider.Close)
			t.Cleanup(func() { releaseOnce.Do(func() { close(release) }) })
			blocked := serving
			blocked.ID, blocked.Name, blocked.Endpoint = uuid.New(), "Blocked transition", provider.URL
			require.NoError(t, f.store.EmbeddingModels().Create(t.Context(), blocked))
			latest := serving
			if !returnToServing {
				latest.ID, latest.Name = uuid.New(), "Latest transition"
				require.NoError(t, f.store.EmbeddingModels().Create(t.Context(), latest))
			}
			cleanupModelCollections(t, f, []uuid.UUID{blocked.ID, latest.ID}, serving.ID)
			selected := patchModelHTTP(t, f, stored.Version, blocked.ID)
			testutil.Receive(t, entered)
			status, err := f.store.EmbeddingModels().RetrievalStatus(t.Context(), f.workspaceID)
			require.NoError(t, err)
			require.NotNil(t, status.TransitionJobID)
			oldJobID := *status.TransitionJobID
			started := time.Now()
			selected = patchModelHTTP(t, f, selected.Version, latest.ID)
			require.Less(t, time.Since(started), time.Second, "selection must commit while the older provider is blocked")
			require.Equal(t, latest.ID, selected.EmbeddingModelId)
			active, _, err := f.store.GetActive(t.Context(), f.workspaceID)
			require.NoError(t, err)
			require.Equal(t, serving.ID, active.ModelID, "the previous complete model serves during replacement")
			stale := modelPatchResponse(t, f, stored.Version, blocked.ID)
			require.Equal(t, http.StatusConflict, stale.Code, "real optimistic settings conflicts stay conflicts")
			releaseOnce.Do(func() { close(release) })
			require.Eventually(t, func() bool {
				active, exists, err := f.store.GetActive(t.Context(), f.workspaceID)
				return err == nil && exists && active.ModelID == latest.ID
			}, 15*time.Second, 20*time.Millisecond)
			require.Eventually(t, func() bool {
				job, exists, err := f.store.Jobs().Get(t.Context(), oldJobID)
				return err == nil && exists && job.Status != domain.JobStatusRunning
			}, 15*time.Second, 20*time.Millisecond)
			active, _, err = f.store.GetActive(t.Context(), f.workspaceID)
			require.NoError(t, err)
			require.Equal(t, latest.ID, active.ModelID, "a late provider result cannot activate the superseded model")
			after := f.feed(t, http.MethodGet, "/api/v1/feed")
			require.Equal(t, published.Items, after.Items, "saved publication facts survive transitions")
		})
	}
}

func TestReturningModelRecoversCollectionRecreatedByAnotherWriter(t *testing.T) {
	f := newStableAPIFixture(t)
	f.prepare(t)
	active, _, err := f.store.GetActive(t.Context(), f.workspaceID)
	require.NoError(t, err)
	require.True(t, active.Initialized, "fresh collection identity must be bound to Serving state")
	require.NotEqual(t, uuid.Nil, active.CollectionIncarnation)
	original := active.ModelID
	model, err := f.store.ResolveModel(t.Context(), original)
	require.NoError(t, err)
	model.ID, model.Name = uuid.New(), "Collection recovery target"
	require.NoError(t, f.store.EmbeddingModels().Create(t.Context(), model))
	cleanupModelCollections(t, f, []uuid.UUID{model.ID}, original)
	stored, _, err := f.store.GetWorkspaceSettings(t.Context(), f.workspaceID)
	require.NoError(t, err)
	selected := patchModelHTTP(t, f, stored.Version, model.ID)
	awaitServingModel(t, f, model.ID)
	before, _, err := f.store.GetActive(t.Context(), f.workspaceID)
	require.NoError(t, err)
	selected = patchModelHTTP(t, f, selected.Version, original)
	awaitServingModel(t, f, original)
	client, err := vectorindex.NewClient(testutil.QdrantURL(t))
	require.NoError(t, err)
	defer func() { require.NoError(t, client.Close()) }()
	require.NoError(t, client.DeleteCollection(t.Context(), vectorindex.CollectionName(model.ID)))
	otherWriter, err := vectorindex.New(client, vectorindex.CollectionName(model.ID), model.Dimensions)
	require.NoError(t, err)
	require.NoError(t, otherWriter.Ensure(t.Context()))
	replaced, err := otherWriter.CollectionIncarnation(t.Context())
	require.NoError(t, err)
	require.NotEqual(t, before.CollectionIncarnation, replaced)
	patchModelHTTP(t, f, selected.Version, model.ID)
	awaitServingModel(t, f, model.ID)
	after, _, err := f.store.GetActive(t.Context(), f.workspaceID)
	require.NoError(t, err)
	require.True(t, after.Initialized)
	require.Equal(t, replaced, after.CollectionIncarnation)
	points, err := client.Count(t.Context(), &qdrant.CountPoints{CollectionName: vectorindex.CollectionName(model.ID), Exact: qdrant.PtrOf(true)})
	require.NoError(t, err)
	require.EqualValues(t, 3, points, "complete recovery must restore all canonical Items")
}

func awaitServingModel(t *testing.T, f stableAPIFixture, modelID uuid.UUID) {
	t.Helper()
	require.Eventually(t, func() bool {
		if err := f.runtime.modelReconciler.Reconcile(t.Context()); err != nil {
			return false
		}
		active, exists, err := f.store.GetActive(t.Context(), f.workspaceID)
		return err == nil && exists && active.ModelID == modelID && active.Initialized
	}, 20*time.Second, 20*time.Millisecond)
}

func TestServingCollectionLossRequestsRecoveryAndKeepsSavedFeedReadable(t *testing.T) {
	f := newStableAPIFixture(t)
	f.prepare(t)
	published := f.feed(t, http.MethodPost, "/api/v1/feed/refresh")
	f.prepare(t)
	active, _, err := f.store.GetActive(t.Context(), f.workspaceID)
	require.NoError(t, err)
	client, err := vectorindex.NewClient(testutil.QdrantURL(t))
	require.NoError(t, err)
	defer func() { require.NoError(t, client.Close()) }()
	require.NoError(t, client.DeleteCollection(t.Context(), active.Collection))
	manager, err := di.Resolve[*retrieval.Manager](f.runtime.container)
	require.NoError(t, err)
	_, err = manager.QueryRecent(t.Context(), ranking.IndexFilter{WorkspaceID: f.workspaceID}, 10)
	require.Error(t, err, "a recreated empty collection must not be returned as complete retrieval")
	initializing, _, err := f.store.GetActive(t.Context(), f.workspaceID)
	require.NoError(t, err)
	require.False(t, initializing.Initialized)
	status, err := f.store.EmbeddingModels().RetrievalStatus(t.Context(), f.workspaceID)
	require.NoError(t, err)
	require.Nil(t, status.ServingModelID, "runtime status must not advertise a lost instance as ready Serving")
	f.exchange(t, apiExchange{method: http.MethodGet, path: "/api/v1/search?q=recovery&mode=semantic", status: http.StatusServiceUnavailable}, nil)
	after := f.feed(t, http.MethodGet, "/api/v1/feed")
	require.Equal(t, published.Items, after.Items)
	awaitServingModel(t, f, active.ModelID)
	hits, err := manager.QueryRecent(t.Context(), ranking.IndexFilter{WorkspaceID: f.workspaceID}, 10)
	require.NoError(t, err)
	require.Len(t, hits, 3)
}

func TestIndependentHTTPObservesWorkerCutoverAndEndpointRelocationWithoutAdvisories(t *testing.T) {
	f := newStableAPIFixture(t)
	f.prepare(t)
	stored, _, err := f.store.GetWorkspaceSettings(t.Context(), f.workspaceID)
	require.NoError(t, err)
	original := stored.EmbeddingModelID
	httpRuntime, err := NewAPI(t.Context(), generated.Config{
		Bootstrap: generated.BootstrapConfig{BootstrapEmbeddingEndpoint: "http://fixture.example/v1", BootstrapEmbeddingModel: "synthetic", BootstrapEmbeddingDimensions: 3},
		DBPrimary: generated.DBPrimaryConfig{Kind: "postgres", PostgresDSN: f.dsn}, HTTP: generated.HTTPConfig{Enabled: true, Address: "127.0.0.1:0"},
		Runtime: generated.RuntimeConfig{EmbeddingBatchSize: 8, EmbeddingBatchInputBytes: 1 << 20, EmbeddingConcurrency: 1, WorkspaceId: f.workspaceID.String(), QdrantUrl: testutil.QdrantURL(t), ShutdownTimeout: time.Second},
	}, "independent-http")
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, httpRuntime.Shutdown(context.Background())) })
	// Do not run any HTTP graph Tasks: no advisory listener can refresh its captured runtime.
	reader := f
	reader.runtime = httpRuntime
	query := "cross-process-captured-query"
	queryPath := "/api/v1/search?q=" + query + "&mode=semantic"
	reader.exchange(t, apiExchange{method: http.MethodGet, path: queryPath, status: 200}, nil)
	firstCount, relocatedCount := &atomic.Int64{}, &atomic.Int64{}
	first := modelQueryProvider(t, query, firstCount)
	relocated := modelQueryProvider(t, query, relocatedCount)
	model, err := f.store.ResolveModel(t.Context(), original)
	require.NoError(t, err)
	model.ID, model.Name, model.Endpoint, model.Dimensions = uuid.New(), "Independent role model", first.URL, 5
	require.NoError(t, f.store.EmbeddingModels().Create(t.Context(), model))
	cleanupModelCollections(t, f, []uuid.UUID{model.ID}, original)
	selected := patchModelHTTP(t, f, stored.Version, model.ID)
	awaitServingModel(t, f, model.ID)
	reader.exchange(t, apiExchange{method: http.MethodGet, path: queryPath, status: 200}, nil)
	require.EqualValues(t, 1, firstCount.Load(), "independent HTTP must use the worker's five-dimensional model")
	before, _, err := f.store.GetActive(t.Context(), f.workspaceID)
	require.NoError(t, err)
	model.Endpoint, model.Version = relocated.URL, model.Version+1
	changed, err := f.store.EmbeddingModels().Update(t.Context(), model, model.Version-1)
	require.NoError(t, err)
	require.True(t, changed)
	reader.exchange(t, apiExchange{method: http.MethodGet, path: queryPath, status: 200}, nil)
	require.EqualValues(t, 1, relocatedCount.Load(), "model registry version refreshes the endpoint without restarting HTTP")
	after, _, err := f.store.GetActive(t.Context(), f.workspaceID)
	require.NoError(t, err)
	require.Equal(t, before.CollectionIncarnation, after.CollectionIncarnation)
	require.Equal(t, before.SelectionGeneration, after.SelectionGeneration, "endpoint relocation must not request a new full seed")
	selected = patchModelHTTP(t, f, selected.Version, original)
	awaitServingModel(t, f, original)
	reader.exchange(t, apiExchange{method: http.MethodGet, path: queryPath, status: 200}, nil)
	require.EqualValues(t, 1, relocatedCount.Load(), "return to A must stop querying B's endpoint")
	assertQuerySnapshotAcrossCutover(t, reader, f, selected.Version)
}

func assertQuerySnapshotAcrossCutover(t *testing.T, reader, worker stableAPIFixture, version int) {
	t.Helper()
	entered, release := make(chan struct{}), make(chan struct{})
	var enterOnce, releaseOnce sync.Once
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		texts, err := fixtureEmbeddingInputs(r)
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		if slices.Contains(texts, "captured-in-flight") {
			enterOnce.Do(func() { close(entered) })
			select {
			case <-release:
			case <-r.Context().Done():
				return
			}
		}
		fixtureEmbeddingResponse(w, texts, []float32{1, 0, 0})
	}))
	t.Cleanup(provider.Close)
	t.Cleanup(func() { releaseOnce.Do(func() { close(release) }) })
	active, _, err := worker.store.GetActive(t.Context(), worker.workspaceID)
	require.NoError(t, err)
	model, err := worker.store.ResolveModel(t.Context(), active.ModelID)
	require.NoError(t, err)
	model.Endpoint, model.Version = provider.URL, model.Version+1
	_, err = worker.store.EmbeddingModels().Update(t.Context(), model, model.Version-1)
	require.NoError(t, err)
	models, err := worker.store.EmbeddingModels().List(t.Context())
	require.NoError(t, err)
	var target uuid.UUID
	for _, candidate := range models {
		if candidate.Dimensions == 5 {
			target = candidate.ID
		}
	}
	require.NotEqual(t, uuid.Nil, target)
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	response := httptest.NewRecorder()
	request := httptest.NewRequestWithContext(ctx, http.MethodGet, "/api/v1/search?q=captured-in-flight&mode=semantic", nil)
	done := make(chan struct{})
	go func() { reader.runtime.server.Handler.ServeHTTP(response, request); close(done) }()
	joined := false
	t.Cleanup(func() {
		releaseOnce.Do(func() { close(release) })
		cancel()
		if !joined {
			testutil.Receive(t, done)
		}
	})
	testutil.Receive(t, entered)
	patchModelHTTP(t, worker, version, target)
	awaitServingModel(t, worker, target)
	releaseOnce.Do(func() { close(release) })
	testutil.Receive(t, done)
	joined = true
	require.Equal(t, http.StatusOK, response.Code, "query must pair A's three-dimensional embedding with A's captured index: %s", response.Body.String())
}

func modelQueryProvider(t *testing.T, query string, count *atomic.Int64) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		texts, err := fixtureEmbeddingInputs(r)
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		if slices.Contains(texts, query) {
			count.Add(1)
		}
		fixtureEmbeddingResponse(w, texts, []float32{1, 0, 0, 0, 0})
	}))
	t.Cleanup(server.Close)
	return server
}

func modelPatchResponse(t *testing.T, f stableAPIFixture, version int, modelID uuid.UUID) *httptest.ResponseRecorder {
	t.Helper()
	body, err := json.Marshal(map[string]any{"embeddingModelId": modelID})
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()
	request := httptest.NewRequestWithContext(ctx, http.MethodPatch, "/api/v1/workspace-settings", bytes.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("If-Match", strconv.Quote(strconv.Itoa(version)))
	response := httptest.NewRecorder()
	f.runtime.server.Handler.ServeHTTP(response, request)
	return response
}

func patchModelHTTP(t *testing.T, f stableAPIFixture, version int, modelID uuid.UUID) (updated struct {
	Version          int       `json:"version"`
	EmbeddingModelId uuid.UUID `json:"embeddingModelId"`
}) {
	t.Helper()
	response := modelPatchResponse(t, f, version, modelID)
	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &updated))
	return updated
}

func cleanupModelCollections(t *testing.T, f stableAPIFixture, models []uuid.UUID, serving uuid.UUID) {
	t.Helper()
	t.Cleanup(func() {
		if f.stopWorkers != nil {
			f.stopWorkers()
		}
		client, err := vectorindex.NewClient(testutil.QdrantURL(t))
		require.NoError(t, err)
		defer func() { require.NoError(t, client.Close()) }()
		// Cancellation joins client workers; server-side collection teardown has its own I/O lifetime.
		// This cleanup budget is independent of the HTTP/indexing assertions above.
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		for _, id := range models {
			if id == serving {
				continue
			}
			exists, err := client.CollectionExists(ctx, vectorindex.CollectionName(id))
			require.NoError(t, err)
			if exists {
				started := time.Now()
				err := client.DeleteCollection(ctx, vectorindex.CollectionName(id))
				t.Logf("owned collection cleanup elapsed=%s error=%v", time.Since(started), err)
				require.NoError(t, err)
			}
		}
	})
}
