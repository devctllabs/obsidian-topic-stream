package ranking

import (
	"context"
	"errors"
	"fmt"
	"math"
	"sort"
	"time"

	"github.com/ethernity/obsidian-topic-stream/internal/domain"
)

// Candidate carries both the current score and named explainability signals accumulated by ranking
// stages. Embedding is transient and deliberately excluded from persisted JSON.
type Candidate struct {
	ItemID      string             `json:"itemId"`
	SourceID    string             `json:"sourceId,omitempty"`
	PublishedAt time.Time          `json:"publishedAt,omitempty"`
	Embedding   []float64          `json:"-"`
	Score       float64            `json:"score"`
	Signals     map[string]float64 `json:"signals"`
	Feedback    *domain.Feedback   `json:"-"`
}

// Profile supplies Prefer and Avoid text to retrieval and scoring stages.
type Profile struct {
	Prefer string
	Avoid  string
}

// Request bounds the retrieved pool and final result count for one pipeline operation.
type Request struct {
	Profile        Profile
	CandidateLimit int
	ResultLimit    int
}

// Retriever produces one named, bounded ranking input list.
type Retriever interface {
	// Name returns the stable retriever name used by fusion weights and score signals.
	Name() string
	// Retrieve returns a bounded candidate list for the preference guidance.
	Retrieve(ctx context.Context, profile Profile, limit int) ([]Candidate, error)
}

// Retrieval pairs a stable retriever name with its ordered candidate list.
type Retrieval struct {
	Name       string
	Candidates []Candidate
}

// Fusion merges ranked retrieval lists by Item identity.
type Fusion interface {
	// Fuse combines retrieval lists into one candidate list, deduplicating by Item ID.
	Fuse(retrievals []Retrieval) ([]Candidate, error)
}

// Gate applies Topic membership before retrieval lists are fused.
type Gate interface {
	// Apply filters retrieval lists using Topic membership rules before fusion.
	Apply(ctx context.Context, retrievals []Retrieval) ([]Retrieval, error)
}

// Scorer adds ranking policy scores and explanation signals to candidates.
type Scorer interface {
	// Score scores candidates using the supplied preference guidance.
	Score(ctx context.Context, profile Profile, candidates []Candidate) ([]Candidate, error)
}

// Reranker reorders candidates after deterministic scoring.
type Reranker interface {
	// Rerank returns a revised candidate order for the supplied ranking inputs.
	Rerank(ctx context.Context, profile Profile, candidates []Candidate) ([]Candidate, error)
}

// Diversifier selects a bounded result set with a diversity policy.
type Diversifier interface {
	// Diversify selects at most limit candidates according to the diversification policy.
	Diversify(candidates []Candidate, limit int) ([]Candidate, error)
}

// Pipeline fixes the ranking order: retrieve, gate, fuse, trim to CandidateLimit, score, rerank,
// then diversify to ResultLimit.
type Pipeline struct {
	Retrievers  []Retriever
	Gate        Gate
	Fusion      Fusion
	Scorers     []Scorer
	Reranker    Reranker
	Diversifier Diversifier
}

// Recommend runs retrieval, optional gating, fusion, scoring, reranking, and final diversification in
// order.
func (p Pipeline) Recommend(ctx context.Context, request Request) ([]Candidate, error) {
	if err := p.validate(); err != nil {
		return nil, err
	}
	request, err := normalizeRequest(request)
	if err != nil {
		return nil, err
	}

	lists, err := retrieveCandidates(ctx, p.Retrievers, request)
	if err != nil {
		return nil, err
	}
	if p.Gate != nil {
		lists, err = p.Gate.Apply(ctx, lists)
		if err != nil {
			return nil, fmt.Errorf("gate candidates: %w", err)
		}
	}

	candidates, err := p.Fusion.Fuse(lists)
	if err != nil {
		return nil, fmt.Errorf("fuse candidates: %w", err)
	}
	if len(candidates) > request.CandidateLimit {
		candidates = candidates[:request.CandidateLimit]
	}
	candidates, err = scoreCandidates(ctx, p.Scorers, request.Profile, candidates)
	if err != nil {
		return nil, err
	}
	candidates, err = p.Reranker.Rerank(ctx, request.Profile, candidates)
	if err != nil {
		return nil, fmt.Errorf("rerank candidates: %w", err)
	}
	return p.Diversifier.Diversify(candidates, request.ResultLimit)
}

func normalizeRequest(request Request) (Request, error) {
	if request.CandidateLimit <= 0 {
		request.CandidateLimit = 100
	}
	if request.ResultLimit <= 0 {
		return Request{}, errors.New("result limit must be positive")
	}
	return request, nil
}

