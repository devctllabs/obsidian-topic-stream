package search_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/ethernity/obsidian-topic-stream/internal/domain"
	"github.com/ethernity/obsidian-topic-stream/internal/service/ranking"
	"github.com/ethernity/obsidian-topic-stream/internal/service/search"
	"github.com/ethernity/obsidian-topic-stream/internal/service/search/mocks"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

type indexStub struct {
	dense, sparse, recent []ranking.IndexHit
	filters               []ranking.IndexFilter
	personal              ranking.PersonalRetrieval
	personalCalls         int
	denseCalls            int
	sparseCalls           int
	denseLimits           []int
	sparseLimits          []int
	recentLimits          []int
}

func TestSearchCollectsInvalidFieldsWithoutRetrieval(t *testing.T) {
	t.Parallel()
	index := &indexStub{}
	service := newSearchService(t, search.Config{WorkspaceID: uuid.New(), Repository: searchItems(t, nil), Index: index, Settings: fixedSearchSettings(domain.RetrievalSettings{})})
	_, err := service.Find(t.Context(), search.Request{Query: strings.Repeat("x", 1001), Limit: 0, Mode: "unknown", Cursor: "invalid"})
	var validation *domain.ValidationError
	require.ErrorAs(t, err, &validation)
	require.ErrorIs(t, err, search.ErrInvalidRequest)
	require.Len(t, validation.Violations, 4)
	require.Zero(t, index.denseCalls)
	require.Zero(t, index.sparseCalls)
}

// RetrievePersonal returns configured personal-feedback evidence.
func (s *indexStub) RetrievePersonal(
	_ context.Context,
	_ ranking.PersonalIndexRequest,
) (ranking.PersonalRetrieval, error) {
	s.personalCalls++
	return s.personal, nil
}

// QueryDenseText returns the fixture's dense retrieval results.
func (s *indexStub) QueryDenseText(_ context.Context, _ string, filter ranking.IndexFilter, limit int) ([]ranking.IndexHit, error) {
	s.denseCalls++
	s.denseLimits = append(s.denseLimits, limit)
	s.filters = append(s.filters, filter)
	return append([]ranking.IndexHit(nil), s.dense...), nil
}

// QuerySparse returns the fixture's lexical retrieval results.
func (s *indexStub) QuerySparse(_ context.Context, _ string, filter ranking.IndexFilter, limit int) ([]ranking.IndexHit, error) {
	s.sparseCalls++
	s.sparseLimits = append(s.sparseLimits, limit)
	s.filters = append(s.filters, filter)
	return append([]ranking.IndexHit(nil), s.sparse...), nil
}

// QueryRecent returns the fixture's chronological retrieval results.
func (s *indexStub) QueryRecent(_ context.Context, filter ranking.IndexFilter, limit int) ([]ranking.IndexHit, error) {
	s.recentLimits = append(s.recentLimits, limit)
	s.filters = append(s.filters, filter)
	return append([]ranking.IndexHit(nil), s.recent...), nil
}

type searchTopicRankerStub struct {
	result   ranking.TopicResult
	requests []ranking.TopicRequest
}

// RankTopic returns the fixture's Topic ranking result.
func (s *searchTopicRankerStub) RankTopic(_ context.Context, request ranking.TopicRequest) (ranking.TopicResult, error) {
	s.requests = append(s.requests, request)
	return s.result, nil
}

type searchSettingsStub struct {
	values []domain.WorkspaceSettings
	calls  int
}

// GetWorkspace returns the fixture's Workspace Settings snapshot.
func (s *searchSettingsStub) GetWorkspace(context.Context) (domain.WorkspaceSettings, error) {
	value := s.values[min(s.calls, len(s.values)-1)]
	s.calls++
	return value, nil
}

func fixedSearchSettings(retrieval domain.RetrievalSettings) *searchSettingsStub {
	return &searchSettingsStub{values: []domain.WorkspaceSettings{{Retrieval: retrieval}}}
}

