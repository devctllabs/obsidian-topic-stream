package indexing

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/ethernity/obsidian-topic-stream/internal/domain"
	"github.com/ethernity/obsidian-topic-stream/internal/service/jobs"
	"github.com/google/uuid"
)

const JobKind = "index-item"
const argumentsVersion = 1
const repairBatchSize = 100

var indexItemJob = jobs.JobDefinition{
	Kind: JobKind, ArgumentsVersion: argumentsVersion, Queue: domain.JobQueueRetrieval,
	Retry: jobs.RetryPolicy{MaxAttempts: 5, BaseDelay: 5 * time.Second, MaxDelay: 5 * time.Minute},
}

// PendingRepository pages through pending projection commits for one Workspace.
type PendingRepository interface {
	// ListPendingItemProjections returns a bounded page of pending projection commits for the Workspace.
	ListPendingItemProjections(ctx context.Context, workspaceID uuid.UUID, after *uuid.UUID, limit int) ([]domain.ItemProjectionCommit, error)
}

// Processor delivers a committed Item projection to derived indexes.
type Processor interface {
	// DeliverItemProjection delivers the committed Item revision to its pending model indexes.
	DeliverItemProjection(ctx context.Context, commit domain.ItemProjectionCommit) error
}

// FeedInvalidator publishes advisory Feed invalidations after durable changes.
type FeedInvalidator interface {
	// InvalidateFeed announces a change affecting the Workspace's Feed views.
	InvalidateFeed(workspaceID uuid.UUID, itemID uuid.UUID)
}

// Service emits feed invalidation only after changed Item content is present in the active retrieval
// generation, keeping the advisory event behind its readable state.
type Service struct {
	workspaceID uuid.UUID
	pending     PendingRepository
	jobs        jobs.Queue
	processor   Processor
	events      FeedInvalidator
}

type arguments struct {
	ItemID             uuid.UUID `json:"itemId"`
	ProjectionRevision uuid.UUID `json:"projectionRevision"`
}

// Config supplies Workspace projection discovery, durable Jobs, delivery, and invalidation dependencies.
type Config struct {
	WorkspaceID uuid.UUID
	Pending     PendingRepository
	Jobs        jobs.Queue
	Processor   Processor
	Events      FeedInvalidator
}

// New validates Workspace projection dependencies and registers durable delivery work.
func New(config Config) (*Service, error) {
	if config.WorkspaceID == uuid.Nil || config.Pending == nil || config.Jobs == nil ||
		config.Processor == nil || config.Events == nil {
		return nil, errors.New("indexing service dependencies are required")
	}
	service := &Service{
		workspaceID: config.WorkspaceID, pending: config.Pending, jobs: config.Jobs,
		processor: config.Processor, events: config.Events,
	}
	config.Jobs.RegisterDefinition(indexItemJob, service.handle)
	return service, nil
}

// Repair recreates durable Jobs for projection work committed before its enqueue completed.
func (s *Service) Repair(ctx context.Context) (int, error) {
	var after *uuid.UUID
	total := 0
	for {
		commits, err := s.pending.ListPendingItemProjections(ctx, s.workspaceID, after, repairBatchSize)
		if err != nil {
			return total, fmt.Errorf("list pending item projections: %w", err)
		}
		for _, commit := range commits {
			if _, err := s.Enqueue(ctx, commit); err != nil {
				return total, err
			}
			total++
		}
		if len(commits) < repairBatchSize {
			return total, nil
		}
		last := commits[len(commits)-1].ItemID
		after = &last
	}
}

// Enqueue submits durable projection delivery and returns an existing equivalent active Job when present.
func (s *Service) Enqueue(ctx context.Context, commit domain.ItemProjectionCommit) (domain.Job, error) {
	if commit.WorkspaceID != s.workspaceID {
		return domain.Job{}, errors.New("cannot index an item outside the configured workspace")
	}
	return enqueueItemProjection(ctx, s.jobs, commit)
}
func enqueueItemProjection(ctx context.Context, queue jobs.Queue, commit domain.ItemProjectionCommit) (domain.Job, error) {
	if commit.ItemID == uuid.Nil || commit.ProjectionRevision == uuid.Nil || commit.WorkspaceID == uuid.Nil {
		return domain.Job{}, errors.New("invalid projection commit")
	}
	job, err := queue.Enqueue(ctx, jobs.EnqueueRequest{
		WorkspaceID: commit.WorkspaceID, Definition: indexItemJob,
		Arguments: arguments{
			ItemID: commit.ItemID, ProjectionRevision: commit.ProjectionRevision,
		},
		IdempotencyKey: JobKind + ":" + commit.ItemID.String() + ":" + commit.ProjectionRevision.String(),
	})
	if err != nil {
		return domain.Job{}, fmt.Errorf("enqueue item indexing: %w", err)
	}
	return job, nil
}

func (s *Service) handle(ctx context.Context, payload json.RawMessage) error {
	var arguments arguments
	if err := json.Unmarshal(payload, &arguments); err != nil ||
		arguments.ItemID == uuid.Nil || arguments.ProjectionRevision == uuid.Nil {
		return jobs.Permanent(errors.New("invalid index-item arguments"))
	}
	execution, _ := jobs.CurrentExecution(ctx)
	if err := s.processor.DeliverItemProjection(ctx, domain.ItemProjectionCommit{
		WorkspaceID: s.workspaceID, ItemID: arguments.ItemID,
		ProjectionRevision: arguments.ProjectionRevision,
		Execution:          domain.ProjectionExecution{JobID: execution.JobID, LeaseOwner: execution.LeaseOwner, Attempt: execution.Attempt},
	}); err != nil {
		return fmt.Errorf("deliver item projection: %w", err)
	}
	s.events.InvalidateFeed(s.workspaceID, arguments.ItemID)
	return nil
}
