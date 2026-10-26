package ranking

import (
	"context"
	"errors"
	"fmt"
	"math"
	"sort"
	"time"

	"github.com/ethernity/obsidian-topic-stream/internal/domain"
	"github.com/google/uuid"
)

const (
	PersonalKNNVersion      = "personal-knn-v1"
	personalHalfLife        = 180 * 24 * time.Hour
	personalEvidenceLimit   = 3
	personalPositiveWeight  = 0.25
	personalNegativeWeight  = 0.50
	strongNegativeThreshold = 0.80
)

// FeedbackNeighborhood pairs one current feedback judgment with similar indexed Items.
type FeedbackNeighborhood struct {
	Feedback domain.ItemFeedback
	Hits     []IndexHit
}

// PersonalKNN scores candidates from direct feedback and positive and negative similarity neighborhoods.
type PersonalKNN struct {
	Index         PersonalIndex
	Filter        IndexFilter
	NeighborLimit int
	FeedbackIDs   []uuid.UUID
	Positive      []FeedbackNeighborhood
	Negative      []FeedbackNeighborhood
	Now           func() time.Time
	// PreserveCandidates keeps strong negative evidence as a score signal instead of filtering the
	// candidate. Search uses this mode to preserve recall.
	PreserveCandidates bool
	evidence           PersonalRetrieval
}

// PersonalIndexRequest bounds feedback evidence and neighborhoods for the current candidate set.
type PersonalIndexRequest struct {
	CandidateIDs  []uuid.UUID
	Filter        IndexFilter
	NeighborLimit int
	FeedbackIDs   []uuid.UUID
	Direct        map[uuid.UUID]domain.Feedback
}

// PersonalRetrieval contains direct feedback and its positive and negative neighborhoods.
type PersonalRetrieval struct {
	Positive []FeedbackNeighborhood
	Negative []FeedbackNeighborhood
	Direct   map[uuid.UUID]domain.Feedback
}

// PersonalIndex retrieves current feedback and its similarity neighborhoods.
type PersonalIndex interface {
	// RetrievePersonal returns direct feedback and positive and negative similarity neighborhoods.
	RetrievePersonal(ctx context.Context, request PersonalIndexRequest) (PersonalRetrieval, error)
}

// Score applies current feedback and decayed similarity evidence, optionally suppressing negative candidates.
// The retrieved neighborhoods remain available to downstream reranking through FeedbackNeighborhoods.
func (s *PersonalKNN) Score(ctx context.Context, _ Profile, candidates []Candidate) ([]Candidate, error) {
	if s.Now == nil {
		return nil, errors.New("personal kNN clock is required")
	}
	return s.score(ctx, candidates)
}

func (s *PersonalKNN) score(ctx context.Context, candidates []Candidate) ([]Candidate, error) {
	positive, negative := s.Positive, s.Negative
	direct := make(map[uuid.UUID]domain.Feedback)
	if s.Index != nil {
		request, err := s.personalRequest(candidates)
		if err != nil {
			return nil, err
		}
		retrieved, err := s.Index.RetrievePersonal(ctx, request)
		if err != nil {
			return nil, fmt.Errorf("retrieve personal evidence: %w", err)
		}
		positive, negative, direct = retrieved.Positive, retrieved.Negative, retrieved.Direct
	}
	s.evidence = PersonalRetrieval{Positive: positive, Negative: negative, Direct: direct}
	now := s.Now()
	evidence := personalScoreContext{
		positive: personalEvidence(positive, now), negative: personalEvidence(negative, now),
		directLikes: feedbackIDs(positive), directDislikes: feedbackIDs(negative),
		preserveCandidates: s.PreserveCandidates,
	}
	addDirectCandidateFeedback(evidence.directLikes, evidence.directDislikes, candidates, direct)
	result := make([]Candidate, 0, len(candidates))
	for _, candidate := range candidates {
		personalized, keep := personalizeCandidate(cloneCandidate(candidate), evidence)
		if keep {
			result = append(result, personalized)
		}
	}
	sortCandidates(result)
	return result, nil
}

func (s *PersonalKNN) personalRequest(candidates []Candidate) (PersonalIndexRequest, error) {
	request := PersonalIndexRequest{
		CandidateIDs:  make([]uuid.UUID, len(candidates)),
		Filter:        s.Filter,
		NeighborLimit: s.NeighborLimit,
		FeedbackIDs:   append([]uuid.UUID(nil), s.FeedbackIDs...),
		Direct:        make(map[uuid.UUID]domain.Feedback),
	}
	if request.NeighborLimit == 0 {
		request.NeighborLimit = 12
	}
	trainingItems := idSet(s.FeedbackIDs)
	for index, candidate := range candidates {
		itemID, err := uuid.Parse(candidate.ItemID)
		if err != nil {
			return PersonalIndexRequest{}, fmt.Errorf("parse personalization candidate ID: %w", err)
		}
		request.CandidateIDs[index] = itemID
		if candidate.Feedback != nil && (len(trainingItems) == 0 || trainingItems[itemID]) {
			request.Direct[itemID] = *candidate.Feedback
		}
	}
	return request, nil
}

