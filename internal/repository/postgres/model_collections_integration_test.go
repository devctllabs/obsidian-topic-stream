//go:build integration

package postgres_test

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ethernity/obsidian-topic-stream/internal/domain"
	repository "github.com/ethernity/obsidian-topic-stream/internal/repository/postgres"
	"github.com/ethernity/obsidian-topic-stream/internal/testutil"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"
)

func TestCollectionReplacementRequiresCompleteRecoveryAndPreservesSameCollectionRebuild(t *testing.T) {
	f := newFeedFixture(t)
	require.NoError(t, f.store.EnsureDefaultEmbeddingModel(t.Context(), testutil.DefaultEmbeddingDescriptor(), f.now))
	_, err := f.store.GetOrCreateActive(t.Context(), f.workspaceID, domain.DefaultEmbeddingModelID, f.now)
	require.NoError(t, err)
	itemID := f.item(t)
	start := domain.ModelTransitionStart{WorkspaceID: f.workspaceID, ModelID: domain.DefaultEmbeddingModelID,
		JobID: enqueueTransitionJob(t, f.store, f.workspaceID, f.now), StartedAt: f.now}
	_, err = f.store.Begin(t.Context(), start)
	require.NoError(t, err)
	work, _, err := f.store.LoadItemProjection(t.Context(), f.workspaceID, itemID)
	require.NoError(t, err)
	delivery := domain.ItemProjectionDelivery{WorkspaceID: f.workspaceID, ItemID: itemID, ModelID: start.ModelID, Revision: work.Projection.Revision}
	_, err = f.store.CompleteItemProjection(t.Context(), delivery)
	require.NoError(t, err)
	first := uuid.New()
	observation := domain.ModelCollectionObservation{WorkspaceID: f.workspaceID, ModelID: start.ModelID, Incarnation: first}
	require.NoError(t, f.store.ObserveModelCollection(t.Context(), observation))
	active, _, err := f.store.GetActive(t.Context(), f.workspaceID)
	require.NoError(t, err)
	require.Equal(t, first, active.CollectionIncarnation)
	require.True(t, active.Initialized)
	second := uuid.New()
	observation.Incarnation = second
	require.NoError(t, f.store.ObserveModelCollection(t.Context(), observation))
	active, _, err = f.store.GetActive(t.Context(), f.workspaceID)
	require.NoError(t, err)
	require.False(t, active.Initialized, "a replaced instance cannot inherit ready Serving state")
	require.Equal(t, second, active.CollectionIncarnation)
	start.SelectionGeneration = active.SelectionGeneration
	start.JobID = enqueueTransitionJob(t, f.store, f.workspaceID, f.now)
	_, err = f.store.Begin(t.Context(), start)
	require.NoError(t, err)
	completed, err := f.store.CompleteItemProjection(t.Context(), delivery)
	require.NoError(t, err)
	require.False(t, completed, "old-incarnation ACK must not erase full recovery work")
	work, _, err = f.store.LoadItemProjection(t.Context(), f.workspaceID, itemID)
	require.NoError(t, err)
	require.Len(t, work.Models, 1)
	delivery.ModelGeneration = work.Models[0].Generation
	completed, err = f.store.CompleteItemProjection(t.Context(), delivery)
	require.NoError(t, err)
	require.True(t, completed)
	require.NoError(t, f.store.Activate(t.Context(), domain.ModelTransitionCutover{WorkspaceID: f.workspaceID,
		ModelID: start.ModelID, JobID: start.JobID, SelectionGeneration: start.SelectionGeneration, ActivatedAt: f.now}))
	active, _, err = f.store.GetActive(t.Context(), f.workspaceID)
	require.NoError(t, err)
	require.True(t, active.Initialized)
	start.JobID = enqueueTransitionJob(t, f.store, f.workspaceID, f.now)
	start.Force = true
	_, err = f.store.Begin(t.Context(), start)
	require.NoError(t, err)
	active, _, err = f.store.GetActive(t.Context(), f.workspaceID)
	require.NoError(t, err)
	require.True(t, active.Initialized, "same-collection explicit rebuild keeps the existing Serving index usable")
}

func TestModelCollectionLockSerializesSeparateStoresAndHonorsCancellation(t *testing.T) {
	f := newFeedFixture(t)
	other, err := repository.Open(t.Context(), f.dsn)
	require.NoError(t, err)
	t.Cleanup(other.Close)
	modelID := uuid.New()
	entered, release, done := make(chan struct{}), make(chan struct{}), make(chan error, 1)
	lockCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	t.Cleanup(func() { close(release); require.NoError(t, testutil.Receive(t, done)); cancel() })
	go func() {
		done <- f.store.WithModelCollectionLock(lockCtx, modelID, func(context.Context) error { close(entered); <-release; return nil })
	}()
	testutil.Receive(t, entered)
	secondCtx, secondCancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
	defer secondCancel()
	var unprotected atomic.Bool
	err = other.WithModelCollectionLock(secondCtx, modelID, func(context.Context) error { unprotected.Store(true); return nil })
	require.ErrorIs(t, err, context.DeadlineExceeded)
	require.False(t, unprotected.Load())
}

func TestServingInitializationDefersExistingItemsToBoundedRecovery(t *testing.T) {
	f := newFeedFixture(t)
	require.NoError(t, f.store.EnsureDefaultEmbeddingModel(t.Context(), testutil.DefaultEmbeddingDescriptor(), f.now))
	conn, err := pgx.Connect(t.Context(), f.dsn)
	require.NoError(t, err)
	defer func() { require.NoError(t, conn.Close(t.Context())) }()
	_, err = conn.Exec(t.Context(), `INSERT INTO items(id,workspace_id,details,title,url,canonical_url)
SELECT md5(n::text)::uuid,$1,'{"type":"article","article":{}}','Synthetic','https://fixture.example/'||n,'https://fixture.example/'||n FROM generate_series(1,1001) n`, f.workspaceID)
	require.NoError(t, err)
	_, err = conn.Exec(t.Context(), `INSERT INTO item_projection_state(workspace_id,item_id,revision,updated_at)
SELECT workspace_id,id,id,$2 FROM items WHERE workspace_id=$1`, f.workspaceID, f.now)
	require.NoError(t, err)
	_, err = f.store.GetOrCreateActive(t.Context(), f.workspaceID, domain.DefaultEmbeddingModelID, f.now)
	require.NoError(t, err)
	var pending int
	require.NoError(t, conn.QueryRow(t.Context(), `SELECT count(*) FROM retrieval_projection_pending WHERE workspace_id=$1`, f.workspaceID).Scan(&pending))
	require.Zero(t, pending, "constructing a runtime must not bulk-seed existing canonical content")
	page, err := f.store.ReconcileModelStates(t.Context(), domain.ModelReconciliationQuery{Limit: 100})
	require.NoError(t, err)
	require.Equal(t, []domain.ModelReconciliationRequest{{WorkspaceID: f.workspaceID, ModelID: domain.DefaultEmbeddingModelID}}, page.Requests)
}
