package ranking_test

import (
	"context"
	"testing"
	"time"

	"github.com/ethernity/obsidian-topic-stream/internal/domain"
	"github.com/ethernity/obsidian-topic-stream/internal/service/ranking"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

type retrievalIndexSpy struct {
	denseText  string
	sparseText string
	filter     ranking.IndexFilter
	limit      int
	hits       []ranking.IndexHit
}

// QueryDenseText returns the fixture's dense retrieval results.
func (s *retrievalIndexSpy) QueryDenseText(_ context.Context, text string, filter ranking.IndexFilter, limit int) ([]ranking.IndexHit, error) {
	s.denseText = text
	s.filter = filter
	s.limit = limit
	return append([]ranking.IndexHit(nil), s.hits...), nil
}

// QuerySparse returns the fixture's lexical retrieval results.
func (s *retrievalIndexSpy) QuerySparse(_ context.Context, text string, filter ranking.IndexFilter, limit int) ([]ranking.IndexHit, error) {
	s.sparseText = text
	s.filter = filter
	s.limit = limit
	return append([]ranking.IndexHit(nil), s.hits...), nil
}

func TestDenseRetrieverEmbedsProfileAndMapsIndexHits(t *testing.T) {
	t.Parallel()

	workspaceID := uuid.New()
	sourceID := uuid.New()
	itemID := uuid.New()
	publishedAt := time.Date(2026, time.September, 20, 12, 0, 0, 0, time.UTC)
	index := &retrievalIndexSpy{hits: []ranking.IndexHit{{
		ItemID: itemID, SourceID: sourceID, PublishedAt: &publishedAt,
		Embedding: []float32{0.4, 0.5}, Score: 0.82,
	}}}
	filter := ranking.IndexFilter{WorkspaceID: workspaceID, SourceIDs: []uuid.UUID{sourceID}, Kinds: []domain.ItemKind{domain.ItemKindPaper}}
	retriever := ranking.NewDenseRetriever(index, filter)

	candidates, err := retriever.Retrieve(context.Background(), ranking.Profile{Prefer: "database internals"}, 25)

	require.NoError(t, err)
	require.Equal(t, "dense", retriever.Name())
	require.Equal(t, "database internals", index.denseText)
	require.Equal(t, filter, index.filter)
	require.Equal(t, 25, index.limit)
	require.Len(t, candidates, 1)
	require.Equal(t, itemID.String(), candidates[0].ItemID)
	require.Equal(t, sourceID.String(), candidates[0].SourceID)
	require.Equal(t, publishedAt, candidates[0].PublishedAt)
	require.InDeltaSlice(t, []float64{0.4, 0.5}, candidates[0].Embedding, 0.000001)
	require.Equal(t, 0.82, candidates[0].Score)
	require.Equal(t, map[string]float64{"dense": 0.82}, candidates[0].Signals)
}

func TestSparseRetrieverSendsProfileTextToBM25Index(t *testing.T) {
	t.Parallel()

	index := &retrievalIndexSpy{hits: []ranking.IndexHit{{ItemID: uuid.New(), Score: 4.2}}}
	retriever := ranking.NewSparseRetriever(index, ranking.IndexFilter{WorkspaceID: uuid.New()})

	candidates, err := retriever.Retrieve(context.Background(), ranking.Profile{Prefer: "MVCC PostgreSQL"}, 40)

	require.NoError(t, err)
	require.Equal(t, "sparse", retriever.Name())
	require.Equal(t, "MVCC PostgreSQL", index.sparseText)
	require.Equal(t, 40, index.limit)
	require.Equal(t, 4.2, candidates[0].Signals["sparse"])
}
