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

type personalIndexStub struct {
	request ranking.PersonalIndexRequest
	result  ranking.PersonalRetrieval
}

// RetrievePersonal returns configured personal-feedback evidence.
func (s *personalIndexStub) RetrievePersonal(
	_ context.Context,
	request ranking.PersonalIndexRequest,
) (ranking.PersonalRetrieval, error) {
	s.request = request
	return s.result, nil
}

func TestTopicSemanticGateFiltersUnionBeforeRRFAndHonorsCorrections(t *testing.T) {
	t.Parallel()

	dense := ranking.StaticRetriever{RetrieverName: "dense", Candidates: []ranking.Candidate{
		{ItemID: "automatic-include", Embedding: []float64{1, 0}},
		{ItemID: "manual-include", Embedding: []float64{0, 1}},
		{ItemID: "manual-exclude", Embedding: []float64{1, 0}},
	}}
	sparse := ranking.StaticRetriever{RetrieverName: "sparse", Candidates: []ranking.Candidate{
		{ItemID: "manual-include", Embedding: []float64{0, 1}},
		{ItemID: "automatic-include", Embedding: []float64{1, 0}},
		{ItemID: "manual-exclude", Embedding: []float64{1, 0}},
	}}
	pipeline := ranking.Pipeline{
		Retrievers: []ranking.Retriever{dense, sparse},
		Gate: ranking.TopicSemanticGate{
			IncludeEmbedding: []float64{1, 0},
			Thresholds:       ranking.TopicThresholds{Include: 0.8, Exclude: 0.8},
			Corrections: map[string]domain.TopicMembership{
				"manual-include": domain.TopicMembershipInclude,
				"manual-exclude": domain.TopicMembershipExclude,
			},
		},
		Fusion: ranking.ReciprocalRankFusion{K: 60}, Reranker: ranking.IdentityReranker{}, Diversifier: ranking.TopN{},
	}

	result, err := pipeline.Recommend(t.Context(), ranking.Request{CandidateLimit: 10, ResultLimit: 10})

	require.NoError(t, err)
	require.Equal(t, []string{"automatic-include", "manual-include"}, candidateIDs(result))
	require.Contains(t, result[0].Signals, "topic.includeCosine")
	require.Equal(t, 1.0, result[0].Signals["topic.accepted"])
	require.Equal(t, 1.0, result[1].Signals["topic.manualInclude"])
	require.Contains(t, result[1].Signals, "rrf.dense")
	require.Contains(t, result[1].Signals, "rrf.sparse")
}

func TestTopicThresholdCalibrationRequiresFiveCorrectionsPerPolarityAndUsesModeBeta(t *testing.T) {
	t.Parallel()

	defaults := ranking.TopicThresholdDefaults{
		Model: "embedding-model", Version: "topic-gate-v1", Shrinkage: 2,
		Ordinary: ranking.TopicThresholds{Include: 0.5, Exclude: 0.5},
		Strict:   ranking.TopicThresholds{Include: 0.5, Exclude: 0.5},
	}
	insufficient := []ranking.TopicCalibrationSample{
		{IncludeCosine: 0.9, ExcludeCosine: 0.1, Membership: domain.TopicMembershipInclude},
		{IncludeCosine: 0.1, ExcludeCosine: 0.9, Membership: domain.TopicMembershipExclude},
	}

	unchanged, calibrated, err := defaults.Calibrate(domain.TopicModeOrdinary, insufficient)
	require.NoError(t, err)
	require.False(t, calibrated)
	require.Equal(t, defaults.Ordinary, unchanged)

	samples := calibrationSamples()
	ordinary, ordinaryCalibrated, err := defaults.Calibrate(domain.TopicModeOrdinary, samples)
	require.NoError(t, err)
	strict, strictCalibrated, err := defaults.Calibrate(domain.TopicModeStrict, samples)
	require.NoError(t, err)
	require.True(t, ordinaryCalibrated)
	require.True(t, strictCalibrated)
	require.Less(t, ordinary.Include, strict.Include, "F2 favors recall while F0.5 favors precision")
	require.Greater(t, ordinary.Exclude, strict.Exclude, "F0.5 rejects cautiously while F2 rejects broadly")
	require.Greater(t, ordinary.Include, 0.3, "shrinkage keeps calibration between empirical and default values")
}

