package projection_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ethernity/obsidian-topic-stream/internal/domain"
	"github.com/ethernity/obsidian-topic-stream/internal/service/projection"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestBatchProjectionGroupsEmbeddingsAndLeavesACKExplicit(t *testing.T) {
	repository, commits, model := batchProjectionInputs(8)
	embedder, writer := &embedderStub{}, &writerStub{}
	service, err := projection.NewWithOptions(repository, &factoryStub{runtimes: map[uuid.UUID]*runtimeStub{model.ID: {embedder: embedder, writer: writer}}}, projection.BatchOptions{Size: 4, InputBytes: 1024, Concurrency: 1})
	require.NoError(t, err)
	var writes atomic.Int64
	results, err := service.WriteItemProjections(t.Context(), commits, func() { writes.Add(1) })
	require.NoError(t, err)
	require.Len(t, results, 8)
	for i, result := range results {
		require.NoError(t, result.Err)
		require.Len(t, result.Deliveries, 1)
		require.Equal(t, commits[i].ItemID, result.Deliveries[0].ItemID)
	}
	require.EqualValues(t, 8, writes.Load())
	require.NotEmpty(t, embedder.batches, "the batch capability must be used")
	require.Less(t, len(embedder.batches), 8, "healthy Items must not become eight single requests")
	for _, batch := range embedder.batches {
		require.LessOrEqual(t, len(batch), 4)
	}
	require.Empty(t, repository.completions, "external delivery does not ACK pending rows")
}

func TestBatchProjectionIsolatesInputErrors(t *testing.T) {
	repository, commits, model := batchProjectionInputs(12)
	badText := repository.works[commits[0].ItemID].Projection.Item.RetrievalText()
	embedder := &embedderStub{batchFunc: func(_ context.Context, texts []string) ([][]float32, error) {
		for _, text := range texts {
			if text == badText {
				return nil, domain.ErrEmbeddingInputRejected
			}
		}
		vectors := make([][]float32, len(texts))
		for i := range vectors {
			vectors[i] = []float32{1, 2, 3}
		}
		return vectors, nil
	}}
	service, err := projection.NewWithOptions(repository, &factoryStub{runtimes: map[uuid.UUID]*runtimeStub{model.ID: {embedder: embedder, writer: &writerStub{}}}}, projection.BatchOptions{Size: 4, InputBytes: 1024, Concurrency: 1})
	require.NoError(t, err)
	results, err := service.WriteItemProjections(t.Context(), commits, nil)
	require.NoError(t, err)
	require.ErrorIs(t, results[0].Err, domain.ErrEmbeddingInputRejected)
	for _, result := range results[1:] {
		require.NoError(t, result.Err)
		require.Len(t, result.Deliveries, 1)
	}
}

func TestBatchProjectionStopsOutageFanout(t *testing.T) {
	repository, commits, model := batchProjectionInputs(12)
	embedder := &embedderStub{batchFunc: func(context.Context, []string) ([][]float32, error) { return nil, errors.New("endpoint unavailable") }}
	service, err := projection.NewWithOptions(repository, &factoryStub{runtimes: map[uuid.UUID]*runtimeStub{model.ID: {embedder: embedder, writer: &writerStub{}}}}, projection.BatchOptions{Size: 4, InputBytes: 1024, Concurrency: 1})
	require.NoError(t, err)
	results, err := service.WriteItemProjections(t.Context(), commits, nil)
	require.NoError(t, err)
	for _, result := range results {
		require.Error(t, result.Err)
		require.Empty(t, result.Deliveries)
	}
	require.Len(t, embedder.batches, 1, "an outage must be cached for this page instead of repeated for every Item")
}

func TestBatchProjectionKeepsHealthyModelWritesWhenAnotherEndpointFails(t *testing.T) {
	repository, commits, healthy := batchProjectionInputs(6)
	failed := healthy
	failed.ID = uuid.New()
	failed.Endpoint = "http://failed/v1"
	for id, work := range repository.works {
		work.Models = append([]domain.ItemProjectionDestination{{EmbeddingModel: failed, CollectionIncarnation: uuid.NameSpaceOID}}, work.Models...)
		repository.works[id] = work
	}
	bad := &embedderStub{batchFunc: func(context.Context, []string) ([][]float32, error) { return nil, errors.New("endpoint offline") }}
	service, err := projection.NewWithOptions(repository, &factoryStub{runtimes: map[uuid.UUID]*runtimeStub{
		failed.ID: {embedder: bad, writer: &writerStub{}}, healthy.ID: {embedder: &embedderStub{}, writer: &writerStub{}},
	}}, projection.BatchOptions{Size: 4, InputBytes: 1024, Concurrency: 1})
	require.NoError(t, err)
	results, err := service.WriteItemProjections(t.Context(), commits, nil)
	require.NoError(t, err)
	for _, result := range results {
		require.Error(t, result.Err)
		require.Len(t, result.Deliveries, 1)
		require.Equal(t, healthy.ID, result.Deliveries[0].ModelID)
	}
	require.Len(t, bad.batches, 1)
}

