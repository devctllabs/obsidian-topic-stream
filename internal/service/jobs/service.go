package jobs

//go:generate go tool mockgen -source=service.go -destination=mocks/service.gen.go -package=mocks

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand/v2"
	"sync"
	"time"

	"github.com/ethernity/obsidian-topic-stream/internal/domain"
	"github.com/google/uuid"
	"golang.org/x/sync/errgroup"
	"golang.org/x/sync/singleflight"
)

var (
	ErrNotFound     = domain.ErrJobNotFound
	ErrClosed       = errors.New("job service is shutting down")
	ErrInvalid      = errors.New("invalid job request")
	ErrNotRetryable = domain.ErrJobNotRetryable
)

// Repository persists durable Jobs and enforces lease ownership for worker transitions.
type Repository interface {
	// Enqueue persists a Job, deduplicating an active non-empty idempotency key; the boolean reports
	// insertion.
	Enqueue(ctx context.Context, job domain.Job) (domain.Job, bool, error)
	// Get returns the Job and whether it exists.
	Get(ctx context.Context, jobID uuid.UUID) (domain.Job, bool, error)
	// List returns the matching Job records.
	List(ctx context.Context, query domain.JobList) ([]domain.Job, error)
	// Claim must grant an exclusive, expiring lease and may recover expired running work.
	Claim(ctx context.Context, request domain.JobClaimRequest) (domain.JobClaim, error)
	// Heartbeat reports both a cancellation request and whether the caller still owns the lease.
	Heartbeat(ctx context.Context, heartbeat domain.JobHeartbeat) (bool, bool, error)
	// Finish may update only work still leased by the supplied owner.
	Finish(ctx context.Context, finish domain.JobFinish) (bool, error)
	// RequestCancel cancels queued work or requests cooperative cancellation of running work.
	RequestCancel(ctx context.Context, jobID uuid.UUID, at time.Time) (domain.Job, bool, error)
}

// Handler executes one durable Job attempt and observes cancellation through its context.
type Handler func(context.Context, json.RawMessage) error

// Queue registers versioned handlers and submits durable Workspace Jobs.
type Queue interface {
	// RegisterDefinition registers a handler for a versioned Job kind.
	RegisterDefinition(definition JobDefinition, handler Handler)
	// Enqueue submits a durable Job and reuses an equivalent active idempotency key.
	Enqueue(ctx context.Context, request EnqueueRequest) (domain.Job, error)
}

// WorkspaceHandlerLoader lazily registers handlers for a claimed Job's Workspace.
type WorkspaceHandlerLoader func(context.Context, uuid.UUID) error

// RetryPolicy bounds attempts and exponential backoff within one Job execution class.
type RetryPolicy struct {
	MaxAttempts int
	BaseDelay   time.Duration
	MaxDelay    time.Duration
}

// RetryHandler admits a replacement through a Job kind's durable domain policy.
type RetryHandler func(context.Context, domain.Job) (domain.Job, error)

// JobDefinition binds a versioned argument contract to its queue and retry policy.
type JobDefinition struct {
	Kind             string
	ArgumentsVersion int
	Queue            domain.JobQueue
	Retry            RetryPolicy
	// RetryHandler overrides generic replacement admission when the domain owns durable identity.
	RetryHandler RetryHandler
}

// StateNotifier announces durable Job state changes.
type StateNotifier interface {
	// NotifyJobChanged announces a durable Job state change to its Workspace.
	NotifyJobChanged(workspaceID uuid.UUID, jobID uuid.UUID)
}

type handlerKey struct {
	kind             string
	argumentsVersion int
}

type registeredHandler struct {
	definition JobDefinition
	handler    Handler
}

// Execution exposes the current durable attempt and lease owner to a running handler.
type Execution struct {
	LeaseOwner  string
	JobID       uuid.UUID
	WorkspaceID uuid.UUID
	Attempt     int
	MaxAttempts int
}

type executionKey struct{}

// CurrentExecution exposes the durable attempt counters to a running Handler. It is unavailable
// outside worker-managed handler contexts.
func CurrentExecution(ctx context.Context) (Execution, bool) {
	value, found := ctx.Value(executionKey{}).(Execution)
	return value, found
}

