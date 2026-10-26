//go:build integration && live

package deps

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"github.com/devctllabs/go-libs/di"
	"github.com/ethernity/obsidian-topic-stream/internal/domain"
	"github.com/ethernity/obsidian-topic-stream/internal/service/ranking"
	"github.com/ethernity/obsidian-topic-stream/internal/service/retrieval"
	"github.com/ethernity/obsidian-topic-stream/internal/testutil"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

type liveLifecycle struct {
	livePilotFixture
	server   *httptest.Server
	sequence atomic.Int64
	report   map[string]any
}

func TestLiveRetrievalLifecycleCapacity(t *testing.T) {
	testutil.RequireOwnedQdrant(t)
	require.Equal(t, "true", os.Getenv("TOPIC_STREAM_PILOT_LIFECYCLE"), "use the explicit live lifecycle profile")
	require.NotEmpty(t, os.Getenv("TOPIC_STREAM_PILOT_OLLAMA"), "explicit live embedding endpoint is required")
	items := livePilotInt(t, "TOPIC_STREAM_PILOT_ITEMS", 500)
	require.Contains(t, []int{3, 500}, items)
	recorder := newLivePilotRecorder(t)
	l := liveLifecycle{livePilotFixture: newLivePilotFixture(t, items, recorder), report: map[string]any{
		"Status": "in_progress", "Items": items, "Dimensions": livePilotDimensions(t), "Model": livePilotModel(t),
		"Scope":           "real-provider 500-Item bounded lifecycle; separate HTTP-only and worker-only DI graphs; same process, separate resources/caches, no HTTP advisory tasks; no 5k or million-provider run",
		"ModelIdentity":   "A and B are distinct registry identities/collections with identical installed embedding weights; validates lifecycle costs, not model quality differences",
		"Instrumentation": "thin HTTP provider/handler observer; request timing includes one local forwarding hop. Direct production Qdrant/SQL clients; no wire/RPC attribution or pure server-time claim",
	}}
	l.report["Dataset"] = l.dataset
	l.server = httptest.NewServer(livePilotHTTPObserver(livePilotSearchHandler(t, l.livePilotFixture), recorder))
	t.Cleanup(l.server.Close)
	l.save(t)
	recorder.setPhase("initial_A")
	started := time.Now()
	job, err := l.maintenance.Rebuild(t.Context())
	require.NoError(t, err)
	stopWorkers := l.startWorkers(t)
	l.complete(t, "initial_A", lifecycleCompletion{job: job.ID, total: items, inputs: items, started: started})
	before := l.publicationFacts(t)
	a := domain.DefaultEmbeddingModelID
	modelB, err := l.store.ResolveModel(t.Context(), a)
	require.NoError(t, err)
	modelB.ID, modelB.Name = uuid.New(), "Lifecycle B, same installed weights"
	require.NoError(t, l.store.EmbeddingModels().Create(t.Context(), modelB))
	l.report["RegistryModels"] = map[string]uuid.UUID{"A": a, "B": modelB.ID}

	recorder.setPhase("A_to_B_with_search")
	started = time.Now()
	id := l.selectModel(t, modelB.ID)
	l.searchDuringTransition(t, id)
	l.complete(t, "A_to_B_with_search", lifecycleCompletion{job: id, total: items, inputs: items, started: started})
	l.transition(t, "unchanged_B_to_A", a, 0)
	l.transition(t, "unchanged_A_to_B", modelB.ID, 0)
	l.changeWhileRetained(t, a, modelB.ID)
	delta := min(100, items)
	l.transition(t, "delta_B_to_A", a, delta)
	l.recoverCollection(t)
	require.Equal(t, before, l.publicationFacts(t), "canonical publication history survives changes and collection recovery")
	stopWorkers()
	assertPilotReadSLO(t, l.recorder.snapshot())
	l.report["Status"] = "complete"
	l.save(t)
}

type lifecycleCompletion struct {
	job     uuid.UUID
	total   int
	inputs  int
	started time.Time
}

func (l *liveLifecycle) save(t *testing.T) {
	t.Helper()
	writeLivePilotReport(t, l.report, l.recorder)
}

