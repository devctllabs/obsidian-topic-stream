package sourceadapter

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	readability "codeberg.org/readeck/go-readability/v2"
	"github.com/ethernity/obsidian-topic-stream/internal/domain"
	"github.com/ethernity/obsidian-topic-stream/internal/platform/httpfetch"
	"github.com/ethernity/obsidian-topic-stream/internal/service/ingest"
	"github.com/mmcdole/gofeed"
	feedextensions "github.com/mmcdole/gofeed/extensions"
	"golang.org/x/net/html/charset"
)

const defaultTrendingBaseURL = "https://raw.githubusercontent.com/devctllabs/github-trending-api/main/data/current"

const (
	defaultArxivAPIURL    = "https://export.arxiv.org/api/query"
	defaultYouTubeFeedURL = "https://www.youtube.com/feeds/videos.xml"
	defaultGitHubAPIURL   = "https://api.github.com"
	articleMaxBytes       = 16 << 20
	githubReadmeMaxBytes  = 2 << 20
	articleContentVersion = "readability-v1"
	githubContentVersion  = "github-trending-v2"
)

// HTTPClientFactory chooses an outbound client for a Source's private-network policy.
type HTTPClientFactory func(allowPrivateNetwork bool) *http.Client

// Options overrides Source adapter endpoints and clients; omitted values use the adapter defaults.
type Options struct {
	GitHubTrendingBaseURL string
	GitHubAPIBaseURL      string
	GitHubHTTPClient      *http.Client
	ArxivAPIURL           string
	YouTubeFeedURL        string
	SyndicationHTTPClient *http.Client
	ArticleHTTPClient     *http.Client
	HTTPClientForNetwork  HTTPClientFactory
}

// Reader polls supported Source kinds and enriches canonical article and repository content.
type Reader struct {
	trendingBaseURL      string
	githubAPIBaseURL     string
	githubHTTPClient     *http.Client
	arxivAPIURL          string
	youtubeFeedURL       string
	syndicationClient    *http.Client
	articleClient        *http.Client
	httpClientForNetwork HTTPClientFactory
}

// NewReaderWithOptions creates Source adapters with the supplied endpoint and client overrides.
func NewReaderWithOptions(options Options) *Reader {
	if options.HTTPClientForNetwork == nil {
		options.HTTPClientForNetwork = httpfetch.NewClient
	}
	if options.GitHubTrendingBaseURL == "" {
		options.GitHubTrendingBaseURL = defaultTrendingBaseURL
	}
	if options.GitHubHTTPClient == nil {
		options.GitHubHTTPClient = options.HTTPClientForNetwork(false)
	}
	if options.GitHubAPIBaseURL == "" {
		options.GitHubAPIBaseURL = defaultGitHubAPIURL
	}
	if options.ArxivAPIURL == "" {
		options.ArxivAPIURL = defaultArxivAPIURL
	}
	if options.YouTubeFeedURL == "" {
		options.YouTubeFeedURL = defaultYouTubeFeedURL
	}
	return &Reader{
		trendingBaseURL: strings.TrimRight(options.GitHubTrendingBaseURL, "/"), githubHTTPClient: options.GitHubHTTPClient,
		githubAPIBaseURL: strings.TrimRight(options.GitHubAPIBaseURL, "/"),
		arxivAPIURL:      options.ArxivAPIURL, youtubeFeedURL: options.YouTubeFeedURL,
		syndicationClient: options.SyndicationHTTPClient, articleClient: options.ArticleHTTPClient,
		httpClientForNetwork: options.HTTPClientForNetwork,
	}
}

