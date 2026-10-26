package retrieval_test

import (
	"context"
	"errors"
	"testing"

	"github.com/ethernity/obsidian-topic-stream/internal/domain"
	"github.com/ethernity/obsidian-topic-stream/internal/service/retrieval"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

type modelStateRepository struct {
	page      domain.ModelReconciliationPage
	existing  bool
	completed []domain.ModelReconciliationRequest
	queries   []domain.ModelReconciliationQuery
}

// ReconcileModelStates returns the bounded durable requests selected by the fixture.
func (s *modelStateRepository) ReconcileModelStates(_ context.Context, query domain.ModelReconciliationQuery) (domain.ModelReconciliationPage, error) {
	s.queries = append(s.queries, query)
	return s.page, nil
}

// ModelRequestHasJob reports a durable request, including cancelled or dead work.
func (s *modelStateRepository) ModelRequestHasJob(context.Context, uuid.UUID, string) (bool, error) {
	return s.existing, nil
}

// CompleteModelRequest records the generation whose enqueue has been durably represented.
func (s *modelStateRepository) CompleteModelRequest(_ context.Context, request domain.ModelReconciliationRequest) error {
	s.completed = append(s.completed, request)
	return nil
}

func TestModelReconcilerRepairsInterruptedEnqueue(t *testing.T) {
	request := domain.ModelReconciliationRequest{WorkspaceID: uuid.New(), ModelID: uuid.New(), SelectionGeneration: 17}
	repository := &modelStateRepository{page: domain.ModelReconciliationPage{Requests: []domain.ModelReconciliationRequest{request}}}
	queue := testQueue(t)
	reconciler := retrieval.NewReconciler(repository, queue)
	require.NoError(t, reconciler.Reconcile(t.Context()))
	require.Len(t, queue.requests, 1)
	require.Contains(t, queue.requests[0].IdempotencyKey, ":17")
	require.Equal(t, request.WorkspaceID, queue.requests[0].WorkspaceID)
	require.Equal(t, []domain.ModelReconciliationRequest{request}, repository.completed)
}

func TestModelReconcilerDoesNotResurrectCancelledRequest(t *testing.T) {
	request := domain.ModelReconciliationRequest{WorkspaceID: uuid.New(), ModelID: uuid.New(), SelectionGeneration: 17}
	repository := &modelStateRepository{existing: true, page: domain.ModelReconciliationPage{Requests: []domain.ModelReconciliationRequest{request}}}
	queue := testQueue(t)
	reconciler := retrieval.NewReconciler(repository, queue)
	require.NoError(t, reconciler.Reconcile(t.Context()))
	require.Empty(t, queue.requests)
	require.Equal(t, []domain.ModelReconciliationRequest{request}, repository.completed)
}

func TestModelReconcilerPreservesRequestAndCursorWhenEnqueueFails(t *testing.T) {
	id := uuid.New()
	request := domain.ModelReconciliationRequest{WorkspaceID: id, ModelID: uuid.New(), SelectionGeneration: 1}
	repository := &modelStateRepository{page: domain.ModelReconciliationPage{Requests: []domain.ModelReconciliationRequest{request}, NextCursor: &id}}
	failure := errors.New("queue unavailable")
	queue := &reindexQueueStub{err: failure}
	reconciler := retrieval.NewReconciler(repository, queue)
	require.ErrorIs(t, reconciler.Reconcile(t.Context()), failure)
	require.Empty(t, repository.completed)
	queue.err = nil
	require.NoError(t, reconciler.Reconcile(t.Context()))
	require.Nil(t, repository.queries[0].AfterWorkspaceID)
	require.Nil(t, repository.queries[1].AfterWorkspaceID)
	require.Equal(t, []domain.ModelReconciliationRequest{request}, repository.completed)
}

func TestModelReconcilerAdvancesAcrossPagesWithoutRequests(t *testing.T) {
	id := uuid.New()
	repository := &modelStateRepository{page: domain.ModelReconciliationPage{NextCursor: &id}}
	reconciler := retrieval.NewReconciler(repository, &reindexQueueStub{})
	require.NoError(t, reconciler.Reconcile(t.Context()))
	repository.page = domain.ModelReconciliationPage{}
	require.NoError(t, reconciler.Reconcile(t.Context()))
	require.Equal(t, &id, repository.queries[1].AfterWorkspaceID)
}
