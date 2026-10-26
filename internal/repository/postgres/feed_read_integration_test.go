//go:build integration

package postgres_test

import (
	"fmt"
	"testing"

	"github.com/ethernity/obsidian-topic-stream/internal/domain"
	repository "github.com/ethernity/obsidian-topic-stream/internal/repository/postgres"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"
)

func TestFeedDisplayFailureRollsBackVisibilityCorrectionAndProjection(t *testing.T) {
	f := newFeedFixture(t)
	itemID := f.item(t)
	_, err := f.store.PublishFeed(t.Context(), domain.FeedPublishCommand{WorkspaceID: f.workspaceID, PublishedAt: f.now,
		Publications: []domain.FeedPublication{{ItemID: itemID, Pipeline: "saved"}}})
	require.NoError(t, err)
	topicID := f.topic(t)
	conn, err := pgx.Connect(t.Context(), f.dsn)
	require.NoError(t, err)
	defer func() { require.NoError(t, conn.Close(t.Context())) }()
	_, err = f.store.SetVisibility(t.Context(), f.workspaceID, itemID, domain.ItemVisibilityHidden)
	require.NoError(t, err)
	before, _, err := f.store.LoadItemProjection(t.Context(), f.workspaceID, itemID)
	require.NoError(t, err)
	_, err = conn.Exec(t.Context(), fmt.Sprintf(`ALTER TABLE feed_display_entries ADD CONSTRAINT reject_restore CHECK(item_id<>'%s'::uuid) NOT VALID`, itemID))
	require.NoError(t, err)
	_, err = f.store.SetVisibility(t.Context(), f.workspaceID, itemID, domain.ItemVisibilityVisible)
	require.Error(t, err)
	item, found, err := f.store.GetItem(t.Context(), f.workspaceID, itemID)
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, domain.ItemVisibilityHidden, item.Visibility)
	after, _, err := f.store.LoadItemProjection(t.Context(), f.workspaceID, itemID)
	require.NoError(t, err)
	require.Equal(t, before.Projection.Version, after.Projection.Version)
	require.Equal(t, before.Projection.Revision, after.Projection.Revision)
	_, err = conn.Exec(t.Context(), `ALTER TABLE feed_display_entries DROP CONSTRAINT reject_restore`)
	require.NoError(t, err)
	_, err = f.store.SetVisibility(t.Context(), f.workspaceID, itemID, domain.ItemVisibilityVisible)
	require.NoError(t, err)
	_, err = conn.Exec(t.Context(), fmt.Sprintf(`ALTER TABLE feed_display_entries ADD CONSTRAINT reject_add CHECK(NOT(topic_ids @> ARRAY['%s'::uuid])) NOT VALID`, topicID))
	require.NoError(t, err)
	_, _, err = f.store.SetTopicCorrection(t.Context(), domain.TopicCorrection{WorkspaceID: f.workspaceID, TopicID: topicID, ItemID: itemID, Membership: domain.TopicMembershipInclude, UpdatedAt: f.now})
	require.Error(t, err)
	var count int
	require.NoError(t, conn.QueryRow(t.Context(), `SELECT count(*) FROM topic_corrections WHERE item_id=$1`, itemID).Scan(&count))
	require.Zero(t, count)
	rows, err := f.store.ReadFeedPublications(t.Context(), domain.FeedPublicationQuery{WorkspaceID: f.workspaceID, HeadOrdinal: 1, TopicID: &topicID, Limit: 25})
	require.NoError(t, err)
	require.Empty(t, rows)
}