func TestHybridSearchFusesRetrievalAndHydratesCanonicalItems(t *testing.T) {
	t.Parallel()

	workspaceID, sourceID := uuid.New(), uuid.New()
	now := time.Date(2026, time.September, 27, 12, 0, 0, 0, time.UTC)
	first := searchableItem(workspaceID, sourceID, "PostgreSQL MVCC", now.Add(-time.Hour))
	second := searchableItem(workspaceID, sourceID, "Query planners", now.Add(-2*time.Hour))
	repository := searchItems(t, []domain.Item{first, second})
	index := &indexStub{
		dense:  []ranking.IndexHit{{ItemID: first.ID, SourceID: sourceID, PublishedAt: first.PublishedAt, Embedding: []float32{1, 0}, Score: 0.9}, {ItemID: second.ID, SourceID: sourceID, Embedding: []float32{0, 1}, Score: 0.8}},
		sparse: []ranking.IndexHit{{ItemID: first.ID, SourceID: sourceID, PublishedAt: first.PublishedAt, Embedding: []float32{1, 0}, Score: 5.0}},
	}
	service := newSearchService(t, search.Config{WorkspaceID: workspaceID, Repository: repository, Index: index, Settings: fixedSearchSettings(domain.RetrievalSettings{
		CandidateLimit: 50, RRFK: 60, RecencyHalfLifeDays: 7, RecencyMaxBoost: 0.1,
	})})
	publishedAfter := now.Add(-24 * time.Hour)

	page, err := service.Find(context.Background(), search.Request{
		Query: "PostgreSQL internals", Mode: search.ModeHybrid, Limit: 10,
		SourceID: &sourceID, PublishedAfter: &publishedAfter,
	})

	require.NoError(t, err)
	require.Len(t, page.Items, 2)
	require.Equal(t, first.ID, page.Items[0].Item.ID)
	require.Contains(t, page.Items[0].Signals, "dense")
	require.Contains(t, page.Items[0].Signals, "sparse")
	require.Contains(t, page.Items[0].Signals, "rrf.dense")
	require.Contains(t, page.Items[0].Signals, "rrf.sparse")
	require.Nil(t, page.NextCursor)
	require.Len(t, index.filters, 2)
	for _, filter := range index.filters {
		require.Equal(t, workspaceID, filter.WorkspaceID)
		require.Equal(t, []uuid.UUID{sourceID}, filter.SourceIDs)
		require.Equal(t, publishedAfter, *filter.PublishedAfter)
	}
}

func TestSearchCursorPaginatesFusedResults(t *testing.T) {
	t.Parallel()

	workspaceID, sourceID := uuid.New(), uuid.New()
	now := time.Date(2026, time.September, 27, 12, 0, 0, 0, time.UTC)
	items := []domain.Item{
		searchableItem(workspaceID, sourceID, "One", now),
		searchableItem(workspaceID, sourceID, "Two", now.Add(-time.Hour)),
		searchableItem(workspaceID, sourceID, "Three", now.Add(-2*time.Hour)),
	}
	hits := make([]ranking.IndexHit, len(items))
	for index, item := range items {
		hits[index] = ranking.IndexHit{ItemID: item.ID, SourceID: sourceID, Score: float64(len(items) - index)}
	}
	index := &indexStub{sparse: hits}
	service := newSearchService(t, search.Config{
		WorkspaceID: workspaceID, Repository: searchItems(t, items), Index: index,
		Settings: fixedSearchSettings(domain.RetrievalSettings{CandidateLimit: 50, RRFK: 60}),
	})

	first, err := service.Find(context.Background(), search.Request{Query: "query", Mode: search.ModeLexical, Limit: 2})
	require.NoError(t, err)
	require.Len(t, first.Items, 2)
	require.NotNil(t, first.NextCursor)
	second, err := service.Find(context.Background(), search.Request{Query: "query", Mode: search.ModeLexical, Limit: 2, Cursor: *first.NextCursor})
	require.NoError(t, err)
	require.Len(t, second.Items, 1)
	require.Equal(t, items[2].ID, second.Items[0].Item.ID)
	require.Nil(t, second.NextCursor)
}

