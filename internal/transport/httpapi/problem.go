package httpapi

import (
	"net/http"
	"strings"

	"github.com/ethernity/obsidian-topic-stream/gen/serverhttp"
	"github.com/labstack/echo/v5"
)

const genericServerErrorDetail = "The server could not complete the request."

type problemKind string

const (
	problemInvalidDigest               problemKind = "invalid_digest"
	problemInvalidFeedback             problemKind = "invalid_feedback"
	problemInvalidFeedRequest          problemKind = "invalid_feed_request"
	problemInvalidItemState            problemKind = "invalid_item_state"
	problemSummaryUnavailable          problemKind = "summary_unavailable"
	problemInvalidJobQuery             problemKind = "invalid_job_query"
	problemInvalidPreferenceProfile    problemKind = "invalid_preference_profile"
	problemInvalidReadingState         problemKind = "invalid_reading_state"
	problemInvalidSearchRequest        problemKind = "invalid_search_request"
	problemInvalidSettings             problemKind = "invalid_settings"
	problemInvalidSource               problemKind = "invalid_source"
	problemInvalidStatus               problemKind = "invalid_status"
	problemInvalidTopic                problemKind = "invalid_topic"
	problemInvalidTopicCorrection      problemKind = "invalid_topic_correction"
	problemInvalidUpstreamImage        problemKind = "invalid_upstream_image"
	problemInvalidVisibility           problemKind = "invalid_visibility"
	problemInvalidWorkspaceArchive     problemKind = "invalid_workspace_archive"
	problemDigestChanged               problemKind = "digest_changed"
	problemEmbeddingModelChanged       problemKind = "embedding_model_changed"
	problemEmbeddingModelNotFound      problemKind = "embedding_model_not_found"
	problemInvalidEmbeddingModel       problemKind = "invalid_embedding_model"
	problemDigestNotFound              problemKind = "digest_not_found"
	problemItemNotFound                problemKind = "item_not_found"
	problemJobNotRetryable             problemKind = "job_not_retryable"
	problemJobNotFound                 problemKind = "job_not_found"
	problemPreferenceProfileChanged    problemKind = "preference_profile_changed"
	problemSettingsChanged             problemKind = "settings_changed"
	problemSourceNotFound              problemKind = "source_not_found"
	problemTopicNotFound               problemKind = "topic_not_found"
	problemVersionPreconditionRequired problemKind = "version_precondition_required"
	problemWorkspaceImportFailed       problemKind = "workspace_import_failed"
	problemInternalError               problemKind = "internal_error"
	problemNotImplemented              problemKind = "not_implemented"
	problemRetrievalUnavailable        problemKind = "retrieval_unavailable"
)

type problemDefinition struct {
	status int
	title  string
	detail string
}

var problemCatalog = map[problemKind]problemDefinition{
	problemInvalidDigest:               {400, "Invalid digest", "Check the digest fields and try again."},
	problemInvalidFeedback:             {400, "Invalid feedback", "Check the feedback fields and try again."},
	problemInvalidFeedRequest:          {400, "Invalid feed request", "Check the feed filters and page token, then try again."},
	problemInvalidItemState:            {400, "Invalid item state", "Check the item state and try again."},
	problemSummaryUnavailable:          {409, "Summary unavailable", "AI summary needs supported full content and enabled generation."},
	problemInvalidJobQuery:             {400, "Invalid job query", "Check the job filters and page token, then try again."},
	problemInvalidPreferenceProfile:    {400, "Invalid preference profile", "Check the preference guidance and try again."},
	problemInvalidReadingState:         {400, "Invalid reading state", "Choose read or unread."},
	problemInvalidSearchRequest:        {400, "Invalid search request", "Check the search query, filters, and page token."},
	problemInvalidSettings:             {400, "Invalid settings", "Check the workspace settings and try again."},
	problemInvalidSource:               {400, "Invalid source", "Check the source fields and try again."},
	problemInvalidStatus:               {400, "Invalid status", "Choose a supported job status."},
	problemInvalidTopic:                {400, "Invalid topic", "Check the topic fields and try again."},
	problemInvalidTopicCorrection:      {400, "Invalid topic correction", "Check the topic and membership selection."},
	problemInvalidUpstreamImage:        {502, "Invalid upstream image", "The source did not return a valid image."},
	problemInvalidVisibility:           {400, "Invalid visibility", "Choose visible or hidden."},
	problemInvalidWorkspaceArchive:     {400, "Invalid workspace archive", "Check the workspace archive and try again."},
	problemDigestChanged:               {409, "Digest changed", "The digest changed. Reload it before applying your changes."},
	problemEmbeddingModelChanged:       {409, "Embedding model changed", "The embedding model changed or its name is already in use. Reload the registry and try again."},
	problemEmbeddingModelNotFound:      {404, "Embedding model not found", "Embedding model not found."},
	problemInvalidEmbeddingModel:       {400, "Invalid embedding model", "Check the embedding model fields and readiness."},
	problemDigestNotFound:              {404, "Digest not found", "Digest not found."},
	problemItemNotFound:                {404, "Item not found", "Item not found."},
	problemJobNotRetryable:             {409, "Job is not retryable", "This job cannot be retried in its current state."},
	problemJobNotFound:                 {404, "Job not found", "Job not found."},
	problemPreferenceProfileChanged:    {409, "Preference profile changed", "The preference profile changed. Reload it before applying your changes."},
	problemSettingsChanged:             {409, "Settings changed", "The workspace settings changed. Reload them before applying your changes."},
	problemSourceNotFound:              {404, "Source not found", "Source not found."},
	problemTopicNotFound:               {404, "Topic not found", "Topic not found."},
	problemVersionPreconditionRequired: {428, "Version precondition required", "Provide the quoted resource version in If-Match."},
	problemWorkspaceImportFailed:       {400, "Workspace import failed", "The workspace archive could not be imported."},
	problemInternalError:               {500, "Internal Server Error", genericServerErrorDetail},
	problemNotImplemented:              {501, "Not Implemented", "This operation is not implemented."},
	problemRetrievalUnavailable:        {503, "Retrieval unavailable", "Search is temporarily unavailable while its index is restored. Check Task Center for progress."},
}

// problemResponse matches generated default responses without depending on an operation.
type problemResponse struct {
	Body       serverhttp.Problem
	StatusCode int
}

func newProblem(kind problemKind) problemResponse {
	definition, known := problemCatalog[kind]
	if !known {
		kind, definition = problemInternalError, problemCatalog[problemInternalError]
	}
	code := string(kind)
	response := serverhttp.Problem{
		Type: "urn:topic-stream:problem:" + strings.ReplaceAll(code, "_", "-"),
		Code: &code, Title: definition.title, Detail: &definition.detail, Status: definition.status,
	}
	if kind == problemNotImplemented {
		response.Type, response.Code = "about:blank", nil
	}
	return problemResponse{Body: response, StatusCode: definition.status}
}

func genericProblem(status int) problemResponse {
	if status >= http.StatusInternalServerError {
		return newProblem(problemInternalError)
	}
	title := http.StatusText(status)
	return problemResponse{Body: serverhttp.Problem{Type: "about:blank", Title: title, Detail: &title, Status: status}, StatusCode: status}
}

func writeProblem(ctx *echo.Context, response problemResponse) error {
	ctx.Response().Header().Set(echo.HeaderContentType, "application/problem+json")
	return ctx.JSON(response.StatusCode, response.Body)
}
