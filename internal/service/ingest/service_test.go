package ingest_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/ethernity/obsidian-topic-stream/internal/domain"
	"github.com/ethernity/obsidian-topic-stream/internal/platform/sourceadapter"
	"github.com/ethernity/obsidian-topic-stream/internal/service/ingest"
	"github.com/ethernity/obsidian-topic-stream/internal/service/ingest/mocks"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

type indexerSpy struct {
	commits []domain.ItemProjectionCommit
}

// Enqueue records the submitted work without executing a handler.
func (s *indexerSpy) Enqueue(_ context.Context, commit domain.ItemProjectionCommit) (domain.Job, error) {
	s.commits = append(s.commits, commit)
	return domain.Job{}, nil
}

type entryReaderStub struct {
	entry ingest.Entry
}

// Read returns the fixture's configured upstream entries.
func (s entryReaderStub) Read(context.Context, domain.Source) ([]ingest.Entry, error) {
	return []ingest.Entry{s.entry}, nil
}

type writerSpy struct {
	items   []domain.Item
	entries []domain.SourceEntry
}

type pendingWriterStub struct {
	commit domain.ItemProjectionCommit
}

// UpsertItem returns an existing pending commit while reporting unchanged canonical content.
func (s pendingWriterStub) UpsertItem(
	context.Context,
	domain.Item,
	domain.SourceEntry,
) (domain.ItemProjectionCommit, bool, error) {
	return s.commit, false, nil
}

// GetItem returns the fixture Item and its lookup result.
func (pendingWriterStub) GetItem(context.Context, uuid.UUID, uuid.UUID) (domain.Item, bool, error) {
	return domain.Item{}, false, nil
}

// UpsertItem records canonical Item and Source Entry writes and returns a synthetic projection commit.
func (s *writerSpy) UpsertItem(
	_ context.Context,
	item domain.Item,
	entry domain.SourceEntry,
) (domain.ItemProjectionCommit, bool, error) {
	s.items = append(s.items, item)
	s.entries = append(s.entries, entry)
	return domain.ItemProjectionCommit{
		WorkspaceID: item.WorkspaceID, ItemID: item.ID, ProjectionRevision: uuid.New(),
	}, true, nil
}

// GetItem returns the fixture Item and its lookup result.
func (s *writerSpy) GetItem(context.Context, uuid.UUID, uuid.UUID) (domain.Item, bool, error) {
	return domain.Item{}, false, nil
}

type enrichingReaderStub struct {
	entry       ingest.Entry
	enrichCalls int
}

// Read returns the fixture's configured upstream entries.
func (s *enrichingReaderStub) Read(context.Context, domain.Source) ([]ingest.Entry, error) {
	return []ingest.Entry{s.entry}, nil
}

// Enrich reuses previous repository extraction when compatible and counts enrichment attempts.
func (s *enrichingReaderStub) Enrich(
	_ context.Context,
	entry ingest.Entry,
	previous *domain.Item,
) (ingest.Entry, error) {
	s.enrichCalls++
	if previous != nil && previous.ExtractionVersion == "repository-enriched-v1" {
		entry.Details.Repository.ReadmeMarkdown = previous.Details.Repository.ReadmeMarkdown
		entry.ExtractionVersion = previous.ExtractionVersion
		return entry, nil
	}
	entry.Details.Repository.ReadmeMarkdown = "# README"
	entry.ExtractionVersion = "repository-enriched-v1"
	return entry, nil
}

