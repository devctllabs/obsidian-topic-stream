package items

//go:generate go tool mockgen -source=service.go -destination=mocks/service.gen.go -package=mocks

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/ethernity/obsidian-topic-stream/internal/domain"
	"github.com/google/uuid"
)

var (
	ErrNotFound      = errors.New("item not found")
	ErrImageNotFound = errors.New("item image not found")
	ErrInvalidImage  = errors.New("invalid item image")
)

// Repository reads canonical Items and commits user state with pending projections.
type Repository interface {
	// GetItem returns the Workspace Item and whether it exists.
	GetItem(ctx context.Context, workspaceID uuid.UUID, itemID uuid.UUID) (domain.Item, bool, error)
	// SetFeedback stores current personal feedback and returns its projection commit and Item existence.
	SetFeedback(ctx context.Context, workspaceID uuid.UUID, itemID uuid.UUID, feedback domain.Feedback) (domain.FeedbackCommit, bool, error)
	// ClearFeedback removes current personal feedback and returns the projection commit and Item existence.
	ClearFeedback(ctx context.Context, workspaceID uuid.UUID, itemID uuid.UUID, at time.Time) (domain.ItemProjectionCommit, bool, error)
	// SetReadingState updates the Item's reading state and reports whether it exists.
	SetReadingState(ctx context.Context, workspaceID uuid.UUID, itemID uuid.UUID, state domain.ReadingState) (bool, error)
	// SetVisibility updates the Item's visibility and reports whether it exists.
	SetVisibility(ctx context.Context, workspaceID uuid.UUID, itemID uuid.UUID, visibility domain.ItemVisibility) (bool, error)
	// SetTopicCorrection stores explicit Topic membership and returns the projection commit and Item
	// existence.
	SetTopicCorrection(ctx context.Context, correction domain.TopicCorrection) (domain.ItemProjectionCommit, bool, error)
}

// Projector delivers canonical Item state to derived retrieval indexes.
type Projector interface {
	// DeliverItemProjection delivers the committed Item revision to its pending model indexes.
	DeliverItemProjection(ctx context.Context, commit domain.ItemProjectionCommit) error
}

// CalibrationNotifier requests asynchronous Topic Gate Calibration after changes.
type CalibrationNotifier interface {
	// NotifyTopicChanged requests best-effort recalibration after a Topic or correction changes.
	NotifyTopicChanged(topicID uuid.UUID)
}

// Service manages canonical Item reads, personal feedback, visibility, and membership corrections.
type Service struct {
	workspaceID uuid.UUID
	repository  Repository
	projector   Projector
	calibration CalibrationNotifier
	imageClient ImageClient
	now         func() time.Time
}

// ImageClient fetches Item images using the configured outbound HTTP policy.
type ImageClient interface {
	// Do executes the HTTP request; the caller must close a successful response body.
	Do(request *http.Request) (*http.Response, error)
}

// Config supplies dependencies and operating settings for the items service.
type Config struct {
	WorkspaceID uuid.UUID
	Repository  Repository
	Projector   Projector
	Calibration CalibrationNotifier
	ImageClient ImageClient
	Now         func() time.Time
}

// New validates and binds Item persistence, projection delivery, calibration, image fetching, and the clock.
func New(config Config) (*Service, error) {
	if config.WorkspaceID == uuid.Nil || config.Repository == nil || config.Projector == nil ||
		config.Calibration == nil || config.ImageClient == nil || config.Now == nil {
		return nil, errors.New("items service dependencies are required")
	}
	return &Service{
		workspaceID: config.WorkspaceID, repository: config.Repository,
		projector: config.Projector, calibration: config.Calibration,
		imageClient: config.ImageClient, now: config.Now,
	}, nil
}

// Get returns the canonical Workspace Item or ErrNotFound.
func (s Service) Get(ctx context.Context, itemID uuid.UUID) (domain.Item, error) {
	item, found, err := s.repository.GetItem(ctx, s.workspaceID, itemID)
	if err != nil {
		return domain.Item{}, fmt.Errorf("get item: %w", err)
	}
	if !found {
		return domain.Item{}, ErrNotFound
	}
	return item, nil
}

