//go:build integration

package postgres_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/ethernity/obsidian-topic-stream/internal/domain"
	repository "github.com/ethernity/obsidian-topic-stream/internal/repository/postgres"
	"github.com/ethernity/obsidian-topic-stream/internal/service/feed"
	"github.com/ethernity/obsidian-topic-stream/internal/testutil"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"
)

type feedFixture struct {
	dsn         string
	store       *repository.Store
	workspaceID uuid.UUID
	sourceID    uuid.UUID
	now         time.Time
}

func newFeedFixture(t *testing.T) feedFixture {
	t.Helper()
	dsn := testutil.MigratedPostgresSchema(t)
	store, err := repository.Open(t.Context(), dsn)
	require.NoError(t, err)
	t.Cleanup(store.Close)
	f := feedFixture{dsn: dsn, store: store, workspaceID: uuid.New(), sourceID: uuid.New(), now: time.Now().UTC().Truncate(time.Microsecond)}
	require.NoError(t, store.EnsureWorkspace(t.Context(), f.workspaceID))
	require.NoError(t, store.Create(t.Context(), domain.Source{ID: f.sourceID, WorkspaceID: f.workspaceID, Name: "Synthetic",
		Config: domain.SourceConfig{Kind: domain.SourceKindRSS, URL: "https://fixture.example/feed"}, Enabled: true,
		PollIntervalMinutes: 60, CreatedAt: f.now, UpdatedAt: f.now}))
	return f
}

func (f feedFixture) item(t *testing.T) uuid.UUID {
	t.Helper()
	id := uuid.New()
	url := "https://fixture.example/" + id.String()
	_, _, err := f.store.UpsertItem(t.Context(), domain.Item{ID: id, WorkspaceID: f.workspaceID, Kind: domain.ItemKindArticle,
		Details: domain.ItemDetails{Type: domain.ItemKindArticle, Article: &domain.ArticleDetails{Description: "Fixture", Content: "Fixture"}},
		Title:   "Fixture", URL: url, CanonicalURL: url, ContentMarkdown: "Fixture", DiscoveredAt: f.now,
	}, domain.SourceEntry{SourceID: f.sourceID, ExternalID: id.String(), ItemID: id, URL: url, DiscoveredAt: f.now, UpdatedAt: f.now,
		Details: domain.SourceEntryDetails{Type: domain.ItemKindArticle, Article: &domain.SourceArticleDetails{Description: "Fixture"}}})
	require.NoError(t, err)
	return id
}

func TestSharedFeedPublishesOnceAndReadsOldSavedOrder(t *testing.T) {
	f := newFeedFixture(t)
	first, second := f.item(t), f.item(t)
	command := domain.FeedPublishCommand{WorkspaceID: f.workspaceID, PublishedAt: f.now, Publications: []domain.FeedPublication{
		{ItemID: first, Score: 0.9, Pipeline: "saved"}, {ItemID: second, Score: 0.8, Pipeline: "saved"},
	}}
	result, err := f.store.PublishFeed(t.Context(), command)
	require.NoError(t, err)
	require.Equal(t, int64(2), result.HeadOrdinal)
	require.Len(t, result.Publications, 2)
	require.Equal(t, int64(2), result.Publications[0].Ordinal)
	command.PublishedAt = f.now.AddDate(1, 0, 0)
	repeated, err := f.store.PublishFeed(t.Context(), command)
	require.NoError(t, err)
	require.Empty(t, repeated.Publications)
	require.Equal(t, result.HeadOrdinal, repeated.HeadOrdinal)
	rows, err := f.store.ReadFeedPublications(t.Context(), domain.FeedPublicationQuery{WorkspaceID: f.workspaceID, HeadOrdinal: 2, Limit: 25})
	require.NoError(t, err)
	require.Len(t, rows, 2)
	require.Equal(t, first, rows[0].Item.ID)
	require.WithinDuration(t, f.now, rows[0].Publication.PublishedAt, 0)
}

func (f feedFixture) topic(t *testing.T) uuid.UUID {
	t.Helper()
	id := uuid.New()
	require.NoError(t, f.store.CreateTopic(t.Context(), domain.Topic{ID: id, WorkspaceID: f.workspaceID, Name: id.String(), Enabled: true, Mode: domain.TopicModeOrdinary, Revision: 1, CreatedAt: f.now, UpdatedAt: f.now}))
	return id
}