func TestBatchProjectionRunsMultipleBatchesConcurrentlyWithinOneRebuild(t *testing.T) {
	repository, commits, model := batchProjectionInputs(8)
	started := make(chan struct{}, 8)
	release := make(chan struct{})
	var releaseOnce sync.Once
	unblock := func() { releaseOnce.Do(func() { close(release) }) }
	t.Cleanup(unblock)
	embedder := &embedderStub{batchFunc: func(ctx context.Context, texts []string) ([][]float32, error) {
		started <- struct{}{}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-release:
		}
		vectors := make([][]float32, len(texts))
		for i := range vectors {
			vectors[i] = []float32{1, 2, 3}
		}
		return vectors, nil
	}}
	service, err := projection.NewWithOptions(repository, &factoryStub{runtimes: map[uuid.UUID]*runtimeStub{model.ID: {embedder: embedder, writer: &writerStub{}}}}, projection.BatchOptions{Size: 2, InputBytes: 1024, Concurrency: 2})
	require.NoError(t, err)
	done := make(chan error, 1)
	go func() { _, err := service.WriteItemProjections(t.Context(), commits, nil); done <- err }()
	for range 2 {
		select {
		case <-started:
		case <-time.After(time.Second):
			t.Fatal("two batch requests must start before either one is released")
		}
	}
	unblock()
	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(5 * time.Second):
		t.Fatal("batch workers did not join")
	}
}

func batchProjectionInputs(count int) (*repositoryStub, []domain.ItemProjectionCommit, domain.EmbeddingModel) {
	workspaceID := uuid.New()
	model := domain.EmbeddingModel{ID: uuid.New(), EmbeddingModelDescriptor: domain.EmbeddingModelDescriptor{Endpoint: "http://fixture/v1", ProviderModel: "fixture", Dimensions: 3}}
	repository := &repositoryStub{works: make(map[uuid.UUID]domain.ItemProjectionWork)}
	commits := make([]domain.ItemProjectionCommit, count)
	for i := range commits {
		commits[i] = domain.ItemProjectionCommit{WorkspaceID: workspaceID, ItemID: uuid.New(), ProjectionRevision: uuid.New()}
		repository.works[commits[i].ItemID] = domain.ItemProjectionWork{Projection: domain.ItemProjection{Revision: commits[i].ProjectionRevision, Item: domain.Item{ID: commits[i].ItemID, WorkspaceID: workspaceID, Title: fmt.Sprintf("item %d", i)}}, Models: []domain.ItemProjectionDestination{{EmbeddingModel: model, CollectionIncarnation: uuid.NameSpaceOID}}}
	}
	return repository, commits, model
}

func TestBatchProjectionReusesVectorsAndSendsOversizedInputUnchanged(t *testing.T) {
	for _, reuse := range []bool{true, false} {
		t.Run(fmt.Sprint(reuse), func(t *testing.T) {
			repository, commits, model := batchProjectionInputs(1)
			work := repository.works[commits[0].ItemID]
			work.Projection.Item.Title = strings.Repeat("x", 2000)
			repository.works[commits[0].ItemID] = work
			embedder := &embedderStub{}
			service, err := projection.NewWithOptions(repository, &factoryStub{runtimes: map[uuid.UUID]*runtimeStub{model.ID: {embedder: embedder, writer: &writerStub{payloadReady: reuse}}}}, projection.BatchOptions{Size: 8, InputBytes: 1024, Concurrency: 1})
			require.NoError(t, err)
			results, err := service.WriteItemProjections(t.Context(), commits, nil)
			require.NoError(t, err)
			require.NoError(t, results[0].Err)
			require.Empty(t, embedder.batches)
			if reuse {
				require.Empty(t, embedder.texts)
			} else {
				require.Equal(t, []string{work.Projection.Item.RetrievalText()}, embedder.texts)
			}
		})
	}
}

func TestBatchProjectionSharesEndpointLimitAcrossWorkspacesAndCancelsWaiters(t *testing.T) {
	repository, commits, model := batchProjectionInputs(2)
	second := repository.works[commits[1].ItemID]
	second.Projection.Item.WorkspaceID = uuid.New()
	repository.works[commits[1].ItemID] = second
	commits[1].WorkspaceID = second.Projection.Item.WorkspaceID
	started := make(chan struct{}, 2)
	release := make(chan struct{})
	var calls atomic.Int64
	embedder := &embedderStub{batchFunc: func(ctx context.Context, texts []string) ([][]float32, error) {
		calls.Add(1)
		started <- struct{}{}
		select {
		case <-release:
			return [][]float32{{1, 2, 3}}, nil
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}}
	service, err := projection.NewWithOptions(repository, &factoryStub{runtimes: map[uuid.UUID]*runtimeStub{model.ID: {embedder: embedder, writer: &writerStub{}}}}, projection.BatchOptions{Size: 4, InputBytes: 1024, Concurrency: 1})
	require.NoError(t, err)
	firstDone := make(chan error, 1)
	go func() { _, err := service.WriteItemProjections(t.Context(), commits[:1], nil); firstDone <- err }()
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("first embedding did not start")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
	defer cancel()
	_, err = service.WriteItemProjections(ctx, commits[1:], nil)
	require.ErrorIs(t, err, context.DeadlineExceeded)
	require.EqualValues(t, 1, calls.Load(), "second Workspace must wait for the same endpoint slot")
	close(release)
	select {
	case err := <-firstDone:
		require.NoError(t, err)
	case <-time.After(5 * time.Second):
		t.Fatal("first delivery did not join")
	}
}
