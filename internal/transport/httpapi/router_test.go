package httpapi_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ethernity/obsidian-topic-stream/internal/domain"
	"github.com/ethernity/obsidian-topic-stream/internal/service/jobs"
	"github.com/ethernity/obsidian-topic-stream/internal/transport/httpapi/mocks"
	"go.uber.org/mock/gomock"

	"github.com/ethernity/obsidian-topic-stream/gen/serverhttp"
	"github.com/ethernity/obsidian-topic-stream/internal/transport/httpapi"
	"github.com/stretchr/testify/require"
)

func TestRouterRejectsRequestsThatDoNotMatchOpenAPI(t *testing.T) {
	t.Parallel()

	router := newTestRouter(t, httpapi.Dependencies{})
	request := httptest.NewRequest(http.MethodPost, "/api/v1/sources", bytes.NewBufferString(`{}`))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()

	router.ServeHTTP(response, request)

	require.Equal(t, http.StatusBadRequest, response.Code)
	require.Equal(t, "application/problem+json", response.Header().Get("Content-Type"))
	var body serverhttp.Problem
	require.NoError(t, json.NewDecoder(response.Body).Decode(&body))
	require.Equal(t, "invalid_request", *body.Code)
	require.Len(t, *body.Errors, 3)
	for _, field := range *body.Errors {
		require.NotEmpty(t, field.Field)
		require.NotEmpty(t, field.Message)
	}
}

func TestRouterMapsOpenAPIRequestBoundariesToProblems(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		method      string
		path        string
		body        string
		contentType string
		status      int
	}{
		{"unknown path", http.MethodGet, "/api/v1/unknown", "", "", http.StatusNotFound},
		{"unsupported method", http.MethodPost, "/api/v1/system/status", "", "", http.StatusMethodNotAllowed},
		{"invalid path parameter", http.MethodGet, "/api/v1/sources/not-a-uuid", "", "", http.StatusBadRequest},
		{"malformed JSON", http.MethodPost, "/api/v1/sources", `{`, "application/json", http.StatusBadRequest},
		{"unsupported media type", http.MethodPost, "/api/v1/sources", `{}`, "text/plain", http.StatusUnsupportedMediaType},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			router := newTestRouter(t, httpapi.Dependencies{})
			request := httptest.NewRequest(test.method, test.path, bytes.NewBufferString(test.body))
			if test.contentType != "" {
				request.Header.Set("Content-Type", test.contentType)
			}
			response := httptest.NewRecorder()

			router.ServeHTTP(response, request)

			require.Equal(t, test.status, response.Code, response.Body.String())
			require.Equal(t, "application/problem+json", response.Header().Get("Content-Type"))
		})
	}
}

func TestOpenAPIBoundaryUsesValidatorProblemType(t *testing.T) {
	t.Parallel()

	router := newTestRouter(t, httpapi.Dependencies{})
	response := performRequest(router, http.MethodGet, "/api/v1/unknown", "")

	require.Equal(t, http.StatusNotFound, response.Code)
	var body serverhttp.Problem
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &body))
	require.Equal(t, "urn:devctl:oapivalidator:problem:not-found", body.Type)
	require.Equal(t, "Operation not found", body.Title)
}

func TestApplicationProblemHasStableMachineReadableType(t *testing.T) {
	t.Parallel()

	service := mocks.NewMockJobs(gomock.NewController(t))
	service.EXPECT().List(gomock.Any(), gomock.Any()).Return(jobs.Page{}, jobs.ErrInvalid)
	router := newTestRouter(t, httpapi.Dependencies{Jobs: service})
	response := performRequest(router, http.MethodGet, "/api/v1/jobs?cursor=not-an-opaque-cursor", "")

	require.Equal(t, http.StatusBadRequest, response.Code)
	var body serverhttp.Problem
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &body))
	require.Equal(t, "urn:topic-stream:problem:invalid-job-query", body.Type)
	require.Equal(t, "Invalid job query", body.Title)
	require.Equal(t, "invalid_job_query", *body.Code)
}

