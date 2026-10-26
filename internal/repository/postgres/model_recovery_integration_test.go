//go:build integration

package postgres_test

import (
	"encoding/json"
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

func newModelRecoveryFixture(t *testing.T) (feedFixture, uuid.UUID, int64) {
	t.Helper()
	f := newFeedFixture(t)
	require.NoError(t, f.store.EnsureDefaultEmbeddingModel(t.Context(), testutil.DefaultEmbeddingDescriptor(), f.now))
	conn, err := pgx.Connect(t.Context(), f.dsn)
	require.NoError(t, err)
	_, err = conn.Exec(t.Context(), `UPDATE embedding_models SET initialization_status='ready' WHERE id=$1`, domain.DefaultEmbeddingModelID)
	require.NoError(t, err)
	require.NoError(t, conn.Close(t.Context()))
	_, err = f.store.GetOrCreateWorkspaceSettings(t.Context(), f.workspaceID, defaultWorkspaceSettings(f.now))
	require.NoError(t, err)
	_, err = f.store.GetOrCreateActive(t.Context(), f.workspaceID, domain.DefaultEmbeddingModelID, f.now)
	require.NoError(t, err)
	model, err := f.store.ResolveModel(t.Context(), domain.DefaultEmbeddingModelID)
	require.NoError(t, err)
	model.ID, model.Name, model.InitializationStatus = uuid.New(), uuid.NewString(), domain.EmbeddingModelReady
	require.NoError(t, f.store.EmbeddingModels().Create(t.Context(), model))
	return f, model.ID, selectWorkspaceModel(t, f.store, f.workspaceID, model.ID)
}

func recoveryJob(t *testing.T, f feedFixture, model uuid.UUID, generation int64) domain.Job {
	t.Helper()
	arguments, err := json.Marshal(map[string]any{"modelId": model, "selectionGeneration": generation, "force": false})
	require.NoError(t, err)
	key := fmt.Sprintf("reindex-workspace:%s:%d", model, generation)
	return domain.Job{ID: uuid.New(), WorkspaceID: f.workspaceID, Kind: "reindex-workspace", Queue: domain.JobQueueMaintenance, ArgumentsVersion: 3, Arguments: arguments, IdempotencyKey: &key, Status: domain.JobStatusQueued, MaxAttempts: 3, AvailableAt: f.now, CreatedAt: f.now, UpdatedAt: f.now}
}

func TestModelRecoveryStatusSurvivesTerminalCleanupAndReopen(t *testing.T) {
	f, model, generation := newModelRecoveryFixture(t)
	status, err := f.store.EmbeddingModels().RetrievalStatus(t.Context(), f.workspaceID)
	require.NoError(t, err)
	require.NotNil(t, status.LatestTransition)
	require.Equal(t, "waiting", status.LatestTransition.Status)
	require.Nil(t, status.LatestTransition.JobID)
	require.False(t, status.LatestTransition.CanRetry)
	job, created, err := f.store.Jobs().Enqueue(t.Context(), recoveryJob(t, f, model, generation))
	require.NoError(t, err)
	require.True(t, created)
	status, err = f.store.EmbeddingModels().RetrievalStatus(t.Context(), f.workspaceID)
	require.NoError(t, err)
	require.Equal(t, &job.ID, status.LatestTransition.JobID)
	require.Equal(t, "queued", status.LatestTransition.Status)
	_, _, err = f.store.Jobs().RequestCancel(t.Context(), job.ID, f.now)
	require.NoError(t, err)
	_, err = f.store.ReconcileModelStates(t.Context(), domain.ModelReconciliationQuery{Limit: 100})
	require.NoError(t, err)
	reopened, err := repository.Open(t.Context(), f.dsn)
	require.NoError(t, err)
	t.Cleanup(reopened.Close)
	status, err = reopened.EmbeddingModels().RetrievalStatus(t.Context(), f.workspaceID)
	require.NoError(t, err)
	require.Nil(t, status.TransitionJobID)
	require.Equal(t, &job.ID, status.LatestTransition.JobID)
	require.Equal(t, "cancelled", status.LatestTransition.Status)
	require.Equal(t, model, status.LatestTransition.ModelID)
	require.Equal(t, generation, status.LatestTransition.SelectionGeneration)
	require.True(t, status.LatestTransition.CanRetry)
	history, found, err := reopened.Jobs().Get(t.Context(), job.ID)
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, status.LatestTransition, history.ModelTransition)
	require.WithinDuration(t, job.CreatedAt, history.CreatedAt, time.Microsecond)
}

