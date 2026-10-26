package search

//go:generate go tool mockgen -source=service.go -destination=mocks/service.gen.go -package=mocks

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/ethernity/obsidian-topic-stream/internal/domain"
	"github.com/ethernity/obsidian-topic-stream/internal/service/ranking"
	"github.com/google/uuid"
)

var (
	ErrInvalidRequest = errors.New("invalid search request")
	ErrTopicNotFound  = errors.New("search topic not found")
)

// Mode selects lexical, semantic, or fused text retrieval.
type Mode string

const (
	ModeLexical  Mode = "lexical"
	ModeSemantic Mode = "semantic"
	ModeHybrid   Mode = "hybrid"
)

// ItemRepository loads canonical Items for hydrated search results.
type ItemRepository interface {
	// GetItem returns the Workspace Item and whether it exists.
	GetItem(ctx context.Context, workspaceID uuid.UUID, itemID uuid.UUID) (domain.Item, bool, error)
}

// Index combines text, chronological, and personal retrieval for search.
type Index interface {
	ranking.Index
	ranking.RecentIndex
	ranking.PersonalIndex
}

// Topics reads Topics within a Workspace for selection and validation.
type Topics interface {
	// GetTopic returns the Workspace Topic and whether it exists.
	GetTopic(ctx context.Context, workspaceID uuid.UUID, topicID uuid.UUID) (domain.Topic, bool, error)
}

// TopicRanker ranks one Topic for a requested retrieval purpose.
type TopicRanker interface {
	// RankTopic retrieves and ranks Items for the Topic and requested purpose.
	RankTopic(ctx context.Context, request ranking.TopicRequest) (ranking.TopicResult, error)
}

// WorkspaceSettings provides a current Workspace Settings snapshot for each operation.
type WorkspaceSettings interface {
	// GetWorkspace returns the current Workspace Settings snapshot.
	GetWorkspace(ctx context.Context) (domain.WorkspaceSettings, error)
}

// Request selects text retrieval, optional Topic and content scope, and bounded pagination.
type Request struct {
	Query           string
	Mode            Mode
	Limit           int
	Cursor          string
	TopicID         *uuid.UUID
	SourceID        *uuid.UUID
	Kinds           []domain.ItemKind
	PublishedAfter  *time.Time
	PublishedBefore *time.Time
}

// Entry pairs a canonical Item with the score and explanation from the current search.
type Entry struct {
	Item     domain.Item
	Score    float64
	Summary  string
	Signals  map[string]float64
	Pipeline string
}

// Page contains hydrated search results and an optional continuation token.
type Page struct {
	Items      []Entry
	NextCursor *string
}

// Service retrieves and hydrates canonical Items with optional Topic scope.
type Service struct {
	workspaceID uuid.UUID
	repository  ItemRepository
	index       Index
	settings    WorkspaceSettings
	now         func() time.Time
	topics      Topics
	ranker      TopicRanker
}

// Config supplies dependencies and operating settings for the search service.
type Config struct {
	WorkspaceID uuid.UUID
	Repository  ItemRepository
	Index       Index
	Settings    WorkspaceSettings
	Now         func() time.Time
	Topics      Topics
	Ranker      TopicRanker
}

// New validates and binds the Workspace's Item, retrieval, Topic ranking, and settings dependencies.
func New(config Config) (*Service, error) {
	if config.WorkspaceID == uuid.Nil || config.Repository == nil || config.Index == nil || config.Now == nil ||
		config.Settings == nil || config.Topics == nil || config.Ranker == nil {
		return nil, errors.New("search service dependencies are required")
	}
	return &Service{
		workspaceID: config.WorkspaceID, repository: config.Repository, index: config.Index,
		settings: config.Settings, now: config.Now, topics: config.Topics, ranker: config.Ranker,
	}, nil
}

