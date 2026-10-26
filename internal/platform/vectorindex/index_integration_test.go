//go:build integration

package vectorindex_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/ethernity/obsidian-topic-stream/internal/domain"
	"github.com/ethernity/obsidian-topic-stream/internal/platform/vectorindex"
	"github.com/ethernity/obsidian-topic-stream/internal/service/ranking"
	"github.com/ethernity/obsidian-topic-stream/internal/testutil"
	"github.com/google/uuid"
	"github.com/qdrant/go-client/qdrant"
	"github.com/stretchr/testify/require"
)

func TestQdrantIndexRoundTripAndWorkspaceIsolation(t *testing.T) {
	fixture := newIndexFixture(t)
	index := fixture.index

	firstWorkspace, secondWorkspace := uuid.New(), uuid.New()
	firstItem, secondItem := uuid.New(), uuid.New()
	revision, topicID := uuid.New(), uuid.New()
	now := time.Date(2026, time.September, 29, 12, 0, 0, 0, time.UTC)
	comment := "deep and practical"
	require.NoError(t, index.UpsertProjection(t.Context(), domain.ItemProjection{
		Revision: revision,
		Item: domain.Item{
			ID: firstItem, WorkspaceID: firstWorkspace, SourceIDs: []uuid.UUID{uuid.New()},
			Kind: domain.ItemKindArticle, Title: "PostgreSQL query planner", ContentMarkdown: "MVCC internals",
			DiscoveredAt: now,
		},
		Feedback: &domain.Feedback{Sentiment: "like", Comment: &comment, UpdatedAt: now},
		TopicCorrections: []domain.TopicCorrectionProjection{{
			TopicID: topicID, Membership: domain.TopicMembershipInclude, CorrectionEpoch: 7,
		}},
	}, []float32{1, 0, 0}))
	require.NoError(t, index.UpsertProjection(t.Context(), domain.ItemProjection{
		Revision: uuid.New(),
		Item: domain.Item{
			ID: secondItem, WorkspaceID: secondWorkspace, SourceIDs: []uuid.UUID{uuid.New()},
			Kind: domain.ItemKindArticle, Title: "PostgreSQL query planner", ContentMarkdown: "Other workspace",
			DiscoveredAt: now,
		},
	}, []float32{1, 0, 0}))

	dense, err := index.QueryDense(t.Context(), []float32{1, 0, 0}, vectorindex.Filter{WorkspaceID: firstWorkspace}, 10)
	require.NoError(t, err)
	require.Equal(t, []uuid.UUID{firstItem}, hitIDs(dense))
	sparse, err := index.QuerySparse(t.Context(), "PostgreSQL planner", vectorindex.Filter{WorkspaceID: firstWorkspace}, 10)
	require.NoError(t, err)
	require.Equal(t, []uuid.UUID{firstItem}, hitIDs(sparse))

	points, err := fixture.client.Get(t.Context(), &qdrant.GetPoints{
		CollectionName: fixture.collection, Ids: []*qdrant.PointId{qdrant.NewIDUUID(firstItem.String())},
		WithPayload: qdrant.NewWithPayload(true), WithVectors: qdrant.NewWithVectors(false),
	})
	require.NoError(t, err)
	require.Len(t, points, 1)
	payload := points[0].Payload
	require.Equal(t, revision.String(), payload["projectionRevision"].GetStringValue())
	require.Equal(t, "like", payload["feedback"].GetStructValue().Fields["sentiment"].GetStringValue())
	correction := payload["topicCorrections"].GetListValue().Values[0].GetStructValue().Fields
	require.Equal(t, topicID.String(), correction["topicId"].GetStringValue())
	require.Equal(t, int64(7), correction["correctionEpoch"].GetIntegerValue())
	exact, err := index.DenseVectors(t.Context(), firstWorkspace, []uuid.UUID{firstItem})
	require.NoError(t, err)
	require.Equal(t, []float32{1, 0, 0}, exact[firstItem])

	corrections, err := index.QueryTopicCorrections(t.Context(), ranking.TopicCorrectionIndexRequest{
		TopicID: topicID, CorrectionEpoch: 7, Membership: domain.TopicMembershipInclude,
		QueryVector: []float32{1, 0, 0},
		Filter:      ranking.IndexFilter{WorkspaceID: firstWorkspace}, Limit: 10,
	})
	require.NoError(t, err)
	require.Equal(t, []uuid.UUID{firstItem}, hitIDs(corrections))

	for range 4 {
		likedItem := uuid.New()
		require.NoError(t, index.UpsertProjection(t.Context(), domain.ItemProjection{
			Revision: uuid.New(),
			Item: domain.Item{
				ID: likedItem, WorkspaceID: firstWorkspace, SourceIDs: []uuid.UUID{uuid.New()},
				Kind: domain.ItemKindArticle, Title: "Related planner article", DiscoveredAt: now,
			},
			Feedback: &domain.Feedback{Sentiment: "like", Comment: &comment, UpdatedAt: now},
		}, []float32{0.9, 0.1, 0}))
	}
	personal, err := index.RetrievePersonal(t.Context(), ranking.PersonalIndexRequest{
		CandidateIDs: []uuid.UUID{firstItem},
		Filter:       ranking.IndexFilter{WorkspaceID: firstWorkspace}, NeighborLimit: 12,
		Direct: map[uuid.UUID]domain.Feedback{
			firstItem: {Sentiment: "like", UpdatedAt: now},
		},
	})
	require.NoError(t, err)
	require.Len(t, personal.Positive, 4, "five Likes activate after the candidate itself is excluded")
	require.Empty(t, personal.Negative)
	require.Equal(t, "like", personal.Direct[firstItem].Sentiment)
	for _, neighborhood := range personal.Positive {
		require.Nil(t, neighborhood.Feedback.Feedback.Comment, "personal kNN does not retrieve feedback comments")
	}

}

