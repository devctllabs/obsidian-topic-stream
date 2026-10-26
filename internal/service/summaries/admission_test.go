package summaries_test

import (
	"context"
	"testing"

	"github.com/ethernity/obsidian-topic-stream/internal/domain"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

func TestExplicitSummaryRequestAdmitsWorkWithoutCallingProvider(t *testing.T) {
	t.Parallel()
	for _, created := range []bool{true, false} {
		t.Run(map[bool]string{true: "new", false: "alias"}[created], func(t *testing.T) {
			f := newSummaryFixture(t)
			itemID, jobID := uuid.New(), uuid.New()
			item := domain.Item{ID: itemID, WorkspaceID: f.workspaceID, Kind: domain.ItemKindArticle, Title: "Article", ContentMarkdown: "Full extracted body", ContentOrigin: domain.ContentOriginArticleBody}
			f.repository.EXPECT().GetSummarySource(gomock.Any(), f.workspaceID, domain.SummarySource{ItemID: itemID}).Return(item, true, nil)
			f.settings.EXPECT().GetWorkspace(gomock.Any()).Return(summarySettings(), nil)
			committed := false
			f.repository.EXPECT().AdmitSummary(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, request domain.SummaryAdmission) (domain.SummaryAdmissionResult, error) {
				require.Equal(t, item.ContentMarkdown, request.Input.Markdown)
				require.Equal(t, f.workspaceID, request.Input.WorkspaceID)
				require.Equal(t, "summarize-item:"+itemID.String()+":"+request.Input.SourceVersion, *request.Job.IdempotencyKey)
				require.Equal(t, domain.JobQueueGeneration, request.Job.Queue)
				require.Equal(t, 3, request.Job.MaxAttempts)
				require.JSONEq(t, `{"itemId":"`+itemID.String()+`","sourceVersion":"`+request.Input.SourceVersion+`"}`, string(request.Job.Arguments))
				require.Len(t, request.ConfigurationFingerprint, 64)
				committed = true
				return domain.SummaryAdmissionResult{Job: domain.Job{ID: jobID, WorkspaceID: f.workspaceID}, Created: created}, nil
			})
			if created {
				f.events.EXPECT().NotifyJobChanged(f.workspaceID, jobID).Do(func(uuid.UUID, uuid.UUID) { require.True(t, committed) })
			}
			job, err := f.service.Request(t.Context(), domain.SummarySource{ItemID: itemID})
			require.NoError(t, err)
			require.Equal(t, jobID, job.ID)
		})
	}
}

func TestSummaryRequestRejectsUnsupportedOrDisabledGeneration(t *testing.T) {
	for _, enabled := range []bool{true, false} {
		t.Run(map[bool]string{true: "unsupported", false: "disabled"}[enabled], func(t *testing.T) {
			f := newSummaryFixture(t)
			itemID := uuid.New()
			origin := domain.ContentOriginArticleBody
			if enabled {
				origin = domain.ContentOriginArticleDescription
			}
			f.repository.EXPECT().GetSummarySource(gomock.Any(), f.workspaceID, domain.SummarySource{ItemID: itemID}).Return(domain.Item{ID: itemID, WorkspaceID: f.workspaceID, Kind: domain.ItemKindArticle, ContentMarkdown: "Some text", ContentOrigin: origin}, true, nil)
			settings := summarySettings()
			settings.Generation.Enabled = enabled
			f.settings.EXPECT().GetWorkspace(gomock.Any()).Return(settings, nil)
			_, err := f.service.Request(t.Context(), domain.SummarySource{ItemID: itemID})
			require.ErrorIs(t, err, domain.ErrSummaryUnavailable)
		})
	}
}

func TestSummaryConfigurationAndInputIgnoreUnrelatedChanges(t *testing.T) {
	t.Parallel()
	f := newSummaryFixture(t)
	itemID := uuid.New()
	item := domain.Item{ID: itemID, WorkspaceID: f.workspaceID, Kind: domain.ItemKindRepository, Title: "Repository", ContentMarkdown: "Description followed by README", ContentOrigin: domain.ContentOriginRepositoryReadme, Details: domain.ItemDetails{Type: domain.ItemKindRepository, Repository: &domain.RepositoryDetails{ReadmeMarkdown: "# Actual README"}}}
	source := domain.SummarySource{ItemID: itemID}
	f.repository.EXPECT().GetSummarySource(gomock.Any(), f.workspaceID, source).Return(item, true, nil).Times(3)
	settings := summarySettings()
	changed := settings
	changed.Version = 99
	changed.EmbeddingModelID = uuid.New()
	changed.Instructions.DigestRerank = "Unrelated"
	changed.Retrieval.CandidateLimit = 77
	newModel := "different-summary-model"
	different := changed
	different.Generation.SummaryModel = &newModel
	gomock.InOrder(f.settings.EXPECT().GetWorkspace(gomock.Any()).Return(settings, nil), f.settings.EXPECT().GetWorkspace(gomock.Any()).Return(changed, nil), f.settings.EXPECT().GetWorkspace(gomock.Any()).Return(different, nil))
	var requests []domain.SummaryAdmission
	f.repository.EXPECT().AdmitSummary(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, request domain.SummaryAdmission) (domain.SummaryAdmissionResult, error) {
		requests = append(requests, request)
		return domain.SummaryAdmissionResult{Job: domain.Job{ID: uuid.New()}}, nil
	}).Times(3)
	for range 3 {
		_, err := f.service.Request(t.Context(), source)
		require.NoError(t, err)
	}
	require.Equal(t, "# Actual README", requests[0].Input.Markdown)
	require.Equal(t, requests[0].Input.SourceVersion, requests[1].Input.SourceVersion)
	require.Equal(t, requests[0].ConfigurationFingerprint, requests[1].ConfigurationFingerprint)
	require.NotEqual(t, requests[1].ConfigurationFingerprint, requests[2].ConfigurationFingerprint)
	item.Visibility = domain.ItemVisibilityHidden
	item.ReadingState = domain.ReadingStateRead
	item.Feedback = &domain.Feedback{Sentiment: "like"}
	require.Equal(t, requests[0].Input.SourceVersion, domain.SummaryInputForItem(item).SourceVersion)
}