// Read dispatches the Source to its kind-specific adapter and returns normalized upstream entries.
func (r *Reader) Read(ctx context.Context, source domain.Source) ([]ingest.Entry, error) {
	switch source.Config.Kind {
	case domain.SourceKindRSS, domain.SourceKindAtom:
		return r.readSyndication(ctx, source, source.Config.URL, source.Config.AllowPrivateNetwork)
	case domain.SourceKindArxiv:
		endpoint, err := url.Parse(r.arxivAPIURL)
		if err != nil {
			return nil, err
		}
		query := endpoint.Query()
		query.Set("search_query", source.Config.Query)
		query.Set("sortBy", "submittedDate")
		query.Set("sortOrder", "descending")
		query.Set("max_results", "100")
		endpoint.RawQuery = query.Encode()
		return r.readSyndication(ctx, source, endpoint.String(), false)
	case domain.SourceKindYoutube:
		endpoint, err := url.Parse(r.youtubeFeedURL)
		if err != nil {
			return nil, err
		}
		query := endpoint.Query()
		query.Set("channel_id", source.Config.ChannelID)
		endpoint.RawQuery = query.Encode()
		return r.readSyndication(ctx, source, endpoint.String(), false)
	case domain.SourceKindGithub:
		return r.readGitHubTrending(ctx, source.Config)
	default:
		return nil, fmt.Errorf("unsupported source kind %q", source.Config.Kind)
	}
}

func (r *Reader) readSyndication(ctx context.Context, source domain.Source, endpoint string, allowPrivateNetwork bool) ([]ingest.Entry, error) {
	parser := gofeed.NewParser()
	parser.Client = r.syndicationClient
	if parser.Client == nil {
		parser.Client = r.httpClientForNetwork(allowPrivateNetwork)
	}
	parser.MaxByteSize = 10 << 20
	parsed, err := parser.ParseURLWithContext(endpoint, ctx)
	if err != nil {
		return nil, err
	}
	entries := make([]ingest.Entry, 0, len(parsed.Items))
	for _, item := range parsed.Items {
		entry, err := r.syndicationEntry(ctx, source, item, allowPrivateNetwork)
		if err != nil {
			return nil, err
		}
		entries = append(entries, entry)
	}
	return entries, nil
}

func (r *Reader) syndicationEntry(
	ctx context.Context,
	source domain.Source,
	item *gofeed.Item,
	allowPrivateNetwork bool,
) (ingest.Entry, error) {
	entry := ingest.Entry{
		ID: item.GUID, Title: item.Title, URL: item.Link,
		Authors: syndicationAuthors(item.Authors), PublishedAt: item.PublishedParsed,
	}
	switch source.Config.Kind {
	case domain.SourceKindArxiv:
		return arxivEntry(entry, item), nil
	case domain.SourceKindYoutube:
		return youtubeEntry(entry, item, source.Config.ChannelID), nil
	default:
		return r.articleEntry(ctx, entry, item, allowPrivateNetwork)
	}
}

func syndicationAuthors(authors []*gofeed.Person) []string {
	result := make([]string, 0, len(authors))
	for _, author := range authors {
		if author != nil && author.Name != "" {
			result = append(result, author.Name)
		}
	}
	return result
}

func arxivEntry(entry ingest.Entry, item *gofeed.Item) ingest.Entry {
	entry.Kind = domain.ItemKindPaper
	entry.ExtractionVersion = "arxiv-atom-v1"
	entry.Details = domain.ItemDetails{Type: entry.Kind, Paper: &domain.PaperDetails{
		Abstract:   preferredText(item.Content, item.Description),
		Categories: append([]string(nil), item.Categories...), DOI: extensionPointer(item.Extensions, "arxiv", "doi"),
	}}
	return entry
}

func youtubeEntry(entry ingest.Entry, item *gofeed.Item, channelID string) ingest.Entry {
	description := preferredText(item.Content, item.Description)
	if description == "" {
		description = nestedExtensionValue(item.Extensions, "media", "group", "description")
	}
	entry.Kind = domain.ItemKindVideo
	entry.ExtractionVersion = "youtube-atom-v1"
	entry.Details = domain.ItemDetails{Type: entry.Kind, Video: &domain.VideoDetails{
		Description: description, ChannelID: channelID,
		VideoID: extensionValue(item.Extensions, "yt", "videoId", strings.TrimPrefix(item.GUID, "yt:video:")),
	}}
	return entry
}

