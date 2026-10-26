//go:build integration

package feed_test

import (
	"context"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ethernity/obsidian-topic-stream/internal/domain"
	"github.com/ethernity/obsidian-topic-stream/internal/platform/vectorindex"
	"github.com/ethernity/obsidian-topic-stream/internal/repository/postgres"
	"github.com/ethernity/obsidian-topic-stream/internal/service/feed"
	"github.com/ethernity/obsidian-topic-stream/internal/service/ranking"
	"github.com/ethernity/obsidian-topic-stream/internal/service/settings"
	"github.com/ethernity/obsidian-topic-stream/internal/testutil"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/qdrant/go-client/qdrant"
	"github.com/stretchr/testify/require"
)

func TestMain(m *testing.M) {
	os.Exit(testutil.RunIntegration(m, testutil.IntegrationConfig{Postgres: true, Qdrant: true}))
}

type countedQueries struct {
	*qdrant.Client
	queries    atomic.Int64
	batches    atomic.Int64
	subqueries atomic.Int64
}

// Query counts single queries while delegating to the real Qdrant client.
func (b *countedQueries) Query(ctx context.Context, q *qdrant.QueryPoints) ([]*qdrant.ScoredPoint, error) {
	b.queries.Add(1)
	return b.Client.Query(ctx, q)
}

// QueryBatch counts batches and subqueries while delegating to the real Qdrant client.
func (b *countedQueries) QueryBatch(ctx context.Context, q *qdrant.QueryBatchPoints) ([]*qdrant.BatchResult, error) {
	b.batches.Add(1)
	b.subqueries.Add(int64(len(q.QueryPoints)))
	return b.Client.QueryBatch(ctx, q)
}

type livePersonalFactory struct{ index *vectorindex.Index }

// BuildPersonalIndex returns the fixture's personal index without opening resources.
func (f livePersonalFactory) BuildPersonalIndex(domain.RetrievalIndex) (ranking.PersonalIndex, error) {
	return f.index, nil
}

type sharedFeedFixture struct {
	store                                *postgres.Store
	service                              feed.Service
	workspaceID, firstTopic, secondTopic uuid.UUID
	ids                                  []uuid.UUID
	backend                              *countedQueries
	now                                  time.Time
}

