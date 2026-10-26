package digests_test

import (
	"context"
	"testing"
	"time"

	"github.com/ethernity/obsidian-topic-stream/internal/domain"
	"github.com/ethernity/obsidian-topic-stream/internal/service/digests"
	"github.com/ethernity/obsidian-topic-stream/internal/service/digests/mocks"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

func TestManualBuildResolvesDurableAdmissionBeforeReadingDefinition(t *testing.T) {
	t.Parallel()
	workspaceID, digestID, identity, jobID := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	now := time.Now().UTC()
	repository := mocks.NewMockRepository(gomock.NewController(t))
	// A replay may outlive its Definition. No live-definition lookup or separate enqueue is allowed.
	repository.EXPECT().AdmitDigestBuild(gomock.Any(), gomock.Any()).DoAndReturn(
		func(_ context.Context, request domain.DigestBuildRequest) (domain.DigestBuildAdmission, error) {
			require.Equal(t, workspaceID, request.WorkspaceID)
			require.Equal(t, digestID, request.DigestID)
			require.Equal(t, identity, request.RequestID)
			require.Equal(t, "generate-digest:"+digestID.String(), *request.Job.IdempotencyKey)
			require.Equal(t, domain.JobQueueGeneration, request.Job.Queue)
			require.Equal(t, 3, request.Job.MaxAttempts)
			require.JSONEq(t, `{"digestId":"`+digestID.String()+`","scheduledFor":null}`, string(request.Job.Arguments))
			return domain.DigestBuildAdmission{Job: domain.Job{ID: jobID, Status: domain.JobStatusSucceeded}}, nil
		})
	service := newDigestService(t, digests.Config{WorkspaceID: workspaceID, Repository: repository,
		Topics: mocks.NewMockTopics(gomock.NewController(t)), Items: mocks.NewMockItems(gomock.NewController(t)),
		Ranker: &digestRankerStub{}, Jobs: newJobQueue(t), Events: mocks.NewMockDigestNotifier(gomock.NewController(t)),
		Now: func() time.Time { return now },
	})
	job, err := service.Generate(t.Context(), digestID, identity)
	require.NoError(t, err)
	require.Equal(t, jobID, job.ID)
}

func TestManualBuildAnnouncesOnlyNewCommittedJobs(t *testing.T) {
	t.Parallel()
	for _, created := range []bool{true, false} {
		t.Run(map[bool]string{true: "new", false: "coalesced"}[created], func(t *testing.T) {
			workspaceID, digestID := uuid.New(), uuid.New()
			repository := mocks.NewMockRepository(gomock.NewController(t))
			events := mocks.NewMockDigestNotifier(gomock.NewController(t))
			job := domain.Job{ID: uuid.New(), WorkspaceID: workspaceID}
			committed := false
			repository.EXPECT().AdmitDigestBuild(gomock.Any(), gomock.Any()).DoAndReturn(func(context.Context, domain.DigestBuildRequest) (domain.DigestBuildAdmission, error) {
				committed = true
				return domain.DigestBuildAdmission{Job: job, Created: created}, nil
			})
			if created {
				events.EXPECT().NotifyJobChanged(workspaceID, job.ID).Do(func(uuid.UUID, uuid.UUID) { require.True(t, committed) })
			}
			service := newDigestService(t, digests.Config{WorkspaceID: workspaceID, Repository: repository,
				Topics: mocks.NewMockTopics(gomock.NewController(t)), Items: mocks.NewMockItems(gomock.NewController(t)),
				Ranker: &digestRankerStub{}, Jobs: newJobQueue(t), Events: events, Now: time.Now,
			})
			actual, err := service.Generate(t.Context(), digestID, uuid.New())
			require.NoError(t, err)
			require.Equal(t, job, actual)
		})
	}
}

func TestDeleteAnnouncesCommittedJobCancellations(t *testing.T) {
	t.Parallel()
	workspaceID, digestID, jobID := uuid.New(), uuid.New(), uuid.New()
	repository := mocks.NewMockRepository(gomock.NewController(t))
	events := mocks.NewMockDigestNotifier(gomock.NewController(t))
	repository.EXPECT().GetDigest(gomock.Any(), workspaceID, digestID).Return(domain.DigestDefinition{ID: digestID, Version: 1}, true, nil)
	committed := false
	repository.EXPECT().DeleteDigest(gomock.Any(), gomock.Any()).DoAndReturn(func(context.Context, domain.DigestDelete) (domain.DigestDeletion, error) {
		committed = true
		return domain.DigestDeletion{Deleted: true, JobIDs: []uuid.UUID{jobID}}, nil
	})
	events.EXPECT().NotifyJobChanged(workspaceID, jobID).Do(func(uuid.UUID, uuid.UUID) { require.True(t, committed) })
	service := newDigestService(t, digests.Config{WorkspaceID: workspaceID, Repository: repository,
		Topics: mocks.NewMockTopics(gomock.NewController(t)), Items: mocks.NewMockItems(gomock.NewController(t)),
		Ranker: &digestRankerStub{}, Jobs: newJobQueue(t), Events: events, Now: time.Now})
	require.NoError(t, service.Delete(t.Context(), digestID, 1))
}
