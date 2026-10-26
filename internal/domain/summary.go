package domain

import (
	"strings"
	"time"

	"github.com/google/uuid"
)

// ContentOrigin records whether canonical Markdown came from full content or a descriptive fallback.
type ContentOrigin string

const (
	ContentOriginUnknown               ContentOrigin = "unknown"
	ContentOriginArticleBody           ContentOrigin = "article_body"
	ContentOriginArticleDescription    ContentOrigin = "article_description"
	ContentOriginPaperAbstract         ContentOrigin = "paper_abstract"
	ContentOriginVideoDescription      ContentOrigin = "video_description"
	ContentOriginRepositoryReadme      ContentOrigin = "repository_readme"
	ContentOriginRepositoryDescription ContentOrigin = "repository_description"
	ContentOriginEmpty                 ContentOrigin = "empty"
)

// SummaryInputKey scopes one actual model-input fingerprint to its owned Item.
type SummaryInputKey struct {
	WorkspaceID   uuid.UUID
	ItemID        uuid.UUID
	SourceVersion string
}

// SummaryInput freezes exactly the server-owned title, source text and provenance requested.
type SummaryInput struct {
	SummaryInputKey
	Title    string
	Markdown string
	Origin   ContentOrigin
}

// Supported reports whether provenance and actual text permit Item AI generation.
func (input SummaryInput) Supported() bool {
	return strings.TrimSpace(input.Markdown) != "" && (input.Origin == ContentOriginArticleBody || input.Origin == ContentOriginRepositoryReadme)
}

// SummaryBinding selects an input's current work and independently readable ready annotation.
type SummaryBinding struct {
	Job    *Job
	Result *AISummary
}

// SummaryBindingQuery selects the shared Item version or one saved Digest annotation binding.
type SummaryBindingQuery struct {
	SummaryInputKey
	IssueID *uuid.UUID
}

// SummaryConfiguration captures only actual Summary model, prompts and generation semantics.
type SummaryConfiguration struct {
	Endpoint             string `json:"endpoint"`
	Model                string `json:"model"`
	Language             string `json:"language"`
	MaxInputChars        int    `json:"maxInputChars"`
	MaxSummaryCharacters int    `json:"maxSummaryCharacters"`
	ChunkInstruction     string `json:"chunkInstruction"`
	ReduceInstruction    string `json:"reduceInstruction"`
	FinalInstruction     string `json:"finalInstruction"`
	InstructionsVersion  string `json:"instructionsVersion"`
	AlgorithmVersion     string `json:"algorithmVersion"`
	SchemaVersion        string `json:"schemaVersion"`
}

// SummaryAdmission atomically coordinates an explicit request with input-scoped active work/cache.
type SummaryAdmission struct {
	Input   SummaryInput
	IssueID *uuid.UUID
	// RetrySourceJobID permanently binds an explicit Retry to its successor.
	RetrySourceJobID *uuid.UUID
	// ConfigurationFingerprint selects cache reuse, never an admission-time settings snapshot.
	ConfigurationFingerprint string
	Job                      Job
}

// SummaryAdmissionResult reports committed work and whether a new Job needs announcing.
type SummaryAdmissionResult struct {
	Job     Job
	Created bool
}

// SummaryPublication commits a generated annotation only for its actual running attempt.
type SummaryPublication struct {
	Input                    SummaryInputKey
	Annotation               AISummary
	Configuration            SummaryConfiguration
	ConfigurationFingerprint string
	ReusedFrom               *uuid.UUID
	LeaseOwner               string
	Attempt                  int
}

// SummarySource selects owned canonical content or one immutable Digest Item snapshot.
type SummarySource struct {
	ItemID  uuid.UUID
	IssueID *uuid.UUID
	// SourceVersion is an optional exact-version read; requests always use server-owned content.
	SourceVersion string
}

// ItemSummaryState describes the separately requested AI annotation of one Item input version.
type ItemSummaryState string

const (
	ItemSummaryNotRequested ItemSummaryState = "not_requested"
	ItemSummaryPending      ItemSummaryState = "pending"
	ItemSummaryReady        ItemSummaryState = "ready"
	ItemSummaryFailed       ItemSummaryState = "failed"
)

// AISummary is an immutable generated annotation with its actual input and model provenance.
type AISummary struct {
	ID                   uuid.UUID
	GenerationJobID      uuid.UUID
	SourceVersion        string
	Text                 string
	Model                string
	Language             string
	MaxInputChars        int
	MaxSummaryCharacters int
	InstructionsVersion  string
	AlgorithmVersion     string
	CreatedAt            time.Time
}

// ItemSummary separates ready readability, input support and permission to request generation.
type ItemSummary struct {
	SourceVersion     string
	Supported         bool
	CanGenerate       bool
	UnavailableReason *string
	State             ItemSummaryState
	JobID             *uuid.UUID
	Failure           *string
	Retryable         bool
	Result            *AISummary
}