type githubRepository struct {
	Description   *string  `json:"description"`
	Homepage      *string  `json:"homepage"`
	Language      *string  `json:"language"`
	Stars         *int     `json:"stargazers_count"`
	Forks         *int     `json:"forks_count"`
	OpenIssues    *int     `json:"open_issues_count"`
	Topics        []string `json:"topics"`
	DefaultBranch *string  `json:"default_branch"`
}

// Enrich extracts article content or repository metadata, reusing compatible previous extraction.
func (r *Reader) Enrich(
	ctx context.Context,
	entry ingest.Entry,
	previous *domain.Item,
) (ingest.Entry, error) {
	if entry.Kind != domain.ItemKindRepository || entry.Details.Repository == nil {
		return entry, nil
	}
	if reuseGitHubEnrichment(entry.Details.Repository, previous) {
		entry.ExtractionVersion = githubContentVersion
		return entry, nil
	}
	owner, name, err := repositoryIdentity(entry.ID)
	if err != nil {
		return ingest.Entry{}, err
	}
	metadata, err := r.githubRepository(ctx, owner, name)
	if err != nil {
		return ingest.Entry{}, err
	}
	readme, err := r.githubReadme(ctx, owner, name)
	if err != nil {
		return ingest.Entry{}, err
	}
	applyGitHubMetadata(entry.Details.Repository, metadata, readme)
	entry.ExtractionVersion = githubContentVersion
	return entry, nil
}

func reuseGitHubEnrichment(details *domain.RepositoryDetails, previous *domain.Item) bool {
	if previous == nil || previous.ExtractionVersion != githubContentVersion ||
		previous.Details.Repository == nil {
		return false
	}
	stored := previous.Details.Repository
	details.Description = stored.Description
	details.Homepage = stored.Homepage
	details.ProgrammingLanguage = stored.ProgrammingLanguage
	details.Stars = stored.Stars
	details.Forks = stored.Forks
	details.OpenIssues = stored.OpenIssues
	details.Topics = append([]string(nil), stored.Topics...)
	details.DefaultBranch = stored.DefaultBranch
	details.ReadmeMarkdown = stored.ReadmeMarkdown
	return true
}

func repositoryIdentity(value string) (string, string, error) {
	parts := strings.Split(value, "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", fmt.Errorf("invalid GitHub repository identity %q", value)
	}
	return parts[0], parts[1], nil
}

func (r *Reader) githubRepository(ctx context.Context, owner, name string) (githubRepository, error) {
	endpoint := fmt.Sprintf("%s/repos/%s/%s", r.githubAPIBaseURL, url.PathEscape(owner), url.PathEscape(name))
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return githubRepository{}, fmt.Errorf("create GitHub repository request: %w", err)
	}
	response, err := r.githubHTTPClient.Do(request)
	if err != nil {
		return githubRepository{}, fmt.Errorf("fetch GitHub repository: %w", err)
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return githubRepository{}, fmt.Errorf("GitHub repository returned %s", response.Status)
	}
	var metadata githubRepository
	decoder := json.NewDecoder(io.LimitReader(response.Body, 1<<20))
	if err := decoder.Decode(&metadata); err != nil {
		return githubRepository{}, fmt.Errorf("decode GitHub repository: %w", err)
	}
	return metadata, nil
}

func (r *Reader) githubReadme(ctx context.Context, owner, name string) (string, error) {
	endpoint := fmt.Sprintf("%s/repos/%s/%s/readme", r.githubAPIBaseURL, url.PathEscape(owner), url.PathEscape(name))
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return "", fmt.Errorf("create GitHub README request: %w", err)
	}
	request.Header.Set("Accept", "application/vnd.github.raw+json")
	response, err := r.githubHTTPClient.Do(request)
	if err != nil {
		return "", fmt.Errorf("fetch GitHub README: %w", err)
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode == http.StatusNotFound {
		return "", nil
	}
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return "", fmt.Errorf("GitHub README returned %s", response.Status)
	}
	readme, err := io.ReadAll(io.LimitReader(response.Body, githubReadmeMaxBytes+1))
	if err != nil {
		return "", fmt.Errorf("read GitHub README: %w", err)
	}
	if len(readme) > githubReadmeMaxBytes {
		return "", fmt.Errorf("GitHub README exceeds %d bytes", githubReadmeMaxBytes)
	}
	return string(readme), nil
}

