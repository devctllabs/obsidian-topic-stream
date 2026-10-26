package topics

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/ethernity/obsidian-topic-stream/internal/domain"
	"github.com/google/uuid"
)

var ErrNotFound = errors.New("topic not found")

// Repository persists Topics, semantic revisions, and correction reset effects.
type Repository interface {
	// CreateTopic stores a Topic and its initial immutable semantic revision.
	CreateTopic(ctx context.Context, topic domain.Topic) error
	// ListTopics returns the Workspace's Topics.
	ListTopics(ctx context.Context, workspaceID uuid.UUID) ([]domain.Topic, error)
	// GetTopic returns the Workspace Topic and whether it exists.
	GetTopic(ctx context.Context, workspaceID uuid.UUID, topicID uuid.UUID) (domain.Topic, bool, error)
	// UpdateTopic atomically appends an immutable revision when semanticChanged is true.
	UpdateTopic(ctx context.Context, update domain.TopicUpdate) (domain.TopicUpdateResult, error)
	// DeleteTopic deletes the Workspace Topic and reports whether it existed.
	DeleteTopic(ctx context.Context, workspaceID uuid.UUID, topicID uuid.UUID) (bool, error)
}

// ProjectionQueue enqueues delivery of changed Topic membership projections.
type ProjectionQueue interface {
	// Enqueue submits durable Item projection work after the canonical commit.
	Enqueue(ctx context.Context, commit domain.ItemProjectionCommit) (domain.Job, error)
}

// CalibrationNotifier requests asynchronous Topic Gate Calibration after changes.
type CalibrationNotifier interface {
	// NotifyTopicChanged requests best-effort recalibration after a Topic or correction changes.
	NotifyTopicChanged(topicID uuid.UUID)
}

// CreateInput supplies a new Workspace Topic and its selection or polling policy.
type CreateInput struct {
	Name      string
	SourceIDs []uuid.UUID
	ItemKinds []domain.ItemKind
	Include   string
	Exclude   string
	Mode      domain.TopicMode
	Enabled   bool
}

// UpdateInput changes supplied Topic fields; nil preserves the corresponding value.
type UpdateInput struct {
	Name             *string
	SourceIDs        *[]uuid.UUID
	ItemKinds        *[]domain.ItemKind
	Include          *string
	Exclude          *string
	Mode             *domain.TopicMode
	Enabled          *bool
	ClearCorrections bool
}

// Service manages Workspace Topics, semantic revisions, and correction resets.
type Service struct {
	workspaceID uuid.UUID
	repository  Repository
	projections ProjectionQueue
	calibration CalibrationNotifier
	now         func() time.Time
}

// Config supplies dependencies and operating settings for the topics service.
type Config struct {
	WorkspaceID uuid.UUID
	Repository  Repository
	Projections ProjectionQueue
	Calibration CalibrationNotifier
	Now         func() time.Time
}

// New binds Workspace Topic persistence, projection submission, and calibration notifications.
func New(config Config) Service {
	return Service{
		workspaceID: config.WorkspaceID, repository: config.Repository, projections: config.Projections,
		calibration: config.Calibration, now: config.Now,
	}
}

// Create validates Topic scope and criteria, then persists its initial semantic revision.
func (s Service) Create(ctx context.Context, input CreateInput) (domain.Topic, error) {
	now := s.now()
	topic := domain.Topic{
		ID: uuid.New(), WorkspaceID: s.workspaceID, Name: strings.TrimSpace(input.Name),
		SourceIDs: normalizeUUIDs(input.SourceIDs), ItemKinds: normalizeKinds(input.ItemKinds),
		Include: strings.TrimSpace(input.Include), Exclude: strings.TrimSpace(input.Exclude),
		Mode: input.Mode, Enabled: input.Enabled, Revision: 1, CorrectionEpoch: 1,
		CreatedAt: now, UpdatedAt: now,
	}
	if err := validate(topic); err != nil {
		return domain.Topic{}, err
	}
	if err := s.repository.CreateTopic(ctx, topic); err != nil {
		return domain.Topic{}, fmt.Errorf("create topic: %w", err)
	}
	if topic.Enabled {
		s.notifyCalibration(topic.ID)
	}
	return topic, nil
}

// List returns Topics belonging to the service's Workspace.
func (s Service) List(ctx context.Context) ([]domain.Topic, error) {
	result, err := s.repository.ListTopics(ctx, s.workspaceID)
	if err != nil {
		return nil, fmt.Errorf("list topics: %w", err)
	}
	sort.Slice(result, func(left, right int) bool { return result[left].Name < result[right].Name })
	return result, nil
}

