//go:build integration

package postgres_test

import (
	"context"
	"testing"
	"time"

	"github.com/ethernity/obsidian-topic-stream/internal/domain"
	"github.com/ethernity/obsidian-topic-stream/internal/service/events"
	"github.com/ethernity/obsidian-topic-stream/internal/service/jobs"
	"github.com/ethernity/obsidian-topic-stream/internal/service/settings"
	"github.com/ethernity/obsidian-topic-stream/internal/service/summaries"
	"github.com/ethernity/obsidian-topic-stream/internal/service/summaries/mocks"
	"github.com/ethernity/obsidian-topic-stream/internal/testutil"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

func TestSummaryPublicationRollsBackResultAndReadyPointersOnCommitFailure(t *testing.T) {
	f, publication := summaryPublicationFixture(t)
	conn, err := pgx.Connect(t.Context(), f.dsn)
	require.NoError(t, err)
	defer func() { require.NoError(t, conn.Close(t.Context())) }()
	_, err = conn.Exec(t.Context(), `CREATE FUNCTION reject_summary_commit() RETURNS trigger LANGUAGE plpgsql AS $body$ BEGIN RAISE EXCEPTION 'commit failure'; RETURN NEW; END; $body$;
 CREATE CONSTRAINT TRIGGER reject_summary_commit AFTER UPDATE ON item_summary_inputs DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION reject_summary_commit()`)
	require.NoError(t, err)
	require.Error(t, f.store.Summaries().PublishSummary(t.Context(), publication))
	_, found, err := f.store.Summaries().GetSummaryResultForJob(t.Context(), f.workspaceID, publication.Annotation.GenerationJobID)
	require.NoError(t, err)
	require.False(t, found)
	binding, err := f.store.Summaries().ReadSummaryBinding(t.Context(), domain.SummaryBindingQuery{SummaryInputKey: publication.Input})
	require.NoError(t, err)
	require.NotNil(t, binding.Job)
	require.Nil(t, binding.Result)
}

func TestSummaryWorkerRecoversCommittedResultAfterLostAcknowledgement(t *testing.T) {
	f := newFeedFixture(t)
	input := domain.SummaryInputForItem(domain.Item{ID: uuid.New(), WorkspaceID: f.workspaceID, Kind: domain.ItemKindArticle, ContentMarkdown: "Captured body", ContentOrigin: domain.ContentOriginArticleBody})
	accepted, err := f.store.Summaries().AdmitSummary(t.Context(), summaryAdmissionRequest(t, input))
	require.NoError(t, err)
	repository := &lostJobAckRepository{JobRepository: f.store.Jobs(), failed: make(chan domain.JobFinish, 1), finished: make(chan domain.JobFinish, 1)}
	worker := jobs.New(repository, time.Now, jobs.Config{PollInterval: time.Millisecond, LeaseDuration: time.Minute, HeartbeatInterval: time.Second})
	configuration := settings.DefaultWorkspaceSettings(time.Now())
	endpoint, model := "https://summary.example/v1", "summary-model"
	configuration.Generation = domain.GenerationSettings{Enabled: true, Endpoint: &endpoint, SummaryModel: &model}
	controller := gomock.NewController(t)
	reader := mocks.NewMockWorkspaceSettings(controller)
	reader.EXPECT().GetWorkspace(gomock.Any()).Return(configuration, nil).Times(1)
	generator := mocks.NewMockGenerator(controller)
	generator.EXPECT().Summarize(gomock.Any(), gomock.Any()).Return("Committed summary", nil).Times(1)
	_, err = summaries.New(summaries.Config{WorkspaceID: f.workspaceID, Repository: f.store.Summaries(), Settings: reader, Generator: generator, Jobs: worker.ForWorkspace(f.workspaceID), Events: events.NewForWorkspace(f.workspaceID, nil), Now: time.Now})
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { done <- worker.Run(ctx) }()
	t.Cleanup(func() { cancel(); require.NoError(t, testutil.Receive(t, done)) })
	first := testutil.Receive(t, repository.failed)
	require.Equal(t, domain.JobStatusSucceeded, first.Status)
	before, found, err := f.store.Summaries().GetSummaryResultForJob(t.Context(), f.workspaceID, accepted.Job.ID)
	require.NoError(t, err)
	require.True(t, found)
	conn, err := pgx.Connect(t.Context(), f.dsn)
	require.NoError(t, err)
	defer func() { require.NoError(t, conn.Close(t.Context())) }()
	_, err = conn.Exec(t.Context(), `UPDATE jobs SET lease_expires_at=clock_timestamp()-interval '1 second' WHERE id=$1`, accepted.Job.ID)
	require.NoError(t, err)
	second := testutil.Receive(t, repository.finished)
	require.Equal(t, domain.JobStatusSucceeded, second.Status)
	after, found, err := f.store.Summaries().GetSummaryResultForJob(t.Context(), f.workspaceID, accepted.Job.ID)
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, before, after)
}
