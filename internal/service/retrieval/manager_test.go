package retrieval_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"sync/atomic"

	"github.com/ethernity/obsidian-topic-stream/internal/domain"
	"github.com/ethernity/obsidian-topic-stream/internal/service/jobs"
	jobmocks "github.com/ethernity/obsidian-topic-stream/internal/service/jobs/mocks"
	"github.com/ethernity/obsidian-topic-stream/internal/service/ranking"
	"github.com/ethernity/obsidian-topic-stream/internal/service/retrieval"
	"github.com/ethernity/obsidian-topic-stream/internal/testutil"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

type catalogStub struct {
	retryModel           func(domain.Job, domain.Job) (domain.Job, bool, error)
	mu                   sync.Mutex
	active               domain.RetrievalIndex
	building             domain.RetrievalIndex
	activations          []int
	models               map[uuid.UUID]domain.EmbeddingModel
	seedFunc             func(context.Context) (bool, error)
	failFunc             func(context.Context) error
	existingModelRequest bool
	starts               []domain.ModelTransitionStart
	cutovers             []domain.ModelTransitionCutover
	failures             []domain.RetrievalIndexFailure
	activating           []domain.ModelTransitionCutover
}

// PrepareActivation records the separately observable pre-cutover phase.
func (s *catalogStub) PrepareActivation(_ context.Context, cutover domain.ModelTransitionCutover) error {
	s.activating = append(s.activating, cutover)
	return nil
}

type progressNotifierFunc func(uuid.UUID, uuid.UUID)

// NotifyJobChanged invalidates the authoritative Job resource after durable progress.
func (f progressNotifierFunc) NotifyJobChanged(workspaceID, jobID uuid.UUID) { f(workspaceID, jobID) }

func TestReindexCoalescesDurableJobNotificationsAndExposesActivation(t *testing.T) {
	t.Parallel()
	workspaceID := uuid.New()
	catalog, modelID := catalogFor(domain.EmbeddingSettings{Endpoint: "http://fixture", Model: "fixture", Dimensions: 3})
	pending := &pendingProjectionStub{}
	for range 30 {
		pending.batches = append(pending.batches, []domain.ItemProjectionCommit{{WorkspaceID: workspaceID, ItemID: uuid.New(), ProjectionRevision: uuid.New()}})
	}
	projector := &projectionDelivererStub{}
	var notificationJobs []uuid.UUID
	var acknowledgedPages []int
	notifier := progressNotifierFunc(func(workspace, job uuid.UUID) {
		require.Equal(t, workspaceID, workspace)
		notificationJobs = append(notificationJobs, job)
		acknowledgedPages = append(acknowledgedPages, len(projector.acknowledgements))
	})
	queue := testQueue(t)
	manager, err := retrieval.New(t.Context(), retrieval.Config{
		WorkspaceID: workspaceID, Catalog: catalog, Pending: pending, Projector: projector,
		Jobs: queue, Factory: &factoryStub{catalog: catalog}, DesiredModelID: modelID,
		ProgressNotifier: notifier, Now: time.Now,
	})
	require.NoError(t, err)
	target := uuid.New()
	catalog.models[target] = testEmbeddingModel(target, domain.EmbeddingSettings{Endpoint: "http://target", Model: "target", Dimensions: 3})
	job, err := manager.ReconcileModel(t.Context(), target)
	require.NoError(t, err)
	require.Equal(t, domain.JobStatusSucceeded, runQueue(t, queue, 1).Status)
	require.GreaterOrEqual(t, len(notificationJobs), 4, "begin, seeded, activating and completed phases invalidate the Job")
	require.Less(t, len(notificationJobs), 15, "fast ACKs must be coalesced, not an event per Item/page")
	for _, notified := range notificationJobs {
		require.Equal(t, job.ID, notified)
	}
	require.Equal(t, []int{30, 30}, acknowledgedPages[len(acknowledgedPages)-2:])
	require.Len(t, catalog.activating, 1)
}

// WithModelCollectionLock serializes the fixture's metadata operation synchronously.
func (s *catalogStub) WithModelCollectionLock(ctx context.Context, _ uuid.UUID, action func(context.Context) error) error {
	return action(ctx)
}

// ObserveModelCollection binds the synthetic collection marker without replacing canonical state.
func (s *catalogStub) ObserveModelCollection(context.Context, domain.ModelCollectionObservation) error {
	return nil
}

// GetActive returns the durable fixture snapshot without creating state.
func (s *catalogStub) GetActive(context.Context, uuid.UUID) (domain.RetrievalIndex, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.active, s.active.ModelID != uuid.Nil, nil
}

// ResolveModelIndex supplies a model's immutable collection schema before starting its Job.
func (s *catalogStub) ResolveModelIndex(_ context.Context, workspaceID, modelID uuid.UUID) (domain.RetrievalIndex, error) {
	model := s.models[modelID]
	return domain.RetrievalIndex{WorkspaceID: workspaceID, ModelID: modelID, Generation: model.Version, Collection: collectionName(modelID, model.Version), Embedding: embeddingSettings(model), CollectionIncarnation: uuid.NameSpaceOID}, nil
}

// ModelRequestHasJob preserves terminal requests across runtime restarts.
func (s *catalogStub) ModelRequestHasJob(context.Context, uuid.UUID, string) (bool, error) {
	return s.existingModelRequest, nil
}

// ResolveModel returns a fixture model or an error when the registry entry is absent.
func (s *catalogStub) ResolveModel(_ context.Context, modelID uuid.UUID) (domain.EmbeddingModel, error) {
	model, found := s.models[modelID]
	if !found {
		return domain.EmbeddingModel{}, errors.New("embedding model not found")
	}
	return model, nil
}

// GetOrCreateActive initializes the fixture's serving generation when none is present.
func (s *catalogStub) GetOrCreateActive(_ context.Context, workspaceID, modelID uuid.UUID, now time.Time) (domain.RetrievalIndex, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.active.Generation == 0 {
		model := s.models[modelID]
		s.active = domain.RetrievalIndex{
			WorkspaceID: workspaceID, ModelID: modelID, Generation: 1, Collection: collectionName(modelID, 1),
			Embedding: embeddingSettings(model), Status: domain.RetrievalIndexActive, CreatedAt: now, ActivatedAt: &now,
			Initialized: true, CollectionIncarnation: uuid.NameSpaceOID,
		}
	}
	return s.active, nil
}

// Begin records a transition and initializes the matching building generation.
func (s *catalogStub) Begin(_ context.Context, start domain.ModelTransitionStart) (domain.RetrievalIndex, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.starts = append(s.starts, start)
	model := s.models[start.ModelID]
	embedding := embeddingSettings(model)
	if s.building.Generation == 0 || s.building.ModelID != start.ModelID {
		s.building = domain.RetrievalIndex{
			WorkspaceID: start.WorkspaceID, ModelID: start.ModelID, Generation: model.Version,
			Collection: collectionName(start.ModelID, model.Version), Embedding: embedding,
			Status: domain.RetrievalIndexBuilding, CreatedAt: start.StartedAt,
		}
	}
	s.building.Status = domain.RetrievalIndexBuilding
	return s.building, nil
}

// SeedModelProjections reports completed synthetic initialization.
func (s *catalogStub) SeedModelProjections(ctx context.Context, _ domain.ModelProjectionSeedCommand) (bool, error) {
	if s.seedFunc != nil {
		return s.seedFunc(ctx)
	}
	return true, nil
}

func embeddingSettings(model domain.EmbeddingModel) domain.EmbeddingSettings {
	return domain.EmbeddingSettings{
		Endpoint: model.Endpoint, Model: model.ProviderModel, Dimensions: model.Dimensions,
	}
}

