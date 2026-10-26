package domain

import (
	"net/url"
	"time"

	"github.com/google/uuid"
)

var DefaultEmbeddingModelID = uuid.MustParse("0199a6e5-4c00-7000-8000-000000000001")

// EmbeddingModelStatus records initialization of an Embedding Model's derived collection.
type EmbeddingModelStatus string

const (
	EmbeddingModelInitializing EmbeddingModelStatus = "initializing"
	EmbeddingModelReady        EmbeddingModelStatus = "ready"
	EmbeddingModelFailed       EmbeddingModelStatus = "failed"
)

// EmbeddingModelDescriptor identifies a portable embedding endpoint, provider model, and vector size.
type EmbeddingModelDescriptor struct {
	Name          string
	Endpoint      string
	ProviderModel string
	Dimensions    int
}

// ValidateEmbeddingModelDescriptor checks a model's identity, vector size, and non-secret HTTP URL.
func ValidateEmbeddingModelDescriptor(value EmbeddingModelDescriptor) error {
	return JoinValidation(CheckLength("name", len(value.Name), 1, 120),
		CheckRequired("providerModel", value.ProviderModel != ""),
		CheckRange("dimensions", float64(value.Dimensions), 1, 65536), validateEmbeddingEndpoint(value.Endpoint))
}

func validateEmbeddingEndpoint(endpoint string) error {
	parsed, err := url.ParseRequestURI(endpoint)
	if err != nil || parsed.Scheme != "http" && parsed.Scheme != "https" || parsed.Host == "" {
		return NewValidationError(Violation{Field: "endpoint", Rule: ValidationHTTPURL})
	}
	if parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return NewValidationError(Violation{Field: "endpoint", Rule: ValidationFormat})
	}
	return nil
}

// EmbeddingModel is a versioned local registry entry with immutable provider model and dimensions.
type EmbeddingModel struct {
	ID uuid.UUID
	EmbeddingModelDescriptor
	Enabled              bool
	InitializationStatus EmbeddingModelStatus
	FailureReason        *string
	Version              int
	CreatedAt            time.Time
	UpdatedAt            time.Time
}

// EmbeddingModelDraft supplies the descriptor for a new registry entry.
type EmbeddingModelDraft struct {
	Name          string
	Endpoint      string
	ProviderModel string
	Dimensions    int
}

// EmbeddingModelUpdate contains mutable registry fields; model and vector dimensions remain fixed.
type EmbeddingModelUpdate struct {
	Name     string
	Endpoint string
	Enabled  bool
}

// ModelCollectionObservation binds one observed Qdrant incarnation to its model and caller scope.
// A zero WorkspaceID is used by installation-wide model initialization.
type ModelCollectionObservation struct {
	WorkspaceID uuid.UUID
	ModelID     uuid.UUID
	Incarnation uuid.UUID
}

// EmbeddingModelOption reports whether a registry entry is ready for selection.
type EmbeddingModelOption struct {
	Model      EmbeddingModel
	Selectable bool
}

// RetrievalStatus distinguishes the desired model from the currently serving model and transition Job.
type RetrievalStatus struct {
	// LatestTransition survives working-pointer cleanup and identifies the current selection.
	LatestTransition *ModelTransition

	DesiredModelID  uuid.UUID
	ServingModelID  *uuid.UUID
	TransitionJobID *uuid.UUID
}
