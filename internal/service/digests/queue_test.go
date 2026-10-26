package digests_test

import (
	"context"
	"testing"
	"time"

	"github.com/ethernity/obsidian-topic-stream/internal/domain"
	"github.com/ethernity/obsidian-topic-stream/internal/service/jobs"
	jobmocks "github.com/ethernity/obsidian-topic-stream/internal/service/jobs/mocks"
	"github.com/google/uuid"
	"go.uber.org/mock/gomock"
)

type jobQueueFixture struct {
	*jobmocks.MockQueue
	handlers map[string]jobs.Handler
	requests []jobs.EnqueueRequest
	jobIDs   []uuid.UUID
}

func newJobQueue(t *testing.T) *jobQueueFixture {
	t.Helper()
	fixture := &jobQueueFixture{MockQueue: jobmocks.NewMockQueue(gomock.NewController(t)), handlers: make(map[string]jobs.Handler)}
	fixture.EXPECT().RegisterDefinition(gomock.Any(), gomock.Any()).Do(func(def jobs.JobDefinition, handler jobs.Handler) { fixture.handlers[def.Kind] = handler }).AnyTimes()
	fixture.EXPECT().Enqueue(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, request jobs.EnqueueRequest) (domain.Job, error) {
		fixture.requests = append(fixture.requests, request)
		id := uuid.New()
		fixture.jobIDs = append(fixture.jobIDs, id)
		return domain.Job{ID: id, Kind: request.Definition.Kind, WorkspaceID: request.WorkspaceID}, nil
	}).AnyTimes()
	return fixture
}
func (q *jobQueueFixture) run(t *testing.T, index int) error {
	t.Helper()
	_, err := executeDigestAttempt(t, q, index, 1)
	return err
}

// seedDigest submits a worker fixture independently of HTTP admission.
func (q *jobQueueFixture) seedDigest(t *testing.T, workspaceID, digestID uuid.UUID) error {
	t.Helper()
	_, err := q.Enqueue(t.Context(), jobs.EnqueueRequest{WorkspaceID: workspaceID,
		Definition: jobs.JobDefinition{Kind: "generate-digest", ArgumentsVersion: 1, Queue: domain.JobQueueGeneration,
			Retry: jobs.RetryPolicy{MaxAttempts: 3, BaseDelay: 30 * time.Second, MaxDelay: 15 * time.Minute}},
		Arguments: map[string]any{"digestId": digestID, "scheduledFor": nil},
	})
	return err
}
