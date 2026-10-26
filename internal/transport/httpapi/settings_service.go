package httpapi

import (
	"context"

	"github.com/ethernity/obsidian-topic-stream/internal/domain"
)

//go:generate go tool mockgen -source=settings_service.go -destination=mocks/settings.gen.go -package=mocks

// Settings exposes the application operations consumed by its HTTP controller.
type Settings interface {
	// GetWorkspace returns the current Workspace Settings snapshot.
	GetWorkspace(ctx context.Context) (domain.WorkspaceSettings, error)
	// UpdateWorkspace commits settings before reconciling an embedding change. Startup reconciliation
	// repairs an interruption between that commit and durable reindex enqueue.
	UpdateWorkspace(ctx context.Context, expectedVersion int, patch domain.WorkspaceSettingsPatch) (domain.WorkspaceSettings, error)
	// GetPreferenceProfile returns the current Preference Profile.
	GetPreferenceProfile(ctx context.Context) (domain.PreferenceProfile, error)
	// UpdatePreferenceProfile saves changed guidance as a new revision using expectedVersion; unchanged text
	// preserves the current revision.
	UpdatePreferenceProfile(ctx context.Context, expectedVersion int, patch domain.PreferenceProfilePatch) (domain.PreferenceProfile, error)
}
