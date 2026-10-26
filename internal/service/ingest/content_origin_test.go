package ingest_test

import (
	"context"
	"testing"
	"time"

	"github.com/ethernity/obsidian-topic-stream/internal/domain"
	"github.com/ethernity/obsidian-topic-stream/internal/service/ingest"
	"github.com/ethernity/obsidian-topic-stream/internal/service/ingest/mocks"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

func TestIngestionRecordsFullBodyVersusDescriptiveContentOrigin(t *testing.T) {
	description := "Repository description"
	cases := []struct {
		name    string
		kind    domain.ItemKind
		details domain.ItemDetails
		origin  domain.ContentOrigin
	}{
		{"article body", domain.ItemKindArticle, domain.ItemDetails{Type: domain.ItemKindArticle, Article: &domain.ArticleDetails{Content: "<p>Extracted article body</p>", Description: "Description"}}, domain.ContentOriginArticleBody},
		{"article fallback", domain.ItemKindArticle, domain.ItemDetails{Type: domain.ItemKindArticle, Article: &domain.ArticleDetails{Description: "Description only"}}, domain.ContentOriginArticleDescription},
		{"empty", domain.ItemKindArticle, domain.ItemDetails{Type: domain.ItemKindArticle, Article: &domain.ArticleDetails{}}, domain.ContentOriginEmpty},
		{"abstract", domain.ItemKindPaper, domain.ItemDetails{Type: domain.ItemKindPaper, Paper: &domain.PaperDetails{Abstract: "Abstract"}}, domain.ContentOriginPaperAbstract},
		{"video description", domain.ItemKindVideo, domain.ItemDetails{Type: domain.ItemKindVideo, Video: &domain.VideoDetails{Description: "Description"}}, domain.ContentOriginVideoDescription},
		{"README", domain.ItemKindRepository, domain.ItemDetails{Type: domain.ItemKindRepository, Repository: &domain.RepositoryDetails{Description: &description, ReadmeMarkdown: "# Actual README"}}, domain.ContentOriginRepositoryReadme},
		{"repository fallback", domain.ItemKindRepository, domain.ItemDetails{Type: domain.ItemKindRepository, Repository: &domain.RepositoryDetails{Description: &description}}, domain.ContentOriginRepositoryDescription},
	}
	for _, scenario := range cases {
		t.Run(scenario.name, func(t *testing.T) {
			controller := gomock.NewController(t)
			reader := mocks.NewMockReader(controller)
			writer := mocks.NewMockWriter(controller)
			source := domain.Source{ID: uuid.New(), WorkspaceID: uuid.New(), Config: domain.SourceConfig{Kind: domain.SourceKindRSS}}
			reader.EXPECT().Read(gomock.Any(), source).Return([]ingest.Entry{{ID: "entry", URL: "https://fixture.example/article", Title: "Title", Kind: scenario.kind, Details: scenario.details}}, nil)
			writer.EXPECT().UpsertItem(gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, item domain.Item, _ domain.SourceEntry) (domain.ItemProjectionCommit, bool, error) {
				require.Equal(t, scenario.origin, item.ContentOrigin)
				return domain.ItemProjectionCommit{}, true, nil
			})
			_, err := ingest.New(reader, writer, nil, time.Now).Sync(t.Context(), source)
			require.NoError(t, err)
		})
	}
}