func (p Pipeline) validate() error {
	if p.Fusion == nil {
		return errors.New("ranking pipeline requires fusion")
	}
	if len(p.Retrievers) == 0 {
		return errors.New("ranking pipeline requires at least one retriever")
	}
	if p.Reranker == nil {
		return errors.New("ranking pipeline requires reranker")
	}
	if p.Diversifier == nil {
		return errors.New("ranking pipeline requires diversifier")
	}
	return nil
}

func retrieveCandidates(ctx context.Context, retrievers []Retriever, request Request) ([]Retrieval, error) {
	lists := make([]Retrieval, 0, len(retrievers))
	for _, retriever := range retrievers {
		candidates, err := retriever.Retrieve(ctx, request.Profile, request.CandidateLimit)
		if err != nil {
			return nil, fmt.Errorf("retrieve %s: %w", retriever.Name(), err)
		}
		lists = append(lists, Retrieval{Name: retriever.Name(), Candidates: candidates})
	}
	return lists, nil
}

func scoreCandidates(
	ctx context.Context,
	scorers []Scorer,
	profile Profile,
	candidates []Candidate,
) ([]Candidate, error) {
	var err error
	for _, scorer := range scorers {
		candidates, err = scorer.Score(ctx, profile, candidates)
		if err != nil {
			return nil, fmt.Errorf("score candidates: %w", err)
		}
	}
	return candidates, nil
}

// StaticRetriever exposes a precomputed candidate list as a named retrieval branch.
type StaticRetriever struct {
	RetrieverName string
	Candidates    []Candidate
}

// Name returns the stable retriever name used by fusion weights and score signals.
func (r StaticRetriever) Name() string { return r.RetrieverName }

// Retrieve returns a bounded copy of the precomputed candidate list.
func (r StaticRetriever) Retrieve(_ context.Context, _ Profile, limit int) ([]Candidate, error) {
	if limit > len(r.Candidates) {
		limit = len(r.Candidates)
	}
	return append([]Candidate(nil), r.Candidates[:limit]...), nil
}

// ReciprocalRankFusion merges candidates by ItemID and records each retrieval list's contribution
// as an rrf.<name> signal. Missing weights default to one and K defaults to 60.
type ReciprocalRankFusion struct {
	K int
	// Weights defaults each omitted retriever name to weight 1.
	Weights map[string]float64
}

// Fuse deduplicates by Item ID and combines weighted reciprocal ranks from each retrieval list.
func (f ReciprocalRankFusion) Fuse(lists []Retrieval) ([]Candidate, error) {
	if f.K < 0 {
		return nil, errors.New("RRF k cannot be negative")
	}
	k := f.K
	if k == 0 {
		k = 60
	}

	byID := make(map[string]Candidate)
	for _, list := range lists {
		weight := fusionWeight(f.Weights, list.Name)
		for index, candidate := range list.Candidates {
			if err := mergeFusedCandidate(byID, list.Name, candidate, weight/float64(k+index+1)); err != nil {
				return nil, err
			}
		}
	}

	result := make([]Candidate, 0, len(byID))
	for _, candidate := range byID {
		result = append(result, candidate)
	}
	sortCandidates(result)
	return result, nil
}

func fusionWeight(weights map[string]float64, name string) float64 {
	if configured, ok := weights[name]; ok {
		return configured
	}
	return 1
}

func mergeFusedCandidate(byID map[string]Candidate, name string, candidate Candidate, contribution float64) error {
	if candidate.ItemID == "" {
		return fmt.Errorf("retriever %s returned an empty item ID", name)
	}
	current, exists := byID[candidate.ItemID]
	if !exists {
		current = candidate
		current.Score = 0
		current.Signals = cloneSignals(candidate.Signals)
	} else {
		for signal, value := range candidate.Signals {
			current.Signals[signal] = value
		}
	}
	current.Score += contribution
	current.Signals["rrf."+name] = contribution
	byID[candidate.ItemID] = current
	return nil
}

func cloneSignals(source map[string]float64) map[string]float64 {
	result := make(map[string]float64, len(source)+1)
	for name, value := range source {
		result[name] = value
	}
	return result
}

// RecencyBoost multiplies scores by a bounded, exponentially decaying age bonus.
type RecencyBoost struct {
	Now func() time.Time
	// HalfLife is the elapsed time after which the age bonus halves.
	HalfLife time.Duration
	MaxBoost float64
}

