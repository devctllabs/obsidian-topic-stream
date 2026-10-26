package ingest

//go:generate go tool mockgen -source=service.go -destination=mocks/service.gen.go -package=mocks

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"
	"unicode/utf8"

	htmltomarkdown "github.com/JohannesKaufmann/html-to-markdown/v2"
	"github.com/ethernity/obsidian-topic-stream/internal/domain"
	"github.com/google/uuid"
)

// Entry carries one typed upstream occurrence. Kind and Details.Type must match exactly one details
// variant before normalization.
type Entry struct {
	ID                string
	Title             string
	URL               string
	Authors           []string
	PublishedAt       *time.Time
	ExtractionVersion string
	Kind              domain.ItemKind
	Details           domain.ItemDetails
}

// Reader reads upstream Source Entries before canonical persistence.
type Reader interface {
	// Read reads upstream entries for the supplied Source.
	Read(ctx context.Context, source domain.Source) ([]Entry, error)
}

// Enricher optionally enriches an entry using previously extracted Item content.
type Enricher interface {
	// Enrich extracts canonical content, reusing previous extraction when the adapter permits it.
	Enrich(ctx context.Context, entry Entry, previous *domain.Item) (Entry, error)
}

// Writer persists canonical Items and Source Entry provenance.
type Writer interface {
	// UpsertItem reports whether canonical content, Source Entry provenance, or pending feedback
	// changed. The ingest service uses this signal to trigger downstream indexing.
	UpsertItem(ctx context.Context, item domain.Item, entry domain.SourceEntry) (domain.ItemProjectionCommit, bool, error)
	// GetItem returns the Workspace Item and whether it exists.
	GetItem(ctx context.Context, workspaceID uuid.UUID, itemID uuid.UUID) (domain.Item, bool, error)
}

// Indexer enqueues durable projection delivery after an ingest commit.
type Indexer interface {
	// Enqueue submits durable Item projection work after the canonical commit.
	Enqueue(ctx context.Context, commit domain.ItemProjectionCommit) (domain.Job, error)
}

// Result counts read entries, changed writes, and unchanged canonical observations.
type Result struct {
	Read      int
	Stored    int
	Unchanged int
}

// Service turns upstream entries into canonical Items and enqueues changed projections.
type Service struct {
	reader  Reader
	writer  Writer
	indexer Indexer
	now     func() time.Time
}

// New binds Source reading and canonical persistence; a nil indexer disables projection enqueueing.
func New(reader Reader, writer Writer, indexer Indexer, now func() time.Time) Service {
	return Service{reader: reader, writer: writer, indexer: indexer, now: now}
}

// Sync reads and enriches Source entries, persists canonical changes, and enqueues their projections.
func (s Service) Sync(ctx context.Context, source domain.Source) (Result, error) {
	entries, err := s.reader.Read(ctx, source)
	if err != nil {
		return Result{}, fmt.Errorf("read %s source: %w", source.Config.Kind, err)
	}
	result := Result{Read: len(entries)}
	for _, entry := range entries {
		changed, err := s.syncEntry(ctx, source, entry)
		if err != nil {
			return result, err
		}
		if changed {
			result.Stored++
		} else {
			result.Unchanged++
		}
	}
	return result, nil
}

func (s Service) syncEntry(ctx context.Context, source domain.Source, entry Entry) (bool, error) {
	item, sourceEntry, err := normalize(source, entry, s.now())
	if err != nil {
		return false, fmt.Errorf("normalize source entry %q: %w", entry.ID, err)
	}
	if enricher, ok := s.reader.(Enricher); ok {
		previous, err := s.previousItem(ctx, item)
		if err != nil {
			return false, fmt.Errorf("load source entry %q for enrichment: %w", entry.ID, err)
		}
		entry, err = enricher.Enrich(ctx, entry, previous)
		if err != nil {
			return false, fmt.Errorf("enrich source entry %q: %w", entry.ID, err)
		}
		item, sourceEntry, err = normalize(source, entry, s.now())
		if err != nil {
			return false, fmt.Errorf("normalize enriched source entry %q: %w", entry.ID, err)
		}
	}
	commit, changed, err := s.writer.UpsertItem(ctx, item, sourceEntry)
	if err != nil {
		return false, fmt.Errorf("store source entry %q: %w", entry.ID, err)
	}
	if commit.ProjectionRevision != uuid.Nil && s.indexer != nil {
		if _, err := s.indexer.Enqueue(ctx, commit); err != nil {
			return false, fmt.Errorf("enqueue source entry %q for indexing: %w", entry.ID, err)
		}
	}
	return changed, nil
}