func TestSharedFeedCorrectionsChangeViewsWithoutRepublishing(t *testing.T) {
	f := newFeedFixture(t)
	itemID := f.item(t)
	firstTopic, secondTopic := f.topic(t), f.topic(t)
	inputs := f.readyTopic(t, firstTopic)
	published, err := f.store.PublishFeed(t.Context(), domain.FeedPublishCommand{WorkspaceID: f.workspaceID, TopicID: &firstTopic, PublishedAt: f.now,
		Publications: []domain.FeedPublication{{ItemID: itemID, MatchedTopicIDs: []uuid.UUID{firstTopic}, TopicInputs: inputs, Pipeline: "original"}}})
	require.NoError(t, err)
	require.Len(t, published.Publications, 1)
	read := func(topicID *uuid.UUID) []domain.FeedPublishedItem {
		result, err := f.store.ReadFeedPublications(t.Context(), domain.FeedPublicationQuery{WorkspaceID: f.workspaceID, HeadOrdinal: 1, Limit: 25, TopicID: topicID})
		require.NoError(t, err)
		return result
	}
	correct := func(topicID uuid.UUID, value domain.TopicMembership) {
		_, found, err := f.store.SetTopicCorrection(t.Context(), domain.TopicCorrection{WorkspaceID: f.workspaceID, TopicID: topicID, ItemID: itemID, Membership: value, UpdatedAt: f.now})
		require.NoError(t, err)
		require.True(t, found)
	}
	correct(firstTopic, domain.TopicMembershipExclude)
	require.Empty(t, read(nil))
	require.Empty(t, read(&firstTopic))
	correct(secondTopic, domain.TopicMembershipInclude)
	restored := read(nil)
	require.Len(t, restored, 1)
	require.ElementsMatch(t, []uuid.UUID{secondTopic}, restored[0].Item.TopicIDs)
	require.Equal(t, published.Publications[0].BatchID, restored[0].Publication.BatchID)
	require.WithinDuration(t, f.now, restored[0].Publication.PublishedAt, 0)
	require.Len(t, read(&secondTopic), 1)
	correct(secondTopic, domain.TopicMembershipAutomatic)
	require.Empty(t, read(nil))
	correct(firstTopic, domain.TopicMembershipAutomatic)
	require.Len(t, read(nil), 1)
	found, err := f.store.SetVisibility(t.Context(), f.workspaceID, itemID, domain.ItemVisibilityHidden)
	require.NoError(t, err)
	require.True(t, found)
	require.Empty(t, read(nil))
	_, err = f.store.SetVisibility(t.Context(), f.workspaceID, itemID, domain.ItemVisibilityVisible)
	require.NoError(t, err)
	_, found, err = f.store.SetFeedback(t.Context(), f.workspaceID, itemID, domain.Feedback{Sentiment: "dislike", CreatedAt: f.now, UpdatedAt: f.now})
	require.NoError(t, err)
	require.True(t, found)
	require.Len(t, read(nil), 1)
	head, err := f.store.FeedHead(t.Context(), f.workspaceID)
	require.NoError(t, err)
	require.Equal(t, int64(1), head)
}

func TestSharedFeedConcurrentPublishersShareOneHistory(t *testing.T) {
	f := newFeedFixture(t)
	itemID := f.item(t)
	command := domain.FeedPublishCommand{WorkspaceID: f.workspaceID, PublishedAt: f.now, Publications: []domain.FeedPublication{{ItemID: itemID, Pipeline: "concurrent"}}}
	type outcome struct {
		result domain.FeedPublishResult
		err    error
	}
	outcomes := make(chan outcome, 8)
	for range 8 {
		go func() { result, err := f.store.PublishFeed(t.Context(), command); outcomes <- outcome{result, err} }()
	}
	total := 0
	for range 8 {
		value := testutil.Receive(t, outcomes)
		require.NoError(t, value.err)
		total += len(value.result.Publications)
		require.Equal(t, int64(1), value.result.HeadOrdinal)
	}
	require.Equal(t, 1, total)
}

