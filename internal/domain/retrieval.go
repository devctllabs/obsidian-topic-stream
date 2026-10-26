package domain

import (
	"time"

	"github.com/google/uuid"
)

// RetrievalIndexStatus records the lifecycle of a derived retrieval generation.
type RetrievalIndexStatus string

const (
	RetrievalIndexBuilding RetrievalIndexStatus = "building"
	RetrievalIndexActive   RetrievalIndexStatus = "active"
)

// RetrievalIndex identifies one rebuildable vector-index generation. PostgreSQL owns its lifecycle;
// the referenced collection remains derived state.
type RetrievalIndex struct {
	WorkspaceID uuid.UUID
	ModelID     uuid.UUID
	Generation  int
	Collection  string
	Embedding   EmbeddingSettings
	Status      RetrievalIndexStatus
	CreatedAt   time.Time
	ActivatedAt *time.Time
	// SelectionGeneration changes only when the Workspace's requested model changes.
	SelectionGeneration   int64
	DesiredModelID        uuid.UUID
	CollectionIncarnation uuid.UUID
	// Initialized means the Serving collection is the same instance that completed initialization.
	Initialized bool
}

// RetrievalIndexFailure records the failed generation and its transition Job.
type RetrievalIndexFailure struct {
	WorkspaceID         uuid.UUID
	ModelID             uuid.UUID
	JobID               uuid.UUID
	Generation          int
	Reason              string
	FailedAt            time.Time
	SelectionGeneration int64
	LeaseOwner          string
}

// ModelTransitionStart starts a durable rebuild for a desired Embedding Model.
type ModelTransitionStart struct {
	WorkspaceID         uuid.UUID
	ModelID             uuid.UUID
	JobID               uuid.UUID
	StartedAt           time.Time
	SelectionGeneration int64
	LeaseOwner          string
	Force               bool
}

// ModelTransitionCutover switches serving retrieval to a completed generation.
type ModelTransitionCutover struct {
	WorkspaceID         uuid.UUID
	ModelID             uuid.UUID
	JobID               uuid.UUID
	ActivatedAt         time.Time
	SelectionGeneration int64
	LeaseOwner          string
}

// ModelReconciliationQuery bounds inspection of Workspace transition state, including healthy rows.
type ModelReconciliationQuery struct {
	AfterWorkspaceID *uuid.UUID
	Limit            int
}

// ModelReconciliationRequest identifies one durable model choice requiring a transition Job.
type ModelReconciliationRequest struct {
	WorkspaceID         uuid.UUID
	ModelID             uuid.UUID
	SelectionGeneration int64
}

// ModelReconciliationPage preserves cursor progress even when inspected Workspaces need no Job.
type ModelReconciliationPage struct {
	Requests   []ModelReconciliationRequest
	NextCursor *uuid.UUID
}

// ModelTransition is verified public linkage for a model-selection operation, independent of its working pointer.
type ModelTransition struct {
	ModelID             uuid.UUID  `json:"modelId"`
	SelectionGeneration int64      `json:"selectionGeneration"`
	JobID               *uuid.UUID `json:"jobId"`
	Status              string     `json:"status"`
	IsCurrent           bool       `json:"isCurrent"`
	CanRetry            bool       `json:"canRetry"`
}