func testEmbeddingModel(id uuid.UUID, embedding domain.EmbeddingSettings) domain.EmbeddingModel {
	return domain.EmbeddingModel{
		ID: id, EmbeddingModelDescriptor: domain.EmbeddingModelDescriptor{
			Name: id.String(), Endpoint: embedding.Endpoint,
			ProviderModel: embedding.Model, Dimensions: embedding.Dimensions,
		}, Enabled: true, InitializationStatus: domain.EmbeddingModelReady, Version: 1,
	}
}

func catalogFor(embedding domain.EmbeddingSettings) (*catalogStub, uuid.UUID) {
	modelID := uuid.New()
	return &catalogStub{models: map[uuid.UUID]domain.EmbeddingModel{
		modelID: testEmbeddingModel(modelID, embedding),
	}}, modelID
}

// Activate promotes the matching fixture generation and rejects an unexpected model or Workspace.
func (s *catalogStub) Activate(_ context.Context, cutover domain.ModelTransitionCutover) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.building.WorkspaceID != cutover.WorkspaceID || s.building.ModelID != cutover.ModelID {
		return errors.New("unexpected generation")
	}
	s.building.Status = domain.RetrievalIndexActive
	s.building.Initialized = true
	s.building.CollectionIncarnation = uuid.NameSpaceOID
	s.building.ActivatedAt = &cutover.ActivatedAt
	s.active = s.building
	s.activations = append(s.activations, s.building.Generation)
	s.cutovers = append(s.cutovers, cutover)
	return nil
}

// Fail clears the fixture transition and records the failure command without changing serving state.
func (s *catalogStub) Fail(ctx context.Context, failure domain.RetrievalIndexFailure) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.building = domain.RetrievalIndex{}
	s.failures = append(s.failures, failure)
	if s.failFunc != nil {
		return s.failFunc(ctx)
	}
	return nil
}

func (s *catalogStub) snapshot() (domain.RetrievalIndex, []int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.active, append([]int(nil), s.activations...)
}

func (s *catalogStub) transitions() (
	[]domain.ModelTransitionStart,
	[]domain.ModelTransitionCutover,
	[]domain.RetrievalIndexFailure,
) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]domain.ModelTransitionStart(nil), s.starts...),
		append([]domain.ModelTransitionCutover(nil), s.cutovers...),
		append([]domain.RetrievalIndexFailure(nil), s.failures...)
}

type pendingProjectionStub struct {
	batches  [][]domain.ItemProjectionCommit
	modelIDs []uuid.UUID
	repeat   bool
}

// ListPendingModelProjections returns configured projection batches, optionally repeating the current
// batch.
func (s *pendingProjectionStub) ListPendingModelProjections(
	_ context.Context,
	query domain.ModelProjectionQuery,
) ([]domain.ItemProjectionCommit, error) {
	s.modelIDs = append(s.modelIDs, query.ModelID)
	if len(s.batches) == 0 {
		return nil, nil
	}
	batch := s.batches[0]
	if !s.repeat {
		s.batches = s.batches[1:]
	}
	return batch, nil
}

type projectionDelivererStub struct {
	batchCalls       int
	acknowledgements [][]domain.ItemProjectionDelivery
	ackError         error
	ackRejected      map[uuid.UUID]bool
	commits          []domain.ItemProjectionCommit
	executions       []domain.ProjectionExecution
	deliverFunc      func(context.Context) error
	err              error
	failures         map[uuid.UUID]error
}

// WriteItemProjections exercises the batch port while preserving the existing per-Item fixture outcomes.
func (s *projectionDelivererStub) WriteItemProjections(ctx context.Context, commits []domain.ItemProjectionCommit, onWritten func()) ([]domain.ItemProjectionWriteResult, error) {
	s.batchCalls++
	results := make([]domain.ItemProjectionWriteResult, len(commits))
	for i, commit := range commits {
		results[i].Deliveries, results[i].Err = s.WriteItemProjection(ctx, commit)
		if len(results[i].Deliveries) > 0 && onWritten != nil {
			onWritten()
		}
	}
	return results, nil
}

// WriteItemProjection records a successful index write for later acknowledgement.
func (s *projectionDelivererStub) WriteItemProjection(ctx context.Context, commit domain.ItemProjectionCommit) ([]domain.ItemProjectionDelivery, error) {
	if err := s.DeliverItemProjection(ctx, commit); err != nil {
		return nil, err
	}
	return []domain.ItemProjectionDelivery{{WorkspaceID: commit.WorkspaceID, ItemID: commit.ItemID, Revision: commit.ProjectionRevision, Execution: commit.Execution}}, nil
}

// CompleteItemProjections records one bounded acknowledgement operation.
func (s *projectionDelivererStub) CompleteItemProjections(_ context.Context, entries []domain.ItemProjectionDelivery) ([]bool, error) {
	s.acknowledgements = append(s.acknowledgements, append([]domain.ItemProjectionDelivery(nil), entries...))
	if s.ackError != nil {
		return nil, s.ackError
	}
	accepted := make([]bool, len(entries))
	for i, entry := range entries {
		accepted[i] = !s.ackRejected[entry.ItemID]
	}
	return accepted, nil
}

func TestReindexDoesNotActivateAfterRejectedOrFailedBatchAcknowledgement(t *testing.T) {
	for _, sqlFailure := range []bool{false, true} {
		t.Run(fmt.Sprint(sqlFailure), func(t *testing.T) {
			workspaceID, target := uuid.New(), uuid.New()
			catalog, serving := catalogFor(domain.EmbeddingSettings{Endpoint: "http://old/v1", Model: "old", Dimensions: 2})
			catalog.models[target] = testEmbeddingModel(target, domain.EmbeddingSettings{Endpoint: "http://new/v1", Model: "new", Dimensions: 3})
			commit := domain.ItemProjectionCommit{WorkspaceID: workspaceID, ItemID: uuid.New(), ProjectionRevision: uuid.New()}
			projector := &projectionDelivererStub{ackRejected: map[uuid.UUID]bool{commit.ItemID: true}}
			if sqlFailure {
				projector.ackError = errors.New("ACK transaction rolled back")
			}
			queue := testQueue(t)
			manager, err := retrieval.New(t.Context(), retrieval.Config{WorkspaceID: workspaceID, Catalog: catalog,
				Pending:   &pendingProjectionStub{batches: [][]domain.ItemProjectionCommit{{commit}, nil}},
				Projector: projector, Jobs: queue, Factory: &factoryStub{catalog: catalog}, DesiredModelID: serving, Now: time.Now})
			require.NoError(t, err)
			_, err = manager.ReconcileModel(t.Context(), target)
			require.NoError(t, err)
			require.Equal(t, domain.JobStatusRetryWait, runQueue(t, queue, 1).Status)
			_, activations := catalog.snapshot()
			require.Empty(t, activations, "index writes cannot activate a target whose ACK did not commit")
			require.Len(t, projector.acknowledgements, 1)
		})
	}
}

type modelActivationNotifierSpy struct{ calls int }

// NotifyModelChanged counts serving-model activation notifications.
func (s *modelActivationNotifierSpy) NotifyModelChanged() { s.calls++ }

// DeliverItemProjection records the projection commit and returns the configured delivery outcome.
func (s *projectionDelivererStub) DeliverItemProjection(
	ctx context.Context,
	commit domain.ItemProjectionCommit,
) error {
	s.executions = append(s.executions, commit.Execution)
	commit.Execution = domain.ProjectionExecution{}
	s.commits = append(s.commits, commit)
	if s.deliverFunc != nil {
		return s.deliverFunc(ctx)
	}
	if err := s.failures[commit.ItemID]; err != nil {
		return err
	}
	return s.err
}

