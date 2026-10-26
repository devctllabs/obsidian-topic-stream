package httpapi_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/ethernity/obsidian-topic-stream/gen/serverhttp"
	"github.com/ethernity/obsidian-topic-stream/internal/domain"
	"github.com/ethernity/obsidian-topic-stream/internal/service/settings"
	"github.com/ethernity/obsidian-topic-stream/internal/transport/httpapi"
	"github.com/ethernity/obsidian-topic-stream/internal/transport/httpapi/mocks"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

func TestWorkspaceSettingsUseETagOptimisticConcurrency(t *testing.T) {
	service := mocks.NewMockSettings(gomock.NewController(t))
	now := time.Date(2026, time.September, 27, 12, 0, 0, 0, time.UTC)
	defaultsValue := settings.DefaultWorkspaceSettings(now)
	updatedValue := defaultsValue
	updatedValue.Version = 2
	updatedValue.Retrieval.CandidateLimit = 60
	service.EXPECT().GetWorkspace(gomock.Any()).Return(defaultsValue, nil)
	service.EXPECT().UpdateWorkspace(gomock.Any(), 1, gomock.Any()).DoAndReturn(func(_ context.Context, version int, patch domain.WorkspaceSettingsPatch) (domain.WorkspaceSettings, error) {
		require.Equal(t, 60, patch.Retrieval.CandidateLimit)
		return updatedValue, nil
	})
	service.EXPECT().UpdateWorkspace(gomock.Any(), 1, gomock.Any()).Return(domain.WorkspaceSettings{}, settings.ErrConflict)
	router := newTestRouter(t, httpapi.Dependencies{Settings: service})

	get := performRequest(router, http.MethodGet, "/api/v1/workspace-settings", "")
	require.Equal(t, http.StatusOK, get.Code, get.Body.String())
	require.Equal(t, `"1"`, get.Header().Get("ETag"))
	require.NotContains(t, get.Body.String(), "apiKey")
	var defaults serverhttp.WorkspaceSettings
	require.NoError(t, json.NewDecoder(get.Body).Decode(&defaults))
	require.Equal(t, domain.DefaultEmbeddingModelID, defaults.EmbeddingModelId)
	require.Equal(t, 50, defaults.Retrieval.CandidateLimit)
	require.NotContains(t, get.Body.String(), `"workers"`)
	require.False(t, defaults.Generation.Enabled)

	patch := performRequestWithHeaders(router, requestWithHeaders{
		method: http.MethodPatch, path: "/api/v1/workspace-settings",
		body: `{"retrieval":{"candidateLimit":60,"rerankLimit":30,"rrfK":60,"recencyHalfLifeDays":7,"recencyMaxBoost":0.1,"feedSourceCap":3,"digestSourceCap":2,"digestMmrLambda":0.75,"llmRerankEnabled":false,"digestExplorationEnabled":true}}`, headers: map[string]string{"If-Match": `"1"`},
	})
	require.Equal(t, http.StatusOK, patch.Code, patch.Body.String())
	require.Equal(t, `"2"`, patch.Header().Get("ETag"))
	var updated serverhttp.WorkspaceSettings
	require.NoError(t, json.NewDecoder(patch.Body).Decode(&updated))
	require.Equal(t, 60, updated.Retrieval.CandidateLimit)

	stale := performRequestWithHeaders(router, requestWithHeaders{
		method: http.MethodPatch, path: "/api/v1/workspace-settings",
		body: `{"retrieval":{"candidateLimit":70,"rerankLimit":30,"rrfK":60,"recencyHalfLifeDays":7,"recencyMaxBoost":0.1,"feedSourceCap":3,"digestSourceCap":2,"digestMmrLambda":0.75,"llmRerankEnabled":false,"digestExplorationEnabled":true}}`, headers: map[string]string{"If-Match": `"1"`},
	})
	require.Equal(t, http.StatusConflict, stale.Code, stale.Body.String())
}

func TestPreferenceProfileUsesIndependentVersionAndRevision(t *testing.T) {
	service := mocks.NewMockSettings(gomock.NewController(t))
	prefer, avoid := "database internals", "shallow product news"
	service.EXPECT().UpdatePreferenceProfile(gomock.Any(), 1, domain.PreferenceProfilePatch{Prefer: &prefer, Avoid: &avoid}).Return(domain.PreferenceProfile{Version: 2, Revision: 2, Prefer: prefer, Avoid: avoid}, nil)
	router := newTestRouter(t, httpapi.Dependencies{Settings: service})

	response := performRequestWithHeaders(router, requestWithHeaders{
		method: http.MethodPatch, path: "/api/v1/preference-profile",
		body: `{"prefer":"database internals","avoid":"shallow product news"}`, headers: map[string]string{"If-Match": `"1"`},
	})
	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	var profile serverhttp.PreferenceProfile
	require.NoError(t, json.NewDecoder(response.Body).Decode(&profile))
	require.Equal(t, 2, profile.Version)
	require.Equal(t, 2, profile.Revision)
	require.Equal(t, "database internals", profile.Prefer)
}

func TestWorkspaceSettingsRejectMissingPreconditionUnknownFieldsAndInvalidGeneration(t *testing.T) {
	service := mocks.NewMockSettings(gomock.NewController(t))
	service.EXPECT().UpdateWorkspace(gomock.Any(), 1, gomock.Any()).Return(domain.WorkspaceSettings{}, settings.ErrInvalid)
	router := newTestRouter(t, httpapi.Dependencies{Settings: service})

	missing := performRequest(router, http.MethodPatch, "/api/v1/workspace-settings", `{"generation":{"enabled":false,"endpoint":null,"summaryModel":null,"rerankModel":null}}`)
	require.Equal(t, http.StatusPreconditionRequired, missing.Code, missing.Body.String())
	secret := performRequestWithHeaders(router, requestWithHeaders{
		method: http.MethodPatch, path: "/api/v1/workspace-settings",
		body: `{"apiKey":"do-not-store"}`, headers: map[string]string{"If-Match": `"1"`},
	})
	require.Equal(t, http.StatusBadRequest, secret.Code, secret.Body.String())
	require.NotContains(t, secret.Body.String(), "do-not-store")
	invalid := performRequestWithHeaders(router, requestWithHeaders{
		method: http.MethodPatch, path: "/api/v1/workspace-settings",
		body: `{"generation":{"enabled":true,"endpoint":null,"summaryModel":null,"rerankModel":null}}`, headers: map[string]string{"If-Match": `"1"`},
	})
	require.Equal(t, http.StatusBadRequest, invalid.Code, invalid.Body.String())
}

type requestWithHeaders struct {
	method  string
	path    string
	body    string
	headers map[string]string
}

func performRequestWithHeaders(handler http.Handler, input requestWithHeaders) *httptest.ResponseRecorder {
	request := httptest.NewRequest(input.method, input.path, bytes.NewBufferString(input.body))
	request.Header.Set("Content-Type", "application/json")
	for name, value := range input.headers {
		request.Header.Set(name, value)
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}
