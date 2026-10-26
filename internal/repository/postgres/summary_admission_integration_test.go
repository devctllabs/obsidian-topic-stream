//go:build integration

package postgres_test

import (
	"sync"
	"testing"
	"time"

	"github.com/ethernity/obsidian-topic-stream/internal/domain"
	"github.com/ethernity/obsidian-topic-stream/internal/repository/postgres"
	"github.com/ethernity/obsidian-topic-stream/internal/service/jobs"
	"github.com/ethernity/obsidian-topic-stream/internal/service/summaries"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"
)

func TestSummaryAdmissionCoalescesConcurrentRequestsAndFreezesInput(t *testing.T) {
	f := newFeedFixture(t)
	input := domain.SummaryInputForItem(domain.Item{ID: uuid.New(), WorkspaceID: f.workspaceID, Kind: domain.ItemKindArticle, Title: "Captured", ContentMarkdown: "Owned article body", ContentOrigin: domain.ContentOriginArticleBody})
	const callers = 16
	requests := make([]domain.SummaryAdmission, callers)
	for i := range requests {
		requests[i] = summaryAdmissionRequest(t, input)
	}
	start := make(chan struct{})
	results := make([]domain.SummaryAdmissionResult, callers)
	failures := make([]error, callers)
	var wg sync.WaitGroup
	for i := range requests {
		wg.Go(func() { <-start; results[i], failures[i] = f.store.Summaries().AdmitSummary(t.Context(), requests[i]) })
	}
	close(start)
	wg.Wait()
	created := 0
	for i, result := range results {
		require.NoError(t, failures[i])
		require.Equal(t, results[0].Job, result.Job)
		if result.Created {
			created++
		}
	}
	require.Equal(t, 1, created)
	fresh, err := postgres.Open(t.Context(), f.dsn)
	require.NoError(t, err)
	defer fresh.Close()
	restarted, err := fresh.Summaries().AdmitSummary(t.Context(), summaryAdmissionRequest(t, input))
	require.NoError(t, err)
	require.False(t, restarted.Created)
	require.Equal(t, results[0].Job.ID, restarted.Job.ID)
	saved, found, err := f.store.Summaries().GetSummaryInput(t.Context(), input.SummaryInputKey)
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, input, saved)
	binding, err := f.store.Summaries().ReadSummaryBinding(t.Context(), domain.SummaryBindingQuery{SummaryInputKey: input.SummaryInputKey})
	require.NoError(t, err)
	require.NotNil(t, binding.Job)
	require.Equal(t, results[0].Job.ID, binding.Job.ID)
	require.Nil(t, binding.Result)
	conn, err := pgx.Connect(t.Context(), f.dsn)
	require.NoError(t, err)
	defer func() { require.NoError(t, conn.Close(t.Context())) }()
	for _, status := range []domain.JobStatus{domain.JobStatusRunning, domain.JobStatusRetryWait, domain.JobStatusDead, domain.JobStatusCancelled} {
		_, err = conn.Exec(t.Context(), `UPDATE jobs SET status=$2,cancellation_requested=true WHERE id=$1`, results[0].Job.ID, status)
		require.NoError(t, err)
		replay, err := f.store.Summaries().AdmitSummary(t.Context(), summaryAdmissionRequest(t, input))
		require.NoError(t, err)
		require.False(t, replay.Created)
		require.Equal(t, results[0].Job.ID, replay.Job.ID)
	}
}

