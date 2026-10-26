package httpapi_test

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/ethernity/obsidian-topic-stream/gen/serverhttp"
	"github.com/ethernity/obsidian-topic-stream/internal/domain"
	"github.com/ethernity/obsidian-topic-stream/internal/transport/httpapi"
	"github.com/ethernity/obsidian-topic-stream/internal/transport/httpapi/mocks"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

func TestItemSummaryReadDoesNotRequestGeneration(t *testing.T) {
	t.Parallel()
	itemID := uuid.New()
	version := strings.Repeat("a", 64)
	service := mocks.NewMockSummaries(gomock.NewController(t))
	service.EXPECT().Read(gomock.Any(), domain.SummarySource{ItemID: itemID, SourceVersion: version}).Return(domain.ItemSummary{SourceVersion: version, Supported: true, CanGenerate: true, State: domain.ItemSummaryNotRequested}, nil)
	router := newTestRouter(t, httpapi.Dependencies{Summaries: service})
	response := performRequest(router, http.MethodGet, "/api/v1/items/"+itemID.String()+"/ai-summary?sourceVersion="+version, "")
	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	var state map[string]any
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &state))
	require.Equal(t, "not_requested", state["state"])
	require.Equal(t, true, state["canGenerate"])
	require.Nil(t, state["result"])
}

func TestExplicitItemAndDigestSummaryRequestsReturnJobReferences(t *testing.T) {
	t.Parallel()
	itemID, issueID, jobID := uuid.New(), uuid.New(), uuid.New()
	service := mocks.NewMockSummaries(gomock.NewController(t))
	service.EXPECT().Request(gomock.Any(), domain.SummarySource{ItemID: itemID}).Return(domain.Job{ID: jobID}, nil)
	service.EXPECT().Request(gomock.Any(), domain.SummarySource{ItemID: itemID, IssueID: &issueID}).Return(domain.Job{ID: jobID}, nil)
	router := newTestRouter(t, httpapi.Dependencies{Summaries: service})
	for _, path := range []string{"/api/v1/items/" + itemID.String() + "/ai-summary", "/api/v1/digest-issues/" + issueID.String() + "/items/" + itemID.String() + "/ai-summary"} {
		response := performRequest(router, http.MethodPost, path, "")
		require.Equal(t, http.StatusAccepted, response.Code, response.Body.String())
		var accepted serverhttp.JobAccepted
		require.NoError(t, json.Unmarshal(response.Body.Bytes(), &accepted))
		require.Equal(t, jobID, accepted.JobId)
	}
}

func TestSummaryReadsReadyResultWhenGenerationIsDisabled(t *testing.T) {
	t.Parallel()
	itemID, jobID := uuid.New(), uuid.New()
	version := strings.Repeat("b", 64)
	reason := "generation_disabled"
	service := mocks.NewMockSummaries(gomock.NewController(t))
	service.EXPECT().Read(gomock.Any(), domain.SummarySource{ItemID: itemID}).Return(domain.ItemSummary{
		SourceVersion: version, Supported: true, CanGenerate: false, UnavailableReason: &reason, State: domain.ItemSummaryReady, JobID: &jobID,
		Result: &domain.AISummary{ID: uuid.New(), GenerationJobID: jobID, SourceVersion: version, Text: "Ready annotation", Model: "summary-model", Language: "same-as-source", MaxInputChars: 48000, MaxSummaryCharacters: 800, InstructionsVersion: "v1", AlgorithmVersion: "v1", CreatedAt: time.Now().UTC()},
	}, nil)
	router := newTestRouter(t, httpapi.Dependencies{Summaries: service})
	response := performRequest(router, http.MethodGet, "/api/v1/items/"+itemID.String()+"/ai-summary", "")
	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	var result serverhttp.ItemSummary
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &result))
	require.False(t, result.CanGenerate)
	require.Equal(t, "Ready annotation", result.Result.Text)
}

func TestSummaryRequestsRejectClientContentAndInvalidVersion(t *testing.T) {
	t.Parallel()
	service := mocks.NewMockSummaries(gomock.NewController(t))
	router := newTestRouter(t, httpapi.Dependencies{Summaries: service})
	path := "/api/v1/items/" + uuid.NewString() + "/ai-summary"
	response := performRequest(router, http.MethodPost, path, `{"text":"arbitrary input","url":"https://other.example"}`)
	require.Equal(t, http.StatusBadRequest, response.Code, response.Body.String())
	response = performRequest(router, http.MethodGet, path+"?sourceVersion=invalid", "")
	require.Equal(t, http.StatusBadRequest, response.Code, response.Body.String())
}

func TestSummaryRequestsMapUnavailableAndMissingOwnedSource(t *testing.T) {
	for _, scenario := range []struct {
		failure error
		status  int
	}{
		{domain.ErrSummaryUnavailable, http.StatusConflict},
		{domain.ErrSummarySourceNotFound, http.StatusNotFound},
	} {
		t.Run(scenario.failure.Error(), func(t *testing.T) {
			itemID := uuid.New()
			service := mocks.NewMockSummaries(gomock.NewController(t))
			service.EXPECT().Request(gomock.Any(), domain.SummarySource{ItemID: itemID}).Return(domain.Job{}, scenario.failure)
			router := newTestRouter(t, httpapi.Dependencies{Summaries: service})
			response := performRequest(router, http.MethodPost, "/api/v1/items/"+itemID.String()+"/ai-summary", "")
			require.Equal(t, scenario.status, response.Code, response.Body.String())
			require.Equal(t, "application/problem+json", response.Header().Get("Content-Type"))
		})
	}
}