func TestSharedFeedCandidatesReplaceAndCannotRestorePublishedItems(t *testing.T) {
	f := newFeedFixture(t)
	topicID := f.topic(t)
	first, second := f.item(t), f.item(t)
	jobID := uuid.New()
	conn, err := pgx.Connect(t.Context(), f.dsn)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, conn.Close(context.Background())) })
	_, err = conn.Exec(t.Context(), `INSERT INTO jobs(id,workspace_id,kind,queue,status,lease_owner,lease_expires_at)
 VALUES($1,$2,'prepare-feed','feed-preparation','running','current',$3)`, jobID, f.workspaceID, f.now.Add(time.Hour))
	require.NoError(t, err)
	require.NoError(t, repository.RequestFeedPreparationForTest(t.Context(), f.store, domain.FeedPreparationRequest{WorkspaceID: f.workspaceID, At: f.now}))
	inputs := domain.FeedCandidateInputs{TopicRevision: 1, CorrectionEpoch: 1, CandidateLimit: 50, RRFK: 60}
	replacement := domain.FeedCandidateReplacement{Work: domain.FeedPreparationWork{WorkspaceID: f.workspaceID, TopicID: topicID, RequestedVersion: 2, InputVersion: 1},
		Inputs: inputs, JobID: jobID, LeaseOwner: "current", At: f.now,
		Candidates: []domain.FeedCandidate{{ItemID: first}, {ItemID: second}}}
	require.NoError(t, f.store.ReplaceFeedCandidates(t.Context(), replacement))
	query := domain.FeedCandidateQuery{WorkspaceID: f.workspaceID, At: f.now, CandidateLimit: 50}
	candidates, err := f.store.ReadFeedCandidates(t.Context(), query)
	require.NoError(t, err)
	require.Len(t, candidates, 2)
	require.Equal(t, f.sourceID, candidates[0].SourceID)
	require.WithinDuration(t, f.now, candidates[0].ContentAt, 0)
	replacement.Candidates = replacement.Candidates[:1]
	require.NoError(t, f.store.ReplaceFeedCandidates(t.Context(), replacement))
	candidates, err = f.store.ReadFeedCandidates(t.Context(), query)
	require.NoError(t, err)
	require.Len(t, candidates, 1)
	_, err = f.store.PublishFeed(t.Context(), domain.FeedPublishCommand{WorkspaceID: f.workspaceID, PublishedAt: f.now,
		Publications: []domain.FeedPublication{{ItemID: first, MatchedTopicIDs: []uuid.UUID{topicID}, TopicInputs: map[uuid.UUID]domain.FeedCandidateInputs{topicID: inputs}, Pipeline: "saved"}}})
	require.NoError(t, err)
	require.NoError(t, f.store.ReplaceFeedCandidates(t.Context(), replacement))
	candidates, err = f.store.ReadFeedCandidates(t.Context(), query)
	require.NoError(t, err)
	require.Empty(t, candidates)
	replacement.LeaseOwner = "lost"
	require.ErrorIs(t, f.store.ReplaceFeedCandidates(t.Context(), replacement), domain.ErrConflict)
	require.NoError(t, repository.RequestFeedPreparationForTest(t.Context(), f.store, domain.FeedPreparationRequest{WorkspaceID: f.workspaceID, Invalidate: true, At: f.now}))
	replacement.LeaseOwner = "current"
	require.ErrorIs(t, f.store.ReplaceFeedCandidates(t.Context(), replacement), domain.ErrConflict)
	topic, found, err := f.store.GetTopic(t.Context(), f.workspaceID, topicID)
	require.NoError(t, err)
	require.True(t, found)
	topic.Enabled = false
	_, err = f.store.UpdateTopic(t.Context(), domain.TopicUpdate{Topic: topic})
	require.NoError(t, err)
	var count int
	require.NoError(t, conn.QueryRow(t.Context(), `SELECT count(*) FROM topic_candidate_state WHERE workspace_id=$1 AND topic_id=$2`, f.workspaceID, topicID).Scan(&count))
	require.Zero(t, count)

}

func TestSharedFeedRechecksTopicScopeBeforeAdmission(t *testing.T) {
	f := newFeedFixture(t)
	topicID, itemID := f.topic(t), f.item(t)
	inputs := f.readyTopic(t, topicID)
	conn, err := pgx.Connect(t.Context(), f.dsn)
	require.NoError(t, err)
	defer func() { require.NoError(t, conn.Close(context.Background())) }()
	_, err = conn.Exec(t.Context(), `INSERT INTO topic_item_kinds(topic_id,item_kind) VALUES($1,'video')`, topicID)
	require.NoError(t, err)
	result, err := f.store.PublishFeed(t.Context(), domain.FeedPublishCommand{WorkspaceID: f.workspaceID, PublishedAt: f.now,
		Publications: []domain.FeedPublication{{ItemID: itemID, MatchedTopicIDs: []uuid.UUID{topicID}, TopicInputs: inputs, Pipeline: "stale-scope"}}})
	require.NoError(t, err)
	require.Empty(t, result.Publications)
	require.Zero(t, result.HeadOrdinal)
}

func (f feedFixture) readyTopic(t *testing.T, topicID uuid.UUID) map[uuid.UUID]domain.FeedCandidateInputs {
	t.Helper()
	inputs := domain.FeedCandidateInputs{TopicRevision: 1, CorrectionEpoch: 1, CandidateLimit: 50, RRFK: 60}
	require.NoError(t, repository.RequestFeedPreparationForTest(t.Context(), f.store, domain.FeedPreparationRequest{WorkspaceID: f.workspaceID, TopicID: &topicID, At: f.now}))
	conn, err := pgx.Connect(t.Context(), f.dsn)
	require.NoError(t, err)
	defer func() { require.NoError(t, conn.Close(context.Background())) }()
	payload, err := json.Marshal(inputs)
	require.NoError(t, err)
	_, err = conn.Exec(t.Context(), `UPDATE topic_candidate_state SET ready_version=requested_version,prepared_input_version=input_version,inputs=$3 WHERE workspace_id=$1 AND topic_id=$2`, f.workspaceID, topicID, payload)
	require.NoError(t, err)
	return map[uuid.UUID]domain.FeedCandidateInputs{topicID: inputs}
}