func TestSyncNormalizesAndIdempotentlyStoresRSSItems(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/feed.xml":
			articleURL := "http://" + request.Host + "/articles/one?utm_source=newsletter&keep=yes#comments"
			response.Header().Set("Content-Type", "application/rss+xml")
			_, _ = fmt.Fprintf(response, `<?xml version="1.0" encoding="UTF-8"?>
<rss version="2.0" xmlns:content="http://purl.org/rss/1.0/modules/content/">
  <channel><title>Example</title><link>https://example.com</link><description>Example feed</description>
    <item><guid>entry-1</guid><title>Useful article</title><link>%s</link>
      <author>Ada Example</author><pubDate>Sat, 27 Sep 2026 09:00:00 GMT</pubDate>
      <description>Useful news</description>
      <content:encoded><![CDATA[<p>Hello <strong>world</strong>.</p>]]></content:encoded>
    </item>
  </channel>
</rss>`, articleURL)
		case "/articles/one":
			response.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = fmt.Fprint(response, `<html><head><title>Useful article</title></head><body><main><article>
<h1>Useful article</h1><p>Hello <strong>world</strong>. This is the complete article body with enough useful detail for extraction.</p>
</article></main></body></html>`)
		default:
			http.NotFound(response, request)
		}
	}))
	defer server.Close()

	workspaceID := uuid.MustParse("018fda3a-4e36-7d1d-8835-45165d3f3596")
	sourceID := uuid.MustParse("018fda3a-89b3-7648-a409-7e033adc3401")
	now := time.Date(2026, time.September, 27, 12, 0, 0, 0, time.UTC)
	repository := mocks.NewMockWriter(gomock.NewController(t))
	var written domain.Item
	repository.EXPECT().GetItem(gomock.Any(), workspaceID, gomock.Any()).Return(domain.Item{}, false, nil)
	repository.EXPECT().GetItem(gomock.Any(), workspaceID, gomock.Any()).DoAndReturn(func(context.Context, uuid.UUID, uuid.UUID) (domain.Item, bool, error) { return written, true, nil })
	repository.EXPECT().UpsertItem(gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, item domain.Item, entry domain.SourceEntry) (domain.ItemProjectionCommit, bool, error) {
		written = item
		require.Equal(t, sourceID, entry.SourceID)
		return domain.ItemProjectionCommit{WorkspaceID: workspaceID, ItemID: item.ID, ProjectionRevision: uuid.New()}, true, nil
	})
	repository.EXPECT().UpsertItem(gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, item domain.Item, _ domain.SourceEntry) (domain.ItemProjectionCommit, bool, error) {
		require.Equal(t, written, item)
		return domain.ItemProjectionCommit{}, false, nil
	})
	indexer := &indexerSpy{}
	service := ingest.New(sourceadapter.NewReaderWithOptions(sourceadapter.Options{}), repository, indexer, func() time.Time { return now })
	source := domain.Source{ID: sourceID, WorkspaceID: workspaceID, Config: domain.SourceConfig{Kind: domain.SourceKindRSS, URL: server.URL + "/feed.xml", AllowPrivateNetwork: true}}

	first, err := service.Sync(context.Background(), source)
	require.NoError(t, err)
	second, err := service.Sync(context.Background(), source)
	require.NoError(t, err)
	require.Equal(t, ingest.Result{Read: 1, Stored: 1}, first)
	require.Equal(t, ingest.Result{Read: 1, Stored: 0, Unchanged: 1}, second)

	items := []domain.Item{written}
	require.Equal(t, server.URL+"/articles/one?keep=yes", items[0].CanonicalURL)
	require.Contains(t, items[0].ContentMarkdown, "Hello **world**")
	require.Contains(t, items[0].ContentMarkdown, "complete article body")
	require.Equal(t, "readability-v1", items[0].ExtractionVersion)
	require.Equal(t, []string{"Ada Example"}, items[0].Authors)
	require.Equal(t, domain.ItemKindArticle, items[0].Kind)
	require.Equal(t, "Useful news", items[0].Details.Article.Description)
	require.Len(t, indexer.commits, 1, "only a changed canonical item is queued for indexing")
	require.Equal(t, items[0].ID, indexer.commits[0].ItemID)
	require.NotEqual(t, uuid.Nil, indexer.commits[0].ProjectionRevision)
}

func TestSyncReenqueuesStillPendingProjectionAfterAnUnchangedRetry(t *testing.T) {
	t.Parallel()

	workspaceID := uuid.New()
	commit := domain.ItemProjectionCommit{
		WorkspaceID: workspaceID, ItemID: uuid.New(), ProjectionRevision: uuid.New(),
	}
	indexer := &indexerSpy{}
	service := ingest.New(entryReaderStub{entry: ingest.Entry{
		ID: "entry", Title: "Pending item", URL: "https://example.com/pending",
		Kind: domain.ItemKindArticle, Details: domain.ItemDetails{
			Type: domain.ItemKindArticle, Article: &domain.ArticleDetails{Description: "Pending"},
		},
	}}, pendingWriterStub{commit: commit}, indexer, time.Now)

	result, err := service.Sync(t.Context(), domain.Source{ID: uuid.New(), WorkspaceID: workspaceID})

	require.NoError(t, err)
	require.Equal(t, ingest.Result{Read: 1, Unchanged: 1}, result)
	require.Equal(t, []domain.ItemProjectionCommit{commit}, indexer.commits)
}

