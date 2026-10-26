package domain

import (
	"strings"
	"time"

	"github.com/google/uuid"
)

// ReadingState records whether the user has deliberately opened or marked an Item read.
type ReadingState string

const (
	ReadingStateUnread ReadingState = "unread"
	ReadingStateRead   ReadingState = "read"
)

// ItemVisibility controls display independently of personal preference.
type ItemVisibility string

const (
	ItemVisibilityVisible ItemVisibility = "visible"
	ItemVisibilityHidden  ItemVisibility = "hidden"
)

// Feedback is the current Like or Dislike judgment with an optional comment.
type Feedback struct {
	Sentiment string
	Comment   *string
	CreatedAt time.Time
	UpdatedAt time.Time
}

// ItemFeedback associates current personal feedback with an Item.
type ItemFeedback struct {
	ItemID   uuid.UUID
	Feedback Feedback
}

// Item is canonical content shared by all upstream Source Entries that resolve to it.
type Item struct {
	ContentOrigin ContentOrigin
	ID            uuid.UUID
	WorkspaceID   uuid.UUID
	// SourceIDs collects provenance from all Sources that observed this canonical Item.
	SourceIDs         []uuid.UUID
	Kind              ItemKind
	Details           ItemDetails
	Title             string
	URL               string
	CanonicalURL      string
	Excerpt           string
	ContentMarkdown   string
	ExtractionVersion string
	Authors           []string
	// PublishedAt is nil when the upstream publication time is unknown; discovery time remains available.
	PublishedAt  *time.Time
	DiscoveredAt time.Time
	// TopicIDs carries membership populated for the current view; it is not a separate publication history.
	TopicIDs     []uuid.UUID
	ReadingState ReadingState
	Visibility   ItemVisibility
	Feedback     *Feedback
}

// ItemDetails is a discriminated union: Type must match exactly one non-nil variant.
type ItemDetails struct {
	Type       ItemKind           `json:"type"`
	Article    *ArticleDetails    `json:"article,omitempty"`
	Paper      *PaperDetails      `json:"paper,omitempty"`
	Video      *VideoDetails      `json:"video,omitempty"`
	Repository *RepositoryDetails `json:"repository,omitempty"`
}

// ArticleDetails holds extracted article content and its source description.
type ArticleDetails struct {
	Description string `json:"description"`
	Content     string `json:"content"`
}

// PaperDetails holds the abstract and bibliographic metadata of a paper.
type PaperDetails struct {
	Abstract   string   `json:"abstract"`
	Categories []string `json:"categories"`
	DOI        *string  `json:"doi"`
}

// VideoDetails holds video provenance and descriptive content.
type VideoDetails struct {
	Description string `json:"description"`
	ChannelID   string `json:"channelId"`
	VideoID     string `json:"videoId"`
}

// RepositoryDetails holds repository metadata and the observed Trending snapshot.
type RepositoryDetails struct {
	Description         *string  `json:"description"`
	Homepage            *string  `json:"homepage"`
	ProgrammingLanguage *string  `json:"programmingLanguage"`
	Stars               *int     `json:"stars"`
	Forks               *int     `json:"forks"`
	OpenIssues          *int     `json:"openIssues"`
	Topics              []string `json:"topics"`
	DefaultBranch       *string  `json:"defaultBranch"`
	ReadmeMarkdown      string   `json:"readmeMarkdown"`
	// StarsInPeriod is nil when the upstream snapshot does not supply period-specific growth.
	StarsInPeriod *int   `json:"starsInPeriod"`
	Rank          int    `json:"rank"`
	Period        string `json:"period"`
	// ObservedAt timestamps the repository metrics snapshot, independently of Item publication time.
	ObservedAt     time.Time `json:"observedAt"`
	SnapshotStatus string    `json:"snapshotStatus"`
}

// SourceEntryDetails keeps compact source-specific provenance separate from the canonical Item
// representation. Type must match exactly one non-nil variant.
type SourceEntryDetails struct {
	Type       ItemKind                 `json:"type"`
	Article    *SourceArticleDetails    `json:"article,omitempty"`
	Paper      *SourcePaperDetails      `json:"paper,omitempty"`
	Video      *SourceVideoDetails      `json:"video,omitempty"`
	Repository *SourceRepositoryDetails `json:"repository,omitempty"`
}

// SourceArticleDetails preserves article metadata from one upstream occurrence.
type SourceArticleDetails struct {
	Description string `json:"description"`
}

// SourcePaperDetails preserves paper metadata from one upstream occurrence.
type SourcePaperDetails struct {
	Abstract   string   `json:"abstract"`
	Categories []string `json:"categories"`
	DOI        *string  `json:"doi"`
}

// SourceVideoDetails preserves video metadata from one upstream occurrence.
type SourceVideoDetails struct {
	Description string `json:"description"`
	ChannelID   string `json:"channelId"`
	VideoID     string `json:"videoId"`
}

// SourceRepositoryDetails preserves repository metrics at one upstream observation.
type SourceRepositoryDetails struct {
	Description         *string   `json:"description"`
	Homepage            *string   `json:"homepage"`
	ProgrammingLanguage *string   `json:"programmingLanguage"`
	Stars               *int      `json:"stars"`
	Forks               *int      `json:"forks"`
	OpenIssues          *int      `json:"openIssues"`
	Topics              []string  `json:"topics"`
	DefaultBranch       *string   `json:"defaultBranch"`
	StarsInPeriod       *int      `json:"starsInPeriod"`
	Rank                int       `json:"rank"`
	Period              string    `json:"period"`
	ObservedAt          time.Time `json:"observedAt"`
	SnapshotStatus      string    `json:"snapshotStatus"`
}

// SourceEntry preserves a Source-specific occurrence and its link to a canonical Item.
type SourceEntry struct {
	SourceID          uuid.UUID
	ExternalID        string
	ItemID            uuid.UUID
	URL               string
	Details           SourceEntryDetails
	ExtractionVersion string
	PublishedAt       *time.Time
	DiscoveredAt      time.Time
	UpdatedAt         time.Time
}

// RetrievalText prefers canonical full content and falls back to the excerpt when extraction
// produced no body.
func (i Item) RetrievalText() string {
	parts := []string{strings.TrimSpace(i.Title)}
	if content := strings.TrimSpace(i.ContentMarkdown); content != "" {
		parts = append(parts, content)
	} else if excerpt := strings.TrimSpace(i.Excerpt); excerpt != "" {
		parts = append(parts, excerpt)
	}
	return strings.Join(parts, "\n\n")
}