func TestConcurrentCollectionCreatorsObserveOnePersistentIncarnation(t *testing.T) {
	f := newIndexFixture(t)
	require.NoError(t, f.client.DeleteCollection(t.Context(), f.collection))
	type observation struct {
		incarnation uuid.UUID
		err         error
	}
	results := make(chan observation, 8)
	start := make(chan struct{})
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	for range 8 {
		go func() {
			<-start
			index, err := vectorindex.New(f.client, f.collection, 3)
			if err == nil {
				err = index.Ensure(ctx)
			}
			var marker uuid.UUID
			if err == nil {
				marker, err = index.CollectionIncarnation(ctx)
			}
			results <- observation{incarnation: marker, err: err}
		}()
	}
	close(start)
	var marker uuid.UUID
	for range 8 {
		observed := testutil.Receive(t, results)
		require.NoError(t, observed.err)
		require.NotEqual(t, uuid.Nil, observed.incarnation)
		if marker == uuid.Nil {
			marker = observed.incarnation
		}
		require.Equal(t, marker, observed.incarnation)
	}
	require.NoError(t, f.client.DeleteCollection(t.Context(), f.collection))
	require.NoError(t, f.index.Ensure(t.Context()))
	replaced, err := f.index.CollectionIncarnation(t.Context())
	require.NoError(t, err)
	require.NotEqual(t, marker, replaced)
	incompatible, err := vectorindex.New(f.client, f.collection, 4)
	require.NoError(t, err)
	require.ErrorContains(t, incompatible.Ensure(t.Context()), "dimensions")
	unchanged, err := f.index.CollectionIncarnation(t.Context())
	require.NoError(t, err)
	require.Equal(t, replaced, unchanged, "schema mismatch must not recreate the collection")
}