func TestDefaultTopicThresholdsAreVersionedForTheActiveEmbeddingModel(t *testing.T) {
	t.Parallel()

	defaults := ranking.DefaultTopicThresholdDefaults(" qwen3-embedding:0.6b ")

	require.Equal(t, "qwen3-embedding:0.6b", defaults.Model)
	require.Equal(t, "topic-gate-bootstrap-v1", defaults.Version)
	require.Equal(t, ranking.TopicThresholds{Include: 0.35, Exclude: 0.75}, defaults.Ordinary)
	require.Equal(t, ranking.TopicThresholds{Include: 0.5, Exclude: 0.6}, defaults.Strict)
	require.Equal(t, 10.0, defaults.Shrinkage)
}

func calibrationSamples() []ranking.TopicCalibrationSample {
	includePositive := []float64{0.9, 0.8, 0.7, 0.4, 0.3}
	includeNegative := []float64{0.65, 0.6, 0.55, 0.2, 0.1}
	excludePositive := []float64{0.9, 0.85, 0.7, 0.45, 0.35}
	excludeNegative := []float64{0.8, 0.4, 0.3, 0.2, 0.1}
	result := make([]ranking.TopicCalibrationSample, 0, 10)
	for index := range includePositive {
		result = append(result, ranking.TopicCalibrationSample{
			IncludeCosine: includePositive[index], ExcludeCosine: excludeNegative[index],
			Membership: domain.TopicMembershipInclude,
		})
		result = append(result, ranking.TopicCalibrationSample{
			IncludeCosine: includeNegative[index], ExcludeCosine: excludePositive[index],
			Membership: domain.TopicMembershipExclude,
		})
	}
	return result
}

func TestPipelineFusesScoresAndDiversifiesCandidates(t *testing.T) {
	t.Parallel()

	dense := ranking.StaticRetriever{
		RetrieverName: "dense",
		Candidates: []ranking.Candidate{
			{ItemID: "a", SourceID: "one", Embedding: []float64{1, 0}},
			{ItemID: "b", SourceID: "one", Embedding: []float64{0.99, 0.01}, Signals: map[string]float64{"dense": 0.8}},
		},
	}
	sparse := ranking.StaticRetriever{
		RetrieverName: "sparse",
		Candidates: []ranking.Candidate{
			{ItemID: "b", SourceID: "one", Embedding: []float64{0.99, 0.01}, Signals: map[string]float64{"sparse": 0.7}},
			{ItemID: "c", SourceID: "two", Embedding: []float64{0, 1}},
		},
	}
	pipeline := ranking.Pipeline{
		Retrievers:  []ranking.Retriever{dense, sparse},
		Fusion:      ranking.ReciprocalRankFusion{K: 60},
		Scorers:     []ranking.Scorer{ranking.SourceCap{Limit: 1}},
		Reranker:    ranking.IdentityReranker{},
		Diversifier: ranking.MMR{Lambda: 0.75},
	}

	result, err := pipeline.Recommend(context.Background(), ranking.Request{CandidateLimit: 10, ResultLimit: 2})

	require.NoError(t, err)
	require.Len(t, result, 2)
	require.Equal(t, "b", result[0].ItemID)
	require.Equal(t, "c", result[1].ItemID)
	require.Contains(t, result[0].Signals, "rrf.dense")
	require.Contains(t, result[0].Signals, "rrf.sparse")
	require.Equal(t, 0.8, result[0].Signals["dense"])
	require.Equal(t, 0.7, result[0].Signals["sparse"])
}

