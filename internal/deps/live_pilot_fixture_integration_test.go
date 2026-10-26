//go:build integration && live

package deps

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	generated "github.com/ethernity/obsidian-topic-stream/gen/config"
	"github.com/ethernity/obsidian-topic-stream/internal/domain"
	"github.com/ethernity/obsidian-topic-stream/internal/platform/vectorindex"
	"github.com/ethernity/obsidian-topic-stream/internal/repository/postgres"
	"github.com/ethernity/obsidian-topic-stream/internal/service/retrieval"
	"github.com/ethernity/obsidian-topic-stream/internal/testutil"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/qdrant/go-client/qdrant"
	"github.com/stretchr/testify/require"
)

type livePilotFixture struct {
	runtime     *API
	worker      *API
	store       *postgres.Store
	conn        *pgx.Conn
	workspaceID uuid.UUID
	sources     []uuid.UUID
	recorder    *livePilotRecorder
	maintenance *retrieval.Maintenance
	client      *qdrant.Client
	items       int
	dataset     map[string]any
}

func newLivePilotFixture(t *testing.T, items int, recorder *livePilotRecorder) livePilotFixture {
	t.Helper()
	dsn := testutil.MigratedPostgresSchema(t)
	store, err := postgres.Open(t.Context(), dsn)
	require.NoError(t, err)
	t.Cleanup(store.Close)
	conn, err := pgx.Connect(t.Context(), dsn)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, conn.Close(context.Background())) })
	f := livePilotFixture{store: store, conn: conn, workspaceID: uuid.New(), recorder: recorder, items: items}
	require.NoError(t, store.EnsureWorkspace(t.Context(), f.workspaceID))
	for i := range 2 {
		id := uuid.New()
		f.sources = append(f.sources, id)
		require.NoError(t, store.Create(t.Context(), domain.Source{ID: id, WorkspaceID: f.workspaceID, Name: fmt.Sprintf("Pilot source %d", i), Enabled: true,
			Config: domain.SourceConfig{Kind: domain.SourceKindRSS, URL: fmt.Sprintf("https://pilot.example/feed/%d", i)}, PollIntervalMinutes: 60, CreatedAt: time.Now(), UpdatedAt: time.Now()}))
	}
	f.dataset = seedLivePilotItems(t, f)
	f.addTopics(t, 100)
	seedLivePilotPublications(t, f)
	endpoint := livePilotEmbeddingProxy(t, recorder)
	qdrantURL := os.Getenv("TOPIC_STREAM_TEST_QDRANT_URL")
	config := generated.Config{
		Bootstrap: generated.BootstrapConfig{BootstrapEmbeddingEndpoint: endpoint, BootstrapEmbeddingModel: livePilotModel(t), BootstrapEmbeddingDimensions: livePilotDimensions(t)},
		DBPrimary: generated.DBPrimaryConfig{Kind: "postgres", PostgresDSN: dsn}, HTTP: generated.HTTPConfig{Enabled: true, Address: "127.0.0.1:0"},
		Runtime: generated.RuntimeConfig{QdrantFullScanThresholdKb: 100000, QdrantHnswEf: 256, QdrantAcornEnabled: true, QdrantAcornMaxSelectivityPercent: 40, EmbeddingBatchSize: livePilotInt(t, "TOPIC_STREAM_PILOT_BATCH_SIZE", 8), EmbeddingBatchInputBytes: 1 << 20, EmbeddingConcurrency: livePilotInt(t, "TOPIC_STREAM_PILOT_CONCURRENCY", 1), WorkspaceId: f.workspaceID.String(), WorkersEnabled: true, SchedulersEnabled: false, QdrantUrl: qdrantURL, ShutdownTimeout: 5 * time.Second,
			WorkerFetchConcurrency: 1, WorkerFetchPerHostConcurrency: 1, WorkerGenerationConcurrency: 1, WorkerRetrievalConcurrency: 1, WorkerFeedPreparationConcurrency: 1, WorkerMaintenanceConcurrency: 1}}
	if os.Getenv("TOPIC_STREAM_PILOT_LIFECYCLE") == "true" {
		config.Runtime.WorkersEnabled = false
	}
	f.runtime, err = NewAPI(t.Context(), config, "live-retrieval-pilot")
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, f.runtime.Shutdown(context.Background())) })
	if os.Getenv("TOPIC_STREAM_PILOT_LIFECYCLE") == "true" {
		config.HTTP.Enabled = false
		config.Runtime.WorkersEnabled = true
		f.worker, err = NewAPI(t.Context(), config, "live-retrieval-worker")
		require.NoError(t, err)
		t.Cleanup(func() { require.NoError(t, f.worker.Shutdown(context.Background())) })
	}
	f.client, err = vectorindex.NewClient(qdrantURL)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, f.client.Close()) })
	queue := f.runtime.jobs
	if f.worker != nil {
		queue = f.worker.jobs
	}
	f.maintenance, err = retrieval.NewMaintenance(retrieval.MaintenanceConfig{WorkspaceID: f.workspaceID, Catalog: store, Queue: queue.ForWorkspace(f.workspaceID)})
	require.NoError(t, err)
	if os.Getenv("TOPIC_STREAM_CAPTURE_STATEMENTS") == "true" {
		_, err = conn.Exec(t.Context(), `CREATE EXTENSION IF NOT EXISTS pg_stat_statements`)
		require.NoError(t, err)
	}
	return f
}

