package digests

//go:generate go tool mockgen -source=service.go -destination=mocks/service.gen.go -package=mocks

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/ethernity/obsidian-topic-stream/internal/domain"
	"github.com/ethernity/obsidian-topic-stream/internal/service/jobs"
	"github.com/ethernity/obsidian-topic-stream/internal/service/ranking"
	"github.com/google/uuid"
)

const GenerationJobKind = "generate-digest"
const generationArgumentsVersion = 1

const schedulePollInterval = time.Minute

var generateDigestJob = jobs.JobDefinition{
	Kind: GenerationJobKind, ArgumentsVersion: generationArgumentsVersion, Queue: domain.JobQueueGeneration,
	Retry: jobs.RetryPolicy{MaxAttempts: 3, BaseDelay: 30 * time.Second, MaxDelay: 15 * time.Minute},
}

var (
	ErrNotFound = domain.ErrDigestNotFound
	ErrConflict = errors.New("digest version conflict")
	ErrInvalid  = errors.New("invalid digest")
)

// Repository persists Digest Definitions, immutable issues, and schedule advancement.
type Repository interface {
	// GetDigestIssueForJob recovers a committed generation result independently of live Definition state.
	GetDigestIssueForJob(ctx context.Context, workspaceID, jobID uuid.UUID) (domain.DigestIssue, bool, error)
	// AdmitScheduledDigestBuild rechecks the observed Definition before coalescing and advancing.
	AdmitScheduledDigestBuild(ctx context.Context, request domain.DigestScheduledBuild) (domain.DigestBuildAdmission, error)
	// AdmitDigestBuild atomically resolves the identity, coalesces active work, and binds it.
	AdmitDigestBuild(ctx context.Context, request domain.DigestBuildRequest) (domain.DigestBuildAdmission, error)
	// CreateDigest stores a Digest Definition and its initial version.
	CreateDigest(ctx context.Context, definition domain.DigestDefinition) error
	// ListDigests returns the Workspace's Digest Definitions.
	ListDigests(ctx context.Context, workspaceID uuid.UUID) ([]domain.DigestDefinition, error)
	// GetDigest returns a Digest Definition and whether it exists in the Workspace.
	GetDigest(ctx context.Context, workspaceID uuid.UUID, digestID uuid.UUID) (domain.DigestDefinition, bool, error)
	// UpdateDigest returns false when the expected version no longer matches.
	UpdateDigest(ctx context.Context, definition domain.DigestDefinition, expectedVersion int) (bool, error)
	// DeleteDigest returns false when the expected version no longer matches.
	DeleteDigest(ctx context.Context, command domain.DigestDelete) (domain.DigestDeletion, error)
	// PublishDigestIssue fences execution and commits the snapshot, consumption and watermark together.
	PublishDigestIssue(ctx context.Context, publication domain.DigestPublication) (domain.DigestPublicationResult, error)
	// ListDigestIssues returns saved issues, optionally restricted to one Digest Definition.
	ListDigestIssues(ctx context.Context, query domain.DigestIssueList) ([]domain.DigestIssueSummary, error)
	// GetDigestIssue returns a saved Digest Issue and whether it exists in the Workspace.
	GetDigestIssue(ctx context.Context, workspaceID uuid.UUID, issueID uuid.UUID) (domain.DigestIssue, bool, error)
	// FindConsumedDigestItemIDs returns only supplied candidates already used by this Definition.
	FindConsumedDigestItemIDs(ctx context.Context, workspaceID uuid.UUID, digestID uuid.UUID, itemIDs []uuid.UUID) ([]uuid.UUID, error)
	// ListDueDigests returns enabled Digest Definitions whose next run is due at the supplied time.
	ListDueDigests(ctx context.Context, workspaceID uuid.UUID, at time.Time) ([]domain.DigestDefinition, error)
}

// Topics reads Topics within a Workspace for selection and validation.
type Topics interface {
	// GetTopic returns the Workspace Topic and whether it exists.
	GetTopic(ctx context.Context, workspaceID uuid.UUID, topicID uuid.UUID) (domain.Topic, bool, error)
}

// TopicRanker ranks one Topic for a requested retrieval purpose.
type TopicRanker interface {
	// RankTopic retrieves and ranks Items for the Topic and requested purpose.
	RankTopic(ctx context.Context, request ranking.TopicRequest) (ranking.TopicResult, error)
}

// WorkspaceSettings provides a current Workspace Settings snapshot for each operation.
type WorkspaceSettings interface {
	// GetWorkspace returns the current Workspace Settings snapshot.
	GetWorkspace(ctx context.Context) (domain.WorkspaceSettings, error)
}

