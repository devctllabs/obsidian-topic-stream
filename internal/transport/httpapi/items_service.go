package httpapi

import (
	"context"

	"github.com/ethernity/obsidian-topic-stream/internal/domain"
	"github.com/ethernity/obsidian-topic-stream/internal/service/items"
	"github.com/google/uuid"
)

//go:generate go tool mockgen -source=items_service.go -destination=mocks/items.gen.go -package=mocks

// Items exposes the application operations consumed by its HTTP controller.
type Items interface {
	// Get returns the canonical Workspace Item or ErrNotFound.
	Get(ctx context.Context, itemID uuid.UUID) (domain.Item, error)
	// SetFeedback validates and commits current feedback, then delivers its projection; ProjectionPendingError
	// leaves the write committed.
	SetFeedback(ctx context.Context, itemID uuid.UUID, sentiment string, comment *string) (domain.Feedback, error)
	// ClearFeedback commits feedback removal, then delivers its projection; ProjectionPendingError leaves the
	// write committed.
	ClearFeedback(ctx context.Context, itemID uuid.UUID) error
	// SetReadingState validates read or unread and updates the Workspace Item, returning ErrNotFound when
	// absent.
	SetReadingState(ctx context.Context, itemID uuid.UUID, state domain.ReadingState) error
	// SetVisibility validates visible or hidden independently of feedback, returning ErrNotFound when absent.
	SetVisibility(ctx context.Context, itemID uuid.UUID, visibility domain.ItemVisibility) error
	// SetTopicCorrection commits membership, requests recalibration, and delivers the changed projection.
	SetTopicCorrection(ctx context.Context, itemID, topicID uuid.UUID, membership domain.TopicMembership) error
	// DownloadImage fetches only an image identified in canonical Markdown and validates type and size.
	DownloadImage(ctx context.Context, itemID, imageID uuid.UUID) (items.ImageDownload, error)
}