func TestNonEmptySearchCapsCandidateWorkAtConfiguredLimit(t *testing.T) {
	t.Parallel()

	workspaceID, sourceID := uuid.New(), uuid.New()
	items := []domain.Item{
		searchableItem(workspaceID, sourceID, "One", time.Now()),
		searchableItem(workspaceID, sourceID, "Two", time.Now()),
		searchableItem(workspaceID, sourceID, "Three", time.Now()),
	}
	index := &indexStub{sparse: []ranking.IndexHit{
		{ItemID: items[0].ID, Score: 3}, {ItemID: items[1].ID, Score: 2}, {ItemID: items[2].ID, Score: 1},
	}}
	service := newSearchService(t, search.Config{
		WorkspaceID: workspaceID, Repository: searchItems(t, items), Index: index,
		Settings: fixedSearchSettings(domain.RetrievalSettings{CandidateLimit: 2, RRFK: 60}),
	})

	page, err := service.Find(t.Context(), search.Request{Query: "systems", Mode: search.ModeLexical, Limit: 10})

	require.NoError(t, err)
	require.Len(t, page.Items, 2)
	require.Equal(t, []int{2}, index.sparseLimits)
}

func TestSearchReadsOneLiveSettingsSnapshotPerFind(t *testing.T) {
	t.Parallel()

	workspaceID := uuid.New()
	item := searchableItem(workspaceID, uuid.New(), "Systems", time.Now())
	index := &indexStub{sparse: []ranking.IndexHit{{ItemID: item.ID, Score: 1}}}
	settings := &searchSettingsStub{values: []domain.WorkspaceSettings{
		{Version: 2, Retrieval: domain.RetrievalSettings{CandidateLimit: 20, RRFK: 40}},
		{Version: 3, Retrieval: domain.RetrievalSettings{CandidateLimit: 30, RRFK: 60}},
	}}
	service := newSearchService(t, search.Config{
		WorkspaceID: workspaceID, Repository: searchItems(t, []domain.Item{item}),
		Index: index, Settings: settings,
	})

	_, err := service.Find(t.Context(), search.Request{Query: "systems", Mode: search.ModeLexical, Limit: 10})
	require.NoError(t, err)
	_, err = service.Find(t.Context(), search.Request{Query: "systems", Mode: search.ModeLexical, Limit: 10})
	require.NoError(t, err)

	require.Equal(t, 2, settings.calls)
	require.Equal(t, []int{20, 30}, index.sparseLimits)
}

func TestSearchDoesNotApplyFeedFreshnessBoost(t *testing.T) {
	t.Parallel()

	workspaceID, sourceID := uuid.New(), uuid.New()
	now := time.Date(2026, time.September, 27, 12, 0, 0, 0, time.UTC)
	older := searchableItem(workspaceID, sourceID, "Exact older result", now.Add(-365*24*time.Hour))
	newer := searchableItem(workspaceID, sourceID, "Weaker new result", now)
	index := &indexStub{sparse: []ranking.IndexHit{
		{ItemID: older.ID, SourceID: sourceID, PublishedAt: older.PublishedAt, Score: 5},
		{ItemID: newer.ID, SourceID: sourceID, PublishedAt: newer.PublishedAt, Score: 4},
	}}
	service := newSearchService(t, search.Config{
		WorkspaceID: workspaceID, Repository: searchItems(t, []domain.Item{older, newer}), Index: index,
		Settings: fixedSearchSettings(domain.RetrievalSettings{
			CandidateLimit: 50, RRFK: 60, RecencyHalfLifeDays: 7, RecencyMaxBoost: 1,
		}),
	})

	page, err := service.Find(t.Context(), search.Request{Query: "exact", Mode: search.ModeLexical, Limit: 10})

	require.NoError(t, err)
	require.Equal(t, older.ID, page.Items[0].Item.ID)
}