func TestSharedFeedAdmissionWindowAndRollback(t *testing.T) {
	f := newFeedFixture(t)
	boundary, old, future, hidden, disliked, valid := f.item(t), f.item(t), f.item(t), f.item(t), f.item(t), f.item(t)
	conn, err := pgx.Connect(t.Context(), f.dsn)
	require.NoError(t, err)
	defer func() { require.NoError(t, conn.Close(context.Background())) }()
	_, err = conn.Exec(t.Context(), `UPDATE items SET published_at=CASE id WHEN $1 THEN $4::timestamptz-interval '30 days' WHEN $2 THEN $4::timestamptz-interval '30 days 1 microsecond' WHEN $3 THEN $4::timestamptz+interval '1 second' END WHERE id=ANY($5)`, boundary, old, future, f.now, []uuid.UUID{boundary, old, future})
	require.NoError(t, err)
	_, err = f.store.SetVisibility(t.Context(), f.workspaceID, hidden, domain.ItemVisibilityHidden)
	require.NoError(t, err)
	_, _, err = f.store.SetFeedback(t.Context(), f.workspaceID, disliked, domain.Feedback{Sentiment: "dislike", CreatedAt: f.now, UpdatedAt: f.now})
	require.NoError(t, err)
	command := domain.FeedPublishCommand{WorkspaceID: f.workspaceID, PublishedAt: f.now}
	for _, id := range []uuid.UUID{boundary, old, future, hidden, disliked, valid, valid} {
		command.Publications = append(command.Publications, domain.FeedPublication{ItemID: id, Pipeline: "boundary"})
	}
	result, err := f.store.PublishFeed(t.Context(), command)
	require.NoError(t, err)
	require.Len(t, result.Publications, 2)
	require.Equal(t, boundary, result.Publications[0].ItemID)
	require.Equal(t, valid, result.Publications[1].ItemID)
	next, err := f.store.PublishFeed(t.Context(), domain.FeedPublishCommand{WorkspaceID: f.workspaceID, PublishedAt: f.now.Add(time.Second), Publications: []domain.FeedPublication{{ItemID: future, Pipeline: "future"}}})
	require.NoError(t, err)
	require.Len(t, next.Publications, 1)
	require.Equal(t, int64(3), next.HeadOrdinal)
	// A failing canonical handoff must roll back both publication and head allocation.
	fresh := f.item(t)
	_, err = conn.Exec(t.Context(), `ALTER TABLE item_projection_state ADD CONSTRAINT fixture_reject CHECK(item_id<>'`+fresh.String()+`') NOT VALID`)
	require.NoError(t, err)
	_, err = f.store.PublishFeed(t.Context(), domain.FeedPublishCommand{WorkspaceID: f.workspaceID, PublishedAt: f.now, Publications: []domain.FeedPublication{{ItemID: fresh, Pipeline: "rollback"}}})
	require.Error(t, err)
	head, err := f.store.FeedHead(t.Context(), f.workspaceID)
	require.NoError(t, err)
	require.Equal(t, int64(3), head)
	var exists bool
	require.NoError(t, conn.QueryRow(t.Context(), `SELECT EXISTS(SELECT 1 FROM feed_publications WHERE item_id=$1)`, fresh).Scan(&exists))
	require.False(t, exists)
}

func TestSharedFeedRejectsInvalidatedPreparedMatches(t *testing.T) {
	f := newFeedFixture(t)
	topicID, itemID := f.topic(t), f.item(t)
	inputs := f.readyTopic(t, topicID)
	require.NoError(t, repository.RequestFeedPreparationForTest(t.Context(), f.store, domain.FeedPreparationRequest{WorkspaceID: f.workspaceID, TopicID: &topicID, Invalidate: true, At: f.now}))
	result, err := f.store.PublishFeed(t.Context(), domain.FeedPublishCommand{WorkspaceID: f.workspaceID, PublishedAt: f.now,
		Publications: []domain.FeedPublication{{ItemID: itemID, MatchedTopicIDs: []uuid.UUID{topicID}, TopicInputs: inputs, Pipeline: "stale-inputs"}}})
	require.NoError(t, err)
	require.Empty(t, result.Publications)
	require.Zero(t, result.HeadOrdinal)
}

