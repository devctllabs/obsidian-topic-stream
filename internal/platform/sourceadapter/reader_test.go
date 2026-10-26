package sourceadapter_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/ethernity/obsidian-topic-stream/internal/domain"
	"github.com/ethernity/obsidian-topic-stream/internal/platform/sourceadapter"
	"github.com/ethernity/obsidian-topic-stream/internal/service/ingest"
	"github.com/stretchr/testify/require"
)

func TestRSSArticlesAreFetchedAndCleanedWithReadability(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/feed.xml":
			response.Header().Set("Content-Type", "application/rss+xml")
			_, _ = fmt.Fprintf(response, `<?xml version="1.0"?><rss version="2.0"><channel>
<title>Feed</title><link>%[1]s</link><description>Feed</description><item><guid>one</guid>
<title>Full article</title><link>%[1]s/article</link><description>Short feed summary.</description></item>
</channel></rss>`, "http://"+request.Host)
		case "/article":
			response.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = fmt.Fprint(response, `<!doctype html><html><head><title>Full article</title></head><body>
<nav>Navigation that must disappear</nav><main><article><h1>Full article</h1>
<p>`+strings.Repeat("Detailed readable paragraph. ", 12)+`</p><a href="/related">Related</a></article></main>
</body></html>`)
		default:
			http.NotFound(response, request)
		}
	}))
	defer server.Close()
	reader := sourceadapter.NewReaderWithOptions(sourceadapter.Options{
		SyndicationHTTPClient: server.Client(), ArticleHTTPClient: server.Client(),
	})

	entries, err := reader.Read(t.Context(), domain.Source{Config: domain.SourceConfig{
		Kind: domain.SourceKindRSS, URL: server.URL + "/feed.xml", AllowPrivateNetwork: true,
	}})

	require.NoError(t, err)
	require.Len(t, entries, 1)
	require.Contains(t, entries[0].Details.Article.Content, "Detailed readable paragraph")
	require.NotContains(t, entries[0].Details.Article.Content, "Navigation that must disappear")
	require.Contains(t, entries[0].Details.Article.Content, `href="`+server.URL+`/related"`)
	require.Equal(t, "readability-v1", entries[0].ExtractionVersion)
}

func TestReaderUsesConfiguredHTTPClientFactoryForFeedAndArticle(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/feed.xml":
			_, _ = fmt.Fprintf(response, `<?xml version="1.0"?><rss version="2.0"><channel>
<title>Feed</title><link>%[1]s</link><description>Feed</description><item><guid>one</guid>
<title>Article</title><link>%[1]s/article</link></item></channel></rss>`, "http://"+request.Host)
		case "/article":
			_, _ = fmt.Fprint(response, `<html><head><title>Article</title></head><body><article><p>`+
				strings.Repeat("Readable content. ", 20)+`</p></article></body></html>`)
		default:
			http.NotFound(response, request)
		}
	}))
	defer server.Close()

	var policies []bool
	reader := sourceadapter.NewReaderWithOptions(sourceadapter.Options{
		HTTPClientForNetwork: func(allowPrivateNetwork bool) *http.Client {
			policies = append(policies, allowPrivateNetwork)
			return server.Client()
		},
	})
	_, err := reader.Read(t.Context(), domain.Source{Config: domain.SourceConfig{
		Kind: domain.SourceKindRSS, URL: server.URL + "/feed.xml", AllowPrivateNetwork: true,
	}})

	require.NoError(t, err)
	require.Equal(t, []bool{false, true, true}, policies)
}

func TestRSSArticleExtractionFailureIsReturnedForDurableRetry(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.URL.Path == "/feed.xml" {
			_, _ = fmt.Fprintf(response, `<?xml version="1.0"?><rss version="2.0"><channel>
<title>Feed</title><link>%[1]s</link><description>Feed</description><item><guid>one</guid>
<title>Unavailable article</title><link>%[1]s/article</link></item></channel></rss>`, "http://"+request.Host)
			return
		}
		http.Error(response, "temporary", http.StatusServiceUnavailable)
	}))
	defer server.Close()
	reader := sourceadapter.NewReaderWithOptions(sourceadapter.Options{
		SyndicationHTTPClient: server.Client(), ArticleHTTPClient: server.Client(),
	})

	_, err := reader.Read(t.Context(), domain.Source{Config: domain.SourceConfig{
		Kind: domain.SourceKindRSS, URL: server.URL + "/feed.xml", AllowPrivateNetwork: true,
	}})

	require.ErrorContains(t, err, "extract article")
	require.ErrorContains(t, err, "503")
}

