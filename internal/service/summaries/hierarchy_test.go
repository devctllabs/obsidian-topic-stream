package summaries_test

import (
	"context"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/ethernity/obsidian-topic-stream/internal/domain"
	"github.com/ethernity/obsidian-topic-stream/internal/service/summaries"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

func TestLongSummaryUsesBoundedUnicodeChunksRecursiveReductionAndFinalSynthesis(t *testing.T) {
	t.Parallel()
	f := newSummaryFixture(t)
	body := strings.Repeat("# 研究\n\n"+strings.Repeat("事实", 150)+"\n\n", 30)
	input, job := summaryAttemptInputText(t, f, body)
	settings := summarySettings()
	settings.Summary.MaxInputChars = 1000
	f.repository.EXPECT().GetSummaryResultForJob(gomock.Any(), f.workspaceID, job.ID).Return(domain.AISummary{}, false, nil)
	f.settings.EXPECT().GetWorkspace(gomock.Any()).Return(settings, nil)
	f.repository.EXPECT().GetSummaryInput(gomock.Any(), input.SummaryInputKey).Return(input, true, nil)
	f.repository.EXPECT().FindReadySummary(gomock.Any(), input.SummaryInputKey, gomock.Any()).Return(domain.AISummary{}, false, nil)
	stages := make(map[string]int)
	f.generator.EXPECT().Summarize(gomock.Any(), gomock.Any()).DoAndReturn(func(ctx context.Context, prompt summaries.Prompt) (string, error) {
		require.True(t, utf8.ValidString(prompt.Text))
		require.LessOrEqual(t, utf8.RuneCountInString(prompt.Text), 1000)
		require.Equal(t, "same-as-source", prompt.Language)
		require.Contains(t, prompt.SourceLanguageReference, "研究")
		_, bounded := ctx.Deadline()
		require.True(t, bounded)
		stages[prompt.Stage]++
		if prompt.Stage == "final" {
			return "研究内容的简明摘要。", nil
		}
		return strings.Repeat("事实", 100), nil
	}).AnyTimes()
	f.repository.EXPECT().PublishSummary(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, publication domain.SummaryPublication) error {
		require.Equal(t, "研究内容的简明摘要。", publication.Annotation.Text)
		return nil
	})
	require.Equal(t, domain.JobStatusSucceeded, runSummaryAttempt(t, f, job).Status)
	require.Greater(t, stages["chunk"], 1)
	require.Positive(t, stages["reduce"])
	require.Equal(t, 1, stages["final"])
}

func TestSummaryRejectsExcessiveCallBudgetBeforeProviderWork(t *testing.T) {
	t.Parallel()
	f := newSummaryFixture(t)
	input, job := summaryAttemptInputText(t, f, strings.Repeat("x", 200000))
	settings := summarySettings()
	settings.Summary.MaxInputChars = 1000
	f.repository.EXPECT().GetSummaryResultForJob(gomock.Any(), f.workspaceID, job.ID).Return(domain.AISummary{}, false, nil)
	f.settings.EXPECT().GetWorkspace(gomock.Any()).Return(settings, nil)
	f.repository.EXPECT().GetSummaryInput(gomock.Any(), input.SummaryInputKey).Return(input, true, nil)
	f.repository.EXPECT().FindReadySummary(gomock.Any(), input.SummaryInputKey, gomock.Any()).Return(domain.AISummary{}, false, nil)
	require.Equal(t, domain.JobStatusDead, runSummaryAttempt(t, f, job).Status)
}

func TestSummaryOutputLimitCountsUnicodeCharacters(t *testing.T) {
	t.Parallel()
	f := newSummaryFixture(t)
	input, job := summaryAttemptInput(t, f)
	f.repository.EXPECT().GetSummaryResultForJob(gomock.Any(), f.workspaceID, job.ID).Return(domain.AISummary{}, false, nil)
	f.settings.EXPECT().GetWorkspace(gomock.Any()).Return(summarySettings(), nil)
	f.repository.EXPECT().GetSummaryInput(gomock.Any(), input.SummaryInputKey).Return(input, true, nil)
	f.repository.EXPECT().FindReadySummary(gomock.Any(), input.SummaryInputKey, gomock.Any()).Return(domain.AISummary{}, false, nil)
	f.generator.EXPECT().Summarize(gomock.Any(), gomock.Any()).Return(strings.Repeat("я", 800), nil)
	f.repository.EXPECT().PublishSummary(gomock.Any(), gomock.Any()).Return(nil)
	require.Equal(t, domain.JobStatusSucceeded, runSummaryAttempt(t, f, job).Status)
}
