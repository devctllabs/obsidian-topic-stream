//go:build integration

package postgres_test

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/ethernity/obsidian-topic-stream/internal/domain"
	repository "github.com/ethernity/obsidian-topic-stream/internal/repository/postgres"
	"github.com/ethernity/obsidian-topic-stream/internal/testutil"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"
)

func TestModelSelectionRacingCutoverKeepsLatestRequest(t *testing.T) {
	f := newFeedFixture(t)
	require.NoError(t, f.store.EnsureDefaultEmbeddingModel(t.Context(), testutil.DefaultEmbeddingDescriptor(), f.now))
	settings, err := f.store.GetOrCreateWorkspaceSettings(t.Context(), f.workspaceID, defaultWorkspaceSettings(f.now))
	require.NoError(t, err)
	_, err = f.store.GetOrCreateActive(t.Context(), f.workspaceID, domain.DefaultEmbeddingModelID, f.now)
	require.NoError(t, err)
	var targets []uuid.UUID
	for range 2 {
		model, err := f.store.ResolveModel(t.Context(), domain.DefaultEmbeddingModelID)
		require.NoError(t, err)
		model.ID, model.Name, model.InitializationStatus = uuid.New(), uuid.NewString(), domain.EmbeddingModelReady
		require.NoError(t, f.store.EmbeddingModels().Create(t.Context(), model))
		targets = append(targets, model.ID)
	}
	generation := selectWorkspaceModel(t, f.store, f.workspaceID, targets[0])
	start := domain.ModelTransitionStart{WorkspaceID: f.workspaceID, ModelID: targets[0],
		JobID: enqueueTransitionJob(t, f.store, f.workspaceID, f.now), StartedAt: f.now, SelectionGeneration: generation}
	_, err = f.store.Begin(t.Context(), start)
	require.NoError(t, err)
	conn, err := pgx.Connect(t.Context(), f.dsn)
	require.NoError(t, err)
	defer func() { require.NoError(t, conn.Close(context.Background())) }()
	tx, err := conn.Begin(t.Context())
	require.NoError(t, err)
	defer func() { _ = tx.Rollback(context.Background()) }()
	_, err = tx.Exec(t.Context(), `SELECT workspace_id FROM feed_state WHERE workspace_id=$1 FOR UPDATE`, f.workspaceID)
	require.NoError(t, err)
	arrived, activation, selection := make(chan struct{}, 2), make(chan error, 1), make(chan error, 1)
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	go func() {
		arrived <- struct{}{}
		activation <- f.store.Activate(ctx, domain.ModelTransitionCutover{WorkspaceID: f.workspaceID,
			ModelID: start.ModelID, JobID: start.JobID, SelectionGeneration: generation, ActivatedAt: f.now})
	}()
	go func() {
		arrived <- struct{}{}
		settings.Version += 2
		settings.EmbeddingModelID = targets[1]
		_, changed, err := f.store.UpdateWorkspaceSettings(ctx, f.workspaceID, settings.Version-1, settings)
		if err == nil && !changed {
			err = domain.ErrConflict
		}
		selection <- err
	}()
	testutil.Receive(t, arrived)
	testutil.Receive(t, arrived)
	require.NoError(t, tx.Commit(t.Context()))
	require.NoError(t, testutil.Receive(t, selection))
	activationErr := testutil.Receive(t, activation)
	require.True(t, activationErr == nil || errors.Is(activationErr, domain.ErrConflict), "%v", activationErr)
	active, _, err := f.store.GetActive(t.Context(), f.workspaceID)
	require.NoError(t, err)
	require.Equal(t, targets[1], active.DesiredModelID)
	require.Greater(t, active.SelectionGeneration, generation)
	start.ModelID, start.JobID, start.SelectionGeneration = targets[1], enqueueTransitionJob(t, f.store, f.workspaceID, f.now), active.SelectionGeneration
	_, err = f.store.Begin(t.Context(), start)
	require.NoError(t, err)
	require.NoError(t, f.store.Activate(t.Context(), domain.ModelTransitionCutover{WorkspaceID: f.workspaceID,
		ModelID: start.ModelID, JobID: start.JobID, SelectionGeneration: start.SelectionGeneration, ActivatedAt: f.now}))
	active, _, err = f.store.GetActive(t.Context(), f.workspaceID)
	require.NoError(t, err)
	require.Equal(t, targets[1], active.ModelID)
}

