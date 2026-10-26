package domain

import (
	"time"

	"github.com/google/uuid"
)

const WorkspaceArchiveFormatV2 = "topic-stream/v2"

// PortableFeedback keys personal feedback by canonical URL across installations.
type PortableFeedback struct {
	CanonicalURL string   `json:"canonicalUrl"`
	Feedback     Feedback `json:"feedback"`
}

// WorkspaceArchiveData contains portable configuration and feedback for one Workspace.
type WorkspaceArchiveData struct {
	WorkspaceID    uuid.UUID                `json:"workspaceId"`
	EmbeddingModel EmbeddingModelDescriptor `json:"embeddingModel"`
	Settings       WorkspaceSettings        `json:"settings"`
	Profile        PreferenceProfile        `json:"preferenceProfile"`
	Sources        []Source                 `json:"sources"`
	Topics         []Topic                  `json:"topics"`
	Feedback       []PortableFeedback       `json:"feedback"`
	Digests        []DigestDefinition       `json:"digests"`
}

// WorkspaceArchive carries portable configuration and feedback, not ingested Items, secrets, or
// derived retrieval indexes.
type WorkspaceArchive struct {
	FormatVersion string               `json:"formatVersion"`
	ExportedAt    time.Time            `json:"exportedAt"`
	Data          WorkspaceArchiveData `json:"data"`
}
