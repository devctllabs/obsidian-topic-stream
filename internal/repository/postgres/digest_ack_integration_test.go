//go:build integration

package postgres_test

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ethernity/obsidian-topic-stream/internal/domain"
	"github.com/ethernity/obsidian-topic-stream/internal/repository/postgres"
	"github.com/ethernity/obsidian-topic-stream/internal/service/digests"
	digestmocks "github.com/ethernity/obsidian-topic-stream/internal/service/digests/mocks"
	"github.com/ethernity/obsidian-topic-stream/internal/service/jobs"
	"github.com/ethernity/obsidian-topic-stream/internal/service/ranking"
	"github.com/ethernity/obsidian-topic-stream/internal/testutil"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

// lostJobAckRepository injects one failed Job acknowledgement after a generation result has committed.
type lostJobAckRepository struct {
	*postgres.JobRepository
	first    atomic.Bool
	failed   chan domain.JobFinish
	finished chan domain.JobFinish
}

// Finish drops the first acknowledgement and delegates subsequent finalizations to real PostgreSQL.
func (r *lostJobAckRepository) Finish(ctx context.Context, finish domain.JobFinish) (bool, error) {
	if r.first.CompareAndSwap(false, true) {
		r.failed <- finish
		return false, errors.New("injected acknowledgement loss")
	}
	changed, err := r.JobRepository.Finish(ctx, finish)
	if changed {
		r.finished <- finish
	}
	return changed, err
}

func TestDigestWorkerRecoversLostAcknowledgementWithoutRerankingOrRepublishing(t *testing.T) {
	f := newFeedFixture(t)
	itemID, topicID := f.item(t), uuid.New()
	definition := domain.DigestDefinition{ID: uuid.New(), WorkspaceID: f.workspaceID, Name: "Recover", Timezone: "UTC", Version: 1, CreatedAt: f.now, UpdatedAt: f.now, Topics: []domain.DigestTopic{{TopicID: topicID, ItemLimit: 1}}}
	require.NoError(t, f.store.CreateDigest(t.Context(), definition))
	repository := &lostJobAckRepository{JobRepository: f.store.Jobs(), failed: make(chan domain.JobFinish, 1), finished: make(chan domain.JobFinish, 1)}
	worker := jobs.New(repository, time.Now, jobs.Config{PollInterval: time.Millisecond, LeaseDuration: time.Minute, HeartbeatInterval: time.Second})
	topics := digestmocks.NewMockTopics(gomock.NewController(t))
	topics.EXPECT().GetTopic(gomock.Any(), f.workspaceID, topicID).Return(domain.Topic{ID: topicID, WorkspaceID: f.workspaceID, Enabled: true, Name: "Topic", Revision: 1}, true, nil)
	settings := digestmocks.NewMockWorkspaceSettings(gomock.NewController(t))
	settings.EXPECT().GetWorkspace(gomock.Any()).Return(domain.WorkspaceSettings{Retrieval: domain.RetrievalSettings{CandidateLimit: 10}}, nil)
	ranker := digestmocks.NewMockTopicRanker(gomock.NewController(t))
	ranker.EXPECT().RankTopic(gomock.Any(), gomock.Any()).Return(ranking.TopicResult{Candidates: []ranking.Candidate{{ItemID: itemID.String()}}, Inputs: ranking.RankingInputs{PipelineVersion: ranking.PipelineVersion}}, nil)
	events := digestmocks.NewMockDigestNotifier(gomock.NewController(t))
	events.EXPECT().NotifyJobChanged(f.workspaceID, gomock.Any())
	events.EXPECT().NotifyDigestReady(f.workspaceID, gomock.Any())
	service, err := digests.New(digests.Config{WorkspaceID: f.workspaceID, Repository: f.store, Topics: topics, Settings: settings, Summaries: digestmocks.NewMockItemSummaries(gomock.NewController(t)), Ranker: ranker, Items: f.store, Jobs: worker, Events: events, Now: time.Now})
	require.NoError(t, err)
	job, err := service.Generate(t.Context(), definition.ID, uuid.New())
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { done <- worker.Run(ctx) }()
	t.Cleanup(func() { cancel(); require.NoError(t, testutil.Receive(t, done)) })
	first := testutil.Receive(t, repository.failed)
	require.Equal(t, domain.JobStatusSucceeded, first.Status)
	before, found, err := f.store.GetDigestIssueForJob(t.Context(), f.workspaceID, job.ID)
	require.NoError(t, err)
	require.True(t, found)
	baseline, found, err := f.store.GetDigest(t.Context(), f.workspaceID, definition.ID)
	require.NoError(t, err)
	require.True(t, found)
	conn, err := pgx.Connect(t.Context(), f.dsn)
	require.NoError(t, err)
	defer func() { require.NoError(t, conn.Close(t.Context())) }()
	_, err = conn.Exec(t.Context(), `UPDATE jobs SET lease_expires_at=clock_timestamp()-interval '1 second' WHERE id=$1`, job.ID)
	require.NoError(t, err)
	second := testutil.Receive(t, repository.finished)
	require.Equal(t, domain.JobStatusSucceeded, second.Status)
	after, found, err := f.store.GetDigestIssueForJob(t.Context(), f.workspaceID, job.ID)
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, before, after)
	current, found, err := f.store.GetDigest(t.Context(), f.workspaceID, definition.ID)
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, baseline.LastSuccessfulAt, current.LastSuccessfulAt)
	stored, found, err := f.store.Jobs().Get(t.Context(), job.ID)
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, 2, stored.Attempt)
	consumed, err := f.store.FindConsumedDigestItemIDs(t.Context(), f.workspaceID, definition.ID, []uuid.UUID{itemID})
	require.NoError(t, err)
	require.Equal(t, []uuid.UUID{itemID}, consumed)
}
