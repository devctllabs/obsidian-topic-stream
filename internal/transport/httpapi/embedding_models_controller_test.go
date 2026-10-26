package httpapi_test

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/ethernity/obsidian-topic-stream/gen/serverhttp"
	"github.com/ethernity/obsidian-topic-stream/internal/domain"
	"github.com/ethernity/obsidian-topic-stream/internal/service/embeddingmodels"
	"github.com/ethernity/obsidian-topic-stream/internal/transport/httpapi"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestEmbeddingModelAdminLifecycleAndWorkspaceStatus(t *testing.T) {
	now := time.Date(2026, 9, 29, 15, 0, 0, 0, time.UTC)
	modelID := uuid.MustParse("0199a6e5-4c00-7000-8000-000000000099")
	repository := &embeddingHTTPRepository{models: make(map[uuid.UUID]domain.EmbeddingModel)}
	service, err := embeddingmodels.New(embeddingmodels.Config{
		WorkspaceID: uuid.New(), Repository: repository, Initializer: embeddingHTTPInitializer{},
		Now: func() time.Time { return now }, NewID: func() uuid.UUID { return modelID },
	})
	require.NoError(t, err)
	router := newTestRouter(t, httpapi.Dependencies{EmbeddingModels: service})

	created := performRequest(router, http.MethodPost, "/api/v1/admin/embedding-models",
		`{"name":"Local Qwen","endpoint":"http://localhost:11434/v1","providerModel":"qwen3-embedding:0.6b","dimensions":1024}`)
	require.Equal(t, http.StatusCreated, created.Code, created.Body.String())
	require.Equal(t, `"2"`, created.Header().Get("ETag"))
	var model serverhttp.EmbeddingModel
	require.NoError(t, json.Unmarshal(created.Body.Bytes(), &model))
	require.Equal(t, modelID, model.Id)
	require.Equal(t, serverhttp.EmbeddingModelInitializationStatusReady, model.InitializationStatus)
	require.False(t, model.Enabled)

	updated := performRequestWithHeaders(router, requestWithHeaders{
		method: http.MethodPut, path: "/api/v1/admin/embedding-models/" + modelID.String(),
		body:    `{"name":"Local Qwen","endpoint":"http://localhost:11434/v1","enabled":true}`,
		headers: map[string]string{"If-Match": `"2"`},
	})
	require.Equal(t, http.StatusOK, updated.Code, updated.Body.String())

	repository.status = domain.RetrievalStatus{DesiredModelID: modelID, ServingModelID: &modelID}
	options := performRequest(router, http.MethodGet, "/api/v1/embedding-model-options", "")
	require.Equal(t, http.StatusOK, options.Code, options.Body.String())
	var optionList serverhttp.EmbeddingModelOptionList
	require.NoError(t, json.Unmarshal(options.Body.Bytes(), &optionList))
	require.Len(t, optionList.Items, 1)
	require.True(t, optionList.Items[0].Selectable)

	status := performRequest(router, http.MethodGet, "/api/v1/retrieval-status", "")
	require.Equal(t, http.StatusOK, status.Code, status.Body.String())
}

type embeddingHTTPRepository struct {
	models map[uuid.UUID]domain.EmbeddingModel
	status domain.RetrievalStatus
}

// List returns records stored by the fixture.
func (r *embeddingHTTPRepository) List(context.Context) ([]domain.EmbeddingModel, error) {
	values := make([]domain.EmbeddingModel, 0, len(r.models))
	for _, model := range r.models {
		values = append(values, model)
	}
	return values, nil
}

// Get returns the fixture record and its lookup result.
func (r *embeddingHTTPRepository) Get(_ context.Context, id uuid.UUID) (domain.EmbeddingModel, bool, error) {
	model, found := r.models[id]
	return model, found, nil
}

// Create stores the supplied record in the fixture.
func (r *embeddingHTTPRepository) Create(_ context.Context, model domain.EmbeddingModel) error {
	r.models[model.ID] = model
	return nil
}

// Update stores the record only when the fixture's current version matches.
func (r *embeddingHTTPRepository) Update(_ context.Context, model domain.EmbeddingModel, version int) (bool, error) {
	current, found := r.models[model.ID]
	if !found || current.Version != version {
		return false, nil
	}
	r.models[model.ID] = model
	return true, nil
}

// RetrievalStatus returns the fixture's desired and serving model state.
func (r *embeddingHTTPRepository) RetrievalStatus(context.Context, uuid.UUID) (domain.RetrievalStatus, error) {
	return r.status, nil
}

type embeddingHTTPInitializer struct{}

// Initialize accepts initialization without opening a derived collection.
func (embeddingHTTPInitializer) Initialize(context.Context, domain.EmbeddingModel) error { return nil }

// Probe accepts the descriptor without contacting an embedding provider.
func (embeddingHTTPInitializer) Probe(context.Context, domain.EmbeddingModelDescriptor) error {
	return nil
}

func TestRetrievalStatusRetainsTerminalSelectionAndWaitingIdentity(t *testing.T) {
	for _, state := range []string{"waiting", "queued", "dead", "cancelled", "succeeded"} {
		t.Run(state, func(t *testing.T) {
			modelID, jobID := uuid.New(), uuid.New()
			transition := &domain.ModelTransition{ModelID: modelID, SelectionGeneration: 17, Status: state, IsCurrent: true, CanRetry: state == "dead" || state == "cancelled"}
			if state != "waiting" {
				transition.JobID = &jobID
			}
			repository := &embeddingHTTPRepository{status: domain.RetrievalStatus{DesiredModelID: modelID, LatestTransition: transition}}
			service, err := embeddingmodels.New(embeddingmodels.Config{WorkspaceID: uuid.New(), Repository: repository, Initializer: embeddingHTTPInitializer{}, Now: time.Now, NewID: uuid.New})
			require.NoError(t, err)
			response := performRequest(newTestRouter(t, httpapi.Dependencies{EmbeddingModels: service}), http.MethodGet, "/api/v1/retrieval-status", "")
			require.Equal(t, http.StatusOK, response.Code, response.Body.String())
			var body map[string]json.RawMessage
			require.NoError(t, json.Unmarshal(response.Body.Bytes(), &body))
			expected, err := json.Marshal(transition)
			require.NoError(t, err)
			require.JSONEq(t, string(expected), string(body["latestTransition"]))
			require.JSONEq(t, "null", string(body["transitionJobId"]), "working pointer stays independent")
		})
	}
}