func newSharedFeedFixture(t *testing.T) sharedFeedFixture {
	t.Helper()
	dsn := testutil.MigratedPostgresSchema(t)
	store, err := postgres.Open(t.Context(), dsn)
	require.NoError(t, err)
	t.Cleanup(store.Close)
	client, err := vectorindex.NewClient(testutil.QdrantURL(t))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, client.Close()) })
	f := sharedFeedFixture{store: store, workspaceID: uuid.New(), firstTopic: uuid.MustParse("00000000-0000-4000-8000-000000000001"), secondTopic: uuid.MustParse("00000000-0000-4000-8000-000000000002"), now: time.Now().UTC().Truncate(time.Microsecond)}
	old := f.now.Add(-2 * time.Minute)
	require.NoError(t, store.EnsureWorkspace(t.Context(), f.workspaceID))
	model := domain.EmbeddingModel{ID: uuid.New(), EmbeddingModelDescriptor: domain.EmbeddingModelDescriptor{Name: "Synthetic shared feed", Endpoint: "http://fixture.example/v1", ProviderModel: "synthetic", Dimensions: 3}, Enabled: true, InitializationStatus: domain.EmbeddingModelReady, Version: 1, CreatedAt: old, UpdatedAt: old}
	require.NoError(t, store.EmbeddingModels().Create(t.Context(), model))
	require.NoError(t, store.EmbeddingModels().EnsureWorkspaceState(t.Context(), f.workspaceID, model.ID, old))
	cfg := settings.DefaultWorkspaceSettings(old)
	cfg.EmbeddingModelID = model.ID
	cfg.Retrieval.CandidateLimit = 2
	_, err = store.GetOrCreateWorkspaceSettings(t.Context(), f.workspaceID, cfg)
	require.NoError(t, err)
	collection := vectorindex.CollectionName(model.ID)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		require.NoError(t, client.DeleteCollection(ctx, collection))
	})
	f.backend = &countedQueries{Client: client}
	index, err := vectorindex.New(f.backend, collection, 3)
	require.NoError(t, err)
	require.NoError(t, index.Ensure(t.Context()))
	sourceID := uuid.New()
	require.NoError(t, store.Create(t.Context(), domain.Source{ID: sourceID, WorkspaceID: f.workspaceID, Name: "Synthetic", Config: domain.SourceConfig{Kind: domain.SourceKindRSS, URL: "https://fixture.example/feed"}, Enabled: true, PollIntervalMinutes: 60, CreatedAt: old, UpdatedAt: old}))
	for _, id := range []uuid.UUID{f.firstTopic, f.secondTopic} {
		require.NoError(t, store.CreateTopic(t.Context(), domain.Topic{ID: id, WorkspaceID: f.workspaceID, Name: id.String(), Mode: domain.TopicModeOrdinary, Enabled: true, Revision: 1, CreatedAt: old, UpdatedAt: old}))
	}
	for range 3 {
		id := uuid.New()
		url := "https://fixture.example/" + id.String()
		_, _, err := store.UpsertItem(t.Context(), domain.Item{ID: id, WorkspaceID: f.workspaceID, Kind: domain.ItemKindArticle, Title: "Synthetic article", URL: url, CanonicalURL: url, DiscoveredAt: old, Details: domain.ItemDetails{Type: domain.ItemKindArticle, Article: &domain.ArticleDetails{Description: "Fixture"}}}, domain.SourceEntry{SourceID: sourceID, ExternalID: id.String(), URL: url, DiscoveredAt: old, UpdatedAt: old, Details: domain.SourceEntryDetails{Type: domain.ItemKindArticle, Article: &domain.SourceArticleDetails{Description: "Fixture"}}})
		require.NoError(t, err)
		work, found, err := store.LoadItemProjection(t.Context(), f.workspaceID, id)
		require.NoError(t, err)
		require.True(t, found)
		require.NoError(t, index.UpsertProjection(t.Context(), work.Projection, []float32{1, 0, 0}))
		_, err = store.CompleteItemProjection(t.Context(), domain.ItemProjectionDelivery{WorkspaceID: f.workspaceID, ItemID: id, ModelID: model.ID, Revision: work.Projection.Revision})
		require.NoError(t, err)
		f.ids = append(f.ids, id)
	}
	conn, err := pgx.Connect(t.Context(), dsn)
	require.NoError(t, err)
	defer func() { require.NoError(t, conn.Close(context.Background())) }()
	jobID := uuid.New()
	_, err = conn.Exec(t.Context(), `INSERT INTO jobs(id,workspace_id,kind,queue,status,lease_owner,lease_expires_at) VALUES($1,$2,'prepare-feed','feed-preparation','running','fixture',$3)`, jobID, f.workspaceID, f.now.Add(time.Hour))
	require.NoError(t, err)
	work, err := store.LoadFeedPreparationWork(t.Context(), domain.FeedPreparationQuery{WorkspaceID: f.workspaceID, At: f.now, Limit: 8})
	require.NoError(t, err)
	require.Len(t, work, 2)
	for _, topic := range work {
		ids := []uuid.UUID{f.ids[0], f.ids[1]}
		if topic.TopicID == f.secondTopic {
			ids[1] = f.ids[2]
		}
		rows := []domain.FeedCandidate{{ItemID: ids[0], Score: 1}, {ItemID: ids[1], Score: 0.8}}
		inputs := domain.FeedCandidateInputs{TopicRevision: 1, CorrectionEpoch: 1, CandidateLimit: 2, RRFK: 60, ModelID: model.ID, GateVersion: "fixture-gate"}
		require.NoError(t, store.ReplaceFeedCandidates(t.Context(), domain.FeedCandidateReplacement{Work: topic, Inputs: inputs, Candidates: rows, JobID: jobID, LeaseOwner: "fixture", At: f.now}))
	}
	f.service = feed.New(feed.Config{History: store, Admission: store, Topics: store, Settings: settings.NewWithCache(settings.ServiceConfig{WorkspaceID: f.workspaceID, Repository: store, Cache: settings.NewCache(store, func() time.Time { return f.now }), Now: func() time.Time { return f.now }}), Ranker: ranking.NewFeedRanker(livePersonalFactory{index: index}), WorkspaceID: f.workspaceID, Now: func() time.Time { return f.now }, GateVersion: "fixture-gate", SelectionStart: func(int) int { return 0 }})
	return f
}

