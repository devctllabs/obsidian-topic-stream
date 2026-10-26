//go:build integration

package deps

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/devctllabs/go-libs/di"
	generated "github.com/ethernity/obsidian-topic-stream/gen/config"
	"github.com/ethernity/obsidian-topic-stream/gen/serverhttp"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestModelRecoveryHTTPReconnectRetryAndCutover(t *testing.T) {
	f := newStableAPIFixture(t)
	f.prepare(t)
	f.stopWorkers()
	stored, _, err := f.store.GetWorkspaceSettings(t.Context(), f.workspaceID)
	require.NoError(t, err)
	model, err := f.store.ResolveModel(t.Context(), stored.EmbeddingModelID)
	require.NoError(t, err)
	serving := model.ID
	model.ID, model.Name = uuid.New(), "Recoverable model"
	require.NoError(t, f.store.EmbeddingModels().Create(t.Context(), model))
	cleanupModelCollections(t, f, []uuid.UUID{model.ID}, serving)
	patchModelHTTP(t, f, stored.Version, model.ID)
	var before serverhttp.RetrievalStatus
	f.exchange(t, apiExchange{method: http.MethodGet, path: "/api/v1/retrieval-status", status: 200}, &before)
	require.NotNil(t, before.LatestTransition)
	require.Equal(t, serverhttp.ModelTransitionStatusQueued, before.LatestTransition.Status)
	require.Equal(t, &serving, before.ServingModelId)
	source := *before.LatestTransition.JobId
	f.exchange(t, apiExchange{method: http.MethodPost, path: "/api/v1/jobs/" + source.String() + "/cancel", status: 202}, nil)
	require.NoError(t, f.runtime.modelReconciler.Reconcile(t.Context()))
	// A fresh HTTP graph has neither old in-memory transition state nor an advisory subscription.
	cfg, err := di.Resolve[generated.Config](f.runtime.container)
	require.NoError(t, err)
	cfg.Runtime.WorkersEnabled = false
	reconnected, err := NewAPI(t.Context(), cfg, "reconnected-model-recovery")
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, reconnected.Shutdown(context.Background())) })
	reader := f
	reader.runtime = reconnected
	var status serverhttp.RetrievalStatus
	reader.exchange(t, apiExchange{method: http.MethodGet, path: "/api/v1/retrieval-status", status: 200}, &status)
	require.Nil(t, status.TransitionJobId)
	require.Equal(t, &source, status.LatestTransition.JobId)
	require.Equal(t, serverhttp.ModelTransitionStatusCancelled, status.LatestTransition.Status)
	require.True(t, status.LatestTransition.CanRetry)
	var retry serverhttp.JobAccepted
	reader.exchange(t, apiExchange{method: http.MethodPost, path: "/api/v1/jobs/" + source.String() + "/retry", status: 202}, &retry)
	require.NotEqual(t, source, retry.JobId)
	reader.exchange(t, apiExchange{method: http.MethodPost, path: "/api/v1/jobs/" + source.String() + "/retry", status: 409}, nil)
	var old serverhttp.Job
	reader.exchange(t, apiExchange{method: http.MethodGet, path: "/api/v1/jobs/" + source.String(), status: 200}, &old)
	require.False(t, old.ModelTransition.IsCurrent)
	require.False(t, old.ModelTransition.CanRetry)
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { done <- f.runtime.jobs.Run(ctx) }()
	t.Cleanup(func() { cancel(); require.NoError(t, <-done) })
	awaitServingModel(t, f, model.ID)
	require.Eventually(t, func() bool {
		job, found, err := f.store.Jobs().Get(t.Context(), retry.JobId)
		return err == nil && found && job.Status == "succeeded"
	}, 10*time.Second, 20*time.Millisecond)
	require.NoError(t, f.runtime.modelReconciler.Reconcile(t.Context()))
	status = serverhttp.RetrievalStatus{}
	reader.exchange(t, apiExchange{method: http.MethodGet, path: "/api/v1/retrieval-status", status: 200}, &status)
	require.Equal(t, &model.ID, status.ServingModelId)
	require.Nil(t, status.TransitionJobId)
	require.Equal(t, &retry.JobId, status.LatestTransition.JobId)
	require.Equal(t, before.LatestTransition.SelectionGeneration, status.LatestTransition.SelectionGeneration)
	require.Equal(t, serverhttp.ModelTransitionStatusSucceeded, status.LatestTransition.Status)
	require.False(t, status.LatestTransition.CanRetry)
}
