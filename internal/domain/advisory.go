package domain

import "github.com/google/uuid"

// AdvisoryKind identifies a best-effort invalidation or product notification.
type AdvisoryKind string

const (
	AdvisorySettingsChanged AdvisoryKind = "settings.changed"
	AdvisoryFeedInvalidated AdvisoryKind = "feedInvalidated"
	AdvisoryDigestReady     AdvisoryKind = "digestReady"
	AdvisoryJobChanged      AdvisoryKind = "jobChanged"
)

// Advisory is the small, non-authoritative envelope exchanged between processes. ResourceID is
// used by product invalidations; Version is used only by settings.changed.
type Advisory struct {
	Kind        AdvisoryKind `json:"kind"`
	WorkspaceID uuid.UUID    `json:"workspaceId"`
	ResourceID  uuid.UUID    `json:"resourceId,omitempty"`
	Version     int          `json:"version,omitempty"`
	// Origin suppresses delivery echoes from the publishing process.
	Origin uuid.UUID `json:"origin,omitempty"`
}
