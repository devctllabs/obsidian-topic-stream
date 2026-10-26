package httpapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"

	"github.com/ethernity/obsidian-topic-stream/gen/serverhttp"
	"github.com/labstack/echo/v5"
)

type strictJSONSerializer struct {
	echo.DefaultJSONSerializer
}

// Deserialize binds one JSON value while preserving the controllers' decoding error categories.
func (strictJSONSerializer) Deserialize(ctx *echo.Context, destination any) error {
	if err := decodeJSON(ctx.Request().Body, destination); err != nil {
		status, kind := http.StatusBadRequest, bodyProblemKind(destination)
		// Conditional controllers checked If-Match before decoding their bodies.
		if preconditionErr := bodyPreconditionError(destination, ctx.Request().Header.Get("If-Match")); preconditionErr != nil {
			status, kind, err = http.StatusPreconditionRequired, problemVersionPreconditionRequired, preconditionErr
		}
		return &bodyDecodeError{status: status, kind: kind, cause: err}
	}
	return nil
}

type bodyDecodeError struct {
	status int
	kind   problemKind
	cause  error
}

// Error returns the same decoding detail previously produced by strictJSON.
func (e *bodyDecodeError) Error() string { return e.cause.Error() }

// Unwrap retains the underlying decoding failure for server-side inspection.
func (e *bodyDecodeError) Unwrap() error { return e.cause }

func bodyPreconditionError(destination any, ifMatch string) error {
	switch destination.(type) {
	case *serverhttp.WorkspaceSettingsPatch, *serverhttp.PreferenceProfilePatch,
		*serverhttp.DigestPatch, *serverhttp.EmbeddingModelUpdate:
		_, err := parseVersionETag(ifMatch)
		return err
	default:
		return nil
	}
}

func decodeJSON(reader io.Reader, destination any) error {
	decoder := json.NewDecoder(reader)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		if err == nil {
			return fmt.Errorf("request body must contain one JSON value")
		}
		return err
	}
	return nil
}

func bodyProblemKind(destination any) problemKind {
	switch destination.(type) {
	case *serverhttp.FeedRefreshInput:
		return problemInvalidFeedRequest
	case *serverhttp.SourceInput, *serverhttp.SourcePatch:
		return problemInvalidSource
	case *serverhttp.TopicInput, *serverhttp.TopicPatch:
		return problemInvalidTopic
	case *serverhttp.DigestInput, *serverhttp.DigestPatch:
		return problemInvalidDigest
	case *serverhttp.FeedbackInput:
		return problemInvalidFeedback
	case *serverhttp.ReadingStateInput:
		return problemInvalidReadingState
	case *serverhttp.VisibilityInput:
		return problemInvalidVisibility
	case *serverhttp.TopicCorrectionInput:
		return problemInvalidTopicCorrection
	default:
		return workspaceBodyProblemKind(destination)
	}
}

func workspaceBodyProblemKind(destination any) problemKind {
	switch destination.(type) {
	case *serverhttp.WorkspaceSettingsPatch:
		return problemInvalidSettings
	case *serverhttp.PreferenceProfilePatch:
		return problemInvalidPreferenceProfile
	case *serverhttp.EmbeddingModelCreate, *serverhttp.EmbeddingModelUpdate:
		return problemInvalidEmbeddingModel
	case *serverhttp.ExportBundle:
		return problemInvalidWorkspaceArchive
	default:
		return problemInvalidItemState
	}
}