func (s Service) previousItem(ctx context.Context, item domain.Item) (*domain.Item, error) {
	previous, found, err := s.writer.GetItem(ctx, item.WorkspaceID, item.ID)
	if err != nil || !found {
		return nil, err
	}
	return &previous, nil
}

func normalize(source domain.Source, entry Entry, discoveredAt time.Time) (domain.Item, domain.SourceEntry, error) {
	canonicalURL, err := canonicalizeURL(entry.URL)
	if err != nil {
		return domain.Item{}, domain.SourceEntry{}, err
	}
	representation, err := representEntry(entry)
	if err != nil {
		return domain.Item{}, domain.SourceEntry{}, err
	}
	markdown, err := contentMarkdown(entry.Kind, representation.rawContent)
	if err != nil {
		return domain.Item{}, domain.SourceEntry{}, err
	}
	markdown = strings.TrimSpace(markdown)
	origin := representation.origin
	if markdown == "" {
		origin = domain.ContentOriginEmpty
	}
	identity := entry.ID
	if identity == "" {
		identity = canonicalURL
	}
	// Canonical Item identity deliberately excludes the Source so the same URL can retain multiple
	// Source Entries without duplicating the Item.
	itemID := uuid.NewSHA1(uuid.NameSpaceURL, []byte(source.WorkspaceID.String()+":"+canonicalURL))
	item := domain.Item{
		ID:                itemID,
		WorkspaceID:       source.WorkspaceID,
		SourceIDs:         []uuid.UUID{source.ID},
		Kind:              entry.Kind,
		Details:           entry.Details,
		Title:             strings.TrimSpace(entry.Title),
		URL:               entry.URL,
		CanonicalURL:      canonicalURL,
		Excerpt:           excerpt(markdown, 280),
		ContentMarkdown:   markdown,
		ContentOrigin:     origin,
		ExtractionVersion: entry.ExtractionVersion,
		Authors:           append([]string(nil), entry.Authors...),
		PublishedAt:       entry.PublishedAt,
		DiscoveredAt:      discoveredAt,
		ReadingState:      domain.ReadingStateUnread,
		Visibility:        domain.ItemVisibilityVisible,
	}
	sourceEntry := domain.SourceEntry{
		SourceID: source.ID, ExternalID: identity, ItemID: itemID, URL: entry.URL,
		Details: representation.details, ExtractionVersion: entry.ExtractionVersion,
		PublishedAt: entry.PublishedAt, DiscoveredAt: discoveredAt, UpdatedAt: discoveredAt,
	}
	return item, sourceEntry, nil
}

func contentMarkdown(kind domain.ItemKind, content string) (string, error) {
	if kind == domain.ItemKindRepository {
		return content, nil
	}
	markdown, err := htmltomarkdown.ConvertString(content)
	if err != nil {
		return "", fmt.Errorf("convert content to Markdown: %w", err)
	}
	return markdown, nil
}

type entryRepresentation struct {
	origin     domain.ContentOrigin
	rawContent string
	details    domain.SourceEntryDetails
}

func representEntry(entry Entry) (entryRepresentation, error) {
	if entry.Details.Type != entry.Kind {
		return entryRepresentation{}, errors.New("entry kind and details type do not match")
	}
	if itemDetailsVariantCount(entry.Details) != 1 {
		return entryRepresentation{}, errors.New("entry details must contain exactly one variant")
	}
	switch entry.Kind {
	case domain.ItemKindArticle:
		return articleRepresentation(entry.Details.Article)
	case domain.ItemKindPaper:
		return paperRepresentation(entry.Details.Paper)
	case domain.ItemKindVideo:
		return videoRepresentation(entry.Details.Video)
	case domain.ItemKindRepository:
		return repositoryRepresentation(entry.Details.Repository)
	default:
		return entryRepresentation{}, fmt.Errorf("unsupported entry kind %q", entry.Kind)
	}
}