func TestReindexContinuesHealthyItemsAndRetriesOnlyRemainingFailures(t *testing.T) {
	workspaceID := uuid.New()
	catalog, modelID := catalogFor(domain.EmbeddingSettings{Endpoint: "http://model/v1", Model: "synthetic", Dimensions: 3})
	targetID := uuid.New()
	catalog.models[targetID] = testEmbeddingModel(targetID, domain.EmbeddingSettings{Endpoint: "http://model/v1", Model: "target", Dimensions: 3})
	commits := make([]domain.ItemProjectionCommit, 3)
	for index := range commits {
		commits[index] = domain.ItemProjectionCommit{WorkspaceID: workspaceID, ItemID: uuid.New(), ProjectionRevision: uuid.New()}
	}
	failed := commits[0]
	pending := &pendingProjectionStub{batches: [][]domain.ItemProjectionCommit{commits[:2], commits[2:], nil}}
	projector := &projectionDelivererStub{failures: map[uuid.UUID]error{failed.ItemID: errors.New("one temporary delivery failure")}}
	queue := testQueue(t)
	manager, err := retrieval.New(t.Context(), retrieval.Config{WorkspaceID: workspaceID, Catalog: catalog, Pending: pending, Projector: projector, Jobs: queue, Factory: &factoryStub{catalog: catalog}, DesiredModelID: modelID, Now: time.Now})
	require.NoError(t, err)
	_, err = manager.ReconcileModel(t.Context(), targetID)
	require.NoError(t, err)
	require.Equal(t, domain.JobStatusRetryWait, runQueue(t, queue, 1).Status)
	require.Equal(t, commits, projector.commits, "one failed Item must not abort healthy deliveries")
	require.Equal(t, 2, projector.batchCalls, "rebuild must hand each whole page to the batch projection capability")
	_, activations := catalog.snapshot()
	require.Empty(t, activations)
	pending.batches = [][]domain.ItemProjectionCommit{{failed}, nil}
	delete(projector.failures, failed.ItemID)
	require.Equal(t, domain.JobStatusSucceeded, runQueue(t, queue, 2).Status)
	require.Equal(t, append(commits, failed), projector.commits)
}

func TestReconcileModelEnqueuesReadyEnabledTarget(t *testing.T) {
	workspaceID, targetID := uuid.New(), uuid.New()
	current := domain.EmbeddingSettings{Endpoint: "http://models/v1", Model: "current", Dimensions: 2}
	target := domain.EmbeddingModel{
		ID: targetID, EmbeddingModelDescriptor: domain.EmbeddingModelDescriptor{
			Name: "target", Endpoint: "http://models/v1", ProviderModel: "target", Dimensions: 3,
		}, Enabled: true, InitializationStatus: domain.EmbeddingModelReady,
	}
	catalog, currentID := catalogFor(current)
	catalog.models[targetID] = target
	manager, err := retrieval.New(t.Context(), retrieval.Config{
		WorkspaceID: workspaceID, Catalog: catalog,
		Pending: &pendingProjectionStub{}, Projector: &projectionDelivererStub{}, Jobs: testQueue(t),
		Factory: &factoryStub{catalog: catalog}, DesiredModelID: currentID, Now: time.Now,
	})
	require.NoError(t, err)

	job, err := manager.ReconcileModel(t.Context(), targetID)

	require.NoError(t, err)
	require.NotEqual(t, uuid.Nil, job.ID)
	require.Equal(t, retrieval.ReindexJobKind, job.Kind)
	var arguments struct {
		ModelID uuid.UUID `json:"modelId"`
	}
	require.NoError(t, json.Unmarshal(job.Arguments, &arguments))
	require.Equal(t, targetID, arguments.ModelID)
}

func TestReindexWithoutProgressTimesOutAndPreservesServing(t *testing.T) {
	for _, phase := range []string{"seed", "delivery"} {
		t.Run(phase, func(t *testing.T) {
			workspaceID := uuid.New()
			catalog, current := catalogFor(domain.EmbeddingSettings{Endpoint: "http://model/v1", Model: "A", Dimensions: 3})
			target := uuid.New()
			catalog.models[target] = testEmbeddingModel(target, domain.EmbeddingSettings{Endpoint: "http://model/v1", Model: "B", Dimensions: 3})
			pending := &pendingProjectionStub{batches: [][]domain.ItemProjectionCommit{{{
				WorkspaceID: workspaceID, ItemID: uuid.New(), ProjectionRevision: uuid.New(),
			}}}}
			projector := &projectionDelivererStub{}
			if phase == "seed" {
				catalog.seedFunc = func(ctx context.Context) (bool, error) { <-ctx.Done(); return false, ctx.Err() }
			} else {
				projector.deliverFunc = func(ctx context.Context) error { <-ctx.Done(); return ctx.Err() }
			}
			queue := testQueue(t)
			manager, err := retrieval.New(t.Context(), retrieval.Config{WorkspaceID: workspaceID, Catalog: catalog,
				Pending: pending, Projector: projector, Jobs: queue, Factory: &factoryStub{catalog: catalog},
				DesiredModelID: current, Now: time.Now, StallTimeout: 50 * time.Millisecond})
			require.NoError(t, err)
			_, err = manager.ReconcileModel(t.Context(), target)
			require.NoError(t, err)
			finish := runQueue(t, queue, 1)
			require.Equal(t, domain.JobStatusRetryWait, finish.Status)
			require.NotNil(t, finish.Reason)
			require.Contains(t, *finish.Reason, "no progress")
			require.Positive(t, queue.heartbeats.Load(), "successful heartbeats cannot extend the progress deadline")
			active, activations := catalog.snapshot()
			require.Equal(t, current, active.ModelID)
			require.Empty(t, activations)
		})
	}
}

func TestCancelledReindexCleanupHasIndependentBoundedContext(t *testing.T) {
	catalog, current := catalogFor(domain.EmbeddingSettings{Endpoint: "http://model/v1", Model: "A", Dimensions: 3})
	target := uuid.New()
	catalog.models[target] = testEmbeddingModel(target, domain.EmbeddingSettings{Endpoint: "http://model/v1", Model: "B", Dimensions: 3})
	type cleanupObservation struct {
		err      error
		deadline time.Time
		bounded  bool
	}
	cleanup := make(chan cleanupObservation, 1)
	catalog.failFunc = func(ctx context.Context) error {
		deadline, bounded := ctx.Deadline()
		cleanup <- cleanupObservation{err: ctx.Err(), deadline: deadline, bounded: bounded}
		return errors.New("cleanup unavailable")
	}
	projector := &projectionDelivererStub{deliverFunc: func(ctx context.Context) error { <-ctx.Done(); return ctx.Err() }}
	workspaceID := uuid.New()
	queue := testQueue(t)
	queue.cancelOnHeartbeat = true
	manager, err := retrieval.New(t.Context(), retrieval.Config{WorkspaceID: workspaceID, Catalog: catalog,
		Pending:   &pendingProjectionStub{batches: [][]domain.ItemProjectionCommit{{{WorkspaceID: workspaceID, ItemID: uuid.New(), ProjectionRevision: uuid.New()}}}},
		Projector: projector, Jobs: queue, Factory: &factoryStub{catalog: catalog}, DesiredModelID: current, Now: time.Now})
	require.NoError(t, err)
	_, err = manager.ReconcileModel(t.Context(), target)
	require.NoError(t, err)
	require.Equal(t, domain.JobStatusCancelled, runQueue(t, queue, 1).Status)
	observed := testutil.Receive(t, cleanup)
	require.NoError(t, observed.err, "cancelled Job context must not cancel cleanup")
	require.True(t, observed.bounded)
	require.InDelta(t, 5, time.Until(observed.deadline).Seconds(), 0.5)
	require.Positive(t, queue.heartbeats.Load())
}