func TestLatestModelSelectionFencesQueuedAndRunningTransitions(t *testing.T) {
	for _, scenario := range []struct{ running, returnToServing bool }{{false, false}, {false, true}, {true, false}, {true, true}} {
		running, returnToServing := scenario.running, scenario.returnToServing
		t.Run(fmt.Sprintf("running=%t/return=%t", running, returnToServing), func(t *testing.T) {
			f := newFeedFixture(t)
			require.NoError(t, f.store.EnsureDefaultEmbeddingModel(t.Context(), testutil.DefaultEmbeddingDescriptor(), f.now))
			bootstrap, err := f.store.ResolveModel(t.Context(), domain.DefaultEmbeddingModelID)
			require.NoError(t, err)
			bootstrap.InitializationStatus = domain.EmbeddingModelReady
			bootstrap.Version++
			_, err = f.store.EmbeddingModels().Update(t.Context(), bootstrap, bootstrap.Version-1)
			require.NoError(t, err)
			settings, err := f.store.GetOrCreateWorkspaceSettings(t.Context(), f.workspaceID, defaultWorkspaceSettings(f.now))
			require.NoError(t, err)
			_, err = f.store.GetOrCreateActive(t.Context(), f.workspaceID, domain.DefaultEmbeddingModelID, f.now)
			require.NoError(t, err)
			var targets []uuid.UUID
			for range 2 {
				model, err := f.store.ResolveModel(t.Context(), domain.DefaultEmbeddingModelID)
				require.NoError(t, err)
				model.ID, model.Name = uuid.New(), uuid.NewString()
				require.NoError(t, f.store.EmbeddingModels().Create(t.Context(), model))
				targets = append(targets, model.ID)
			}
			settings.Version++
			settings.EmbeddingModelID = targets[0]
			settings, changed, err := f.store.UpdateWorkspaceSettings(t.Context(), f.workspaceID, 1, settings)
			require.NoError(t, err)
			require.True(t, changed)
			active, _, err := f.store.GetActive(t.Context(), f.workspaceID)
			require.NoError(t, err)
			old := domain.ModelTransitionStart{WorkspaceID: f.workspaceID, ModelID: targets[0],
				JobID: enqueueTransitionJob(t, f.store, f.workspaceID, f.now), StartedAt: f.now,
				SelectionGeneration: active.SelectionGeneration}
			if running {
				_, err = f.store.Begin(t.Context(), old)
				require.NoError(t, err)
			}
			latestID := targets[1]
			if returnToServing {
				latestID = domain.DefaultEmbeddingModelID
			}
			previousVersion := settings.Version
			settings.Version++
			settings.EmbeddingModelID = latestID
			settings, changed, err = f.store.UpdateWorkspaceSettings(t.Context(), f.workspaceID, previousVersion, settings)
			require.NoError(t, err)
			require.True(t, changed, "the latest choice must not wait for an older transition")
			active, _, err = f.store.GetActive(t.Context(), f.workspaceID)
			require.NoError(t, err)
			require.NotEqual(t, old.SelectionGeneration, active.SelectionGeneration)
			require.Equal(t, latestID, active.DesiredModelID)
			_, err = f.store.Begin(t.Context(), old)
			require.ErrorIs(t, err, domain.ErrConflict, "queued or retried obsolete work cannot begin")
			_, err = f.store.SeedModelProjections(t.Context(), domain.ModelProjectionSeedCommand{WorkspaceID: f.workspaceID, ModelID: old.ModelID, JobID: old.JobID, Limit: 1000})
			require.ErrorIs(t, err, domain.ErrConflict, "superseded initialization cannot advance model progress")
			require.ErrorIs(t, f.store.Activate(t.Context(), domain.ModelTransitionCutover{
				WorkspaceID: f.workspaceID, ModelID: old.ModelID, JobID: old.JobID,
				SelectionGeneration: old.SelectionGeneration, ActivatedAt: f.now}), domain.ErrConflict)
			if returnToServing {
				require.Equal(t, latestID, active.ModelID)
				return
			}
			current := domain.ModelTransitionStart{WorkspaceID: f.workspaceID, ModelID: latestID,
				JobID: enqueueTransitionJob(t, f.store, f.workspaceID, f.now), StartedAt: f.now,
				SelectionGeneration: active.SelectionGeneration}
			_, err = f.store.Begin(t.Context(), current)
			require.NoError(t, err)
			require.NoError(t, f.store.Fail(t.Context(), domain.RetrievalIndexFailure{
				WorkspaceID: f.workspaceID, ModelID: old.ModelID, JobID: old.JobID,
				SelectionGeneration: old.SelectionGeneration, FailedAt: f.now.Add(time.Second)}))
			require.NoError(t, f.store.Activate(t.Context(), domain.ModelTransitionCutover{
				WorkspaceID: f.workspaceID, ModelID: current.ModelID, JobID: current.JobID,
				SelectionGeneration: current.SelectionGeneration, ActivatedAt: f.now}))
			active, _, err = f.store.GetActive(t.Context(), f.workspaceID)
			require.NoError(t, err)
			require.Equal(t, latestID, active.ModelID)
		})
	}
}