func TestGitHubTrendingV1SnapshotBecomesRepositoryEntries(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		require.Equal(t, "/weekly/go.json", request.URL.Path)
		response.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(response, `{
  "schemaVersion":1,"kind":"snapshot","period":"weekly","language":"go",
  "sourceUrl":"https://github.com/trending/go?since=weekly","observedAt":"2026-09-27T08:00:00Z",
  "status":"complete","issues":[],"additiveField":"ignored",
  "items":[{"rank":1,"fullName":"owner/project","url":"https://github.com/owner/project",
    "description":"Useful project","programmingLanguage":"Go","stars":120,"forks":4,"starsInPeriod":20}]
}`)
	}))
	defer server.Close()
	reader := sourceadapter.NewReaderWithOptions(sourceadapter.Options{GitHubTrendingBaseURL: server.URL, GitHubHTTPClient: server.Client()})
	entries, err := reader.Read(t.Context(), domain.Source{Config: domain.SourceConfig{Kind: domain.SourceKindGithub, Period: "weekly", Language: "go"}})
	require.NoError(t, err)
	require.Len(t, entries, 1)
	require.Equal(t, "owner/project", entries[0].ID)
	require.Equal(t, "https://github.com/owner/project", entries[0].URL)
	require.Equal(t, domain.ItemKindRepository, entries[0].Kind)
	require.Equal(t, domain.ItemKindRepository, entries[0].Details.Type)
	require.Equal(t, 20, *entries[0].Details.Repository.StarsInPeriod)
	require.Equal(t, "Go", *entries[0].Details.Repository.ProgrammingLanguage)
	require.Equal(t, "weekly", entries[0].Details.Repository.Period)
}

func TestGitHubRepositoryEnrichmentLoadsMetadataAndReadme(t *testing.T) {
	t.Parallel()

	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		requests.Add(1)
		switch request.URL.Path {
		case "/repos/owner/project":
			response.Header().Set("Content-Type", "application/json")
			_, _ = fmt.Fprint(response, `{
  "description":"Enriched project","homepage":"https://project.example","language":"Go",
  "stargazers_count":150,"forks_count":8,"open_issues_count":3,
  "topics":["feeds","retrieval"],"default_branch":"main"
}`)
		case "/repos/owner/project/readme":
			require.Equal(t, "application/vnd.github.raw+json", request.Header.Get("Accept"))
			response.Header().Set("Content-Type", "text/plain; charset=utf-8")
			_, _ = fmt.Fprint(response, "# Project\n\nUseful README.")
		default:
			http.NotFound(response, request)
		}
	}))
	defer server.Close()

	description := "Trending description"
	reader := sourceadapter.NewReaderWithOptions(sourceadapter.Options{
		GitHubAPIBaseURL: server.URL, GitHubHTTPClient: server.Client(),
	})
	entry, err := reader.Enrich(t.Context(), ingest.Entry{
		ID: "owner/project", Title: "owner/project", URL: "https://github.com/owner/project",
		Kind: domain.ItemKindRepository, ExtractionVersion: "github-trending-v1",
		Details: domain.ItemDetails{Type: domain.ItemKindRepository, Repository: &domain.RepositoryDetails{
			Description: &description, Rank: 1, Period: "weekly", SnapshotStatus: "complete",
		}},
	}, nil)

	require.NoError(t, err)
	require.Equal(t, "github-trending-v2", entry.ExtractionVersion)
	require.Equal(t, "Enriched project", *entry.Details.Repository.Description)
	require.Equal(t, "https://project.example", *entry.Details.Repository.Homepage)
	require.Equal(t, "Go", *entry.Details.Repository.ProgrammingLanguage)
	require.Equal(t, 150, *entry.Details.Repository.Stars)
	require.Equal(t, 8, *entry.Details.Repository.Forks)
	require.Equal(t, 3, *entry.Details.Repository.OpenIssues)
	require.Equal(t, []string{"feeds", "retrieval"}, entry.Details.Repository.Topics)
	require.Equal(t, "main", *entry.Details.Repository.DefaultBranch)
	require.Equal(t, "# Project\n\nUseful README.", entry.Details.Repository.ReadmeMarkdown)
	require.Equal(t, int32(2), requests.Load())

	starsInPeriod := 30
	refreshed, err := reader.Enrich(t.Context(), ingest.Entry{
		ID: "owner/project", Title: "owner/project", URL: "https://github.com/owner/project",
		Kind: domain.ItemKindRepository, ExtractionVersion: "github-trending-v1",
		Details: domain.ItemDetails{Type: domain.ItemKindRepository, Repository: &domain.RepositoryDetails{
			Description: &description, StarsInPeriod: &starsInPeriod,
			Rank: 2, Period: "weekly", SnapshotStatus: "complete",
		}},
	}, &domain.Item{ExtractionVersion: entry.ExtractionVersion, Details: entry.Details})

	require.NoError(t, err)
	require.Equal(t, int32(2), requests.Load(), "current enrichment must not call GitHub again")
	require.Equal(t, "# Project\n\nUseful README.", refreshed.Details.Repository.ReadmeMarkdown)
	require.Equal(t, 2, refreshed.Details.Repository.Rank, "new Trending snapshot fields stay current")
	require.Equal(t, 30, *refreshed.Details.Repository.StarsInPeriod)
}