func TestModelRecoveryRetryAdmitsOnlyTheCurrentFailedOperation(t *testing.T) {
	f, model, generation := newModelRecoveryFixture(t)
	source, _, err := f.store.Jobs().Enqueue(t.Context(), recoveryJob(t, f, model, generation))
	require.NoError(t, err)
	_, _, err = f.store.Jobs().RequestCancel(t.Context(), source.ID, f.now)
	require.NoError(t, err)
	replacement := recoveryJob(t, f, model, generation)
	next, created, err := f.store.RetryModelTransition(t.Context(), source, replacement)
	require.NoError(t, err)
	require.True(t, created)
	require.Equal(t, replacement.ID, next.ID)
	status, err := f.store.EmbeddingModels().RetrievalStatus(t.Context(), f.workspaceID)
	require.NoError(t, err)
	require.Equal(t, &next.ID, status.LatestTransition.JobID)
	require.False(t, status.LatestTransition.CanRetry)
	_, _, err = f.store.RetryModelTransition(t.Context(), source, recoveryJob(t, f, model, generation))
	require.ErrorIs(t, err, domain.ErrJobNotRetryable)
	old, found, err := f.store.Jobs().Get(t.Context(), source.ID)
	require.NoError(t, err)
	require.True(t, found)
	require.False(t, old.ModelTransition.IsCurrent)
	require.False(t, old.ModelTransition.CanRetry)
	_, _, err = f.store.Jobs().RequestCancel(t.Context(), next.ID, f.now)
	require.NoError(t, err)
	selectWorkspaceModel(t, f.store, f.workspaceID, domain.DefaultEmbeddingModelID)
	later := selectWorkspaceModel(t, f.store, f.workspaceID, model)
	require.Greater(t, later, generation)
	_, _, err = f.store.RetryModelTransition(t.Context(), next, recoveryJob(t, f, model, generation))
	require.ErrorIs(t, err, domain.ErrJobNotRetryable, "A to B to A must not revive an old operation")
	status, err = f.store.EmbeddingModels().RetrievalStatus(t.Context(), f.workspaceID)
	require.NoError(t, err)
	require.Equal(t, "waiting", status.LatestTransition.Status)
	require.Equal(t, later, status.LatestTransition.SelectionGeneration)
}

func TestModelRecoveryRetryResumesPartialRebuildWithoutResettingModelProgress(t *testing.T) {
	f, model, generation := newModelRecoveryFixture(t)
	for range 3 {
		f.item(t)
	}
	job := recoveryJob(t, f, model, generation)
	var arguments map[string]any
	require.NoError(t, json.Unmarshal(job.Arguments, &arguments))
	arguments["force"] = true
	job.Arguments, _ = json.Marshal(arguments)
	source, _, err := f.store.Jobs().Enqueue(t.Context(), job)
	require.NoError(t, err)
	start := domain.ModelTransitionStart{WorkspaceID: f.workspaceID, ModelID: model, JobID: source.ID, SelectionGeneration: generation, StartedAt: f.now, Force: true}
	_, err = f.store.Begin(t.Context(), start)
	require.NoError(t, err)
	pending, err := f.store.ListPendingModelProjections(t.Context(), domain.ModelProjectionQuery{WorkspaceID: f.workspaceID, ModelID: model, Limit: 10})
	require.NoError(t, err)
	require.Len(t, pending, 3)
	conn, err := pgx.Connect(t.Context(), f.dsn)
	require.NoError(t, err)
	var checkpoint uuid.UUID
	require.NoError(t, conn.QueryRow(t.Context(), `SELECT generation FROM workspace_model_progress WHERE workspace_id=$1 AND embedding_model_id=$2`, f.workspaceID, model).Scan(&checkpoint))
	require.NoError(t, conn.Close(t.Context()))
	accepted, err := f.store.CompleteItemProjection(t.Context(), domain.ItemProjectionDelivery{WorkspaceID: f.workspaceID, ModelID: model, ItemID: pending[0].ItemID, Revision: pending[0].ProjectionRevision, ModelGeneration: checkpoint})
	require.NoError(t, err)
	require.True(t, accepted)
	old, _, err := f.store.Jobs().Get(t.Context(), source.ID)
	require.NoError(t, err)
	require.EqualValues(t, 1, old.Progress.ProcessedItems)
	_, _, err = f.store.Jobs().RequestCancel(t.Context(), source.ID, f.now)
	require.NoError(t, err)
	next, created, err := f.store.RetryModelTransition(t.Context(), source, recoveryJob(t, f, model, generation))
	require.NoError(t, err)
	require.True(t, created)
	require.NoError(t, json.Unmarshal(next.Arguments, &arguments))
	require.Equal(t, false, arguments["force"])
	start.JobID = next.ID
	start.Force = false
	_, err = f.store.Begin(t.Context(), start)
	require.NoError(t, err)
	current, _, err := f.store.Jobs().Get(t.Context(), next.ID)
	require.NoError(t, err)
	require.EqualValues(t, 2, *current.Progress.TotalItems, "new Job captures only remaining work")
	old, _, err = f.store.Jobs().Get(t.Context(), source.ID)
	require.NoError(t, err)
	require.EqualValues(t, 3, *old.Progress.TotalItems)
	require.EqualValues(t, 1, old.Progress.ProcessedItems)
}