func TestRouterPreservesValidatedJSONBody(t *testing.T) {
	t.Parallel()

	for _, suffix := range []string{` {}`, ` null`, ` trailing`} {
		t.Run(suffix, func(t *testing.T) {
			t.Parallel()
			service := mocks.NewMockFeed(gomock.NewController(t))
			service.EXPECT().Refresh(gomock.Any(), domain.FeedRefreshCommand{Limit: 25}).Return(domain.FeedPage{}, domain.ErrNotImplemented)
			router := newTestRouter(t, httpapi.Dependencies{Feed: service})
			response := performRequest(router, http.MethodPost, "/api/v1/feed/refresh", `{}`+suffix)

			require.Equal(t, http.StatusNotImplemented, response.Code, response.Body.String())
			require.Equal(t, "application/problem+json", response.Header().Get("Content-Type"))
			var body serverhttp.Problem
			require.NoError(t, json.Unmarshal(response.Body.Bytes(), &body))
			require.Equal(t, "about:blank", body.Type)
			require.Nil(t, body.Code)
		})
	}
}

func TestRouterPreservesInvalidPreconditionBeforeTrailingJSON(t *testing.T) {
	t.Parallel()
	service := mocks.NewMockSettings(gomock.NewController(t))
	router := newTestRouter(t, httpapi.Dependencies{Settings: service})
	response := performRequestWithHeaders(router, requestWithHeaders{
		method: http.MethodPatch, path: "/api/v1/workspace-settings",
		body: `{"embeddingModelId":"018fda3a-4e36-7d1d-8835-45165d3f3596"} {}`, headers: map[string]string{"If-Match": `"999999999999999999999999999999"`},
	})

	require.Equal(t, http.StatusPreconditionRequired, response.Code, response.Body.String())
	var body serverhttp.Problem
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &body))
	require.Equal(t, "urn:topic-stream:problem:version-precondition-required", body.Type)
}

func TestRouterMapsBodyDecodeErrorsToOperationProblems(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		path string
		body string
		code string
	}{
		{"/api/v1/workspace-settings", `{"embeddingModelId":"018fda3a-4e36-7d1d-8835-45165d3f3596"} {}`, "invalid_settings"},
		{"/api/v1/preference-profile", `{"prefer":"databases"} {}`, "invalid_preference_profile"},
	} {
		t.Run(test.code, func(t *testing.T) {
			t.Parallel()
			router := newTestRouter(t, httpapi.Dependencies{})
			response := performRequestWithHeaders(router, requestWithHeaders{
				method: http.MethodPatch, path: test.path, body: test.body,
				headers: map[string]string{"If-Match": `"1"`},
			})

			require.Equal(t, http.StatusBadRequest, response.Code, response.Body.String())
			var body serverhttp.Problem
			require.NoError(t, json.Unmarshal(response.Body.Bytes(), &body))
			require.Equal(t, test.code, *body.Code)
			require.NotNil(t, body.Errors)
			require.Equal(t, "body", (*body.Errors)[0].Field)
			require.Equal(t, "Provide exactly one valid JSON value with the expected fields.", (*body.Errors)[0].Message)
		})
	}
}

func TestRouterPropagatesRequestContextThroughStrictHandler(t *testing.T) {
	t.Parallel()
	type contextKey struct{}
	ctx, cancel := context.WithCancel(context.WithValue(t.Context(), contextKey{}, "request value"))
	defer cancel()
	service := mocks.NewMockFeed(gomock.NewController(t))
	var received context.Context
	service.EXPECT().Read(gomock.Any(), domain.FeedReadQuery{Limit: 25}).DoAndReturn(
		func(callContext context.Context, query domain.FeedReadQuery) (domain.FeedPage, error) {
			received = callContext
			return domain.FeedPage{}, domain.ErrNotImplemented
		},
	)
	router := newTestRouter(t, httpapi.Dependencies{Feed: service})
	request := httptest.NewRequest(http.MethodGet, "/api/v1/feed", nil).WithContext(ctx)
	response := httptest.NewRecorder()

	router.ServeHTTP(response, request)

	require.Equal(t, http.StatusNotImplemented, response.Code)
	require.NotNil(t, received)
	require.Equal(t, "request value", received.Value(contextKey{}))
	cancel()
	require.ErrorIs(t, received.Err(), context.Canceled)
}

