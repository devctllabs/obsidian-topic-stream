package httpapi_test

import (
	"encoding/json"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/ethernity/obsidian-topic-stream/gen/serverhttp"
	"github.com/ethernity/obsidian-topic-stream/internal/domain"
	"github.com/ethernity/obsidian-topic-stream/internal/service/items"
	"github.com/ethernity/obsidian-topic-stream/internal/transport/httpapi"
	"github.com/ethernity/obsidian-topic-stream/internal/transport/httpapi/mocks"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

func TestFeedbackRoutesMapServiceResultsAndTimestamps(t *testing.T) {
	t.Parallel()

	workspaceID := uuid.MustParse("018fda3a-4e36-7d1d-8835-45165d3f3596")
	sourceID := uuid.MustParse("018fda3a-89b3-7648-a409-7e033adc3401")
	itemID := "018fda3b-0002-7000-8000-000000000002"
	now := time.Date(2026, time.September, 27, 12, 0, 0, 0, time.UTC)
	itemService := mocks.NewMockItems(gomock.NewController(t))
	firstAt := now
	comment := "Deep and practical"
	itemService.EXPECT().SetFeedback(gomock.Any(), uuid.MustParse(itemID), "like", &comment).Return(domain.Feedback{Sentiment: "like", Comment: &comment, CreatedAt: firstAt, UpdatedAt: firstAt}, nil)
	itemService.EXPECT().SetFeedback(gomock.Any(), uuid.MustParse(itemID), "dislike", nil).Return(domain.Feedback{Sentiment: "dislike", CreatedAt: firstAt, UpdatedAt: firstAt.Add(time.Hour)}, nil)
	currentValue := item(itemID, workspaceID, feedItem{sourceID: sourceID, title: "Feedback target", discoveredAt: now})
	currentValue.Feedback = &domain.Feedback{Sentiment: "dislike", CreatedAt: firstAt, UpdatedAt: firstAt.Add(time.Hour)}
	itemService.EXPECT().Get(gomock.Any(), uuid.MustParse(itemID)).Return(currentValue, nil)
	router := newTestRouter(t, httpapi.Dependencies{Items: itemService})

	likedResponse := performRequest(router, http.MethodPut, "/api/v1/items/"+itemID+"/feedback", `{"sentiment":"like","comment":"Deep and practical"}`)
	require.Equal(t, http.StatusOK, likedResponse.Code, likedResponse.Body.String())
	var liked serverhttp.Feedback
	require.NoError(t, json.NewDecoder(likedResponse.Body).Decode(&liked))
	require.Equal(t, serverhttp.FeedbackSentimentLike, liked.Sentiment)
	require.Equal(t, now, liked.CreatedAt)

	now = now.Add(time.Hour)
	dislikedResponse := performRequest(router, http.MethodPut, "/api/v1/items/"+itemID+"/feedback", `{"sentiment":"dislike","comment":null}`)
	require.Equal(t, http.StatusOK, dislikedResponse.Code, dislikedResponse.Body.String())
	var disliked serverhttp.Feedback
	require.NoError(t, json.NewDecoder(dislikedResponse.Body).Decode(&disliked))
	require.Equal(t, serverhttp.FeedbackSentimentDislike, disliked.Sentiment)
	require.Equal(t, liked.CreatedAt, disliked.CreatedAt)
	require.Equal(t, now, disliked.UpdatedAt)

	response := performRequest(router, http.MethodGet, "/api/v1/items/"+itemID, "")
	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	var current serverhttp.Item
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &current))
	require.NotNil(t, current.Feedback)
	require.Equal(t, serverhttp.FeedbackSentimentDislike, current.Feedback.Sentiment)
}