// ItemSummaries reads existing annotations for saved Items without requesting generation.
type ItemSummaries interface {
	// ReadDigest returns annotation states using the exact saved input versions.
	ReadDigest(ctx context.Context, issue domain.DigestIssue) (map[uuid.UUID]domain.ItemSummary, error)
}

// Items reads canonical Items for assembling immutable Digest Issues.
type Items interface {
	// GetItem returns the Workspace Item and whether it exists.
	GetItem(ctx context.Context, workspaceID uuid.UUID, itemID uuid.UUID) (domain.Item, bool, error)
}

// DigestNotifier announces newly saved Digest Issues.
type DigestNotifier interface {
	// NotifyJobChanged announces newly committed generation work.
	NotifyJobChanged(workspaceID uuid.UUID, jobID uuid.UUID)
	// NotifyDigestReady announces a saved Digest Issue to the Workspace's subscribers.
	NotifyDigestReady(workspaceID uuid.UUID, issueID uuid.UUID)
}

// Config supplies dependencies and operating settings for the digests service.
type Config struct {
	WorkspaceID uuid.UUID
	Repository  Repository
	Topics      Topics
	Ranker      TopicRanker
	Settings    WorkspaceSettings
	Summaries   ItemSummaries
	Items       Items
	Jobs        jobs.Queue
	Events      DigestNotifier
	Now         func() time.Time
}

// Service manages Digest Definitions and durable generation of immutable issues.
type Service struct {
	workspaceID uuid.UUID
	repository  Repository
	topics      Topics
	ranker      TopicRanker
	settings    WorkspaceSettings
	summaries   ItemSummaries
	items       Items
	jobs        jobs.Queue
	events      DigestNotifier
	now         func() time.Time
}

// Input supplies a new Digest Definition's ordered sections and local calendar schedule.
type Input struct {
	Name       string
	Topics     []domain.DigestTopic
	Timezone   string
	Recurrence domain.DigestRecurrence
	Enabled    bool
}

// Patch updates supplied Digest Definition fields; nil preserves the current value.
type Patch struct {
	Name       *string
	Topics     *[]domain.DigestTopic
	Timezone   *string
	Recurrence *domain.DigestRecurrence
	Enabled    *bool
}

type generationArguments struct {
	DigestID     uuid.UUID  `json:"digestId"`
	ScheduledFor *time.Time `json:"scheduledFor"`
}

// New validates dependencies and registers the durable Digest generation handler.
func New(config Config) (*Service, error) {
	if config.WorkspaceID == uuid.Nil || !validContentDependencies(config) ||
		config.Jobs == nil || config.Events == nil || config.Now == nil {
		return nil, errors.New("digest service dependencies are required")
	}
	service := &Service{
		workspaceID: config.WorkspaceID, repository: config.Repository, topics: config.Topics,
		ranker: config.Ranker, items: config.Items, jobs: config.Jobs, events: config.Events, now: config.Now,
		settings: config.Settings, summaries: config.Summaries,
	}
	definition := generateDigestJob
	definition.RetryHandler = service.retryGeneration
	config.Jobs.RegisterDefinition(definition, service.handleGeneration)
	return service, nil
}

func validContentDependencies(config Config) bool {
	return config.Repository != nil && config.Topics != nil && config.Ranker != nil &&
		config.Items != nil && config.Settings != nil && config.Summaries != nil
}

// List returns the Workspace's Digest Definitions.
func (s *Service) List(ctx context.Context) ([]domain.DigestDefinition, error) {
	result, err := s.repository.ListDigests(ctx, s.workspaceID)
	if err != nil {
		return nil, fmt.Errorf("list digests: %w", err)
	}
	return result, nil
}

// Create validates sections and schedule, then stores the initial version and next run.
func (s *Service) Create(ctx context.Context, input Input) (domain.DigestDefinition, error) {
	normalized, nextRun, err := s.validateInput(ctx, input)
	if err != nil {
		return domain.DigestDefinition{}, err
	}
	now := s.now()
	definition := domain.DigestDefinition{
		ID: uuid.New(), WorkspaceID: s.workspaceID, Name: normalized.Name, Topics: normalized.Topics,
		Timezone: normalized.Timezone, Recurrence: normalized.Recurrence, Enabled: normalized.Enabled,
		Version: 1, NextRunAt: nextRun, CreatedAt: now, UpdatedAt: now,
	}
	if err := s.repository.CreateDigest(ctx, definition); err != nil {
		return domain.DigestDefinition{}, fmt.Errorf("create digest: %w", err)
	}
	return definition, nil
}

