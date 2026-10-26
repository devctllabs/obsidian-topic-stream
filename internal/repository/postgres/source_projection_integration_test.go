//go:build integration

package postgres_test

import (
	"testing"

	"github.com/ethernity/obsidian-topic-stream/internal/domain"
	"github.com/ethernity/obsidian-topic-stream/internal/testutil"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"
)

func TestSourceDeletionCapturesProvenanceForRetainedModel(t *testing.T) {
	f := newFeedFixture(t)
	require.NoError(t, f.store.EnsureDefaultEmbeddingModel(t.Context(), testutil.DefaultEmbeddingDescriptor(), f.now))
	_, err := f.store.GetOrCreateActive(t.Context(), f.workspaceID, domain.DefaultEmbeddingModelID, f.now)
	require.NoError(t, err)
	itemID := f.item(t)
	model, err := f.store.ResolveModel(t.Context(), domain.DefaultEmbeddingModelID)
	require.NoError(t, err)
	model.ID, model.Name = uuid.New(), uuid.NewString()
	require.NoError(t, f.store.EmbeddingModels().Create(t.Context(), model))
	start := domain.ModelTransitionStart{WorkspaceID: f.workspaceID, ModelID: model.ID,
		JobID: enqueueTransitionJob(t, f.store, f.workspaceID, f.now), StartedAt: f.now}
	_, err = f.store.Begin(t.Context(), start)
	require.NoError(t, err)
	work, _, err := f.store.LoadItemProjection(t.Context(), f.workspaceID, itemID)
	require.NoError(t, err)
	for _, destination := range work.Models {
		_, err = f.store.CompleteItemProjection(t.Context(), domain.ItemProjectionDelivery{
			WorkspaceID: f.workspaceID, ItemID: itemID, ModelID: destination.ID,
			Revision: work.Projection.Revision, ModelGeneration: destination.Generation})
		require.NoError(t, err)
	}
	require.NoError(t, f.store.Fail(t.Context(), domain.RetrievalIndexFailure{
		WorkspaceID: f.workspaceID, ModelID: model.ID, JobID: start.JobID, FailedAt: f.now}))
	deleted, err := f.store.Delete(t.Context(), f.workspaceID, f.sourceID)
	require.NoError(t, err)
	require.True(t, deleted)
	updated, exists, err := f.store.LoadItemProjection(t.Context(), f.workspaceID, itemID)
	require.NoError(t, err)
	require.True(t, exists)
	require.Empty(t, updated.Projection.Item.SourceIDs)
	require.NotEqual(t, work.Projection.Revision, updated.Projection.Revision)
	pending, err := f.store.ListPendingModelProjections(t.Context(), domain.ModelProjectionQuery{
		WorkspaceID: f.workspaceID, ModelID: model.ID, Limit: 10})
	require.NoError(t, err)
	require.Equal(t, []domain.ItemProjectionCommit{{WorkspaceID: f.workspaceID, ItemID: itemID,
		ProjectionRevision: updated.Projection.Revision}}, pending)

	// A failure while recording projection work must also roll back provenance removal.
	f2 := newFeedFixture(t)
	require.NoError(t, f2.store.EnsureDefaultEmbeddingModel(t.Context(), testutil.DefaultEmbeddingDescriptor(), f2.now))
	_, err = f2.store.GetOrCreateActive(t.Context(), f2.workspaceID, domain.DefaultEmbeddingModelID, f2.now)
	require.NoError(t, err)
	secondID := f2.item(t)
	conn, err := pgx.Connect(t.Context(), f2.dsn)
	require.NoError(t, err)
	defer func() { require.NoError(t, conn.Close(t.Context())) }()
	_, err = conn.Exec(t.Context(), `ALTER TABLE retrieval_projection_pending ADD CONSTRAINT reject_projection CHECK (false) NOT VALID`)
	require.NoError(t, err)
	_, err = f2.store.Delete(t.Context(), f2.workspaceID, f2.sourceID)
	require.Error(t, err)
	item, exists, err := f2.store.GetItem(t.Context(), f2.workspaceID, secondID)
	require.NoError(t, err)
	require.True(t, exists)
	require.Equal(t, []uuid.UUID{f2.sourceID}, item.SourceIDs)
}
