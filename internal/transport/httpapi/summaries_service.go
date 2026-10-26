package httpapi

import (
	"context"

	"github.com/ethernity/obsidian-topic-stream/internal/domain"
)

//go:generate go tool mockgen -source=summaries_service.go -destination=mocks/summaries.gen.go -package=mocks

// Summaries exposes explicit, asynchronous Item AI annotations without fetching additional content.
type Summaries interface {
	// Read returns supported-input, permission and annotation state without starting work.
	Read(ctx context.Context, source domain.SummarySource) (domain.ItemSummary, error)
	// Request binds an explicit request to accepted active work or a matching cached result.
	Request(ctx context.Context, source domain.SummarySource) (domain.Job, error)
}