func TestQdrantQueriesApplyCombinedPayloadFilters(t *testing.T) {
	fixture := newIndexFixture(t)
	now := time.Date(2026, time.October, 1, 12, 0, 0, 0, time.UTC)
	workspaceID, sourceID := uuid.New(), uuid.New()
	from, until := now.Add(-time.Hour), now.Add(time.Hour)
	base := vectorindex.Document{
		WorkspaceID: workspaceID, SourceIDs: []uuid.UUID{uuid.New(), sourceID}, Kind: domain.ItemKindArticle,
		Text: "PostgreSQL planner", Dense: []float32{1, 0, 0}, PublishedAt: &now, DiscoveredAt: now,
	}
	var expected []uuid.UUID
	for _, publishedAt := range []time.Time{from, now, until} {
		document := base
		document.ItemID = uuid.New()
		document.PublishedAt = &publishedAt
		require.NoError(t, fixture.index.Upsert(t.Context(), document))
		expected = append(expected, document.ItemID)
	}
	for _, mutate := range []func(*vectorindex.Document){
		func(document *vectorindex.Document) { document.WorkspaceID = uuid.New() },
		func(document *vectorindex.Document) { document.SourceIDs = []uuid.UUID{uuid.New()} },
		func(document *vectorindex.Document) { document.Kind = domain.ItemKindPaper },
		func(document *vectorindex.Document) {
			earlier := from.Add(-time.Second)
			document.PublishedAt = &earlier
		},
		func(document *vectorindex.Document) { later := until.Add(time.Second); document.PublishedAt = &later },
		func(document *vectorindex.Document) { document.PublishedAt = nil },
	} {
		document := base
		document.ItemID = uuid.New()
		mutate(&document)
		require.NoError(t, fixture.index.Upsert(t.Context(), document))
	}
	filter := vectorindex.Filter{
		WorkspaceID: workspaceID, SourceIDs: []uuid.UUID{sourceID}, Kinds: []domain.ItemKind{domain.ItemKindArticle},
		PublishedAfter: &from, PublishedBefore: &until,
	}
	dense, err := fixture.index.QueryDense(t.Context(), []float32{1, 0, 0}, filter, 20)
	require.NoError(t, err)
	require.ElementsMatch(t, expected, hitIDs(dense))
	sparse, err := fixture.index.QuerySparse(t.Context(), "PostgreSQL planner", filter, 20)
	require.NoError(t, err)
	require.ElementsMatch(t, expected, hitIDs(sparse))
}

type indexFixture struct {
	index      *vectorindex.Index
	client     *qdrant.Client
	collection string
}

func newIndexFixture(t *testing.T) indexFixture {
	t.Helper()
	started := time.Now()
	client, err := vectorindex.NewClient(testutil.QdrantURL(t))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, client.Close()) })
	collection := "topic_stream_test_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	index, err := vectorindex.New(client, collection, 3)
	require.NoError(t, err)
	t.Cleanup(func() {
		cleanupStarted := time.Now()
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		exists, err := client.CollectionExists(ctx, collection)
		require.NoError(t, err)
		if exists {
			require.NoError(t, client.DeleteCollection(ctx, collection))
		}
		t.Logf("integration phase=collection_cleanup elapsed_ms=%d", time.Since(cleanupStarted).Milliseconds())
	})
	require.NoError(t, index.Ensure(t.Context()))
	t.Logf("integration phase=collection_setup elapsed_ms=%d", time.Since(started).Milliseconds())
	return indexFixture{index: index, client: client, collection: collection}
}

func hitIDs(hits []vectorindex.Hit) []uuid.UUID {
	result := make([]uuid.UUID, len(hits))
	for index, hit := range hits {
		result[index] = hit.ItemID
	}
	return result
}

