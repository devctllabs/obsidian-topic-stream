package jobs_test

import (
	"context"
	"encoding/json"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ethernity/obsidian-topic-stream/internal/domain"
	"github.com/ethernity/obsidian-topic-stream/internal/service/jobs"
	"github.com/ethernity/obsidian-topic-stream/internal/service/jobs/mocks"
	"github.com/ethernity/obsidian-topic-stream/internal/testutil"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

type jobNotifier chan uuid.UUID

// NotifyJobChanged records an invalidation for assertions.
func (n jobNotifier) NotifyJobChanged(_ uuid.UUID, id uuid.UUID) { n <- id }

func maintenanceDefinition(kind string) jobs.JobDefinition {
	return jobs.JobDefinition{Kind: kind, ArgumentsVersion: 1, Queue: domain.JobQueueMaintenance, Retry: jobs.RetryPolicy{MaxAttempts: 3, BaseDelay: time.Millisecond, MaxDelay: time.Second}}
}

// workerFixture feeds scripted claims to the real worker and records finalization commands.
// It does not implement persistence, lease arbitration, or retry scheduling.
type workerFixture struct {
	repository *mocks.MockRepository
	service    *jobs.Service
	claims     chan domain.JobClaim
	finishes   chan domain.JobFinish
}

func newWorkerFixture(t *testing.T, config jobs.Config) *workerFixture {
	t.Helper()
	config.Owner = "test-worker"
	config.PollInterval = time.Millisecond
	config.LeaseDuration = time.Second
	config.HeartbeatInterval = time.Millisecond
	if config.RandomFloat64 == nil {
		config.RandomFloat64 = func() float64 { return 0 }
	}
	repository := mocks.NewMockRepository(gomock.NewController(t))
	return &workerFixture{repository: repository, service: jobs.New(repository, time.Now, config), claims: make(chan domain.JobClaim, 8), finishes: make(chan domain.JobFinish, 8)}
}
func (f *workerFixture) start(t *testing.T) {
	t.Helper()
	f.repository.EXPECT().Claim(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, request domain.JobClaimRequest) (domain.JobClaim, error) {
		if request.Queue == domain.JobQueueMaintenance {
			select {
			case claim := <-f.claims:
				return claim, nil
			default:
			}
		}
		return domain.JobClaim{}, nil
	}).AnyTimes()
	f.repository.EXPECT().Heartbeat(gomock.Any(), gomock.Any()).Return(false, true, nil).AnyTimes()
	f.repository.EXPECT().Finish(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, finish domain.JobFinish) (bool, error) { f.finishes <- finish; return true, nil }).AnyTimes()
	runWorker(t, f.service)
}
func claimedJob(kind string) domain.Job {
	return domain.Job{ID: uuid.New(), WorkspaceID: uuid.New(), Kind: kind, Queue: domain.JobQueueMaintenance, ArgumentsVersion: 1, Arguments: json.RawMessage(`{}`), Status: domain.JobStatusRunning, Attempt: 1, MaxAttempts: 3}
}
func (f *workerFixture) claim(job domain.Job) {
	f.claims <- domain.JobClaim{Job: job, ChangedJobs: []domain.JobIdentity{{ID: job.ID, WorkspaceID: job.WorkspaceID}}}
}
func runWorker(t *testing.T, service *jobs.Service) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- service.Run(ctx) }()
	t.Cleanup(func() { cancel(); require.NoError(t, testutil.Receive(t, done)) })
}