func TestRecencyBoostIsBoundedAndDoesNotPenalizeOlderItems(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.September, 27, 12, 0, 0, 0, time.UTC)
	scorer := ranking.RecencyBoost{Now: func() time.Time { return now }, HalfLife: 7 * 24 * time.Hour, MaxBoost: 0.10}
	candidates := []ranking.Candidate{
		{ItemID: "new", Score: 1, PublishedAt: now},
		{ItemID: "week-old", Score: 1, PublishedAt: now.Add(-7 * 24 * time.Hour)},
		{ItemID: "unknown", Score: 1},
	}

	result, err := scorer.Score(context.Background(), ranking.Profile{}, candidates)

	require.NoError(t, err)
	require.InDelta(t, 1.10, result[0].Score, 0.0001)
	require.InDelta(t, 1.05, result[1].Score, 0.0001)
	require.InDelta(t, 1.00, result[2].Score, 0.0001)
}

func TestPipelineRejectsIncompleteConfiguration(t *testing.T) {
	t.Parallel()

	_, err := (ranking.Pipeline{}).Recommend(context.Background(), ranking.Request{ResultLimit: 10})

	require.ErrorContains(t, err, "fusion")
}

func TestSourceCapBackfillsWhenAlternativesAreInsufficient(t *testing.T) {
	t.Parallel()

	candidates := []ranking.Candidate{
		{ItemID: "a", SourceID: "one", Score: 4},
		{ItemID: "b", SourceID: "one", Score: 3},
		{ItemID: "c", SourceID: "one", Score: 2},
		{ItemID: "d", SourceID: "two", Score: 1},
	}

	result, err := (ranking.SourceCap{Limit: 1}).Score(t.Context(), ranking.Profile{}, candidates)

	require.NoError(t, err)
	require.Equal(t, []string{"a", "d", "b", "c"}, candidateIDs(result))
}

func TestDigestDiversifierAppliesSourceLimitDuringMMRSelection(t *testing.T) {
	t.Parallel()

	for name, testCase := range map[string]struct {
		candidates []ranking.Candidate
		want       []string
	}{
		"prefer another source": {
			candidates: []ranking.Candidate{
				{ItemID: "a-1", SourceID: "a", Score: 4},
				{ItemID: "a-2", SourceID: "a", Score: 3},
				{ItemID: "a-3", SourceID: "a", Score: 2},
				{ItemID: "b-1", SourceID: "b", Score: 1},
			},
			want: []string{"a-1", "a-2", "b-1"},
		},
		"backfill when no alternative remains": {
			candidates: []ranking.Candidate{
				{ItemID: "a-1", SourceID: "a", Score: 3},
				{ItemID: "a-2", SourceID: "a", Score: 2},
				{ItemID: "a-3", SourceID: "a", Score: 1},
			},
			want: []string{"a-1", "a-2", "a-3"},
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			result, err := (ranking.DigestDiversifier{
				MMR: ranking.MMR{Lambda: 1}, SourceLimit: 2,
			}).Diversify(testCase.candidates, 3)

			require.NoError(t, err)
			require.Equal(t, testCase.want, candidateIDs(result))
		})
	}
}

func TestPersonalKNNUsesThreeStrongestTimeDecayedSignalsAndHonorsDirectFeedback(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.September, 27, 12, 0, 0, 0, time.UTC)
	candidateID := uuid.MustParse("10000000-0000-0000-0000-000000000001")
	liked := uuid.MustParse("10000000-0000-0000-0000-000000000002")
	disliked := uuid.MustParse("10000000-0000-0000-0000-000000000003")
	scorer := ranking.PersonalKNN{
		Now: func() time.Time { return now },
		Positive: []ranking.FeedbackNeighborhood{
			feedbackNeighborhood(feedbackValue(liked, "like", now), candidateID, 0.9),
			feedbackNeighborhood(feedbackValue(uuid.New(), "like", now.Add(-180*24*time.Hour)), candidateID, 1),
			feedbackNeighborhood(feedbackValue(uuid.New(), "like", now), candidateID, 0.6),
			feedbackNeighborhood(feedbackValue(uuid.New(), "like", now), candidateID, 0.1),
		},
		Negative: []ranking.FeedbackNeighborhood{
			feedbackNeighborhood(feedbackValue(disliked, "dislike", now), candidateID, 0.4),
		},
	}
	candidates := []ranking.Candidate{
		{ItemID: candidateID.String(), Score: 1},
		{ItemID: liked.String(), Score: 0.5},
		{ItemID: disliked.String(), Score: 0.9},
	}

	result, err := scorer.Score(t.Context(), ranking.Profile{}, candidates)

	require.NoError(t, err)
	require.Equal(t, []string{candidateID.String(), liked.String()}, candidateIDs(result))
	require.InDelta(t, 2.0/3.0, result[0].Signals["personal.positive"], 0.0001)
	require.InDelta(t, 0.4, result[0].Signals["personal.negative"], 0.0001)
	require.Contains(t, result[0].Signals, "personal.multiplier")
	require.Equal(t, 1.0, result[1].Signals["personal.directLike"])
}