func TestArxivAndYouTubeUseDifferentTypedPayloads(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		response.Header().Set("Content-Type", "application/atom+xml")
		switch request.URL.Path {
		case "/arxiv":
			require.Equal(t, "cat:cs.AI", request.URL.Query().Get("search_query"))
			_, _ = fmt.Fprint(response, `<?xml version="1.0"?><feed xmlns="http://www.w3.org/2005/Atom">
<title>arXiv</title><id>https://arxiv.org/</id><updated>2026-09-27T08:00:00Z</updated>
<entry><id>http://arxiv.org/abs/2609.00001v1</id><title>Typed paper</title><link href="https://arxiv.org/abs/2609.00001"/>
<updated>2026-09-27T08:00:00Z</updated><published>2026-09-27T08:00:00Z</published><summary>Paper abstract.</summary>
<author><name>Ada</name></author><category term="cs.AI"/></entry></feed>`)
		case "/youtube":
			require.Equal(t, "UCabcdefghijklmnopqrstuv", request.URL.Query().Get("channel_id"))
			_, _ = fmt.Fprint(response, `<?xml version="1.0"?><feed xmlns="http://www.w3.org/2005/Atom" xmlns:yt="http://www.youtube.com/xml/schemas/2015" xmlns:media="http://search.yahoo.com/mrss/">
<title>Videos</title><id>yt:channel:test</id><updated>2026-09-27T08:00:00Z</updated>
<entry><id>yt:video:abc123</id><yt:videoId>abc123</yt:videoId><title>Typed video</title>
<link rel="alternate" href="https://www.youtube.com/watch?v=abc123"/><published>2026-09-27T08:00:00Z</published>
<media:group><media:description>Video description.</media:description></media:group></entry></feed>`)
		default:
			http.NotFound(response, request)
		}
	}))
	defer server.Close()
	reader := sourceadapter.NewReaderWithOptions(sourceadapter.Options{
		ArxivAPIURL: server.URL + "/arxiv", YouTubeFeedURL: server.URL + "/youtube", SyndicationHTTPClient: server.Client(),
	})

	papers, err := reader.Read(t.Context(), domain.Source{Config: domain.SourceConfig{Kind: domain.SourceKindArxiv, Query: "cat:cs.AI"}})
	require.NoError(t, err)
	require.Len(t, papers, 1)
	require.Equal(t, domain.ItemKindPaper, papers[0].Kind)
	require.Equal(t, "Paper abstract.", papers[0].Details.Paper.Abstract)
	require.Equal(t, []string{"cs.AI"}, papers[0].Details.Paper.Categories)

	videos, err := reader.Read(t.Context(), domain.Source{Config: domain.SourceConfig{Kind: domain.SourceKindYoutube, ChannelID: "UCabcdefghijklmnopqrstuv"}})
	require.NoError(t, err)
	require.Len(t, videos, 1)
	require.Equal(t, domain.ItemKindVideo, videos[0].Kind)
	require.Equal(t, "Video description.", videos[0].Details.Video.Description)
	require.Equal(t, "abc123", videos[0].Details.Video.VideoID)
	require.Equal(t, "UCabcdefghijklmnopqrstuv", videos[0].Details.Video.ChannelID)
}

func TestGitHubTrendingRejectsBreakingOrIncompleteContracts(t *testing.T) {
	t.Parallel()
	tests := []struct{ name, body, want string }{
		{"schema version", `{"schemaVersion":2,"kind":"snapshot","period":"daily","language":"all","sourceUrl":"x","observedAt":"2026-09-27T08:00:00Z","status":"complete","issues":[],"items":[]}`, "schemaVersion 2"},
		{"missing nullable field", `{"schemaVersion":1,"kind":"snapshot","period":"daily","language":"all","sourceUrl":"x","observedAt":"2026-09-27T08:00:00Z","status":"complete","issues":[],"items":[{"rank":1,"fullName":"o/r","url":"https://github.com/o/r","programmingLanguage":null,"stars":null,"forks":null}]}`, "missing description"},
		{"negative metric", `{"schemaVersion":1,"kind":"snapshot","period":"daily","language":"all","sourceUrl":"x","observedAt":"2026-09-27T08:00:00Z","status":"complete","issues":[],"items":[{"rank":1,"fullName":"o/r","url":"https://github.com/o/r","description":null,"programmingLanguage":null,"stars":-1,"forks":null}]}`, "negative metric"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) { _, _ = fmt.Fprint(response, test.body) }))
			defer server.Close()
			reader := sourceadapter.NewReaderWithOptions(sourceadapter.Options{GitHubTrendingBaseURL: server.URL, GitHubHTTPClient: server.Client()})
			_, err := reader.Read(t.Context(), domain.Source{Config: domain.SourceConfig{Kind: domain.SourceKindGithub, Period: "daily", Language: "all"}})
			require.ErrorContains(t, err, test.want)
		})
	}
}