func TestWorkerRecoversFromTransientClaimFailure(t *testing.T) {
	t.Parallel()
	f := newWorkerFixture(t, jobs.Config{})
	f.repository.EXPECT().Claim(gomock.Any(), gomock.Any()).Return(domain.JobClaim{}, errors.New("temporary database outage")).Times(2)
	f.service.RegisterDefinition(maintenanceDefinition("work"), func(context.Context, json.RawMessage) error { return nil })
	f.claim(claimedJob("work"))
	f.start(t)
	require.Equal(t, domain.JobStatusSucceeded, testutil.Receive(t, f.finishes).Status)
}
func TestWorkerLoadsWorkspaceHandlersAndKeepsThemIsolated(t *testing.T) {
	t.Parallel()
	f := newWorkerFixture(t, jobs.Config{})
	handled := make(chan jobs.Execution, 2)
	loaded := make(chan uuid.UUID, 2)
	definition := maintenanceDefinition("workspace-scoped")
	f.service.SetWorkspaceHandlerLoader(func(_ context.Context, id uuid.UUID) error {
		loaded <- id
		f.service.ForWorkspace(id).RegisterDefinition(definition, func(ctx context.Context, _ json.RawMessage) error {
			execution, found := jobs.CurrentExecution(ctx)
			if !found {
				return errors.New("missing execution")
			}
			handled <- execution
			return nil
		})
		return nil
	})
	first, second := claimedJob(definition.Kind), claimedJob(definition.Kind)
	f.claim(first)
	f.claim(second)
	f.start(t)
	for _, job := range []domain.Job{first, second} {
		require.Equal(t, job.WorkspaceID, testutil.Receive(t, loaded))
		execution := testutil.Receive(t, handled)
		require.Equal(t, job.ID, execution.JobID)
		require.Equal(t, job.WorkspaceID, execution.WorkspaceID)
		require.Equal(t, job.Attempt, execution.Attempt)
		require.Equal(t, job.MaxAttempts, execution.MaxAttempts)
		require.Contains(t, execution.LeaseOwner, "test-worker/maintenance/")
		require.Equal(t, domain.JobStatusSucceeded, testutil.Receive(t, f.finishes).Status)
	}
	_, found := jobs.CurrentExecution(t.Context())
	require.False(t, found)
}
func TestWorkerRetriesTransientWorkspaceHandlerLoadFailure(t *testing.T) {
	t.Parallel()
	f := newWorkerFixture(t, jobs.Config{})
	var loads atomic.Int32
	definition := maintenanceDefinition("load-retry")
	f.service.SetWorkspaceHandlerLoader(func(_ context.Context, id uuid.UUID) error {
		if loads.Add(1) == 1 {
			return errors.New("temporary initialization failure")
		}
		f.service.ForWorkspace(id).RegisterDefinition(definition, func(context.Context, json.RawMessage) error { return nil })
		return nil
	})
	job := claimedJob(definition.Kind)
	f.claim(job)
	f.start(t)
	finish := testutil.Receive(t, f.finishes)
	require.Equal(t, domain.JobStatusRetryWait, finish.Status)
	require.Contains(t, *finish.Reason, "initialization failure")
	job.Attempt = 2
	f.claim(job)
	require.Equal(t, domain.JobStatusSucceeded, testutil.Receive(t, f.finishes).Status)
}
func TestWorkerRecoversFromTransientHeartbeatFailureBeforeLeaseExpires(t *testing.T) {
	t.Parallel()
	f := newWorkerFixture(t, jobs.Config{})
	recovered := make(chan struct{})
	job := claimedJob("heartbeat")
	first := f.repository.EXPECT().Heartbeat(gomock.Any(), gomock.Any()).Return(false, false, errors.New("temporary outage")).Times(2)
	f.repository.EXPECT().Heartbeat(gomock.Any(), gomock.Any()).DoAndReturn(func(context.Context, domain.JobHeartbeat) (bool, bool, error) {
		close(recovered)
		return false, true, nil
	}).After(first)
	f.service.RegisterDefinition(maintenanceDefinition(job.Kind), func(ctx context.Context, _ json.RawMessage) error {
		select {
		case <-recovered:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	})
	f.claim(job)
	f.start(t)
	require.Equal(t, domain.JobStatusSucceeded, testutil.Receive(t, f.finishes).Status)
}
func TestWorkerThatLosesLeaseCancelsHandlerWithoutFinalizingStaleWork(t *testing.T) {
	t.Parallel()
	f := newWorkerFixture(t, jobs.Config{})
	stopped := make(chan struct{})
	job := claimedJob("lease-loss")
	f.repository.EXPECT().Heartbeat(gomock.Any(), gomock.Any()).Return(false, false, nil)
	f.repository.EXPECT().Finish(gomock.Any(), gomock.Any()).Times(0)
	f.service.RegisterDefinition(maintenanceDefinition(job.Kind), func(ctx context.Context, _ json.RawMessage) error { <-ctx.Done(); close(stopped); return ctx.Err() })
	f.claim(job)
	f.start(t)
	testutil.Receive(t, stopped)
}
func TestJobDefinitionControlsPersistedQueueAndAttempts(t *testing.T) {
	t.Parallel()
	repository := mocks.NewMockRepository(gomock.NewController(t))
	workspaceID := uuid.New()
	now := time.Now().UTC()
	definition := jobs.JobDefinition{Kind: "sync-source", ArgumentsVersion: 2, Queue: domain.JobQueueFetch, Retry: jobs.RetryPolicy{MaxAttempts: 5, BaseDelay: time.Second, MaxDelay: time.Hour}}
	repository.EXPECT().Enqueue(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, job domain.Job) (domain.Job, bool, error) {
		require.Equal(t, workspaceID, job.WorkspaceID)
		require.Equal(t, domain.JobQueueFetch, job.Queue)
		require.Equal(t, 5, job.MaxAttempts)
		require.Equal(t, 2, job.ArgumentsVersion)
		require.Equal(t, now, job.AvailableAt)
		require.Equal(t, domain.JobStatusQueued, job.Status)
		require.JSONEq(t, `{"safe":"argument"}`, string(job.Arguments))
		require.Equal(t, "sync:one", *job.IdempotencyKey)
		return job, true, nil
	})
	service := jobs.New(repository, func() time.Time { return now }, jobs.Config{})
	_, err := service.Enqueue(t.Context(), jobs.EnqueueRequest{WorkspaceID: workspaceID, Definition: definition, Arguments: map[string]string{"safe": "argument"}, IdempotencyKey: "sync:one"})
	require.NoError(t, err)
}
func TestSignificantJobStateChangesPublishInvalidations(t *testing.T) {
	t.Parallel()
	notifications := make(jobNotifier, 3)
	f := newWorkerFixture(t, jobs.Config{Notifier: notifications})
	job := claimedJob("succeeds")
	f.repository.EXPECT().Enqueue(gomock.Any(), gomock.Any()).Return(job, true, nil)
	f.service.RegisterDefinition(maintenanceDefinition(job.Kind), func(context.Context, json.RawMessage) error { return nil })
	_, err := f.service.Enqueue(t.Context(), jobs.EnqueueRequest{WorkspaceID: job.WorkspaceID, Definition: maintenanceDefinition(job.Kind), Arguments: struct{}{}})
	require.NoError(t, err)
	f.claim(job)
	f.start(t)
	testutil.Receive(t, f.finishes)
	for range 3 {
		require.Equal(t, job.ID, testutil.Receive(t, notifications))
	}
}
func TestLeaseRecoveryPublishesChangedJobInvalidation(t *testing.T) {
	t.Parallel()
	notifications := make(jobNotifier, 1)
	f := newWorkerFixture(t, jobs.Config{Notifier: notifications})
	identity := domain.JobIdentity{ID: uuid.New(), WorkspaceID: uuid.New()}
	f.claims <- domain.JobClaim{ChangedJobs: []domain.JobIdentity{identity}}
	f.start(t)
	require.Equal(t, identity.ID, testutil.Receive(t, notifications))
}
func TestWorkerRetriesThenFinalizesSuccessAndExhaustedOrPermanentFailures(t *testing.T) {
	temporary := errors.New("temporary failure")
	for _, scenario := range []struct {
		name     string
		failures []error
		attempts []int
		statuses []domain.JobStatus
	}{
		{"success", []error{temporary, nil}, []int{1, 2}, []domain.JobStatus{domain.JobStatusRetryWait, domain.JobStatusSucceeded}},
		{"exhausted", []error{temporary, temporary}, []int{1, 3}, []domain.JobStatus{domain.JobStatusRetryWait, domain.JobStatusDead}},
		{"permanent", []error{jobs.Permanent(errors.New("invalid configuration"))}, []int{1}, []domain.JobStatus{domain.JobStatusDead}},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			f := newWorkerFixture(t, jobs.Config{})
			job := claimedJob("retry")
			var calls atomic.Int32
			f.service.RegisterDefinition(maintenanceDefinition(job.Kind), func(context.Context, json.RawMessage) error { return scenario.failures[calls.Add(1)-1] })
			f.start(t)
			for index, status := range scenario.statuses {
				job.Attempt = scenario.attempts[index]
				f.claim(job)
				finish := testutil.Receive(t, f.finishes)
				require.Equal(t, status, finish.Status)
				if status == domain.JobStatusRetryWait {
					require.Nil(t, finish.FinishedAt)
					require.NotNil(t, finish.Reason)
				} else {
					require.NotNil(t, finish.FinishedAt)
				}
			}
		})
	}
}

