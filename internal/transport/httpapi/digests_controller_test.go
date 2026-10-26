package httpapi_test

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/ethernity/obsidian-topic-stream/gen/serverhttp"
	"github.com/ethernity/obsidian-topic-stream/internal/domain"
	"github.com/ethernity/obsidian-topic-stream/internal/service/digests"
	"github.com/ethernity/obsidian-topic-stream/internal/transport/httpapi"
	"github.com/ethernity/obsidian-topic-stream/internal/transport/httpapi/mocks"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

func TestDigestRoutesCreateAndListPerTopicLimits(t *testing.T) {
	t.Parallel()

	workspaceID, topicID := uuid.New(), uuid.New()
	now := time.Date(2026, time.September, 28, 10, 0, 0, 0, time.UTC)
	service := mocks.NewMockDigests(gomock.NewController(t))
	definition := domain.DigestDefinition{ID: uuid.New(), WorkspaceID: workspaceID, Name: "Research", Topics: []domain.DigestTopic{{TopicID: topicID, ItemLimit: 7}}, Timezone: "UTC", Recurrence: domain.DigestRecurrence{Frequency: "weekly", Weekdays: []int{1, 4}, Hour: 12}, Enabled: true, Version: 1, CreatedAt: now, UpdatedAt: now}
	service.EXPECT().Create(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, input digests.Input) (domain.DigestDefinition, error) {
		require.Equal(t, definition.Name, input.Name)
		require.Equal(t, definition.Topics, input.Topics)
		require.Equal(t, definition.Recurrence, input.Recurrence)
		require.Equal(t, definition.Timezone, input.Timezone)
		require.True(t, input.Enabled)
		return definition, nil
	})
	service.EXPECT().List(gomock.Any()).Return([]domain.DigestDefinition{definition}, nil)
	router := newTestRouter(t, httpapi.Dependencies{WorkspaceID: workspaceID, Digests: service})

	createdResponse := performRequest(router, http.MethodPost, "/api/v1/digests", `{
		"name":"Research","topics":[{"topicId":"`+topicID.String()+`","itemLimit":7}],
		"timezone":"UTC","recurrence":{"frequency":"weekly","weekdays":[1,4],"hour":12,"minute":0},
		"enabled":true
	}`)

	require.Equal(t, http.StatusCreated, createdResponse.Code, createdResponse.Body.String())
	require.Equal(t, `"1"`, createdResponse.Header().Get("ETag"))
	var created serverhttp.DigestDefinition
	require.NoError(t, json.NewDecoder(createdResponse.Body).Decode(&created))
	require.Equal(t, 7, created.Topics[0].ItemLimit)
	require.Equal(t, 1, created.Version)

	listResponse := performRequest(router, http.MethodGet, "/api/v1/digests", "")
	require.Equal(t, http.StatusOK, listResponse.Code, listResponse.Body.String())
	var list serverhttp.DigestList
	require.NoError(t, json.NewDecoder(listResponse.Body).Decode(&list))
	require.Len(t, list.Items, 1)
	require.Equal(t, created.Id, list.Items[0].Id)
}

func TestDigestIssueHistoryReturnsBoundedSummaries(t *testing.T) {
	t.Parallel()
	service := mocks.NewMockDigests(gomock.NewController(t))
	service.EXPECT().ListIssues(gomock.Any(), digests.IssueListRequest{Limit: 1}).Return(digests.IssuePage{Items: []domain.DigestIssueSummary{{
		ID: uuid.New(), DigestID: uuid.New(), Title: "Saved", Status: domain.DigestIssueComplete,
		GeneratedAt: time.Now().UTC(),
	}}}, nil)
	router := newTestRouter(t, httpapi.Dependencies{WorkspaceID: uuid.New(), Digests: service})
	response := performRequest(router, http.MethodGet, "/api/v1/digest-issues?limit=1", "")
	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	var page map[string]any
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &page))
	require.Contains(t, page, "nextCursor")
	items := page["items"].([]any)
	require.Len(t, items, 1)
	require.NotContains(t, items[0].(map[string]any), "sections")
}

func TestManualBuildRequiresRequestIdentity(t *testing.T) {
	t.Parallel()
	service := mocks.NewMockDigests(gomock.NewController(t))
	service.EXPECT().Generate(gomock.Any(), gomock.Any(), gomock.Any()).Return(domain.Job{ID: uuid.New()}, nil).AnyTimes()
	router := newTestRouter(t, httpapi.Dependencies{WorkspaceID: uuid.New(), Digests: service})
	for _, key := range []string{"", "not-a-uuid"} {
		response := performRequestWithHeaders(router, requestWithHeaders{
			method: http.MethodPost, path: "/api/v1/digests/" + uuid.NewString() + "/generate",
			headers: map[string]string{"Idempotency-Key": key},
		})
		require.Equal(t, http.StatusBadRequest, response.Code, response.Body.String())
		require.Equal(t, "application/problem+json", response.Header().Get("Content-Type"))
	}
}

func TestManualBuildForwardsRequestIdentity(t *testing.T) {
	t.Parallel()
	digestID, requestID, jobID := uuid.New(), uuid.New(), uuid.New()
	service := mocks.NewMockDigests(gomock.NewController(t))
	service.EXPECT().Generate(gomock.Any(), digestID, requestID).Return(domain.Job{ID: jobID}, nil)
	router := newTestRouter(t, httpapi.Dependencies{WorkspaceID: uuid.New(), Digests: service})
	response := performRequestWithHeaders(router, requestWithHeaders{
		method: http.MethodPost, path: "/api/v1/digests/" + digestID.String() + "/generate",
		headers: map[string]string{"Idempotency-Key": requestID.String()},
	})
	require.Equal(t, http.StatusAccepted, response.Code, response.Body.String())
	var accepted serverhttp.JobAccepted
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &accepted))
	require.Equal(t, jobID, accepted.JobId)
}

func TestDigestIssueResponsesExposeGeneratingJobIdentity(t *testing.T) {
	t.Parallel()
	issueID, digestID, jobID := uuid.New(), uuid.New(), uuid.New()
	service := mocks.NewMockDigests(gomock.NewController(t))
	service.EXPECT().GetIssue(gomock.Any(), issueID).Return(domain.DigestIssue{ID: issueID, DigestID: digestID, GenerationJobID: jobID, Title: "Saved", Status: domain.DigestIssueComplete, GeneratedAt: time.Now().UTC()}, nil)
	service.EXPECT().ListIssues(gomock.Any(), digests.IssueListRequest{Limit: 20}).Return(digests.IssuePage{Items: []domain.DigestIssueSummary{{ID: issueID, DigestID: digestID, GenerationJobID: jobID, Title: "Saved", Status: domain.DigestIssueComplete, GeneratedAt: time.Now().UTC()}}}, nil)
	router := newTestRouter(t, httpapi.Dependencies{WorkspaceID: uuid.New(), Digests: service})
	response := performRequest(router, http.MethodGet, "/api/v1/digest-issues/"+issueID.String(), "")
	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	var detail map[string]any
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &detail))
	require.Equal(t, jobID.String(), detail["generationJobId"])
	response = performRequest(router, http.MethodGet, "/api/v1/digest-issues", "")
	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	var page struct {
		Items []map[string]any `json:"items"`
	}
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &page))
	require.Len(t, page.Items, 1)
	require.Equal(t, jobID.String(), page.Items[0]["generationJobId"])
}