func TestSharedFeedFallbackExcludesPublishedBeforeLimitAndReusesHistoryAfterRestart(t *testing.T) {
	f := newFeedFixture(t)
	first, second := f.item(t), f.item(t)
	result, err := f.store.PublishFeed(t.Context(), domain.FeedPublishCommand{WorkspaceID: f.workspaceID, PublishedAt: f.now, Publications: []domain.FeedPublication{{ItemID: first, Pipeline: "original"}}})
	require.NoError(t, err)
	require.Len(t, result.Publications, 1)
	rows, err := f.store.RecentUnpublishedFeed(t.Context(), domain.FeedCandidateQuery{WorkspaceID: f.workspaceID, At: f.now, CandidateLimit: 1})
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.Equal(t, second, rows[0].ID)
	replica, err := repository.Open(t.Context(), f.dsn)
	require.NoError(t, err)
	defer replica.Close()
	page, err := replica.ReadFeedPublications(t.Context(), domain.FeedPublicationQuery{WorkspaceID: f.workspaceID, HeadOrdinal: 1, Limit: 25})
	require.NoError(t, err)
	require.Len(t, page, 1)
	require.Equal(t, result.Publications[0].BatchID, page[0].Publication.BatchID)
	foreign := uuid.New()
	require.NoError(t, f.store.EnsureWorkspace(t.Context(), foreign))
	empty, err := replica.ReadFeedPublications(t.Context(), domain.FeedPublicationQuery{WorkspaceID: foreign, HeadOrdinal: 1, Limit: 25})
	require.NoError(t, err)
	require.Empty(t, empty)
	duplicate, err := replica.PublishFeed(t.Context(), domain.FeedPublishCommand{WorkspaceID: foreign, PublishedAt: f.now, Publications: []domain.FeedPublication{{ItemID: first, Pipeline: "foreign"}}})
	require.NoError(t, err)
	require.Empty(t, duplicate.Publications)
}

func TestSharedFeedTraversesHistoryAcrossRefreshAndSourceRediscovery(t *testing.T) {
	f := newFeedFixture(t)
	command := domain.FeedPublishCommand{WorkspaceID: f.workspaceID, PublishedAt: f.now}
	for range 28 {
		command.Publications = append(command.Publications, domain.FeedPublication{ItemID: f.item(t), Pipeline: "original"})
	}
	_, err := f.store.PublishFeed(t.Context(), command)
	require.NoError(t, err)
	reader := feed.New(feed.Config{History: f.store, WorkspaceID: f.workspaceID, Now: func() time.Time { return f.now.AddDate(1, 0, 0) }})
	first, err := reader.Read(t.Context(), domain.FeedReadQuery{})
	require.NoError(t, err)
	require.Len(t, first.Items, 25)
	require.NotNil(t, first.NextCursor)
	newID := f.item(t)
	_, err = f.store.PublishFeed(t.Context(), domain.FeedPublishCommand{WorkspaceID: f.workspaceID, PublishedAt: f.now, Publications: []domain.FeedPublication{{ItemID: newID, Pipeline: "new"}}})
	require.NoError(t, err)
	older, err := reader.Read(t.Context(), domain.FeedReadQuery{Snapshot: first.Snapshot, Cursor: *first.NextCursor})
	require.NoError(t, err)
	require.Len(t, older.Items, 3)
	require.Nil(t, older.NextCursor)
	require.Equal(t, command.Publications[25].ItemID, older.Items[0].Item.ID)
	sourceID := uuid.New()
	require.NoError(t, f.store.Create(t.Context(), domain.Source{ID: sourceID, WorkspaceID: f.workspaceID, Name: "Other Source", Config: domain.SourceConfig{Kind: domain.SourceKindRSS, URL: "https://other.example/feed"}, Enabled: true, PollIntervalMinutes: 60, CreatedAt: f.now, UpdatedAt: f.now}))
	original, found, err := f.store.GetItem(t.Context(), f.workspaceID, command.Publications[0].ItemID)
	require.NoError(t, err)
	require.True(t, found)
	original.ID = uuid.New()
	stored, _, err := f.store.UpsertItem(t.Context(), original, domain.SourceEntry{SourceID: sourceID, ExternalID: "rediscovered", URL: original.URL, DiscoveredAt: f.now, UpdatedAt: f.now, Details: domain.SourceEntryDetails{Type: domain.ItemKindArticle, Article: &domain.SourceArticleDetails{Description: "Other"}}})
	require.NoError(t, err)
	require.Equal(t, command.Publications[0].ItemID, stored.ItemID)
	repeated, err := f.store.PublishFeed(t.Context(), domain.FeedPublishCommand{WorkspaceID: f.workspaceID, PublishedAt: f.now, Publications: []domain.FeedPublication{{ItemID: stored.ItemID, Pipeline: "rediscovered"}}})
	require.NoError(t, err)
	require.Empty(t, repeated.Publications)
	latest, err := reader.Read(t.Context(), domain.FeedReadQuery{})
	require.NoError(t, err)
	require.Equal(t, newID, latest.Items[0].Item.ID)
}

func TestPublicationIsIncludedInCanonicalProjectionSnapshot(t *testing.T) {
	f := newFeedFixture(t)
	itemID := f.item(t)
	_, err := f.store.PublishFeed(t.Context(), domain.FeedPublishCommand{WorkspaceID: f.workspaceID, PublishedAt: f.now, Publications: []domain.FeedPublication{{ItemID: itemID, Pipeline: "projection"}}})
	require.NoError(t, err)
	work, found, err := f.store.LoadItemProjection(t.Context(), f.workspaceID, itemID)
	require.NoError(t, err)
	require.True(t, found)
	data, err := json.Marshal(work.Projection)
	require.NoError(t, err)
	var fields map[string]any
	require.NoError(t, json.Unmarshal(data, &fields))
	require.Equal(t, true, fields["Published"])
	require.Equal(t, float64(2), fields["Version"])
	_, _, err = f.store.SetFeedback(t.Context(), f.workspaceID, itemID, domain.Feedback{Sentiment: "like", CreatedAt: f.now, UpdatedAt: f.now})
	require.NoError(t, err)
	work, found, err = f.store.LoadItemProjection(t.Context(), f.workspaceID, itemID)
	require.NoError(t, err)
	require.True(t, found)
	data, err = json.Marshal(work.Projection)
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(data, &fields))
	require.Equal(t, true, fields["Published"])
	require.Equal(t, float64(3), fields["Version"])
}