func TestQdrantProjectionRejectsOlderDeliveryAndClearsOptionalPayload(t *testing.T) {
	fixture := newIndexFixture(t)
	now, topicID := time.Now().UTC(), uuid.New()
	value := domain.ItemProjection{Version: 1, Revision: uuid.New(),
		Item:             domain.Item{WorkspaceID: uuid.New(), ID: uuid.New(), Kind: domain.ItemKindArticle, Title: "Canonical", PublishedAt: &now, DiscoveredAt: now},
		Feedback:         &domain.Feedback{Sentiment: "like", UpdatedAt: now},
		TopicCorrections: []domain.TopicCorrectionProjection{{TopicID: topicID, Membership: domain.TopicMembershipInclude, CorrectionEpoch: 7}},
	}
	request := ranking.TopicCorrectionIndexRequest{TopicID: topicID, CorrectionEpoch: 7, Membership: domain.TopicMembershipInclude,
		QueryVector: []float32{1, 0, 0}, Filter: ranking.IndexFilter{WorkspaceID: value.Item.WorkspaceID}, Limit: 10}
	require.NoError(t, fixture.index.UpsertProjection(t.Context(), value, []float32{1, 0, 0}))
	hits, err := fixture.index.QueryTopicCorrections(t.Context(), request)
	require.NoError(t, err)
	require.Equal(t, []uuid.UUID{value.Item.ID}, hitIDs(hits))
	old := value
	value.Version, value.Revision, value.Published = 2, uuid.New(), true
	value.Feedback, value.TopicCorrections, value.Item.PublishedAt = nil, nil, nil
	// Full projection replacement must remove the optional fields too.
	require.NoError(t, fixture.index.UpsertProjection(t.Context(), value, []float32{1, 0, 0}))
	require.Error(t, fixture.index.UpsertProjection(t.Context(), old, []float32{0, 1, 0}))
	points, err := fixture.client.Get(t.Context(), &qdrant.GetPoints{CollectionName: fixture.collection, Ids: []*qdrant.PointId{qdrant.NewIDUUID(value.Item.ID.String())}, WithPayload: qdrant.NewWithPayload(true), WithVectors: qdrant.NewWithVectors(false)})
	require.NoError(t, err)
	require.Len(t, points, 1)
	require.True(t, points[0].Payload["published"].GetBoolValue())
	require.Equal(t, int64(2), points[0].Payload["projectionVersion"].GetIntegerValue())
	require.Equal(t, value.Revision.String(), points[0].Payload["projectionRevision"].GetStringValue())
	require.NotContains(t, points[0].Payload, "feedback")
	require.Empty(t, points[0].Payload["topicCorrections"].GetListValue().Values)
	require.NotContains(t, points[0].Payload, "publishedAt")
	hits, err = fixture.index.QueryTopicCorrections(t.Context(), request)
	require.NoError(t, err)
	require.Empty(t, hits)
	// Restore fields through the payload-only path, making its subsequent removal non-vacuous.
	value.Version, value.Revision = 3, uuid.New()
	value.Feedback, value.TopicCorrections, value.Item.PublishedAt = old.Feedback, old.TopicCorrections, old.Item.PublishedAt
	updated, err := fixture.index.UpdateProjectionPayload(t.Context(), value)
	require.NoError(t, err)
	require.True(t, updated)
	hits, err = fixture.index.QueryTopicCorrections(t.Context(), request)
	require.NoError(t, err)
	require.Equal(t, []uuid.UUID{value.Item.ID}, hitIDs(hits))
	points, err = fixture.client.Get(t.Context(), &qdrant.GetPoints{CollectionName: fixture.collection, Ids: []*qdrant.PointId{qdrant.NewIDUUID(value.Item.ID.String())}, WithPayload: qdrant.NewWithPayload(true), WithVectors: qdrant.NewWithVectors(false)})
	require.NoError(t, err)
	require.Equal(t, "like", points[0].Payload["feedback"].GetStructValue().Fields["sentiment"].GetStringValue())
	require.Contains(t, points[0].Payload, "publishedAt")
	value.Version, value.Revision = 4, uuid.New()
	value.Feedback, value.TopicCorrections, value.Item.PublishedAt = nil, nil, nil
	updated, err = fixture.index.UpdateProjectionPayload(t.Context(), value)
	require.NoError(t, err)
	require.True(t, updated)
	points, err = fixture.client.Get(t.Context(), &qdrant.GetPoints{CollectionName: fixture.collection, Ids: []*qdrant.PointId{qdrant.NewIDUUID(value.Item.ID.String())}, WithPayload: qdrant.NewWithPayload(true), WithVectors: qdrant.NewWithVectors(false)})
	require.NoError(t, err)
	require.NotContains(t, points[0].Payload, "feedback")
	require.Empty(t, points[0].Payload["topicCorrections"].GetListValue().Values)
	require.NotContains(t, points[0].Payload, "publishedAt")
	vectors, err := fixture.index.DenseVectors(t.Context(), value.Item.WorkspaceID, []uuid.UUID{value.Item.ID})
	require.NoError(t, err)
	require.Equal(t, []float32{1, 0, 0}, vectors[value.Item.ID])
	_, err = fixture.index.UpdateProjectionPayload(t.Context(), old)
	require.ErrorIs(t, err, domain.ErrConflict)
	changed := value
	changed.Version, changed.Item.Title = 5, "Changed text"
	updated, err = fixture.index.UpdateProjectionPayload(t.Context(), changed)
	require.NoError(t, err)
	require.False(t, updated)
}

