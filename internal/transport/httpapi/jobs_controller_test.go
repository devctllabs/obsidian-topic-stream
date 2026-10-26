package httpapi_test

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/ethernity/obsidian-topic-stream/gen/serverhttp"
	"github.com/ethernity/obsidian-topic-stream/internal/domain"
	"github.com/ethernity/obsidian-topic-stream/internal/service/jobs"
	"github.com/ethernity/obsidian-topic-stream/internal/transport/httpapi"
	"github.com/ethernity/obsidian-topic-stream/internal/transport/httpapi/mocks"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

func TestTaskCenterListsAndRetriesJobsWithoutExposingArguments(t *testing.T) {
	workspaceID := uuid.New()
	service := mocks.NewMockJobs(gomock.NewController(t))
	now := time.Now().UTC()
	started, finished := now.Add(-time.Second), now
	created := domain.Job{ID: uuid.New(), WorkspaceID: workspaceID, Kind: "fails", Status: domain.JobStatusDead, Queue: domain.JobQueueMaintenance, Arguments: json.RawMessage(`{"apiToken":"must-not-leak"}`), StartedAt: &started, FinishedAt: &finished}
	status := domain.JobStatusDead
	service.EXPECT().List(gomock.Any(), jobs.ListRequest{WorkspaceID: workspaceID, Status: &status, Limit: 20}).Return(jobs.Page{Items: []domain.Job{created}}, nil)
	service.EXPECT().RetryForWorkspace(gomock.Any(), workspaceID, created.ID).Return(domain.Job{ID: uuid.New()}, nil)
	router := newTestRouter(t, httpapi.Dependencies{WorkspaceID: workspaceID, Jobs: service})
	response := performRequest(router, http.MethodGet, "/api/v1/jobs?status=dead&limit=20", "")
	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	require.NotContains(t, response.Body.String(), "must-not-leak")
	require.NotContains(t, response.Body.String(), "apiToken")
	var page serverhttp.JobList
	require.NoError(t, json.NewDecoder(response.Body).Decode(&page))
	require.Len(t, page.Items, 1)
	require.Equal(t, serverhttp.JobStatusDead, page.Items[0].Status)
	require.Equal(t, serverhttp.Maintenance, page.Items[0].Queue)
	require.NotNil(t, page.Items[0].StartedAt)
	require.NotNil(t, page.Items[0].FinishedAt)
	require.NotNil(t, page.Items[0].DurationMs)

	retry := performRequest(router, http.MethodPost, "/api/v1/jobs/"+created.ID.String()+"/retry", "")
	require.Equal(t, http.StatusAccepted, retry.Code, retry.Body.String())
	var accepted serverhttp.JobAccepted
	require.NoError(t, json.NewDecoder(retry.Body).Decode(&accepted))
	require.NotEqual(t, created.ID, accepted.JobId)
}

func TestTaskCenterReturnsCohortProgressWithoutClaimingJobCompletion(t *testing.T) {
	workspaceID, id := uuid.New(), uuid.New()
	service := mocks.NewMockJobs(gomock.NewController(t))
	total := int64(3)
	job := domain.Job{ID: id, WorkspaceID: workspaceID, Kind: "reindex-workspace", Status: domain.JobStatusRunning, Queue: domain.JobQueueMaintenance,
		Progress: &domain.JobProgress{Phase: "catching_up", ProcessedItems: 3, TotalItems: &total, UpdatedAt: time.Now().UTC()}}
	service.EXPECT().GetForWorkspace(gomock.Any(), workspaceID, id).Return(job, nil)
	service.EXPECT().List(gomock.Any(), jobs.ListRequest{WorkspaceID: workspaceID, Limit: 20}).Return(jobs.Page{Items: []domain.Job{job}}, nil)
	router := newTestRouter(t, httpapi.Dependencies{WorkspaceID: workspaceID, Jobs: service})
	response := performRequest(router, http.MethodGet, "/api/v1/jobs/"+id.String(), "")
	require.Equal(t, 200, response.Code)
	var body map[string]any
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &body))
	require.Equal(t, "running", body["status"])
	require.NotNil(t, body["progress"], "fixed-cohort completion must be exposed separately from Job success")
	progress := body["progress"].(map[string]any)
	require.Equal(t, "catching_up", progress["phase"])
	require.Equal(t, float64(3), progress["processedItems"])
	require.Equal(t, float64(3), progress["totalItems"])
	require.NotContains(t, progress, "percent")
	response = performRequest(router, http.MethodGet, "/api/v1/jobs?limit=20", "")
	require.Equal(t, 200, response.Code)
	require.Contains(t, response.Body.String(), `"processedItems":3`)
}