func TestServingProjectionCompletionDurablyRequestsFeedPreparation(t *testing.T) {
	f := newFeedFixture(t)
	require.NoError(t, f.store.EnsureDefaultEmbeddingModel(t.Context(), testutil.DefaultEmbeddingDescriptor(), f.now))
	_, err := f.store.GetOrCreateActive(t.Context(), f.workspaceID, domain.DefaultEmbeddingModelID, f.now)
	require.NoError(t, err)
	topicID := f.topic(t)
	f.readyTopic(t, topicID)
	itemID := f.item(t)
	work, found, err := f.store.LoadItemProjection(t.Context(), f.workspaceID, itemID)
	require.NoError(t, err)
	require.True(t, found)
	completed, err := f.store.CompleteItemProjection(t.Context(), domain.ItemProjectionDelivery{WorkspaceID: f.workspaceID, ItemID: itemID, ModelID: domain.DefaultEmbeddingModelID, Revision: work.Projection.Revision})
	require.NoError(t, err)
	require.True(t, completed)
	due, err := f.store.DueFeedWorkspaces(t.Context(), domain.FeedPreparationDueQuery{At: f.now.Add(2 * time.Minute), Limit: 100})
	require.NoError(t, err)
	require.Equal(t, []uuid.UUID{f.workspaceID}, due)
	_, err = f.store.LoadFeedPreparationWork(t.Context(), domain.FeedPreparationQuery{WorkspaceID: f.workspaceID, At: f.now.Add(2 * time.Minute), Limit: 8})
	require.NoError(t, err)
	conn, err := pgx.Connect(t.Context(), f.dsn)
	require.NoError(t, err)
	defer func() { require.NoError(t, conn.Close(context.Background())) }()
	var requested, ready int64
	var dirty *time.Time
	require.NoError(t, conn.QueryRow(t.Context(), `SELECT requested_version,ready_version,first_dirty_at FROM topic_candidate_state WHERE workspace_id=$1 AND topic_id=$2`, f.workspaceID, topicID).Scan(&requested, &ready, &dirty))
	require.Greater(t, requested, ready)
	require.NotNil(t, dirty)
	repeated, err := f.store.CompleteItemProjection(t.Context(), domain.ItemProjectionDelivery{WorkspaceID: f.workspaceID, ItemID: itemID, ModelID: domain.DefaultEmbeddingModelID, Revision: work.Projection.Revision})
	require.NoError(t, err)
	require.False(t, repeated)
}

func TestPublicationRacingModelCutoverCannotLeaveUnmarkedTarget(t *testing.T) {
	f := newFeedFixture(t)
	require.NoError(t, f.store.EnsureDefaultEmbeddingModel(t.Context(), testutil.DefaultEmbeddingDescriptor(), f.now))
	_, err := f.store.GetOrCreateActive(t.Context(), f.workspaceID, domain.DefaultEmbeddingModelID, f.now)
	require.NoError(t, err)
	itemID := f.item(t)
	work, found, err := f.store.LoadItemProjection(t.Context(), f.workspaceID, itemID)
	require.NoError(t, err)
	require.True(t, found)
	_, err = f.store.CompleteItemProjection(t.Context(), domain.ItemProjectionDelivery{WorkspaceID: f.workspaceID, ItemID: itemID, ModelID: domain.DefaultEmbeddingModelID, Revision: work.Projection.Revision})
	require.NoError(t, err)
	target, found, err := f.store.EmbeddingModels().Get(t.Context(), domain.DefaultEmbeddingModelID)
	require.NoError(t, err)
	require.True(t, found)
	target.ID = uuid.New()
	target.Name = "Cutover fixture"
	require.NoError(t, f.store.EmbeddingModels().Create(t.Context(), target))
	jobID := enqueueTransitionJob(t, f.store, f.workspaceID, f.now)
	_, err = f.store.Begin(t.Context(), domain.ModelTransitionStart{WorkspaceID: f.workspaceID, ModelID: target.ID, JobID: jobID, StartedAt: f.now})
	require.NoError(t, err)
	_, err = f.store.CompleteItemProjection(t.Context(), domain.ItemProjectionDelivery{WorkspaceID: f.workspaceID, ItemID: itemID, ModelID: target.ID, Revision: work.Projection.Revision})
	require.NoError(t, err)
	start := make(chan struct{})
	type published struct {
		result domain.FeedPublishResult
		err    error
	}
	publishedResult := make(chan published, 1)
	activated := make(chan error, 1)
	go func() {
		<-start
		result, err := f.store.PublishFeed(t.Context(), domain.FeedPublishCommand{WorkspaceID: f.workspaceID, PublishedAt: f.now, Publications: []domain.FeedPublication{{ItemID: itemID, ModelID: domain.DefaultEmbeddingModelID, Pipeline: "cutover-race"}}})
		publishedResult <- published{result, err}
	}()
	go func() {
		<-start
		activated <- f.store.Activate(t.Context(), domain.ModelTransitionCutover{WorkspaceID: f.workspaceID, ModelID: target.ID, JobID: jobID, ActivatedAt: f.now})
	}()
	close(start)
	publication := testutil.Receive(t, publishedResult)
	activationErr := testutil.Receive(t, activated)
	require.NoError(t, publication.err)
	if len(publication.result.Publications) > 0 {
		require.ErrorIs(t, activationErr, domain.ErrConflict)
		pending, err := f.store.ListPendingModelProjections(t.Context(), domain.ModelProjectionQuery{WorkspaceID: f.workspaceID, ModelID: target.ID, Limit: 10})
		require.NoError(t, err)
		require.Len(t, pending, 1)
	} else {
		require.NoError(t, activationErr)
	}
}