func TestRetryUsesEqualJitterAndRetryAfterIsCapped(t *testing.T) {
	for _, scenario := range []struct {
		name    string
		failure error
		delay   time.Duration
	}{{"jitter", errors.New("temporary"), 15 * time.Second}, {"retry-after", jobs.RetryAfter(errors.New("rate limited"), time.Hour), time.Minute}} {
		t.Run(scenario.name, func(t *testing.T) {
			f := newWorkerFixture(t, jobs.Config{})
			now := time.Now().UTC()
			f.service = jobs.New(f.repository, func() time.Time { return now }, jobs.Config{PollInterval: time.Millisecond, RandomFloat64: func() float64 { return 0 }})
			definition := maintenanceDefinition("retry")
			definition.Retry.BaseDelay = 30 * time.Second
			definition.Retry.MaxDelay = time.Minute
			f.service.RegisterDefinition(definition, func(context.Context, json.RawMessage) error { return scenario.failure })
			f.claim(claimedJob(definition.Kind))
			f.start(t)
			require.Equal(t, now.Add(scenario.delay), testutil.Receive(t, f.finishes).AvailableAt)
		})
	}
}
func TestRunningCancellationCancelsHandlerEvenWhenItReturnsSuccess(t *testing.T) {
	t.Parallel()
	f := newWorkerFixture(t, jobs.Config{})
	job := claimedJob("cancel")
	f.repository.EXPECT().Heartbeat(gomock.Any(), gomock.Any()).Return(true, true, nil)
	f.service.RegisterDefinition(maintenanceDefinition(job.Kind), func(ctx context.Context, _ json.RawMessage) error { <-ctx.Done(); return nil })
	f.claim(job)
	f.start(t)
	require.Equal(t, domain.JobStatusCancelled, testutil.Receive(t, f.finishes).Status)
}
func TestEnqueueReturnsDeduplicatedJobWithoutInvalidation(t *testing.T) {
	t.Parallel()
	repository := mocks.NewMockRepository(gomock.NewController(t))
	notifications := make(jobNotifier, 1)
	existing := claimedJob("sync")
	repository.EXPECT().Enqueue(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, job domain.Job) (domain.Job, bool, error) {
		require.Equal(t, "sync:one", *job.IdempotencyKey)
		return existing, false, nil
	})
	service := jobs.New(repository, time.Now, jobs.Config{Notifier: notifications})
	actual, err := service.Enqueue(t.Context(), jobs.EnqueueRequest{WorkspaceID: existing.WorkspaceID, Definition: maintenanceDefinition(existing.Kind), Arguments: struct{}{}, IdempotencyKey: "sync:one"})
	require.NoError(t, err)
	require.Equal(t, existing, actual)
	require.Empty(t, notifications)
}
func TestCancellationNotifiesOnlyForNonterminalWork(t *testing.T) {
	for _, status := range []domain.JobStatus{domain.JobStatusQueued, domain.JobStatusCancelled} {
		t.Run(string(status), func(t *testing.T) {
			repository := mocks.NewMockRepository(gomock.NewController(t))
			notifications := make(jobNotifier, 1)
			job := claimedJob("cancel")
			job.Status = status
			repository.EXPECT().Get(gomock.Any(), job.ID).Return(job, true, nil)
			cancelled := job
			cancelled.Status = domain.JobStatusCancelled
			repository.EXPECT().RequestCancel(gomock.Any(), job.ID, gomock.Any()).Return(cancelled, true, nil)
			service := jobs.New(repository, time.Now, jobs.Config{Notifier: notifications})
			result, err := service.CancelForWorkspace(t.Context(), job.WorkspaceID, job.ID)
			require.NoError(t, err)
			require.Equal(t, domain.JobStatusCancelled, result.Status)
			if status == domain.JobStatusQueued {
				require.Equal(t, job.ID, testutil.Receive(t, notifications))
			} else {
				require.Empty(t, notifications)
			}
		})
	}
}
func TestWorkspaceJobOperationsRejectAnotherWorkspaceBeforeMutation(t *testing.T) {
	t.Parallel()
	repository := mocks.NewMockRepository(gomock.NewController(t))
	job := claimedJob("private")
	repository.EXPECT().Get(gomock.Any(), job.ID).Return(job, true, nil).Times(3)
	service := jobs.New(repository, time.Now, jobs.Config{})
	other := uuid.New()
	_, err := service.GetForWorkspace(t.Context(), other, job.ID)
	require.ErrorIs(t, err, jobs.ErrNotFound)
	_, err = service.RetryForWorkspace(t.Context(), other, job.ID)
	require.ErrorIs(t, err, jobs.ErrNotFound)
	_, err = service.CancelForWorkspace(t.Context(), other, job.ID)
	require.ErrorIs(t, err, jobs.ErrNotFound)
}

