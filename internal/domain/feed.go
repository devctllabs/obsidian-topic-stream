package domain

import (
	"time"

	"github.com/google/uuid"
)

const JobKindPrepareFeed = "prepare-feed"

// FeedReadQuery selects a saved Feed view and optional fixed-head traversal tokens.
type FeedReadQuery struct {
	Limit    int
	Cursor   string
	Snapshot string
	// TopicID is nil for the shared All Topics view.
	TopicID *uuid.UUID
}

// FeedRefreshCommand requests one bounded publication batch for All Topics or one Topic.
type FeedRefreshCommand struct {
	Limit   int
	TopicID *uuid.UUID
}

// FeedEntry combines canonical Item content with its saved admission explanation.
type FeedEntry struct {
	Item     Item
	Score    float64
	Summary  string
	Signals  map[string]float64
	Pipeline string
}

// FeedPage contains saved publications and tokens for traversal under a fixed shared head.
type FeedPage struct {
	Items       []FeedEntry
	NextCursor  *string
	Snapshot    string
	GeneratedAt time.Time
}

// FeedPublication is the permanent admission fact shared by all Topic views.
type FeedPublication struct {
	WorkspaceID uuid.UUID
	ItemID      uuid.UUID
	BatchID     uuid.UUID
	PublishedAt time.Time
	// Ordinal is immutable and increases within the Workspace's shared publication history.
	Ordinal int64
	// MatchedTopicIDs freezes admission membership; reads overlay current explicit corrections.
	MatchedTopicIDs []uuid.UUID
	TopicInputs     map[uuid.UUID]FeedCandidateInputs
	// Score freezes the admission score rather than recomputing it during reads.
	Score    float64
	Signals  map[string]float64
	Pipeline string
	ModelID  uuid.UUID
}

// FeedCandidateInputs fingerprints the settings, Topic, model, and gates used to prepare candidates.
type FeedCandidateInputs struct {
	TopicRevision     int
	CorrectionEpoch   int64
	CorrectionVersion int64
	ModelID           uuid.UUID
	// CandidateLimit is the preparation bound captured with these inputs.
	CandidateLimit int
	GateVersion    string
	// RRFK is the reciprocal-rank fusion smoothing constant used during preparation.
	RRFK int
	// EmbeddingKey identifies compatible cached Include and Exclude embeddings.
	EmbeddingKey string
	GateInclude  float64
	GateExclude  float64
}

// FeedCandidate is an unpublished Item prepared and ranked within one Topic.
type FeedCandidate struct {
	SourceID    uuid.UUID
	ContentAt   time.Time
	Feedback    *Feedback
	WorkspaceID uuid.UUID
	TopicID     uuid.UUID
	ItemID      uuid.UUID
	Position    int
	Score       float64
	Signals     map[string]float64
	Inputs      FeedCandidateInputs
}

// FeedSnapshot binds a traversal to a Workspace, optional Topic filter, and committed head.
type FeedSnapshot struct {
	Version     int
	WorkspaceID uuid.UUID
	TopicID     *uuid.UUID
	// HeadOrdinal fixes the newest visible publication; later admissions stay outside this traversal.
	HeadOrdinal int64
	CreatedAt   time.Time
}

// FeedPublicationQuery selects visible matching publications at or below the head and before the cursor.
type FeedPublicationQuery struct {
	WorkspaceID uuid.UUID
	TopicID     *uuid.UUID
	HeadOrdinal int64
	// BeforeOrdinal is an exclusive continuation boundary; zero starts at the fixed head.
	BeforeOrdinal int64
	Limit         int
}

// FeedPublishedItem pairs saved admission facts with current canonical Item state.
type FeedPublishedItem struct {
	Publication FeedPublication
	Item        Item
}

// FeedPublishCommand proposes an ordered batch whose eligibility is rechecked at commit.
type FeedPublishCommand struct {
	WorkspaceID  uuid.UUID
	TopicID      *uuid.UUID
	PublishedAt  time.Time
	Publications []FeedPublication
}

// FeedPublishResult reports the resulting shared head and publications actually admitted.
type FeedPublishResult struct {
	HeadOrdinal  int64
	Publications []FeedPublication
}

// FeedPreparationRequest marks candidate preparation due, optionally invalidating current inputs.
type FeedPreparationRequest struct {
	WorkspaceID uuid.UUID
	TopicID     *uuid.UUID
	Invalidate  bool
	At          time.Time
}

// FeedPreparationQuery selects bounded due Topic preparation work for a Workspace.
type FeedPreparationQuery struct {
	GateVersion string
	WorkspaceID uuid.UUID
	At          time.Time
	Limit       int
}

// FeedPreparationWork captures one Topic's preparation versions and reusable gate embeddings.
type FeedPreparationWork struct {
	// NextEligibleAt schedules age-based reconsideration; nil means no deferred eligibility boundary.
	NextEligibleAt *time.Time
	WorkspaceID    uuid.UUID
	TopicID        uuid.UUID
	// RequestedVersion is the captured request watermark; newer requests remain dirty after this commit.
	RequestedVersion int64
	// InputVersion fences preparation against changes that invalidate candidate inputs.
	InputVersion     int64
	Inputs           FeedCandidateInputs
	IncludeEmbedding []float64
	ExcludeEmbedding []float64
}

// FeedCandidateReplacement commits prepared candidates under Job lease and input-version fences.
type FeedCandidateReplacement struct {
	Work           FeedPreparationWork
	Inputs         FeedCandidateInputs
	Candidates     []FeedCandidate
	JobID          uuid.UUID
	LeaseOwner     string
	At             time.Time
	NextEligibleAt *time.Time
}

// FeedCandidateQuery bounds eligible admission candidates at one operation time.
type FeedCandidateQuery struct {
	GateVersion    string
	WorkspaceID    uuid.UUID
	At             time.Time
	CandidateLimit int
}

// FeedTopicPreparation freezes one serving generation and settings snapshot for background retrieval.
type FeedTopicPreparation struct {
	Topic            Topic
	Generation       RetrievalIndex
	Settings         WorkspaceSettings
	Inputs           FeedCandidateInputs
	IncludeEmbedding []float64
	ExcludeEmbedding []float64
	At               time.Time
}

// FeedTopicPrepared returns candidate facts and reusable embeddings for a captured preparation snapshot.
type FeedTopicPrepared struct {
	Inputs           FeedCandidateInputs
	Candidates       []FeedCandidate
	IncludeEmbedding []float64
	ExcludeEmbedding []float64
}

// FeedPreparationDueQuery pages through Workspaces with due candidate preparation.
type FeedPreparationDueQuery struct {
	At          time.Time
	After       *uuid.UUID
	Limit       int
	GateVersion string
}

// FeedRankingRequest freezes settings and serving generation for one common personal pass.
type FeedRankingRequest struct {
	WorkspaceID uuid.UUID
	Generation  RetrievalIndex
	Settings    WorkspaceSettings
	Candidates  []FeedCandidate
	At          time.Time
}
