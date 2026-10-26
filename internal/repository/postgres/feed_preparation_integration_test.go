//go:build integration

package postgres_test

import (
	"testing"
	"time"

	"github.com/ethernity/obsidian-topic-stream/internal/domain"
	"github.com/ethernity/obsidian-topic-stream/internal/testutil"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"
)

func TestServingACKCoalescesTopicWritesAndPreservesChangesDuringPreparation(t *testing.T) {
	f := newFeedFixture(t)
	require.NoError(t, f.store.EnsureDefaultEmbeddingModel(t.Context(), testutil.DefaultEmbeddingDescriptor(), f.now))
	_, err := f.store.GetOrCreateActive(t.Context(), f.workspaceID, domain.DefaultEmbeddingModelID, f.now)
	require.NoError(t, err)
	topicID := f.topic(t)
	inputs := f.readyTopic(t, topicID)[topicID]
	inputs.ModelID = domain.DefaultEmbeddingModelID
	conn, err := pgx.Connect(t.Context(), f.dsn)
	require.NoError(t, err)
	defer func() { require.NoError(t, conn.Close(t.Context())) }()
	var before string
	require.NoError(t, conn.QueryRow(t.Context(), `SELECT xmin::text FROM topic_candidate_state WHERE topic_id=$1`, topicID).Scan(&before))
	ack := func() {
		id := f.item(t)
		work, found, err := f.store.LoadItemProjection(t.Context(), f.workspaceID, id)
		require.NoError(t, err)
		require.True(t, found)
		ok, err := f.store.CompleteItemProjection(t.Context(), domain.ItemProjectionDelivery{WorkspaceID: f.workspaceID, ItemID: id, ModelID: domain.DefaultEmbeddingModelID, Revision: work.Projection.Revision})
		require.NoError(t, err)
		require.True(t, ok)
	}
	for range 10 {
		ack()
	}
	var after string
	require.NoError(t, conn.QueryRow(t.Context(), `SELECT xmin::text FROM topic_candidate_state WHERE topic_id=$1`, topicID).Scan(&after))
	require.Equal(t, before, after, "a burst of serving ACKs must not rewrite every Topic")
	due, err := f.store.DueFeedWorkspaces(t.Context(), domain.FeedPreparationDueQuery{At: f.now.Add(2 * time.Minute), Limit: 100})
	require.NoError(t, err)
	require.Equal(t, []uuid.UUID{f.workspaceID}, due)
	work, err := f.store.LoadFeedPreparationWork(t.Context(), domain.FeedPreparationQuery{WorkspaceID: f.workspaceID, At: f.now.Add(2 * time.Minute), Limit: 8})
	require.NoError(t, err)
	require.Len(t, work, 1)
	jobID := uuid.New()
	_, err = conn.Exec(t.Context(), `INSERT INTO jobs(id,workspace_id,kind,queue,status,lease_owner,lease_expires_at)
VALUES($1,$2,'prepare-feed','feed-preparation','running','worker',$3)`, jobID, f.workspaceID, f.now.Add(time.Hour))
	require.NoError(t, err)
	ack() // Arrives after the worker's snapshot, before its commit.
	require.NoError(t, f.store.ReplaceFeedCandidates(t.Context(), domain.FeedCandidateReplacement{
		Work: work[0], Inputs: inputs, Candidates: []domain.FeedCandidate{}, JobID: jobID, LeaseOwner: "worker", At: f.now.Add(2 * time.Minute),
	}))
	_, err = conn.Exec(t.Context(), `UPDATE jobs SET status='succeeded' WHERE id=$1`, jobID)
	require.NoError(t, err)
	due, err = f.store.DueFeedWorkspaces(t.Context(), domain.FeedPreparationDueQuery{At: f.now.Add(2 * time.Minute), Limit: 100})
	require.NoError(t, err)
	require.Equal(t, []uuid.UUID{f.workspaceID}, due, "the in-flight ACK must survive the older preparation commit")
	next, err := f.store.LoadFeedPreparationWork(t.Context(), domain.FeedPreparationQuery{WorkspaceID: f.workspaceID, At: f.now.Add(2 * time.Minute), Limit: 8})
	require.NoError(t, err)
	require.Len(t, next, 1)
	require.Greater(t, next[0].RequestedVersion, work[0].RequestedVersion)
	require.Equal(t, work[0].InputVersion, next[0].InputVersion, "refill requests do not invalidate captured semantic inputs")
}
