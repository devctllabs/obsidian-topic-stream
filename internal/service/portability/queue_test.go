package portability_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/ethernity/obsidian-topic-stream/internal/domain"
	"github.com/ethernity/obsidian-topic-stream/internal/service/jobs"
	jobmocks "github.com/ethernity/obsidian-topic-stream/internal/service/jobs/mocks"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

type jobQueueFixture struct {
	*jobmocks.MockQueue
	handlers map[string]jobs.Handler
	requests []jobs.EnqueueRequest
}

func newJobQueue(t *testing.T) *jobQueueFixture {
	t.Helper()
	fixture := &jobQueueFixture{MockQueue: jobmocks.NewMockQueue(gomock.NewController(t)), handlers: make(map[string]jobs.Handler)}
	fixture.EXPECT().RegisterDefinition(gomock.Any(), gomock.Any()).Do(func(def jobs.JobDefinition, handler jobs.Handler) { fixture.handlers[def.Kind] = handler }).AnyTimes()
	fixture.EXPECT().Enqueue(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, request jobs.EnqueueRequest) (domain.Job, error) {
		fixture.requests = append(fixture.requests, request)
		return domain.Job{ID: uuid.New(), Kind: request.Definition.Kind, WorkspaceID: request.WorkspaceID}, nil
	}).AnyTimes()
	return fixture
}
func (q *jobQueueFixture) run(t *testing.T, index int) error {
	t.Helper()
	request := q.requests[index]
	payload, err := json.Marshal(request.Arguments)
	require.NoError(t, err)
	return q.handlers[request.Definition.Kind](t.Context(), payload)
}