// Config supplies dependencies and operating settings for the jobs service.
type Config struct {
	Owner                      string
	PollInterval               time.Duration
	LeaseDuration              time.Duration
	HeartbeatInterval          time.Duration
	FetchConcurrency           int
	GenerationConcurrency      int
	RetrievalConcurrency       int
	MaintenanceConcurrency     int
	FeedPreparationConcurrency int
	RandomFloat64              func() float64
	// RetryDelay optionally overrides backoff calculation; nil uses the queue policy with jitter.
	RetryDelay func(RetryPolicy, int, error) time.Duration
	Notifier   StateNotifier
}

// DefaultConfig supplies bounded worker capacities, lease timing, and default polling behavior.
func DefaultConfig() Config {
	return Config{
		Owner: uuid.NewString(), PollInterval: time.Second, LeaseDuration: 30 * time.Second,
		HeartbeatInterval:          10 * time.Second,
		FeedPreparationConcurrency: 2, FetchConcurrency: 4, GenerationConcurrency: 1, RetrievalConcurrency: 2, MaintenanceConcurrency: 1,
		RandomFloat64: rand.Float64,
	}
}

// Service runs durable Jobs in bounded queue-specific worker pools with lease maintenance.
type Service struct {
	repository Repository
	now        func() time.Time
	config     Config
	notifier   StateNotifier

	// mu protects handler registration and the single Run/Shutdown lifecycle.
	mu                sync.RWMutex
	handlers          map[handlerKey]registeredHandler
	workspaceHandlers map[uuid.UUID]map[handlerKey]registeredHandler
	workspaceLoader   WorkspaceHandlerLoader
	workspaceLoads    singleflight.Group
	closed            bool
	running           bool
	cancel            context.CancelFunc
	done              chan struct{}
}

// New prepares handler registration and fills omitted worker capacities and timing from DefaultConfig.
// Workers start only when Run is called.
func New(repository Repository, now func() time.Time, config Config) *Service {
	defaults := DefaultConfig()
	if config.Owner == "" {
		config.Owner = defaults.Owner
	}
	if config.PollInterval <= 0 {
		config.PollInterval = defaults.PollInterval
	}
	if config.LeaseDuration <= 0 {
		config.LeaseDuration = defaults.LeaseDuration
	}
	if config.HeartbeatInterval <= 0 {
		config.HeartbeatInterval = defaults.HeartbeatInterval
	}
	config = workerDefaults(config, defaults)
	if config.RandomFloat64 == nil {
		config.RandomFloat64 = defaults.RandomFloat64
	}
	return &Service{
		repository: repository, now: now, config: config, notifier: config.Notifier,
		handlers:          make(map[handlerKey]registeredHandler),
		workspaceHandlers: make(map[uuid.UUID]map[handlerKey]registeredHandler),
	}
}

// RegisterDefinition registers a handler for a versioned Job kind.
func (s *Service) RegisterDefinition(definition JobDefinition, handler Handler) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.handlers[handlerKey{kind: definition.Kind, argumentsVersion: definition.ArgumentsVersion}] = registeredHandler{
		definition: definition, handler: handler,
	}
}

// SetWorkspaceHandlerLoader sets lazy handler construction for Jobs claimed across Workspaces.
func (s *Service) SetWorkspaceHandlerLoader(loader WorkspaceHandlerLoader) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.workspaceLoader = loader
}

// ForWorkspace returns a queue view that binds registrations and submissions to one Workspace.
func (s *Service) ForWorkspace(workspaceID uuid.UUID) Queue {
	return workspaceQueue{service: s, workspaceID: workspaceID}
}

// workspaceQueue binds handler registration and enqueueing to one Workspace without owning workers.
type workspaceQueue struct {
	service     *Service
	workspaceID uuid.UUID
}

// RegisterDefinition registers a handler for a versioned Job kind.
func (q workspaceQueue) RegisterDefinition(definition JobDefinition, handler Handler) {
	q.service.mu.Lock()
	defer q.service.mu.Unlock()
	handlers := q.service.workspaceHandlers[q.workspaceID]
	if handlers == nil {
		handlers = make(map[handlerKey]registeredHandler)
		q.service.workspaceHandlers[q.workspaceID] = handlers
	}
	handlers[handlerKey{kind: definition.Kind, argumentsVersion: definition.ArgumentsVersion}] = registeredHandler{
		definition: definition, handler: handler,
	}
}