func livePilotModel(t *testing.T) string {
	t.Helper()
	model := strings.TrimSpace(os.Getenv("TOPIC_STREAM_PILOT_MODEL"))
	require.NotEmpty(t, model, "explicit installed embedding model is required")
	return model
}

func livePilotDimensions(t *testing.T) int {
	t.Helper()
	dimensions := livePilotInt(t, "TOPIC_STREAM_PILOT_DIMENSIONS", 1024)
	require.Greater(t, dimensions, 0)
	require.LessOrEqual(t, dimensions, 65536)
	return dimensions
}

func livePilotInt(t *testing.T, name string, fallback int) int {
	t.Helper()
	value := os.Getenv(name)
	if value == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(value)
	require.NoError(t, err)
	return parsed
}

func seedLivePilotItems(t *testing.T, f livePilotFixture) map[string]any {
	t.Helper()
	var numbers []int32
	var titles, bodies []string
	var bytesTotal, runesTotal int
	lengths := make(map[int]int)
	for n := 1; n <= f.items; n++ {
		bucket := (n - 1) % 20
		if f.items == 3 {
			bucket = []int{0, 10, 19}[n-1]
		}
		length := 512
		if bucket >= 10 {
			length = 4096
		}
		if bucket == 19 {
			length = 16384
		}
		text := "PostgreSQL indexes, database transactions and distributed systems require consistent snapshots. Engineering teams measure latency and throughput before changing query plans. "
		if n%3 == 0 {
			text = "PostgreSQL: индексы и транзакции помогают строить распределённые системы. Инженеры измеряют задержки, проверяют согласованность данных и производительность SQL-запросов. "
		}
		runes := []rune(text)
		body := string([]rune(strings.Repeat(text, length/len(runes)+1))[:length])
		title := fmt.Sprintf("Pilot item %05d: PostgreSQL distributed systems", n)
		numbers = append(numbers, int32(n))
		titles = append(titles, title)
		bodies = append(bodies, body)
		lengths[length]++
		bytesTotal += len(title) + 2 + len(body)
		runesTotal += len([]rune(title)) + 2 + len([]rune(body))
	}
	_, err := f.conn.Exec(t.Context(), `CREATE UNLOGGED TABLE pilot_seed AS
 SELECT n,md5('pilot:item:'||n)::uuid AS id,title,body FROM unnest($1::integer[],$2::text[],$3::text[]) AS input(n,title,body)`, numbers, titles, bodies)
	require.NoError(t, err)
	_, err = f.conn.Exec(t.Context(), `INSERT INTO items(id,workspace_id,details,title,url,canonical_url,excerpt,content_markdown,published_at,discovered_at)
 SELECT id,$1,'{"type":"article","article":{"description":"Controlled benchmark text"}}',title,'https://pilot.example/item/'||n,'https://pilot.example/item/'||n,
 'PostgreSQL systems benchmark',body,clock_timestamp()-(n%30)*interval '1 day',clock_timestamp()-(n%30)*interval '1 day' FROM pilot_seed`, f.workspaceID)
	require.NoError(t, err)
	_, err = f.conn.Exec(t.Context(), `INSERT INTO item_projection_state(workspace_id,item_id,revision,updated_at)
 SELECT $1,id,id,clock_timestamp() FROM pilot_seed`, f.workspaceID)
	require.NoError(t, err)
	_, err = f.conn.Exec(t.Context(), `INSERT INTO source_entries(source_id,external_id,item_id,url,details,discovered_at,updated_at)
 SELECT CASE WHEN n%2=0 THEN $1::uuid ELSE $2::uuid END,n::text,id,'https://pilot.example/item/'||n,
 '{"type":"article","article":{"description":"Controlled benchmark source"}}',clock_timestamp(),clock_timestamp() FROM pilot_seed`, f.sources[0], f.sources[1])
	require.NoError(t, err)
	return map[string]any{"Items": f.items, "BodyRuneCounts": lengths, "TotalEmbeddingInputBytes": bytesTotal, "TotalEmbeddingInputRunes": runesTotal,
		"Preparation": "Item.RetrievalText: title, two newlines, complete canonical Markdown; no truncation", "Content": "controlled synthetic English/Russian articles, unique titles, two Sources; not relevance-quality or production-vocabulary evidence"}
}