func TestUnknownApplicationErrorsReturnSafeInternalProblem(t *testing.T) {
	t.Parallel()
	for _, area := range []string{"items", "sources", "topics", "import", "settings"} {
		t.Run(area, func(t *testing.T) {
			t.Parallel()
			failure := errors.New("private database failure with credential")
			dependencies := httpapi.Dependencies{}
			method, path, body := http.MethodGet, "/api/v1/items/018fda3a-4e36-7d1d-8835-45165d3f3596", ""
			switch area {
			case "items":
				service := mocks.NewMockItems(gomock.NewController(t))
				service.EXPECT().Get(gomock.Any(), gomock.Any()).Return(domain.Item{}, failure)
				dependencies.Items = service
			case "sources":
				service := mocks.NewMockSources(gomock.NewController(t))
				service.EXPECT().Get(gomock.Any(), gomock.Any()).Return(domain.Source{}, failure)
				dependencies.Sources, path = service, "/api/v1/sources/018fda3a-4e36-7d1d-8835-45165d3f3596"
			case "topics":
				service := mocks.NewMockTopics(gomock.NewController(t))
				service.EXPECT().Delete(gomock.Any(), gomock.Any()).Return(failure)
				dependencies.Topics, method, path = service, http.MethodDelete, "/api/v1/topics/018fda3a-4e36-7d1d-8835-45165d3f3596"
			case "import":
				dependencies.Portability = &portabilityServiceStub{importErr: failure}
				method, path, body = http.MethodPost, "/api/v1/import", archiveJSON(t)
			case "settings":
				service := mocks.NewMockSettings(gomock.NewController(t))
				service.EXPECT().GetWorkspace(gomock.Any()).Return(domain.WorkspaceSettings{}, failure)
				dependencies.Settings, path = service, "/api/v1/workspace-settings"
			}
			router := newTestRouter(t, dependencies)

			response := performRequest(router, method, path, body)

			require.Equal(t, http.StatusInternalServerError, response.Code, response.Body.String())
			require.Equal(t, "application/problem+json", response.Header().Get("Content-Type"))
			var problem serverhttp.Problem
			require.NoError(t, json.Unmarshal(response.Body.Bytes(), &problem))
			require.Equal(t, "urn:topic-stream:problem:internal-error", problem.Type)
			require.Equal(t, "internal_error", *problem.Code)
			require.Equal(t, "The server could not complete the request.", *problem.Detail)
			require.NotContains(t, response.Body.String(), "private")
		})
	}
}

func TestWrappedBusinessValidationReturnsSafeFieldMessages(t *testing.T) {
	t.Parallel()
	service := mocks.NewMockSources(gomock.NewController(t))
	failure := fmt.Errorf("private operation context: %w", domain.NewValidationError(
		domain.Violation{Field: "name", Rule: domain.ValidationRequired},
		domain.Violation{Field: "pollIntervalMinutes", Rule: domain.ValidationRange, Minimum: 15, Maximum: 10080},
	))
	service.EXPECT().Create(gomock.Any(), gomock.Any()).Return(domain.Source{}, failure)
	router := newTestRouter(t, httpapi.Dependencies{Sources: service})
	response := performRequest(router, http.MethodPost, "/api/v1/sources",
		`{"name":"Research","config":{"kind":"rss","url":"https://example.com/feed"},"enabled":true,"pollIntervalMinutes":60}`)

	require.Equal(t, http.StatusBadRequest, response.Code, response.Body.String())
	var problem serverhttp.Problem
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &problem))
	require.Equal(t, "invalid_source", *problem.Code)
	require.NotContains(t, response.Body.String(), "private")
	require.NotNil(t, problem.Errors)
	require.Len(t, *problem.Errors, 2)
	require.Equal(t, "name", (*problem.Errors)[0].Field)
	require.Equal(t, "This field is required.", (*problem.Errors)[0].Message)
	require.Equal(t, "Enter a value between 15 and 10080.", (*problem.Errors)[1].Message)
}

