package httpapi

import (
	"context"

	"github.com/ethernity/obsidian-topic-stream/internal/service/search"
)

//go:generate go tool mockgen -source=search_service.go -destination=mocks/search.gen.go -package=mocks

// Search exposes the application operations consumed by its HTTP controller.
type Search interface {
	// Find validates the request and returns bounded, hydrated results with optional Topic membership and
	// current personal scoring.
	Find(ctx context.Context, request search.Request) (search.Page, error)
}