// Update increments the semantic revision only when scope, criteria, or mode changes; display and
// enabled-state edits do not invalidate historical ranking inputs.
func (s Service) Update(ctx context.Context, topicID uuid.UUID, input UpdateInput) (domain.Topic, error) {
	topic, err := s.get(ctx, topicID)
	if err != nil {
		return domain.Topic{}, err
	}
	before := topic
	apply(&topic, input)
	if err := validate(topic); err != nil {
		return domain.Topic{}, err
	}
	semanticChanged := !sameSemantics(before, topic)
	if semanticChanged {
		topic.Revision++
	}
	topic.UpdatedAt = s.now()
	result, err := s.repository.UpdateTopic(ctx, domain.TopicUpdate{
		Topic: topic, SemanticChanged: semanticChanged, ClearCorrections: input.ClearCorrections,
	})
	if err != nil {
		return domain.Topic{}, fmt.Errorf("update topic: %w", err)
	}
	if err := s.enqueueProjections(ctx, result.ProjectionCommits); err != nil {
		return domain.Topic{}, err
	}
	if topic.Enabled && (semanticChanged || input.ClearCorrections || before.Enabled != topic.Enabled) {
		s.notifyCalibration(topic.ID)
	}
	return topic, nil
}

func (s Service) get(ctx context.Context, topicID uuid.UUID) (domain.Topic, error) {
	topic, found, err := s.repository.GetTopic(ctx, s.workspaceID, topicID)
	if err != nil {
		return domain.Topic{}, fmt.Errorf("get topic: %w", err)
	}
	if !found {
		return domain.Topic{}, ErrNotFound
	}
	return topic, nil
}

func (s Service) enqueueProjections(ctx context.Context, commits []domain.ItemProjectionCommit) error {
	if len(commits) > 0 && s.projections == nil {
		return errors.New("topic correction projection queue is unavailable")
	}
	for _, commit := range commits {
		if _, err := s.projections.Enqueue(ctx, commit); err != nil {
			return fmt.Errorf("enqueue cleared topic correction projection: %w", err)
		}
	}
	return nil
}

func (s Service) notifyCalibration(topicID uuid.UUID) {
	if s.calibration != nil {
		s.calibration.NotifyTopicChanged(topicID)
	}
}

// Delete removes a Workspace Topic, returning ErrNotFound when absent.
func (s Service) Delete(ctx context.Context, topicID uuid.UUID) error {
	found, err := s.repository.DeleteTopic(ctx, s.workspaceID, topicID)
	if err != nil {
		return fmt.Errorf("delete topic: %w", err)
	}
	if !found {
		return ErrNotFound
	}
	return nil
}

func apply(topic *domain.Topic, input UpdateInput) {
	if input.Name != nil {
		topic.Name = strings.TrimSpace(*input.Name)
	}
	if input.SourceIDs != nil {
		topic.SourceIDs = normalizeUUIDs(*input.SourceIDs)
	}
	if input.ItemKinds != nil {
		topic.ItemKinds = normalizeKinds(*input.ItemKinds)
	}
	if input.Include != nil {
		topic.Include = strings.TrimSpace(*input.Include)
	}
	if input.Exclude != nil {
		topic.Exclude = strings.TrimSpace(*input.Exclude)
	}
	if input.Mode != nil {
		topic.Mode = *input.Mode
	}
	if input.Enabled != nil {
		topic.Enabled = *input.Enabled
	}
}

func validate(topic domain.Topic) error {
	checks := []error{
		domain.CheckLength("name", len(topic.Name), 1, 80),
		domain.CheckLength("include", len(topic.Include), 0, 4000),
		domain.CheckLength("exclude", len(topic.Exclude), 0, 4000),
		domain.CheckChoice("mode", string(topic.Mode), "ordinary", "strict"),
	}
	for index, kind := range topic.ItemKinds {
		checks = append(checks, domain.CheckChoice(fmt.Sprintf("itemKinds[%d]", index), string(kind), "article", "paper", "video", "repository"))
	}
	return domain.JoinValidation(checks...)
}

func sameSemantics(left, right domain.Topic) bool {
	return slices.Equal(left.SourceIDs, right.SourceIDs) && slices.Equal(left.ItemKinds, right.ItemKinds) &&
		left.Include == right.Include && left.Exclude == right.Exclude && left.Mode == right.Mode
}

func normalizeUUIDs(values []uuid.UUID) []uuid.UUID {
	result := make([]uuid.UUID, len(values))
	copy(result, values)
	slices.SortFunc(result, func(left, right uuid.UUID) int { return strings.Compare(left.String(), right.String()) })
	return slices.Compact(result)
}

func normalizeKinds(values []domain.ItemKind) []domain.ItemKind {
	result := make([]domain.ItemKind, len(values))
	copy(result, values)
	slices.Sort(result)
	return slices.Compact(result)
}