func TestSearchAppliesPersonalSignalsWithoutHidingNegativeMatches(t *testing.T) {
	t.Parallel()

	workspaceID, sourceID := uuid.New(), uuid.New()
	now := time.Date(2026, time.September, 27, 12, 0, 0, 0, time.UTC)
	liked := searchableItem(workspaceID, sourceID, "Liked", now)
	disliked := searchableItem(workspaceID, sourceID, "Disliked", now)
	liked.Feedback = &domain.Feedback{Sentiment: "like", UpdatedAt: now}
	disliked.Feedback = &domain.Feedback{Sentiment: "dislike", UpdatedAt: now}
	index := &indexStub{
		sparse: []ranking.IndexHit{
			{ItemID: disliked.ID, SourceID: sourceID, Score: 2},
			{ItemID: liked.ID, SourceID: sourceID, Score: 1},
		},
		personal: ranking.PersonalRetrieval{
			Positive: []ranking.FeedbackNeighborhood{{Feedback: domain.ItemFeedback{ItemID: liked.ID, Feedback: *liked.Feedback}}},
			Negative: []ranking.FeedbackNeighborhood{{Feedback: domain.ItemFeedback{ItemID: disliked.ID, Feedback: *disliked.Feedback}}},
		},
	}
	service := newSearchService(t, search.Config{
		WorkspaceID: workspaceID, Repository: searchItems(t, []domain.Item{liked, disliked}), Index: index,
		Settings: fixedSearchSettings(domain.RetrievalSettings{CandidateLimit: 50, RRFK: 60}),
	})

	page, err := service.Find(t.Context(), search.Request{Query: "systems", Mode: search.ModeLexical, Limit: 10})

	require.NoError(t, err)
	require.Equal(t, []uuid.UUID{liked.ID, disliked.ID}, []uuid.UUID{page.Items[0].Item.ID, page.Items[1].Item.ID})
	require.Equal(t, 1.0, page.Items[0].Signals["personal.directLike"])
	require.Equal(t, 1.0, page.Items[1].Signals["personal.directDislike"])
	require.Equal(t, 1, index.personalCalls)
}

func TestEmptySearchReturnsNewestArchiveWithoutRetrievalOrPersonalization(t *testing.T) {
	t.Parallel()

	workspaceID, sourceID := uuid.New(), uuid.New()
	now := time.Date(2026, time.September, 27, 12, 0, 0, 0, time.UTC)
	newest := searchableItem(workspaceID, sourceID, "Newest", now)
	older := searchableItem(workspaceID, sourceID, "Older", now.Add(-time.Hour))
	index := &indexStub{recent: []ranking.IndexHit{
		{ItemID: newest.ID, SourceID: sourceID, PublishedAt: newest.PublishedAt, Score: 1},
		{ItemID: older.ID, SourceID: sourceID, PublishedAt: older.PublishedAt, Score: 0.5},
	}}
	service := newSearchService(t, search.Config{
		WorkspaceID: workspaceID, Repository: searchItems(t, []domain.Item{newest, older}), Index: index,
		Settings: fixedSearchSettings(domain.RetrievalSettings{CandidateLimit: 50, RRFK: 60}),
	})

	page, err := service.Find(t.Context(), search.Request{Limit: 20})

	require.NoError(t, err)
	require.Equal(t, []uuid.UUID{newest.ID, older.ID}, []uuid.UUID{page.Items[0].Item.ID, page.Items[1].Item.ID})
	require.Zero(t, index.denseCalls)
	require.Zero(t, index.sparseCalls)
	require.Zero(t, index.personalCalls)
	require.Equal(t, []int{100}, index.recentLimits)
}

