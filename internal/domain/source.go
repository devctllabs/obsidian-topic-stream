package domain

import (
	"time"

	"github.com/google/uuid"
)

// SourceKind selects the adapter used to poll an upstream stream.
type SourceKind string

const (
	SourceKindRSS     SourceKind = "rss"
	SourceKindAtom    SourceKind = "atom"
	SourceKindArxiv   SourceKind = "arxiv"
	SourceKindGithub  SourceKind = "github"
	SourceKindYoutube SourceKind = "youtube"
)

// SourceConfig contains the upstream location and kind-specific polling options.
type SourceConfig struct {
	Kind SourceKind `json:"kind"`
	URL  string     `json:"url,omitempty"`
	// AllowPrivateNetwork permits private and special-use destinations otherwise blocked by HTTP fetching.
	AllowPrivateNetwork bool   `json:"allowPrivateNetwork,omitempty"`
	Query               string `json:"query,omitempty"`
	Period              string `json:"period,omitempty"`
	Language            string `json:"language,omitempty"`
	ChannelID           string `json:"channelId,omitempty"`
}

// Source is a Workspace-owned upstream stream with a polling policy.
type Source struct {
	ID                  uuid.UUID
	WorkspaceID         uuid.UUID
	Name                string
	Config              SourceConfig
	Enabled             bool
	PollIntervalMinutes int
	LastSyncedAt        *time.Time
	CreatedAt           time.Time
	UpdatedAt           time.Time
}