// Enqueue binds the request to this queue view's Workspace before submitting durable work.
func (q workspaceQueue) Enqueue(ctx context.Context, request EnqueueRequest) (domain.Job, error) {
	if request.WorkspaceID != q.workspaceID {
		return domain.Job{}, fmt.Errorf("%w: job workspace does not match queue workspace", ErrInvalid)
	}
	return q.service.Enqueue(ctx, request)
}

// EnqueueRequest supplies a versioned Job, serializable arguments, and optional active-work idempotency
// key.
type EnqueueRequest struct {
	WorkspaceID    uuid.UUID
	Definition     JobDefinition
	Arguments      any
	IdempotencyKey string
}

// Enqueue returns the existing active Job when the repository finds the same non-empty idempotency
// key in this workspace.
func (s *Service) Enqueue(ctx context.Context, request EnqueueRequest) (domain.Job, error) {
	s.mu.RLock()
	closed := s.closed
	s.mu.RUnlock()
	if closed {
		return domain.Job{}, ErrClosed
	}
	job, err := Prepare(request, s.now())
	if err != nil {
		return domain.Job{}, err
	}
	var created bool
	job, created, err = s.repository.Enqueue(ctx, job)
	if err != nil {
		return domain.Job{}, fmt.Errorf("enqueue job: %w", err)
	}
	if created {
		s.notifyChanged(job.WorkspaceID, job.ID)
	}
	return job, nil
}

// Prepare constructs a queued Job using the same policy for standalone and atomic domain admission.
func Prepare(request EnqueueRequest, now time.Time) (domain.Job, error) {
	definition := request.Definition
	if request.WorkspaceID == uuid.Nil || !validDefinition(definition) {
		return domain.Job{}, fmt.Errorf("%w: workspace, kind, and arguments version are required", ErrInvalid)
	}
	payload, err := json.Marshal(request.Arguments)
	if err != nil {
		return domain.Job{}, fmt.Errorf("encode job arguments: %w", err)
	}
	job := domain.Job{
		ID: uuid.New(), WorkspaceID: request.WorkspaceID, Kind: definition.Kind, Queue: definition.Queue,
		ArgumentsVersion: definition.ArgumentsVersion, Arguments: payload,
		Status: domain.JobStatusQueued, MaxAttempts: definition.Retry.MaxAttempts, AvailableAt: now,
		CreatedAt: now, UpdatedAt: now,
	}
	if request.IdempotencyKey != "" {
		job.IdempotencyKey = &request.IdempotencyKey
	}
	return job, nil
}

func validDefinition(definition JobDefinition) bool {
	return definition.Kind != "" && definition.ArgumentsVersion >= 1 && definition.Queue != "" &&
		definition.Retry.MaxAttempts >= 1 && definition.Retry.BaseDelay > 0 &&
		definition.Retry.MaxDelay >= definition.Retry.BaseDelay
}

// Get returns a Job by ID or ErrNotFound; Workspace-scoped callers should use GetForWorkspace.
func (s *Service) Get(ctx context.Context, id uuid.UUID) (domain.Job, error) {
	job, found, err := s.repository.Get(ctx, id)
	if err != nil {
		return domain.Job{}, fmt.Errorf("get job: %w", err)
	}
	if !found {
		return domain.Job{}, ErrNotFound
	}
	return job, nil
}

// GetForWorkspace returns a Job only within the requested Workspace, otherwise ErrNotFound.
func (s *Service) GetForWorkspace(ctx context.Context, workspaceID, id uuid.UUID) (domain.Job, error) {
	job, err := s.Get(ctx, id)
	if err != nil {
		return domain.Job{}, err
	}
	if job.WorkspaceID != workspaceID {
		return domain.Job{}, ErrNotFound
	}
	return job, nil
}

// Page returns a bounded Job list with an optional continuation token.
type Page struct {
	Items      []domain.Job
	NextCursor *string
}

// ListRequest selects a Workspace Job page with an optional status and cursor.
type ListRequest struct {
	WorkspaceID uuid.UUID
	Status      *domain.JobStatus
	Cursor      string
	Limit       int
}