// Update applies supplied fields using expectedVersion; stale edits return ErrConflict.
func (s *Service) Update(
	ctx context.Context,
	digestID uuid.UUID,
	expectedVersion int,
	patch Patch,
) (domain.DigestDefinition, error) {
	current, err := s.get(ctx, digestID)
	if err != nil {
		return domain.DigestDefinition{}, err
	}
	if current.Version != expectedVersion {
		return domain.DigestDefinition{}, ErrConflict
	}
	input := applyPatch(current, patch)
	normalized, nextRun, err := s.validateInput(ctx, input)
	if err != nil {
		return domain.DigestDefinition{}, err
	}
	current.Name, current.Topics = normalized.Name, normalized.Topics
	current.Timezone, current.Recurrence, current.Enabled = normalized.Timezone, normalized.Recurrence, normalized.Enabled
	if patch.Timezone == nil && patch.Recurrence == nil && patch.Enabled == nil {
		nextRun = current.NextRunAt
	}
	current.NextRunAt, current.UpdatedAt, current.Version = nextRun, s.now(), current.Version+1
	updated, err := s.repository.UpdateDigest(ctx, current, expectedVersion)
	if err != nil {
		return domain.DigestDefinition{}, fmt.Errorf("update digest: %w", err)
	}
	if !updated {
		return domain.DigestDefinition{}, ErrConflict
	}
	return current, nil
}

// Delete removes a definition using expectedVersion; stale edits return ErrConflict.
func (s *Service) Delete(ctx context.Context, digestID uuid.UUID, expectedVersion int) error {
	current, err := s.get(ctx, digestID)
	if err != nil {
		return err
	}
	if current.Version != expectedVersion {
		return ErrConflict
	}
	found, err := s.repository.DeleteDigest(ctx, domain.DigestDelete{
		WorkspaceID: s.workspaceID, DigestID: digestID, ExpectedVersion: expectedVersion, DeletedAt: s.now(),
	})
	if err != nil {
		return fmt.Errorf("delete digest: %w", err)
	}
	if !found.Deleted {
		return ErrConflict
	}
	for _, jobID := range found.JobIDs {
		s.events.NotifyJobChanged(s.workspaceID, jobID)
	}
	return nil
}

// Generate enqueues on-demand generation and reuses equivalent active work for the definition.
func (s *Service) Generate(ctx context.Context, digestID, requestID uuid.UUID) (domain.Job, error) {
	if requestID == uuid.Nil {
		return domain.Job{}, ErrInvalid
	}
	job, err := s.prepareGeneration(generationArguments{DigestID: digestID}, s.now())
	if err != nil {
		return domain.Job{}, err
	}
	admission, err := s.repository.AdmitDigestBuild(ctx, domain.DigestBuildRequest{
		WorkspaceID: s.workspaceID, DigestID: digestID, RequestID: requestID, Kind: domain.DigestBuildManual, Job: job,
	})
	if err != nil {
		return domain.Job{}, fmt.Errorf("admit manual digest build: %w", err)
	}
	if admission.Created {
		s.events.NotifyJobChanged(admission.Job.WorkspaceID, admission.Job.ID)
	}
	return admission.Job, nil
}

// Run polls due Digest schedules until ctx is cancelled.
func (s *Service) Run(ctx context.Context) error {
	if err := s.PollSchedules(ctx); err != nil {
		return err
	}
	ticker := time.NewTicker(schedulePollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			if err := s.PollSchedules(ctx); err != nil {
				return err
			}
		}
	}
}

