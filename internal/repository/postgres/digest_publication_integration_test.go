//go:build integration

package postgres_test

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/ethernity/obsidian-topic-stream/internal/domain"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"
)

func TestDigestPublicationCommitsOneImmutableResultPerJob(t *testing.T) {
	f := newFeedFixture(t)
	definition := domain.DigestDefinition{ID: uuid.New(), WorkspaceID: f.workspaceID, Name: "Publish", Timezone: "UTC", Version: 1, CreatedAt: f.now, UpdatedAt: f.now}
	require.NoError(t, f.store.CreateDigest(t.Context(), definition))
	request := digestPublication(t, f, domain.DigestIssue{ID: uuid.New(), WorkspaceID: f.workspaceID, DigestID: definition.ID, Title: "Original", Status: domain.DigestIssueComplete, GeneratedAt: f.now,
		Sections: []domain.DigestSection{{Items: []domain.DigestItem{{Item: domain.Item{ID: uuid.New(), ContentMarkdown: "Original snapshot"}}}}}})
	first, err := f.store.PublishDigestIssue(t.Context(), request)
	require.NoError(t, err)
	require.True(t, first.Created)
	require.Equal(t, request.Issue, first.Issue)
	retry := request
	retry.Issue.ID = uuid.New()
	retry.Issue.Title = "Changed"
	retry.Issue.GeneratedAt = f.now.Add(time.Hour)
	second, err := f.store.PublishDigestIssue(t.Context(), retry)
	require.NoError(t, err)
	require.False(t, second.Created)
	assertSameDigestIssue(t, first.Issue, second.Issue)
	saved, found, err := f.store.GetDigestIssueForJob(t.Context(), f.workspaceID, first.Issue.GenerationJobID)
	require.NoError(t, err)
	require.True(t, found)
	assertSameDigestIssue(t, first.Issue, saved)
	summaries, err := f.store.ListDigestIssues(t.Context(), domain.DigestIssueList{WorkspaceID: f.workspaceID, Limit: 20})
	require.NoError(t, err)
	require.Len(t, summaries, 1)
	require.Equal(t, first.Issue.GenerationJobID, summaries[0].GenerationJobID)
	definition, found, err = f.store.GetDigest(t.Context(), f.workspaceID, definition.ID)
	require.NoError(t, err)
	require.True(t, found)
	require.True(t, f.now.Equal(*definition.LastSuccessfulAt))
	_, err = f.store.DeleteDigest(t.Context(), domain.DigestDelete{WorkspaceID: f.workspaceID, DigestID: definition.ID, ExpectedVersion: 1, DeletedAt: f.now})
	require.NoError(t, err)
	replay, err := f.store.PublishDigestIssue(t.Context(), request)
	require.NoError(t, err)
	require.False(t, replay.Created)
	assertSameDigestIssue(t, first.Issue, replay.Issue)
}

func TestDigestPublicationRejectsWrongExecutionMetadata(t *testing.T) {
	cases := []struct {
		name   string
		change func(*domain.DigestPublication)
	}{
		{"owner", func(p *domain.DigestPublication) { p.LeaseOwner = "obsolete" }},
		{"attempt", func(p *domain.DigestPublication) { p.Attempt++ }},
		{"missing attempt", func(p *domain.DigestPublication) { p.Attempt = 0 }},
		{"workspace", func(p *domain.DigestPublication) { p.Issue.WorkspaceID = uuid.New() }},
		{"job", func(p *domain.DigestPublication) { p.Issue.GenerationJobID = uuid.New() }},
		{"digest", func(p *domain.DigestPublication) { p.Issue.DigestID = uuid.New() }},
		{"scheduled identity", func(p *domain.DigestPublication) { now := time.Now().UTC(); p.Issue.ScheduledFor = &now }},
	}
	for _, scenario := range cases {
		t.Run(scenario.name, func(t *testing.T) {
			f, request := newDigestPublicationFixture(t)
			scenario.change(&request)
			if scenario.name == "digest" {
				require.NoError(t, f.store.CreateDigest(t.Context(), domain.DigestDefinition{ID: request.Issue.DigestID, WorkspaceID: f.workspaceID, Name: "Other", Timezone: "UTC", Version: 1, CreatedAt: f.now, UpdatedAt: f.now}))
			}
			_, err := f.store.PublishDigestIssue(t.Context(), request)
			require.ErrorIs(t, err, domain.ErrDigestPublicationRejected)
			_, found, err := f.store.GetDigestIssue(t.Context(), f.workspaceID, request.Issue.ID)
			require.NoError(t, err)
			require.False(t, found)
		})
	}
}

func TestDigestPublicationRejectsExpiredCancelledAndTerminalJobs(t *testing.T) {
	for _, statement := range []string{
		`UPDATE jobs SET lease_expires_at=clock_timestamp()-interval '1 second' WHERE id=$1`,
		`UPDATE jobs SET cancellation_requested=true WHERE id=$1`,
		`UPDATE jobs SET status='dead' WHERE id=$1`,
	} {
		t.Run(statement, func(t *testing.T) {
			f, request := newDigestPublicationFixture(t)
			conn, err := pgx.Connect(t.Context(), f.dsn)
			require.NoError(t, err)
			defer func() { require.NoError(t, conn.Close(t.Context())) }()
			_, err = conn.Exec(t.Context(), statement, request.Issue.GenerationJobID)
			require.NoError(t, err)
			_, err = f.store.PublishDigestIssue(t.Context(), request)
			require.ErrorIs(t, err, domain.ErrDigestPublicationRejected)
		})
	}
}