func TestPersonalKNNLoadsEvidenceForOnlyTheAcceptedCandidatePool(t *testing.T) {
	t.Parallel()

	workspaceID, firstID, secondID := uuid.New(), uuid.New(), uuid.New()
	now := time.Date(2026, time.September, 29, 12, 0, 0, 0, time.UTC)
	index := &personalIndexStub{result: ranking.PersonalRetrieval{
		Positive: []ranking.FeedbackNeighborhood{
			feedbackNeighborhood(feedbackValue(uuid.New(), "like", now), firstID, 0.8),
		},
	}}
	dislike := domain.Feedback{Sentiment: "dislike", UpdatedAt: now}
	scorer := ranking.PersonalKNN{
		Index: index, Filter: ranking.IndexFilter{WorkspaceID: workspaceID}, NeighborLimit: 12,
		Now: func() time.Time { return now },
	}

	result, err := scorer.Score(t.Context(), ranking.Profile{}, []ranking.Candidate{
		{ItemID: firstID.String(), Score: 1},
		{ItemID: secondID.String(), Score: 0.9, Feedback: &dislike},
	})

	require.NoError(t, err)
	require.Equal(t, []uuid.UUID{firstID, secondID}, index.request.CandidateIDs)
	require.Equal(t, 12, index.request.NeighborLimit)
	require.Equal(t, []string{firstID.String()}, candidateIDs(result), "direct Dislike suppresses without waiting for kNN activation")
	require.Greater(t, result[0].Signals["personal.positive"], 0.0)
}

func TestPersonalKNNLimitsDirectFeedbackToBenchmarkTrainingItems(t *testing.T) {
	t.Parallel()

	trainingID, heldOutID := uuid.New(), uuid.New()
	now := time.Date(2026, time.September, 29, 12, 0, 0, 0, time.UTC)
	dislike := domain.Feedback{Sentiment: "dislike", UpdatedAt: now}
	index := &personalIndexStub{}
	scorer := ranking.PersonalKNN{
		Index:       index,
		Filter:      ranking.IndexFilter{WorkspaceID: uuid.New()},
		FeedbackIDs: []uuid.UUID{trainingID},
		Now:         func() time.Time { return now },
	}

	_, err := scorer.Score(t.Context(), ranking.Profile{}, []ranking.Candidate{{
		ItemID: heldOutID.String(), Score: 1, Feedback: &dislike,
	}})

	require.NoError(t, err)
	require.Empty(t, index.request.Direct, "held-out feedback must not leak into benchmark personalization")
}

