//go:build integration

package postgres_test

import (
	"testing"
	"time"

	"github.com/ethernity/obsidian-topic-stream/internal/domain"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"
)

func TestDigestSummaryMarkersAttachLateResultWithoutChangingSnapshot(t *testing.T) {
	f, publication := summaryPublicationFixture(t)
	input, found, err := f.store.Summaries().GetSummaryInput(t.Context(), publication.Input)
	require.NoError(t, err)
	require.True(t, found)
	item := domain.Item{ID: input.ItemID, WorkspaceID: input.WorkspaceID, Kind: domain.ItemKindArticle, Title: input.Title, ContentMarkdown: input.Markdown, ContentOrigin: input.Origin}
	issueID := publishSummaryDigest(t, f, item)
	query := domain.SummaryBindingQuery{SummaryInputKey: input.SummaryInputKey, IssueID: &issueID}
	binding, err := f.store.Summaries().ReadSummaryBinding(t.Context(), query)
	require.NoError(t, err)
	require.NotNil(t, binding.Job)
	require.Equal(t, publication.Annotation.GenerationJobID, binding.Job.ID)
	require.Nil(t, binding.Result)
	before, found, err := f.store.GetDigestIssue(t.Context(), f.workspaceID, issueID)
	require.NoError(t, err)
	require.True(t, found)
	require.NoError(t, f.store.Summaries().PublishSummary(t.Context(), publication))
	binding, err = f.store.Summaries().ReadSummaryBinding(t.Context(), query)
	require.NoError(t, err)
	require.NotNil(t, binding.Result)
	require.Equal(t, publication.Annotation.ID, binding.Result.ID)
	batch, err := f.store.Summaries().ReadDigestSummaryBindings(t.Context(), f.workspaceID, issueID)
	require.NoError(t, err)
	require.Equal(t, binding.Result, batch[input.SummaryInputKey].Result)
	after, found, err := f.store.GetDigestIssue(t.Context(), f.workspaceID, issueID)
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, before, after)
	// A newly saved context sees a result already committed before its publication.
	secondID := publishSummaryDigest(t, f, item)
	query.IssueID = &secondID
	binding, err = f.store.Summaries().ReadSummaryBinding(t.Context(), query)
	require.NoError(t, err)
	require.NotNil(t, binding.Result)
	require.Equal(t, publication.Annotation.ID, binding.Result.ID)
	// Different saved text cannot receive this source version's result.
	item.ContentMarkdown = "A newer body"
	thirdID := publishSummaryDigest(t, f, item)
	query.IssueID = &thirdID
	binding, err = f.store.Summaries().ReadSummaryBinding(t.Context(), query)
	require.NoError(t, err)
	require.Nil(t, binding.Result)
}