func TestModelRecoveryConcurrentRetriesAdmitOneReplacement(t *testing.T) {
	f, model, generation := newModelRecoveryFixture(t)
	source, _, err := f.store.Jobs().Enqueue(t.Context(), recoveryJob(t, f, model, generation))
	require.NoError(t, err)
	_, _, err = f.store.Jobs().RequestCancel(t.Context(), source.ID, f.now)
	require.NoError(t, err)
	type outcome struct {
		job domain.Job
		err error
	}
	results := make(chan outcome, 2)
	start := make(chan struct{})
	replacements := []domain.Job{recoveryJob(t, f, model, generation), recoveryJob(t, f, model, generation)}
	for _, replacement := range replacements {
		go func() {
			<-start
			next, _, failure := f.store.RetryModelTransition(t.Context(), source, replacement)
			results <- outcome{next, failure}
		}()
	}
	close(start)
	successful := 0
	for range 2 {
		result := testutil.Receive(t, results)
		if result.err == nil {
			successful++
		} else {
			require.ErrorIs(t, result.err, domain.ErrJobNotRetryable)
		}
	}
	require.Equal(t, 1, successful)
}

func TestModelRecoveryObsoleteWorkerCannotBeginAfterRetry(t *testing.T) {
	f, model, generation := newModelRecoveryFixture(t)
	source, _, err := f.store.Jobs().Enqueue(t.Context(), recoveryJob(t, f, model, generation))
	require.NoError(t, err)
	_, _, err = f.store.Jobs().RequestCancel(t.Context(), source.ID, f.now)
	require.NoError(t, err)
	_, _, err = f.store.RetryModelTransition(t.Context(), source, recoveryJob(t, f, model, generation))
	require.NoError(t, err)
	_, err = f.store.Begin(t.Context(), domain.ModelTransitionStart{WorkspaceID: f.workspaceID, ModelID: model, JobID: source.ID, SelectionGeneration: generation, StartedAt: f.now})
	require.ErrorIs(t, err, domain.ErrConflict)
}

func TestModelRecoveryRetryRacingSelectionCannotRestoreTheOldModel(t *testing.T) {
	f, model, generation := newModelRecoveryFixture(t)
	source, _, err := f.store.Jobs().Enqueue(t.Context(), recoveryJob(t, f, model, generation))
	require.NoError(t, err)
	_, _, err = f.store.Jobs().RequestCancel(t.Context(), source.ID, f.now)
	require.NoError(t, err)
	start := make(chan struct{})
	done := make(chan error, 1)
	replacement := recoveryJob(t, f, model, generation)
	go func() {
		<-start
		_, _, failure := f.store.RetryModelTransition(t.Context(), source, replacement)
		done <- failure
	}()
	close(start)
	selectWorkspaceModel(t, f.store, f.workspaceID, domain.DefaultEmbeddingModelID)
	if failure := testutil.Receive(t, done); failure != nil {
		require.ErrorIs(t, failure, domain.ErrJobNotRetryable)
	}
	settings, _, err := f.store.GetWorkspaceSettings(t.Context(), f.workspaceID)
	require.NoError(t, err)
	require.Equal(t, domain.DefaultEmbeddingModelID, settings.EmbeddingModelID)
	job, found, err := f.store.Jobs().Get(t.Context(), replacement.ID)
	require.NoError(t, err)
	if found {
		require.Equal(t, domain.JobStatusCancelled, job.Status, "selection cancels an earlier admitted replacement")
		require.False(t, job.ModelTransition.IsCurrent)
		require.False(t, job.ModelTransition.CanRetry)
	}
}

func TestModelRecoveryRetryBeforeFirstBeginRetainsExplicitRebuildIntent(t *testing.T) {
	f, model, generation := newModelRecoveryFixture(t)
	job := recoveryJob(t, f, model, generation)
	job.Arguments, _ = json.Marshal(map[string]any{"modelId": model, "selectionGeneration": generation, "force": true})
	source, _, err := f.store.Jobs().Enqueue(t.Context(), job)
	require.NoError(t, err)
	conn, err := pgx.Connect(t.Context(), f.dsn)
	require.NoError(t, err)
	_, err = conn.Exec(t.Context(), `UPDATE jobs SET status='dead',reason='fixture initialization failure',finished_at=$2 WHERE id=$1`, source.ID, f.now)
	require.NoError(t, err)
	require.NoError(t, conn.Close(t.Context()))
	status, err := f.store.EmbeddingModels().RetrievalStatus(t.Context(), f.workspaceID)
	require.NoError(t, err)
	require.Nil(t, status.ServingModelID, "first initialization recovery works without a previous Serving model")
	require.Equal(t, "dead", status.LatestTransition.Status)
	require.True(t, status.LatestTransition.CanRetry)
	next, created, err := f.store.RetryModelTransition(t.Context(), source, recoveryJob(t, f, model, generation))
	require.NoError(t, err)
	require.True(t, created)
	var arguments struct {
		Force bool `json:"force"`
	}
	require.NoError(t, json.Unmarshal(next.Arguments, &arguments))
	require.True(t, arguments.Force, "an explicit rebuild resets once when its first Begin eventually runs")
}