func TestRetryUsesDomainAdmissionWhenRegisteredAndKeepsGenericReplacement(t *testing.T) {
	t.Parallel()
	for _, custom := range []bool{false, true} {
		t.Run(map[bool]string{false: "generic", true: "domain"}[custom], func(t *testing.T) {
			repository := mocks.NewMockRepository(gomock.NewController(t))
			service := jobs.New(repository, time.Now, jobs.Config{})
			source := claimedJob("retry policy")
			source.Status = domain.JobStatusDead
			repository.EXPECT().Get(gomock.Any(), source.ID).Return(source, true, nil)
			definition := maintenanceDefinition(source.Kind)
			successor := claimedJob(source.Kind)
			successor.Status = domain.JobStatusSucceeded
			if custom {
				definition.RetryHandler = func(_ context.Context, actual domain.Job) (domain.Job, error) {
					require.Equal(t, source, actual)
					return successor, nil
				}
			} else {
				repository.EXPECT().Enqueue(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, job domain.Job) (domain.Job, bool, error) {
					require.Nil(t, job.IdempotencyKey)
					require.Equal(t, source.Arguments, job.Arguments)
					return successor, true, nil
				})
			}
			service.ForWorkspace(source.WorkspaceID).RegisterDefinition(definition, func(context.Context, json.RawMessage) error { return nil })
			actual, err := service.RetryForWorkspace(t.Context(), source.WorkspaceID, source.ID)
			require.NoError(t, err)
			require.Equal(t, successor, actual)
		})
	}
}
func TestListEncodesAndDecodesOpaqueCursor(t *testing.T) {
	t.Parallel()
	repository := mocks.NewMockRepository(gomock.NewController(t))
	workspaceID := uuid.New()
	now := time.Now().UTC()
	first, second, third := claimedJob("page"), claimedJob("page"), claimedJob("page")
	first.CreatedAt = now
	second.CreatedAt = now.Add(-time.Hour)
	third.CreatedAt = now.Add(-2 * time.Hour)
	repository.EXPECT().List(gomock.Any(), domain.JobList{WorkspaceID: workspaceID, Limit: 3}).Return([]domain.Job{first, second, third}, nil)
	repository.EXPECT().List(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, query domain.JobList) ([]domain.Job, error) {
		require.Equal(t, workspaceID, query.WorkspaceID)
		require.Equal(t, 3, query.Limit)
		require.Equal(t, second.ID, query.Cursor.ID)
		require.Equal(t, second.CreatedAt, query.Cursor.CreatedAt)
		return []domain.Job{third}, nil
	})
	service := jobs.New(repository, time.Now, jobs.Config{})
	page, err := service.List(t.Context(), jobs.ListRequest{WorkspaceID: workspaceID, Limit: 2})
	require.NoError(t, err)
	require.Len(t, page.Items, 2)
	require.NotNil(t, page.NextCursor)
	require.NotContains(t, *page.NextCursor, second.ID.String())
	page, err = service.List(t.Context(), jobs.ListRequest{WorkspaceID: workspaceID, Limit: 2, Cursor: *page.NextCursor})
	require.NoError(t, err)
	require.Equal(t, []domain.Job{third}, page.Items)
	require.Nil(t, page.NextCursor)
}
func TestWorkerRejectsUnknownArgumentsVersionWithoutCallingHandler(t *testing.T) {
	t.Parallel()
	f := newWorkerFixture(t, jobs.Config{})
	job := claimedJob("versioned")
	job.ArgumentsVersion = 2
	var calls atomic.Int32
	f.service.RegisterDefinition(maintenanceDefinition(job.Kind), func(context.Context, json.RawMessage) error { calls.Add(1); return nil })
	f.claim(job)
	f.start(t)
	finish := testutil.Receive(t, f.finishes)
	require.Equal(t, domain.JobStatusDead, finish.Status)
	require.Equal(t, "no handler registered for job kind versioned arguments version 2", *finish.Reason)
	require.Zero(t, calls.Load())
}
func TestEnqueueRejectsInvalidJobDefinition(t *testing.T) {
	t.Parallel()
	service := jobs.New(mocks.NewMockRepository(gomock.NewController(t)), time.Now, jobs.Config{})
	_, err := service.Enqueue(t.Context(), jobs.EnqueueRequest{WorkspaceID: uuid.New(), Definition: jobs.JobDefinition{Kind: "invalid", ArgumentsVersion: 1, Queue: domain.JobQueueMaintenance}, Arguments: struct{}{}})
	require.ErrorIs(t, err, jobs.ErrInvalid)
}