func TestPersonalKNNHidesStrongIndirectNegativeUnlessItemIsLiked(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.September, 27, 12, 0, 0, 0, time.UTC)
	hiddenID := uuid.MustParse("20000000-0000-0000-0000-000000000001")
	likedID := uuid.MustParse("20000000-0000-0000-0000-000000000002")
	scorer := ranking.PersonalKNN{
		Now: func() time.Time { return now },
		Positive: []ranking.FeedbackNeighborhood{
			feedbackNeighborhood(feedbackValue(likedID, "like", now), likedID, 0.2),
		},
		Negative: []ranking.FeedbackNeighborhood{
			feedbackNeighborhood(feedbackValue(uuid.New(), "dislike", now), hiddenID, 0.9),
			feedbackNeighborhood(feedbackValue(uuid.New(), "dislike", now), likedID, 0.95),
		},
	}
	candidates := []ranking.Candidate{
		{ItemID: hiddenID.String(), Score: 1},
		{ItemID: likedID.String(), Score: 0.4},
	}

	result, err := scorer.Score(t.Context(), ranking.Profile{}, candidates)

	require.NoError(t, err)
	require.Equal(t, []string{likedID.String()}, candidateIDs(result))
}

func TestDigestDiversifierReservesLastPositionForUnknownPreference(t *testing.T) {
	t.Parallel()

	candidates := []ranking.Candidate{
		{ItemID: "known-1", Score: 1, Embedding: []float64{1, 0}, Signals: map[string]float64{"personal.positive": 0.9}},
		{ItemID: "known-2", Score: 0.9, Embedding: []float64{0, 1}, Signals: map[string]float64{"personal.negative": 0.2}},
		{ItemID: "known-3", Score: 0.8, Embedding: []float64{-1, 0}, Signals: map[string]float64{"personal.positive": 0.4}},
		{ItemID: "known-4", Score: 0.7, Embedding: []float64{0, -1}, Signals: map[string]float64{"personal.positive": 0.3}},
		{ItemID: "unknown", Score: 0.1, Embedding: []float64{0.5, 0.5}},
	}

	result, err := (ranking.DigestDiversifier{
		MMR: ranking.MMR{Lambda: 0.75}, SourceLimit: 2, ExplorationEnabled: true,
	}).Diversify(candidates, 5)

	require.NoError(t, err)
	require.Len(t, result, 5)
	require.Equal(t, "unknown", result[4].ItemID)
	require.Equal(t, 1.0, result[4].Signals["exploration"])
}

func TestDigestExplorationReservesItsSourceBeforeMMRSelection(t *testing.T) {
	t.Parallel()

	known := map[string]float64{"personal.positive": 0.5}
	candidates := []ranking.Candidate{
		{ItemID: "explore-a", SourceID: "a", Score: 7},
		{ItemID: "a-1", SourceID: "a", Score: 6, Signals: known},
		{ItemID: "a-2", SourceID: "a", Score: 5, Signals: known},
		{ItemID: "a-3", SourceID: "a", Score: 4, Signals: known},
		{ItemID: "b-1", SourceID: "b", Score: 3, Signals: known},
		{ItemID: "b-2", SourceID: "b", Score: 2, Signals: known},
		{ItemID: "c-1", SourceID: "c", Score: 1, Signals: known},
	}

	result, err := (ranking.DigestDiversifier{
		MMR: ranking.MMR{Lambda: 1}, SourceLimit: 2, ExplorationEnabled: true,
	}).Diversify(candidates, 5)

	require.NoError(t, err)
	require.Equal(t, []string{"a-1", "b-1", "b-2", "c-1", "explore-a"}, candidateIDs(result))
	require.Equal(t, 1.0, result[4].Signals["exploration"])
}

func feedbackValue(referenceID uuid.UUID, sentiment string, updatedAt time.Time) domain.ItemFeedback {
	return domain.ItemFeedback{
		ItemID: referenceID, Feedback: domain.Feedback{Sentiment: sentiment, UpdatedAt: updatedAt},
	}
}

func feedbackNeighborhood(feedback domain.ItemFeedback, candidateID uuid.UUID, score float64) ranking.FeedbackNeighborhood {
	return ranking.FeedbackNeighborhood{
		Feedback: feedback,
		Hits:     []ranking.IndexHit{{ItemID: candidateID, Score: score}},
	}
}

func candidateIDs(candidates []ranking.Candidate) []string {
	result := make([]string, len(candidates))
	for index, candidate := range candidates {
		result[index] = candidate.ItemID
	}
	return result
}