func TestQdrantFeedExclusionPrecedesEveryCandidateLimit(t *testing.T) {
	fixture := newIndexFixture(t)
	workspaceID, topicID := uuid.New(), uuid.New()
	now := time.Now().UTC()
	var unpublished uuid.UUID
	for n := 0; n < 21; n++ {
		value := domain.ItemProjection{Version: 1, Revision: uuid.New(), Published: n < 20, Item: domain.Item{ID: uuid.New(), WorkspaceID: workspaceID, Kind: domain.ItemKindArticle, Title: "Synthetic planner", DiscoveredAt: now},
			TopicCorrections: []domain.TopicCorrectionProjection{{TopicID: topicID, Membership: domain.TopicMembershipInclude, CorrectionEpoch: 1}}}
		if n == 20 {
			unpublished = value.Item.ID
		}
		require.NoError(t, fixture.index.UpsertProjection(t.Context(), value, []float32{1, 0, 0}))
	}
	filter := ranking.IndexFilter{WorkspaceID: workspaceID, FeedCandidates: true}
	dense, err := fixture.index.QueryDense(t.Context(), []float32{1, 0, 0}, filter, 1)
	require.NoError(t, err)
	require.Equal(t, []uuid.UUID{unpublished}, hitIDs(dense))
	sparse, err := fixture.index.QuerySparse(t.Context(), "planner", filter, 1)
	require.NoError(t, err)
	require.Equal(t, []uuid.UUID{unpublished}, hitIDs(sparse))
	recent, err := fixture.index.QueryRecent(t.Context(), filter, 1)
	require.NoError(t, err)
	require.Equal(t, []uuid.UUID{unpublished}, hitIDs(recent))
	added, err := fixture.index.QueryTopicCorrections(t.Context(), ranking.TopicCorrectionIndexRequest{TopicID: topicID, CorrectionEpoch: 1, Membership: domain.TopicMembershipInclude, QueryVector: []float32{1, 0, 0}, Filter: filter, Limit: 1})
	require.NoError(t, err)
	require.Equal(t, []uuid.UUID{unpublished}, hitIDs(added))
	search, err := fixture.index.QueryDense(t.Context(), []float32{1, 0, 0}, ranking.IndexFilter{WorkspaceID: workspaceID}, 100)
	require.NoError(t, err)
	require.Len(t, search, 21)
}

