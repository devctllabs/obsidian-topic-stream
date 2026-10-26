package items_test

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/ethernity/obsidian-topic-stream/internal/domain"
	"github.com/ethernity/obsidian-topic-stream/internal/service/items"
	"github.com/ethernity/obsidian-topic-stream/internal/service/items/mocks"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

type projectorFunc func(context.Context, domain.ItemProjectionCommit) error

// DeliverItemProjection delegates to the callback selected by the test.
func (f projectorFunc) DeliverItemProjection(ctx context.Context, commit domain.ItemProjectionCommit) error {
	return f(ctx, commit)
}

func successfulProjector() projectorFunc {
	return func(context.Context, domain.ItemProjectionCommit) error { return nil }
}

type calibrationNotifierSpy struct{ topicIDs []uuid.UUID }

// NotifyTopicChanged records a Topic calibration notification for assertions.
func (s *calibrationNotifierSpy) NotifyTopicChanged(topicID uuid.UUID) {
	s.topicIDs = append(s.topicIDs, topicID)
}

type noopCalibrationNotifier struct{}

// NotifyTopicChanged ignores calibration notifications for tests that do not observe them.
func (noopCalibrationNotifier) NotifyTopicChanged(uuid.UUID) {}

func TestSetFeedbackReportsPendingProjectionAfterCanonicalCommit(t *testing.T) {
	t.Parallel()

	workspaceID, itemID := uuid.New(), uuid.New()
	repository := mocks.NewMockRepository(gomock.NewController(t))
	deliveryFailure := errors.New("qdrant unavailable")
	projected := false
	repositoryRevision := uuid.New()
	now := time.Now().UTC()
	feedback := domain.Feedback{Sentiment: "like", CreatedAt: now, UpdatedAt: now}
	repository.EXPECT().SetFeedback(gomock.Any(), workspaceID, itemID, feedback).Return(domain.FeedbackCommit{Feedback: feedback, ItemProjectionCommit: domain.ItemProjectionCommit{WorkspaceID: workspaceID, ItemID: itemID, ProjectionRevision: repositoryRevision}}, true, nil)
	service := newService(t, items.Config{
		WorkspaceID: workspaceID, Repository: repository, ImageClient: imageClientFunc(func(*http.Request) (*http.Response, error) {
			return nil, errors.New("unused")
		}),
		Projector: projectorFunc(func(_ context.Context, commit domain.ItemProjectionCommit) error {
			projected = true
			require.Equal(t, workspaceID, commit.WorkspaceID)
			require.Equal(t, itemID, commit.ItemID)
			require.Equal(t, repositoryRevision, commit.ProjectionRevision)
			return deliveryFailure
		}),
		Now: func() time.Time { return now },
	})

	saved, err := service.SetFeedback(t.Context(), itemID, "like", nil)

	var pending *domain.ProjectionPendingError
	require.ErrorAs(t, err, &pending)
	require.Equal(t, repositoryRevision, pending.Revision)
	require.ErrorIs(t, err, deliveryFailure)
	require.True(t, projected)
	require.Equal(t, feedback, saved)
}

func TestFeedbackCollectsViolationsBeforePersistence(t *testing.T) {
	t.Parallel()
	service := newService(t, items.Config{WorkspaceID: uuid.New(), Repository: mocks.NewMockRepository(gomock.NewController(t)),
		Projector: successfulProjector(), ImageClient: imageClientFunc(func(*http.Request) (*http.Response, error) { return nil, errors.New("unused") }), Now: time.Now})
	comment := strings.Repeat("x", 2001)
	_, err := service.SetFeedback(t.Context(), uuid.New(), "unknown", &comment)
	var validation *domain.ValidationError
	require.ErrorAs(t, err, &validation)
	require.Len(t, validation.Violations, 2)
	require.Equal(t, "sentiment", validation.Violations[0].Field)
	require.Equal(t, "comment", validation.Violations[1].Field)
}

