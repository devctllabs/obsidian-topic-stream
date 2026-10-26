package indexing_test

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/ethernity/obsidian-topic-stream/internal/domain"
	"github.com/ethernity/obsidian-topic-stream/internal/service/indexing"
	"github.com/ethernity/obsidian-topic-stream/internal/service/jobs"
	jobmocks "github.com/ethernity/obsidian-topic-stream/internal/service/jobs/mocks"
	"github.com/ethernity/obsidian-topic-stream/internal/testutil"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

func TestIndexItemForwardsCurrentJobAttemptToProjection(t *testing.T) {
	queue := newJobQueue(t)
	processor := &processorSpy{}
	workspaceID := uuid.New()
	service := newIndexingService(t, indexing.Config{WorkspaceID: workspaceID, Pending: pendingRepositoryStub{}, Jobs: queue, Processor: processor, Events: &feedInvalidatorSpy{}})
	job, err := service.Enqueue(t.Context(), domain.ItemProjectionCommit{WorkspaceID: workspaceID, ItemID: uuid.New(), ProjectionRevision: uuid.New()})
	require.NoError(t, err)
	request := queue.requests[0]
	job.Queue, job.ArgumentsVersion = request.Definition.Queue, request.Definition.ArgumentsVersion
	job.Arguments, err = json.Marshal(request.Arguments)
	require.NoError(t, err)
	job.Status, job.Attempt, job.MaxAttempts = domain.JobStatusRunning, 2, 3
	repository := jobmocks.NewMockRepository(gomock.NewController(t))
	var once sync.Once
	repository.EXPECT().Claim(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, claim domain.JobClaimRequest) (domain.JobClaim, error) {
		var result domain.JobClaim
		if claim.Queue == job.Queue {
			once.Do(func() { result.Job = job })
		}
		return result, nil
	}).AnyTimes()
	repository.EXPECT().Heartbeat(gomock.Any(), gomock.Any()).Return(false, true, nil).AnyTimes()
	finished := make(chan domain.JobFinish, 1)
	repository.EXPECT().Finish(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, finish domain.JobFinish) (bool, error) { finished <- finish; return true, nil })
	worker := jobs.New(repository, time.Now, jobs.Config{PollInterval: time.Millisecond, HeartbeatInterval: time.Second})
	worker.RegisterDefinition(request.Definition, queue.handlers[job.Kind])
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { done <- worker.Run(ctx) }()
	t.Cleanup(func() { cancel(); require.NoError(t, testutil.Receive(t, done)) })
	require.Equal(t, domain.JobStatusSucceeded, testutil.Receive(t, finished).Status)
	execution := processor.snapshot().Execution
	require.Equal(t, job.ID, execution.JobID)
	require.Equal(t, 2, execution.Attempt)
	require.NotEmpty(t, execution.LeaseOwner)
}
