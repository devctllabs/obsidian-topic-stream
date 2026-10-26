package sources

//go:generate go tool mockgen -source=service.go -destination=mocks/service.gen.go -package=mocks

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/ethernity/obsidian-topic-stream/internal/domain"
	"github.com/ethernity/obsidian-topic-stream/internal/platform/httpfetch"
	"github.com/ethernity/obsidian-topic-stream/internal/service/ingest"
	"github.com/ethernity/obsidian-topic-stream/internal/service/jobs"
	"github.com/google/uuid"
)

const sourceJobArgumentsVersion = 1

var (
	syncSourceJob = jobs.JobDefinition{
		Kind: "sync-source", ArgumentsVersion: sourceJobArgumentsVersion, Queue: domain.JobQueueFetch,
		Retry: jobs.RetryPolicy{MaxAttempts: 5, BaseDelay: 30 * time.Second, MaxDelay: time.Hour},
	}
	syncAllSourcesJob = jobs.JobDefinition{
		Kind: "sync-all-sources", ArgumentsVersion: sourceJobArgumentsVersion, Queue: domain.JobQueueFetch,
		Retry: jobs.RetryPolicy{MaxAttempts: 5, BaseDelay: 30 * time.Second, MaxDelay: time.Hour},
	}
)

var (
	ErrNotFound      = errors.New("source not found")
	youtubeChannelID = regexp.MustCompile(`^UC[A-Za-z0-9_-]{20,}$`)
)

// Repository persists Workspace Sources and successful poll timestamps.
type Repository interface {
	// Create stores a new Source.
	Create(ctx context.Context, source domain.Source) error
	// List returns the matching Source records.
	List(ctx context.Context, workspaceID uuid.UUID) ([]domain.Source, error)
	// Get returns the Source and whether it exists.
	Get(ctx context.Context, workspaceID uuid.UUID, sourceID uuid.UUID) (domain.Source, bool, error)
	// Update updates the Source.
	Update(ctx context.Context, source domain.Source) error
	// Delete deletes the Source and reports whether it existed.
	Delete(ctx context.Context, workspaceID uuid.UUID, sourceID uuid.UUID) (bool, error)
	// SetLastSyncedAt records the Source's successful synchronization time.
	SetLastSyncedAt(ctx context.Context, sourceID uuid.UUID, at time.Time) error
}

// CreateInput supplies a new Workspace Source and its selection or polling policy.
type CreateInput struct {
	Name                string
	Config              domain.SourceConfig
	Enabled             bool
	PollIntervalMinutes int
}

// UpdateInput changes supplied Source fields; nil preserves the corresponding value.
type UpdateInput struct {
	Name                *string
	Config              *domain.SourceConfig
	Enabled             *bool
	PollIntervalMinutes *int
}

// Service manages Workspace Sources and durable upstream synchronization.
type Service struct {
	workspaceID uuid.UUID
	repository  Repository
	ingester    *ingest.Service
	jobs        jobs.Queue
	now         func() time.Time
}

// Config supplies dependencies and operating settings for the sources service.
type Config struct {
	WorkspaceID uuid.UUID
	Repository  Repository
	Ingester    *ingest.Service
	Jobs        jobs.Queue
	Now         func() time.Time
}

// New binds Workspace Source operations and registers synchronization handlers when a Job queue is supplied.
func New(config Config) Service {
	service := Service{
		workspaceID: config.WorkspaceID, repository: config.Repository,
		ingester: config.Ingester, jobs: config.Jobs, now: config.Now,
	}
	if config.Jobs != nil {
		config.Jobs.RegisterDefinition(syncSourceJob, service.handleSyncSource)
		config.Jobs.RegisterDefinition(syncAllSourcesJob, service.handleSyncAll)
	}
	return service
}

// Create validates Source configuration and persists a new Workspace Source.
func (s Service) Create(ctx context.Context, input CreateInput) (domain.Source, error) {
	if err := domain.JoinValidation(domain.CheckRequired("name", strings.TrimSpace(input.Name) != ""),
		validateConfig(input.Config), validateInterval(input.PollIntervalMinutes)); err != nil {
		return domain.Source{}, err
	}
	now := s.now()
	source := domain.Source{
		ID:                  uuid.New(),
		WorkspaceID:         s.workspaceID,
		Name:                strings.TrimSpace(input.Name),
		Config:              input.Config,
		Enabled:             input.Enabled,
		PollIntervalMinutes: input.PollIntervalMinutes,
		CreatedAt:           now,
		UpdatedAt:           now,
	}
	if err := s.repository.Create(ctx, source); err != nil {
		return domain.Source{}, fmt.Errorf("create source: %w", err)
	}
	return source, nil
}

// List returns Sources belonging to the service's Workspace.
func (s Service) List(ctx context.Context) ([]domain.Source, error) {
	result, err := s.repository.List(ctx, s.workspaceID)
	if err != nil {
		return nil, fmt.Errorf("list sources: %w", err)
	}
	sort.Slice(result, func(left, right int) bool { return result[left].Name < result[right].Name })
	return result, nil
}

// Get returns a Workspace Source or ErrNotFound.
func (s Service) Get(ctx context.Context, sourceID uuid.UUID) (domain.Source, error) {
	source, found, err := s.repository.Get(ctx, s.workspaceID, sourceID)
	if err != nil {
		return domain.Source{}, fmt.Errorf("get source: %w", err)
	}
	if !found {
		return domain.Source{}, ErrNotFound
	}
	return source, nil
}

