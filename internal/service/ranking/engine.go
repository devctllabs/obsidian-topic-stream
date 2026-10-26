package ranking

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/ethernity/obsidian-topic-stream/internal/domain"
	"github.com/google/uuid"
)

const PipelineVersion = "topic-stream-ranking-v2"

// Purpose selects the ranking behavior used for Search or Digest generation.
type Purpose string

const (
	PurposeFeed   Purpose = "feed"
	PurposeDigest Purpose = "digest"
	PurposeSearch Purpose = "search"
)

// TopicRetrieval groups one Topic's retrieval branches, corrections, and reusable criterion embeddings.
type TopicRetrieval struct {
	Dense            []IndexHit
	Sparse           []IndexHit
	Recent           []IndexHit
	IncludeEmbedding []float32
	ExcludeEmbedding []float32
	Corrections      []IndexHit
}

// TopicIndexRequest scopes a bounded Topic retrieval and optionally reuses criterion embeddings.
type TopicIndexRequest struct {
	// IncludeEmbedding and ExcludeEmbedding may reuse previously prepared criterion vectors.
	IncludeEmbedding []float32
	ExcludeEmbedding []float32
	TopicID          uuid.UUID
	CorrectionEpoch  int64
	Include          string
	Exclude          string
	Filter           IndexFilter
	Limit            int
}

// TopicCorrectionIndexRequest retrieves explicit membership evidence from the current correction epoch.
type TopicCorrectionIndexRequest struct {
	TopicID         uuid.UUID
	CorrectionEpoch int64
	Membership      domain.TopicMembership
	QueryVector     []float32
	Filter          IndexFilter
	Limit           int
}

// TopicIndex retrieves Topic candidates and personal feedback evidence.
type TopicIndex interface {
	// RetrieveTopic retrieves dense, sparse, and correction evidence using one Topic index request.
	RetrieveTopic(ctx context.Context, request TopicIndexRequest) (TopicRetrieval, error)
	PersonalIndex
}

// PreferenceProfiles supplies current global preference guidance.
type PreferenceProfiles interface {
	// GetPreferenceProfile returns the current Preference Profile.
	GetPreferenceProfile(ctx context.Context) (domain.PreferenceProfile, error)
}

// TopicThresholdProvider resolves calibrated or bootstrap Topic membership thresholds.
type TopicThresholdProvider interface {
	// Thresholds returns effective Topic gates, using defaults when calibration is incompatible.
	Thresholds(ctx context.Context, topic domain.Topic, defaults TopicThresholdDefaults) (TopicThresholds, error)
}

// WorkspaceSettingsProvider supplies Workspace settings for a ranking operation.
type WorkspaceSettingsProvider interface {
	// GetWorkspace returns the current Workspace Settings snapshot.
	GetWorkspace(ctx context.Context) (domain.WorkspaceSettings, error)
}

// LLMRerankClientFactory constructs the optional generation-backed reranker.
type LLMRerankClientFactory interface {
	// NewRerankClient constructs a reranking client from non-secret generation settings.
	NewRerankClient(settings domain.GenerationSettings) (LLMRerankClient, error)
}

// EngineConfig supplies Topic retrieval, current policy snapshots, gates, and optional reranking
// dependencies.
type EngineConfig struct {
	Index             TopicIndex
	Profiles          PreferenceProfiles
	WorkspaceSettings WorkspaceSettingsProvider
	Thresholds        TopicThresholdProvider
	Items             RankingItemStore
	LLMFactory        LLMRerankClientFactory
	ThresholdDefaults TopicThresholdDefaults
	Now               func() time.Time
}

// Engine composes Topic retrieval, membership gates, personal scoring, and purpose-specific
// diversification.
type Engine struct {
	config EngineConfig
}

// TopicRequest selects a Topic, ranking purpose, time window, and optional frozen settings snapshot.
type TopicRequest struct {
	Topic           domain.Topic
	Purpose         Purpose
	PublishedAfter  *time.Time
	PublishedBefore *time.Time
	Limit           int
	// WorkspaceSettings freezes a caller-supplied snapshot; nil loads current settings.
	WorkspaceSettings *domain.WorkspaceSettings
}

// RankingInputs records the policy, model, and component versions explaining a ranked result.
type RankingInputs struct {
	PipelineVersion        string          `json:"pipelineVersion"`
	TopicGateVersion       string          `json:"topicGateVersion"`
	EmbeddingModel         string          `json:"embeddingModel"`
	TopicRevision          int             `json:"topicRevision"`
	Thresholds             TopicThresholds `json:"thresholds"`
	Purpose                Purpose         `json:"purpose"`
	PersonalizationVersion string          `json:"personalizationVersion,omitempty"`
	PreferenceRevision     int             `json:"preferenceRevision"`
	RerankerVersion        string          `json:"rerankerVersion,omitempty"`
	RerankerModel          string          `json:"rerankerModel,omitempty"`
	PromptVersion          string          `json:"promptVersion,omitempty"`
	CandidateLimit         int             `json:"candidateLimit"`
}