// Score multiplies relevance by an age-dependent factor between 1 and 1+MaxBoost.
func (s RecencyBoost) Score(_ context.Context, _ Profile, candidates []Candidate) ([]Candidate, error) {
	if s.Now == nil || s.HalfLife <= 0 || s.MaxBoost < 0 {
		return nil, errors.New("invalid recency boost configuration")
	}
	result := cloneCandidates(candidates)
	now := s.Now()
	for index := range result {
		if result[index].PublishedAt.IsZero() {
			continue
		}
		age := now.Sub(result[index].PublishedAt)
		if age < 0 {
			age = 0
		}
		boost := s.MaxBoost * math.Pow(0.5, float64(age)/float64(s.HalfLife))
		result[index].Score *= 1 + boost
		ensureSignals(&result[index])["recency"] = boost
	}
	sortCandidates(result)
	return result, nil
}

// SourceCap is a soft diversity constraint: excess candidates move behind candidates within the
// cap instead of being discarded.
type SourceCap struct {
	Limit int
}

// Score moves candidates beyond each Source's soft cap behind candidates within the cap.
func (s SourceCap) Score(_ context.Context, _ Profile, candidates []Candidate) ([]Candidate, error) {
	if s.Limit <= 0 {
		return nil, errors.New("source cap must be positive")
	}
	counts := make(map[string]int)
	result := make([]Candidate, 0, len(candidates))
	overflow := make([]Candidate, 0, len(candidates))
	for _, candidate := range candidates {
		if counts[candidate.SourceID] >= s.Limit {
			overflow = append(overflow, candidate)
			continue
		}
		counts[candidate.SourceID]++
		result = append(result, candidate)
	}
	return append(result, overflow...), nil
}

// IdentityReranker preserves the candidate order when reranking is disabled.
type IdentityReranker struct{}

// Rerank preserves candidate order and returns a copy.
func (IdentityReranker) Rerank(_ context.Context, _ Profile, candidates []Candidate) ([]Candidate, error) {
	return cloneCandidates(candidates), nil
}

// TopN preserves ranking order while truncating the result to the requested limit.
type TopN struct{}

// Diversify returns the first limit candidates without changing ranked order.
func (TopN) Diversify(candidates []Candidate, limit int) ([]Candidate, error) {
	if limit <= 0 {
		return nil, errors.New("result limit must be positive")
	}
	if limit > len(candidates) {
		limit = len(candidates)
	}
	return cloneCandidates(candidates[:limit]), nil
}

// MMR balances the incoming relevance score against embedding similarity to already selected
// candidates. Lambda one is relevance-only; zero is diversity-only.
type MMR struct {
	// Lambda ranges from 0 to 1; higher values favor relevance over diversity.
	Lambda float64
}

// DigestDiversifier enforces a soft Source limit while MMR selects the final set. When exploration
// is active, its reserved final candidate consumes Source capacity before the other slots are chosen.
type DigestDiversifier struct {
	MMR                MMR
	SourceLimit        int
	ExplorationEnabled bool
}

// Diversify applies MMR and source balancing, optionally reserving an exploration result.
func (d DigestDiversifier) Diversify(candidates []Candidate, limit int) ([]Candidate, error) {
	if d.SourceLimit <= 0 {
		return nil, errors.New("digest source limit must be positive")
	}
	if !d.ExplorationEnabled || limit < 5 {
		return d.MMR.diversify(candidates, limit, d.SourceLimit, nil)
	}
	explorationIndex := selectExploration(candidates)
	if explorationIndex < 0 || len(candidates) < limit {
		return d.MMR.diversify(candidates, limit, d.SourceLimit, nil)
	}
	exploration := cloneCandidate(candidates[explorationIndex])
	remaining := append(cloneCandidates(candidates[:explorationIndex]), candidates[explorationIndex+1:]...)
	selected, err := d.MMR.diversify(
		remaining,
		limit-1,
		d.SourceLimit,
		map[string]int{exploration.SourceID: 1},
	)
	if err != nil {
		return nil, err
	}
	ensureSignals(&exploration)["exploration"] = 1
	return append(selected, exploration), nil
}

func selectExploration(candidates []Candidate) int {
	hasKnown := false
	for _, candidate := range candidates {
		if !unknownPreference(candidate) {
			hasKnown = true
			break
		}
	}
	if !hasKnown {
		return -1
	}
	for index, candidate := range candidates {
		if unknownPreference(candidate) {
			return index
		}
	}
	return -1
}

