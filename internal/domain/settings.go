package domain

import (
	"time"

	"github.com/google/uuid"
)

// EmbeddingSettings describes the provider connection and vector dimensions captured by an index.
type EmbeddingSettings struct {
	Endpoint   string `json:"endpoint"`
	Model      string `json:"model"`
	Dimensions int    `json:"dimensions"`
}

// GenerationSettings selects optional summary and reranking endpoints without credentials.
type GenerationSettings struct {
	Enabled      bool    `json:"enabled"`
	Endpoint     *string `json:"endpoint"`
	SummaryModel *string `json:"summaryModel"`
	RerankModel  *string `json:"rerankModel"`
}

// SummarySettings bounds summary input and output and selects the output language.
type SummarySettings struct {
	Language             string `json:"language"`
	MaxInputChars        int    `json:"maxInputChars"`
	MaxSummaryCharacters int    `json:"maxSummaryCharacters"`
}

// TaskInstructions contains user-configurable generation prompts for each task stage.
type TaskInstructions struct {
	SummaryChunk  string `json:"summaryChunk"`
	SummaryReduce string `json:"summaryReduce"`
	SummaryFinal  string `json:"summaryFinal"`
	DigestRerank  string `json:"digestRerank"`
}

// RetentionSettings controls canonical full-content retention.
type RetentionSettings struct {
	// FullContentDays is nil when full content is retained indefinitely.
	FullContentDays *int `json:"fullContentDays"`
}

// RetrievalSettings controls candidate limits, ranking, personalization, and diversification.
type RetrievalSettings struct {
	CandidateLimit           int     `json:"candidateLimit"`
	RerankLimit              int     `json:"rerankLimit"`
	RRFK                     int     `json:"rrfK"`
	RecencyHalfLifeDays      float64 `json:"recencyHalfLifeDays"`
	RecencyMaxBoost          float64 `json:"recencyMaxBoost"`
	FeedSourceCap            int     `json:"feedSourceCap"`
	DigestSourceCap          int     `json:"digestSourceCap"`
	DigestMMRLambda          float64 `json:"digestMmrLambda"`
	LLMRerankEnabled         bool    `json:"llmRerankEnabled"`
	DigestExplorationEnabled bool    `json:"digestExplorationEnabled"`
}

// WorkspaceSettings is a versioned snapshot of non-secret retrieval and generation behavior.
type WorkspaceSettings struct {
	Version int `json:"version"`
	// EmbeddingModelID is the desired model; a previous ready model may still serve during transition.
	EmbeddingModelID uuid.UUID          `json:"embeddingModelId"`
	Generation       GenerationSettings `json:"generation"`
	Summary          SummarySettings    `json:"summary"`
	Instructions     TaskInstructions   `json:"instructions"`
	Retention        RetentionSettings  `json:"retention"`
	Retrieval        RetrievalSettings  `json:"retrieval"`
	UpdatedAt        time.Time          `json:"updatedAt"`
}

// WorkspaceSettingsPatch replaces supplied settings groups; nil fields preserve current values.
type WorkspaceSettingsPatch struct {
	EmbeddingModelID *uuid.UUID
	Generation       *GenerationSettings
	Summary          *SummarySettings
	Instructions     *TaskInstructions
	Retention        *RetentionSettings
	Retrieval        *RetrievalSettings
}

// PreferenceProfile is versioned global Prefer and Avoid guidance with immutable semantic revisions.
type PreferenceProfile struct {
	Prefer string `json:"prefer"`
	Avoid  string `json:"avoid"`
	// Revision identifies immutable semantic guidance; Version also fences concurrent edits.
	Revision  int       `json:"revision"`
	Version   int       `json:"version"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// PreferenceProfilePatch updates supplied guidance; nil preserves the corresponding text.
type PreferenceProfilePatch struct {
	Prefer *string
	Avoid  *string
}
