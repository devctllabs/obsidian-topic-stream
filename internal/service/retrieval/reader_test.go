package retrieval_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/ethernity/obsidian-topic-stream/internal/domain"
	"github.com/ethernity/obsidian-topic-stream/internal/service/jobs"
	"github.com/ethernity/obsidian-topic-stream/internal/service/ranking"
	"github.com/ethernity/obsidian-topic-stream/internal/service/retrieval"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

type reindexQueueStub struct {
	request jobs.EnqueueRequest
	job     domain.Job
	err     error
}

type activeCatalogStub struct {
	generation domain.RetrievalIndex
	found      bool
}

// GetActive returns the fixture's serving generation and lookup result.
func (s activeCatalogStub) GetActive(context.Context, uuid.UUID) (domain.RetrievalIndex, bool, error) {
	return s.generation, s.found, nil
}

// Enqueue records the submitted work without executing a handler.
func (s *reindexQueueStub) Enqueue(_ context.Context, request jobs.EnqueueRequest) (domain.Job, error) {
	s.request = request
	return s.job, s.err
}

func TestReaderQueriesExistingGenerationWithoutEnsuringDerivedState(t *testing.T) {
	t.Parallel()

	workspaceID := uuid.New()
	generation := domain.RetrievalIndex{
		WorkspaceID: workspaceID,
		Generation:  3,
		Collection:  "existing-generation",
		Embedding: domain.EmbeddingSettings{
			Endpoint: "http://models/v1", Model: "current", Dimensions: 2,
		},
		Status:      domain.RetrievalIndexActive,
		Initialized: true, CollectionIncarnation: uuid.NameSpaceOID,
	}
	factory := &factoryStub{
		denseHits:  []ranking.IndexHit{{ItemID: uuid.New(), Score: 0.9}},
		sparseHits: []ranking.IndexHit{{ItemID: uuid.New(), Score: 0.8}},
	}
	reader, err := retrieval.NewReader(retrieval.ReaderConfig{Generation: generation, Factory: factory})
	require.NoError(t, err)

	publishedAfter := time.Now().Add(-24 * time.Hour)
	filter := ranking.IndexFilter{WorkspaceID: workspaceID, PublishedAfter: &publishedAfter}
	_, err = reader.QueryDenseText(t.Context(), "retrieval systems", filter, 50)
	require.NoError(t, err)
	_, err = reader.QuerySparse(t.Context(), "retrieval systems", filter, 50)
	require.NoError(t, err)
	_, err = reader.RetrievePersonal(t.Context(), ranking.PersonalIndexRequest{Filter: filter})
	require.NoError(t, err)

	require.Empty(t, factory.ensured, "read-only retrieval must not create or alter Qdrant collections")
}

func TestReadOnlySnapshotRejectsCollectionReplacementDuringQuery(t *testing.T) {
	factory := &factoryStub{replaceAfterQuery: true, denseHits: []ranking.IndexHit{{ItemID: uuid.New()}}}
	reader, err := retrieval.NewReader(retrieval.ReaderConfig{Factory: factory, Generation: domain.RetrievalIndex{
		WorkspaceID: uuid.New(), ModelID: uuid.New(), Generation: 1, Collection: "captured", Status: domain.RetrievalIndexActive,
		Initialized: true, CollectionIncarnation: uuid.NameSpaceOID, Embedding: domain.EmbeddingSettings{Dimensions: 3},
	}})
	require.NoError(t, err)
	hits, err := reader.QueryDenseText(t.Context(), "query", ranking.IndexFilter{}, 10)
	require.ErrorIs(t, err, domain.ErrRetrievalUnavailable)
	require.Empty(t, hits, "a changed instance must not expose partial query results")
	require.Empty(t, factory.ensured, "read-only validation must not mutate derived state")
}

func TestEnqueueRebuildUsesVersionedDurableJobContract(t *testing.T) {
	t.Parallel()

	workspaceID := uuid.New()
	modelID := uuid.New()
	embedding := domain.EmbeddingSettings{Endpoint: "http://models/v1", Model: "current", Dimensions: 1024}
	queue := &reindexQueueStub{job: domain.Job{ID: uuid.New(), Status: domain.JobStatusQueued}}
	maintenance, err := retrieval.NewMaintenance(retrieval.MaintenanceConfig{
		WorkspaceID: workspaceID,
		Catalog: activeCatalogStub{found: true, generation: domain.RetrievalIndex{
			WorkspaceID: workspaceID, ModelID: modelID, Generation: 2, SelectionGeneration: 17,
			Status: domain.RetrievalIndexActive, Embedding: embedding,
		}},
		Queue: queue,
	})
	require.NoError(t, err)

	job, err := maintenance.Rebuild(t.Context())

	require.NoError(t, err)
	require.Equal(t, queue.job, job)
	require.Equal(t, workspaceID, queue.request.WorkspaceID)
	require.Equal(t, retrieval.ReindexJobKind, queue.request.Definition.Kind)
	require.Equal(t, 3, queue.request.Definition.ArgumentsVersion)
	require.Equal(t, domain.JobQueueMaintenance, queue.request.Definition.Queue)
	payload, err := json.Marshal(queue.request.Arguments)
	require.NoError(t, err)
	require.JSONEq(t, `{"modelId":"`+modelID.String()+`","selectionGeneration":17,"force":true}`, string(payload))
	require.NotEmpty(t, queue.request.IdempotencyKey)
}

func TestExplicitRebuildCannotOverrideLatestModelSelection(t *testing.T) {
	t.Parallel()
	queue := &reindexQueueStub{}
	maintenance, err := retrieval.NewMaintenance(retrieval.MaintenanceConfig{
		WorkspaceID: uuid.New(), Queue: queue,
		Catalog: activeCatalogStub{found: true, generation: domain.RetrievalIndex{ModelID: uuid.New(), DesiredModelID: uuid.New()}},
	})
	require.NoError(t, err)
	_, err = maintenance.Rebuild(t.Context())
	require.ErrorIs(t, err, domain.ErrConflict)
	require.Empty(t, queue.request, "rebuild must not enqueue the older Serving Model over a newer selection")
}