func TestSyncReusesStoredOneTimeEnrichment(t *testing.T) {
	t.Parallel()

	workspaceID, sourceID := uuid.New(), uuid.New()
	now := time.Date(2026, time.September, 28, 12, 0, 0, 0, time.UTC)
	description := "Repository"
	reader := &enrichingReaderStub{entry: ingest.Entry{
		ID: "owner/project", Title: "owner/project", URL: "https://github.com/owner/project",
		Kind: domain.ItemKindRepository, ExtractionVersion: "repository-base-v1",
		Details: domain.ItemDetails{Type: domain.ItemKindRepository, Repository: &domain.RepositoryDetails{
			Description: &description, Rank: 1, Period: "weekly", ObservedAt: now, SnapshotStatus: "complete",
		}},
	}}
	repository := mocks.NewMockWriter(gomock.NewController(t))
	var written domain.Item
	repository.EXPECT().GetItem(gomock.Any(), workspaceID, gomock.Any()).Return(domain.Item{}, false, nil)
	repository.EXPECT().GetItem(gomock.Any(), workspaceID, gomock.Any()).DoAndReturn(func(context.Context, uuid.UUID, uuid.UUID) (domain.Item, bool, error) { return written, true, nil })
	repository.EXPECT().UpsertItem(gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, item domain.Item, entry domain.SourceEntry) (domain.ItemProjectionCommit, bool, error) {
		written = item
		require.Equal(t, sourceID, entry.SourceID)
		return domain.ItemProjectionCommit{WorkspaceID: workspaceID, ItemID: item.ID, ProjectionRevision: uuid.New()}, true, nil
	})
	repository.EXPECT().UpsertItem(gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, item domain.Item, _ domain.SourceEntry) (domain.ItemProjectionCommit, bool, error) {
		require.Equal(t, written, item)
		return domain.ItemProjectionCommit{}, false, nil
	})
	service := ingest.New(reader, repository, nil, func() time.Time { return now })
	source := domain.Source{
		ID: sourceID, WorkspaceID: workspaceID,
		Config: domain.SourceConfig{Kind: domain.SourceKindGithub, Period: "weekly", Language: "all"},
	}

	first, err := service.Sync(t.Context(), source)
	require.NoError(t, err)
	second, err := service.Sync(t.Context(), source)
	require.NoError(t, err)

	require.Equal(t, ingest.Result{Read: 1, Stored: 1}, first)
	require.Equal(t, ingest.Result{Read: 1, Unchanged: 1}, second)
	require.Equal(t, 2, reader.enrichCalls, "the adapter is consulted but must reuse the prior enrichment")
	itemID := uuid.NewSHA1(uuid.NameSpaceURL, []byte(workspaceID.String()+":https://github.com/owner/project"))
	require.Equal(t, itemID, written.ID)
	item := written
	require.Equal(t, "# README", item.Details.Repository.ReadmeMarkdown)
	require.Equal(t, "Repository\n\n# README", item.ContentMarkdown)
}