func TestFeedReadMergesOriginalAndAddedMembershipBeforePageLimit(t *testing.T) {
	f := newFeedFixture(t)
	topicID, otherTopic := f.topic(t), f.topic(t)
	ids := make([]uuid.UUID, 6)
	conn, err := pgx.Connect(t.Context(), f.dsn)
	require.NoError(t, err)
	defer func() { require.NoError(t, conn.Close(t.Context())) }()
	// Old chronological publication, original match, manual Add, Wrong, hidden, beyond snapshot.
	for index := range ids {
		ids[index] = f.item(t)
		matches := []uuid.UUID{topicID}
		if index == 0 {
			matches = []uuid.UUID{}
		}
		if index == 2 {
			matches = []uuid.UUID{otherTopic}
		}
		_, err = conn.Exec(t.Context(), `INSERT INTO feed_publications
(workspace_id,item_id,batch_id,published_at,ordinal,matched_topic_ids,topic_inputs,score,signals,pipeline,model_id)
VALUES ($1,$2,$3,$4,$5,$6,'{}',0.8,'{}','saved',$7)`, f.workspaceID, ids[index], uuid.New(), f.now, index+1, matches, uuid.Nil)
		require.NoError(t, err)
	}
	require.NoError(t, repository.RefreshFeedDisplayForTest(t.Context(), f.store, f.workspaceID, ids))
	for _, index := range []int{1, 2} {
		_, found, err := f.store.SetTopicCorrection(t.Context(), domain.TopicCorrection{WorkspaceID: f.workspaceID, TopicID: topicID, ItemID: ids[index], Membership: domain.TopicMembershipInclude, UpdatedAt: f.now})
		require.NoError(t, err)
		require.True(t, found)
	}
	_, _, err = f.store.SetTopicCorrection(t.Context(), domain.TopicCorrection{WorkspaceID: f.workspaceID, TopicID: topicID, ItemID: ids[3], Membership: domain.TopicMembershipExclude, UpdatedAt: f.now})
	require.NoError(t, err)
	_, err = f.store.SetVisibility(t.Context(), f.workspaceID, ids[4], domain.ItemVisibilityHidden)
	require.NoError(t, err)
	for _, filter := range []*uuid.UUID{nil, &topicID} {
		page, err := f.store.ReadFeedPublications(t.Context(), domain.FeedPublicationQuery{WorkspaceID: f.workspaceID, HeadOrdinal: 5, TopicID: filter, Limit: 2})
		require.NoError(t, err)
		require.Len(t, page, 2)
		require.Equal(t, ids[2], page[0].Item.ID)
		require.Equal(t, ids[1], page[1].Item.ID)
		require.Equal(t, "Fixture", page[0].Item.ContentMarkdown)
		require.Contains(t, page[0].Item.TopicIDs, topicID)
		next, err := f.store.ReadFeedPublications(t.Context(), domain.FeedPublicationQuery{WorkspaceID: f.workspaceID, HeadOrdinal: 5, BeforeOrdinal: page[1].Publication.Ordinal, TopicID: filter, Limit: 2})
		require.NoError(t, err)
		if filter == nil {
			require.Len(t, next, 1)
			require.Equal(t, ids[0], next[0].Item.ID)
		} else {
			require.Empty(t, next)
		}
	}
}

func TestFeedReadKeepsDeletedAndDisabledTopicHistoryInAll(t *testing.T) {
	f := newFeedFixture(t)
	itemID := f.item(t)
	first, deleted := f.topic(t), f.topic(t)
	conn, err := pgx.Connect(t.Context(), f.dsn)
	require.NoError(t, err)
	defer func() { require.NoError(t, conn.Close(t.Context())) }()
	_, err = conn.Exec(t.Context(), `INSERT INTO feed_publications
(workspace_id,item_id,batch_id,published_at,ordinal,matched_topic_ids,score,pipeline,model_id)
VALUES ($1,$2,$3,$4,1,$5,0.8,'saved',$6)`, f.workspaceID, itemID, uuid.New(), f.now, []uuid.UUID{first, deleted}, uuid.Nil)
	require.NoError(t, err)
	require.NoError(t, repository.RefreshFeedDisplayForTest(t.Context(), f.store, f.workspaceID, []uuid.UUID{itemID}))
	_, err = conn.Exec(t.Context(), `UPDATE topics SET enabled=false WHERE id=$1`, first)
	require.NoError(t, err)
	_, err = f.store.DeleteTopic(t.Context(), f.workspaceID, deleted)
	require.NoError(t, err)
	rows, err := f.store.ReadFeedPublications(t.Context(), domain.FeedPublicationQuery{WorkspaceID: f.workspaceID, HeadOrdinal: 1, TopicID: &first, Limit: 1})
	require.NoError(t, err)
	require.Len(t, rows, 1, "disabled Topic keeps its saved view")
	require.Equal(t, []uuid.UUID{first}, rows[0].Item.TopicIDs)
	rows, err = f.store.ReadFeedPublications(t.Context(), domain.FeedPublicationQuery{WorkspaceID: f.workspaceID, HeadOrdinal: 1, TopicID: &deleted, Limit: 1})
	require.NoError(t, err)
	require.Empty(t, rows, "deleted Topic is absent from effective display membership")
	_, _, err = f.store.SetTopicCorrection(t.Context(), domain.TopicCorrection{WorkspaceID: f.workspaceID, TopicID: first, ItemID: itemID, Membership: domain.TopicMembershipExclude, UpdatedAt: f.now})
	require.NoError(t, err)
	rows, err = f.store.ReadFeedPublications(t.Context(), domain.FeedPublicationQuery{WorkspaceID: f.workspaceID, HeadOrdinal: 1, Limit: 1})
	require.NoError(t, err)
	require.Len(t, rows, 1, "deleted original match is not an explicit Wrong")
	require.Empty(t, rows[0].Item.TopicIDs)
	require.ElementsMatch(t, []uuid.UUID{first, deleted}, rows[0].Publication.MatchedTopicIDs)
}

