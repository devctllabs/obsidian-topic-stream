//go:build integration

package postgres_test

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/ethernity/obsidian-topic-stream/internal/domain"
	"github.com/ethernity/obsidian-topic-stream/internal/testutil"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"
)

func TestModelReconciliationRepairsRequestsAndTerminalTransitionState(t *testing.T) {
	f := newFeedFixture(t)
	require.NoError(t, f.store.EnsureDefaultEmbeddingModel(t.Context(), testutil.DefaultEmbeddingDescriptor(), f.now))
	_, err := f.store.GetOrCreateWorkspaceSettings(t.Context(), f.workspaceID, defaultWorkspaceSettings(f.now))
	require.NoError(t, err)
	_, err = f.store.GetOrCreateActive(t.Context(), f.workspaceID, domain.DefaultEmbeddingModelID, f.now)
	require.NoError(t, err)
	model, err := f.store.ResolveModel(t.Context(), domain.DefaultEmbeddingModelID)
	require.NoError(t, err)
	model.ID, model.Name = uuid.New(), uuid.NewString()
	model.InitializationStatus = domain.EmbeddingModelReady
	require.NoError(t, f.store.EmbeddingModels().Create(t.Context(), model))
	generation := selectWorkspaceModel(t, f.store, f.workspaceID, model.ID)
	request := domain.ModelReconciliationRequest{WorkspaceID: f.workspaceID, ModelID: model.ID, SelectionGeneration: generation}
	page, err := f.store.ReconcileModelStates(t.Context(), domain.ModelReconciliationQuery{Limit: 1})
	require.NoError(t, err)
	require.Equal(t, []domain.ModelReconciliationRequest{request}, page.Requests, "selection survives interruption before enqueue")
	require.NotNil(t, page.NextCursor)
	stale := request
	stale.SelectionGeneration--
	require.NoError(t, f.store.CompleteModelRequest(t.Context(), stale))
	page, err = f.store.ReconcileModelStates(t.Context(), domain.ModelReconciliationQuery{Limit: 1})
	require.NoError(t, err)
	require.Equal(t, []domain.ModelReconciliationRequest{request}, page.Requests)
	require.NoError(t, f.store.CompleteModelRequest(t.Context(), request))
	key := fmt.Sprintf("reindex-workspace:%s:%d", model.ID, generation)
	conn, err := pgx.Connect(t.Context(), f.dsn)
	require.NoError(t, err)
	defer func() { require.NoError(t, conn.Close(t.Context())) }()
	for _, status := range []domain.JobStatus{domain.JobStatusDead, domain.JobStatusCancelled} {
		job, created, err := f.store.Jobs().Enqueue(t.Context(), domain.Job{ID: uuid.New(), WorkspaceID: f.workspaceID,
			Kind: "reindex-workspace", Queue: domain.JobQueueMaintenance, ArgumentsVersion: 3, Arguments: json.RawMessage(`{}`),
			IdempotencyKey: &key, Status: domain.JobStatusQueued, MaxAttempts: 3, AvailableAt: f.now, CreatedAt: f.now, UpdatedAt: f.now})
		require.NoError(t, err)
		require.True(t, created)
		_, err = f.store.Begin(t.Context(), domain.ModelTransitionStart{WorkspaceID: f.workspaceID, ModelID: model.ID,
			JobID: job.ID, StartedAt: f.now, SelectionGeneration: generation})
		require.NoError(t, err)
		if status == domain.JobStatusDead {
			_, err = conn.Exec(t.Context(), `UPDATE jobs SET status='running',attempt=max_attempts,lease_owner='crashed-worker',lease_expires_at=clock_timestamp()-interval '1 second' WHERE id=$1`, job.ID)
			require.NoError(t, err)
			claim, err := f.store.Jobs().Claim(t.Context(), domain.JobClaimRequest{Queue: domain.JobQueueMaintenance, Owner: "recovery-worker", Now: f.now.Add(time.Minute), LeaseExpiresAt: f.now.Add(2 * time.Minute)})
			require.NoError(t, err)
			require.Contains(t, claim.ChangedJobs, domain.JobIdentity{ID: job.ID, WorkspaceID: f.workspaceID})
		} else {
			_, err = conn.Exec(t.Context(), `UPDATE jobs SET status='retry_wait',available_at=clock_timestamp()+interval '1 hour' WHERE id=$1`, job.ID)
			require.NoError(t, err)
			cancelled, exists, err := f.store.Jobs().RequestCancel(t.Context(), job.ID, f.now)
			require.NoError(t, err)
			require.True(t, exists)
			require.Equal(t, domain.JobStatusCancelled, cancelled.Status)
		}
		page, err = f.store.ReconcileModelStates(t.Context(), domain.ModelReconciliationQuery{Limit: 1})
		require.NoError(t, err)
		require.Empty(t, page.Requests, "terminal work must not silently restart the same request")
		retrievalStatus, err := f.store.EmbeddingModels().RetrievalStatus(t.Context(), f.workspaceID)
		require.NoError(t, err)
		require.Nil(t, retrievalStatus.TransitionJobID, "terminal cleanup must not depend on the dead worker's defer")
		exists, err := f.store.ModelRequestHasJob(t.Context(), f.workspaceID, key)
		require.NoError(t, err)
		require.True(t, exists, "terminal request history prevents automatic resurrection")
	}
}
