package indexing_test

import (
	"context"
	"sync"
	"testing"

	"github.com/ethernity/obsidian-topic-stream/internal/domain"

	"github.com/ethernity/obsidian-topic-stream/internal/service/indexing"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

type processorSpy struct {
	mu     sync.Mutex
	commit domain.ItemProjectionCommit
}

type pendingRepositoryStub struct {
	commits []domain.ItemProjectionCommit
}

// ListPendingItemProjections returns a copy of the fixture's pending Workspace commits.
func (s pendingRepositoryStub) ListPendingItemProjections(
	context.Context,
	uuid.UUID,
	*uuid.UUID,
	int,
) ([]domain.ItemProjectionCommit, error) {
	return append([]domain.ItemProjectionCommit(nil), s.commits...), nil
}

type feedInvalidatorSpy struct {
	mu      sync.Mutex
	itemIDs []uuid.UUID
}

// InvalidateFeed records the Item invalidation for assertions.
func (s *feedInvalidatorSpy) InvalidateFeed(_ uuid.UUID, itemID uuid.UUID) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.itemIDs = append(s.itemIDs, itemID)
}

func (s *feedInvalidatorSpy) contains(itemID uuid.UUID) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, current := range s.itemIDs {
		if current == itemID {
			return true
		}
	}
	return false
}

// DeliverItemProjection records the projection commit and returns the configured delivery outcome.
func (s *processorSpy) DeliverItemProjection(_ context.Context, commit domain.ItemProjectionCommit) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.commit = commit
	return nil
}

func (s *processorSpy) snapshot() domain.ItemProjectionCommit {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.commit
}

func TestIndexItemJobDeliversTheExactCommittedProjection(t *testing.T) {
	t.Parallel()

	workspaceID := uuid.New()
	commit := domain.ItemProjectionCommit{
		WorkspaceID: workspaceID, ItemID: uuid.New(), ProjectionRevision: uuid.New(),
	}
	queue := newJobQueue(t)
	processor := &processorSpy{}
	invalidation := &feedInvalidatorSpy{}
	service := newIndexingService(t, indexing.Config{
		WorkspaceID: workspaceID, Pending: pendingRepositoryStub{}, Jobs: queue,
		Processor: processor, Events: invalidation,
	})
	_, err := service.Enqueue(t.Context(), commit)
	require.NoError(t, err)
	require.NoError(t, queue.run(t, 0))
	require.Equal(t, commit, processor.snapshot())
	require.True(t, invalidation.contains(commit.ItemID))
}

func TestIndexItemRejectsACommitOutsideTheConfiguredWorkspace(t *testing.T) {
	t.Parallel()

	workspaceID := uuid.New()
	queue := newJobQueue(t)
	service := newIndexingService(t, indexing.Config{
		WorkspaceID: workspaceID, Pending: pendingRepositoryStub{}, Jobs: queue,
		Processor: &processorSpy{}, Events: &feedInvalidatorSpy{},
	})

	_, err := service.Enqueue(t.Context(), domain.ItemProjectionCommit{
		WorkspaceID: uuid.New(), ItemID: uuid.New(), ProjectionRevision: uuid.New(),
	})
	require.ErrorContains(t, err, "outside the configured workspace")
}

func TestRepairReenqueuesEveryPersistedPendingProjection(t *testing.T) {
	t.Parallel()

	workspaceID := uuid.New()
	commits := []domain.ItemProjectionCommit{
		{WorkspaceID: workspaceID, ItemID: uuid.New(), ProjectionRevision: uuid.New()},
		{WorkspaceID: workspaceID, ItemID: uuid.New(), ProjectionRevision: uuid.New()},
	}
	queue := newJobQueue(t)
	processor := &processorSpy{}
	service := newIndexingService(t, indexing.Config{
		WorkspaceID: workspaceID, Pending: pendingRepositoryStub{commits: commits}, Jobs: queue,
		Processor: processor, Events: &feedInvalidatorSpy{},
	})

	repaired, err := service.Repair(t.Context())

	require.NoError(t, err)
	require.Equal(t, 2, repaired)
	require.Len(t, queue.requests, 2)
	for i, request := range queue.requests {
		require.Equal(t, workspaceID, request.WorkspaceID)
		require.Contains(t, request.IdempotencyKey, commits[i].ProjectionRevision.String())
	}

}

func newIndexingService(t *testing.T, config indexing.Config) *indexing.Service {
	t.Helper()
	service, err := indexing.New(config)
	require.NoError(t, err)
	return service
}