func TestFeedPreparationCoalescesFirstChangeAndLoadsEightOfFiftyTopics(t *testing.T) {
	f := newFeedFixture(t)
	for range 50 {
		f.topic(t)
	}
	require.NoError(t, repository.RequestFeedPreparationForTest(t.Context(), f.store, domain.FeedPreparationRequest{WorkspaceID: f.workspaceID, At: f.now}))
	for n := 1; n < 60; n++ {
		require.NoError(t, repository.RequestFeedPreparationForTest(t.Context(), f.store, domain.FeedPreparationRequest{WorkspaceID: f.workspaceID, At: f.now.Add(time.Duration(n) * time.Second)}))
	}
	early, err := f.store.LoadFeedPreparationWork(t.Context(), domain.FeedPreparationQuery{WorkspaceID: f.workspaceID, At: f.now.Add(59 * time.Second), Limit: 8})
	require.NoError(t, err)
	require.Empty(t, early)
	due, err := f.store.DueFeedWorkspaces(t.Context(), domain.FeedPreparationDueQuery{At: f.now.Add(time.Minute), Limit: 100})
	require.NoError(t, err)
	require.Equal(t, []uuid.UUID{f.workspaceID}, due)
	chunk, err := f.store.LoadFeedPreparationWork(t.Context(), domain.FeedPreparationQuery{WorkspaceID: f.workspaceID, At: f.now.Add(time.Minute), Limit: 8})
	require.NoError(t, err)
	require.Len(t, chunk, 8)
	require.Equal(t, int64(61), chunk[0].RequestedVersion)
	firstIDs := make([]uuid.UUID, 0, len(chunk))
	for _, work := range chunk {
		firstIDs = append(firstIDs, work.TopicID)
	}
	conn, err := pgx.Connect(t.Context(), f.dsn)
	require.NoError(t, err)
	defer func() { require.NoError(t, conn.Close(context.Background())) }()
	_, err = conn.Exec(t.Context(), `UPDATE topic_candidate_state SET ready_version=$3,prepared_at=$2 WHERE topic_id=ANY($1)`, firstIDs, f.now.Add(time.Minute), chunk[0].RequestedVersion)
	require.NoError(t, err)
	require.NoError(t, repository.RequestFeedPreparationForTest(t.Context(), f.store, domain.FeedPreparationRequest{WorkspaceID: f.workspaceID, At: f.now.Add(time.Minute)}))
	next, err := f.store.LoadFeedPreparationWork(t.Context(), domain.FeedPreparationQuery{WorkspaceID: f.workspaceID, At: f.now.Add(time.Minute), Limit: 8})
	require.NoError(t, err)
	require.Len(t, next, 8)
	for _, work := range next {
		require.NotContains(t, firstIDs, work.TopicID, "newly dirtied prepared Topics must not starve unprepared Topics")
	}

	head, err := f.store.FeedHead(t.Context(), f.workspaceID)
	require.NoError(t, err)
	require.Zero(t, head)
}

func TestNewTopicPersistsPreparationRequestWithoutAdvisoryOrEnqueue(t *testing.T) {
	f := newFeedFixture(t)
	f.topic(t)
	due, err := f.store.DueFeedWorkspaces(t.Context(), domain.FeedPreparationDueQuery{At: f.now.Add(time.Minute), Limit: 100})
	require.NoError(t, err)
	require.Equal(t, []uuid.UUID{f.workspaceID}, due)
}