// Update applies supplied Source fields after validating the resulting configuration.
func (s Service) Update(ctx context.Context, sourceID uuid.UUID, input UpdateInput) (domain.Source, error) {
	source, err := s.Get(ctx, sourceID)
	if err != nil {
		return domain.Source{}, err
	}
	if input.Name != nil {
		name := strings.TrimSpace(*input.Name)
		source.Name = name
	}
	var checks []error
	if input.Name != nil {
		checks = append(checks, domain.CheckRequired("name", source.Name != ""))
	}
	if input.Config != nil {
		checks = append(checks, validateConfig(*input.Config))
		source.Config = *input.Config
	}
	if input.Enabled != nil {
		source.Enabled = *input.Enabled
	}
	if input.PollIntervalMinutes != nil {
		checks = append(checks, validateInterval(*input.PollIntervalMinutes))
		source.PollIntervalMinutes = *input.PollIntervalMinutes
	}
	if err := domain.JoinValidation(checks...); err != nil {
		return domain.Source{}, err
	}
	source.UpdatedAt = s.now()
	if err := s.repository.Update(ctx, source); err != nil {
		return domain.Source{}, fmt.Errorf("update source: %w", err)
	}
	return source, nil
}

// Delete removes a Workspace Source, returning ErrNotFound when absent.
func (s Service) Delete(ctx context.Context, sourceID uuid.UUID) error {
	found, err := s.repository.Delete(ctx, s.workspaceID, sourceID)
	if err != nil {
		return fmt.Errorf("delete source: %w", err)
	}
	if !found {
		return ErrNotFound
	}
	return nil
}

// Sync enqueues durable work and deduplicates concurrent requests for the same Source.
func (s Service) Sync(ctx context.Context, sourceID uuid.UUID) (domain.Job, error) {
	if _, err := s.Get(ctx, sourceID); err != nil {
		return domain.Job{}, err
	}
	return s.jobs.Enqueue(ctx, jobs.EnqueueRequest{
		WorkspaceID: s.workspaceID, Definition: syncSourceJob,
		Arguments:      syncSourceArguments{SourceID: sourceID},
		IdempotencyKey: "sync-source:" + sourceID.String(),
	})
}

// SyncAll enqueues durable synchronization of enabled Workspace Sources.
func (s Service) SyncAll(ctx context.Context) (domain.Job, error) {
	return s.jobs.Enqueue(ctx, jobs.EnqueueRequest{
		WorkspaceID: s.workspaceID, Definition: syncAllSourcesJob,
		Arguments: struct{}{}, IdempotencyKey: "sync-all-sources",
	})
}

type syncSourceArguments struct {
	SourceID uuid.UUID `json:"sourceId"`
}

func (s Service) handleSyncSource(ctx context.Context, payload json.RawMessage) error {
	var arguments syncSourceArguments
	if err := json.Unmarshal(payload, &arguments); err != nil || arguments.SourceID == uuid.Nil {
		return jobs.Permanent(errors.New("invalid sync-source arguments"))
	}
	source, err := s.Get(ctx, arguments.SourceID)
	if errors.Is(err, ErrNotFound) {
		return jobs.Permanent(err)
	}
	if err != nil {
		return err
	}
	if _, err := s.ingester.Sync(ctx, source); err != nil {
		var retryAfter httpfetch.RetryAfterError
		if errors.As(err, &retryAfter) {
			return jobs.RetryAfter(err, retryAfter.Delay())
		}
		return err
	}
	return s.repository.SetLastSyncedAt(ctx, source.ID, s.now())
}

func (s Service) handleSyncAll(ctx context.Context, _ json.RawMessage) error {
	configured, err := s.List(ctx)
	if err != nil {
		return err
	}
	for _, source := range configured {
		if !source.Enabled {
			continue
		}
		if _, err := s.jobs.Enqueue(ctx, jobs.EnqueueRequest{
			WorkspaceID: s.workspaceID, Definition: syncSourceJob,
			Arguments:      syncSourceArguments{SourceID: source.ID},
			IdempotencyKey: "sync-source:" + source.ID.String(),
		}); err != nil {
			return fmt.Errorf("enqueue source %s: %w", source.ID, err)
		}
	}
	return nil
}

func validateConfig(config domain.SourceConfig) error {
	switch config.Kind {
	case domain.SourceKindRSS, domain.SourceKindAtom:
		return validateURL(config.URL)
	case domain.SourceKindArxiv:
		return validateArxivConfig(config)
	case domain.SourceKindGithub:
		return validateGitHubConfig(config)
	case domain.SourceKindYoutube:
		return validateYouTubeConfig(config)
	default:
		return domain.CheckChoice("config.kind", string(config.Kind), "rss", "atom", "arxiv", "github", "youtube")
	}
}

func validateArxivConfig(config domain.SourceConfig) error {
	query := strings.TrimSpace(config.Query)
	return domain.CheckLength("config.query", len(query), 1, 500)
}

func validateGitHubConfig(config domain.SourceConfig) error {
	return domain.JoinValidation(
		domain.CheckChoice("config.period", config.Period, "daily", "weekly", "monthly"),
		domain.CheckChoice("config.language", config.Language, "all", "python", "rust", "go", "javascript", "typescript"),
	)
}

func validateYouTubeConfig(config domain.SourceConfig) error {
	return domain.CheckCondition("config.channelId", domain.ValidationFormat, youtubeChannelID.MatchString(config.ChannelID))
}

func validateURL(raw string) error {
	parsedURL, err := url.ParseRequestURI(raw)
	if err != nil || parsedURL.Host == "" || parsedURL.User != nil || parsedURL.Scheme != "http" && parsedURL.Scheme != "https" {
		return domain.NewValidationError(domain.Violation{Field: "config.url", Rule: domain.ValidationHTTPURL})
	}
	return nil
}

func validateInterval(minutes int) error {
	return domain.CheckRange("pollIntervalMinutes", float64(minutes), 15, 10080)
}