// TopicResult returns ranked candidates with their reproducible input versions.
type TopicResult struct {
	Candidates []Candidate
	Inputs     RankingInputs
}

// NewEngine validates the dependencies used by Topic ranking.
func NewEngine(config EngineConfig) (*Engine, error) {
	if missingEngineDependencies(config) {
		return nil, errors.New("ranking engine dependencies are required")
	}
	if _, _, err := config.ThresholdDefaults.Calibrate(domain.TopicModeOrdinary, nil); err != nil {
		return nil, err
	}
	if _, _, err := config.ThresholdDefaults.Calibrate(domain.TopicModeStrict, nil); err != nil {
		return nil, err
	}
	return &Engine{config: config}, nil
}

func validEngineSettings(settings domain.RetrievalSettings) bool {
	return settings.CandidateLimit >= 1 && settings.CandidateLimit <= 100 &&
		settings.RRFK >= 1 && settings.RecencyHalfLifeDays > 0 &&
		settings.RecencyMaxBoost >= 0 && settings.FeedSourceCap >= 1 && settings.DigestSourceCap >= 1
}

func missingEngineDependencies(config EngineConfig) bool {
	return config.Index == nil || config.Profiles == nil || config.WorkspaceSettings == nil ||
		config.Thresholds == nil || config.Now == nil
}

// RankTopic retrieves and ranks Items for the Topic and requested purpose.
func (e *Engine) RankTopic(ctx context.Context, request TopicRequest) (TopicResult, error) {
	if err := validateTopicRequest(request); err != nil {
		return TopicResult{}, err
	}
	workspaceSettings, err := e.workspaceSettings(ctx, request.WorkspaceSettings)
	if err != nil {
		return TopicResult{}, err
	}
	thresholds, err := e.config.Thresholds.Thresholds(ctx, request.Topic, e.config.ThresholdDefaults)
	if err != nil {
		return TopicResult{}, fmt.Errorf("load topic thresholds: %w", err)
	}
	profile, err := e.config.Profiles.GetPreferenceProfile(ctx)
	if err != nil {
		return TopicResult{}, fmt.Errorf("get preference profile: %w", err)
	}
	filter := IndexFilter{
		FeedCandidates:  request.Purpose == PurposeFeed,
		WorkspaceID:     request.Topic.WorkspaceID,
		SourceIDs:       append([]uuid.UUID(nil), request.Topic.SourceIDs...),
		Kinds:           append([]domain.ItemKind(nil), request.Topic.ItemKinds...),
		PublishedAfter:  request.PublishedAfter,
		PublishedBefore: request.PublishedBefore,
	}

	input := topicRankingInput{
		request: request, filter: filter, profile: profile,
		thresholds: thresholds, settings: workspaceSettings,
		retrievalLimit: workspaceSettings.Retrieval.CandidateLimit,
	}
	result, err := e.retrieveAndRank(ctx, input)
	if err != nil {
		return TopicResult{}, err
	}
	inputs := RankingInputs{
		PipelineVersion: PipelineVersion, TopicGateVersion: e.config.ThresholdDefaults.Version,
		EmbeddingModel: e.config.ThresholdDefaults.Model, TopicRevision: request.Topic.Revision,
		Thresholds: result.thresholds, Purpose: request.Purpose,
		PersonalizationVersion: PersonalKNNVersion, PreferenceRevision: profile.Revision,
		CandidateLimit: workspaceSettings.Retrieval.CandidateLimit,
	}
	inputs.RerankerVersion, inputs.RerankerModel, inputs.PromptVersion = e.rerankerInputs(request.Purpose, workspaceSettings)
	return TopicResult{Candidates: result.candidates, Inputs: inputs}, nil
}

func (e *Engine) workspaceSettings(
	ctx context.Context,
	snapshot *domain.WorkspaceSettings,
) (domain.WorkspaceSettings, error) {
	var settings domain.WorkspaceSettings
	if snapshot != nil {
		settings = *snapshot
	} else {
		var err error
		settings, err = e.config.WorkspaceSettings.GetWorkspace(ctx)
		if err != nil {
			return domain.WorkspaceSettings{}, fmt.Errorf("get ranking settings: %w", err)
		}
	}
	if !validEngineSettings(settings.Retrieval) {
		return domain.WorkspaceSettings{}, errors.New("ranking engine settings are invalid")
	}
	return settings, nil
}