func TestOpenAPIValidationExplainsIndependentConstraints(t *testing.T) {
	t.Parallel()
	router := newTestRouter(t, httpapi.Dependencies{})
	response := performRequest(router, http.MethodPost, "/api/v1/sources",
		`{"name":"","config":{"kind":"rss","url":"https://example.com/feed"},"enabled":true,"pollIntervalMinutes":1}`)
	require.Equal(t, http.StatusBadRequest, response.Code)
	var problem serverhttp.Problem
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &problem))
	require.Equal(t, "invalid_request", *problem.Code)
	require.NotNil(t, problem.Errors)
	messages := make(map[string]string)
	for _, issue := range *problem.Errors {
		messages[issue.Field] = issue.Message
	}
	require.Equal(t, "Enter at least 1 character.", messages["/name"])
	require.Equal(t, "Enter a value of at least 15.", messages["/pollIntervalMinutes"])
}

func TestRetryJobKeepsItsConflictStatusAndCode(t *testing.T) {
	t.Parallel()
	service := mocks.NewMockJobs(gomock.NewController(t))
	service.EXPECT().RetryForWorkspace(gomock.Any(), gomock.Any(), gomock.Any()).Return(domain.Job{}, jobs.ErrNotRetryable)
	router := newTestRouter(t, httpapi.Dependencies{Jobs: service})
	response := performRequest(router, http.MethodPost, "/api/v1/jobs/018fda3a-4e36-7d1d-8835-45165d3f3596/retry", "")
	require.Equal(t, http.StatusConflict, response.Code, response.Body.String())
	var problem serverhttp.Problem
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &problem))
	require.Equal(t, "job_not_retryable", *problem.Code)
}

func TestValidationPreservesTheReportLimit(t *testing.T) {
	t.Parallel()
	values := make([]string, 25)
	for index := range values {
		values[index] = fmt.Sprintf("unsupported-%d", index)
	}
	items, err := json.Marshal(values)
	require.NoError(t, err)
	body := `{"name":"Topic","sourceIds":[],"itemKinds":` + string(items) + `,"include":"","exclude":"","mode":"ordinary","enabled":true}`
	router := newTestRouter(t, httpapi.Dependencies{})
	response := performRequest(router, http.MethodPost, "/api/v1/topics", body)
	require.Equal(t, http.StatusBadRequest, response.Code, response.Body.String())
	var problem serverhttp.Problem
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &problem))
	require.NotNil(t, problem.Errors)
	require.Len(t, *problem.Errors, 20)
	require.NotContains(t, response.Body.String(), "unsupported-")
}

func TestForeignValidationCategoryIsNotTreatedAsInvalidUserInput(t *testing.T) {
	t.Parallel()
	service := mocks.NewMockSources(gomock.NewController(t))
	service.EXPECT().Get(gomock.Any(), gomock.Any()).Return(domain.Source{}, jobs.ErrInvalid)
	router := newTestRouter(t, httpapi.Dependencies{Sources: service})
	response := performRequest(router, http.MethodGet, "/api/v1/sources/018fda3a-4e36-7d1d-8835-45165d3f3596", "")
	require.Equal(t, http.StatusInternalServerError, response.Code, response.Body.String())
}

func TestOpenAPIValidationMessagesForBoundsTypesAndChoices(t *testing.T) {
	t.Parallel()
	for _, test := range []struct{ name, method, path, body, message string }{
		{"maximum", http.MethodGet, "/api/v1/feed?limit=101", "", "Enter a value between 1 and 100."},
		{"type", http.MethodPost, "/api/v1/sources", `{"name":"Feed","config":{"kind":"rss","url":"https://example.com/feed"},"enabled":true,"pollIntervalMinutes":"private-value"}`, "Use the expected value type: integer."},
		{"length", http.MethodPost, "/api/v1/sources", `{"name":"` + strings.Repeat("x", 121) + `","config":{"kind":"rss","url":"https://example.com/feed"},"enabled":true,"pollIntervalMinutes":60}`, "Enter at most 120 characters."},
		{"enum", http.MethodPut, "/api/v1/items/018fda3a-4e36-7d1d-8835-45165d3f3596/feedback", `{"sentiment":"private-value","comment":null}`, "Choose one of: like, dislike."},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			router := newTestRouter(t, httpapi.Dependencies{})
			response := performRequest(router, test.method, test.path, test.body)
			require.Equal(t, http.StatusBadRequest, response.Code, response.Body.String())
			require.Contains(t, response.Body.String(), test.message)
			require.NotContains(t, response.Body.String(), "private-value")
		})
	}
}