func applyGitHubMetadata(details *domain.RepositoryDetails, metadata githubRepository, readme string) {
	if metadata.Description != nil {
		details.Description = metadata.Description
	}
	details.Homepage = metadata.Homepage
	details.ProgrammingLanguage = metadata.Language
	details.Stars = metadata.Stars
	details.Forks = metadata.Forks
	details.OpenIssues = metadata.OpenIssues
	details.Topics = append([]string(nil), metadata.Topics...)
	details.DefaultBranch = metadata.DefaultBranch
	details.ReadmeMarkdown = readme
}

func (r *Reader) articleEntry(
	ctx context.Context,
	entry ingest.Entry,
	item *gofeed.Item,
	allowPrivateNetwork bool,
) (ingest.Entry, error) {
	content, title, byline, err := r.extractArticle(ctx, item.Link, allowPrivateNetwork)
	if err != nil {
		return ingest.Entry{}, fmt.Errorf("extract article %q: %w", item.Link, err)
	}
	if entry.Title == "" {
		entry.Title = title
	}
	if len(entry.Authors) == 0 && byline != "" {
		entry.Authors = []string{byline}
	}
	entry.Kind = domain.ItemKindArticle
	entry.ExtractionVersion = articleContentVersion
	entry.Details = domain.ItemDetails{
		Type: entry.Kind, Article: &domain.ArticleDetails{Description: item.Description, Content: content},
	}
	return entry, nil
}

func (r *Reader) extractArticle(ctx context.Context, rawURL string, allowPrivateNetwork bool) (string, string, string, error) {
	pageURL, err := url.Parse(rawURL)
	if err != nil || pageURL.Scheme == "" || pageURL.Host == "" {
		return "", "", "", errors.New("article URL must be absolute")
	}
	raw, contentType, err := r.fetchArticle(ctx, pageURL, allowPrivateNetwork)
	if err != nil {
		return "", "", "", err
	}
	return parseArticle(pageURL, raw, contentType)
}

func (r *Reader) fetchArticle(
	ctx context.Context,
	pageURL *url.URL,
	allowPrivateNetwork bool,
) ([]byte, string, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, pageURL.String(), nil)
	if err != nil {
		return nil, "", fmt.Errorf("create article request: %w", err)
	}
	client := r.articleClient
	if client == nil {
		client = r.httpClientForNetwork(allowPrivateNetwork)
	}
	response, err := client.Do(request)
	if err != nil {
		return nil, "", fmt.Errorf("fetch article: %w", err)
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return nil, "", fmt.Errorf("fetch article returned status %d", response.StatusCode)
	}
	raw, err := io.ReadAll(io.LimitReader(response.Body, articleMaxBytes+1))
	if err != nil {
		return nil, "", fmt.Errorf("read article: %w", err)
	}
	if len(raw) > articleMaxBytes {
		return nil, "", fmt.Errorf("article exceeds %d bytes", articleMaxBytes)
	}
	return raw, response.Header.Get("Content-Type"), nil
}

func parseArticle(pageURL *url.URL, raw []byte, contentType string) (string, string, string, error) {
	decoded, err := charset.NewReader(bytes.NewReader(raw), contentType)
	if err != nil {
		return "", "", "", fmt.Errorf("decode article charset: %w", err)
	}
	article, err := readability.FromReader(decoded, pageURL)
	if err != nil {
		return "", "", "", fmt.Errorf("parse readable article: %w", err)
	}
	var content bytes.Buffer
	if err := article.RenderHTML(&content); err != nil {
		return "", "", "", fmt.Errorf("render readable article: %w", err)
	}
	if strings.TrimSpace(content.String()) == "" {
		return "", "", "", errors.New("readable article is empty")
	}
	return content.String(), article.Title(), article.Byline(), nil
}