func (l *liveLifecycle) complete(t *testing.T, label string, expected lifecycleCompletion) {
	t.Helper()
	result := l.waitRebuildCohort(t, expected.job, expected.total)
	result["WallMs"] = float64(time.Since(expected.started)) / float64(time.Millisecond)
	inputs, calls := l.documentCalls(label)
	result["DocumentInputs"], result["DocumentRPCs"] = inputs, calls
	l.report[label] = result
	l.save(t)
	require.Equal(t, expected.inputs, inputs, "phase=%s actual provider inputs", label)
	require.EqualValues(t, 1, result["Attempt"], "phase=%s hidden retries are not acceptable", label)
	l.assertComplete(t)
	t.Logf("lifecycle phase=%s wall_ms=%.3f document_inputs=%d rpc=%d", label, result["WallMs"], inputs, calls)
}

func (l *liveLifecycle) documentCalls(phase string) (inputs, calls int) {
	for _, sample := range l.recorder.snapshot() {
		if sample.Phase == phase && sample.Kind == "embedding" && sample.Operation == "document" {
			inputs += sample.Inputs
			calls++
		}
	}
	return inputs, calls
}

func (l *liveLifecycle) selectModel(t *testing.T, modelID uuid.UUID) uuid.UUID {
	t.Helper()
	settings, _, err := l.store.GetWorkspaceSettings(t.Context(), l.workspaceID)
	require.NoError(t, err)
	patchModelHTTP(t, stableAPIFixture{runtime: l.runtime, store: l.store, workspaceID: l.workspaceID}, settings.Version, modelID)
	return l.currentJob(t, modelID)
}

func (l *liveLifecycle) currentJob(t *testing.T, modelID uuid.UUID) uuid.UUID {
	t.Helper()
	active, _, err := l.store.GetActive(t.Context(), l.workspaceID)
	require.NoError(t, err)
	key := fmt.Sprintf("reindex-workspace:%s:%d", modelID, active.SelectionGeneration)
	var id uuid.UUID
	require.NoError(t, l.conn.QueryRow(t.Context(), `SELECT id FROM jobs WHERE workspace_id=$1 AND idempotency_key=$2 ORDER BY created_at DESC LIMIT 1`, l.workspaceID, key).Scan(&id))
	return id
}

func (l *liveLifecycle) transition(t *testing.T, label string, modelID uuid.UUID, delta int) {
	t.Helper()
	l.recorder.setPhase(label)
	started := time.Now()
	id := l.selectModel(t, modelID)
	l.complete(t, label, lifecycleCompletion{job: id, total: delta, inputs: delta, started: started})
}

func (l *liveLifecycle) searchDuringTransition(t *testing.T, jobID uuid.UUID) {
	t.Helper()
	client := livePilotHTTPClient{base: l.server.URL, recorder: l.recorder, sequence: &l.sequence, client: &http.Client{Timeout: time.Minute}}
	stopReads := livePilotReadLoad(t, l.server.URL, l.recorder, &l.sequence)
	defer stopReads()
	deadline := time.Now().Add(20 * time.Minute)
	topics := 100
	var observations []map[string]any
	for n := 0; time.Now().Before(deadline); n++ {
		job, found, err := l.store.Jobs().Get(t.Context(), jobID)
		require.NoError(t, err)
		require.True(t, found)
		observations = append(observations, map[string]any{"At": time.Now().UTC(), "Status": job.Status, "Progress": job.Progress})
		require.NotContains(t, []domain.JobStatus{domain.JobStatusDead, domain.JobStatusRetryWait, domain.JobStatusCancelled}, job.Status, job.Reason)
		if job.Progress != nil && job.Progress.ProcessedItems >= int64(l.items/2) && topics == 100 {
			l.addTopics(t, 500)
			topics = 500
		}
		values := lifecycleSearchValues(l.livePilotFixture, n)
		client.search(t, values, fmt.Sprintf("%d/%s/%s", topics, values.Get("mode"), []string{"all", "source", "date", "topic"}[n%4]))
		if job.Status == domain.JobStatusSucceeded {
			l.report["TransitionSearchProgress"] = observations
			return
		}
	}
	t.Fatal("transition/search exceeded bounded diagnostic time")
}

func lifecycleSearchValues(f livePilotFixture, n int) url.Values {
	values := url.Values{"q": {"PostgreSQL distributed systems"}, "mode": {[]string{"lexical", "semantic", "hybrid"}[n/4%3]}, "limit": {"20"}}
	switch n % 4 {
	case 1:
		values.Set("sourceId", f.sources[0].String())
	case 2:
		values.Set("publishedAfter", time.Now().UTC().Add(-15*24*time.Hour).Format(time.RFC3339))
	case 3:
		values.Set("topicId", livePilotTopicID())
	}
	return values
}