func TestFeedbackReturnsProjectionPendingProblemAfterCanonicalCommit(t *testing.T) {
	t.Parallel()

	itemID, deliveredRevision := uuid.New(), uuid.New()
	itemService := mocks.NewMockItems(gomock.NewController(t))
	itemService.EXPECT().SetFeedback(gomock.Any(), itemID, "like", nil).Return(domain.Feedback{}, &domain.ProjectionPendingError{Revision: deliveredRevision, Cause: errors.New("qdrant unavailable")})
	router := newTestRouter(t, httpapi.Dependencies{Items: itemService})

	response := performRequest(router, http.MethodPut, "/api/v1/items/"+itemID.String()+"/feedback", `{"sentiment":"like","comment":null}`)

	require.Equal(t, http.StatusServiceUnavailable, response.Code, response.Body.String())
	var problem serverhttp.ProjectionPendingProblem
	require.NoError(t, json.NewDecoder(response.Body).Decode(&problem))
	require.Equal(t, serverhttp.UrnTopicStreamProblemProjectionPending, problem.Type)
	require.Equal(t, serverhttp.ProjectionPendingProblemCodeProjectionPending, problem.Code)
	require.Equal(t, serverhttp.True, problem.CanonicalCommitted)
	require.NotEqual(t, uuid.Nil, deliveredRevision)
	require.Equal(t, deliveredRevision, problem.ProjectionRevision)

}

func TestItemMutationsPreserveProjectionPendingProblem(t *testing.T) {
	t.Parallel()
	for _, correction := range []bool{false, true} {
		name := "clear feedback"
		if correction {
			name = "topic correction"
		}
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			itemID, topicID, revision := uuid.New(), uuid.New(), uuid.New()
			failure := &domain.ProjectionPendingError{Revision: revision, Cause: errors.New("private backend detail")}
			service := mocks.NewMockItems(gomock.NewController(t))
			method, path, body := http.MethodDelete, "/api/v1/items/"+itemID.String()+"/feedback", ""
			if correction {
				service.EXPECT().SetTopicCorrection(gomock.Any(), itemID, topicID, domain.TopicMembershipExclude).Return(failure)
				method, path = http.MethodPut, "/api/v1/items/"+itemID.String()+"/topic-corrections"
				body = `{"topicId":"` + topicID.String() + `","membership":"exclude"}`
			} else {
				service.EXPECT().ClearFeedback(gomock.Any(), itemID).Return(failure)
			}
			router := newTestRouter(t, httpapi.Dependencies{Items: service})

			response := performRequest(router, method, path, body)

			require.Equal(t, http.StatusServiceUnavailable, response.Code, response.Body.String())
			require.Equal(t, "application/problem+json", response.Header().Get("Content-Type"))
			var problem serverhttp.ProjectionPendingProblem
			require.NoError(t, json.Unmarshal(response.Body.Bytes(), &problem))
			require.Equal(t, serverhttp.UrnTopicStreamProblemProjectionPending, problem.Type)
			require.Equal(t, serverhttp.ProjectionPendingProblemCodeProjectionPending, problem.Code)
			require.Equal(t, serverhttp.True, problem.CanonicalCommitted)
			require.Equal(t, revision, problem.ProjectionRevision)
			require.NotContains(t, response.Body.String(), "private backend detail")
		})
	}
}

func TestTopicCorrectionUpdatesCurrentMembershipOverride(t *testing.T) {
	t.Parallel()

	itemID, topicID := uuid.New(), uuid.New()
	itemService := mocks.NewMockItems(gomock.NewController(t))
	itemService.EXPECT().SetTopicCorrection(gomock.Any(), itemID, topicID, domain.TopicMembershipExclude).Return(nil)
	router := newTestRouter(t, httpapi.Dependencies{Items: itemService})

	response := performRequest(router, http.MethodPut, "/api/v1/items/"+itemID.String()+"/topic-corrections",
		`{"topicId":"`+topicID.String()+`","membership":"exclude"}`)

	require.Equal(t, http.StatusNoContent, response.Code, response.Body.String())

}

func TestItemImageRouteDownloadsOnlyAssociatedImage(t *testing.T) {
	t.Parallel()

	itemID, imageID := uuid.New(), uuid.New()
	itemService := mocks.NewMockItems(gomock.NewController(t))
	itemService.EXPECT().DownloadImage(gomock.Any(), itemID, imageID).Return(items.ImageDownload{ContentType: "image/png", Bytes: []byte("png bytes")}, nil)
	router := newTestRouter(t, httpapi.Dependencies{Items: itemService})
	response := performRequest(router, http.MethodGet, "/api/v1/items/"+itemID.String()+"/images/"+imageID.String(), "")

	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	require.Equal(t, "image/png", response.Header().Get("Content-Type"))
	require.Equal(t, "nosniff", response.Header().Get("X-Content-Type-Options"))
	require.Equal(t, "png bytes", response.Body.String())
}