type topicRankingInput struct {
	request        TopicRequest
	filter         IndexFilter
	profile        domain.PreferenceProfile
	thresholds     TopicThresholds
	settings       domain.WorkspaceSettings
	retrievalLimit int
}

type topicRankingResult struct {
	candidates []Candidate
	thresholds TopicThresholds
}

func (e *Engine) retrieveAndRank(ctx context.Context, input topicRankingInput) (topicRankingResult, error) {
	retrieved, err := e.config.Index.RetrieveTopic(ctx, TopicIndexRequest{
		TopicID: input.request.Topic.ID, CorrectionEpoch: input.request.Topic.CorrectionEpoch,
		Include: input.request.Topic.Include, Exclude: input.request.Topic.Exclude,
		Filter: input.filter, Limit: input.retrievalLimit,
	})
	if err != nil {
		return topicRankingResult{}, fmt.Errorf("retrieve topic candidates: %w", err)
	}
	corrections := currentTopicCorrections(retrieved, input.request.Topic)
	retrieved = mergeManualIncludes(retrieved, corrections)
	retrievers := topicRetrievers(retrieved)
	if len(retrievers) == 0 {
		return topicRankingResult{candidates: []Candidate{}, thresholds: input.thresholds}, nil
	}
	personalizer := &PersonalKNN{
		Index: e.config.Index, Filter: IndexFilter{WorkspaceID: input.filter.WorkspaceID},
		NeighborLimit: 12, Now: e.config.Now, PreserveCandidates: input.request.Purpose == PurposeSearch,
	}
	pipeline := Pipeline{
		Retrievers: retrievers,
		Gate: TopicSemanticGate{
			IncludeEmbedding: float64Vector(retrieved.IncludeEmbedding),
			ExcludeEmbedding: float64Vector(retrieved.ExcludeEmbedding),
			Thresholds:       input.thresholds, Corrections: correctionKeys(corrections),
		},
		Fusion:      ReciprocalRankFusion{K: input.settings.Retrieval.RRFK},
		Scorers:     e.scorers(personalizer, input.request.Purpose, input.settings.Retrieval),
		Reranker:    e.reranker(input, personalizer),
		Diversifier: e.diversifier(input.request.Purpose, input.settings.Retrieval),
	}
	result, err := pipeline.Recommend(ctx, Request{
		Profile:        Profile{Prefer: input.profile.Prefer, Avoid: input.profile.Avoid},
		CandidateLimit: input.retrievalLimit, ResultLimit: input.request.Limit,
	})
	if err != nil {
		return topicRankingResult{}, fmt.Errorf("rank topic candidates: %w", err)
	}
	return topicRankingResult{candidates: result, thresholds: input.thresholds}, nil
}

func (e *Engine) reranker(input topicRankingInput, evidence FeedbackEvidence) Reranker {
	if input.request.Purpose != PurposeDigest || !input.settings.Retrieval.LLMRerankEnabled ||
		!input.settings.Generation.Enabled || e.config.LLMFactory == nil || e.config.Items == nil {
		return IdentityReranker{}
	}
	client, _ := e.config.LLMFactory.NewRerankClient(input.settings.Generation)
	limit := min(60, max(30, input.settings.Retrieval.RerankLimit, 3*input.request.Limit))
	return DigestLLMReranker{
		Client: client, Items: e.config.Items, WorkspaceID: input.request.Topic.WorkspaceID,
		Topic: input.request.Topic, Profile: input.profile,
		Evidence:    evidence,
		Instruction: input.settings.Instructions.DigestRerank, Limit: limit, Timeout: 30 * time.Second,
	}
}

func (e *Engine) rerankerInputs(purpose Purpose, settings domain.WorkspaceSettings) (string, string, string) {
	if purpose != PurposeDigest || !settings.Retrieval.LLMRerankEnabled || !settings.Generation.Enabled ||
		e.config.LLMFactory == nil || e.config.Items == nil {
		return "", "", ""
	}
	model := ""
	if settings.Generation.RerankModel != nil {
		model = *settings.Generation.RerankModel
	}
	prompt := fmt.Sprintf("%s/settings-%d", DigestRerankPromptVersion, settings.Version)
	return DigestLLMRerankerVersion, model, prompt
}

func (e *Engine) scorers(personalizer *PersonalKNN, purpose Purpose, settings domain.RetrievalSettings) []Scorer {
	result := make([]Scorer, 0, 3)
	result = append(result, personalizer)
	if purpose == PurposeSearch {
		return result
	}
	result = append(result, RecencyBoost{
		Now: e.config.Now, HalfLife: time.Duration(settings.RecencyHalfLifeDays * float64(24*time.Hour)),
		MaxBoost: settings.RecencyMaxBoost,
	})
	if purpose == PurposeFeed {
		result = append(result, SourceCap{Limit: settings.FeedSourceCap})
	}
	return result
}

