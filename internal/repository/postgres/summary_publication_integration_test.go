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

func TestSummaryPublicationIsImmutableAndFillsSharedReadyState(t *testing.T) {
	f, request := summaryPublicationFixture(t)
	require.NoError(t, f.store.Summaries().PublishSummary(t.Context(), request))
	retry := request
	retry.Annotation.ID = uuid.New()
	retry.Annotation.Text = "Must not replace"
	require.NoError(t, f.store.Summaries().PublishSummary(t.Context(), retry))
	result, found, err := f.store.Summaries().GetSummaryResultForJob(t.Context(), f.workspaceID, request.Annotation.GenerationJobID)
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, request.Annotation.ID, result.ID)
	require.Equal(t, request.Annotation.Text, result.Text)
	require.Equal(t, request.Configuration.Model, result.Model)
	binding, err := f.store.Summaries().ReadSummaryBinding(t.Context(), domain.SummaryBindingQuery{SummaryInputKey: request.Input})
	require.NoError(t, err)
	require.NotNil(t, binding.Result)
	require.Equal(t, result.ID, binding.Result.ID)
	cached, found, err := f.store.Summaries().FindReadySummary(t.Context(), request.Input, request.ConfigurationFingerprint)
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, result.ID, cached.ID)
	_, found, err = f.store.Summaries().FindReadySummary(t.Context(), request.Input, "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	require.NoError(t, err)
	require.False(t, found)
}

func TestSummaryPublicationRejectsWrongAttemptOwnershipAndSourceVersion(t *testing.T) {
	cases := []struct {
		name   string
		change func(*domain.SummaryPublication)
	}{
		{"owner", func(p *domain.SummaryPublication) { p.LeaseOwner = "stale" }},
		{"attempt", func(p *domain.SummaryPublication) { p.Attempt++ }},
		{"missing attempt", func(p *domain.SummaryPublication) { p.Attempt = 0 }},
		{"workspace", func(p *domain.SummaryPublication) { p.Input.WorkspaceID = uuid.New() }},
		{"item", func(p *domain.SummaryPublication) { p.Input.ItemID = uuid.New() }},
		{"source version", func(p *domain.SummaryPublication) {
			p.Input.SourceVersion = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
		}},
		{"job", func(p *domain.SummaryPublication) { p.Annotation.GenerationJobID = uuid.New() }},
	}
	for _, scenario := range cases {
		t.Run(scenario.name, func(t *testing.T) {
			f, request := summaryPublicationFixture(t)
			scenario.change(&request)
			require.ErrorIs(t, f.store.Summaries().PublishSummary(t.Context(), request), domain.ErrSummaryPublicationRejected)
		})
	}
}

func TestSummaryPublicationRejectsExpiredCancelledAndTerminalExecution(t *testing.T) {
	for _, statement := range []string{
		`UPDATE jobs SET lease_expires_at=clock_timestamp()-interval '1 second' WHERE id=$1`,
		`UPDATE jobs SET cancellation_requested=true WHERE id=$1`,
		`UPDATE jobs SET status='dead' WHERE id=$1`,
		`UPDATE jobs SET kind='generate-digest' WHERE id=$1`,
	} {
		t.Run(statement, func(t *testing.T) {
			f, request := summaryPublicationFixture(t)
			conn, err := pgx.Connect(t.Context(), f.dsn)
			require.NoError(t, err)
			defer func() { require.NoError(t, conn.Close(t.Context())) }()
			_, err = conn.Exec(t.Context(), statement, request.Annotation.GenerationJobID)
			require.NoError(t, err)
			require.ErrorIs(t, f.store.Summaries().PublishSummary(t.Context(), request), domain.ErrSummaryPublicationRejected)
			_, found, err := f.store.Summaries().GetSummaryResultForJob(t.Context(), f.workspaceID, request.Annotation.GenerationJobID)
			require.NoError(t, err)
			require.False(t, found)
		})
	}
}

func TestSummaryPublicationReplayCannotAliasAnotherItemWithIdenticalText(t *testing.T) {
	f, publication := summaryPublicationFixture(t)
	require.NoError(t, f.store.Summaries().PublishSummary(t.Context(), publication))
	input, found, err := f.store.Summaries().GetSummaryInput(t.Context(), publication.Input)
	require.NoError(t, err)
	require.True(t, found)
	input.ItemID = uuid.New()
	_, err = f.store.Summaries().AdmitSummary(t.Context(), summaryAdmissionRequest(t, input))
	require.NoError(t, err)
	publication.Input = input.SummaryInputKey
	require.ErrorIs(t, f.store.Summaries().PublishSummary(t.Context(), publication), domain.ErrSummaryPublicationRejected)
}

// summaryPublicationFixture creates accepted captured input and claims its real Generation Job.
func summaryPublicationFixture(t *testing.T) (feedFixture, domain.SummaryPublication) {
	t.Helper()
	f := newFeedFixture(t)
	input := domain.SummaryInputForItem(domain.Item{ID: uuid.New(), WorkspaceID: f.workspaceID, Kind: domain.ItemKindArticle, Title: "Captured", ContentMarkdown: "Owned body", ContentOrigin: domain.ContentOriginArticleBody})
	admission := summaryAdmissionRequest(t, input)
	stored, err := f.store.Summaries().AdmitSummary(t.Context(), admission)
	require.NoError(t, err)
	require.True(t, stored.Created)
	claim, err := f.store.Jobs().Claim(t.Context(), domain.JobClaimRequest{Queue: domain.JobQueueGeneration, Owner: "summary-worker", Now: time.Now().UTC(), LeaseExpiresAt: time.Now().UTC().Add(time.Minute)})
	require.NoError(t, err)
	require.Equal(t, stored.Job.ID, claim.Job.ID)
	configuration := domain.SummaryConfiguration{Endpoint: "https://summary.example/v1", Model: "summary-model", Language: "same-as-source", MaxInputChars: 48000, MaxSummaryCharacters: 800, ChunkInstruction: "Extract", ReduceInstruction: "Reduce", FinalInstruction: "Summarize", InstructionsVersion: "fixture-v1", AlgorithmVersion: "item-summary-hierarchical-v1", SchemaVersion: "item-summary-output-v1"}
	annotation := domain.AISummary{ID: uuid.New(), GenerationJobID: claim.Job.ID, SourceVersion: input.SourceVersion, Text: "Generated annotation", Model: configuration.Model, Language: configuration.Language, MaxInputChars: configuration.MaxInputChars, MaxSummaryCharacters: configuration.MaxSummaryCharacters, InstructionsVersion: configuration.InstructionsVersion, AlgorithmVersion: configuration.AlgorithmVersion, CreatedAt: f.now}
	return f, domain.SummaryPublication{Input: input.SummaryInputKey, Annotation: annotation, Configuration: configuration, ConfigurationFingerprint: admission.ConfigurationFingerprint, LeaseOwner: *claim.Job.LeaseOwner, Attempt: claim.Job.Attempt}
}