func TestHealthyReindexCanOutlastItsNoProgressWindow(t *testing.T) {
	workspaceID := uuid.New()
	catalog, current := catalogFor(domain.EmbeddingSettings{Endpoint: "http://model/v1", Model: "A", Dimensions: 3})
	target := uuid.New()
	catalog.models[target] = testEmbeddingModel(target, domain.EmbeddingSettings{Endpoint: "http://model/v1", Model: "B", Dimensions: 3})
	var commits []domain.ItemProjectionCommit
	for range 5 {
		commits = append(commits, domain.ItemProjectionCommit{WorkspaceID: workspaceID, ItemID: uuid.New(), ProjectionRevision: uuid.New()})
	}
	projector := &projectionDelivererStub{deliverFunc: func(ctx context.Context) error {
		timer := time.NewTimer(30 * time.Millisecond)
		defer timer.Stop()
		select {
		case <-timer.C:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}}
	queue := testQueue(t)
	manager, err := retrieval.New(t.Context(), retrieval.Config{WorkspaceID: workspaceID, Catalog: catalog,
		Pending: &pendingProjectionStub{batches: [][]domain.ItemProjectionCommit{commits}}, Projector: projector,
		Jobs: queue, Factory: &factoryStub{catalog: catalog}, DesiredModelID: current, Now: time.Now,
		StallTimeout: 100 * time.Millisecond})
	require.NoError(t, err)
	_, err = manager.ReconcileModel(t.Context(), target)
	require.NoError(t, err)
	require.Equal(t, domain.JobStatusSucceeded, runQueue(t, queue, 1).Status)
	require.Len(t, projector.commits, 5)
}

func TestReconcileModelUsesDurableServingInsteadOfCapturedRuntime(t *testing.T) {
	catalog, modelA := catalogFor(domain.EmbeddingSettings{Endpoint: "http://model/v1", Model: "A", Dimensions: 3})
	modelB := uuid.New()
	catalog.models[modelB] = testEmbeddingModel(modelB, domain.EmbeddingSettings{Endpoint: "http://model/v1", Model: "B", Dimensions: 3})
	queue := testQueue(t)
	manager, err := retrieval.New(t.Context(), retrieval.Config{WorkspaceID: uuid.New(), Catalog: catalog,
		Pending: &pendingProjectionStub{}, Projector: &projectionDelivererStub{}, Jobs: queue,
		Factory: &factoryStub{catalog: catalog}, DesiredModelID: modelA, Now: time.Now})
	require.NoError(t, err)
	// A different worker has cut over to B. This Manager still captured A at construction.
	catalog.mu.Lock()
	catalog.active.ModelID = modelB
	catalog.mu.Unlock()
	job, err := manager.ReconcileModel(t.Context(), modelA)
	require.NoError(t, err)
	require.NotEqual(t, uuid.Nil, job.ID, "returning to A must compare against durable Serving=B")
	require.Len(t, queue.requests, 1)
}

func TestQueryUsesDurableModelAndRelocatedEndpointWithoutNotifications(t *testing.T) {
	catalog, current := catalogFor(domain.EmbeddingSettings{Endpoint: "http://old/v1", Model: "A", Dimensions: 3})
	factory := &factoryStub{catalog: catalog}
	manager, err := retrieval.New(t.Context(), retrieval.Config{WorkspaceID: uuid.New(), Catalog: catalog,
		Pending: &pendingProjectionStub{}, Projector: &projectionDelivererStub{}, Jobs: testQueue(t), Factory: factory, DesiredModelID: current, Now: time.Now})
	require.NoError(t, err)
	catalog.mu.Lock()
	catalog.active.ModelID = uuid.New()
	catalog.active.Generation = 7
	catalog.active.Collection = "new-durable-collection"
	catalog.active.Embedding = domain.EmbeddingSettings{Endpoint: "http://relocated/v1", Model: "B", Dimensions: 5}
	catalog.mu.Unlock()
	vector, err := manager.Embed(t.Context(), "query")
	require.NoError(t, err)
	require.Len(t, vector, 5, "the next operation must use the independent worker's durable model")
}

func TestCutoverDuringExplicitEnsureCannotRelabelAnOldEmbeddingRuntime(t *testing.T) {
	catalog, current := catalogFor(domain.EmbeddingSettings{Endpoint: "http://old/v1", Model: "A", Dimensions: 3})
	target := testEmbeddingModel(uuid.New(), domain.EmbeddingSettings{Endpoint: "http://new/v1", Model: "B", Dimensions: 5})
	catalog.models[target.ID] = target
	factory := &factoryStub{catalog: catalog, ensureHook: func() {
		catalog.mu.Lock()
		defer catalog.mu.Unlock()
		catalog.active.ModelID, catalog.active.DesiredModelID = target.ID, target.ID
		catalog.active.Collection = collectionName(target.ID, target.Version)
		catalog.active.Embedding = embeddingSettings(target)
	}}
	manager, err := retrieval.New(t.Context(), retrieval.Config{WorkspaceID: uuid.New(), Catalog: catalog,
		Pending: &pendingProjectionStub{}, Projector: &projectionDelivererStub{}, Jobs: testQueue(t), Factory: factory, DesiredModelID: current, Now: time.Now})
	require.NoError(t, err)
	require.NoError(t, manager.Ensure(t.Context()))
	vector, err := manager.Embed(t.Context(), "query")
	require.NoError(t, err)
	require.Len(t, vector, 5, "a cutover during ensure must preserve model/runtime pairing")
}

func TestReconcileCarriesDurableSelectionGeneration(t *testing.T) {
	catalog, current := catalogFor(domain.EmbeddingSettings{Endpoint: "http://model/v1", Model: "A", Dimensions: 3})
	target := uuid.New()
	catalog.models[target] = testEmbeddingModel(target, domain.EmbeddingSettings{Endpoint: "http://model/v1", Model: "B", Dimensions: 3})
	queue := testQueue(t)
	manager, err := retrieval.New(t.Context(), retrieval.Config{WorkspaceID: uuid.New(), Catalog: catalog,
		Pending: &pendingProjectionStub{}, Projector: &projectionDelivererStub{}, Jobs: queue,
		Factory: &factoryStub{catalog: catalog}, DesiredModelID: current, Now: time.Now})
	require.NoError(t, err)
	catalog.mu.Lock()
	catalog.active.SelectionGeneration = 17
	catalog.active.DesiredModelID = target
	catalog.mu.Unlock()
	job, err := manager.ReconcileModel(t.Context(), target)
	require.NoError(t, err)
	var payload struct {
		SelectionGeneration int64 `json:"selectionGeneration"`
	}
	require.NoError(t, json.Unmarshal(job.Arguments, &payload))
	require.Equal(t, int64(17), payload.SelectionGeneration)
	require.Contains(t, queue.requests[0].IdempotencyKey, ":17")
}

func TestReconcileIgnoresSelectionAlreadyReplacedInDatabase(t *testing.T) {
	catalog, current := catalogFor(domain.EmbeddingSettings{Endpoint: "http://model/v1", Model: "A", Dimensions: 3})
	target := uuid.New()
	catalog.models[target] = testEmbeddingModel(target, domain.EmbeddingSettings{Endpoint: "http://model/v1", Model: "B", Dimensions: 3})
	queue := testQueue(t)
	manager, err := retrieval.New(t.Context(), retrieval.Config{WorkspaceID: uuid.New(), Catalog: catalog,
		Pending: &pendingProjectionStub{}, Projector: &projectionDelivererStub{}, Jobs: queue,
		Factory: &factoryStub{catalog: catalog}, DesiredModelID: current, Now: time.Now})
	require.NoError(t, err)
	catalog.mu.Lock()
	catalog.active.DesiredModelID = uuid.New()
	catalog.mu.Unlock()
	job, err := manager.ReconcileModel(t.Context(), target)
	require.NoError(t, err)
	require.Equal(t, uuid.Nil, job.ID)
	require.Empty(t, queue.requests)
}

func TestReconcileDoesNotRestartTerminalModelRequest(t *testing.T) {
	catalog, current := catalogFor(domain.EmbeddingSettings{Endpoint: "http://model/v1", Model: "A", Dimensions: 3})
	target := uuid.New()
	catalog.models[target] = testEmbeddingModel(target, domain.EmbeddingSettings{Endpoint: "http://model/v1", Model: "B", Dimensions: 3})
	queue := testQueue(t)
	manager, err := retrieval.New(t.Context(), retrieval.Config{WorkspaceID: uuid.New(), Catalog: catalog,
		Pending: &pendingProjectionStub{}, Projector: &projectionDelivererStub{}, Jobs: queue,
		Factory: &factoryStub{catalog: catalog}, DesiredModelID: current, Now: time.Now})
	require.NoError(t, err)
	catalog.existingModelRequest = true
	job, err := manager.ReconcileModel(t.Context(), target)
	require.NoError(t, err)
	require.Equal(t, uuid.Nil, job.ID, "startup must respect a cancelled or exhausted request")
	require.Empty(t, queue.requests)
}

func TestReconcileModelRejectsUnknownOrUnselectableTargets(t *testing.T) {
	current := domain.EmbeddingSettings{Endpoint: "http://models/v1", Model: "current", Dimensions: 2}
	readyDisabledID, failedID := uuid.New(), uuid.New()
	catalog, currentID := catalogFor(current)
	catalog.models[readyDisabledID] = domain.EmbeddingModel{
		ID: readyDisabledID, EmbeddingModelDescriptor: domain.EmbeddingModelDescriptor{
			Name: "disabled", Endpoint: "http://models/v1", ProviderModel: "disabled", Dimensions: 2,
		}, InitializationStatus: domain.EmbeddingModelReady,
	}
	catalog.models[failedID] = domain.EmbeddingModel{
		ID: failedID, EmbeddingModelDescriptor: domain.EmbeddingModelDescriptor{
			Name: "failed", Endpoint: "http://models/v1", ProviderModel: "failed", Dimensions: 2,
		}, Enabled: true, InitializationStatus: domain.EmbeddingModelFailed,
	}
	manager, err := retrieval.New(t.Context(), retrieval.Config{
		WorkspaceID: uuid.New(), Catalog: catalog,
		Pending: &pendingProjectionStub{}, Projector: &projectionDelivererStub{}, Jobs: testQueue(t),
		Factory: &factoryStub{catalog: catalog}, DesiredModelID: currentID, Now: time.Now,
	})
	require.NoError(t, err)

	for _, modelID := range []uuid.UUID{uuid.New(), readyDisabledID, failedID} {
		_, reconcileErr := manager.ReconcileModel(t.Context(), modelID)
		require.Error(t, reconcileErr)
	}
}

type factoryStub struct {
	catalog            *catalogStub
	failEnsure         bool
	mu                 sync.Mutex
	ensured            map[string]int
	denseHits          []ranking.IndexHit
	sparseHits         []ranking.IndexHit
	recentHits         []ranking.IndexHit
	denseCalls         []string
	sparseCalls        []string
	recentCalls        int
	personalRequests   []ranking.PersonalIndexRequest
	personalResult     ranking.PersonalRetrieval
	correctionRequests []ranking.TopicCorrectionIndexRequest
	correctionHits     []ranking.IndexHit
	incarnation        uuid.UUID
	replaceAfterQuery  bool
	ensureHook         func()
}

// Build returns model-specific fixture dependencies without acquiring production resources.
func (s *factoryStub) Build(generation domain.RetrievalIndex) (retrieval.Embedder, retrieval.Index, error) {
	return generationEmbedder{dimensions: generation.Embedding.Dimensions, owner: s}, &generationIndex{
		collection: generation.Collection, owner: s,
	}, nil
}

type generationEmbedder struct {
	dimensions int
	owner      *factoryStub
}

// Embed returns a deterministic fixture vector without contacting a provider.
func (e generationEmbedder) Embed(_ context.Context, text string) ([]float32, error) {
	e.owner.mu.Lock()
	e.owner.denseCalls = append(e.owner.denseCalls, text)
	e.owner.mu.Unlock()
	result := make([]float32, e.dimensions)
	result[0] = float32(len(text))
	return result, nil
}

type generationIndex struct {
	collection string
	owner      *factoryStub
}

// CollectionIncarnation returns the synthetic collection's stable identity.
func (i *generationIndex) CollectionIncarnation(context.Context) (uuid.UUID, error) {
	i.owner.mu.Lock()
	defer i.owner.mu.Unlock()
	if i.owner.incarnation != uuid.Nil {
		return i.owner.incarnation, nil
	}
	return uuid.NameSpaceOID, nil
}

// SetCollectionIncarnation represents metadata initialization at the index boundary.
func (i *generationIndex) SetCollectionIncarnation(context.Context, uuid.UUID) error { return nil }

// Ensure records index initialization and returns its configured outcome.
func (i *generationIndex) Ensure(context.Context) error {
	i.owner.mu.Lock()
	defer i.owner.mu.Unlock()
	if i.owner.ensured == nil {
		i.owner.ensured = make(map[string]int)
	}
	i.owner.ensured[i.collection]++
	if i.owner.ensureHook != nil {
		hook := i.owner.ensureHook
		i.owner.ensureHook = nil
		hook()
	}
	if i.owner.failEnsure {
		return errors.New("qdrant unavailable")
	}
	return nil
}

// RetrievePersonal returns configured personal-feedback evidence.
func (i *generationIndex) RetrievePersonal(
	_ context.Context,
	request ranking.PersonalIndexRequest,
) (ranking.PersonalRetrieval, error) {
	i.owner.mu.Lock()
	defer i.owner.mu.Unlock()
	i.owner.personalRequests = append(i.owner.personalRequests, request)
	return i.owner.personalResult, nil
}

// QueryTopicCorrections records correction retrieval and returns configured evidence.
func (i *generationIndex) QueryTopicCorrections(
	_ context.Context,
	request ranking.TopicCorrectionIndexRequest,
) ([]ranking.IndexHit, error) {
	i.owner.mu.Lock()
	defer i.owner.mu.Unlock()
	i.owner.correctionRequests = append(i.owner.correctionRequests, request)
	return append([]ranking.IndexHit(nil), i.owner.correctionHits...), nil
}

// QueryDense returns a copy of the fixture's dense matches.
func (i *generationIndex) QueryDense(context.Context, []float32, ranking.IndexFilter, int) ([]ranking.IndexHit, error) {
	i.owner.mu.Lock()
	defer i.owner.mu.Unlock()
	if i.owner.replaceAfterQuery {
		i.owner.incarnation = uuid.New()
	}
	return append([]ranking.IndexHit(nil), i.owner.denseHits...), nil
}

// QuerySparse returns the fixture's lexical retrieval results.
func (i *generationIndex) QuerySparse(_ context.Context, text string, _ ranking.IndexFilter, _ int) ([]ranking.IndexHit, error) {
	i.owner.mu.Lock()
	defer i.owner.mu.Unlock()
	i.owner.sparseCalls = append(i.owner.sparseCalls, text)
	return append([]ranking.IndexHit(nil), i.owner.sparseHits...), nil
}

// QueryRecent returns the fixture's chronological retrieval results.
func (i *generationIndex) QueryRecent(context.Context, ranking.IndexFilter, int) ([]ranking.IndexHit, error) {
	i.owner.mu.Lock()
	defer i.owner.mu.Unlock()
	i.owner.recentCalls++
	return append([]ranking.IndexHit(nil), i.owner.recentHits...), nil
}

func TestRetrieveTopicLoadsBoundedManualIncludesFromQdrant(t *testing.T) {
	t.Parallel()

	workspaceID, topicID, includedID, excludedID := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	embedding := domain.EmbeddingSettings{Endpoint: "http://models/v1", Model: "current", Dimensions: 2}
	catalog, modelID := catalogFor(embedding)
	factory := &factoryStub{
		catalog: catalog,
		correctionHits: []ranking.IndexHit{
			{ItemID: includedID, Embedding: []float32{1, 0}, TopicCorrections: []domain.TopicCorrectionProjection{{
				TopicID: topicID, CorrectionEpoch: 3, Membership: domain.TopicMembershipInclude,
			}}},
			{ItemID: excludedID, Embedding: []float32{-1, 0}, TopicCorrections: []domain.TopicCorrectionProjection{{
				TopicID: topicID, CorrectionEpoch: 3, Membership: domain.TopicMembershipExclude,
			}}},
		},
	}
	manager, err := retrieval.New(t.Context(), retrieval.Config{
		WorkspaceID: workspaceID, Catalog: catalog,
		Pending: &pendingProjectionStub{}, Projector: &projectionDelivererStub{}, Jobs: testQueue(t),
		Factory: factory, DesiredModelID: modelID, Now: time.Now,
	})
	require.NoError(t, err)

	result, err := manager.RetrieveTopic(t.Context(), ranking.TopicIndexRequest{
		TopicID: topicID, CorrectionEpoch: 3, Include: "include", Exclude: "avoid",
		Filter: ranking.IndexFilter{WorkspaceID: workspaceID}, Limit: 50,
	})

	require.NoError(t, err)
	require.Equal(t, factory.correctionHits, result.Corrections)
	require.Equal(t, []ranking.TopicCorrectionIndexRequest{{
		TopicID: topicID, CorrectionEpoch: 3, Membership: domain.TopicMembershipInclude,
		QueryVector: []float32{7, 0}, Filter: ranking.IndexFilter{WorkspaceID: workspaceID}, Limit: 50,
	}}, factory.correctionRequests)
}

func TestRetrieveTopicKeepsManualIncludeBudgetBoundedWhenAutomaticRetrievalIsNearlyFull(t *testing.T) {
	t.Parallel()

	workspaceID, topicID := uuid.New(), uuid.New()
	embedding := domain.EmbeddingSettings{Endpoint: "http://models/v1", Model: "current", Dimensions: 2}
	catalog, modelID := catalogFor(embedding)
	dense := make([]ranking.IndexHit, 0, 98)
	for range 98 {
		dense = append(dense, ranking.IndexHit{ItemID: uuid.New()})
	}
	factory := &factoryStub{catalog: catalog, denseHits: dense}
	manager, err := retrieval.New(t.Context(), retrieval.Config{
		WorkspaceID: workspaceID, Catalog: catalog,
		Pending: &pendingProjectionStub{}, Projector: &projectionDelivererStub{}, Jobs: testQueue(t),
		Factory: factory, DesiredModelID: modelID, Now: time.Now,
	})
	require.NoError(t, err)

	_, err = manager.RetrieveTopic(t.Context(), ranking.TopicIndexRequest{
		TopicID: topicID, CorrectionEpoch: 1, Include: "include",
		Filter: ranking.IndexFilter{WorkspaceID: workspaceID}, Limit: 100,
	})

	require.NoError(t, err)
	require.Equal(t, 10, factory.correctionRequests[0].Limit)
}

func TestRetrieveTopicUsesOneGenerationForQueriesAndGateEmbeddings(t *testing.T) {
	t.Parallel()

	workspaceID := uuid.New()
	itemID := uuid.New()
	embedding := domain.EmbeddingSettings{Endpoint: "http://models/v1", Model: "current", Dimensions: 2}
	catalog, modelID := catalogFor(embedding)
	factory := &factoryStub{
		catalog:    catalog,
		denseHits:  []ranking.IndexHit{{ItemID: itemID, Score: 0.9}},
		sparseHits: []ranking.IndexHit{{ItemID: itemID, Score: 4}},
	}
	manager, err := retrieval.New(t.Context(), retrieval.Config{
		WorkspaceID: workspaceID, Catalog: catalog,
		Pending: &pendingProjectionStub{}, Projector: &projectionDelivererStub{}, Jobs: testQueue(t),
		Factory: factory, DesiredModelID: modelID, Now: time.Now,
	})
	require.NoError(t, err)

	result, err := manager.RetrieveTopic(t.Context(), ranking.TopicIndexRequest{
		Include: "include", Exclude: "avoid", Filter: ranking.IndexFilter{WorkspaceID: workspaceID}, Limit: 50,
	})

	require.NoError(t, err)
	require.Equal(t, factory.denseHits, result.Dense)
	require.Equal(t, factory.sparseHits, result.Sparse)
	require.Equal(t, []float32{7, 0}, result.IncludeEmbedding)
	require.Equal(t, []float32{5, 0}, result.ExcludeEmbedding)
	_, err = manager.RetrieveTopic(t.Context(), ranking.TopicIndexRequest{Include: "include", Exclude: "avoid", IncludeEmbedding: result.IncludeEmbedding, ExcludeEmbedding: result.ExcludeEmbedding, Filter: ranking.IndexFilter{WorkspaceID: workspaceID}, Limit: 50})
	require.NoError(t, err)

	factory.mu.Lock()
	require.Equal(t, []string{"include", "avoid"}, factory.denseCalls)
	require.Equal(t, []string{"include", "include"}, factory.sparseCalls)
	factory.mu.Unlock()
}

func TestRetrieveTopicFallsBackToRecentCandidatesWithoutIncludeGuidance(t *testing.T) {
	t.Parallel()

	workspaceID := uuid.New()
	itemID := uuid.New()
	embedding := domain.EmbeddingSettings{Endpoint: "http://models/v1", Model: "current", Dimensions: 2}
	catalog, modelID := catalogFor(embedding)
	factory := &factoryStub{
		catalog:    catalog,
		recentHits: []ranking.IndexHit{{ItemID: itemID}},
	}
	manager, err := retrieval.New(t.Context(), retrieval.Config{
		WorkspaceID: workspaceID, Catalog: catalog,
		Pending: &pendingProjectionStub{}, Projector: &projectionDelivererStub{}, Jobs: testQueue(t),
		Factory: factory, DesiredModelID: modelID, Now: time.Now,
	})
	require.NoError(t, err)

	result, err := manager.RetrieveTopic(t.Context(), ranking.TopicIndexRequest{
		Exclude: "avoid", Filter: ranking.IndexFilter{WorkspaceID: workspaceID}, Limit: 50,
	})

	require.NoError(t, err)
	require.Equal(t, factory.recentHits, result.Recent)
	require.Empty(t, result.Dense)
	require.Empty(t, result.Sparse)
	require.Equal(t, []float32{5, 0}, result.ExcludeEmbedding)
	factory.mu.Lock()
	require.Equal(t, 1, factory.recentCalls)
	require.Equal(t, []string{"avoid"}, factory.denseCalls)
	factory.mu.Unlock()
}

func TestReindexBuildsNewGenerationBeforeAtomicActivation(t *testing.T) {
	t.Parallel()

	workspaceID := uuid.New()
	oldEmbedding := domain.EmbeddingSettings{Endpoint: "http://old/v1", Model: "old", Dimensions: 2}
	newEmbedding := domain.EmbeddingSettings{Endpoint: "http://new/v1", Model: "new", Dimensions: 3}
	newModelID := uuid.New()
	commits := []domain.ItemProjectionCommit{
		{WorkspaceID: workspaceID, ItemID: uuid.MustParse("10000000-0000-0000-0000-000000000001"), ProjectionRevision: uuid.New()},
		{WorkspaceID: workspaceID, ItemID: uuid.MustParse("10000000-0000-0000-0000-000000000002"), ProjectionRevision: uuid.New()},
	}
	catalog, oldModelID := catalogFor(oldEmbedding)
	newModel := testEmbeddingModel(newModelID, newEmbedding)
	newModel.Version = 2
	catalog.models[newModelID] = newModel
	factory := &factoryStub{catalog: catalog}
	pending := &pendingProjectionStub{batches: [][]domain.ItemProjectionCommit{commits, nil}}
	projector := &projectionDelivererStub{}
	notifier := &modelActivationNotifierSpy{}
	queue := testQueue(t)
	manager, err := retrieval.New(t.Context(), retrieval.Config{
		WorkspaceID: workspaceID, Catalog: catalog, Pending: pending, Projector: projector,
		Jobs: queue, Factory: factory, ModelNotifier: notifier,
		DesiredModelID: oldModelID, Now: time.Now,
	})
	require.NoError(t, err)

	job, err := manager.ReconcileModel(t.Context(), newModelID)
	require.NoError(t, err)
	require.NotEqual(t, uuid.Nil, job.ID)
	require.Equal(t, domain.JobStatusSucceeded, runQueue(t, queue, 2).Status)

	active, activations := catalog.snapshot()
	require.Equal(t, 2, active.Generation)
	require.Equal(t, newEmbedding, active.Embedding)
	require.Equal(t, []int{2}, activations)
	require.Equal(t, commits, projector.commits)
	for _, execution := range projector.executions {
		require.Equal(t, job.ID, execution.JobID)
		require.Equal(t, 2, execution.Attempt)
		require.NotEmpty(t, execution.LeaseOwner)
	}
	require.Len(t, projector.acknowledgements, 1, "one page of written projections is acknowledged together")
	require.Len(t, projector.acknowledgements[0], len(commits))
	require.Equal(t, []uuid.UUID{newModelID, newModelID}, pending.modelIDs)
	require.Equal(t, 1, notifier.calls)
	starts, cutovers, failures := catalog.transitions()
	require.Len(t, starts, 1)
	require.Len(t, cutovers, 1)
	require.Empty(t, failures)
	require.Equal(t, job.ID, starts[0].JobID)
	require.Equal(t, job.ID, cutovers[0].JobID)
	vector, err := manager.Embed(t.Context(), "query")
	require.NoError(t, err)
	require.Len(t, vector, 3, "queries use the newly activated embedding generation")
}

func TestFailedReindexKeepsPreviousGenerationActive(t *testing.T) {
	t.Parallel()

	workspaceID := uuid.New()
	oldEmbedding := domain.EmbeddingSettings{Endpoint: "http://old/v1", Model: "old", Dimensions: 2}
	newEmbedding := domain.EmbeddingSettings{Endpoint: "http://new/v1", Model: "new", Dimensions: 3}
	newModelID := uuid.New()
	catalog, oldModelID := catalogFor(oldEmbedding)
	newModel := testEmbeddingModel(newModelID, newEmbedding)
	newModel.Version = 2
	catalog.models[newModelID] = newModel
	factory := &factoryStub{catalog: catalog}
	pending := &pendingProjectionStub{repeat: true, batches: [][]domain.ItemProjectionCommit{{{
		WorkspaceID: workspaceID, ItemID: uuid.New(), ProjectionRevision: uuid.New(),
	}}}}
	projector := &projectionDelivererStub{err: errors.New("qdrant unavailable")}
	queue := testQueue(t)
	manager, err := retrieval.New(t.Context(), retrieval.Config{
		WorkspaceID: workspaceID, Catalog: catalog,
		Pending: pending, Projector: projector,
		Jobs: queue, Factory: factory, DesiredModelID: oldModelID, Now: time.Now,
	})
	require.NoError(t, err)

	job, err := manager.ReconcileModel(t.Context(), newModelID)
	require.NoError(t, err)
	for attempt := 1; attempt <= 3; attempt++ {
		finish := runQueue(t, queue, attempt)
		expected := domain.JobStatusRetryWait
		if attempt == 3 {
			expected = domain.JobStatusDead
		}
		require.Equal(t, expected, finish.Status)
	}

	active, activations := catalog.snapshot()
	require.Equal(t, 1, active.Generation)
	require.Equal(t, oldEmbedding, active.Embedding)
	require.Equal(t, domain.RetrievalIndexActive, active.Status)
	require.Empty(t, activations)
	starts, cutovers, failures := catalog.transitions()
	require.Len(t, starts, 3)
	require.Empty(t, cutovers)
	require.Len(t, failures, 3)
	for index := range starts {
		require.Equal(t, job.ID, starts[index].JobID)
		require.Equal(t, job.ID, failures[index].JobID)
		require.Equal(t, newModelID, failures[index].ModelID)
		require.Contains(t, failures[index].Reason, "qdrant unavailable")
	}
	vector, err := manager.Embed(t.Context(), "query")
	require.NoError(t, err)
	require.Len(t, vector, 2)
}

func TestManagerStartupSchedulesReindexWhenPersistedSettingsDifferFromActiveGeneration(t *testing.T) {
	t.Parallel()

	workspaceID := uuid.New()
	oldEmbedding := domain.EmbeddingSettings{Endpoint: "http://old/v1", Model: "old", Dimensions: 2}
	configuredEmbedding := domain.EmbeddingSettings{Endpoint: "http://new/v1", Model: "new", Dimensions: 3}
	oldModelID, configuredModelID := uuid.New(), uuid.New()
	activatedAt := time.Now()
	catalog := &catalogStub{active: domain.RetrievalIndex{
		WorkspaceID: workspaceID, ModelID: oldModelID, Generation: 1, Collection: collectionName(oldModelID, 1),
		Embedding: oldEmbedding, Status: domain.RetrievalIndexActive, CreatedAt: activatedAt, ActivatedAt: &activatedAt,
	}, models: map[uuid.UUID]domain.EmbeddingModel{
		oldModelID:        testEmbeddingModel(oldModelID, oldEmbedding),
		configuredModelID: testEmbeddingModel(configuredModelID, configuredEmbedding),
	}}
	queue := testQueue(t)

	_, err := retrieval.New(t.Context(), retrieval.Config{
		WorkspaceID: workspaceID, Catalog: catalog,
		Pending: &pendingProjectionStub{}, Projector: &projectionDelivererStub{}, Jobs: queue,
		Factory:        &factoryStub{catalog: catalog},
		DesiredModelID: configuredModelID, Now: time.Now,
	})

	require.NoError(t, err)
	require.Len(t, queue.requests, 1)
	require.Equal(t, workspaceID, queue.requests[0].WorkspaceID)
	require.Equal(t, retrieval.ReindexJobKind, queue.requests[0].Definition.Kind)

}

func TestManagerConstructionDefersEnsuringActiveCollection(t *testing.T) {
	t.Parallel()

	workspaceID := uuid.New()
	embedding := domain.EmbeddingSettings{Endpoint: "http://models/v1", Model: "current", Dimensions: 2}
	catalog, modelID := catalogFor(embedding)
	factory := &factoryStub{catalog: catalog}

	_, err := retrieval.New(t.Context(), retrieval.Config{
		WorkspaceID: workspaceID, Catalog: catalog,
		Pending: &pendingProjectionStub{}, Projector: &projectionDelivererStub{}, Jobs: testQueue(t),
		Factory: factory, DesiredModelID: modelID, Now: time.Now,
	})

	require.NoError(t, err)
	factory.mu.Lock()
	require.Zero(t, factory.ensured[collectionName(modelID, 1)])
	factory.mu.Unlock()
}

func TestManagerStartsDegradedAndRetriesCollectionEnsureOnQuery(t *testing.T) {
	t.Parallel()

	workspaceID := uuid.New()
	embedding := domain.EmbeddingSettings{Endpoint: "http://models/v1", Model: "current", Dimensions: 2}
	catalog, modelID := catalogFor(embedding)
	factory := &factoryStub{catalog: catalog, failEnsure: true}
	manager, err := retrieval.New(t.Context(), retrieval.Config{
		WorkspaceID: workspaceID, Catalog: catalog,
		Pending: &pendingProjectionStub{}, Projector: &projectionDelivererStub{}, Jobs: testQueue(t),
		Factory: factory, DesiredModelID: modelID, Now: time.Now,
	})
	require.NoError(t, err)

	factory.mu.Lock()
	factory.failEnsure = false
	factory.mu.Unlock()
	_, err = manager.QuerySparse(t.Context(), "postgres", ranking.IndexFilter{WorkspaceID: workspaceID}, 10)

	require.NoError(t, err)
	factory.mu.Lock()
	require.Equal(t, 1, factory.ensured[collectionName(modelID, 1)])
	factory.mu.Unlock()
}

type reindexQueueFixture struct {
	*jobmocks.MockQueue
	handlers          map[string]jobs.Handler
	requests          []jobs.EnqueueRequest
	job               domain.Job
	heartbeats        atomic.Int64
	cancelOnHeartbeat bool
}

func testQueue(t *testing.T) *reindexQueueFixture {
	t.Helper()
	queue := &reindexQueueFixture{MockQueue: jobmocks.NewMockQueue(gomock.NewController(t)), handlers: make(map[string]jobs.Handler)}
	queue.EXPECT().RegisterDefinition(gomock.Any(), gomock.Any()).Do(func(definition jobs.JobDefinition, handler jobs.Handler) { queue.handlers[definition.Kind] = handler }).AnyTimes()
	queue.EXPECT().Enqueue(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, request jobs.EnqueueRequest) (domain.Job, error) {
		queue.requests = append(queue.requests, request)
		payload, err := json.Marshal(request.Arguments)
		if err != nil {
			return domain.Job{}, err
		}
		queue.job = domain.Job{ID: uuid.New(), WorkspaceID: request.WorkspaceID, Kind: request.Definition.Kind, Queue: request.Definition.Queue, ArgumentsVersion: request.Definition.ArgumentsVersion, Arguments: payload, MaxAttempts: request.Definition.Retry.MaxAttempts}
		return queue.job, nil
	}).AnyTimes()
	return queue
}
func runQueue(t *testing.T, queue *reindexQueueFixture, attempt int) domain.JobFinish {
	t.Helper()
	job := queue.job
	job.Attempt = attempt
	repository := jobmocks.NewMockRepository(gomock.NewController(t))
	var claimed atomic.Bool
	repository.EXPECT().Claim(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, request domain.JobClaimRequest) (domain.JobClaim, error) {
		if request.Queue == job.Queue && claimed.CompareAndSwap(false, true) {
			return domain.JobClaim{Job: job}, nil
		}
		return domain.JobClaim{}, nil
	}).AnyTimes()
	repository.EXPECT().Heartbeat(gomock.Any(), gomock.Any()).DoAndReturn(func(context.Context, domain.JobHeartbeat) (bool, bool, error) {
		queue.heartbeats.Add(1)
		return queue.cancelOnHeartbeat, true, nil
	}).AnyTimes()
	finished := make(chan domain.JobFinish, 1)
	repository.EXPECT().Finish(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, finish domain.JobFinish) (bool, error) { finished <- finish; return true, nil })
	worker := jobs.New(repository, time.Now, jobs.Config{PollInterval: time.Millisecond, HeartbeatInterval: 5 * time.Millisecond})
	worker.RegisterDefinition(queue.requests[0].Definition, queue.handlers[job.Kind])
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { done <- worker.Run(ctx) }()
	t.Cleanup(func() { cancel(); require.NoError(t, testutil.Receive(t, done)) })
	finish := testutil.Receive(t, finished)
	cancel()
	return finish
}

