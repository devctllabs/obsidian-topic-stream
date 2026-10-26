package httpapi_test

import (
	"testing"

	"github.com/ethernity/obsidian-topic-stream/internal/transport/httpapi"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestNewServerRejectsIncompleteDependencies(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		missing string
		remove  func(*httpapi.Dependencies)
	}{
		{"workspace", "workspace ID", func(d *httpapi.Dependencies) { d.WorkspaceID = uuid.Nil }},
		{"status", "status service", func(d *httpapi.Dependencies) { d.Status = nil }},
		{"feed", "feed service", func(d *httpapi.Dependencies) { d.Feed = nil }},
		{"sources", "sources service", func(d *httpapi.Dependencies) { d.Sources = nil }},
		{"jobs", "jobs service", func(d *httpapi.Dependencies) { d.Jobs = nil }},
		{"items", "items service", func(d *httpapi.Dependencies) { d.Items = nil }},
		{"topics", "topics service", func(d *httpapi.Dependencies) { d.Topics = nil }},
		{"settings", "settings service", func(d *httpapi.Dependencies) { d.Settings = nil }},
		{"embedding models", "embedding models service", func(d *httpapi.Dependencies) { d.EmbeddingModels = nil }},
		{"search", "search service", func(d *httpapi.Dependencies) { d.Search = nil }},
		{"digests", "digests service", func(d *httpapi.Dependencies) { d.Digests = nil }},
		{"events", "events service", func(d *httpapi.Dependencies) { d.Events = nil }},
		{"portability", "portability service", func(d *httpapi.Dependencies) { d.Portability = nil }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			dependencies := completeTestDependencies(t)
			test.remove(&dependencies)

			_, err := httpapi.NewServer(dependencies)

			require.ErrorContains(t, err, test.missing)
		})
	}
}