// PollSchedules enqueues due generations and advances schedules using an expected-instant comparison.
func (s *Service) PollSchedules(ctx context.Context) error {
	now := s.now()
	due, err := s.repository.ListDueDigests(ctx, s.workspaceID, now)
	if err != nil {
		return fmt.Errorf("list due digests: %w", err)
	}
	for _, definition := range due {
		if err := s.schedule(ctx, definition, now); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) schedule(ctx context.Context, definition domain.DigestDefinition, now time.Time) error {
	if definition.NextRunAt == nil {
		return nil
	}
	scheduledFor := *definition.NextRunAt
	nextRun, err := NextRun(definition.Recurrence, definition.Timezone, now)
	if err != nil {
		return fmt.Errorf("compute next digest run: %w", err)
	}
	job, err := s.prepareGeneration(generationArguments{DigestID: definition.ID, ScheduledFor: &scheduledFor}, now)
	if err != nil {
		return err
	}
	admission, err := s.repository.AdmitScheduledDigestBuild(ctx, domain.DigestScheduledBuild{
		Advance:         domain.DigestScheduleAdvance{WorkspaceID: s.workspaceID, DigestID: definition.ID, Expected: scheduledFor, NextRunAt: nextRun, UpdatedAt: now},
		ExpectedVersion: definition.Version, Job: job,
	})
	if err != nil {
		return fmt.Errorf("admit scheduled digest: %w", err)
	}
	if admission.Created {
		s.events.NotifyJobChanged(admission.Job.WorkspaceID, admission.Job.ID)
	}
	return nil
}

// GetIssue returns a saved Workspace issue or ErrNotFound.
func (s *Service) GetIssue(ctx context.Context, issueID uuid.UUID) (domain.DigestIssue, error) {
	issue, found, err := s.repository.GetDigestIssue(ctx, s.workspaceID, issueID)
	if err != nil {
		return domain.DigestIssue{}, fmt.Errorf("get digest issue: %w", err)
	}
	if !found {
		return domain.DigestIssue{}, ErrNotFound
	}
	annotations, err := s.summaries.ReadDigest(ctx, issue)
	if err != nil {
		return domain.DigestIssue{}, fmt.Errorf("read saved Item annotations: %w", err)
	}
	for section := range issue.Sections {
		for index := range issue.Sections[section].Items {
			item := &issue.Sections[section].Items[index]
			if annotation, found := annotations[item.Item.ID]; found {
				item.AISummary = &annotation
			}
		}
	}
	return issue, nil
}

func (s *Service) get(ctx context.Context, digestID uuid.UUID) (domain.DigestDefinition, error) {
	definition, found, err := s.repository.GetDigest(ctx, s.workspaceID, digestID)
	if err != nil {
		return domain.DigestDefinition{}, fmt.Errorf("get digest: %w", err)
	}
	if !found {
		return domain.DigestDefinition{}, ErrNotFound
	}
	return definition, nil
}

func (s *Service) validateInput(ctx context.Context, input Input) (Input, *time.Time, error) {
	input.Name = strings.TrimSpace(input.Name)
	input.Timezone = strings.TrimSpace(input.Timezone)
	next, scheduleErr := NextRun(input.Recurrence, input.Timezone, s.now())
	if err := domain.JoinValidation(domain.CheckLength("name", len([]rune(input.Name)), 1, 120),
		domain.CheckRequired("topics", len(input.Topics) > 0), validateTopicSelection(input.Topics), scheduleErr); err != nil {
		return Input{}, nil, fmt.Errorf("%w: %w", ErrInvalid, err)
	}
	if err := s.validateTopics(ctx, input.Topics); err != nil {
		return Input{}, nil, err
	}
	if !input.Enabled {
		return input, nil, nil
	}
	return input, &next, nil
}

func validateTopicSelection(configured []domain.DigestTopic) error {
	seen := make(map[uuid.UUID]struct{}, len(configured))
	var checks []error
	for index, relationship := range configured {
		prefix := fmt.Sprintf("topics[%d]", index)
		checks = append(checks, domain.CheckRequired(prefix+".topicId", relationship.TopicID != uuid.Nil),
			domain.CheckRange(prefix+".itemLimit", float64(relationship.ItemLimit), 1, 15))
		if _, duplicate := seen[relationship.TopicID]; duplicate {
			checks = append(checks, domain.CheckCondition(prefix+".topicId", domain.ValidationDuplicate, false))
		}
		seen[relationship.TopicID] = struct{}{}
	}
	return domain.JoinValidation(checks...)
}

func (s *Service) validateTopics(ctx context.Context, configured []domain.DigestTopic) error {
	var checks []error
	for index, relationship := range configured {
		topic, found, err := s.topics.GetTopic(ctx, s.workspaceID, relationship.TopicID)
		if err != nil {
			return fmt.Errorf("validate digest topic: %w", err)
		}
		if !found || !topic.Enabled {
			checks = append(checks, domain.CheckCondition(fmt.Sprintf("topics[%d].topicId", index), domain.ValidationRelationship, false))
		}
	}
	if err := domain.JoinValidation(checks...); err != nil {
		return fmt.Errorf("%w: %w", ErrInvalid, err)
	}
	return nil
}

func applyPatch(current domain.DigestDefinition, patch Patch) Input {
	input := Input{
		Name: current.Name, Topics: append([]domain.DigestTopic(nil), current.Topics...),
		Timezone: current.Timezone, Recurrence: current.Recurrence, Enabled: current.Enabled,
	}
	if patch.Name != nil {
		input.Name = *patch.Name
	}
	if patch.Topics != nil {
		input.Topics = append([]domain.DigestTopic(nil), (*patch.Topics)...)
	}
	if patch.Timezone != nil {
		input.Timezone = *patch.Timezone
	}
	if patch.Recurrence != nil {
		input.Recurrence = *patch.Recurrence
	}
	if patch.Enabled != nil {
		input.Enabled = *patch.Enabled
	}
	return input
}

func (s *Service) prepareGeneration(arguments generationArguments, at time.Time) (domain.Job, error) {
	return jobs.Prepare(jobs.EnqueueRequest{WorkspaceID: s.workspaceID, Definition: generateDigestJob,
		Arguments: arguments, IdempotencyKey: "generate-digest:" + arguments.DigestID.String(),
	}, at)
}
