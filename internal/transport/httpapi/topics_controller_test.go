package httpapi_test

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/ethernity/obsidian-topic-stream/gen/serverhttp"
	"github.com/ethernity/obsidian-topic-stream/internal/domain"
	"github.com/ethernity/obsidian-topic-stream/internal/service/topics"
	"github.com/ethernity/obsidian-topic-stream/internal/transport/httpapi"
	"github.com/ethernity/obsidian-topic-stream/internal/transport/httpapi/mocks"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

func TestTopicRoutesMapServiceInputsAndRevisions(t *testing.T) {
	t.Parallel()

	workspaceID := uuid.New()
	sourceID := uuid.New()
	now := time.Date(2026, time.September, 27, 12, 0, 0, 0, time.UTC)
	service := mocks.NewMockTopics(gomock.NewController(t))
	topic := domain.Topic{ID: uuid.New(), WorkspaceID: workspaceID, Name: "Practical AI", SourceIDs: []uuid.UUID{sourceID}, ItemKinds: []domain.ItemKind{domain.ItemKindArticle, domain.ItemKindPaper}, Include: "technical advances in inference", Exclude: "beginner tutorials", Mode: domain.TopicModeOrdinary, Enabled: true, Revision: 1, CreatedAt: now, UpdatedAt: now}
	service.EXPECT().Create(gomock.Any(), topics.CreateInput{Name: topic.Name, SourceIDs: topic.SourceIDs, ItemKinds: topic.ItemKinds, Include: topic.Include, Exclude: topic.Exclude, Mode: topic.Mode, Enabled: true}).Return(topic, nil)
	renamedValue := topic
	renamedValue.Name = "Applied AI"
	rename := "Applied AI"
	service.EXPECT().Update(gomock.Any(), topic.ID, topics.UpdateInput{Name: &rename}).Return(renamedValue, nil)
	revisedValue := renamedValue
	revisedValue.Revision = 2
	revisedValue.Mode = domain.TopicModeStrict
	revisedValue.Include = "production-grade inference systems"
	mode, include := domain.TopicModeStrict, "production-grade inference systems"
	service.EXPECT().Update(gomock.Any(), topic.ID, topics.UpdateInput{Mode: &mode, Include: &include}).Return(revisedValue, nil)
	service.EXPECT().List(gomock.Any()).Return([]domain.Topic{revisedValue}, nil)
	service.EXPECT().Delete(gomock.Any(), topic.ID).Return(nil)
	service.EXPECT().List(gomock.Any()).Return([]domain.Topic{}, nil)
	router := newTestRouter(t, httpapi.Dependencies{Topics: service})

	createdResponse := performRequest(router, http.MethodPost, "/api/v1/topics", `{
		"name":"Practical AI","sourceIds":["`+sourceID.String()+`"],"itemKinds":["article","paper"],
		"include":"technical advances in inference","exclude":"beginner tutorials","mode":"ordinary","enabled":true
	}`)
	require.Equal(t, http.StatusCreated, createdResponse.Code, createdResponse.Body.String())
	var created serverhttp.Topic
	require.NoError(t, json.NewDecoder(createdResponse.Body).Decode(&created))
	require.Equal(t, 1, created.Revision)
	require.Equal(t, []serverhttp.UUID{sourceID}, created.SourceIds)

	renameResponse := performRequest(router, http.MethodPatch, "/api/v1/topics/"+created.Id.String(), `{"name":"Applied AI"}`)
	require.Equal(t, http.StatusOK, renameResponse.Code, renameResponse.Body.String())
	var renamed serverhttp.Topic
	require.NoError(t, json.NewDecoder(renameResponse.Body).Decode(&renamed))
	require.Equal(t, 1, renamed.Revision)

	semanticResponse := performRequest(router, http.MethodPatch, "/api/v1/topics/"+created.Id.String(), `{"mode":"strict","include":"production-grade inference systems"}`)
	require.Equal(t, http.StatusOK, semanticResponse.Code, semanticResponse.Body.String())
	var revised serverhttp.Topic
	require.NoError(t, json.NewDecoder(semanticResponse.Body).Decode(&revised))
	require.Equal(t, 2, revised.Revision)
	require.Equal(t, serverhttp.Strict, revised.Mode)

	listResponse := performRequest(router, http.MethodGet, "/api/v1/topics", "")
	require.Equal(t, http.StatusOK, listResponse.Code)
	var listed serverhttp.TopicList
	require.NoError(t, json.NewDecoder(listResponse.Body).Decode(&listed))
	require.Len(t, listed.Items, 1)
	require.Equal(t, 2, listed.Items[0].Revision)

	deleteResponse := performRequest(router, http.MethodDelete, "/api/v1/topics/"+created.Id.String(), "")
	require.Equal(t, http.StatusNoContent, deleteResponse.Code)
	listResponse = performRequest(router, http.MethodGet, "/api/v1/topics", "")
	require.Equal(t, http.StatusOK, listResponse.Code)
	require.NoError(t, json.NewDecoder(listResponse.Body).Decode(&listed))
	require.Empty(t, listed.Items)
}

func TestTopicRejectsUnknownFieldsAndEmptyScopeIsAllowed(t *testing.T) {
	t.Parallel()

	service := mocks.NewMockTopics(gomock.NewController(t))
	service.EXPECT().Create(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, input topics.CreateInput) (domain.Topic, error) {
		require.Empty(t, input.SourceIDs)
		require.Empty(t, input.ItemKinds)
		require.Equal(t, "crypto", input.Exclude)
		return domain.Topic{ID: uuid.New(), Name: input.Name, Mode: input.Mode, Revision: 1}, nil
	})
	router := newTestRouter(t, httpapi.Dependencies{Topics: service})

	valid := performRequest(router, http.MethodPost, "/api/v1/topics", `{"name":"Everything","sourceIds":[],"itemKinds":[],"include":"","exclude":"crypto","mode":"ordinary","enabled":true}`)
	require.Equal(t, http.StatusCreated, valid.Code, valid.Body.String())

	invalid := performRequest(router, http.MethodPost, "/api/v1/topics", `{"name":"Bad","sourceIds":[],"itemKinds":[],"include":"","exclude":"","mode":"ordinary","enabled":true,"filter":"legacy"}`)
	require.Equal(t, http.StatusBadRequest, invalid.Code)
}

func TestTopicPatchMapsExplicitCorrectionClearPolicy(t *testing.T) {
	t.Parallel()

	topicID := uuid.New()
	service := mocks.NewMockTopics(gomock.NewController(t))
	service.EXPECT().Update(gomock.Any(), topicID, topics.UpdateInput{ClearCorrections: true}).Return(domain.Topic{ID: topicID, Name: "AI", Mode: domain.TopicModeOrdinary, Revision: 1}, nil)
	router := newTestRouter(t, httpapi.Dependencies{Topics: service})

	response := performRequest(
		router,
		http.MethodPatch,
		"/api/v1/topics/"+topicID.String(),
		`{"correctionPolicy":"clear"}`,
	)

	require.Equal(t, http.StatusOK, response.Code, response.Body.String())

	invalid := performRequest(
		router,
		http.MethodPatch,
		"/api/v1/topics/"+topicID.String(),
		`{"correctionPolicy":"drop"}`,
	)
	require.Equal(t, http.StatusBadRequest, invalid.Code)
}
