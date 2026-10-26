//go:build integration

package postgres_test

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ethernity/obsidian-topic-stream/internal/domain"
	"github.com/ethernity/obsidian-topic-stream/internal/repository/postgres"
	"github.com/ethernity/obsidian-topic-stream/internal/testutil"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"
)

type captureGateKey struct{}
type captureGate struct{ arrived, release chan struct{} }

// TraceQueryStart identifies capture's shared retrieval coordination boundary.
func (*captureGate) TraceQueryStart(ctx context.Context, _ *pgx.Conn, query pgx.TraceQueryStartData) context.Context {
	return context.WithValue(ctx, captureGateKey{}, strings.HasPrefix(query.SQL, "SELECT workspace_id FROM workspace_retrieval_state"))
}

// TraceQueryEnd holds the real transaction after its lock has been granted.
func (gate *captureGate) TraceQueryEnd(ctx context.Context, _ *pgx.Conn, result pgx.TraceQueryEndData) {
	if ctx.Value(captureGateKey{}) == true && result.Err == nil {
		close(gate.arrived)
		select {
		case <-gate.release:
		case <-ctx.Done():
		}
	}
}

func TestBulkCaptureSerializesModelEvictionWithoutExposingPartialCorrections(t *testing.T) {
	f := newFeedFixture(t)
	seedBulkCaptureModels(t, f, 3)
	itemID, topicID := f.item(t), f.topic(t)
	_, _, err := f.store.SetTopicCorrection(t.Context(), domain.TopicCorrection{WorkspaceID: f.workspaceID, TopicID: topicID, ItemID: itemID, Membership: domain.TopicMembershipInclude, UpdatedAt: f.now})
	require.NoError(t, err)
	before, found, err := f.store.LoadItemProjection(t.Context(), f.workspaceID, itemID)
	require.NoError(t, err)
	require.True(t, found)
	topic, _, err := f.store.GetTopic(t.Context(), f.workspaceID, topicID)
	require.NoError(t, err)
	model, err := f.store.ResolveModel(t.Context(), domain.DefaultEmbeddingModelID)
	require.NoError(t, err)
	model.ID, model.Name, model.InitializationStatus = uuid.New(), uuid.NewString(), domain.EmbeddingModelReady
	require.NoError(t, f.store.EmbeddingModels().Create(t.Context(), model))
	start := domain.ModelTransitionStart{WorkspaceID: f.workspaceID, ModelID: model.ID, JobID: enqueueTransitionJob(t, f.store, f.workspaceID, f.now), StartedAt: f.now.Add(time.Minute)}
	gate := &captureGate{arrived: make(chan struct{}), release: make(chan struct{})}
	traced, err := postgres.OpenWithTracerForTest(t.Context(), f.dsn, gate)
	require.NoError(t, err)
	defer traced.Close()
	var once sync.Once
	release := func() { once.Do(func() { close(gate.release) }) }
	defer release()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	type outcome struct {
		result domain.TopicUpdateResult
		err    error
	}
	captured := make(chan outcome, 1)
	go func() {
		result, err := traced.UpdateTopic(ctx, domain.TopicUpdate{Topic: topic, ClearCorrections: true})
		captured <- outcome{result, err}
	}()
	testutil.Receive(t, gate.arrived)
	transition := make(chan error, 1)
	go func() { _, err := f.store.Begin(ctx, start); transition <- err }()
	visible, found, err := f.store.LoadItemProjection(ctx, f.workspaceID, itemID)
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, before.Projection.Revision, visible.Projection.Revision)
	require.Len(t, visible.Projection.TopicCorrections, 1, "uncommitted correction removal must remain invisible")
	release()
	result := testutil.Receive(t, captured)
	require.NoError(t, result.err)
	require.Len(t, result.result.ProjectionCommits, 1)
	require.NoError(t, testutil.Receive(t, transition))
	conn, err := pgx.Connect(t.Context(), f.dsn)
	require.NoError(t, err)
	defer func() { require.NoError(t, conn.Close(t.Context())) }()
	var count int
	require.NoError(t, conn.QueryRow(t.Context(), `SELECT count(*) FROM retrieval_projection_pending pending
 JOIN workspace_model_progress progress USING(workspace_id,embedding_model_id)
 JOIN item_projection_state state USING(workspace_id,item_id)
 WHERE pending.workspace_id=$1 AND pending.item_id=$2 AND progress.retained
 AND pending.revision=state.revision AND pending.model_generation=progress.generation`, f.workspaceID, itemID).Scan(&count))
	require.Equal(t, 3, count, "capture and newly seeded selection preserve every retained latest revision")
	var evicted uuid.UUID
	require.NoError(t, conn.QueryRow(t.Context(), `SELECT embedding_model_id FROM workspace_model_progress WHERE workspace_id=$1 AND NOT retained`, f.workspaceID).Scan(&evicted))
	accepted, err := f.store.CompleteItemProjection(t.Context(), domain.ItemProjectionDelivery{WorkspaceID: f.workspaceID, ItemID: itemID, ModelID: evicted, Revision: result.result.ProjectionCommits[0].ProjectionRevision})
	require.NoError(t, err)
	require.False(t, accepted, "an evicted destination cannot acknowledge captured work")
}
