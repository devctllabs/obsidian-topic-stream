//go:build integration

package projection_test

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/ethernity/obsidian-topic-stream/internal/domain"
	"github.com/ethernity/obsidian-topic-stream/internal/platform/vectorindex"
	"github.com/ethernity/obsidian-topic-stream/internal/repository/postgres"
	"github.com/ethernity/obsidian-topic-stream/internal/service/projection"
	"github.com/ethernity/obsidian-topic-stream/internal/service/ranking"
	"github.com/ethernity/obsidian-topic-stream/internal/testutil"
	"github.com/google/uuid"
	"github.com/qdrant/go-client/qdrant"
	"github.com/stretchr/testify/require"
)

func TestMain(m *testing.M) {
	os.Exit(testutil.RunIntegration(m, testutil.IntegrationConfig{Postgres: true, Qdrant: true}))
}

type deliveryFactory struct {
	client   *qdrant.Client
	embedder *embedderStub
}

// Build returns model-specific fixture dependencies without acquiring production resources.
func (f deliveryFactory) Build(model domain.EmbeddingModel) (projection.Embedder, projection.Writer, error) {
	index, err := vectorindex.New(f.client, vectorindex.CollectionName(model.ID), model.Dimensions)
	return f.embedder, index, err
}

func TestCanonicalDeliveryRepairsPublicationLagAndRebuildsMarkers(t *testing.T) {
	store, err := postgres.Open(t.Context(), testutil.MigratedPostgresSchema(t))
	require.NoError(t, err)
	t.Cleanup(store.Close)
	client, err := vectorindex.NewClient(testutil.QdrantURL(t))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, client.Close()) })
	workspaceID := uuid.New()
	now := time.Now().UTC()
	require.NoError(t, store.EnsureWorkspace(t.Context(), workspaceID))
	model := domain.EmbeddingModel{ID: uuid.New(), EmbeddingModelDescriptor: domain.EmbeddingModelDescriptor{Name: "Synthetic delivery", Endpoint: "http://fixture.example/v1", ProviderModel: "synthetic", Dimensions: 3}, Enabled: true, InitializationStatus: domain.EmbeddingModelReady, Version: 1, CreatedAt: now, UpdatedAt: now}
	require.NoError(t, store.EmbeddingModels().Create(t.Context(), model))
	require.NoError(t, store.EmbeddingModels().EnsureWorkspaceState(t.Context(), workspaceID, model.ID, now))
	source := domain.Source{ID: uuid.New(), WorkspaceID: workspaceID, Name: "First", Config: domain.SourceConfig{Kind: domain.SourceKindRSS, URL: "https://fixture.example/feed"}, Enabled: true, PollIntervalMinutes: 60, CreatedAt: now, UpdatedAt: now}
	require.NoError(t, store.Create(t.Context(), source))
	item := domain.Item{ID: uuid.New(), WorkspaceID: workspaceID, Kind: domain.ItemKindArticle, Title: "Synthetic canonical text", URL: "https://fixture.example/article", CanonicalURL: "https://fixture.example/article", DiscoveredAt: now, Details: domain.ItemDetails{Type: domain.ItemKindArticle, Article: &domain.ArticleDetails{Description: "Fixture"}}}
	entry := domain.SourceEntry{SourceID: source.ID, ItemID: item.ID, ExternalID: "first", URL: item.URL, DiscoveredAt: now, UpdatedAt: now, Details: domain.SourceEntryDetails{Type: domain.ItemKindArticle, Article: &domain.SourceArticleDetails{Description: "Fixture"}}}
	commit, _, err := store.UpsertItem(t.Context(), item, entry)
	require.NoError(t, err)
	factory := deliveryFactory{client: client, embedder: &embedderStub{}}
	service, err := projection.New(store, factory)
	require.NoError(t, err)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		require.NoError(t, client.DeleteCollection(ctx, vectorindex.CollectionName(model.ID)))
	})
	require.NoError(t, service.DeliverItemProjection(t.Context(), commit))
	require.Len(t, factory.embedder.texts, 1)
	inactive := model
	inactive.ID, inactive.Name = uuid.New(), "Inactive retained delivery"
	require.NoError(t, store.EmbeddingModels().Create(t.Context(), inactive))
	inactiveJob, _, err := store.Jobs().Enqueue(t.Context(), domain.Job{ID: uuid.New(), WorkspaceID: workspaceID, Kind: "reindex-workspace", Queue: domain.JobQueueMaintenance, ArgumentsVersion: 1, Arguments: []byte(`{}`), Status: domain.JobStatusQueued, MaxAttempts: 3, AvailableAt: now, CreatedAt: now, UpdatedAt: now})
	require.NoError(t, err)
	_, err = store.Begin(t.Context(), domain.ModelTransitionStart{WorkspaceID: workspaceID, ModelID: inactive.ID, JobID: inactiveJob.ID, StartedAt: now})
	require.NoError(t, err)
	require.NoError(t, store.Fail(t.Context(), domain.RetrievalIndexFailure{WorkspaceID: workspaceID, ModelID: inactive.ID, JobID: inactiveJob.ID, FailedAt: now}))
	old, found, err := store.LoadItemProjection(t.Context(), workspaceID, item.ID)
	require.NoError(t, err)
	require.True(t, found)
	result, err := store.PublishFeed(t.Context(), domain.FeedPublishCommand{WorkspaceID: workspaceID, PublishedAt: now, Publications: []domain.FeedPublication{{ItemID: item.ID, ModelID: model.ID, Pipeline: "lag"}}})
	require.NoError(t, err)
	require.Len(t, result.Publications, 1)
	index, err := vectorindex.New(client, vectorindex.CollectionName(model.ID), 3)
	require.NoError(t, err)
	filter := ranking.IndexFilter{WorkspaceID: workspaceID, FeedCandidates: true}
	lagged, err := index.QueryRecent(t.Context(), filter, 1)
	require.NoError(t, err)
	require.Len(t, lagged, 1)
	pending, err := store.ListPendingItemProjections(t.Context(), workspaceID, nil, 10)
	require.NoError(t, err)
	require.Len(t, pending, 1)
	require.NoError(t, service.DeliverItemProjection(t.Context(), pending[0]))
	require.Len(t, factory.embedder.texts, 1, "publication reuses both vectors")
	inactivePending, err := store.ListPendingModelProjections(t.Context(), domain.ModelProjectionQuery{WorkspaceID: workspaceID, ModelID: inactive.ID, Limit: 10})
	require.NoError(t, err)
	require.Len(t, inactivePending, 1, "publication advances the retained pending revision")
	require.Equal(t, pending[0].ProjectionRevision, inactivePending[0].ProjectionRevision)
	inactiveExists, err := client.CollectionExists(t.Context(), vectorindex.CollectionName(inactive.ID))
	require.NoError(t, err)
	require.False(t, inactiveExists, "ordinary delivery must not create or write an inactive model's collection")
	repaired, err := index.QueryRecent(t.Context(), filter, 1)
	require.NoError(t, err)
	require.Empty(t, repaired)
	require.ErrorIs(t, index.UpsertProjection(t.Context(), old.Projection, []float32{1, 2, 3}), domain.ErrConflict)
	source.ID = uuid.New()
	source.Name = "Other"
	source.Config.URL = "https://other.example/feed"
	require.NoError(t, store.Create(t.Context(), source))
	entry.SourceID = source.ID
	entry.ExternalID = "second"
	item.ID = uuid.New()
	commit, _, err = store.UpsertItem(t.Context(), item, entry)
	require.NoError(t, err)
	require.NoError(t, service.DeliverItemProjection(t.Context(), commit))
	require.Len(t, factory.embedder.texts, 1, "Source provenance does not re-embed unchanged text")
	// A same-incarnation snapshot can lose points without changing collection metadata.
	_, err = client.Delete(t.Context(), &qdrant.DeletePoints{CollectionName: vectorindex.CollectionName(model.ID),
		Points: qdrant.NewPointsSelector(qdrant.NewIDUUID(commit.ItemID.String())), Wait: qdrant.PtrOf(true)})
	require.NoError(t, err)
	job, _, err := store.Jobs().Enqueue(t.Context(), domain.Job{ID: uuid.New(), WorkspaceID: workspaceID, Kind: "reindex-workspace", Queue: domain.JobQueueMaintenance, ArgumentsVersion: 1, Arguments: []byte(`{}`), Status: domain.JobStatusQueued, MaxAttempts: 3, AvailableAt: now, CreatedAt: now, UpdatedAt: now})
	require.NoError(t, err)
	_, err = store.Begin(t.Context(), domain.ModelTransitionStart{WorkspaceID: workspaceID, ModelID: model.ID, JobID: job.ID, StartedAt: now, Force: true})
	require.NoError(t, err)
	cutover := domain.ModelTransitionCutover{WorkspaceID: workspaceID, ModelID: model.ID, JobID: job.ID, ActivatedAt: now}
	require.Error(t, store.Activate(t.Context(), cutover), "cutover waits for canonical projections")
	pending, err = store.ListPendingModelProjections(t.Context(), domain.ModelProjectionQuery{WorkspaceID: workspaceID, ModelID: model.ID, Limit: 10})
	require.NoError(t, err)
	require.Len(t, pending, 1)
	require.NoError(t, service.DeliverItemProjection(t.Context(), pending[0]))
	require.Len(t, factory.embedder.texts, 2, "missing rebuilt point must embed again")
	require.NoError(t, store.Activate(t.Context(), cutover))
	rebuilt, err := index.QueryRecent(t.Context(), filter, 1)
	require.NoError(t, err)
	require.Empty(t, rebuilt, "canonical rebuild retains publication exclusion")
}