func itemDetailsVariantCount(details domain.ItemDetails) int {
	count := 0
	for _, present := range []bool{
		details.Article != nil,
		details.Paper != nil,
		details.Video != nil,
		details.Repository != nil,
	} {
		if present {
			count++
		}
	}
	return count
}

func articleRepresentation(details *domain.ArticleDetails) (entryRepresentation, error) {
	if details == nil {
		return entryRepresentation{}, errors.New("article details are required")
	}
	content := details.Content
	origin := domain.ContentOriginArticleBody
	if content == "" {
		content = details.Description
		origin = domain.ContentOriginArticleDescription
	}
	return entryRepresentation{
		rawContent: content,
		origin:     origin,
		details: domain.SourceEntryDetails{Type: domain.ItemKindArticle, Article: &domain.SourceArticleDetails{
			Description: details.Description,
		}},
	}, nil
}

func paperRepresentation(details *domain.PaperDetails) (entryRepresentation, error) {
	if details == nil {
		return entryRepresentation{}, errors.New("paper details are required")
	}
	return entryRepresentation{
		rawContent: details.Abstract,
		origin:     domain.ContentOriginPaperAbstract,
		details: domain.SourceEntryDetails{Type: domain.ItemKindPaper, Paper: &domain.SourcePaperDetails{
			Abstract: details.Abstract, Categories: append([]string(nil), details.Categories...), DOI: details.DOI,
		}},
	}, nil
}

func videoRepresentation(details *domain.VideoDetails) (entryRepresentation, error) {
	if details == nil {
		return entryRepresentation{}, errors.New("video details are required")
	}
	return entryRepresentation{
		rawContent: details.Description,
		origin:     domain.ContentOriginVideoDescription,
		details: domain.SourceEntryDetails{Type: domain.ItemKindVideo, Video: &domain.SourceVideoDetails{
			Description: details.Description, ChannelID: details.ChannelID, VideoID: details.VideoID,
		}},
	}, nil
}

func repositoryRepresentation(details *domain.RepositoryDetails) (entryRepresentation, error) {
	if details == nil {
		return entryRepresentation{}, errors.New("repository details are required")
	}
	origin := domain.ContentOriginRepositoryDescription
	content := make([]string, 0, 2)
	if details.Description != nil {
		content = append(content, *details.Description)
	}
	if details.ReadmeMarkdown != "" {
		origin = domain.ContentOriginRepositoryReadme
		content = append(content, details.ReadmeMarkdown)
	}
	return entryRepresentation{
		rawContent: strings.Join(content, "\n\n"),
		origin:     origin,
		details: domain.SourceEntryDetails{Type: domain.ItemKindRepository, Repository: &domain.SourceRepositoryDetails{
			Description: details.Description, Homepage: details.Homepage,
			ProgrammingLanguage: details.ProgrammingLanguage, Stars: details.Stars,
			Forks: details.Forks, OpenIssues: details.OpenIssues, Topics: append([]string(nil), details.Topics...),
			DefaultBranch: details.DefaultBranch, StarsInPeriod: details.StarsInPeriod,
			Rank: details.Rank, Period: details.Period, ObservedAt: details.ObservedAt,
			SnapshotStatus: details.SnapshotStatus,
		}},
	}, nil
}

func canonicalizeURL(raw string) (string, error) {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return "", fmt.Errorf("invalid article URL %q", raw)
	}
	parsed.Fragment = ""
	query := parsed.Query()
	for key := range query {
		lower := strings.ToLower(key)
		if strings.HasPrefix(lower, "utm_") || lower == "fbclid" || lower == "gclid" {
			query.Del(key)
		}
	}
	parsed.RawQuery = query.Encode()
	return parsed.String(), nil
}

func excerpt(markdown string, limit int) string {
	if utf8.RuneCountInString(markdown) <= limit {
		return markdown
	}
	runes := []rune(markdown)
	return strings.TrimSpace(string(runes[:limit])) + "…"
}