// Find validates the request and returns bounded, hydrated results with optional Topic membership and
// current personal scoring.
func (s *Service) Find(ctx context.Context, request Request) (Page, error) {
	plan, err := prepareFind(request, s.workspaceID)
	if err != nil {
		return Page{}, err
	}
	settings, err := s.workspaceSettings(ctx)
	if err != nil {
		return Page{}, err
	}
	plan, err = s.applyTopic(ctx, plan, &settings)
	if err != nil {
		return Page{}, err
	}
	wanted := min(101, plan.offset+plan.request.Limit+1)
	candidateLimit := settings.Retrieval.CandidateLimit
	if plan.query == "" {
		candidateLimit = 100
	}
	pipeline := ranking.Pipeline{
		Retrievers: searchRetrievers(s.index, plan.request.Mode, plan.query, plan.filter),
		Fusion:     ranking.ReciprocalRankFusion{K: settings.Retrieval.RRFK},
		Scorers:    searchScorers(s.index, plan.filter, plan.query, s.now),
		Reranker:   ranking.IdentityReranker{}, Diversifier: searchDiversifier(plan.query),
	}
	candidates, err := pipeline.Recommend(ctx, ranking.Request{
		Profile: ranking.Profile{Prefer: plan.query}, CandidateLimit: candidateLimit, ResultLimit: wanted,
	})
	if err != nil {
		return Page{}, fmt.Errorf("rank search results: %w", err)
	}
	if plan.topicIDs != nil {
		candidates = filterTopicCandidates(candidates, plan.topicIDs)
	}
	candidates, more, offset := searchWindow(candidates, plan.offset, plan.request.Limit)
	entries, err := s.hydrate(ctx, candidates, plan.request.Mode, plan.request.TopicID)
	if err != nil {
		return Page{}, err
	}
	page := Page{Items: entries}
	if more && offset+plan.request.Limit < 100 {
		next := encodeCursor(offset + plan.request.Limit)
		page.NextCursor = &next
	}
	return page, nil
}

func (s *Service) applyTopic(
	ctx context.Context,
	plan findPlan,
	settings *domain.WorkspaceSettings,
) (findPlan, error) {
	if plan.request.TopicID == nil {
		return plan, nil
	}
	topic, found, err := s.topics.GetTopic(ctx, s.workspaceID, *plan.request.TopicID)
	if err != nil {
		return findPlan{}, fmt.Errorf("get search topic: %w", err)
	}
	if !found || !topic.Enabled {
		return findPlan{}, ErrTopicNotFound
	}
	if !applyTopicScope(&plan.filter, topic) {
		plan.topicIDs = map[string]struct{}{}
		return plan, nil
	}
	result, err := s.ranker.RankTopic(ctx, ranking.TopicRequest{
		Topic: topic, Purpose: ranking.PurposeSearch,
		PublishedAfter: plan.request.PublishedAfter, PublishedBefore: plan.request.PublishedBefore,
		Limit: settings.Retrieval.CandidateLimit, WorkspaceSettings: settings,
	})
	if err != nil {
		return findPlan{}, fmt.Errorf("rank search topic: %w", err)
	}
	plan.topicIDs = make(map[string]struct{}, len(result.Candidates))
	for _, candidate := range result.Candidates {
		plan.topicIDs[candidate.ItemID] = struct{}{}
	}
	return plan, nil
}

func (s *Service) workspaceSettings(ctx context.Context) (domain.WorkspaceSettings, error) {
	settings, err := s.settings.GetWorkspace(ctx)
	if err != nil {
		return domain.WorkspaceSettings{}, fmt.Errorf("get search settings: %w", err)
	}
	if settings.Retrieval.CandidateLimit < 1 || settings.Retrieval.RRFK < 1 {
		return domain.WorkspaceSettings{}, errors.New("search settings are invalid")
	}
	return settings, nil
}

func applyTopicScope(filter *ranking.IndexFilter, topic domain.Topic) bool {
	if len(topic.SourceIDs) > 0 {
		if len(filter.SourceIDs) == 1 && !containsUUID(topic.SourceIDs, filter.SourceIDs[0]) {
			return false
		}
		if len(filter.SourceIDs) == 0 {
			filter.SourceIDs = append([]uuid.UUID(nil), topic.SourceIDs...)
		}
	}
	filter.Kinds = append([]domain.ItemKind(nil), topic.ItemKinds...)
	return true
}

func containsUUID(values []uuid.UUID, wanted uuid.UUID) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}

func searchScorers(
	index ranking.PersonalIndex,
	filter ranking.IndexFilter,
	query string,
	now func() time.Time,
) []ranking.Scorer {
	if query == "" {
		return nil
	}
	return []ranking.Scorer{&ranking.PersonalKNN{
		Index: index, Filter: filter, NeighborLimit: 12, Now: now, PreserveCandidates: true,
	}}
}

type findPlan struct {
	request  Request
	query    string
	offset   int
	filter   ranking.IndexFilter
	topicIDs map[string]struct{}
}