func TestQdrantConcurrentFirstWritesKeepNewestCanonicalProjection(t *testing.T) {
	fixture := newIndexFixture(t)
	item := domain.Item{ID: uuid.New(), WorkspaceID: uuid.New(), Title: "Concurrent first writes", Kind: domain.ItemKindArticle, DiscoveredAt: time.Now().UTC()}
	start := make(chan struct{})
	outcomes := make(chan error, 8)
	for version := int64(1); version <= 8; version++ {
		go func() {
			<-start
			outcomes <- fixture.index.UpsertProjection(t.Context(), domain.ItemProjection{Version: version, Revision: uuid.New(), Published: version == 8, Item: item}, []float32{1, 0, 0})
		}()
	}
	close(start)
	for range 8 {
		err := testutil.Receive(t, outcomes)
		if err != nil {
			require.ErrorIs(t, err, domain.ErrConflict)
		}
	}
	points, err := fixture.client.Get(t.Context(), &qdrant.GetPoints{CollectionName: fixture.collection, Ids: []*qdrant.PointId{qdrant.NewIDUUID(item.ID.String())}, WithPayload: qdrant.NewWithPayload(true), WithVectors: qdrant.NewWithVectors(false)})
	require.NoError(t, err)
	require.Len(t, points, 1)
	require.Equal(t, int64(8), points[0].Payload["projectionVersion"].GetIntegerValue())
	require.True(t, points[0].Payload["published"].GetBoolValue())
}

func TestQdrantPublishedFeedbackRemainsPersonalEvidence(t *testing.T) {
	fixture := newIndexFixture(t)
	workspaceID := uuid.New()
	now := time.Now().UTC()
	candidate := domain.ItemProjection{Version: 1, Revision: uuid.New(), Item: domain.Item{ID: uuid.New(), WorkspaceID: workspaceID, Kind: domain.ItemKindArticle, Title: "Candidate", DiscoveredAt: now}}
	require.NoError(t, fixture.index.UpsertProjection(t.Context(), candidate, []float32{1, 0, 0}))
	for range 5 {
		liked := candidate
		liked.Item.ID = uuid.New()
		liked.Published = true
		liked.Feedback = &domain.Feedback{Sentiment: "like", UpdatedAt: now}
		require.NoError(t, fixture.index.UpsertProjection(t.Context(), liked, []float32{1, 0, 0}))
	}
	personal, err := fixture.index.RetrievePersonal(t.Context(), ranking.PersonalIndexRequest{Filter: ranking.IndexFilter{WorkspaceID: workspaceID, FeedCandidates: true}, CandidateIDs: []uuid.UUID{candidate.Item.ID}, NeighborLimit: 12})
	require.NoError(t, err)
	require.Len(t, personal.Positive, 5)
}

func TestQdrantLegacyPointWaitsForCanonicalBackfill(t *testing.T) {
	fixture := newIndexFixture(t)
	value := domain.ItemProjection{Version: 1, Revision: uuid.New(), Item: domain.Item{ID: uuid.New(), WorkspaceID: uuid.New(), Title: "Legacy point", Kind: domain.ItemKindArticle, DiscoveredAt: time.Now().UTC()}}
	_, err := fixture.client.Upsert(t.Context(), &qdrant.UpsertPoints{CollectionName: fixture.collection, Wait: qdrant.PtrOf(true), Points: []*qdrant.PointStruct{{
		Id: qdrant.NewIDUUID(value.Item.ID.String()), Vectors: qdrant.NewVectorsMap(map[string]*qdrant.Vector{vectorindex.DenseVectorName: qdrant.NewVector(1, 0, 0), vectorindex.SparseVectorName: qdrant.NewVectorDocument(&qdrant.Document{Model: vectorindex.BM25Model, Text: value.Item.Title})}),
		Payload: map[string]*qdrant.Value{"workspaceId": qdrant.NewValueString(value.Item.WorkspaceID.String()), "discoveredAt": qdrant.NewValueString(value.Item.DiscoveredAt.Format(time.RFC3339Nano))},
	}}})
	require.NoError(t, err)
	filter := ranking.IndexFilter{WorkspaceID: value.Item.WorkspaceID, FeedCandidates: true}
	before, err := fixture.index.QueryDense(t.Context(), []float32{1, 0, 0}, filter, 1)
	require.NoError(t, err)
	require.Empty(t, before)
	require.NoError(t, fixture.index.UpsertProjection(t.Context(), value, []float32{1, 0, 0}))
	after, err := fixture.index.QueryDense(t.Context(), []float32{1, 0, 0}, filter, 1)
	require.NoError(t, err)
	require.Equal(t, []uuid.UUID{value.Item.ID}, hitIDs(after))
}
