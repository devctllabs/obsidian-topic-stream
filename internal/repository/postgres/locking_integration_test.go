//go:build integration

package postgres_test

import (
	"context"
	"testing"
	"time"

	"github.com/ethernity/obsidian-topic-stream/internal/domain"
	"github.com/ethernity/obsidian-topic-stream/internal/testutil"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"
)

func TestUnchangedIngestionDoesNotWaitForFeedPublicationLock(t *testing.T) {
	f := newFeedFixture(t)
	require.NoError(t, f.store.EnsureDefaultEmbeddingModel(t.Context(), testutil.DefaultEmbeddingDescriptor(), f.now))
	_, err := f.store.GetOrCreateActive(t.Context(), f.workspaceID, domain.DefaultEmbeddingModelID, f.now)
	require.NoError(t, err)
	item, entry := lockingItem(f)
	commit, changed, err := f.store.UpsertItem(t.Context(), item, entry)
	require.NoError(t, err)
	require.True(t, changed)
	conn, err := pgx.Connect(t.Context(), f.dsn)
	require.NoError(t, err)
	defer func() { require.NoError(t, conn.Close(t.Context())) }()
	tx, err := conn.Begin(t.Context())
	require.NoError(t, err)
	defer func() { _ = tx.Rollback(context.Background()) }()
	_, err = tx.Exec(t.Context(), `SELECT workspace_id FROM feed_state WHERE workspace_id=$1 FOR UPDATE`, f.workspaceID)
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()
	retried, changed, err := f.store.UpsertItem(ctx, item, entry)
	require.NoError(t, err, "unchanged polling must finish while another transaction owns the publication lock")
	require.False(t, changed)
	require.Equal(t, commit, retried, "pending repair remains discoverable without a new revision")
	item.Title = "Updated"
	blocked, stop := context.WithTimeout(t.Context(), 100*time.Millisecond)
	defer stop()
	_, _, err = f.store.UpsertItem(blocked, item, entry)
	require.ErrorIs(t, err, context.DeadlineExceeded, "changed eligibility still shares publication coordination")
	require.NoError(t, tx.Rollback(t.Context()))
	updated, changed, err := f.store.UpsertItem(t.Context(), item, entry)
	require.NoError(t, err)
	require.True(t, changed)
	require.NotEqual(t, commit.ProjectionRevision, updated.ProjectionRevision)
}

func lockingItem(f feedFixture) (domain.Item, domain.SourceEntry) {
	id := uuid.New()
	url := "https://fixture.example/" + id.String()
	return domain.Item{ID: id, WorkspaceID: f.workspaceID, Kind: domain.ItemKindArticle,
			Details: domain.ItemDetails{Type: domain.ItemKindArticle, Article: &domain.ArticleDetails{Description: "Fixture", Content: "Body"}},
			Title:   "Fixture", URL: url, CanonicalURL: url, ContentMarkdown: "Body", DiscoveredAt: f.now},
		domain.SourceEntry{SourceID: f.sourceID, ExternalID: id.String(), ItemID: id, URL: url, DiscoveredAt: f.now, UpdatedAt: f.now,
			Details: domain.SourceEntryDetails{Type: domain.ItemKindArticle, Article: &domain.SourceArticleDetails{Description: "Fixture"}}}
}

func TestClearManyCorrectionsAtomicallyAdvancesEveryProjectionOnce(t *testing.T) {
	f := newFeedFixture(t)
	require.NoError(t, f.store.EnsureDefaultEmbeddingModel(t.Context(), testutil.DefaultEmbeddingDescriptor(), f.now))
	_, err := f.store.GetOrCreateActive(t.Context(), f.workspaceID, domain.DefaultEmbeddingModelID, f.now)
	require.NoError(t, err)
	topicID := f.topic(t)
	before := make(map[uuid.UUID]domain.ItemProjection)
	for range 8 {
		id := f.item(t)
		_, _, err := f.store.SetTopicCorrection(t.Context(), domain.TopicCorrection{WorkspaceID: f.workspaceID, TopicID: topicID, ItemID: id, Membership: domain.TopicMembershipInclude, UpdatedAt: f.now})
		require.NoError(t, err)
		work, _, err := f.store.LoadItemProjection(t.Context(), f.workspaceID, id)
		require.NoError(t, err)
		before[id] = work.Projection
	}
	topic, _, err := f.store.GetTopic(t.Context(), f.workspaceID, topicID)
	require.NoError(t, err)
	conn, err := pgx.Connect(t.Context(), f.dsn)
	require.NoError(t, err)
	defer func() { require.NoError(t, conn.Close(t.Context())) }()
	var rejectedID uuid.UUID
	for id := range before {
		rejectedID = id
		break
	}
	_, err = conn.Exec(t.Context(), `ALTER TABLE retrieval_projection_pending ADD CONSTRAINT reject_fixture_item CHECK(item_id<>'`+rejectedID.String()+`'::uuid) NOT VALID`)
	require.NoError(t, err)
	_, err = f.store.UpdateTopic(t.Context(), domain.TopicUpdate{Topic: topic, ClearCorrections: true})
	require.Error(t, err)
	for id, previous := range before {
		work, _, err := f.store.LoadItemProjection(t.Context(), f.workspaceID, id)
		require.NoError(t, err)
		require.Equal(t, previous.Version, work.Projection.Version)
		require.Equal(t, previous.Revision, work.Projection.Revision)
		require.Len(t, work.Projection.TopicCorrections, 1, "failure rolls back correction removal and every version change")
	}
	_, err = conn.Exec(t.Context(), `ALTER TABLE retrieval_projection_pending DROP CONSTRAINT reject_fixture_item`)
	require.NoError(t, err)
	result, err := f.store.UpdateTopic(t.Context(), domain.TopicUpdate{Topic: topic, ClearCorrections: true})
	require.NoError(t, err)
	require.Len(t, result.ProjectionCommits, len(before))
	seen := make(map[uuid.UUID]bool)
	for _, commit := range result.ProjectionCommits {
		require.False(t, seen[commit.ItemID])
		seen[commit.ItemID] = true
		work, _, err := f.store.LoadItemProjection(t.Context(), f.workspaceID, commit.ItemID)
		require.NoError(t, err)
		require.Equal(t, before[commit.ItemID].Version+1, work.Projection.Version)
		require.NotEqual(t, before[commit.ItemID].Revision, work.Projection.Revision)
		require.Equal(t, commit.ProjectionRevision, work.Projection.Revision)
		require.Empty(t, work.Projection.TopicCorrections)
		require.Len(t, work.Models, 1)
	}
}