func TestListCollectsInvalidLimitAndCursorBeforeRepositoryCall(t *testing.T) {
	t.Parallel()
	service := jobs.New(mocks.NewMockRepository(gomock.NewController(t)), time.Now, jobs.Config{})
	_, err := service.List(t.Context(), jobs.ListRequest{WorkspaceID: uuid.New(), Limit: 0, Cursor: "invalid"})
	var validation *domain.ValidationError
	require.ErrorAs(t, err, &validation)
	require.ErrorIs(t, err, jobs.ErrInvalid)
	require.Len(t, validation.Violations, 2)
}
func TestShutdownWaitsForRunningHandlerAndRejectsNewWork(t *testing.T) {
	t.Parallel()
	f := newWorkerFixture(t, jobs.Config{})
	started, release := make(chan struct{}), make(chan struct{})
	f.service.RegisterDefinition(maintenanceDefinition("blocking"), func(context.Context, json.RawMessage) error { close(started); <-release; return nil })
	f.claim(claimedJob("blocking"))
	f.start(t)
	testutil.Receive(t, started)
	done := make(chan error, 1)
	go func() { done <- f.service.Shutdown(t.Context()) }()
	require.Never(t, func() bool { return len(done) > 0 }, 20*time.Millisecond, 5*time.Millisecond)
	close(release)
	require.NoError(t, testutil.Receive(t, done))
	require.Equal(t, domain.JobStatusRetryWait, testutil.Receive(t, f.finishes).Status)
	_, err := f.service.Enqueue(t.Context(), jobs.EnqueueRequest{WorkspaceID: uuid.New(), Definition: maintenanceDefinition("late"), Arguments: struct{}{}})
	require.ErrorIs(t, err, jobs.ErrClosed)
}

