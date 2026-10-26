package domain

import (
	"time"

	"github.com/google/uuid"
)

// ItemKind selects the Article, Paper, Video, or Repository content variant.
type ItemKind string

const (
	ItemKindArticle    ItemKind = "article"
	ItemKindPaper      ItemKind = "paper"
	ItemKindVideo      ItemKind = "video"
	ItemKindRepository ItemKind = "repository"
)

// TopicMode selects ordinary or strict semantic membership thresholds.
type TopicMode string

// TopicMembership expresses an automatic, explicitly included, or explicitly excluded membership.
type TopicMembership string

const (
	TopicModeOrdinary TopicMode = "ordinary"
	TopicModeStrict   TopicMode = "strict"
)

const (
	TopicMembershipAutomatic TopicMembership = "automatic"
	TopicMembershipInclude   TopicMembership = "include"
	TopicMembershipExclude   TopicMembership = "exclude"
)

// Topic defines source and Item-kind scope with natural-language Include and Exclude criteria.
type Topic struct {
	ID          uuid.UUID
	WorkspaceID uuid.UUID
	Name        string
	// SourceIDs is unrestricted when empty.
	SourceIDs []uuid.UUID
	// ItemKinds limits the kinds eligible for this Topic.
	ItemKinds []ItemKind
	Include   string
	Exclude   string
	Mode      TopicMode
	Enabled   bool
	// Revision advances for semantic scope, criteria, or mode changes, preserving historical meaning.
	Revision int
	// CorrectionEpoch advances when the complete correction set is explicitly cleared.
	CorrectionEpoch int64
	// CorrectionVersion advances as individual current membership judgments change.
	CorrectionVersion int64
	CreatedAt         time.Time
	UpdatedAt         time.Time
}

// TopicCorrection is the current explicit membership judgment for one Workspace Item and Topic.
type TopicCorrection struct {
	WorkspaceID uuid.UUID
	ItemID      uuid.UUID
	TopicID     uuid.UUID
	Membership  TopicMembership
	UpdatedAt   time.Time
}

// TopicCorrectionProjection carries membership and its epoch into derived retrieval state.
type TopicCorrectionProjection struct {
	TopicID         uuid.UUID
	Membership      TopicMembership
	CorrectionEpoch int64
}

// TopicUpdate combines a Topic edit with semantic revision and correction-reset effects.
type TopicUpdate struct {
	Topic            Topic
	SemanticChanged  bool
	ClearCorrections bool
}

// TopicUpdateResult returns projection work created by changes to Topic corrections.
type TopicUpdateResult struct {
	ProjectionCommits []ItemProjectionCommit
}

// TopicGateCalibration stores model-specific thresholds with their Topic, correction, and algorithm
// inputs.
type TopicGateCalibration struct {
	TopicID           uuid.UUID
	EmbeddingModelID  uuid.UUID
	TopicRevision     int
	CorrectionEpoch   int64
	CorrectionVersion int64
	AlgorithmVersion  string
	IncludeThreshold  float64
	ExcludeThreshold  float64
	UpdatedAt         time.Time
}

// TopicCalibrationSnapshot freezes one Topic, model, and current correction set for calibration.
type TopicCalibrationSnapshot struct {
	Topic       Topic
	Model       EmbeddingModel
	Corrections []TopicCorrection
}