func prepareFind(request Request, workspaceID uuid.UUID) (findPlan, error) {
	query := strings.TrimSpace(request.Query)
	if request.Mode == "" {
		request.Mode = ModeHybrid
	}
	offset, err := decodeCursor(request.Cursor)
	if failure := domain.JoinValidation(domain.CheckLength("q", len([]rune(query)), 0, 1000),
		domain.CheckRange("limit", float64(request.Limit), 1, 100),
		domain.CheckChoice("mode", string(request.Mode), string(ModeHybrid), string(ModeSemantic), string(ModeLexical)),
		domain.CheckCondition("cursor", domain.ValidationFormat, err == nil && offset < 100)); failure != nil {
		return findPlan{}, fmt.Errorf("%w: %w", ErrInvalidRequest, failure)
	}
	filter := ranking.IndexFilter{
		WorkspaceID: workspaceID, Kinds: append([]domain.ItemKind(nil), request.Kinds...),
		PublishedAfter: request.PublishedAfter, PublishedBefore: request.PublishedBefore,
	}
	if request.SourceID != nil {
		filter.SourceIDs = []uuid.UUID{*request.SourceID}
	}
	return findPlan{request: request, query: query, offset: offset, filter: filter}, nil
}

func searchRetrievers(index Index, mode Mode, query string, filter ranking.IndexFilter) []ranking.Retriever {
	if query == "" {
		return []ranking.Retriever{ranking.NewRecentRetriever(index, filter)}
	}
	result := make([]ranking.Retriever, 0, 2)
	if mode == ModeSemantic || mode == ModeHybrid {
		result = append(result, ranking.NewDenseRetriever(index, filter))
	}
	if mode == ModeLexical || mode == ModeHybrid {
		result = append(result, ranking.NewSparseRetriever(index, filter))
	}
	return result
}

func searchDiversifier(query string) ranking.Diversifier {
	if query == "" {
		return ranking.TopN{}
	}
	return ranking.MMR{Lambda: 0.85}
}

func filterTopicCandidates(candidates []ranking.Candidate, allowed map[string]struct{}) []ranking.Candidate {
	result := make([]ranking.Candidate, 0, len(candidates))
	for _, candidate := range candidates {
		if _, found := allowed[candidate.ItemID]; found {
			result = append(result, candidate)
		}
	}
	return result
}

func searchWindow(candidates []ranking.Candidate, offset, limit int) ([]ranking.Candidate, bool, int) {
	if offset > len(candidates) {
		offset = len(candidates)
	}
	candidates = candidates[offset:]
	more := len(candidates) > limit
	if more {
		candidates = candidates[:limit]
	}
	return candidates, more, offset
}

func (s *Service) hydrate(
	ctx context.Context,
	candidates []ranking.Candidate,
	mode Mode,
	topicID *uuid.UUID,
) ([]Entry, error) {
	entries := make([]Entry, 0, len(candidates))
	for _, candidate := range candidates {
		itemID, parseErr := uuid.Parse(candidate.ItemID)
		if parseErr != nil {
			return nil, fmt.Errorf("ranked item has invalid ID: %w", parseErr)
		}
		item, found, getErr := s.repository.GetItem(ctx, s.workspaceID, itemID)
		if getErr != nil {
			return nil, fmt.Errorf("hydrate search item: %w", getErr)
		}
		if !found {
			continue
		}
		if topicID != nil {
			item.TopicIDs = appendTopicID(item.TopicIDs, *topicID)
		}
		entries = append(entries, Entry{
			Item: item, Score: candidate.Score, Summary: "Matched by " + string(mode) + " search",
			Signals: candidate.Signals, Pipeline: "search-" + string(mode) + "-v1",
		})
	}
	return entries, nil
}

func appendTopicID(topicIDs []uuid.UUID, topicID uuid.UUID) []uuid.UUID {
	if containsUUID(topicIDs, topicID) {
		return topicIDs
	}
	return append(topicIDs, topicID)
}

type cursor struct {
	Offset int `json:"offset"`
}

func encodeCursor(offset int) string {
	encoded, _ := json.Marshal(cursor{Offset: offset})
	return base64.RawURLEncoding.EncodeToString(encoded)
}

func decodeCursor(value string) (int, error) {
	if value == "" {
		return 0, nil
	}
	decoded, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil {
		return 0, err
	}
	var cursor cursor
	if err := json.Unmarshal(decoded, &cursor); err != nil || cursor.Offset < 0 {
		return 0, errors.New("invalid cursor")
	}
	return cursor.Offset, nil
}