func TestSummaryAdmissionSeparatesInputVersionsItemsAndWorkspaces(t *testing.T) {
	f := newFeedFixture(t)
	itemID := uuid.New()
	item := domain.Item{ID: itemID, WorkspaceID: f.workspaceID, Kind: domain.ItemKindArticle, Title: "Article", ContentMarkdown: "First body", ContentOrigin: domain.ContentOriginArticleBody}
	first, err := f.store.Summaries().AdmitSummary(t.Context(), summaryAdmissionRequest(t, domain.SummaryInputForItem(item)))
	require.NoError(t, err)
	item.ContentMarkdown = "Changed body"
	second, err := f.store.Summaries().AdmitSummary(t.Context(), summaryAdmissionRequest(t, domain.SummaryInputForItem(item)))
	require.NoError(t, err)
	require.True(t, second.Created)
	require.NotEqual(t, first.Job.ID, second.Job.ID)
	item.ID = uuid.New()
	third, err := f.store.Summaries().AdmitSummary(t.Context(), summaryAdmissionRequest(t, domain.SummaryInputForItem(item)))
	require.NoError(t, err)
	require.True(t, third.Created)
	require.NotEqual(t, second.Job.ID, third.Job.ID)
	item.ID = itemID
	item.WorkspaceID = uuid.New()
	require.NoError(t, f.store.EnsureWorkspace(t.Context(), item.WorkspaceID))
	fourth, err := f.store.Summaries().AdmitSummary(t.Context(), summaryAdmissionRequest(t, domain.SummaryInputForItem(item)))
	require.NoError(t, err)
	require.True(t, fourth.Created)
	require.NotEqual(t, second.Job.ID, fourth.Job.ID)
}

func TestSummaryAdmissionRollsBackCapturedBodyAndJobWhenDigestBindingFails(t *testing.T) {
	f := newFeedFixture(t)
	item := domain.Item{ID: uuid.New(), WorkspaceID: f.workspaceID, Kind: domain.ItemKindArticle, Title: "Captured", ContentMarkdown: "Owned captured article", ContentOrigin: domain.ContentOriginArticleBody}
	definition := domain.DigestDefinition{ID: uuid.New(), WorkspaceID: f.workspaceID, Name: "Saved", Timezone: "UTC", Version: 1, CreatedAt: f.now, UpdatedAt: f.now}
	require.NoError(t, f.store.CreateDigest(t.Context(), definition))
	issueID := uuid.New()
	require.NoError(t, publishDigestFixture(t, f, domain.DigestIssue{ID: issueID, WorkspaceID: f.workspaceID, DigestID: definition.ID, Title: "Saved", Status: domain.DigestIssueComplete, GeneratedAt: f.now, Sections: []domain.DigestSection{{Items: []domain.DigestItem{{Item: item}}}}}))
	request := summaryAdmissionRequest(t, domain.SummaryInputForItem(item))
	request.IssueID = &issueID
	conn, err := pgx.Connect(t.Context(), f.dsn)
	require.NoError(t, err)
	defer func() { require.NoError(t, conn.Close(t.Context())) }()
	_, err = conn.Exec(t.Context(), `ALTER TABLE digest_item_summaries ADD CONSTRAINT fail_binding CHECK(job_id IS NULL)`)
	require.NoError(t, err)
	_, err = f.store.Summaries().AdmitSummary(t.Context(), request)
	require.Error(t, err)
	_, found, err := f.store.Jobs().Get(t.Context(), request.Job.ID)
	require.NoError(t, err)
	require.False(t, found)
	_, found, err = f.store.Summaries().GetSummaryInput(t.Context(), request.Input.SummaryInputKey)
	require.NoError(t, err)
	require.False(t, found)
}

// summaryAdmissionRequest prepares one queued candidate without freezing mutable Summary settings.
func summaryAdmissionRequest(t *testing.T, input domain.SummaryInput) domain.SummaryAdmission {
	t.Helper()
	job, err := jobs.Prepare(jobs.EnqueueRequest{WorkspaceID: input.WorkspaceID,
		Definition: jobs.JobDefinition{Kind: summaries.GenerationJobKind, ArgumentsVersion: 1, Queue: domain.JobQueueGeneration, Retry: jobs.RetryPolicy{MaxAttempts: 3, BaseDelay: 30 * time.Second, MaxDelay: 15 * time.Minute}},
		Arguments:  map[string]any{"itemId": input.ItemID, "sourceVersion": input.SourceVersion}, IdempotencyKey: "summarize-item:" + input.ItemID.String() + ":" + input.SourceVersion}, time.Now().UTC())
	require.NoError(t, err)
	return domain.SummaryAdmission{Input: input, ConfigurationFingerprint: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", Job: job}
}