func TestSyncStoresTypedSourceEntryDetails(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.September, 28, 12, 0, 0, 0, time.UTC)
	doi := "10.1000/retrieval"
	description := "Repository description"
	homepage := "https://example.com/project"
	language := "Go"
	stars, forks, issues, starsInPeriod := 100, 10, 2, 7
	branch := "main"
	tests := []struct {
		name        string
		entry       ingest.Entry
		wantDetails domain.SourceEntryDetails
		wantContent string
	}{
		{
			name: "article",
			entry: ingest.Entry{Kind: domain.ItemKindArticle, Details: domain.ItemDetails{
				Type:    domain.ItemKindArticle,
				Article: &domain.ArticleDetails{Description: "Feed description", Content: "<p>Full article</p>"},
			}},
			wantDetails: domain.SourceEntryDetails{Type: domain.ItemKindArticle, Article: &domain.SourceArticleDetails{
				Description: "Feed description",
			}},
			wantContent: "Full article",
		},
		{
			name: "paper",
			entry: ingest.Entry{Kind: domain.ItemKindPaper, Details: domain.ItemDetails{
				Type:  domain.ItemKindPaper,
				Paper: &domain.PaperDetails{Abstract: "Paper abstract", Categories: []string{"cs.IR"}, DOI: &doi},
			}},
			wantDetails: domain.SourceEntryDetails{Type: domain.ItemKindPaper, Paper: &domain.SourcePaperDetails{
				Abstract: "Paper abstract", Categories: []string{"cs.IR"}, DOI: &doi,
			}},
			wantContent: "Paper abstract",
		},
		{
			name: "video",
			entry: ingest.Entry{Kind: domain.ItemKindVideo, Details: domain.ItemDetails{
				Type:  domain.ItemKindVideo,
				Video: &domain.VideoDetails{Description: "Video description", ChannelID: "channel", VideoID: "video"},
			}},
			wantDetails: domain.SourceEntryDetails{Type: domain.ItemKindVideo, Video: &domain.SourceVideoDetails{
				Description: "Video description", ChannelID: "channel", VideoID: "video",
			}},
			wantContent: "Video description",
		},
		{
			name: "repository",
			entry: ingest.Entry{Kind: domain.ItemKindRepository, Details: domain.ItemDetails{
				Type: domain.ItemKindRepository,
				Repository: &domain.RepositoryDetails{
					Description: &description, Homepage: &homepage, ProgrammingLanguage: &language,
					Stars: &stars, Forks: &forks, OpenIssues: &issues, Topics: []string{"retrieval"},
					DefaultBranch: &branch, ReadmeMarkdown: "# README", StarsInPeriod: &starsInPeriod,
					Rank: 1, Period: "weekly", ObservedAt: now, SnapshotStatus: "complete",
				},
			}},
			wantDetails: domain.SourceEntryDetails{Type: domain.ItemKindRepository, Repository: &domain.SourceRepositoryDetails{
				Description: &description, Homepage: &homepage, ProgrammingLanguage: &language,
				Stars: &stars, Forks: &forks, OpenIssues: &issues, Topics: []string{"retrieval"},
				DefaultBranch: &branch, StarsInPeriod: &starsInPeriod, Rank: 1, Period: "weekly",
				ObservedAt: now, SnapshotStatus: "complete",
			}},
			wantContent: "Repository description\n\n# README",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			writer := &writerSpy{}
			test.entry.ID = test.name
			test.entry.Title = test.name
			test.entry.URL = "https://example.com/" + test.name
			test.entry.ExtractionVersion = "pipeline-v1"
			service := ingest.New(entryReaderStub{entry: test.entry}, writer, nil, func() time.Time { return now })
			source := domain.Source{ID: uuid.New(), WorkspaceID: uuid.New()}

			_, err := service.Sync(t.Context(), source)

			require.NoError(t, err)
			require.Equal(t, test.wantDetails, writer.entries[0].Details)
			require.Equal(t, "pipeline-v1", writer.entries[0].ExtractionVersion)
			require.Equal(t, test.wantContent, writer.items[0].ContentMarkdown)
		})
	}
}

func TestSyncRejectsInvalidItemDetailsUnion(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		details domain.ItemDetails
	}{
		{
			name: "mismatched type",
			details: domain.ItemDetails{
				Type: domain.ItemKindPaper, Article: &domain.ArticleDetails{Description: "article"},
			},
		},
		{name: "missing variant", details: domain.ItemDetails{Type: domain.ItemKindArticle}},
		{
			name: "wrong variant",
			details: domain.ItemDetails{
				Type: domain.ItemKindArticle, Paper: &domain.PaperDetails{Abstract: "paper"},
			},
		},
		{
			name: "multiple variants",
			details: domain.ItemDetails{
				Type:    domain.ItemKindArticle,
				Article: &domain.ArticleDetails{Description: "article"},
				Paper:   &domain.PaperDetails{Abstract: "paper"},
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			writer := &writerSpy{}
			entry := ingest.Entry{
				ID: test.name, Title: test.name, URL: "https://example.com/invalid",
				Kind: domain.ItemKindArticle, Details: test.details,
			}
			service := ingest.New(entryReaderStub{entry: entry}, writer, nil, time.Now)

			_, err := service.Sync(t.Context(), domain.Source{ID: uuid.New(), WorkspaceID: uuid.New()})

			require.Error(t, err)
			require.Empty(t, writer.entries)
		})
	}
}
