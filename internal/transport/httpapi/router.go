package httpapi

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/devctllabs/go-libs/oapivalidator"
	"github.com/ethernity/obsidian-topic-stream/gen/serverhttp"
	"github.com/ethernity/obsidian-topic-stream/internal/domain"
	"github.com/ethernity/obsidian-topic-stream/internal/service/embeddingmodels"
	"github.com/ethernity/obsidian-topic-stream/internal/service/events"
	"github.com/ethernity/obsidian-topic-stream/internal/service/systemstatus"
	"github.com/google/uuid"
	"github.com/labstack/echo/v5"
)

// Dependencies supplies the Workspace-scoped capabilities exposed by the HTTP API.
type Dependencies struct {
	WorkspaceID     uuid.UUID
	Status          *systemstatus.Service
	Feed            Feed
	Sources         Sources
	Jobs            Jobs
	Items           Items
	Topics          Topics
	Settings        Settings
	EmbeddingModels *embeddingmodels.Service
	Search          Search
	Digests         Digests
	Summaries       Summaries
	Events          *events.Service
	Portability     Portability
}

// Portability exports portable state and submits validated archive imports.
type Portability interface {
	// Export returns portable configuration and current feedback without ingested content or secrets.
	Export(ctx context.Context) (domain.WorkspaceArchive, error)
	// Import validates an archive and enqueues its application to the Workspace.
	Import(ctx context.Context, archive domain.WorkspaceArchive) (domain.Job, error)
}

// NewRouter validates dependencies and installs generated routes, request validation, and problem
// handling.
func NewRouter(dependencies Dependencies) (http.Handler, error) {
	server, err := NewServer(dependencies)
	if err != nil {
		return nil, fmt.Errorf("construct HTTP server: %w", err)
	}
	validator, err := newOpenAPIValidator()
	if err != nil {
		return nil, err
	}
	router := echo.New()
	router.HTTPErrorHandler = handleHTTPError
	router.JSONSerializer = strictJSONSerializer{}
	router.Use(validator)

	serverhttp.RegisterHandlers(router, serverhttp.NewStrictHandler(server, nil))

	return router, nil
}

func handleHTTPError(context *echo.Context, err error) {
	response, unwrapErr := echo.UnwrapResponse(context.Response())
	if unwrapErr == nil && response.Committed {
		return
	}
	var decodeError *bodyDecodeError
	if errors.As(err, &decodeError) {
		failure := newProblem(decodeError.kind)
		if decodeError.status == http.StatusBadRequest {
			fields := []problemField{{Field: "body", Message: "Provide exactly one valid JSON value with the expected fields."}}
			failure.Body.Errors = &fields
		}
		_ = writeProblem(context, failure)
		return
	}
	status := http.StatusInternalServerError
	var httpError *echo.HTTPError
	if errors.As(err, &httpError) {
		status = httpError.Code
	}
	if status >= http.StatusInternalServerError {
		logInternalError(context, err)
	}
	_ = writeProblem(context, genericProblem(status))
}

func logInternalError(ctx *echo.Context, err error) {
	cause := err
	for child := errors.Unwrap(cause); child != nil; child = errors.Unwrap(cause) {
		cause = child
	}
	// Arbitrary error strings may contain SQL, URLs, or credentials. Log bounded diagnostic facts.
	attributes := []any{"error_type", fmt.Sprintf("%T", err), "cause_type", fmt.Sprintf("%T", cause),
		"method", ctx.Request().Method, "route", ctx.Path()}
	var databaseError interface{ SQLState() string }
	if errors.As(err, &databaseError) {
		attributes = append(attributes, "sql_state", databaseError.SQLState())
	}
	ctx.Logger().ErrorContext(ctx.Request().Context(), "HTTP operation failed", attributes...)
}

func newOpenAPIValidator() (echo.MiddlewareFunc, error) {
	document, err := serverhttp.GetSpec()
	if err != nil {
		return nil, fmt.Errorf("load embedded OpenAPI document: %w", err)
	}
	middleware, err := oapivalidator.New(
		document,
		oapivalidator.WithFailureHandler(oapivalidator.FailureHandlerFunc(handleOpenAPIFailure)),
	)
	if err != nil {
		return nil, fmt.Errorf("construct OpenAPI validator: %w", err)
	}
	return middleware, nil
}

type boundaryProblem struct {
	Type   string            `json:"type"`
	Title  string            `json:"title"`
	Status int               `json:"status"`
	Detail string            `json:"detail,omitempty"`
	Code   string            `json:"code,omitempty"`
	Errors []validationError `json:"errors,omitempty"`
}

type validationError struct {
	Field   string `json:"field"`
	Message string `json:"message"`
}

func handleOpenAPIFailure(context *echo.Context, failure *oapivalidator.Failure) error {
	source := failure.Problem()
	response := boundaryProblem{
		Type: source.Type, Title: source.Title, Status: validationStatus(failure),
		Detail: source.Detail, Code: string(failure.Kind),
		Errors: make([]validationError, 0, len(failure.Errors)),
	}
	messages := schemaMessages(failure.Cause, failure.Errors)
	for _, field := range failure.Errors {
		response.Errors = append(response.Errors, validationError{Field: validationField(field), Message: boundaryFieldMessage(field, messages)})
	}
	context.Response().Header().Set(echo.HeaderContentType, "application/problem+json")
	return context.JSON(response.Status, response)
}

func validationStatus(failure *oapivalidator.Failure) int {
	if failure.Kind == oapivalidator.FailureInvalidRequest {
		for _, field := range failure.Errors {
			if field.Code == "required" && field.In == oapivalidator.LocationHeader && strings.EqualFold(field.Parameter, "If-Match") {
				return http.StatusPreconditionRequired
			}
		}
		return http.StatusBadRequest
	}
	if failure.Kind == oapivalidator.FailureMalformedRequest {
		return http.StatusBadRequest
	}
	return failure.Status
}

func validationField(field oapivalidator.FieldError) string {
	if field.Pointer != "" {
		return field.Pointer
	}
	if field.Parameter != "" {
		return field.Parameter
	}
	return string(field.In)
}