// SetFeedback validates and commits current feedback, then delivers its projection; ProjectionPendingError
// leaves the write committed.
func (s Service) SetFeedback(ctx context.Context, itemID uuid.UUID, sentiment string, comment *string) (domain.Feedback, error) {
	checks := []error{domain.CheckChoice("sentiment", sentiment, "like", "dislike")}
	if comment != nil {
		trimmed := strings.TrimSpace(*comment)
		checks = append(checks, domain.CheckLength("comment", len([]rune(trimmed)), 0, 2000))
		comment = &trimmed
	}
	if err := domain.JoinValidation(checks...); err != nil {
		return domain.Feedback{}, err
	}
	now := s.now()
	commit, found, err := s.repository.SetFeedback(ctx, s.workspaceID, itemID, domain.Feedback{
		Sentiment: sentiment, Comment: comment, CreatedAt: now, UpdatedAt: now,
	})
	if err != nil {
		return domain.Feedback{}, fmt.Errorf("set feedback: %w", err)
	}
	if !found {
		return domain.Feedback{}, ErrNotFound
	}
	if err := s.deliverProjection(ctx, commit.ItemProjectionCommit); err != nil {
		return commit.Feedback, err
	}
	return commit.Feedback, nil
}

// ClearFeedback commits feedback removal, then delivers its projection; ProjectionPendingError leaves the
// write committed.
func (s Service) ClearFeedback(ctx context.Context, itemID uuid.UUID) error {
	commit, found, err := s.repository.ClearFeedback(ctx, s.workspaceID, itemID, s.now())
	if err := stateResult("clear feedback", found, err); err != nil {
		return err
	}
	return s.deliverProjection(ctx, commit)
}

// SetReadingState validates read or unread and updates the Workspace Item, returning ErrNotFound when
// absent.
func (s Service) SetReadingState(ctx context.Context, itemID uuid.UUID, state domain.ReadingState) error {
	if state != domain.ReadingStateRead && state != domain.ReadingStateUnread {
		return domain.CheckChoice("state", string(state), "read", "unread")
	}
	found, err := s.repository.SetReadingState(ctx, s.workspaceID, itemID, state)
	return stateResult("set reading state", found, err)
}

// SetVisibility validates visible or hidden independently of feedback, returning ErrNotFound when absent.
func (s Service) SetVisibility(ctx context.Context, itemID uuid.UUID, visibility domain.ItemVisibility) error {
	if visibility != domain.ItemVisibilityVisible && visibility != domain.ItemVisibilityHidden {
		return domain.CheckChoice("visibility", string(visibility), "visible", "hidden")
	}
	found, err := s.repository.SetVisibility(ctx, s.workspaceID, itemID, visibility)
	return stateResult("set visibility", found, err)
}

// SetTopicCorrection commits membership, requests recalibration, and delivers the changed projection.
func (s Service) SetTopicCorrection(ctx context.Context, itemID, topicID uuid.UUID, membership domain.TopicMembership) error {
	if err := domain.JoinValidation(domain.CheckRequired("topicId", topicID != uuid.Nil),
		domain.CheckChoice("membership", string(membership), "automatic", "include", "exclude")); err != nil {
		return err
	}
	commit, found, err := s.repository.SetTopicCorrection(ctx, domain.TopicCorrection{
		WorkspaceID: s.workspaceID, ItemID: itemID, TopicID: topicID, Membership: membership, UpdatedAt: s.now(),
	})
	if err := stateResult("set topic correction", found, err); err != nil {
		return err
	}
	s.calibration.NotifyTopicChanged(topicID)
	return s.deliverProjection(ctx, commit)
}

func (s Service) deliverProjection(ctx context.Context, commit domain.ItemProjectionCommit) error {
	if err := s.projector.DeliverItemProjection(ctx, commit); err != nil {
		return &domain.ProjectionPendingError{
			Revision: commit.ProjectionRevision, Cause: fmt.Errorf("deliver item projection: %w", err),
		}
	}
	return nil
}

func stateResult(action string, found bool, err error) error {
	if err != nil {
		return fmt.Errorf("%s: %w", action, err)
	}
	if !found {
		return ErrNotFound
	}
	return nil
}