// List validates pagination and returns Jobs belonging to the selected Workspace.
func (s *Service) List(ctx context.Context, request ListRequest) (Page, error) {
	after, err := decodeCursor(request.Cursor)
	if failure := domain.JoinValidation(domain.CheckRange("limit", float64(request.Limit), 1, 100),
		domain.CheckCondition("cursor", domain.ValidationFormat, err == nil)); failure != nil {
		return Page{}, fmt.Errorf("%w: %w", ErrInvalid, failure)
	}
	result, err := s.repository.List(ctx, domain.JobList{
		WorkspaceID: request.WorkspaceID, Status: request.Status, Cursor: after, Limit: request.Limit + 1,
	})
	if err != nil {
		return Page{}, fmt.Errorf("list jobs: %w", err)
	}
	page := Page{Items: result}
	if len(result) > request.Limit {
		page.Items = result[:request.Limit]
		next := encodeCursor(page.Items[len(page.Items)-1])
		page.NextCursor = &next
	}
	return page, nil
}

// RetryForWorkspace creates a fresh attempt sequence for dead or cancelled Workspace work with an
// available handler.
func (s *Service) RetryForWorkspace(ctx context.Context, workspaceID, id uuid.UUID) (domain.Job, error) {
	job, err := s.GetForWorkspace(ctx, workspaceID, id)
	if err != nil {
		return domain.Job{}, err
	}
	return s.retry(ctx, job)
}

func (s *Service) retry(ctx context.Context, job domain.Job) (domain.Job, error) {
	if job.Status != domain.JobStatusDead && job.Status != domain.JobStatusCancelled {
		return domain.Job{}, ErrNotRetryable
	}
	var arguments any
	if err := json.Unmarshal(job.Arguments, &arguments); err != nil {
		return domain.Job{}, fmt.Errorf("decode stored job arguments: %w", err)
	}
	registered, err := s.resolveHandler(ctx, job)
	if err != nil || registered.handler == nil {
		return domain.Job{}, ErrNotRetryable
	}
	if registered.definition.RetryHandler != nil {
		return registered.definition.RetryHandler(ctx, job)
	}
	return s.Enqueue(ctx, EnqueueRequest{
		WorkspaceID: job.WorkspaceID, Definition: registered.definition, Arguments: arguments,
	})
}

func (s *Service) requestCancel(ctx context.Context, id uuid.UUID, notify bool) (domain.Job, error) {
	job, found, err := s.repository.RequestCancel(ctx, id, s.now())
	if err != nil {
		return domain.Job{}, fmt.Errorf("cancel job: %w", err)
	}
	if !found {
		return domain.Job{}, ErrNotFound
	}
	if notify {
		s.notifyChanged(job.WorkspaceID, job.ID)
	}
	return job, nil
}

// CancelForWorkspace cancels queued work or requests cancellation of a running Workspace Job.
func (s *Service) CancelForWorkspace(ctx context.Context, workspaceID, id uuid.UUID) (domain.Job, error) {
	current, err := s.GetForWorkspace(ctx, workspaceID, id)
	if err != nil {
		return domain.Job{}, err
	}
	return s.requestCancel(ctx, id, !current.Status.Terminal())
}

// Run executes bounded queue-specific worker pools until its context is cancelled or Shutdown begins.
// Only one Run call may be active on a Service; Shutdown prevents subsequent runs.
func (s *Service) Run(ctx context.Context) error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return ErrClosed
	}
	if s.running {
		s.mu.Unlock()
		return errors.New("job worker is already running")
	}
	runContext, cancel := context.WithCancel(ctx)
	s.running = true
	s.cancel = cancel
	s.done = make(chan struct{})
	done := s.done
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		s.running = false
		close(done)
		s.mu.Unlock()
	}()

	group, groupContext := errgroup.WithContext(runContext)
	for _, pool := range s.workerPools() {
		for index := range pool.concurrency {
			queue := pool.queue
			owner := fmt.Sprintf("%s/%s/%d", s.config.Owner, queue, index)
			group.Go(func() error { return s.runWorker(groupContext, queue, owner) })
		}
	}
	return group.Wait()
}

type workerPool struct {
	queue       domain.JobQueue
	concurrency int
}

func (s *Service) workerPools() []workerPool {
	return []workerPool{
		{queue: domain.JobQueueFetch, concurrency: s.config.FetchConcurrency},
		{queue: domain.JobQueueGeneration, concurrency: s.config.GenerationConcurrency},
		{queue: domain.JobQueueRetrieval, concurrency: s.config.RetrievalConcurrency},
		{queue: domain.JobQueueFeedPreparation, concurrency: s.config.FeedPreparationConcurrency},
		{queue: domain.JobQueueMaintenance, concurrency: s.config.MaintenanceConcurrency},
	}
}