func TestDigestReadySummaryIsNeverReplacedByNewConfiguration(t *testing.T) {
	f, first := summaryPublicationFixture(t)
	require.NoError(t, f.store.Summaries().PublishSummary(t.Context(), first))
	input, found, err := f.store.Summaries().GetSummaryInput(t.Context(), first.Input)
	require.NoError(t, err)
	require.True(t, found)
	issueID := publishSummaryDigest(t, f, domain.Item{ID: input.ItemID, WorkspaceID: input.WorkspaceID, Kind: domain.ItemKindArticle, Title: input.Title, ContentMarkdown: input.Markdown, ContentOrigin: input.Origin})
	conn, err := pgx.Connect(t.Context(), f.dsn)
	require.NoError(t, err)
	defer func() { require.NoError(t, conn.Close(t.Context())) }()
	_, err = conn.Exec(t.Context(), `UPDATE jobs SET status='succeeded' WHERE id=$1`, first.Annotation.GenerationJobID)
	require.NoError(t, err)
	request := summaryAdmissionRequest(t, input)
	request.ConfigurationFingerprint = "cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"
	next, err := f.store.Summaries().AdmitSummary(t.Context(), request)
	require.NoError(t, err)
	require.True(t, next.Created)
	claim, err := f.store.Jobs().Claim(t.Context(), domain.JobClaimRequest{Queue: domain.JobQueueGeneration, Owner: "summary-worker", Now: time.Now(), LeaseExpiresAt: time.Now().Add(time.Minute)})
	require.NoError(t, err)
	require.Equal(t, next.Job.ID, claim.Job.ID)
	second := first
	second.Annotation.ID, second.Annotation.GenerationJobID, second.Annotation.Text = uuid.New(), next.Job.ID, "New model annotation"
	second.Configuration.Model = "new-summary-model"
	second.ConfigurationFingerprint = request.ConfigurationFingerprint
	second.Attempt = claim.Job.Attempt
	require.NoError(t, f.store.Summaries().PublishSummary(t.Context(), second))
	batch, err := f.store.Summaries().ReadDigestSummaryBindings(t.Context(), f.workspaceID, issueID)
	require.NoError(t, err)
	require.Equal(t, first.Annotation.ID, batch[input.SummaryInputKey].Result.ID)
	canonical, err := f.store.Summaries().ReadSummaryBinding(t.Context(), domain.SummaryBindingQuery{SummaryInputKey: input.SummaryInputKey})
	require.NoError(t, err)
	require.Equal(t, second.Annotation.ID, canonical.Result.ID)
	request.IssueID = &issueID
	saved, err := f.store.Summaries().AdmitSummary(t.Context(), request)
	require.NoError(t, err)
	require.False(t, saved.Created)
	require.Equal(t, first.Annotation.GenerationJobID, saved.Job.ID)
}

func TestDigestSummaryMarkersDoNotCaptureUnrequestedBodiesOrCreateJobs(t *testing.T) {
	f := newFeedFixture(t)
	item := domain.Item{ID: uuid.New(), WorkspaceID: f.workspaceID, Kind: domain.ItemKindArticle, Title: "Unrequested", ContentMarkdown: "Full body", ContentOrigin: domain.ContentOriginArticleBody}
	issueID := publishSummaryDigest(t, f, item)
	input := domain.SummaryInputForItem(item)
	_, found, err := f.store.Summaries().GetSummaryInput(t.Context(), input.SummaryInputKey)
	require.NoError(t, err)
	require.False(t, found)
	binding, err := f.store.Summaries().ReadSummaryBinding(t.Context(), domain.SummaryBindingQuery{SummaryInputKey: input.SummaryInputKey, IssueID: &issueID})
	require.NoError(t, err)
	require.Nil(t, binding.Job)
	require.Nil(t, binding.Result)
	request := summaryAdmissionRequest(t, input)
	admission, err := f.store.Summaries().AdmitSummary(t.Context(), request)
	require.NoError(t, err)
	require.True(t, admission.Created)
	binding, err = f.store.Summaries().ReadSummaryBinding(t.Context(), domain.SummaryBindingQuery{SummaryInputKey: input.SummaryInputKey, IssueID: &issueID})
	require.NoError(t, err)
	require.NotNil(t, binding.Job)
	require.Equal(t, admission.Job.ID, binding.Job.ID)
}

func publishSummaryDigest(t *testing.T, f feedFixture, item domain.Item) uuid.UUID {
	t.Helper()
	definition := domain.DigestDefinition{ID: uuid.New(), WorkspaceID: f.workspaceID, Name: "Saved", Timezone: "UTC", Version: 1, CreatedAt: f.now, UpdatedAt: f.now}
	require.NoError(t, f.store.CreateDigest(t.Context(), definition))
	issueID := uuid.New()
	require.NoError(t, publishDigestFixture(t, f, domain.DigestIssue{ID: issueID, WorkspaceID: f.workspaceID, DigestID: definition.ID, Title: "Snapshot", Status: domain.DigestIssueComplete, GeneratedAt: f.now, Sections: []domain.DigestSection{{Items: []domain.DigestItem{{Item: item}}}}}))
	return issueID
}
