package summaries_test

import (
	"context"
	"encoding/json"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ethernity/obsidian-topic-stream/internal/domain"
	"github.com/ethernity/obsidian-topic-stream/internal/service/jobs"
	jobmocks "github.com/ethernity/obsidian-topic-stream/internal/service/jobs/mocks"
	"github.com/ethernity/obsidian-topic-stream/internal/service/summaries"
	"github.com/ethernity/obsidian-topic-stream/internal/testutil"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

func TestSummaryAttemptUsesCapturedInputAndStartSettingsOutsideAdmission(t *testing.T) {
	t.Parallel()
	f := newSummaryFixture(t)
	input, job := summaryAttemptInput(t, f)
	f.repository.EXPECT().GetSummaryResultForJob(gomock.Any(), f.workspaceID, job.ID).Return(domain.AISummary{}, false, nil)
	settings := summarySettings()
	model := "model-at-attempt-start"
	settings.Generation.SummaryModel = &model
	f.settings.EXPECT().GetWorkspace(gomock.Any()).Return(settings, nil)
	f.repository.EXPECT().GetSummaryInput(gomock.Any(), input.SummaryInputKey).Return(input, true, nil)
	f.repository.EXPECT().FindReadySummary(gomock.Any(), input.SummaryInputKey, gomock.Any()).Return(domain.AISummary{}, false, nil)
	f.generator.EXPECT().Summarize(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, prompt summaries.Prompt) (string, error) {
		require.Equal(t, "final", prompt.Stage)
		require.Equal(t, input.Markdown, prompt.Text)
		require.Equal(t, "model-at-attempt-start", prompt.Model)
		require.Equal(t, settings.Instructions.SummaryFinal, prompt.Instruction)
		require.Equal(t, "same-as-source", prompt.Language)
		model = "edited-during-call"
		return "Generated annotation", nil
	})
	f.repository.EXPECT().PublishSummary(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, publication domain.SummaryPublication) error {
		require.Equal(t, job.ID, publication.Annotation.GenerationJobID)
		require.Equal(t, input.SummaryInputKey, publication.Input)
		require.Equal(t, "Generated annotation", publication.Annotation.Text)
		require.Equal(t, "model-at-attempt-start", publication.Configuration.Model)
		require.Equal(t, 1, publication.Attempt)
		require.NotEmpty(t, publication.LeaseOwner)
		require.Len(t, publication.ConfigurationFingerprint, 64)
		return nil
	})
	finish := runSummaryAttempt(t, f, job)
	require.Equal(t, domain.JobStatusSucceeded, finish.Status)
}

func TestSummaryAttemptRecoversSavedResultBeforeReadingInputsOrSettings(t *testing.T) {
	t.Parallel()
	f := newSummaryFixture(t)
	input, job := summaryAttemptInput(t, f)
	f.repository.EXPECT().GetSummaryResultForJob(gomock.Any(), f.workspaceID, job.ID).Return(domain.AISummary{ID: uuid.New(), GenerationJobID: job.ID, SourceVersion: input.SourceVersion, Text: "Saved"}, true, nil)
	finish := runSummaryAttempt(t, f, job)
	require.Equal(t, domain.JobStatusSucceeded, finish.Status)
}

func TestSummaryAttemptReusesMatchingCacheWithoutProvider(t *testing.T) {
	t.Parallel()
	f := newSummaryFixture(t)
	input, job := summaryAttemptInput(t, f)
	cachedID := uuid.New()
	f.repository.EXPECT().GetSummaryResultForJob(gomock.Any(), f.workspaceID, job.ID).Return(domain.AISummary{}, false, nil)
	f.settings.EXPECT().GetWorkspace(gomock.Any()).Return(summarySettings(), nil)
	f.repository.EXPECT().GetSummaryInput(gomock.Any(), input.SummaryInputKey).Return(input, true, nil)
	f.repository.EXPECT().FindReadySummary(gomock.Any(), input.SummaryInputKey, gomock.Any()).Return(domain.AISummary{ID: cachedID, GenerationJobID: uuid.New(), SourceVersion: input.SourceVersion, Text: "Cached"}, true, nil)
	f.repository.EXPECT().PublishSummary(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, publication domain.SummaryPublication) error {
		require.Equal(t, "Cached", publication.Annotation.Text)
		require.Equal(t, &cachedID, publication.ReusedFrom)
		return nil
	})
	require.Equal(t, domain.JobStatusSucceeded, runSummaryAttempt(t, f, job).Status)
}

