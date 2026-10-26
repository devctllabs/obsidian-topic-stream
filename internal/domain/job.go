package domain

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

// JobStatus records queued, leased, retrying, or terminal durable work.
type JobStatus string

// JobQueue selects a closed execution class with its own worker capacity and retry policy.
type JobQueue string

const (
	JobQueueFetch           JobQueue = "fetch"
	JobQueueGeneration      JobQueue = "generation"
	JobQueueRetrieval       JobQueue = "retrieval"
	JobQueueFeedPreparation JobQueue = "feed-preparation"
	JobQueueMaintenance     JobQueue = "maintenance"
)

const (
	JobStatusQueued    JobStatus = "queued"
	JobStatusRunning   JobStatus = "running"
	JobStatusRetryWait JobStatus = "retry_wait"
	JobStatusSucceeded JobStatus = "succeeded"
	JobStatusDead      JobStatus = "dead"
	JobStatusCancelled JobStatus = "cancelled"
)

// Terminal reports whether the status is succeeded, dead, or cancelled.
func (s JobStatus) Terminal() bool {
	return s == JobStatusSucceeded || s == JobStatusDead || s == JobStatusCancelled
}

// Job is the durable state of leased background work. Attempt counts claims, including the
// currently running claim, while cancellation is cooperative once work is running.
type Job struct {
	// ModelTransition is present only for a durably verified model-operation binding.
	ModelTransition *ModelTransition

	// Progress describes the fixed initial Item cohort for a background retrieval rebuild.
	Progress         *JobProgress
	ID               uuid.UUID
	WorkspaceID      uuid.UUID
	Kind             string
	Queue            JobQueue
	ArgumentsVersion int
	Arguments        json.RawMessage
	// IdempotencyKey deduplicates active work within a Workspace; nil disables key-based deduplication.
	IdempotencyKey *string
	Status         JobStatus
	Attempt        int
	MaxAttempts    int
	// AvailableAt is the earliest claim time, including retry backoff.
	AvailableAt time.Time
	// LeaseOwner and LeaseExpiresAt fence a running attempt; nil means no current lease.
	LeaseOwner     *string
	LeaseExpiresAt *time.Time
	// CancellationRequested is observed cooperatively by the current worker heartbeat.
	CancellationRequested bool
	Reason                *string
	CreatedAt             time.Time
	StartedAt             *time.Time
	FinishedAt            *time.Time
	UpdatedAt             time.Time
}

// JobProgress reports durable historical-work counts independently of final Job status.
// A nil total means preparation has not captured the initial cohort yet.
type JobProgress struct {
	Phase          string
	ProcessedItems int64
	TotalItems     *int64
	UpdatedAt      time.Time
}

// JobCursor identifies a creation-time and ID boundary for Job pagination.
type JobCursor struct {
	CreatedAt time.Time
	ID        uuid.UUID
}

// JobList selects a bounded page of Workspace Jobs with an optional status filter.
type JobList struct {
	WorkspaceID uuid.UUID
	Status      *JobStatus
	Cursor      *JobCursor
	Limit       int
}

// JobFinish proposes the result of an attempt owned by a particular lease holder.
type JobFinish struct {
	ID uuid.UUID
	// Owner must still hold the Job lease before this transition may be committed.
	Owner       string
	Status      JobStatus
	Reason      *string
	AvailableAt time.Time
	FinishedAt  *time.Time
	UpdatedAt   time.Time
}

// JobHeartbeat extends an owned lease and observes cooperative cancellation.
type JobHeartbeat struct {
	ID             uuid.UUID
	Owner          string
	Now            time.Time
	LeaseExpiresAt time.Time
}

// JobIdentity identifies a changed Job and the Workspace to notify.
type JobIdentity struct {
	ID          uuid.UUID
	WorkspaceID uuid.UUID
}

// JobClaim reports both leased work and every Job whose durable state changed while claiming.
// ChangedJobs includes lease-expired Jobs moved to dead even when no new Job was leased.
type JobClaim struct {
	Job         Job
	ChangedJobs []JobIdentity
}

// JobClaimRequest selects a queue and supplies the owner and lease interval for one claim.
type JobClaimRequest struct {
	Queue          JobQueue
	Owner          string
	Now            time.Time
	LeaseExpiresAt time.Time
}