type trendingSnapshot struct {
	SchemaVersion int               `json:"schemaVersion"`
	Kind          string            `json:"kind"`
	Period        string            `json:"period"`
	Language      string            `json:"language"`
	SourceURL     string            `json:"sourceUrl"`
	ObservedAt    time.Time         `json:"observedAt"`
	Status        string            `json:"status"`
	Issues        []json.RawMessage `json:"issues"`
	Items         []trendingItem    `json:"items"`
}

type trendingItem struct {
	Rank                int     `json:"rank"`
	FullName            string  `json:"fullName"`
	URL                 string  `json:"url"`
	Description         *string `json:"description"`
	ProgrammingLanguage *string `json:"programmingLanguage"`
	Stars               *int    `json:"stars"`
	Forks               *int    `json:"forks"`
	StarsInPeriod       *int    `json:"starsInPeriod"`
	// present distinguishes an omitted upstream field from a supplied null or zero value.
	present map[string]bool
}

// UnmarshalJSON decodes repository metadata and records field presence for snapshot validation.
func (item *trendingItem) UnmarshalJSON(data []byte) error {
	type alias trendingItem
	var decoded alias
	if err := json.Unmarshal(data, &decoded); err != nil {
		return err
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return err
	}
	*item = trendingItem(decoded)
	item.present = make(map[string]bool, len(fields))
	for name := range fields {
		item.present[name] = true
	}
	return nil
}

func (r *Reader) readGitHubTrending(ctx context.Context, config domain.SourceConfig) ([]ingest.Entry, error) {
	endpoint := fmt.Sprintf("%s/%s/%s.json", r.trendingBaseURL, config.Period, config.Language)
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	response, err := r.githubHTTPClient.Do(request)
	if err != nil {
		return nil, err
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, fmt.Errorf("GitHub Trending returned %s", response.Status)
	}
	decoder := json.NewDecoder(io.LimitReader(response.Body, 10<<20))
	var snapshot trendingSnapshot
	if err := decoder.Decode(&snapshot); err != nil {
		return nil, fmt.Errorf("decode GitHub Trending snapshot: %w", err)
	}
	if err := ensureJSONEnd(decoder); err != nil {
		return nil, err
	}
	if err := validateSnapshot(snapshot, config); err != nil {
		return nil, err
	}
	entries := make([]ingest.Entry, 0, len(snapshot.Items))
	for _, item := range snapshot.Items {
		entries = append(entries, ingest.Entry{
			ID: item.FullName, Title: item.FullName, URL: item.URL,
			Authors: []string{strings.SplitN(item.FullName, "/", 2)[0]}, PublishedAt: &snapshot.ObservedAt,
			ExtractionVersion: "github-trending-v1", Kind: domain.ItemKindRepository,
			Details: domain.ItemDetails{Type: domain.ItemKindRepository, Repository: &domain.RepositoryDetails{
				Description: item.Description, ProgrammingLanguage: item.ProgrammingLanguage,
				Stars: item.Stars, Forks: item.Forks, StarsInPeriod: item.StarsInPeriod,
				Rank: item.Rank, Period: snapshot.Period, ObservedAt: snapshot.ObservedAt, SnapshotStatus: snapshot.Status,
			}},
		})
	}
	return entries, nil
}

func validateSnapshot(snapshot trendingSnapshot, config domain.SourceConfig) error {
	if err := validateSnapshotMetadata(snapshot, config); err != nil {
		return err
	}
	identity := regexp.MustCompile(`^[^/\s]+/[^/\s]+$`)
	for _, item := range snapshot.Items {
		if err := validateTrendingItem(item, identity); err != nil {
			return err
		}
	}
	return nil
}