func TestTaskCenterCancelsQueuedJob(t *testing.T) {
	workspaceID := uuid.New()
	service := mocks.NewMockJobs(gomock.NewController(t))
	created := domain.Job{ID: uuid.New(), WorkspaceID: workspaceID, Status: domain.JobStatusCancelled}
	service.EXPECT().CancelForWorkspace(gomock.Any(), workspaceID, created.ID).Return(created, nil)
	router := newTestRouter(t, httpapi.Dependencies{WorkspaceID: workspaceID, Jobs: service})

	response := performRequest(router, http.MethodPost, "/api/v1/jobs/"+created.ID.String()+"/cancel", "")
	require.Equal(t, http.StatusAccepted, response.Code, response.Body.String())
	require.True(t, strings.Contains(response.Body.String(), `"status":"cancelled"`))
}

func TestTaskCenterCannotReadOrMutateAnotherWorkspaceJob(t *testing.T) {
	t.Parallel()

	selectedWorkspace := uuid.New()
	created := domain.Job{ID: uuid.New()}
	service := mocks.NewMockJobs(gomock.NewController(t))
	service.EXPECT().GetForWorkspace(gomock.Any(), selectedWorkspace, created.ID).Return(domain.Job{}, jobs.ErrNotFound)
	service.EXPECT().RetryForWorkspace(gomock.Any(), selectedWorkspace, created.ID).Return(domain.Job{}, jobs.ErrNotFound)
	service.EXPECT().CancelForWorkspace(gomock.Any(), selectedWorkspace, created.ID).Return(domain.Job{}, jobs.ErrNotFound)
	router := newTestRouter(t, httpapi.Dependencies{WorkspaceID: selectedWorkspace, Jobs: service})

	for _, request := range []struct {
		method string
		path   string
	}{
		{http.MethodGet, "/api/v1/jobs/" + created.ID.String()},
		{http.MethodPost, "/api/v1/jobs/" + created.ID.String() + "/retry"},
		{http.MethodPost, "/api/v1/jobs/" + created.ID.String() + "/cancel"},
	} {
		response := performRequest(router, request.method, request.path, "")
		require.Equal(t, http.StatusNotFound, response.Code, response.Body.String())
	}

}

func TestJobReadsExposeVerifiedModelSelectionWithoutArguments(t *testing.T) {
	workspaceID, id := uuid.New(), uuid.New()
	service := mocks.NewMockJobs(gomock.NewController(t))
	link := &domain.ModelTransition{ModelID: uuid.New(), SelectionGeneration: 4, JobID: &id, Status: "cancelled", IsCurrent: false, CanRetry: false}
	job := domain.Job{ID: id, WorkspaceID: workspaceID, Kind: "reindex-workspace", Status: domain.JobStatusCancelled, Queue: domain.JobQueueMaintenance, ModelTransition: link, Arguments: json.RawMessage(`{"secret":"private-payload"}`)}
	service.EXPECT().GetForWorkspace(gomock.Any(), workspaceID, id).Return(job, nil)
	service.EXPECT().List(gomock.Any(), jobs.ListRequest{WorkspaceID: workspaceID, Limit: 20}).Return(jobs.Page{Items: []domain.Job{job}}, nil)
	router := newTestRouter(t, httpapi.Dependencies{WorkspaceID: workspaceID, Jobs: service})
	for _, path := range []string{"/api/v1/jobs/" + id.String(), "/api/v1/jobs?limit=20"} {
		response := performRequest(router, http.MethodGet, path, "")
		require.Equal(t, 200, response.Code, response.Body.String())
		require.Contains(t, response.Body.String(), `"modelTransition":`)
		require.Contains(t, response.Body.String(), `"selectionGeneration":4`)
		require.Contains(t, response.Body.String(), `"isCurrent":false`)
		require.NotContains(t, response.Body.String(), "private-payload")
	}
}