func (s *Service) runWorker(ctx context.Context, queue domain.JobQueue, owner string) error {
	ticker := time.NewTicker(s.config.PollInterval)
	defer ticker.Stop()
	consecutiveErrors := 0
	for {
		if ctx.Err() != nil {
			return nil
		}
		worked, err := s.runNext(ctx, queue, owner)
		if err != nil {
			consecutiveErrors++
			if !waitForWorker(ctx, workerStoreRetryDelay(s.config.PollInterval, consecutiveErrors)) {
				return nil
			}
			continue
		}
		consecutiveErrors = 0
		if worked {
			continue
		}
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		}
	}
}

func workerStoreRetryDelay(base time.Duration, failures int) time.Duration {
	delay := base
	for index := 1; index < failures && delay < 5*time.Second; index++ {
		delay = min(delay*2, 5*time.Second)
	}
	return delay
}

func waitForWorker(ctx context.Context, delay time.Duration) bool {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

// Shutdown prevents future enqueue and run operations, cooperatively cancels active work, and waits
// for the worker loop until ctx expires.
func (s *Service) Shutdown(ctx context.Context) error {
	s.mu.Lock()
	s.closed = true
	cancel := s.cancel
	done := s.done
	s.mu.Unlock()
	if cancel == nil || done == nil {
		return nil
	}
	cancel()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return fmt.Errorf("wait for job worker: %w", ctx.Err())
	}
}

func (s *Service) runNext(ctx context.Context, queue domain.JobQueue, owner string) (bool, error) {
	now := s.now()
	claim, err := s.repository.Claim(ctx, domain.JobClaimRequest{
		Queue: queue, Owner: owner,
		Now: now, LeaseExpiresAt: now.Add(s.config.LeaseDuration),
	})
	if err != nil {
		return false, err
	}
	for _, job := range claim.ChangedJobs {
		s.notifyChanged(job.WorkspaceID, job.ID)
	}
	if claim.Job.ID == uuid.Nil {
		return len(claim.ChangedJobs) > 0, nil
	}
	return true, s.execute(ctx, claim.Job, owner)
}

func (s *Service) execute(workerContext context.Context, job domain.Job, owner string) error {
	registered, loadErr := s.resolveHandler(workerContext, job)
	if loadErr != nil {
		return s.finishExecution(job, executionOutcome{
			owner: owner, retry: queueRetryPolicy(job.Queue), failure: loadErr,
		})
	}
	if registered.handler == nil {
		return s.finishUnhandled(workerContext, job, owner)
	}
	return s.waitForExecution(workerContext, job, owner, registered)
}

func (s *Service) finishUnhandled(ctx context.Context, job domain.Job, owner string) error {
	reason := fmt.Sprintf("no handler registered for job kind %s arguments version %d", job.Kind, job.ArgumentsVersion)
	now := s.now()
	changed, err := s.repository.Finish(ctx, domain.JobFinish{
		ID: job.ID, Owner: owner, Status: domain.JobStatusDead,
		Reason: &reason, AvailableAt: now, FinishedAt: &now, UpdatedAt: now,
	})
	if changed {
		s.notifyChanged(job.WorkspaceID, job.ID)
	}
	return err
}

type runningExecution struct {
	cancel            context.CancelFunc
	completed         chan error
	ticker            *time.Ticker
	cancelRequested   bool
	heartbeatFailures int
	leaseExpiresAt    time.Time
}

func (s *Service) waitForExecution(
	workerContext context.Context,
	job domain.Job,
	owner string,
	registered registeredHandler,
) error {
	jobContext, cancel := context.WithCancel(workerContext)
	jobContext = context.WithValue(jobContext, executionKey{}, Execution{
		LeaseOwner: owner, JobID: job.ID, WorkspaceID: job.WorkspaceID, Attempt: job.Attempt, MaxAttempts: job.MaxAttempts,
	})
	defer cancel()
	completed := make(chan error, 1)
	go func() { completed <- registered.handler(jobContext, job.Arguments) }()
	ticker := time.NewTicker(s.config.HeartbeatInterval)
	defer ticker.Stop()
	running := &runningExecution{
		cancel: cancel, completed: completed, ticker: ticker,
		leaseExpiresAt: s.now().Add(s.config.LeaseDuration),
	}
	if job.LeaseExpiresAt != nil {
		running.leaseExpiresAt = *job.LeaseExpiresAt
	}

	for {
		select {
		case handlerErr := <-completed:
			return s.finishExecution(job, executionOutcome{
				owner: owner, retry: registered.definition.Retry, failure: handlerErr,
				cancelled: running.cancelRequested, interrupted: workerContext.Err() != nil,
			})
		case <-workerContext.Done():
			cancel()
			handlerErr := <-completed
			return s.finishExecution(job, executionOutcome{
				owner: owner, retry: registered.definition.Retry, failure: handlerErr,
				cancelled: running.cancelRequested, interrupted: true,
			})
		case <-ticker.C:
			if s.maintainLease(workerContext, job, owner, running) {
				<-completed //nolint:errcheck // The lease is already lost; this attempt's result cannot be persisted.
				return nil
			}
		}
	}
}