func TestSummaryAttemptRejectsDisabledGenerationAndMissingExecution(t *testing.T) {
	t.Parallel()
	f := newSummaryFixture(t)
	_, job := summaryAttemptInput(t, f)
	require.ErrorIs(t, f.handler(t.Context(), job.Arguments), domain.ErrSummaryPublicationRejected)
	f.repository.EXPECT().GetSummaryResultForJob(gomock.Any(), f.workspaceID, job.ID).Return(domain.AISummary{}, false, nil)
	settings := summarySettings()
	settings.Generation.Enabled = false
	f.settings.EXPECT().GetWorkspace(gomock.Any()).Return(settings, nil)
	require.Equal(t, domain.JobStatusDead, runSummaryAttempt(t, f, job).Status)
}

func TestSummaryAttemptEnforcesOutputLimitIndependentOfPrompt(t *testing.T) {
	t.Parallel()
	f := newSummaryFixture(t)
	input, job := summaryAttemptInput(t, f)
	f.repository.EXPECT().GetSummaryResultForJob(gomock.Any(), f.workspaceID, job.ID).Return(domain.AISummary{}, false, nil)
	f.settings.EXPECT().GetWorkspace(gomock.Any()).Return(summarySettings(), nil)
	f.repository.EXPECT().GetSummaryInput(gomock.Any(), input.SummaryInputKey).Return(input, true, nil)
	f.repository.EXPECT().FindReadySummary(gomock.Any(), input.SummaryInputKey, gomock.Any()).Return(domain.AISummary{}, false, nil)
	f.generator.EXPECT().Summarize(gomock.Any(), gomock.Any()).Return(strings.Repeat("я", 801), nil)
	require.Equal(t, domain.JobStatusRetryWait, runSummaryAttempt(t, f, job).Status)
}

// summaryAttemptInput prepares owned captured text and a real attempt's versioned arguments.
func summaryAttemptInput(t *testing.T, f *summaryFixture) (domain.SummaryInput, domain.Job) {
	t.Helper()
	return summaryAttemptInputText(t, f, "Captured article body")
}

// summaryAttemptInputText prepares a real model-input fingerprint for a scenario's source text.
func summaryAttemptInputText(t *testing.T, f *summaryFixture, text string) (domain.SummaryInput, domain.Job) {
	t.Helper()
	input := domain.SummaryInputForItem(domain.Item{ID: uuid.New(), WorkspaceID: f.workspaceID, Kind: domain.ItemKindArticle, Title: "Captured", ContentMarkdown: text, ContentOrigin: domain.ContentOriginArticleBody})
	payload, err := json.Marshal(map[string]any{"itemId": input.ItemID, "sourceVersion": input.SourceVersion})
	require.NoError(t, err)
	job := domain.Job{ID: uuid.New(), WorkspaceID: f.workspaceID, Kind: summaries.GenerationJobKind, Queue: domain.JobQueueGeneration, ArgumentsVersion: 1, Arguments: payload, Attempt: 1, MaxAttempts: 3, Status: domain.JobStatusRunning}
	return input, job
}

// runSummaryAttempt uses the real worker to supply authentic execution identity and attempt fencing.
func runSummaryAttempt(t *testing.T, f *summaryFixture, job domain.Job) domain.JobFinish {
	t.Helper()
	repository := jobmocks.NewMockRepository(gomock.NewController(t))
	var claimed atomic.Bool
	repository.EXPECT().Claim(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, request domain.JobClaimRequest) (domain.JobClaim, error) {
		if request.Queue == job.Queue && claimed.CompareAndSwap(false, true) {
			return domain.JobClaim{Job: job}, nil
		}
		return domain.JobClaim{}, nil
	}).AnyTimes()
	repository.EXPECT().Heartbeat(gomock.Any(), gomock.Any()).Return(false, true, nil).AnyTimes()
	finished := make(chan domain.JobFinish, 1)
	repository.EXPECT().Finish(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, finish domain.JobFinish) (bool, error) { finished <- finish; return true, nil })
	worker := jobs.New(repository, time.Now, jobs.Config{PollInterval: time.Millisecond})
	worker.RegisterDefinition(f.definition, f.handler)
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { done <- worker.Run(ctx) }()
	t.Cleanup(func() { cancel(); require.NoError(t, testutil.Receive(t, done)) })
	finish := testutil.Receive(t, finished)
	cancel()
	return finish
}
