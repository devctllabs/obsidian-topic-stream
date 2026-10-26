package summaries

//go:generate go tool mockgen -source=service.go -destination=mocks/service.gen.go -package=mocks

import (
	"context"
	"errors"
	"time"

	"github.com/ethernity/obsidian-topic-stream/internal/domain"
	"github.com/ethernity/obsidian-topic-stream/internal/service/jobs"
	"github.com/google/uuid"
)

const GenerationJobKind = "summarize-item"

var summaryJob = jobs.JobDefinition{Kind: GenerationJobKind, ArgumentsVersion: 1, Queue: domain.JobQueueGeneration, Retry: jobs.RetryPolicy{MaxAttempts: 3, BaseDelay: 30 * time.Second, MaxDelay: 15 * time.Minute}}

// Repository reads owned input and durable annotation state.
type Repository interface {
	// ReadDigestSummaryBindings reads all saved context bindings in a bounded batch.
	ReadDigestSummaryBindings(ctx context.Context, workspaceID, issueID uuid.UUID) (map[domain.SummaryInputKey]domain.SummaryBinding, error)
	// GetSummaryResultForJob recovers a committed result before provider work or mutable settings reads.
	GetSummaryResultForJob(ctx context.Context, workspaceID, jobID uuid.UUID) (domain.AISummary, bool, error)
	// FindReadySummary reads a cache entry for exact input and actual Summary semantics.
	FindReadySummary(ctx context.Context, key domain.SummaryInputKey, configurationFingerprint string) (domain.AISummary, bool, error)
	// PublishSummary fences the actual Job attempt and fills only its still-matching context bindings.
	PublishSummary(ctx context.Context, publication domain.SummaryPublication) error
	// AdmitSummary atomically resolves shared work/cache and records requested context bindings.
	AdmitSummary(ctx context.Context, request domain.SummaryAdmission) (domain.SummaryAdmissionResult, error)
	// GetSummarySource resolves current canonical content or the exact saved Digest Item.
	GetSummarySource(ctx context.Context, workspaceID uuid.UUID, source domain.SummarySource) (domain.Item, bool, error)
	// GetSummaryInput reads a previously requested exact input version.
	GetSummaryInput(ctx context.Context, key domain.SummaryInputKey) (domain.SummaryInput, bool, error)
	// ReadSummaryBinding reads existing Job/result facts without admitting work.
	ReadSummaryBinding(ctx context.Context, query domain.SummaryBindingQuery) (domain.SummaryBinding, error)
}

// WorkspaceSettings supplies one current settings snapshot for an operation or actual attempt.
type WorkspaceSettings interface {
	// GetWorkspace returns non-secret Workspace configuration.
	GetWorkspace(ctx context.Context) (domain.WorkspaceSettings, error)
}

// Prompt is one bounded generation call with immutable task stage and source envelope semantics.
type Prompt struct {
	Endpoint                string
	Model                   string
	Stage                   string
	Instruction             string
	Language                string
	SourceLanguageReference string
	Title                   string
	Text                    string
	MaxOutputCharacters     int
}

// Generator performs an outbound structured text-generation call.
type Generator interface {
	// Summarize returns one plain-text fact extraction, reduction or final summary.
	Summarize(ctx context.Context, prompt Prompt) (string, error)
}

// Config supplies the independent Item-summary owner's dependencies.
type Config struct {
	WorkspaceID uuid.UUID
	Repository  Repository
	Settings    WorkspaceSettings
	Generator   Generator
	Jobs        jobs.Queue
	Events      jobs.StateNotifier
	Now         func() time.Time
}

// Service owns explicit asynchronous Item summaries and their version-specific reuse.
type Service struct{ config Config }

// New validates dependencies before registering the separate Generation Job kind.
func New(config Config) (*Service, error) {
	if config.WorkspaceID == uuid.Nil || config.Repository == nil || config.Settings == nil || config.Generator == nil || config.Jobs == nil || config.Events == nil || config.Now == nil {
		return nil, errors.New("summary service dependencies are required")
	}
	s := &Service{config: config}
	definition := summaryJob
	definition.RetryHandler = s.retryGeneration
	config.Jobs.RegisterDefinition(definition, s.handle)
	return s, nil
}
