package httpapi

import (
	"errors"
	"fmt"
	"strings"

	"github.com/ethernity/obsidian-topic-stream/internal/domain"
	"github.com/ethernity/obsidian-topic-stream/internal/service/digests"
	"github.com/ethernity/obsidian-topic-stream/internal/service/embeddingmodels"
	"github.com/ethernity/obsidian-topic-stream/internal/service/feed"
	"github.com/ethernity/obsidian-topic-stream/internal/service/items"
	"github.com/ethernity/obsidian-topic-stream/internal/service/jobs"
	"github.com/ethernity/obsidian-topic-stream/internal/service/search"
	"github.com/ethernity/obsidian-topic-stream/internal/service/settings"
	"github.com/ethernity/obsidian-topic-stream/internal/service/sources"
	"github.com/ethernity/obsidian-topic-stream/internal/service/topics"
)

var expectedErrors = []struct {
	err  error
	kind problemKind
}{
	{domain.ErrSummarySourceNotFound, problemItemNotFound}, {domain.ErrSummaryUnavailable, problemSummaryUnavailable},
	{items.ErrNotFound, problemItemNotFound}, {items.ErrImageNotFound, problemItemNotFound},
	{items.ErrInvalidImage, problemInvalidUpstreamImage}, {sources.ErrNotFound, problemSourceNotFound},
	{topics.ErrNotFound, problemTopicNotFound}, {digests.ErrNotFound, problemDigestNotFound},
	{digests.ErrConflict, problemDigestChanged}, {jobs.ErrNotFound, problemJobNotFound},
	{jobs.ErrNotRetryable, problemJobNotRetryable}, {search.ErrTopicNotFound, problemTopicNotFound},
	{feed.ErrTopicNotFound, problemTopicNotFound}, {domain.ErrNotImplemented, problemNotImplemented},
	{embeddingmodels.ErrNotFound, problemEmbeddingModelNotFound}, {embeddingmodels.ErrConflict, problemEmbeddingModelChanged},
	{domain.ErrRetrievalUnavailable, problemRetrievalUnavailable},
}

var validationCategories = map[problemKind]error{
	problemInvalidSettings: settings.ErrInvalid, problemInvalidPreferenceProfile: settings.ErrInvalid,
	problemInvalidDigest: digests.ErrInvalid, problemInvalidJobQuery: jobs.ErrInvalid,
	problemInvalidSearchRequest: search.ErrInvalidRequest, problemInvalidEmbeddingModel: embeddingmodels.ErrInvalid,
	problemInvalidFeedRequest: feed.ErrInvalidPageToken,
}

// applicationProblem returns nil for an unknown failure so the strict adapter can forward its cause.
func applicationProblem(err error, invalid problemKind) *problemResponse {
	category := validationCategories[invalid]
	if errors.Is(err, domain.ErrValidation) || (category != nil && errors.Is(err, category)) {
		response := validationProblem(invalid, err)
		return &response
	}
	for _, mapping := range expectedErrors {
		if errors.Is(err, mapping.err) {
			response := newProblem(mapping.kind)
			return &response
		}
	}
	return nil
}

type problemField = struct {
	Field   string `json:"field"`
	Message string `json:"message"`
}

func validationProblem(kind problemKind, err error) problemResponse {
	response := newProblem(kind)
	var validation *domain.ValidationError
	if !errors.As(err, &validation) {
		return response
	}
	fields := make([]problemField, 0, len(validation.Violations))
	for _, violation := range validation.Violations {
		fields = append(fields, problemField{Field: violation.Field, Message: violationMessage(violation)})
	}
	response.Body.Errors = &fields
	return response
}

func violationMessage(value domain.Violation) string {
	switch value.Rule {
	case domain.ValidationRequired:
		return "This field is required."
	case domain.ValidationLength:
		return fmt.Sprintf("Enter between %g and %g characters.", value.Minimum, value.Maximum)
	case domain.ValidationRange:
		return fmt.Sprintf("Enter a value between %g and %g.", value.Minimum, value.Maximum)
	case domain.ValidationMinimum:
		return fmt.Sprintf("Enter a value of at least %g.", value.Minimum)
	case domain.ValidationMaximum:
		return fmt.Sprintf("Enter a value no greater than %g.", value.Maximum)
	case domain.ValidationChoice:
		return "Choose one of: " + strings.Join(value.Choices, ", ") + "."
	default:
		return specialViolationMessage(value.Rule)
	}
}

func specialViolationMessage(rule domain.ValidationRule) string {
	switch rule {
	case domain.ValidationHTTPURL:
		return "Enter an absolute HTTP or HTTPS URL without credentials."
	case domain.ValidationEndpoint:
		return "The endpoint could not be verified. Check its address and configuration."
	case domain.ValidationReady:
		return "The embedding model must be ready before it can be enabled."
	case domain.ValidationRelationship:
		return "Choose an available resource in this workspace."
	case domain.ValidationDuplicate:
		return "Remove the duplicate entry."
	case domain.ValidationNotEmpty:
		return "Provide at least one change."
	case domain.ValidationPositive:
		return "Enter a value greater than zero."
	case domain.ValidationTimezone:
		return "Choose a valid IANA time zone, such as UTC or Europe/Berlin."
	default:
		return "Enter a valid value."
	}
}