func (f livePilotFixture) addTopics(t *testing.T, count int) {
	t.Helper()
	_, err := f.conn.Exec(t.Context(), `INSERT INTO topics(id,workspace_id,name,include_text)
 SELECT md5('pilot:topic:'||n)::uuid,$1,'Pilot topic '||n,'' FROM generate_series(1,$2::integer) n ON CONFLICT DO NOTHING`, f.workspaceID, count)
	require.NoError(t, err)
}

func seedLivePilotPublications(t *testing.T, f livePilotFixture) {
	t.Helper()
	_, err := f.conn.Exec(t.Context(), `INSERT INTO feed_publications(workspace_id,item_id,batch_id,published_at,ordinal,matched_topic_ids,score,pipeline,model_id)
 SELECT $1,id,$2,clock_timestamp(),n,ARRAY[md5('pilot:topic:'||(1+n%100))::uuid],0.8,'controlled-pilot',$3 FROM pilot_seed`, f.workspaceID, uuid.New(), domain.DefaultEmbeddingModelID)
	// The model FK is intentionally not required by publication facts; model registration follows in NewAPI.
	require.NoError(t, err)
	_, err = f.conn.Exec(t.Context(), `INSERT INTO feed_display_entries(workspace_id,item_id,ordinal,topic_ids)
 SELECT workspace_id,item_id,ordinal,matched_topic_ids FROM feed_publications WHERE workspace_id=$1`, f.workspaceID)
	require.NoError(t, err)
	_, err = f.conn.Exec(t.Context(), `INSERT INTO feed_state(workspace_id,head_ordinal) VALUES($1,$2)`, f.workspaceID, f.items)
	require.NoError(t, err)
}

func (f livePilotFixture) startWorkers(t *testing.T) func() {
	t.Helper()
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	worker := f.runtime
	if f.worker != nil {
		worker = f.worker
	}
	go func() { done <- worker.jobs.Run(ctx) }()
	var once sync.Once
	stop := func() { once.Do(func() { cancel(); require.NoError(t, testutil.Receive(t, done)) }) }
	t.Cleanup(stop)
	return stop
}