func TestRetryPreservesStoredArgumentsAndVersionWithinWorkspace(t *testing.T) {
	t.Parallel()
	repository := mocks.NewMockRepository(gomock.NewController(t))
	previous := claimedJob("versioned")
	previous.Status = domain.JobStatusDead
	previous.ArgumentsVersion = 2
	previous.Arguments = json.RawMessage(`{"sourceId":"fixture"}`)
	repository.EXPECT().Get(gomock.Any(), previous.ID).Return(previous, true, nil)
	repository.EXPECT().Enqueue(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, job domain.Job) (domain.Job, bool, error) {
		require.NotEqual(t, previous.ID, job.ID)
		require.Equal(t, previous.WorkspaceID, job.WorkspaceID)
		require.Equal(t, previous.Queue, job.Queue)
		require.Equal(t, previous.ArgumentsVersion, job.ArgumentsVersion)
		require.JSONEq(t, string(previous.Arguments), string(job.Arguments))
		require.Equal(t, domain.JobStatusQueued, job.Status)
		require.Zero(t, job.Attempt)
		return job, true, nil
	})
	service := jobs.New(repository, time.Now, jobs.Config{})
	definition := maintenanceDefinition(previous.Kind)
	definition.ArgumentsVersion = 2
	service.RegisterDefinition(definition, func(context.Context, json.RawMessage) error { return nil })
	_, err := service.RetryForWorkspace(t.Context(), previous.WorkspaceID, previous.ID)
	require.NoError(t, err)
}