func (l *liveLifecycle) changeWhileRetained(t *testing.T, inactive, serving uuid.UUID) {
	t.Helper()
	l.recorder.setPhase("delta_while_A_inactive")
	started := time.Now()
	for n := 1; n <= min(100, l.items); n++ {
		var itemID uuid.UUID
		require.NoError(t, l.conn.QueryRow(t.Context(), `SELECT id FROM pilot_seed WHERE n=$1`, n).Scan(&itemID))
		item, found, err := l.store.GetItem(t.Context(), l.workspaceID, itemID)
		require.NoError(t, err)
		require.True(t, found)
		item.ContentMarkdown += "\n\nChanged while model A was retained."
		_, changed, err := l.store.UpsertItem(t.Context(), item, domain.SourceEntry{
			SourceID: l.sources[n%2], ExternalID: strconv.Itoa(n), ItemID: item.ID, URL: item.URL,
			Details: domain.SourceEntryDetails{Type: domain.ItemKindArticle, Article: &domain.SourceArticleDetails{}}, DiscoveredAt: item.DiscoveredAt, UpdatedAt: time.Now(),
		})
		require.NoError(t, err)
		require.True(t, changed)
	}
	require.NoError(t, l.worker.projectionReconciler.Reconcile(t.Context()))
	require.Eventually(t, func() bool {
		var pending int
		err := l.conn.QueryRow(t.Context(), `SELECT count(*) FROM retrieval_projection_pending WHERE workspace_id=$1 AND embedding_model_id=$2`, l.workspaceID, serving).Scan(&pending)
		return err == nil && pending == 0
	}, 5*time.Minute, 100*time.Millisecond)
	var pending int
	require.NoError(t, l.conn.QueryRow(t.Context(), `SELECT count(*) FROM retrieval_projection_pending WHERE workspace_id=$1 AND embedding_model_id=$2`, l.workspaceID, inactive).Scan(&pending))
	require.Equal(t, min(100, l.items), pending)
	inputs, calls := l.documentCalls("delta_while_A_inactive")
	l.report["delta_while_A_inactive"] = map[string]any{"WallMs": float64(time.Since(started)) / float64(time.Millisecond), "InactivePending": pending, "DocumentInputs": inputs, "DocumentRPCs": calls}
	l.save(t)
	require.Equal(t, min(100, l.items), inputs, "only the Serving model receives inactive-period delivery")
}

func (l *liveLifecycle) recoverCollection(t *testing.T) {
	t.Helper()
	l.recorder.setPhase("selected_collection_loss")
	started := time.Now()
	before, _, err := l.store.GetActive(t.Context(), l.workspaceID)
	require.NoError(t, err)
	require.NoError(t, l.client.DeleteCollection(t.Context(), before.Collection))
	manager, err := di.Resolve[*retrieval.Manager](l.runtime.container)
	require.NoError(t, err)
	_, err = manager.QueryRecent(t.Context(), ranking.IndexFilter{WorkspaceID: l.workspaceID}, 10)
	require.Error(t, err, "a replaced empty collection must not be advertised as a ready index")
	require.NoError(t, l.worker.modelReconciler.Reconcile(t.Context()))
	id := l.currentJob(t, before.ModelID)
	l.complete(t, "selected_collection_loss", lifecycleCompletion{job: id, total: l.items, inputs: l.items, started: started})
	after, _, err := l.store.GetActive(t.Context(), l.workspaceID)
	require.NoError(t, err)
	require.NotEqual(t, before.CollectionIncarnation, after.CollectionIncarnation)
	l.report["CollectionIncarnations"] = map[string]uuid.UUID{"Before": before.CollectionIncarnation, "After": after.CollectionIncarnation}
	l.save(t)
}

func (l *liveLifecycle) publicationFacts(t *testing.T) string {
	t.Helper()
	var facts string
	require.NoError(t, l.conn.QueryRow(t.Context(), `SELECT COALESCE(jsonb_agg(to_jsonb(p) ORDER BY ordinal),'[]'::jsonb)::text FROM feed_publications p WHERE workspace_id=$1`, l.workspaceID).Scan(&facts))
	return facts
}