func TestOldTransitionLeaseCannotClearOrActivateNewAttempt(t *testing.T) {
	f := newFeedFixture(t)
	require.NoError(t, f.store.EnsureDefaultEmbeddingModel(t.Context(), testutil.DefaultEmbeddingDescriptor(), f.now))
	_, err := f.store.GetOrCreateActive(t.Context(), f.workspaceID, domain.DefaultEmbeddingModelID, f.now)
	require.NoError(t, err)
	itemID := f.item(t)
	jobID := enqueueTransitionJob(t, f.store, f.workspaceID, f.now)
	claim, err := f.store.Jobs().Claim(t.Context(), domain.JobClaimRequest{Queue: domain.JobQueueMaintenance,
		Owner: "old-worker", Now: f.now, LeaseExpiresAt: f.now.Add(time.Minute)})
	require.NoError(t, err)
	start := domain.ModelTransitionStart{WorkspaceID: f.workspaceID, ModelID: domain.DefaultEmbeddingModelID,
		JobID: jobID, LeaseOwner: "old-worker", StartedAt: f.now}
	_, err = f.store.Begin(t.Context(), start)
	require.NoError(t, err)
	conn, err := pgx.Connect(t.Context(), f.dsn)
	require.NoError(t, err)
	defer func() { require.NoError(t, conn.Close(t.Context())) }()
	_, err = conn.Exec(t.Context(), `UPDATE jobs SET lease_owner='new-worker' WHERE id=$1`, jobID)
	require.NoError(t, err)
	start.LeaseOwner = "new-worker"
	_, err = f.store.Begin(t.Context(), start)
	require.NoError(t, err)
	require.NoError(t, f.store.Fail(t.Context(), domain.RetrievalIndexFailure{WorkspaceID: f.workspaceID,
		ModelID: start.ModelID, JobID: jobID, LeaseOwner: "old-worker", FailedAt: f.now}))
	cutover := domain.ModelTransitionCutover{WorkspaceID: f.workspaceID, ModelID: start.ModelID,
		JobID: jobID, LeaseOwner: "old-worker", ActivatedAt: f.now}
	require.ErrorIs(t, f.store.Activate(t.Context(), cutover), domain.ErrConflict)
	work, _, err := f.store.LoadItemProjection(t.Context(), f.workspaceID, itemID)
	require.NoError(t, err)
	completed, err := f.store.CompleteItemProjection(t.Context(), domain.ItemProjectionDelivery{
		WorkspaceID: f.workspaceID, ItemID: itemID, ModelID: start.ModelID, Revision: work.Projection.Revision,
		Execution: domain.ProjectionExecution{JobID: jobID, LeaseOwner: "new-worker", Attempt: claim.Job.Attempt}})
	require.NoError(t, err)
	require.True(t, completed)
	cutover.LeaseOwner = "new-worker"
	require.NoError(t, f.store.Activate(t.Context(), cutover))
}

func TestCancelledTransitionStopsDeliveryBeforeBackgroundCleanup(t *testing.T) {
	f := newFeedFixture(t)
	require.NoError(t, f.store.EnsureDefaultEmbeddingModel(t.Context(), testutil.DefaultEmbeddingDescriptor(), f.now))
	_, err := f.store.GetOrCreateActive(t.Context(), f.workspaceID, domain.DefaultEmbeddingModelID, f.now)
	require.NoError(t, err)
	itemID := f.item(t)
	model, err := f.store.ResolveModel(t.Context(), domain.DefaultEmbeddingModelID)
	require.NoError(t, err)
	model.ID, model.Name = uuid.New(), uuid.NewString()
	require.NoError(t, f.store.EmbeddingModels().Create(t.Context(), model))
	jobID := enqueueTransitionJob(t, f.store, f.workspaceID, f.now)
	_, err = f.store.Begin(t.Context(), domain.ModelTransitionStart{WorkspaceID: f.workspaceID, ModelID: model.ID, JobID: jobID, StartedAt: f.now})
	require.NoError(t, err)
	_, _, err = f.store.Jobs().RequestCancel(t.Context(), jobID, f.now)
	require.NoError(t, err)
	work, _, err := f.store.LoadItemProjection(t.Context(), f.workspaceID, itemID)
	require.NoError(t, err)
	require.Len(t, work.Models, 1, "cancelled target must not receive provider calls while cleanup is pending")
	require.Equal(t, domain.DefaultEmbeddingModelID, work.Models[0].ID)
	_, err = f.store.CompleteItemProjection(t.Context(), domain.ItemProjectionDelivery{WorkspaceID: f.workspaceID, ItemID: itemID, ModelID: domain.DefaultEmbeddingModelID, Revision: work.Projection.Revision})
	require.NoError(t, err)
	page, err := f.store.ListPendingGlobalItemProjections(t.Context(), domain.ProjectionRepairQuery{Limit: 100})
	require.NoError(t, err)
	require.Empty(t, page.Commits)
}

func selectWorkspaceModel(t *testing.T, store *repository.Store, workspaceID, modelID uuid.UUID) int64 {
	t.Helper()
	settings, exists, err := store.GetWorkspaceSettings(t.Context(), workspaceID)
	require.NoError(t, err)
	require.True(t, exists)
	version := settings.Version
	settings.Version++
	settings.EmbeddingModelID = modelID
	settings.UpdatedAt = time.Now().UTC()
	_, changed, err := store.UpdateWorkspaceSettings(t.Context(), workspaceID, version, settings)
	require.NoError(t, err)
	require.True(t, changed)
	active, exists, err := store.GetActive(t.Context(), workspaceID)
	require.NoError(t, err)
	require.True(t, exists)
	return active.SelectionGeneration
}