func (f livePilotFixture) waitRebuild(t *testing.T, jobID uuid.UUID) map[string]any {
	t.Helper()
	return f.waitRebuildCohort(t, jobID, f.items)
}

func (f livePilotFixture) waitRebuildCohort(t *testing.T, jobID uuid.UUID, total int) map[string]any {
	t.Helper()
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()
	progress := time.NewTicker(10 * time.Second)
	defer progress.Stop()
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Minute)
	defer cancel()
	var observations []map[string]any
	var progressReadMs []float64
	previous := ""
	for {
		readStarted := time.Now()
		job, found, err := f.store.Jobs().Get(ctx, jobID)
		progressReadMs = append(progressReadMs, float64(time.Since(readStarted))/float64(time.Millisecond))
		require.NoError(t, err)
		require.True(t, found)
		if job.Progress != nil {
			total := int64(-1)
			if job.Progress.TotalItems != nil {
				total = *job.Progress.TotalItems
			}
			key := fmt.Sprintf("%s/%s/%d/%d", job.Status, job.Progress.Phase, job.Progress.ProcessedItems, total)
			if key != previous {
				observations = append(observations, map[string]any{"ObservedAt": time.Now().UTC(), "Status": job.Status, "Progress": job.Progress})
				previous = key
			}
		}
		if job.Status == domain.JobStatusSucceeded {
			require.NotNil(t, job.Progress)
			require.Equal(t, "completed", job.Progress.Phase)
			require.NotNil(t, job.Progress.TotalItems)
			require.EqualValues(t, total, *job.Progress.TotalItems)
			require.EqualValues(t, total, job.Progress.ProcessedItems)
			return map[string]any{"JobID": job.ID, "StartedAt": job.StartedAt, "FinishedAt": job.FinishedAt, "Attempt": job.Attempt, "Progress": job.Progress, "ProgressObservations": observations, "ProgressReadMs": progressReadMs, "ProgressPollIntervalMs": 500}
		}
		require.NotContains(t, []domain.JobStatus{domain.JobStatusDead, domain.JobStatusCancelled, domain.JobStatusRetryWait}, job.Status, "pilot errors remain visible: %s", job.Reason)
		select {
		case <-ctx.Done():
			require.NoError(t, ctx.Err())
		case <-progress.C:
			var pending int
			require.NoError(t, f.conn.QueryRow(ctx, `SELECT count(*) FROM retrieval_projection_pending WHERE workspace_id=$1`, f.workspaceID).Scan(&pending))
			t.Logf("live pilot pending=%d/%d", pending, f.items)
		case <-ticker.C:
		}
	}
}

func (f livePilotFixture) assertComplete(t *testing.T) {
	t.Helper()
	var pending int
	require.NoError(t, f.conn.QueryRow(t.Context(), `SELECT count(*) FROM retrieval_projection_pending WHERE workspace_id=$1`, f.workspaceID).Scan(&pending))
	require.Zero(t, pending)
	active, found, err := f.store.GetActive(t.Context(), f.workspaceID)
	require.NoError(t, err)
	require.True(t, found)
	require.True(t, active.Initialized)
	count, err := f.client.Count(t.Context(), &qdrant.CountPoints{CollectionName: active.Collection, Exact: qdrant.PtrOf(true)})
	require.NoError(t, err)
	require.EqualValues(t, f.items, count, "a complete pilot Workspace has all real index points")
}

func (f livePilotFixture) qdrantMetrics(t *testing.T, label string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, os.Getenv("TOPIC_STREAM_PILOT_QDRANT_HTTP")+"/metrics", nil)
	require.NoError(t, err)
	response, err := http.DefaultClient.Do(request)
	require.NoError(t, err)
	defer func() { _ = response.Body.Close() }()
	require.Equal(t, http.StatusOK, response.StatusCode)
	body, err := io.ReadAll(response.Body)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(filepath.Dir(os.Getenv("TOPIC_STREAM_PERF_REPORT")), label+"-qdrant.prom"), body, 0o600))
}
