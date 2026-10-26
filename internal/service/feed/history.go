package feed

import (
	"context"

	"github.com/ethernity/obsidian-topic-stream/internal/domain"
	"github.com/google/uuid"
)

//go:generate go tool mockgen -source=history.go -destination=mocks/history.gen.go -package=mocks

// History reads saved Feed Publications without running retrieval or ranking.
type History interface {
	// FeedHead reads the committed Workspace head; missing history returns zero without creating state.
	FeedHead(ctx context.Context, workspaceID uuid.UUID) (int64, error)
	// ReadFeedPublications returns visible matching publications below a fixed head, ordered by descending ordinal.
	ReadFeedPublications(ctx context.Context, query domain.FeedPublicationQuery) ([]domain.FeedPublishedItem, error)
}
