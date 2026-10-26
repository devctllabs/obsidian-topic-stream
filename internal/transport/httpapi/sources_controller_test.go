package httpapi_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/ethernity/obsidian-topic-stream/gen/serverhttp"
	"github.com/ethernity/obsidian-topic-stream/internal/domain"
	"github.com/ethernity/obsidian-topic-stream/internal/service/sources"
	"github.com/ethernity/obsidian-topic-stream/internal/transport/httpapi"
	"github.com/ethernity/obsidian-topic-stream/internal/transport/httpapi/mocks"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

func TestCreateAndSyncSourceMapsApplicationResults(t *testing.T) {
	t.Parallel()

	sourceID, jobID := uuid.New(), uuid.New()
	service := mocks.NewMockSources(gomock.NewController(t))
	jobService := mocks.NewMockJobs(gomock.NewController(t))
	now := time.Date(2026, time.September, 27, 12, 0, 0, 0, time.UTC)
	source := domain.Source{ID: sourceID, Name: "Research", Config: domain.SourceConfig{Kind: domain.SourceKindRSS, URL: "https://example.com/feed", AllowPrivateNetwork: true}, Enabled: true, PollIntervalMinutes: 60, CreatedAt: now, UpdatedAt: now}
	service.EXPECT().Create(gomock.Any(), sources.CreateInput{Name: source.Name, Config: source.Config, Enabled: true, PollIntervalMinutes: 60}).Return(source, nil)
	service.EXPECT().SyncAll(gomock.Any()).Return(domain.Job{ID: jobID}, nil)
	jobService.EXPECT().GetForWorkspace(gomock.Any(), uuid.MustParse("018fda3a-4e36-7d1d-8835-45165d3f3596"), jobID).Return(domain.Job{ID: jobID, Status: domain.JobStatusSucceeded, Queue: domain.JobQueueFetch}, nil)
	router := newTestRouter(t, httpapi.Dependencies{Sources: service, Jobs: jobService})
	body := fmt.Sprintf(`{"name":"Research","config":{"kind":"rss","url":%q,"allowPrivateNetwork":true},"enabled":true,"pollIntervalMinutes":60}`, source.Config.URL)
	createResponse := performRequest(router, http.MethodPost, "/api/v1/sources", body)
	require.Equal(t, http.StatusCreated, createResponse.Code, createResponse.Body.String())
	var created serverhttp.Source
	require.NoError(t, json.NewDecoder(createResponse.Body).Decode(&created))

	syncResponse := performRequest(router, http.MethodPost, "/api/v1/sources/sync", "")
	require.Equal(t, http.StatusAccepted, syncResponse.Code, syncResponse.Body.String())
	var accepted serverhttp.JobAccepted
	require.NoError(t, json.NewDecoder(syncResponse.Body).Decode(&accepted))

	jobResponse := performRequest(router, http.MethodGet, "/api/v1/jobs/"+accepted.JobId.String(), "")
	require.Equal(t, http.StatusOK, jobResponse.Code)
	var job serverhttp.Job
	require.NoError(t, json.NewDecoder(jobResponse.Body).Decode(&job))
	require.Equal(t, serverhttp.JobStatusSucceeded, job.Status)

}

func TestSourceCanBeUpdatedAndDeleted(t *testing.T) {
	t.Parallel()

	now := time.Now().UTC()
	service := mocks.NewMockSources(gomock.NewController(t))
	source := domain.Source{ID: uuid.New(), Name: "Old", Config: domain.SourceConfig{Kind: domain.SourceKindRSS, URL: "https://example.com/feed"}, Enabled: true, PollIntervalMinutes: 60, CreatedAt: now, UpdatedAt: now}
	service.EXPECT().Create(gomock.Any(), sources.CreateInput{Name: source.Name, Config: source.Config, Enabled: true, PollIntervalMinutes: 60}).Return(source, nil)
	updatedValue := source
	updatedValue.Name = "New"
	updatedValue.Enabled = false
	updatedValue.PollIntervalMinutes = 120
	name, enabled, interval := "New", false, 120
	service.EXPECT().Update(gomock.Any(), source.ID, sources.UpdateInput{Name: &name, Enabled: &enabled, PollIntervalMinutes: &interval}).Return(updatedValue, nil)
	service.EXPECT().Get(gomock.Any(), source.ID).Return(updatedValue, nil)
	service.EXPECT().Delete(gomock.Any(), source.ID).Return(nil)
	service.EXPECT().Get(gomock.Any(), source.ID).Return(domain.Source{}, sources.ErrNotFound)
	router := newTestRouter(t, httpapi.Dependencies{Sources: service})
	createdResponse := performRequest(router, http.MethodPost, "/api/v1/sources", `{"name":"Old","config":{"kind":"rss","url":"https://example.com/feed","allowPrivateNetwork":false},"enabled":true,"pollIntervalMinutes":60}`)
	require.Equal(t, http.StatusCreated, createdResponse.Code)
	var created serverhttp.Source
	require.NoError(t, json.NewDecoder(createdResponse.Body).Decode(&created))

	updatedResponse := performRequest(router, http.MethodPatch, "/api/v1/sources/"+created.Id.String(), `{"name":"New","enabled":false,"pollIntervalMinutes":120}`)
	require.Equal(t, http.StatusOK, updatedResponse.Code, updatedResponse.Body.String())
	var updated serverhttp.Source
	require.NoError(t, json.NewDecoder(updatedResponse.Body).Decode(&updated))
	require.Equal(t, "New", updated.Name)
	require.False(t, updated.Enabled)
	require.Equal(t, 120, updated.PollIntervalMinutes)

	getResponse := performRequest(router, http.MethodGet, "/api/v1/sources/"+created.Id.String(), "")
	require.Equal(t, http.StatusOK, getResponse.Code)

	deleteResponse := performRequest(router, http.MethodDelete, "/api/v1/sources/"+created.Id.String(), "")
	require.Equal(t, http.StatusNoContent, deleteResponse.Code)
	missingResponse := performRequest(router, http.MethodGet, "/api/v1/sources/"+created.Id.String(), "")
	require.Equal(t, http.StatusNotFound, missingResponse.Code)
}

func performRequest(handler http.Handler, method, path, body string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(method, path, bytes.NewBufferString(body))
	if body != "" {
		request.Header.Set("Content-Type", "application/json")
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}
