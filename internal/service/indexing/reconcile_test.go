package indexing_test

import (
	"context"
	"errors"
	"testing"

	"github.com/ethernity/obsidian-topic-stream/internal/domain"

	"github.com/ethernity/obsidian-topic-stream/internal/service/indexing"
	"github.com/ethernity/obsidian-topic-stream/internal/service/jobs"
	jobmocks "github.com/ethernity/obsidian-topic-stream/internal/service/jobs/mocks"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

type globalPending struct{ commits []domain.ItemProjectionCommit }

// ListPendingGlobalItemProjections returns the fixture's pending cross-Workspace commits.
func (s globalPending) ListPendingGlobalItemProjections(context.Context, domain.ProjectionRepairQuery) (domain.ProjectionRepairPage, error) {
	return domain.ProjectionRepairPage{Commits: s.commits}, nil
}
func TestGlobalProjectionReconcilerRecoversMissingEnqueue(t *testing.T) {
	commit := domain.ItemProjectionCommit{WorkspaceID: uuid.New(), ItemID: uuid.New(), ProjectionRevision: uuid.New()}
	queue := newJobQueue(t)
	reconciler := indexing.NewReconciler(globalPending{commits: []domain.ItemProjectionCommit{commit}}, queue)
	require.NoError(t, reconciler.Reconcile(t.Context()))
	require.NoError(t, reconciler.Reconcile(t.Context()))
	require.Len(t, queue.requests, 2)
	require.Equal(t, queue.requests[0], queue.requests[1])
	require.Equal(t, commit.WorkspaceID, queue.requests[0].WorkspaceID)
	require.Equal(t, indexing.JobKind, queue.requests[0].Definition.Kind)
	require.Contains(t, queue.requests[0].IdempotencyKey, commit.ProjectionRevision.String())

}

type repairPages struct {
	pages   []domain.ProjectionRepairPage
	queries []domain.ProjectionRepairQuery
}

// ListPendingGlobalItemProjections records bounded page requests, including pages with no new Jobs.
func (s *repairPages) ListPendingGlobalItemProjections(_ context.Context, query domain.ProjectionRepairQuery) (domain.ProjectionRepairPage, error) {
	s.queries = append(s.queries, query)
	return s.pages[len(s.queries)-1], nil
}

func TestReconcilerAdvancesPastActivePagesAndWrapsAfterEnd(t *testing.T) {
	cursor := &domain.ProjectionRepairCursor{WorkspaceID: uuid.New(), ItemID: uuid.New()}
	commit := domain.ItemProjectionCommit{WorkspaceID: cursor.WorkspaceID, ItemID: uuid.New(), ProjectionRevision: uuid.New()}
	pending := &repairPages{pages: []domain.ProjectionRepairPage{
		{NextCursor: cursor}, {Commits: []domain.ItemProjectionCommit{commit}}, {},
	}}
	queue := newJobQueue(t)
	reconciler := indexing.NewReconciler(pending, queue)
	for range 3 {
		require.NoError(t, reconciler.Reconcile(t.Context()))
	}
	require.Equal(t, []domain.ProjectionRepairQuery{{Limit: 100}, {Limit: 100, After: cursor}, {Limit: 100}}, pending.queries)
	require.Len(t, queue.requests, 1)
}

func TestRepairRetriesTheSamePageWhenEnqueueFails(t *testing.T) {
	commit := domain.ItemProjectionCommit{WorkspaceID: uuid.New(), ItemID: uuid.New(), ProjectionRevision: uuid.New()}
	page := domain.ProjectionRepairPage{Commits: []domain.ItemProjectionCommit{commit}, NextCursor: &domain.ProjectionRepairCursor{WorkspaceID: commit.WorkspaceID, ItemID: commit.ItemID}}
	pending := &repairPages{pages: []domain.ProjectionRepairPage{page, page}}
	queue := jobmocks.NewMockQueue(gomock.NewController(t))
	failed := errors.New("queue unavailable")
	gomock.InOrder(
		queue.EXPECT().Enqueue(gomock.Any(), gomock.Any()).Return(domain.Job{}, failed),
		queue.EXPECT().Enqueue(gomock.Any(), gomock.AssignableToTypeOf(jobs.EnqueueRequest{})).Return(domain.Job{}, nil),
	)
	reconciler := indexing.NewReconciler(pending, queue)
	require.ErrorIs(t, reconciler.Reconcile(t.Context()), failed)
	require.NoError(t, reconciler.Reconcile(t.Context()))
	require.Equal(t, []domain.ProjectionRepairQuery{{Limit: 100}, {Limit: 100}}, pending.queries)
}