func (s *Service) maintainLease(
	ctx context.Context,
	job domain.Job,
	owner string,
	running *runningExecution,
) bool {
	now := s.now()
	requested, owned, err := s.repository.Heartbeat(ctx, domain.JobHeartbeat{
		ID: job.ID, Owner: owner, Now: now, LeaseExpiresAt: now.Add(s.config.LeaseDuration),
	})
	if err != nil {
		running.heartbeatFailures++
		if !now.Before(running.leaseExpiresAt) {
			running.cancel()
			return true
		}
		delay := min(workerStoreRetryDelay(s.config.HeartbeatInterval, running.heartbeatFailures), running.leaseExpiresAt.Sub(now))
		running.ticker.Reset(delay)
		return false
	}
	running.heartbeatFailures = 0
	if !owned {
		running.cancel()
		return true
	}
	if requested {
		running.cancelRequested = true
		running.cancel()
	}
	running.leaseExpiresAt = now.Add(s.config.LeaseDuration)
	running.ticker.Reset(s.config.HeartbeatInterval)
	return false
}

func (s *Service) resolveHandler(ctx context.Context, job domain.Job) (registeredHandler, error) {
	key := handlerKey{kind: job.Kind, argumentsVersion: job.ArgumentsVersion}
	registered, loader := s.lookupHandler(job.WorkspaceID, key)
	if registered.handler != nil || loader == nil {
		return registered, nil
	}
	_, err, _ := s.workspaceLoads.Do(job.WorkspaceID.String(), func() (any, error) {
		return nil, loader(ctx, job.WorkspaceID)
	})
	if err != nil {
		return registeredHandler{}, fmt.Errorf("load workspace job handlers: %w", err)
	}
	registered, _ = s.lookupHandler(job.WorkspaceID, key)
	return registered, nil
}

func (s *Service) lookupHandler(workspaceID uuid.UUID, key handlerKey) (registeredHandler, WorkspaceHandlerLoader) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if registered, found := s.workspaceHandlers[workspaceID][key]; found {
		return registered, s.workspaceLoader
	}
	return s.handlers[key], s.workspaceLoader
}

func queueRetryPolicy(queue domain.JobQueue) RetryPolicy {
	switch queue {
	case domain.JobQueueFetch:
		return RetryPolicy{MaxAttempts: 5, BaseDelay: 30 * time.Second, MaxDelay: time.Hour}
	case domain.JobQueueGeneration:
		return RetryPolicy{MaxAttempts: 3, BaseDelay: 30 * time.Second, MaxDelay: 15 * time.Minute}
	case domain.JobQueueRetrieval, domain.JobQueueFeedPreparation:
		return RetryPolicy{MaxAttempts: 5, BaseDelay: 5 * time.Second, MaxDelay: 5 * time.Minute}
	case domain.JobQueueMaintenance:
		return RetryPolicy{MaxAttempts: 3, BaseDelay: time.Minute, MaxDelay: 30 * time.Minute}
	default:
		return RetryPolicy{MaxAttempts: 1, BaseDelay: time.Second, MaxDelay: time.Second}
	}
}

type executionOutcome struct {
	owner       string
	retry       RetryPolicy
	failure     error
	cancelled   bool
	interrupted bool
}

