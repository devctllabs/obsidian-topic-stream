package feed

import (
	"context"

	"github.com/ethernity/obsidian-topic-stream/internal/domain"
	"github.com/google/uuid"
)

//go:generate go tool mockgen -source=admission.go -destination=mocks/admission.gen.go -package=mocks

// Admission loads admission candidates and commits shared Feed Publications.
type Admission interface {
	// HasEnabledFeedTopics reports whether the Workspace has any enabled Topics.
	HasEnabledFeedTopics(ctx context.Context, workspaceID uuid.UUID) (bool, error)
	// ReadFeedCandidates returns prepared candidates whose inputs remain eligible for Feed admission.
	ReadFeedCandidates(ctx context.Context, query domain.FeedCandidateQuery) ([]domain.FeedCandidate, error)
	// RecentUnpublishedFeed returns recent unpublished Items eligible for fallback admission.
	RecentUnpublishedFeed(ctx context.Context, query domain.FeedCandidateQuery) ([]domain.Item, error)
	// GetActive returns the Workspace's serving retrieval generation and whether one exists.
	GetActive(ctx context.Context, workspaceID uuid.UUID) (domain.RetrievalIndex, bool, error)
	// PublishFeed commits eligible, previously unpublished Items under the shared Workspace head.
	PublishFeed(ctx context.Context, command domain.FeedPublishCommand) (domain.FeedPublishResult, error)
}

// WorkspaceSettings provides a current Workspace Settings snapshot for each operation.
type WorkspaceSettings interface {
	// GetWorkspace returns the current Workspace Settings snapshot.
	GetWorkspace(ctx context.Context) (domain.WorkspaceSettings, error)
}

// PersonalRanker personalizes a common pool of unpublished Feed Candidates.
type PersonalRanker interface {
	// RankFeed applies one personal ranking pass to the common Feed candidate pool.
	RankFeed(ctx context.Context, request domain.FeedRankingRequest) ([]domain.FeedCandidate, error)
}

// FeedInvalidator publishes advisory Feed invalidations after durable changes.
type FeedInvalidator interface {
	// InvalidateFeed announces a change affecting the Workspace's Feed views.
	InvalidateFeed(workspaceID uuid.UUID, itemID uuid.UUID)
}