func unknownPreference(candidate Candidate) bool {
	for _, name := range []string{
		"personal.positive", "personal.negative", "personal.directLike", "personal.directDislike",
	} {
		if candidate.Signals[name] != 0 {
			return false
		}
	}
	return true
}

// Diversify balances relevance against similarity to already selected candidates.
func (m MMR) Diversify(candidates []Candidate, limit int) ([]Candidate, error) {
	return m.diversify(candidates, limit, 0, nil)
}

func (m MMR) diversify(
	candidates []Candidate,
	limit int,
	sourceLimit int,
	initialSourceCounts map[string]int,
) ([]Candidate, error) {
	if m.Lambda < 0 || m.Lambda > 1 {
		return nil, errors.New("MMR lambda must be between zero and one")
	}
	if limit <= 0 {
		return nil, errors.New("MMR limit must be positive")
	}
	if limit > len(candidates) {
		limit = len(candidates)
	}
	remaining := cloneCandidates(candidates)
	selected := make([]Candidate, 0, limit)
	sources := newMMRSourceLimit(sourceLimit, initialSourceCounts)
	for len(selected) < limit {
		bestIndex, bestScore := bestMMRCandidate(m.Lambda, remaining, selected, sources)
		chosen := remaining[bestIndex]
		ensureSignals(&chosen)["mmr"] = bestScore
		selected = append(selected, chosen)
		sources.record(chosen)
		remaining = append(remaining[:bestIndex], remaining[bestIndex+1:]...)
	}
	return selected, nil
}

type mmrSourceLimit struct {
	limit  int
	counts map[string]int
}

func newMMRSourceLimit(limit int, initialCounts map[string]int) mmrSourceLimit {
	counts := make(map[string]int, len(initialCounts))
	for sourceID, count := range initialCounts {
		counts[sourceID] = count
	}
	return mmrSourceLimit{limit: limit, counts: counts}
}

func (s mmrSourceLimit) enforced(candidates []Candidate) bool {
	if s.limit <= 0 {
		return false
	}
	for _, candidate := range candidates {
		if s.counts[candidate.SourceID] < s.limit {
			return true
		}
	}
	return false
}

func (s mmrSourceLimit) allows(candidate Candidate, enforced bool) bool {
	return !enforced || s.counts[candidate.SourceID] < s.limit
}

func (s *mmrSourceLimit) record(candidate Candidate) {
	s.counts[candidate.SourceID]++
}

func bestMMRCandidate(
	lambda float64,
	candidates []Candidate,
	selected []Candidate,
	sources mmrSourceLimit,
) (int, float64) {
	bestIndex, bestScore := -1, math.Inf(-1)
	enforceSourceLimit := sources.enforced(candidates)
	for index, candidate := range candidates {
		if !sources.allows(candidate, enforceSourceLimit) {
			continue
		}
		score := lambda*candidate.Score - (1-lambda)*maximumSimilarity(candidate, selected)
		if score > bestScore {
			bestIndex, bestScore = index, score
		}
	}
	return bestIndex, bestScore
}

func maximumSimilarity(candidate Candidate, selected []Candidate) float64 {
	result := 0.0
	for _, chosen := range selected {
		result = max(result, cosine(candidate.Embedding, chosen.Embedding))
	}
	return result
}

func cosine(left, right []float64) float64 {
	if len(left) == 0 || len(left) != len(right) {
		return 0
	}
	var dot, leftNorm, rightNorm float64
	for index := range left {
		dot += left[index] * right[index]
		leftNorm += left[index] * left[index]
		rightNorm += right[index] * right[index]
	}
	if leftNorm == 0 || rightNorm == 0 {
		return 0
	}
	return dot / (math.Sqrt(leftNorm) * math.Sqrt(rightNorm))
}

func cloneCandidates(candidates []Candidate) []Candidate {
	result := make([]Candidate, len(candidates))
	for index, candidate := range candidates {
		result[index] = cloneCandidate(candidate)
	}
	return result
}

func cloneCandidate(candidate Candidate) Candidate {
	result := candidate
	result.Embedding = append([]float64(nil), candidate.Embedding...)
	result.Signals = cloneSignals(candidate.Signals)
	return result
}

func ensureSignals(candidate *Candidate) map[string]float64 {
	if candidate.Signals == nil {
		candidate.Signals = make(map[string]float64)
	}
	return candidate.Signals
}

func sortCandidates(candidates []Candidate) {
	sort.SliceStable(candidates, func(left, right int) bool {
		if candidates[left].Score == candidates[right].Score {
			return candidates[left].ItemID < candidates[right].ItemID
		}
		return candidates[left].Score > candidates[right].Score
	})
}