func (s *Service) finishExecution(job domain.Job, outcome executionOutcome) error {
	now := s.now()
	status := domain.JobStatusSucceeded
	var reason *string
	finishedAt := &now
	availableAt := now
	if outcome.cancelled {
		status = domain.JobStatusCancelled
		message := "cancelled by user"
		reason = &message
	} else if outcome.interrupted {
		status = domain.JobStatusRetryWait
		message := "worker interrupted"
		reason = &message
		finishedAt = nil
	} else if outcome.failure != nil {
		message := outcome.failure.Error()
		reason = &message
		if IsPermanent(outcome.failure) || job.Attempt >= job.MaxAttempts {
			status = domain.JobStatusDead
		} else {
			status = domain.JobStatusRetryWait
			finishedAt = nil
			availableAt = now.Add(s.retryDelay(outcome.retry, job.Attempt, outcome.failure))
		}
	}
	finishContext, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	changed, err := s.repository.Finish(finishContext, domain.JobFinish{
		ID: job.ID, Owner: outcome.owner, Status: status, Reason: reason,
		AvailableAt: availableAt, FinishedAt: finishedAt, UpdatedAt: now,
	})
	if changed {
		s.notifyChanged(job.WorkspaceID, job.ID)
	}
	return err
}

func (s *Service) notifyChanged(workspaceID, jobID uuid.UUID) {
	if s.notifier != nil {
		s.notifier.NotifyJobChanged(workspaceID, jobID)
	}
}

func (s *Service) retryDelay(policy RetryPolicy, attempt int, failure error) time.Duration {
	if s.config.RetryDelay != nil {
		return min(policy.MaxDelay, max(0, s.config.RetryDelay(policy, attempt, failure)))
	}
	var requested retryAfterError
	if errors.As(failure, &requested) {
		return min(requested.delay, policy.MaxDelay)
	}
	delay := policy.BaseDelay
	for index := 1; index < attempt; index++ {
		delay *= 2
		if delay >= policy.MaxDelay {
			delay = policy.MaxDelay
			break
		}
	}
	half := delay / 2
	random := min(1, max(0, s.config.RandomFloat64()))
	return half + time.Duration(random*float64(delay-half))
}

// retryAfterError preserves the failure cause while imposing a minimum retry delay.
type retryAfterError struct {
	cause error
	delay time.Duration
}

// Error returns the caller-facing failure description.
func (e retryAfterError) Error() string { return e.cause.Error() }

// Unwrap preserves the underlying cause for errors.Is and errors.As.
func (e retryAfterError) Unwrap() error { return e.cause }

// RetryAfter wraps a failure with a minimum retry delay while preserving its cause.
func RetryAfter(err error, delay time.Duration) error {
	if err == nil {
		return nil
	}
	return retryAfterError{cause: err, delay: max(0, delay)}
}

// permanentError preserves a handler failure while disabling retries.
type permanentError struct{ cause error }

// Error returns the caller-facing failure description.
func (e permanentError) Error() string { return e.cause.Error() }

// Unwrap preserves the underlying cause for errors.Is and errors.As.
func (e permanentError) Unwrap() error { return e.cause }

// Permanent marks a handler failure as non-retryable while preserving its cause for errors.Is and
// errors.As.
func Permanent(err error) error { return permanentError{cause: err} }

// IsPermanent reports whether an error chain marks the handler failure as non-retryable.
func IsPermanent(err error) bool {
	var target permanentError
	return errors.As(err, &target)
}

func encodeCursor(job domain.Job) string {
	payload, _ := json.Marshal(domain.JobCursor{CreatedAt: job.CreatedAt, ID: job.ID})
	return base64.RawURLEncoding.EncodeToString(payload)
}

func decodeCursor(value string) (*domain.JobCursor, error) {
	if value == "" {
		return nil, nil
	}
	payload, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil {
		return nil, err
	}
	var cursor domain.JobCursor
	if err := json.Unmarshal(payload, &cursor); err != nil || cursor.ID == uuid.Nil || cursor.CreatedAt.IsZero() {
		return nil, errors.New("invalid cursor")
	}
	return &cursor, nil
}

func workerDefaults(config, defaults Config) Config {
	if config.FetchConcurrency <= 0 {
		config.FetchConcurrency = defaults.FetchConcurrency
	}
	if config.GenerationConcurrency <= 0 {
		config.GenerationConcurrency = defaults.GenerationConcurrency
	}
	if config.RetrievalConcurrency <= 0 {
		config.RetrievalConcurrency = defaults.RetrievalConcurrency
	}
	if config.FeedPreparationConcurrency <= 0 {
		config.FeedPreparationConcurrency = defaults.FeedPreparationConcurrency
	}
	if config.MaintenanceConcurrency <= 0 {
		config.MaintenanceConcurrency = defaults.MaintenanceConcurrency
	}

	return config
}
