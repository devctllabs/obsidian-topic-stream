package httpapi

import (
	"errors"

	"github.com/ethernity/obsidian-topic-stream/gen/serverhttp"
	"github.com/google/uuid"
)

// Server maps generated HTTP operations to application capabilities and problem responses.
type Server struct {
	Dependencies
}

var _ serverhttp.StrictServerInterface = (*Server)(nil)

// NewServer validates the Workspace capabilities required by generated HTTP operations.
func NewServer(dependencies Dependencies) (*Server, error) {
	if err := dependencies.validate(); err != nil {
		return nil, err
	}
	return &Server{Dependencies: dependencies}, nil
}

func (d Dependencies) validate() error {
	checks := []struct {
		missing bool
		message string
	}{
		{d.WorkspaceID == uuid.Nil, "workspace ID is required"},
		{d.Status == nil, "status service is required"},
		{d.Feed == nil, "feed service is required"},
		{d.Sources == nil, "sources service is required"},
		{d.Jobs == nil, "jobs service is required"},
		{d.Items == nil, "items service is required"},
		{d.Topics == nil, "topics service is required"},
		{d.Settings == nil, "settings service is required"},
		{d.EmbeddingModels == nil, "embedding models service is required"},
		{d.Search == nil, "search service is required"},
		{d.Digests == nil, "digests service is required"},
		{d.Summaries == nil, "summary service is required"},
		{d.Events == nil, "events service is required"},
		{d.Portability == nil, "portability service is required"},
	}
	for _, check := range checks {
		if check.missing {
			return errors.New(check.message)
		}
	}
	return nil
}

func valueOr[T any](value *T, fallback T) T {
	if value == nil {
		return fallback
	}
	return *value
}