func TestInitialServingModelRequestsBoundedRecoveryForPreviouslyStoredItems(t *testing.T) {
	f := newFeedFixture(t)
	f.item(t)
	require.NoError(t, f.store.EnsureDefaultEmbeddingModel(t.Context(), testutil.DefaultEmbeddingDescriptor(), f.now))
	_, err := f.store.GetOrCreateActive(t.Context(), f.workspaceID, domain.DefaultEmbeddingModelID, f.now)
	require.NoError(t, err)
	pending, err := f.store.ListPendingItemProjections(t.Context(), f.workspaceID, nil, 100)
	require.NoError(t, err)
	require.Empty(t, pending, "runtime construction must not bulk-seed existing content")
	page, err := f.store.ReconcileModelStates(t.Context(), domain.ModelReconciliationQuery{Limit: 100})
	require.NoError(t, err)
	require.Equal(t, []domain.ModelReconciliationRequest{{WorkspaceID: f.workspaceID, ModelID: domain.DefaultEmbeddingModelID}}, page.Requests)
}

func TestFeedPreparationSchedulesFutureAdmissionAndCandidateExpiryWithoutIngestion(t *testing.T) {
	f := newFeedFixture(t)
	topicID := f.topic(t)
	current, future := f.item(t), f.item(t)
	conn, err := pgx.Connect(t.Context(), f.dsn)
	require.NoError(t, err)
	defer func() { require.NoError(t, conn.Close(context.Background())) }()
	futureAt := f.now.Add(2 * time.Minute)
	_, err = conn.Exec(t.Context(), `UPDATE items SET published_at=$2 WHERE id=$1`, future, futureAt)
	require.NoError(t, err)
	jobID := uuid.New()
	_, err = conn.Exec(t.Context(), `INSERT INTO jobs(id,workspace_id,kind,queue,status,lease_owner,lease_expires_at) VALUES($1,$2,'prepare-feed','feed-preparation','running','clock-fixture',$3)`, jobID, f.workspaceID, f.now.Add(time.Hour))
	require.NoError(t, err)
	prepare := func(at time.Time) {
		work, err := f.store.LoadFeedPreparationWork(t.Context(), domain.FeedPreparationQuery{WorkspaceID: f.workspaceID, At: at, Limit: 8})
		require.NoError(t, err)
		require.Len(t, work, 1)
		require.NoError(t, f.store.ReplaceFeedCandidates(t.Context(), domain.FeedCandidateReplacement{Work: work[0], Inputs: domain.FeedCandidateInputs{TopicRevision: 1, CorrectionEpoch: 1, CandidateLimit: 50, RRFK: 60}, Candidates: []domain.FeedCandidate{{ItemID: current}, {ItemID: future}}, JobID: jobID, LeaseOwner: "clock-fixture", At: at, NextEligibleAt: work[0].NextEligibleAt}))
	}
	prepare(f.now.Add(time.Minute))
	rows, err := f.store.ReadFeedCandidates(t.Context(), domain.FeedCandidateQuery{WorkspaceID: f.workspaceID, At: f.now.Add(time.Minute), CandidateLimit: 50})
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.Equal(t, current, rows[0].ItemID)
	var next *time.Time
	require.NoError(t, conn.QueryRow(t.Context(), `SELECT next_eligible_at FROM topic_candidate_state WHERE topic_id=$1`, topicID).Scan(&next))
	require.NotNil(t, next)
	require.WithinDuration(t, futureAt, *next, 0)
	prepare(futureAt)
	rows, err = f.store.ReadFeedCandidates(t.Context(), domain.FeedCandidateQuery{WorkspaceID: f.workspaceID, At: futureAt, CandidateLimit: 50})
	require.NoError(t, err)
	require.Len(t, rows, 2)
	expiredAt := f.now.AddDate(0, 0, 30).Add(time.Microsecond)
	prepare(expiredAt)
	rows, err = f.store.ReadFeedCandidates(t.Context(), domain.FeedCandidateQuery{WorkspaceID: f.workspaceID, At: expiredAt, CandidateLimit: 50})
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.Equal(t, future, rows[0].ItemID)
}

func TestSharedFeedDoesNotConvertStaleTopicAdmissionIntoChronologicalPublication(t *testing.T) {
	f := newFeedFixture(t)
	topicID, itemID := f.topic(t), f.item(t)
	inputs := f.readyTopic(t, topicID)
	topic, found, err := f.store.GetTopic(t.Context(), f.workspaceID, topicID)
	require.NoError(t, err)
	require.True(t, found)
	topic.Enabled = false
	_, err = f.store.UpdateTopic(t.Context(), domain.TopicUpdate{Topic: topic})
	require.NoError(t, err)
	result, err := f.store.PublishFeed(t.Context(), domain.FeedPublishCommand{WorkspaceID: f.workspaceID, PublishedAt: f.now, Publications: []domain.FeedPublication{{ItemID: itemID, MatchedTopicIDs: []uuid.UUID{topicID}, TopicInputs: inputs, Pipeline: "stale-topic"}}})
	require.NoError(t, err)
	require.Empty(t, result.Publications)
	require.Zero(t, result.HeadOrdinal)
}