func validateSnapshotMetadata(snapshot trendingSnapshot, config domain.SourceConfig) error {
	if snapshot.SchemaVersion != 1 {
		return fmt.Errorf("unsupported GitHub Trending schemaVersion %d", snapshot.SchemaVersion)
	}
	if snapshot.Kind != "snapshot" {
		return fmt.Errorf("unexpected GitHub Trending kind %q", snapshot.Kind)
	}
	if !snapshotMatchesConfig(snapshot, config) {
		return errors.New("GitHub Trending snapshot does not match source configuration")
	}
	if !snapshotMetadataComplete(snapshot) {
		return errors.New("GitHub Trending snapshot metadata is incomplete")
	}
	if !validSnapshotStatus(snapshot.Status) {
		return fmt.Errorf("unexpected GitHub Trending status %q", snapshot.Status)
	}
	if snapshot.Issues == nil || snapshot.Items == nil {
		return errors.New("GitHub Trending snapshot arrays must be present")
	}
	return nil
}

func snapshotMatchesConfig(snapshot trendingSnapshot, config domain.SourceConfig) bool {
	return snapshot.Period == config.Period && snapshot.Language == config.Language
}

func snapshotMetadataComplete(snapshot trendingSnapshot) bool {
	return !snapshot.ObservedAt.IsZero() && snapshot.SourceURL != ""
}

func validSnapshotStatus(status string) bool {
	return status == "complete" || status == "degraded"
}

func validateTrendingItem(item trendingItem, identity *regexp.Regexp) error {
	if missing := missingTrendingField(item); missing != "" {
		return fmt.Errorf("GitHub Trending item %q is missing %s", item.FullName, missing)
	}
	if !validTrendingIdentity(item, identity) {
		return fmt.Errorf("invalid GitHub Trending repository identity %q", item.FullName)
	}
	if !validGitHubURL(item.URL) {
		return fmt.Errorf("invalid GitHub repository URL %q", item.URL)
	}
	if hasNegativeMetric(item) {
		return fmt.Errorf("negative metric for GitHub repository %q", item.FullName)
	}
	return nil
}

func missingTrendingField(item trendingItem) string {
	for _, required := range []string{"rank", "fullName", "url", "description", "programmingLanguage", "stars", "forks"} {
		if !item.present[required] {
			return required
		}
	}
	return ""
}

func validTrendingIdentity(item trendingItem, identity *regexp.Regexp) bool {
	return item.Rank >= 1 && identity.MatchString(item.FullName)
}

func validGitHubURL(raw string) bool {
	parsed, err := url.Parse(raw)
	return err == nil && parsed.Scheme == "https" && strings.EqualFold(parsed.Hostname(), "github.com")
}

func hasNegativeMetric(item trendingItem) bool {
	for _, metric := range []*int{item.Stars, item.Forks, item.StarsInPeriod} {
		if metric != nil && *metric < 0 {
			return true
		}
	}
	return false
}

func ensureJSONEnd(decoder *json.Decoder) error {
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return errors.New("GitHub Trending response must contain one JSON document")
	}
	return nil
}

func preferredText(content, description string) string {
	if content != "" {
		return content
	}
	return description
}

func extensionValue(values map[string]map[string][]feedextensions.Extension, namespace, name, fallback string) string {
	if matches := values[namespace][name]; len(matches) > 0 && matches[0].Value != "" {
		return matches[0].Value
	}
	return fallback
}

func extensionPointer(values map[string]map[string][]feedextensions.Extension, namespace, name string) *string {
	value := extensionValue(values, namespace, name, "")
	if value == "" {
		return nil
	}
	return &value
}

func nestedExtensionValue(
	values map[string]map[string][]feedextensions.Extension,
	namespace, parent, child string,
) string {
	parents := values[namespace][parent]
	if len(parents) == 0 {
		return ""
	}
	children := parents[0].Children[child]
	if len(children) == 0 {
		return ""
	}
	return children[0].Value
}