func TestClearFeedbackAndTopicCorrectionDeliverCurrentProjection(t *testing.T) {
	t.Parallel()

	workspaceID, itemID, topicID := uuid.New(), uuid.New(), uuid.New()
	repository := mocks.NewMockRepository(gomock.NewController(t))
	commit := domain.ItemProjectionCommit{WorkspaceID: workspaceID, ItemID: itemID, ProjectionRevision: uuid.New()}
	repository.EXPECT().ClearFeedback(gomock.Any(), workspaceID, itemID, gomock.Any()).Return(commit, true, nil)
	repository.EXPECT().SetTopicCorrection(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, correction domain.TopicCorrection) (domain.ItemProjectionCommit, bool, error) {
		require.Equal(t, workspaceID, correction.WorkspaceID)
		require.Equal(t, itemID, correction.ItemID)
		require.Equal(t, topicID, correction.TopicID)
		require.Equal(t, domain.TopicMembershipExclude, correction.Membership)
		return commit, true, nil
	})
	deliveries := make([]uuid.UUID, 0, 2)
	calibration := &calibrationNotifierSpy{}
	service := newService(t, items.Config{
		WorkspaceID: workspaceID, Repository: repository,
		Calibration: calibration,
		Projector: projectorFunc(func(_ context.Context, commit domain.ItemProjectionCommit) error {
			require.Equal(t, workspaceID, commit.WorkspaceID)
			deliveries = append(deliveries, commit.ItemID)
			return nil
		}),
		ImageClient: imageClientFunc(func(*http.Request) (*http.Response, error) {
			return nil, errors.New("unused")
		}),
		Now: time.Now,
	})

	require.NoError(t, service.ClearFeedback(t.Context(), itemID))
	require.NoError(t, service.SetTopicCorrection(t.Context(), itemID, topicID, domain.TopicMembershipExclude))

	require.Equal(t, []uuid.UUID{itemID, itemID}, deliveries)
	require.Equal(t, []uuid.UUID{topicID}, calibration.topicIDs)
}

type imageClientFunc func(*http.Request) (*http.Response, error)

// Do delegates HTTP execution to the callback selected by the test.
func (f imageClientFunc) Do(request *http.Request) (*http.Response, error) {
	return f(request)
}

func TestExtractImagesReturnsStableSafeMarkdownImageDescriptors(t *testing.T) {
	t.Parallel()

	item := domain.Item{
		ID: uuid.New(),
		ContentMarkdown: "![Diagram](https://cdn.example.com/diagram.png)\n" +
			"![](http://images.example.com/no-alt.webp)\n" +
			"![Unsafe](data:image/png;base64,AAAA)\n" +
			"[ordinary link](https://example.com/not-an-image.png)",
	}

	first := items.ExtractImages(item)
	second := items.ExtractImages(item)

	require.Len(t, first, 2)
	require.Equal(t, first, second)
	require.Equal(t, "https://cdn.example.com/diagram.png", first[0].URL)
	require.Equal(t, "Diagram", *first[0].AltText)
	require.Nil(t, first[1].AltText)
}

func TestDownloadImageOnlyFetchesAssociatedImageAndValidatesPayload(t *testing.T) {
	t.Parallel()

	workspaceID, itemID := uuid.New(), uuid.New()
	item := domain.Item{
		ID: itemID, WorkspaceID: workspaceID,
		ContentMarkdown: "![Diagram](https://cdn.example.com/diagram.png)",
	}
	images := items.ExtractImages(item)
	client := imageClientFunc(func(request *http.Request) (*http.Response, error) {
		require.Equal(t, images[0].URL, request.URL.String())
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"image/png"}},
			Body:       io.NopCloser(strings.NewReader("png bytes")),
		}, nil
	})
	service := newService(t, items.Config{
		WorkspaceID: workspaceID, Repository: itemRepository(t, item),
		Projector: successfulProjector(), ImageClient: client, Now: time.Now,
	})

	download, err := service.DownloadImage(context.Background(), itemID, images[0].ID)

	require.NoError(t, err)
	require.Equal(t, "image/png", download.ContentType)
	require.Equal(t, []byte("png bytes"), download.Bytes)

	_, err = service.DownloadImage(context.Background(), itemID, uuid.New())
	require.ErrorIs(t, err, items.ErrImageNotFound)
}

func TestDownloadImageRejectsNonImagePayload(t *testing.T) {
	t.Parallel()

	workspaceID, itemID := uuid.New(), uuid.New()
	item := domain.Item{ID: itemID, WorkspaceID: workspaceID, ContentMarkdown: "![](https://example.com/image)"}
	image := items.ExtractImages(item)[0]
	service := newService(t, items.Config{
		WorkspaceID: workspaceID, Repository: itemRepository(t, item), Now: time.Now,
		Projector: successfulProjector(),
		ImageClient: imageClientFunc(func(*http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"text/html"}},
				Body: io.NopCloser(strings.NewReader("not an image")),
			}, nil
		}),
	})

	_, err := service.DownloadImage(context.Background(), itemID, image.ID)

	require.ErrorIs(t, err, items.ErrInvalidImage)
}

func newService(t *testing.T, config items.Config) *items.Service {
	t.Helper()
	if config.Calibration == nil {
		config.Calibration = noopCalibrationNotifier{}
	}
	service, err := items.New(config)
	require.NoError(t, err)
	return service
}

func itemRepository(t *testing.T, item domain.Item) *mocks.MockRepository {
	t.Helper()
	repository := mocks.NewMockRepository(gomock.NewController(t))
	repository.EXPECT().GetItem(gomock.Any(), item.WorkspaceID, item.ID).Return(item, true, nil).AnyTimes()
	return repository
}