func collectionName(workspaceID uuid.UUID, generation int) string {
	return "topic_stream_items_" + workspaceID.String() + "_v" + string(rune('0'+generation))
}

// RetryModelTransition records a domain-admitted replacement for the service scenario.
func (s *catalogStub) RetryModelTransition(_ context.Context, source, replacement domain.Job) (domain.Job, bool, error) {
	if s.retryModel != nil {
		return s.retryModel(source, replacement)
	}
	return domain.Job{}, false, domain.ErrJobNotRetryable
}

func TestModelRetryUsesSelectionAdmissionInsteadOfGenericJobEnqueue(t *testing.T) {
	workspaceID := uuid.New()
	catalog, modelID := catalogFor(domain.EmbeddingSettings{Endpoint: "http://models/v1", Model: "current", Dimensions: 2})
	catalog.active.Initialized = true
	payload, err := json.Marshal(map[string]any{"modelId": modelID, "selectionGeneration": int64(7), "force": false})
	require.NoError(t, err)
	source := domain.Job{ID: uuid.New(), WorkspaceID: workspaceID, Kind: "reindex-workspace", Queue: domain.JobQueueMaintenance, ArgumentsVersion: 3, Arguments: payload, Status: domain.JobStatusDead}
	repository := jobmocks.NewMockRepository(gomock.NewController(t))
	repository.EXPECT().Get(gomock.Any(), source.ID).Return(source, true, nil)
	called := false
	catalog.retryModel = func(old, replacement domain.Job) (domain.Job, bool, error) {
		called = true
		require.Equal(t, source, old)
		require.NotEqual(t, old.ID, replacement.ID)
		require.Equal(t, source.WorkspaceID, replacement.WorkspaceID)
		require.JSONEq(t, string(source.Arguments), string(replacement.Arguments))
		require.NotNil(t, replacement.IdempotencyKey)
		return replacement, true, nil
	}
	worker := jobs.New(repository, time.Now, jobs.Config{})
	_, err = retrieval.New(t.Context(), retrieval.Config{WorkspaceID: workspaceID, Catalog: catalog, Pending: &pendingProjectionStub{}, Projector: &projectionDelivererStub{}, Jobs: worker.ForWorkspace(workspaceID), Factory: &factoryStub{catalog: catalog}, DesiredModelID: modelID, Now: time.Now})
	require.NoError(t, err)
	next, err := worker.RetryForWorkspace(t.Context(), workspaceID, source.ID)
	require.NoError(t, err)
	require.True(t, called)
	require.NotEqual(t, source.ID, next.ID)
}
