package httpapi_test

import (
	"encoding/json"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/ethernity/obsidian-topic-stream/gen/serverhttp"
	"github.com/ethernity/obsidian-topic-stream/internal/domain"
	"github.com/ethernity/obsidian-topic-stream/internal/service/settings"
	"github.com/ethernity/obsidian-topic-stream/internal/transport/httpapi"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestExportWorkspaceDoesNotLeakInternalErrorDetails(t *testing.T) {
	t.Parallel()

	router := newTestRouter(t, httpapi.Dependencies{
		Portability: &portabilityServiceStub{exportErr: errors.New("postgres password secret-value")},
	})
	response := performRequest(router, http.MethodGet, "/api/v1/export", "")

	require.Equal(t, http.StatusInternalServerError, response.Code)
	var body serverhttp.Problem
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &body))
	require.Equal(t, "urn:topic-stream:problem:internal-error", body.Type)
	require.Equal(t, http.StatusText(http.StatusInternalServerError), body.Title)
	require.NotContains(t, *body.Detail, "secret-value")
}

func TestExportWorkspaceReturnsVersionedPortableState(t *testing.T) {
	t.Parallel()

	workspaceID := uuid.New()
	now := time.Date(2026, time.September, 28, 12, 0, 0, 0, time.UTC)
	archive := domain.WorkspaceArchive{
		FormatVersion: domain.WorkspaceArchiveFormatV2,
		ExportedAt:    now,
		Data: domain.WorkspaceArchiveData{
			WorkspaceID:    workspaceID,
			EmbeddingModel: domain.EmbeddingModelDescriptor{Name: "default", Endpoint: "http://localhost:11434/v1", ProviderModel: "qwen3-embedding:0.6b", Dimensions: 1024},
			Settings:       settings.DefaultWorkspaceSettings(now),
			Profile:        domain.PreferenceProfile{Prefer: "systems", Revision: 1, Version: 1, UpdatedAt: now},
			Sources:        []domain.Source{}, Topics: []domain.Topic{},
			Feedback: []domain.PortableFeedback{}, Digests: []domain.DigestDefinition{},
		},
	}
	router := newTestRouter(t, httpapi.Dependencies{
		WorkspaceID: workspaceID, Portability: &portabilityServiceStub{archive: archive},
	})
	response := performRequest(router, http.MethodGet, "/api/v1/export", "")

	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	var output serverhttp.ExportBundle
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &output))
	require.Equal(t, serverhttp.TopicStreamv2, output.FormatVersion)
	require.Equal(t, workspaceID, output.WorkspaceId)
	require.Equal(t, "systems", output.PreferenceProfile.Prefer)
	require.Empty(t, output.Feedback)
}

func TestImportWorkspaceAcceptsArchiveAsDurableJob(t *testing.T) {
	t.Parallel()

	workspaceID, jobID := uuid.New(), uuid.New()
	now := time.Date(2026, time.September, 28, 12, 0, 0, 0, time.UTC)
	capability := &portabilityServiceStub{job: domain.Job{ID: jobID}}
	router := newTestRouter(t, httpapi.Dependencies{WorkspaceID: workspaceID, Portability: capability})
	bundle := serverhttp.ExportBundle{
		FormatVersion: serverhttp.TopicStreamv2, ExportedAt: now, WorkspaceId: workspaceID,
		EmbeddingModel:    serverhttp.EmbeddingModelDescriptor{Name: "default", Endpoint: "http://localhost:11434/v1", ProviderModel: "qwen3-embedding:0.6b", Dimensions: 1024},
		Settings:          apiTestWorkspaceSettings(now),
		PreferenceProfile: serverhttp.PreferenceProfile{Revision: 1, Version: 1, UpdatedAt: now},
		Sources:           []serverhttp.Source{}, Topics: []serverhttp.Topic{},
		Feedback: []serverhttp.PortableFeedback{}, Digests: []serverhttp.DigestDefinition{},
	}
	body, err := json.Marshal(bundle)
	require.NoError(t, err)

	response := performRequest(router, http.MethodPost, "/api/v1/import", string(body))

	require.Equal(t, http.StatusAccepted, response.Code, response.Body.String())
	var accepted serverhttp.JobAccepted
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &accepted))
	require.Equal(t, jobID, accepted.JobId)
	require.NotNil(t, capability.imported)
	require.Equal(t, domain.WorkspaceArchiveFormatV2, capability.imported.FormatVersion)
	require.Equal(t, workspaceID, capability.imported.Data.WorkspaceID)
}

func apiTestWorkspaceSettings(now time.Time) serverhttp.WorkspaceSettings {
	defaults := settings.DefaultWorkspaceSettings(now)
	return serverhttp.WorkspaceSettings{
		Version: defaults.Version, EmbeddingModelId: defaults.EmbeddingModelID,
		Generation: serverhttp.GenerationSettings{Enabled: false},
		Summary: serverhttp.SummarySettings{
			Language: defaults.Summary.Language, MaxInputChars: defaults.Summary.MaxInputChars,
			MaxSummaryCharacters: defaults.Summary.MaxSummaryCharacters,
		},
		Instructions: serverhttp.TaskInstructions{
			SummaryChunk: defaults.Instructions.SummaryChunk, SummaryReduce: defaults.Instructions.SummaryReduce,
			SummaryFinal: defaults.Instructions.SummaryFinal, DigestRerank: defaults.Instructions.DigestRerank,
		},
		Retention: serverhttp.RetentionSettings{},
		Retrieval: serverhttp.RetrievalSettings{
			CandidateLimit: defaults.Retrieval.CandidateLimit,
			RerankLimit:    defaults.Retrieval.RerankLimit, RrfK: defaults.Retrieval.RRFK,
			RecencyHalfLifeDays: float32(defaults.Retrieval.RecencyHalfLifeDays),
			RecencyMaxBoost:     float32(defaults.Retrieval.RecencyMaxBoost), FeedSourceCap: defaults.Retrieval.FeedSourceCap,
			DigestSourceCap: defaults.Retrieval.DigestSourceCap, DigestMmrLambda: float32(defaults.Retrieval.DigestMMRLambda),
			LlmRerankEnabled:         defaults.Retrieval.LLMRerankEnabled,
			DigestExplorationEnabled: defaults.Retrieval.DigestExplorationEnabled,
		},
		UpdatedAt: now,
	}
}

func archiveJSON(t *testing.T) string {
	t.Helper()
	now := time.Now().UTC()
	value := serverhttp.ExportBundle{
		FormatVersion: serverhttp.TopicStreamv2, ExportedAt: now, WorkspaceId: uuid.New(),
		EmbeddingModel: serverhttp.EmbeddingModelDescriptor{Name: "default", Endpoint: "http://localhost:11434/v1", ProviderModel: "test", Dimensions: 1024},
		Settings:       apiTestWorkspaceSettings(now), PreferenceProfile: serverhttp.PreferenceProfile{Revision: 1, Version: 1, UpdatedAt: now},
		Sources: []serverhttp.Source{}, Topics: []serverhttp.Topic{}, Feedback: []serverhttp.PortableFeedback{}, Digests: []serverhttp.DigestDefinition{},
	}
	data, err := json.Marshal(value)
	require.NoError(t, err)
	return string(data)
}
