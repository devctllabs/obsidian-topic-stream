package ranking_test

import (
	"context"
	"testing"
	"time"

	"github.com/ethernity/obsidian-topic-stream/internal/domain"
	"github.com/ethernity/obsidian-topic-stream/internal/service/ranking"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

type topicIndexStub struct {
	limits           []int
	filters          []ranking.IndexFilter
	includes         []string
	excludes         []string
	requests         []ranking.TopicIndexRequest
	results          map[int]ranking.TopicRetrieval
	personalRequests []ranking.PersonalIndexRequest
	personalResult   ranking.PersonalRetrieval
}

type thresholdProviderStub struct {
	calls      int
	thresholds ranking.TopicThresholds
}

type workspaceSettingsStub struct {
	values []domain.WorkspaceSettings
	calls  int
}

type llmFactoryStub struct {
	client   ranking.LLMRerankClient
	settings []domain.GenerationSettings
}

// NewRerankClient records generation settings and returns the fixture client.
func (s *llmFactoryStub) NewRerankClient(settings domain.GenerationSettings) (ranking.LLMRerankClient, error) {
	s.settings = append(s.settings, settings)
	return s.client, nil
}

// GetWorkspace returns the fixture's Workspace Settings snapshot.
func (s *workspaceSettingsStub) GetWorkspace(context.Context) (domain.WorkspaceSettings, error) {
	value := s.values[min(s.calls, len(s.values)-1)]
	s.calls++
	return value, nil
}

func TestEngineFreezesOneLiveSettingsSnapshotPerRanking(t *testing.T) {
	t.Parallel()

	workspaceID, topicID, itemID := uuid.New(), uuid.New(), uuid.New()
	settings := &workspaceSettingsStub{values: []domain.WorkspaceSettings{
		{Version: 2, Retrieval: validRetrievalSettings(25)},
		{Version: 3, Retrieval: validRetrievalSettings(40)},
	}}
	index := &topicIndexStub{results: map[int]ranking.TopicRetrieval{
		25: {IncludeEmbedding: []float32{1, 0}, Dense: []ranking.IndexHit{{ItemID: itemID, Embedding: []float32{1, 0}, Score: 1}}},
		40: {IncludeEmbedding: []float32{1, 0}, Dense: []ranking.IndexHit{{ItemID: itemID, Embedding: []float32{1, 0}, Score: 1}}},
	}}
	engine, err := ranking.NewEngine(ranking.EngineConfig{
		Index: index, Profiles: profileStub{}, Thresholds: &thresholdProviderStub{},
		WorkspaceSettings: settings, ThresholdDefaults: ranking.DefaultTopicThresholdDefaults("qwen"), Now: time.Now,
	})
	require.NoError(t, err)
	request := ranking.TopicRequest{
		Topic:   domain.Topic{ID: topicID, WorkspaceID: workspaceID, Include: "systems", Mode: domain.TopicModeOrdinary, Enabled: true},
		Purpose: ranking.PurposeFeed, Limit: 1,
	}

	first, err := engine.RankTopic(t.Context(), request)
	require.NoError(t, err)
	second, err := engine.RankTopic(t.Context(), request)
	require.NoError(t, err)

	require.Equal(t, []int{25, 40}, index.limits)
	require.True(t, index.filters[0].FeedCandidates)
	require.Equal(t, 25, first.Inputs.CandidateLimit)
	require.Equal(t, 40, second.Inputs.CandidateLimit)
	require.Equal(t, 2, settings.calls)
}

func TestEngineUsesCallerSettingsSnapshotWithoutReloadingIt(t *testing.T) {
	t.Parallel()

	workspaceID, topicID, itemID := uuid.New(), uuid.New(), uuid.New()
	provider := &workspaceSettingsStub{values: []domain.WorkspaceSettings{{Retrieval: validRetrievalSettings(40)}}}
	snapshot := domain.WorkspaceSettings{Version: 7, Retrieval: validRetrievalSettings(25)}
	index := &topicIndexStub{results: map[int]ranking.TopicRetrieval{
		25: {IncludeEmbedding: []float32{1, 0}, Dense: []ranking.IndexHit{{ItemID: itemID, Embedding: []float32{1, 0}, Score: 1}}},
	}}
	engine, err := ranking.NewEngine(ranking.EngineConfig{
		Index: index, Profiles: profileStub{}, Thresholds: &thresholdProviderStub{},
		WorkspaceSettings: provider, ThresholdDefaults: ranking.DefaultTopicThresholdDefaults("qwen"), Now: time.Now,
	})
	require.NoError(t, err)

	result, err := engine.RankTopic(t.Context(), ranking.TopicRequest{
		Topic: domain.Topic{
			ID: topicID, WorkspaceID: workspaceID, Include: "systems",
			Mode: domain.TopicModeOrdinary, Enabled: true,
		},
		Purpose: ranking.PurposeFeed, Limit: 1, WorkspaceSettings: &snapshot,
	})

	require.NoError(t, err)
	require.Equal(t, []int{25}, index.limits)
	require.Equal(t, 25, result.Inputs.CandidateLimit)
	require.Zero(t, provider.calls)
}

func validRetrievalSettings(candidateLimit int) domain.RetrievalSettings {
	return domain.RetrievalSettings{
		CandidateLimit: candidateLimit, RerankLimit: 30, RRFK: 60,
		RecencyHalfLifeDays: 7, RecencyMaxBoost: 0.1, FeedSourceCap: 3,
		DigestSourceCap: 2, DigestMMRLambda: 0.75,
	}
}

func fixedWorkspaceSettings(retrieval domain.RetrievalSettings) *workspaceSettingsStub {
	return &workspaceSettingsStub{values: []domain.WorkspaceSettings{{Retrieval: retrieval}}}
}

// Thresholds returns configured gates or the mode-specific bootstrap thresholds.
func (s *thresholdProviderStub) Thresholds(
	_ context.Context,
	topic domain.Topic,
	defaults ranking.TopicThresholdDefaults,
) (ranking.TopicThresholds, error) {
	s.calls++
	if s.thresholds != (ranking.TopicThresholds{}) {
		return s.thresholds, nil
	}
	if topic.Mode == domain.TopicModeStrict {
		return defaults.Strict, nil
	}
	return defaults.Ordinary, nil
}

// RetrievePersonal returns configured personal-feedback evidence.
func (s *topicIndexStub) RetrievePersonal(
	_ context.Context,
	request ranking.PersonalIndexRequest,
) (ranking.PersonalRetrieval, error) {
	s.personalRequests = append(s.personalRequests, request)
	return s.personalResult, nil
}

// RetrieveTopic records Topic retrieval inputs and returns the configured branches.
func (s *topicIndexStub) RetrieveTopic(
	_ context.Context,
	request ranking.TopicIndexRequest,
) (ranking.TopicRetrieval, error) {
	s.requests = append(s.requests, request)
	s.limits = append(s.limits, request.Limit)
	s.filters = append(s.filters, request.Filter)
	s.includes = append(s.includes, request.Include)
	s.excludes = append(s.excludes, request.Exclude)
	return s.results[request.Limit], nil
}

func TestEngineRejectsInvalidDigestSourceCap(t *testing.T) {
	t.Parallel()

	engine, err := ranking.NewEngine(ranking.EngineConfig{
		Index:             &topicIndexStub{},
		Profiles:          profileStub{},
		Thresholds:        &thresholdProviderStub{},
		ThresholdDefaults: ranking.DefaultTopicThresholdDefaults("qwen"),
		WorkspaceSettings: fixedWorkspaceSettings(domain.RetrievalSettings{
			CandidateLimit: 50, RRFK: 60, RecencyHalfLifeDays: 7, RecencyMaxBoost: 0.1,
			FeedSourceCap: 3, DigestSourceCap: 0,
		}),
		Now: time.Now,
	})
	require.NoError(t, err)

	_, err = engine.RankTopic(t.Context(), ranking.TopicRequest{
		Topic: domain.Topic{
			ID: uuid.New(), WorkspaceID: uuid.New(), Include: "systems",
			Mode: domain.TopicModeOrdinary, Enabled: true,
		},
		Purpose: ranking.PurposeFeed, Limit: 1,
	})

	require.ErrorContains(t, err, "settings are invalid")
}

func TestNewEngineRequiresWorkspaceSettingsProvider(t *testing.T) {
	t.Parallel()

	_, err := ranking.NewEngine(ranking.EngineConfig{
		Index:             &topicIndexStub{},
		Profiles:          profileStub{},
		Thresholds:        &thresholdProviderStub{},
		ThresholdDefaults: ranking.DefaultTopicThresholdDefaults("qwen"),
		Now:               time.Now,
	})

	require.ErrorContains(t, err, "dependencies are required")
}

func TestEngineLoadsTopicThresholdsOnceBeforeRetrieval(t *testing.T) {
	t.Parallel()

	workspaceID, topicID, candidateID := uuid.New(), uuid.New(), uuid.New()
	defaults := ranking.DefaultTopicThresholdDefaults("qwen")
	expected := ranking.TopicThresholds{Include: 0.73, Exclude: 0.81}
	thresholds := &thresholdProviderStub{thresholds: expected}
	index := &topicIndexStub{results: map[int]ranking.TopicRetrieval{50: {
		IncludeEmbedding: []float32{1, 0}, ExcludeEmbedding: []float32{0, 1},
		Dense: []ranking.IndexHit{{
			ItemID: candidateID, Embedding: []float32{1, 0}, Score: 1,
		}},
	}}}
	engine, err := ranking.NewEngine(ranking.EngineConfig{
		Index:    index,
		Profiles: profileStub{}, Thresholds: thresholds, ThresholdDefaults: defaults,
		WorkspaceSettings: fixedWorkspaceSettings(domain.RetrievalSettings{
			CandidateLimit: 50, RRFK: 60, RecencyHalfLifeDays: 7, RecencyMaxBoost: 0.1,
			FeedSourceCap: 3, DigestSourceCap: 2, DigestMMRLambda: 0.75,
		}),
		Now: time.Now,
	})
	require.NoError(t, err)

	result, err := engine.RankTopic(t.Context(), ranking.TopicRequest{
		Topic: domain.Topic{
			ID: topicID, WorkspaceID: workspaceID, Include: "retrieval", Exclude: "noise",
			Mode: domain.TopicModeOrdinary, Enabled: true, Revision: 1, CorrectionEpoch: 1,
		},
		Purpose: ranking.PurposeFeed, Limit: 1,
	})

	require.NoError(t, err)
	require.Equal(t, expected, result.Inputs.Thresholds)
	require.Equal(t, 1, thresholds.calls)
	require.Equal(t, []int{50}, index.limits)
	require.Equal(t, topicID, index.requests[0].TopicID)
	require.Equal(t, int64(1), index.requests[0].CorrectionEpoch)
}

func TestEngineRanksTopicOnceAndReturnsUnderfilledResult(t *testing.T) {
	t.Parallel()

	workspaceID, sourceID, topicID := uuid.New(), uuid.New(), uuid.New()
	automaticID, includedID, excludedID := uuid.New(), uuid.New(), uuid.New()
	now := time.Date(2026, time.September, 27, 12, 0, 0, 0, time.UTC)
	index := &topicIndexStub{results: map[int]ranking.TopicRetrieval{
		50: {
			IncludeEmbedding: []float32{1, 0},
			Dense: []ranking.IndexHit{
				{ItemID: automaticID, SourceID: sourceID, PublishedAt: timePointer(now), Embedding: []float32{1, 0}, Score: 0.9},
				{ItemID: excludedID, SourceID: sourceID, PublishedAt: timePointer(now), Embedding: []float32{1, 0}, Score: 0.8, TopicCorrections: []domain.TopicCorrectionProjection{{TopicID: topicID, CorrectionEpoch: 3, Membership: domain.TopicMembershipExclude}}},
			},
			Sparse: []ranking.IndexHit{{ItemID: automaticID, SourceID: sourceID, PublishedAt: timePointer(now), Embedding: []float32{1, 0}, Score: 4}},
			Corrections: []ranking.IndexHit{
				{ItemID: includedID, SourceID: sourceID, PublishedAt: timePointer(now), Embedding: []float32{0, 1}, TopicCorrections: []domain.TopicCorrectionProjection{{TopicID: topicID, CorrectionEpoch: 3, Membership: domain.TopicMembershipInclude}}},
			},
		},
	}}
	engine, err := ranking.NewEngine(ranking.EngineConfig{
		Index:    index,
		Profiles: profileStub{}, Thresholds: &thresholdProviderStub{},
		ThresholdDefaults: ranking.TopicThresholdDefaults{
			Model: "qwen", Version: "topic-gate-v1", Shrinkage: 10,
			Ordinary: ranking.TopicThresholds{Include: 0.8, Exclude: 0.8},
			Strict:   ranking.TopicThresholds{Include: 0.9, Exclude: 0.7},
		},
		WorkspaceSettings: fixedWorkspaceSettings(domain.RetrievalSettings{
			CandidateLimit: 50, RRFK: 60, RecencyHalfLifeDays: 7, RecencyMaxBoost: 0.1,
			FeedSourceCap: 3, DigestSourceCap: 2,
		}),
		Now: func() time.Time { return now },
	})
	require.NoError(t, err)
	publishedAfter := now.Add(-30 * 24 * time.Hour)
	topic := domain.Topic{
		ID: topicID, WorkspaceID: workspaceID, SourceIDs: []uuid.UUID{sourceID},
		ItemKinds: []domain.ItemKind{domain.ItemKindPaper}, Include: "retrieval", Mode: domain.TopicModeOrdinary,
		Enabled: true, Revision: 3, CorrectionEpoch: 3,
	}

	result, err := engine.RankTopic(t.Context(), ranking.TopicRequest{
		Topic: topic, Purpose: ranking.PurposeFeed, PublishedAfter: &publishedAfter, Limit: 3,
	})

	require.NoError(t, err)
	require.Equal(t, []int{50}, index.limits)
	require.Equal(t, []string{"retrieval"}, index.includes)
	require.Equal(t, []string{automaticID.String(), includedID.String()}, candidateIDs(result.Candidates))
	require.Equal(t, "topic-stream-ranking-v2", result.Inputs.PipelineVersion)
	require.Equal(t, 50, result.Inputs.CandidateLimit)
	require.Equal(t, "topic-gate-v1", result.Inputs.TopicGateVersion)
	require.Equal(t, 3, result.Inputs.TopicRevision)
	for _, filter := range index.filters {
		require.Equal(t, workspaceID, filter.WorkspaceID)
		require.Equal(t, []uuid.UUID{sourceID}, filter.SourceIDs)
		require.Equal(t, []domain.ItemKind{domain.ItemKindPaper}, filter.Kinds)
		require.Equal(t, publishedAfter, *filter.PublishedAfter)
	}
}

type profileStub struct {
	value domain.PreferenceProfile
}

// GetPreferenceProfile returns the fixture's Preference Profile.
func (s profileStub) GetPreferenceProfile(context.Context) (domain.PreferenceProfile, error) {
	return s.value, nil
}

func TestEngineLoadsPersonalEvidenceForAcceptedCandidatesFromQdrant(t *testing.T) {
	t.Parallel()

	workspaceID, topicID := uuid.New(), uuid.New()
	preferredID, rejectedID := uuid.New(), uuid.New()
	now := time.Date(2026, time.September, 27, 12, 0, 0, 0, time.UTC)
	positive := make([]ranking.FeedbackNeighborhood, 0, 5)
	negative := make([]ranking.FeedbackNeighborhood, 0, 5)
	for range 5 {
		liked := domain.ItemFeedback{ItemID: uuid.New(), Feedback: domain.Feedback{Sentiment: "like", UpdatedAt: now}}
		disliked := domain.ItemFeedback{ItemID: uuid.New(), Feedback: domain.Feedback{Sentiment: "dislike", UpdatedAt: now}}
		positive = append(positive, ranking.FeedbackNeighborhood{
			Feedback: liked, Hits: []ranking.IndexHit{{ItemID: preferredID, Embedding: []float32{1, 0}, Score: 0.9}},
		})
		negative = append(negative, ranking.FeedbackNeighborhood{
			Feedback: disliked, Hits: []ranking.IndexHit{{ItemID: rejectedID, Embedding: []float32{1, 0}, Score: 0.9}},
		})
	}
	index := &topicIndexStub{results: map[int]ranking.TopicRetrieval{50: {
		IncludeEmbedding: []float32{1, 0},
		Dense: []ranking.IndexHit{
			{ItemID: preferredID, Embedding: []float32{1, 0}, Score: 0.5},
			{ItemID: rejectedID, Embedding: []float32{1, 0}, Score: 0.6},
		},
	}}, personalResult: ranking.PersonalRetrieval{Positive: positive, Negative: negative}}
	engine, err := ranking.NewEngine(ranking.EngineConfig{
		Index:             index,
		Profiles:          profileStub{value: domain.PreferenceProfile{Revision: 4}},
		Thresholds:        &thresholdProviderStub{},
		ThresholdDefaults: ranking.DefaultTopicThresholdDefaults("qwen"),
		WorkspaceSettings: fixedWorkspaceSettings(domain.RetrievalSettings{
			CandidateLimit: 50, RRFK: 60, RecencyHalfLifeDays: 7, RecencyMaxBoost: 0.1,
			FeedSourceCap: 3, DigestSourceCap: 2, DigestMMRLambda: 0.75,
		}),
		Now: func() time.Time { return now },
	})
	require.NoError(t, err)

	result, err := engine.RankTopic(t.Context(), ranking.TopicRequest{
		Topic: domain.Topic{
			ID: topicID, WorkspaceID: workspaceID, Include: "systems",
			Mode: domain.TopicModeOrdinary, Enabled: true,
		},
		Purpose: ranking.PurposeFeed, Limit: 1,
	})

	require.NoError(t, err)
	require.Equal(t, []uuid.UUID{preferredID, rejectedID}, index.personalRequests[0].CandidateIDs)
	require.Equal(t, []string{preferredID.String()}, candidateIDs(result.Candidates))
	require.Equal(t, ranking.PersonalKNNVersion, result.Inputs.PersonalizationVersion)
	require.Equal(t, 4, result.Inputs.PreferenceRevision)
	require.Greater(t, result.Candidates[0].Signals["personal.positive"], 0.0)
}

func TestEngineAppliesDigestSourceCapDuringFinalSelection(t *testing.T) {
	t.Parallel()

	workspaceID, topicID := uuid.New(), uuid.New()
	firstSourceID, secondSourceID := uuid.New(), uuid.New()
	firstID, secondID, overflowID, alternativeID := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	now := time.Date(2026, time.September, 27, 12, 0, 0, 0, time.UTC)
	index := &topicIndexStub{results: map[int]ranking.TopicRetrieval{50: {
		IncludeEmbedding: []float32{1, 0},
		Dense: []ranking.IndexHit{
			{ItemID: firstID, SourceID: firstSourceID, PublishedAt: timePointer(now), Embedding: []float32{1, 0}, Score: 1},
			{ItemID: secondID, SourceID: firstSourceID, PublishedAt: timePointer(now), Embedding: []float32{1, 0}, Score: 0.9},
			{ItemID: overflowID, SourceID: firstSourceID, PublishedAt: timePointer(now), Embedding: []float32{1, 0}, Score: 0.8},
			{ItemID: alternativeID, SourceID: secondSourceID, PublishedAt: timePointer(now), Embedding: []float32{1, 0}, Score: 0.7},
		},
	}}}
	engine, err := ranking.NewEngine(ranking.EngineConfig{
		Index:             index,
		Profiles:          profileStub{},
		Thresholds:        &thresholdProviderStub{},
		ThresholdDefaults: ranking.DefaultTopicThresholdDefaults("qwen"),
		WorkspaceSettings: fixedWorkspaceSettings(domain.RetrievalSettings{
			CandidateLimit: 50, RRFK: 60, RecencyHalfLifeDays: 7, RecencyMaxBoost: 0.1,
			FeedSourceCap: 3, DigestSourceCap: 2, DigestMMRLambda: 0.75,
		}),
		Now: func() time.Time { return now },
	})
	require.NoError(t, err)

	result, err := engine.RankTopic(t.Context(), ranking.TopicRequest{
		Topic: domain.Topic{
			ID: topicID, WorkspaceID: workspaceID, Include: "systems",
			Mode: domain.TopicModeOrdinary, Enabled: true,
		},
		Purpose: ranking.PurposeDigest, Limit: 3,
	})

	require.NoError(t, err)
	require.Equal(t, []string{firstID.String(), secondID.String(), alternativeID.String()}, candidateIDs(result.Candidates))
}

func TestEngineAppliesDigestSourceCapAfterLLMReranking(t *testing.T) {
	t.Parallel()

	workspaceID, topicID := uuid.New(), uuid.New()
	firstSourceID, secondSourceID := uuid.New(), uuid.New()
	firstID, secondID, overflowID, alternativeID := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	orderedIDs := []uuid.UUID{firstID, secondID, overflowID, alternativeID}
	now := time.Date(2026, time.September, 27, 12, 0, 0, 0, time.UTC)
	dense := make([]ranking.IndexHit, len(orderedIDs))
	items := make(map[uuid.UUID]domain.Item, len(orderedIDs))
	for index, itemID := range orderedIDs {
		sourceID := firstSourceID
		if itemID == alternativeID {
			sourceID = secondSourceID
		}
		dense[index] = ranking.IndexHit{
			ItemID: itemID, SourceID: sourceID, PublishedAt: timePointer(now),
			Embedding: []float32{1, 0}, Score: float64(len(orderedIDs) - index),
		}
		items[itemID] = domain.Item{ID: itemID, WorkspaceID: workspaceID, SourceIDs: []uuid.UUID{sourceID}}
	}
	index := &topicIndexStub{results: map[int]ranking.TopicRetrieval{50: {
		IncludeEmbedding: []float32{1, 0}, Dense: dense,
	}}}
	client := &llmClientStub{order: []string{
		overflowID.String(), firstID.String(), secondID.String(), alternativeID.String(),
	}}
	engine, err := ranking.NewEngine(ranking.EngineConfig{
		Index:    index,
		Profiles: profileStub{}, Thresholds: &thresholdProviderStub{},
		Items: rankingItemStoreStub{items: items}, LLMFactory: &llmFactoryStub{client: client},
		ThresholdDefaults: ranking.DefaultTopicThresholdDefaults("qwen"),
		WorkspaceSettings: &workspaceSettingsStub{values: []domain.WorkspaceSettings{{
			Generation: domain.GenerationSettings{Enabled: true},
			Retrieval: domain.RetrievalSettings{
				CandidateLimit: 50, RRFK: 60, RecencyHalfLifeDays: 7, RecencyMaxBoost: 0.1,
				FeedSourceCap: 3, DigestSourceCap: 2, DigestMMRLambda: 0.75,
				LLMRerankEnabled: true,
			},
		}}},
		Now: func() time.Time { return now },
	})
	require.NoError(t, err)

	result, err := engine.RankTopic(t.Context(), ranking.TopicRequest{
		Topic: domain.Topic{
			ID: topicID, WorkspaceID: workspaceID, Include: "systems",
			Mode: domain.TopicModeOrdinary, Enabled: true,
		},
		Purpose: ranking.PurposeDigest, Limit: 3,
	})

	require.NoError(t, err)
	require.Equal(t, []string{firstID.String(), overflowID.String(), alternativeID.String()}, candidateIDs(result.Candidates))
	require.NotEmpty(t, client.request.Candidates)
	for _, candidate := range result.Candidates {
		require.Contains(t, candidate.Signals, "rrf.llm")
	}
}

func TestEngineUsesOptionalLLMRerankerOnlyForDigestAndReturnsVersions(t *testing.T) {
	t.Parallel()

	workspaceID, topicID, itemID := uuid.New(), uuid.New(), uuid.New()
	now := time.Date(2026, time.September, 27, 12, 0, 0, 0, time.UTC)
	index := &topicIndexStub{results: map[int]ranking.TopicRetrieval{50: {
		IncludeEmbedding: []float32{1, 0},
		Dense:            []ranking.IndexHit{{ItemID: itemID, Embedding: []float32{1, 0}, Score: 0.9}},
	}}}
	client := &llmClientStub{order: []string{itemID.String()}}
	clientFactory := &llmFactoryStub{client: client}
	model := "qwen-reranker"
	engine, err := ranking.NewEngine(ranking.EngineConfig{
		Index:    index,
		Profiles: profileStub{value: domain.PreferenceProfile{Revision: 2}}, Thresholds: &thresholdProviderStub{},
		Items: rankingItemStoreStub{items: map[uuid.UUID]domain.Item{
			itemID: {ID: itemID, WorkspaceID: workspaceID, Title: "Candidate"},
		}},
		LLMFactory:        clientFactory,
		ThresholdDefaults: ranking.DefaultTopicThresholdDefaults("qwen"),
		WorkspaceSettings: &workspaceSettingsStub{values: []domain.WorkspaceSettings{{
			Version: 7, Generation: domain.GenerationSettings{Enabled: true, RerankModel: &model},
			Instructions: domain.TaskInstructions{DigestRerank: "Rank for depth"},
			Retrieval: domain.RetrievalSettings{
				CandidateLimit: 50, RRFK: 60, RecencyHalfLifeDays: 7, RecencyMaxBoost: 0.1,
				FeedSourceCap: 3, DigestSourceCap: 2, DigestMMRLambda: 0.75,
				LLMRerankEnabled: true,
			},
		}}},
		Now: func() time.Time { return now },
	})
	require.NoError(t, err)
	topic := domain.Topic{
		ID: topicID, WorkspaceID: workspaceID, Include: "systems",
		Mode: domain.TopicModeOrdinary, Enabled: true,
	}

	digest, err := engine.RankTopic(t.Context(), ranking.TopicRequest{
		Topic: topic, Purpose: ranking.PurposeDigest, Limit: 1,
	})
	require.NoError(t, err)
	require.Equal(t, ranking.DigestLLMRerankerVersion, digest.Inputs.RerankerVersion)
	require.Equal(t, model, digest.Inputs.RerankerModel)
	require.Equal(t, ranking.DigestRerankPromptVersion+"/settings-7", digest.Inputs.PromptVersion)
	require.Equal(t, "Rank for depth", client.request.Instruction)
	require.Equal(t, []domain.GenerationSettings{{Enabled: true, RerankModel: &model}}, clientFactory.settings)

	client.request = ranking.LLMRerankRequest{}
	_, err = engine.RankTopic(t.Context(), ranking.TopicRequest{
		Topic: topic, Purpose: ranking.PurposeFeed, Limit: 1,
	})
	require.NoError(t, err)
	require.Empty(t, client.request.Candidates)
}

func timePointer(value time.Time) *time.Time { return &value }