func TestDigestRetryRejectsCommittedResultAfterLostAcknowledgement(t *testing.T) {
	f, request := newDigestPublicationFixture(t)
	_, err := f.store.PublishDigestIssue(t.Context(), request)
	require.NoError(t, err)
	conn, err := pgx.Connect(t.Context(), f.dsn)
	require.NoError(t, err)
	defer func() { require.NoError(t, conn.Close(t.Context())) }()
	_, err = conn.Exec(t.Context(), `UPDATE jobs SET status='dead' WHERE id=$1`, request.Issue.GenerationJobID)
	require.NoError(t, err)
	command := manualDigestRequest(t, domain.DigestDefinition{ID: request.Issue.DigestID, WorkspaceID: f.workspaceID}, request.Issue.GenerationJobID)
	command.Kind = domain.DigestBuildRetry
	_, err = f.store.AdmitDigestBuild(t.Context(), command)
	require.ErrorIs(t, err, domain.ErrJobNotRetryable)
}

// newDigestPublicationFixture prepares a valid running attempt and a snapshot with one consumed Item.
func newDigestPublicationFixture(t *testing.T) (feedFixture, domain.DigestPublication) {
	t.Helper()
	f := newFeedFixture(t)
	definition := domain.DigestDefinition{ID: uuid.New(), WorkspaceID: f.workspaceID, Name: "Publish", Timezone: "UTC", Version: 1, CreatedAt: f.now, UpdatedAt: f.now}
	require.NoError(t, f.store.CreateDigest(t.Context(), definition))
	return f, digestPublication(t, f, domain.DigestIssue{ID: uuid.New(), WorkspaceID: f.workspaceID, DigestID: definition.ID, Title: "Original", Status: domain.DigestIssueComplete, GeneratedAt: f.now, Sections: []domain.DigestSection{{Items: []domain.DigestItem{{Item: domain.Item{ID: uuid.New()}}}}}})
}

// digestPublication creates and claims a real generation Job for the supplied immutable snapshot.
func digestPublication(t *testing.T, f feedFixture, issue domain.DigestIssue) domain.DigestPublication {
	t.Helper()
	definition := domain.DigestDefinition{ID: issue.DigestID, WorkspaceID: issue.WorkspaceID}
	job := manualDigestRequest(t, definition, uuid.New()).Job
	payload, err := json.Marshal(map[string]any{"digestId": issue.DigestID, "scheduledFor": issue.ScheduledFor})
	require.NoError(t, err)
	job.Arguments = payload
	job.AvailableAt = f.now
	stored, created, err := f.store.Jobs().Enqueue(t.Context(), job)
	require.NoError(t, err)
	require.True(t, created)
	claim, err := f.store.Jobs().Claim(t.Context(), domain.JobClaimRequest{Queue: domain.JobQueueGeneration, Owner: "publication-fixture", Now: time.Now().UTC(), LeaseExpiresAt: time.Now().UTC().Add(10 * time.Minute)})
	require.NoError(t, err)
	require.Equal(t, stored.ID, claim.Job.ID)
	issue.GenerationJobID = claim.Job.ID
	return domain.DigestPublication{Issue: issue, LeaseOwner: *claim.Job.LeaseOwner, Attempt: claim.Job.Attempt}
}

// assertSameDigestIssue compares immutable facts while treating timestamptz values as instants.
func assertSameDigestIssue(t *testing.T, expected, actual domain.DigestIssue) {
	t.Helper()
	require.True(t, expected.GeneratedAt.Equal(actual.GeneratedAt))
	actual.GeneratedAt = expected.GeneratedAt
	if expected.ScheduledFor != nil && actual.ScheduledFor != nil {
		require.True(t, expected.ScheduledFor.Equal(*actual.ScheduledFor))
		actual.ScheduledFor = expected.ScheduledFor
	}
	require.Equal(t, expected, actual)
}

// publishDigestFixture keeps historical snapshot tests on the same fenced publication path as workers.
func publishDigestFixture(t *testing.T, f feedFixture, issue domain.DigestIssue) error {
	t.Helper()
	request := digestPublication(t, f, issue)
	_, err := f.store.PublishDigestIssue(t.Context(), request)
	status := domain.JobStatusSucceeded
	if err != nil {
		status = domain.JobStatusDead
	}
	finished, finishErr := f.store.Jobs().Finish(t.Context(), domain.JobFinish{ID: request.Issue.GenerationJobID, Owner: request.LeaseOwner, Status: status, AvailableAt: f.now, FinishedAt: &f.now, UpdatedAt: f.now})
	require.NoError(t, finishErr)
	require.True(t, finished)
	return err
}