func TestFeedDisplayRestoresAfterWrongClearDeleteAndVisibilityChanges(t *testing.T) {
	f := newFeedFixture(t)
	itemID := f.item(t)
	topicID := f.topic(t)
	inputs := f.readyTopic(t, topicID)
	_, err := f.store.PublishFeed(t.Context(), domain.FeedPublishCommand{WorkspaceID: f.workspaceID, PublishedAt: f.now,
		Publications: []domain.FeedPublication{{ItemID: itemID, MatchedTopicIDs: []uuid.UUID{topicID},
			TopicInputs: inputs, Pipeline: "saved"}}})
	require.NoError(t, err)
	query := domain.FeedPublicationQuery{WorkspaceID: f.workspaceID, HeadOrdinal: 1, Limit: 25}
	assertVisible := func(visible bool) {
		t.Helper()
		rows, readErr := f.store.ReadFeedPublications(t.Context(), query)
		require.NoError(t, readErr)
		if visible {
			require.Len(t, rows, 1)
			require.Equal(t, itemID, rows[0].Item.ID)
			require.Equal(t, int64(1), rows[0].Publication.Ordinal)
		} else {
			require.Empty(t, rows)
		}
	}
	assertVisible(true)
	wrong := domain.TopicCorrection{WorkspaceID: f.workspaceID, TopicID: topicID, ItemID: itemID, Membership: domain.TopicMembershipExclude, UpdatedAt: f.now}
	_, _, err = f.store.SetTopicCorrection(t.Context(), wrong)
	require.NoError(t, err)
	assertVisible(false)
	topic, found, err := f.store.GetTopic(t.Context(), f.workspaceID, topicID)
	require.NoError(t, err)
	require.True(t, found)
	_, err = f.store.UpdateTopic(t.Context(), domain.TopicUpdate{Topic: topic, ClearCorrections: true})
	require.NoError(t, err)
	assertVisible(true)
	_, _, err = f.store.SetTopicCorrection(t.Context(), wrong)
	require.NoError(t, err)
	assertVisible(false)
	deleted, err := f.store.DeleteTopic(t.Context(), f.workspaceID, topicID)
	require.NoError(t, err)
	require.True(t, deleted)
	assertVisible(true)
	_, err = f.store.SetVisibility(t.Context(), f.workspaceID, itemID, domain.ItemVisibilityHidden)
	require.NoError(t, err)
	assertVisible(false)
	_, err = f.store.SetReadingState(t.Context(), f.workspaceID, itemID, domain.ReadingStateRead)
	require.NoError(t, err)
	assertVisible(false)
	_, err = f.store.SetVisibility(t.Context(), f.workspaceID, itemID, domain.ItemVisibilityVisible)
	require.NoError(t, err)
	assertVisible(true)
}
