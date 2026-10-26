package domain

import "github.com/google/uuid"

// ProjectionRepairCursor identifies the last pending Item inspected across Workspaces.
type ProjectionRepairCursor struct {
	WorkspaceID uuid.UUID
	ModelID     uuid.UUID
	ItemID      uuid.UUID
	// Exhausted advances to the next eligible destination, including after an empty destination.
	Exhausted bool
}

// ProjectionRepairQuery bounds pending Item inspection, including already active Jobs.
type ProjectionRepairQuery struct {
	After *ProjectionRepairCursor
	Limit int
}

// ProjectionRepairPage carries missing Jobs and progress through all inspected pending Items.
type ProjectionRepairPage struct {
	Commits    []ItemProjectionCommit
	NextCursor *ProjectionRepairCursor
}

// ModelProjectionQuery bounds one ordered page of current pending projections for a model.
type ModelProjectionQuery struct {
	WorkspaceID uuid.UUID
	ModelID     uuid.UUID
	AfterItemID *uuid.UUID
	Limit       int
}

// ModelProjectionSeedCommand resumes bounded initialization of one durable model rebuild.
type ModelProjectionSeedCommand struct {
	WorkspaceID uuid.UUID
	ModelID     uuid.UUID
	JobID       uuid.UUID
	Limit       int
	LeaseOwner  string
}

// ItemProjection captures canonical content and user state for replacement in derived indexes.
type ItemProjection struct {
	// Version increases monotonically per Item so older deliveries cannot overwrite newer payloads.
	Version int64
	// Published marks lifetime Feed admission and excludes the Item from future candidate retrieval.
	Published bool
	// Revision identifies the exact canonical state acknowledged by a successful delivery.
	Revision         uuid.UUID
	Item             Item
	Feedback         *Feedback
	TopicCorrections []TopicCorrectionProjection
}

// ItemProjectionDestination pairs a model with its retained synchronization generation.
type ItemProjectionDestination struct {
	EmbeddingModel
	Generation            uuid.UUID
	CollectionIncarnation uuid.UUID
}

// ItemProjectionWork pairs the latest canonical projection with its eligible model deliveries.
type ItemProjectionWork struct {
	Projection ItemProjection
	Models     []ItemProjectionDestination
}

// ItemProjectionDelivery acknowledges one Item revision delivered to one model.
type ItemProjectionDelivery struct {
	WorkspaceID uuid.UUID
	ItemID      uuid.UUID
	ModelID     uuid.UUID
	Revision    uuid.UUID
	// ModelGeneration fences delivery from an evicted or invalidated model checkpoint.
	ModelGeneration uuid.UUID
	Execution       ProjectionExecution
}

// ItemProjectionWriteResult preserves successful destinations even when another model failed.
// Batch results follow input order and do not acknowledge durable pending work.
type ItemProjectionWriteResult struct {
	Deliveries []ItemProjectionDelivery
	Err        error
}

// ProjectionExecution identifies the durable lease permitted to acknowledge delivery.
// A zero JobID is reserved for direct non-Job delivery.
type ProjectionExecution struct {
	JobID      uuid.UUID
	LeaseOwner string
	Attempt    int
}

// ItemProjectionCommit identifies durable projection work created by a canonical write.
type ItemProjectionCommit struct {
	WorkspaceID        uuid.UUID
	ItemID             uuid.UUID
	ProjectionRevision uuid.UUID
	Execution          ProjectionExecution
}

// FeedbackCommit returns saved feedback alongside the projection work created by the write.
type FeedbackCommit struct {
	Feedback
	ItemProjectionCommit
}
