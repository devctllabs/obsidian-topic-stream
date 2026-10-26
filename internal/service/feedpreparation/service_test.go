package feedpreparation_test

import (
	"context"
	"testing"
	"time"

	"sync/atomic"

	"github.com/ethernity/obsidian-topic-stream/internal/domain"
	"github.com/ethernity/obsidian-topic-stream/internal/service/feedpreparation"
	"github.com/ethernity/obsidian-topic-stream/internal/service/jobs"
	jobmocks "github.com/ethernity/obsidian-topic-stream/internal/service/jobs/mocks"
	"github.com/ethernity/obsidian-topic-stream/internal/testutil"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

type preparationStore struct {
	workspaceID, topicID uuid.UUID
	commits              chan domain.FeedCandidateReplacement
}

// LoadFeedPreparationWork returns a synthetic preparation unit with fixed version fences.
func (s *preparationStore) LoadFeedPreparationWork(_ context.Context, q domain.FeedPreparationQuery) ([]domain.FeedPreparationWork, error) {
	return []domain.FeedPreparationWork{{WorkspaceID: s.workspaceID, TopicID: s.topicID, RequestedVersion: 2, InputVersion: 1}}, nil
}

// ReplaceFeedCandidates records the proposed candidate replacement for assertions.
func (s *preparationStore) ReplaceFeedCandidates(_ context.Context, r domain.FeedCandidateReplacement) error {
	s.commits <- r
	return nil
}

// GetTopic returns the fixture Topic and its lookup result.
func (s *preparationStore) GetTopic(context.Context, uuid.UUID, uuid.UUID) (domain.Topic, bool, error) {
	return domain.Topic{ID: s.topicID, WorkspaceID: s.workspaceID, Enabled: true, Revision: 1, CorrectionEpoch: 1}, true, nil
}

// GetActive returns the fixture's serving generation and lookup result.
func (s *preparationStore) GetActive(context.Context, uuid.UUID) (domain.RetrievalIndex, bool, error) {
	return domain.RetrievalIndex{WorkspaceID: s.workspaceID, ModelID: uuid.New(), Status: domain.RetrievalIndexActive, Generation: 1}, true, nil
}

type preparationSettings struct{}

// GetWorkspace returns the fixture's Workspace Settings snapshot.
func (preparationSettings) GetWorkspace(context.Context) (domain.WorkspaceSettings, error) {
	return domain.WorkspaceSettings{Retrieval: domain.RetrievalSettings{CandidateLimit: 50, RRFK: 60}}, nil
}

type preparer struct{ itemID uuid.UUID }

// PrepareFeed returns synthetic prepared candidates without running retrieval.
func (p preparer) PrepareFeed(_ context.Context, r domain.FeedTopicPreparation) (domain.FeedTopicPrepared, error) {
	return domain.FeedTopicPrepared{Inputs: r.Inputs, Candidates: []domain.FeedCandidate{{ItemID: p.itemID}}}, nil
}

func TestPreparationJobCommitsCandidatesWithActualClaimIdentity(t *testing.T) {
	now := time.Now().UTC()
	workspaceID, topicID, itemID := uuid.New(), uuid.New(), uuid.New()
	repository := jobmocks.NewMockRepository(gomock.NewController(t))
	var queued domain.Job
	repository.EXPECT().Enqueue(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, job domain.Job) (domain.Job, bool, error) { queued = job; return job, true, nil })
	var claimed atomic.Bool
	repository.EXPECT().Claim(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, request domain.JobClaimRequest) (domain.JobClaim, error) {
		if request.Queue == domain.JobQueueFeedPreparation && claimed.CompareAndSwap(false, true) {
			job := queued
			job.Attempt = 1
			return domain.JobClaim{Job: job}, nil
		}
		return domain.JobClaim{}, nil
	}).AnyTimes()
	repository.EXPECT().Heartbeat(gomock.Any(), gomock.Any()).Return(false, true, nil).AnyTimes()
	repository.EXPECT().Finish(gomock.Any(), gomock.Any()).Return(true, nil).AnyTimes()
	config := jobs.DefaultConfig()
	config.Owner = "preparation-test"
	config.PollInterval = time.Millisecond
	queue := jobs.New(repository, func() time.Time { return now }, config)
	store := &preparationStore{workspaceID: workspaceID, topicID: topicID, commits: make(chan domain.FeedCandidateReplacement, 1)}
	feedpreparation.New(feedpreparation.Config{WorkspaceID: workspaceID, Repository: store, Settings: preparationSettings{}, Preparer: preparer{itemID: itemID}, Jobs: queue.ForWorkspace(workspaceID), Now: func() time.Time { return now }})
	job, err := queue.Enqueue(t.Context(), jobs.EnqueueRequest{WorkspaceID: workspaceID, Definition: feedpreparation.Definition, Arguments: struct{}{}, IdempotencyKey: "prepare-feed"})
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { done <- queue.Run(ctx) }()
	t.Cleanup(func() { cancel(); require.NoError(t, testutil.Receive(t, done)) })
	select {
	case commit := <-store.commits:
		require.Equal(t, job.ID, commit.JobID)
		require.Equal(t, workspaceID, commit.Work.WorkspaceID)
		require.Contains(t, commit.LeaseOwner, "preparation-test/feed-preparation/")
		require.Equal(t, itemID, commit.Candidates[0].ItemID)
	case <-time.After(time.Second):
		t.Fatal("preparation job never committed candidates")
	}
}