func TestSearchRetainsHiddenMatchesAndAppliesTopicMembership(t *testing.T) {
	t.Parallel()

	workspaceID, sourceID, topicID := uuid.New(), uuid.New(), uuid.New()
	now := time.Date(2026, time.September, 27, 12, 0, 0, 0, time.UTC)
	hidden := searchableItem(workspaceID, sourceID, "Hidden but searchable", now)
	hidden.Visibility = domain.ItemVisibilityHidden
	excluded := searchableItem(workspaceID, sourceID, "Outside topic", now)
	index := &indexStub{sparse: []ranking.IndexHit{
		{ItemID: hidden.ID, SourceID: sourceID, Score: 2},
		{ItemID: excluded.ID, SourceID: sourceID, Score: 1},
	}}
	topics := mocks.NewMockTopics(gomock.NewController(t))
	topics.EXPECT().GetTopic(gomock.Any(), workspaceID, topicID).Return(domain.Topic{ID: topicID, WorkspaceID: workspaceID, Name: "Databases", Enabled: true}, true, nil)
	ranker := &searchTopicRankerStub{result: ranking.TopicResult{
		Candidates: []ranking.Candidate{{ItemID: hidden.ID.String()}},
	}}
	settings := fixedSearchSettings(domain.RetrievalSettings{CandidateLimit: 50, RRFK: 60})
	service := newSearchService(t, search.Config{
		WorkspaceID: workspaceID, Repository: searchItems(t, []domain.Item{hidden, excluded}), Index: index,
		Topics: topics, Ranker: ranker,
		Settings: settings,
	})

	page, err := service.Find(t.Context(), search.Request{
		Query: "database", Mode: search.ModeLexical, Limit: 20, TopicID: &topicID,
	})

	require.NoError(t, err)
	require.Len(t, page.Items, 1)
	require.Equal(t, hidden.ID, page.Items[0].Item.ID)
	require.Equal(t, domain.ItemVisibilityHidden, page.Items[0].Item.Visibility)
	require.Contains(t, page.Items[0].Item.TopicIDs, topicID)
	require.Equal(t, 1, settings.calls)
	require.NotNil(t, ranker.requests[0].WorkspaceSettings)
	require.Equal(t, 50, ranker.requests[0].WorkspaceSettings.Retrieval.CandidateLimit)
}

func newSearchService(t *testing.T, config search.Config) *search.Service {
	t.Helper()
	if config.Now == nil {
		config.Now = time.Now
	}
	if config.Topics == nil {
		config.Topics = mocks.NewMockTopics(gomock.NewController(t))
	}
	if config.Ranker == nil {
		config.Ranker = &searchTopicRankerStub{}
	}
	service, err := search.New(config)
	require.NoError(t, err)
	return service
}

func searchableItem(workspaceID, sourceID uuid.UUID, title string, publishedAt time.Time) domain.Item {
	return domain.Item{
		ID: uuid.New(), WorkspaceID: workspaceID, SourceIDs: []uuid.UUID{sourceID}, Kind: domain.ItemKindArticle,
		Details: domain.ItemDetails{Type: domain.ItemKindArticle, Article: &domain.ArticleDetails{}},
		Title:   title, URL: "https://example.com/" + uuid.NewString(), CanonicalURL: "https://example.com/" + uuid.NewString(),
		PublishedAt: &publishedAt, DiscoveredAt: publishedAt, ReadingState: domain.ReadingStateUnread, Visibility: domain.ItemVisibilityVisible,
	}
}

func searchItems(t *testing.T, items []domain.Item) *mocks.MockItemRepository {
	t.Helper()
	repository := mocks.NewMockItemRepository(gomock.NewController(t))
	for _, item := range items {
		repository.EXPECT().GetItem(gomock.Any(), item.WorkspaceID, item.ID).Return(item, true, nil).AnyTimes()
	}
	return repository
}