func idSet(ids []uuid.UUID) map[uuid.UUID]bool {
	result := make(map[uuid.UUID]bool, len(ids))
	for _, id := range ids {
		result[id] = true
	}
	return result
}

// FeedbackNeighborhoods returns the positive and negative feedback evidence used for scoring.
func (s *PersonalKNN) FeedbackNeighborhoods() ([]FeedbackNeighborhood, []FeedbackNeighborhood) {
	return s.evidence.Positive, s.evidence.Negative
}

func addDirectCandidateFeedback(
	likes, dislikes map[string]struct{},
	candidates []Candidate,
	retrieved map[uuid.UUID]domain.Feedback,
) {
	for _, candidate := range candidates {
		feedback := candidate.Feedback
		if feedback == nil {
			if itemID, err := uuid.Parse(candidate.ItemID); err == nil {
				if value, found := retrieved[itemID]; found {
					feedback = &value
				}
			}
		}
		if feedback == nil {
			continue
		}
		switch feedback.Sentiment {
		case "like":
			likes[candidate.ItemID] = struct{}{}
		case "dislike":
			dislikes[candidate.ItemID] = struct{}{}
		}
	}
}

type personalScoreContext struct {
	positive           map[string][]float64
	negative           map[string][]float64
	directLikes        map[string]struct{}
	directDislikes     map[string]struct{}
	preserveCandidates bool
}

func personalizeCandidate(candidate Candidate, evidence personalScoreContext) (Candidate, bool) {
	positive, negative, liked, disliked := personalScores(candidate.ItemID, evidence)
	if evidence.suppresses(positive, negative, liked, disliked) {
		return Candidate{}, false
	}
	multiplier := 1 + personalPositiveWeight*positive - personalNegativeWeight*negative
	candidate.Score *= max(multiplier, 0)
	signals := ensureSignals(&candidate)
	signals["personal.positive"] = positive
	signals["personal.negative"] = negative
	signals["personal.multiplier"] = multiplier
	addDirectFeedbackSignals(signals, liked, disliked)
	return candidate, true
}

func personalScores(itemID string, evidence personalScoreContext) (float64, float64, bool, bool) {
	_, liked := evidence.directLikes[itemID]
	_, disliked := evidence.directDislikes[itemID]
	positive := strongestMean(evidence.positive[itemID], personalEvidenceLimit)
	negative := strongestMean(evidence.negative[itemID], personalEvidenceLimit)
	if liked {
		positive = max(positive, 1)
	}
	if disliked {
		negative = max(negative, 1)
	}
	return positive, negative, liked, disliked
}

func (e personalScoreContext) suppresses(positive, negative float64, liked, disliked bool) bool {
	if e.preserveCandidates {
		return false
	}
	return disliked || !liked && negative >= strongNegativeThreshold && negative > positive
}

func addDirectFeedbackSignals(signals map[string]float64, liked, disliked bool) {
	if liked {
		signals["personal.directLike"] = 1
	}
	if disliked {
		signals["personal.directDislike"] = 1
	}
}

func personalEvidence(neighborhoods []FeedbackNeighborhood, now time.Time) map[string][]float64 {
	result := make(map[string][]float64)
	for _, neighborhood := range neighborhoods {
		age := now.Sub(neighborhood.Feedback.Feedback.UpdatedAt)
		if age < 0 {
			age = 0
		}
		decay := math.Pow(0.5, float64(age)/float64(personalHalfLife))
		for _, hit := range neighborhood.Hits {
			if hit.Score <= 0 {
				continue
			}
			itemID := hit.ItemID.String()
			result[itemID] = append(result[itemID], hit.Score*decay)
		}
	}
	return result
}

func feedbackIDs(neighborhoods []FeedbackNeighborhood) map[string]struct{} {
	result := make(map[string]struct{}, len(neighborhoods))
	for _, neighborhood := range neighborhoods {
		result[neighborhood.Feedback.ItemID.String()] = struct{}{}
	}
	return result
}

func strongestMean(values []float64, limit int) float64 {
	if len(values) == 0 {
		return 0
	}
	values = append([]float64(nil), values...)
	sort.Sort(sort.Reverse(sort.Float64Slice(values)))
	if len(values) > limit {
		values = values[:limit]
	}
	var total float64
	for _, value := range values {
		total += value
	}
	return total / float64(len(values))
}