func TestSharedRefreshUsesOnePersonalBatchAndKeepsOneCommonHistory(t *testing.T) {
	f := newSharedFeedFixture(t)
	all, err := f.service.Refresh(t.Context(), domain.FeedRefreshCommand{})
	require.NoError(t, err)
	require.Len(t, all.Items, 2)
	require.Equal(t, f.ids[0], all.Items[0].Item.ID)
	require.Equal(t, f.ids[2], all.Items[1].Item.ID)
	require.Equal(t, int64(1), f.backend.batches.Load())
	require.Equal(t, int64(4), f.backend.subqueries.Load())
	require.Zero(t, f.backend.queries.Load(), "Refresh does not retrieve Topics")
	first, err := f.service.Refresh(t.Context(), domain.FeedRefreshCommand{TopicID: &f.firstTopic})
	require.NoError(t, err)
	require.Len(t, first.Items, 2)
	require.Equal(t, f.ids[1], first.Items[0].Item.ID)
	require.Equal(t, int64(2), f.backend.batches.Load())
	require.Equal(t, int64(6), f.backend.subqueries.Load())
	second, err := f.service.Refresh(t.Context(), domain.FeedRefreshCommand{TopicID: &f.secondTopic})
	require.NoError(t, err)
	require.Len(t, second.Items, 2)
	require.Equal(t, int64(2), f.backend.batches.Load(), "empty admission does not rank again")
	saved, err := f.service.Read(t.Context(), domain.FeedReadQuery{})
	require.NoError(t, err)
	require.Len(t, saved.Items, 3)
	require.Equal(t, []uuid.UUID{f.ids[1], f.ids[0], f.ids[2]}, []uuid.UUID{saved.Items[0].Item.ID, saved.Items[1].Item.ID, saved.Items[2].Item.ID})
	require.Equal(t, int64(2), f.backend.batches.Load(), "GET reads PostgreSQL only")
}

func TestConcurrentAllAndTopicRefreshPublishEachItemAtMostOnce(t *testing.T) {
	f := newSharedFeedFixture(t)
	type result struct {
		page domain.FeedPage
		err  error
	}
	results := make(chan result, 2)
	start := make(chan struct{})
	for _, topicID := range []*uuid.UUID{nil, &f.firstTopic} {
		go func() {
			<-start
			page, err := f.service.Refresh(t.Context(), domain.FeedRefreshCommand{TopicID: topicID})
			results <- result{page, err}
		}()
	}
	close(start)
	for range 2 {
		value := testutil.Receive(t, results)
		require.NoError(t, value.err)
	}
	head, err := f.store.FeedHead(t.Context(), f.workspaceID)
	require.NoError(t, err)
	require.Equal(t, int64(3), head)
	saved, err := f.service.Read(t.Context(), domain.FeedReadQuery{})
	require.NoError(t, err)
	require.Len(t, saved.Items, 3)
	require.Zero(t, f.backend.queries.Load())
	require.LessOrEqual(t, f.backend.subqueries.Load(), int64(8))
}

type changeAfterRanking struct {
	ranker feed.PersonalRanker
	change func(context.Context) error
}

// RankFeed runs the real ranker, then changes durable state before publication to exercise the commit
// fence.
func (r changeAfterRanking) RankFeed(ctx context.Context, request domain.FeedRankingRequest) ([]domain.FeedCandidate, error) {
	result, err := r.ranker.RankFeed(ctx, request)
	if err != nil {
		return nil, err
	}
	return result, r.change(ctx)
}

func TestRefreshDropsNewlyHiddenCandidateAtCommitWithoutRefill(t *testing.T) {
	f := newSharedFeedFixture(t)
	generation, found, err := f.store.GetActive(t.Context(), f.workspaceID)
	require.NoError(t, err)
	require.True(t, found)
	index, err := vectorindex.New(f.backend, generation.Collection, 3)
	require.NoError(t, err)
	ranker := changeAfterRanking{ranker: ranking.NewFeedRanker(livePersonalFactory{index: index}), change: func(ctx context.Context) error {
		_, err := f.store.SetVisibility(ctx, f.workspaceID, f.ids[0], domain.ItemVisibilityHidden)
		return err
	}}
	service := feed.New(feed.Config{History: f.store, Admission: f.store, Topics: f.store, Settings: settings.NewWithCache(settings.ServiceConfig{WorkspaceID: f.workspaceID, Repository: f.store, Cache: settings.NewCache(f.store, func() time.Time { return f.now }), Now: func() time.Time { return f.now }}), Ranker: ranker, WorkspaceID: f.workspaceID, Now: func() time.Time { return f.now }, GateVersion: "fixture-gate", SelectionStart: func(int) int { return 0 }})
	page, err := service.Refresh(t.Context(), domain.FeedRefreshCommand{})
	require.NoError(t, err)
	require.Len(t, page.Items, 1)
	require.Equal(t, f.ids[2], page.Items[0].Item.ID)
	require.Equal(t, int64(1), f.backend.batches.Load())
	require.Equal(t, int64(4), f.backend.subqueries.Load())
	head, err := f.store.FeedHead(t.Context(), f.workspaceID)
	require.NoError(t, err)
	require.Equal(t, int64(1), head)
}