func currentTopicCorrections(retrieved TopicRetrieval, topic domain.Topic) map[uuid.UUID]domain.TopicMembership {
	result := make(map[uuid.UUID]domain.TopicMembership)
	for _, hits := range [][]IndexHit{retrieved.Dense, retrieved.Sparse, retrieved.Recent, retrieved.Corrections} {
		for _, hit := range hits {
			for _, correction := range hit.TopicCorrections {
				if correction.TopicID == topic.ID && correction.CorrectionEpoch == topic.CorrectionEpoch {
					result[hit.ItemID] = correction.Membership
				}
			}
		}
	}
	return result
}

func mergeManualIncludes(
	retrieved TopicRetrieval,
	corrections map[uuid.UUID]domain.TopicMembership,
) TopicRetrieval {
	manual := make([]IndexHit, 0)
	for _, hit := range retrieved.Corrections {
		if corrections[hit.ItemID] == domain.TopicMembershipInclude {
			manual = append(manual, hit)
		}
	}
	if len(manual) == 0 {
		return retrieved
	}
	if len(retrieved.IncludeEmbedding) > 0 {
		include := float64Vector(retrieved.IncludeEmbedding)
		for index := range manual {
			manual[index].Score = cosine(float64Vector(manual[index].Embedding), include)
		}
		retrieved.Dense = mergeHits(retrieved.Dense, manual)
		sort.SliceStable(retrieved.Dense, func(left, right int) bool {
			return retrieved.Dense[left].Score > retrieved.Dense[right].Score
		})
		return retrieved
	}
	retrieved.Recent = mergeHits(retrieved.Recent, manual)
	sort.SliceStable(retrieved.Recent, func(left, right int) bool {
		leftTime, rightTime := retrieved.Recent[left].PublishedAt, retrieved.Recent[right].PublishedAt
		if leftTime == nil {
			return false
		}
		return rightTime == nil || leftTime.After(*rightTime)
	})
	return retrieved
}

func mergeHits(primary, additions []IndexHit) []IndexHit {
	result := append([]IndexHit(nil), primary...)
	present := make(map[uuid.UUID]struct{}, len(primary))
	for _, hit := range primary {
		present[hit.ItemID] = struct{}{}
	}
	for _, hit := range additions {
		if _, found := present[hit.ItemID]; found {
			continue
		}
		result = append(result, hit)
		present[hit.ItemID] = struct{}{}
	}
	return result
}

func topicRetrievers(retrieved TopicRetrieval) []Retriever {
	result := make([]Retriever, 0, 3)
	if len(retrieved.Dense) > 0 {
		result = append(result, StaticRetriever{RetrieverName: "dense", Candidates: candidatesFromHits(retrieved.Dense, "dense")})
	}
	if len(retrieved.Sparse) > 0 {
		result = append(result, StaticRetriever{RetrieverName: "sparse", Candidates: candidatesFromHits(retrieved.Sparse, "sparse")})
	}
	if len(retrieved.Recent) > 0 {
		result = append(result, StaticRetriever{RetrieverName: "recent", Candidates: candidatesFromHits(retrieved.Recent, "recent")})
	}
	return result
}

func correctionKeys(source map[uuid.UUID]domain.TopicMembership) map[string]domain.TopicMembership {
	result := make(map[string]domain.TopicMembership, len(source))
	for itemID, membership := range source {
		result[itemID.String()] = membership
	}
	return result
}

func float64Vector(source []float32) []float64 {
	result := make([]float64, len(source))
	for index, value := range source {
		result[index] = float64(value)
	}
	return result
}

func (e *Engine) diversifier(purpose Purpose, retrieval domain.RetrievalSettings) Diversifier {
	if purpose == PurposeDigest {
		return DigestDiversifier{
			MMR:                MMR{Lambda: retrieval.DigestMMRLambda},
			SourceLimit:        retrieval.DigestSourceCap,
			ExplorationEnabled: retrieval.DigestExplorationEnabled,
		}
	}
	return TopN{}
}

func validateTopicRequest(request TopicRequest) error {
	if request.Topic.ID == uuid.Nil || request.Topic.WorkspaceID == uuid.Nil || !request.Topic.Enabled {
		return errors.New("enabled topic with workspace and ID is required")
	}
	if request.Purpose != PurposeFeed && request.Purpose != PurposeDigest && request.Purpose != PurposeSearch {
		return errors.New("ranking purpose must be feed, digest, or search")
	}
	if request.Limit < 1 || request.Limit > 100 {
		return errors.New("topic result limit must be between 1 and 100")
	}
	return nil
}
