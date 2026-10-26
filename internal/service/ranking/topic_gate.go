package ranking

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/ethernity/obsidian-topic-stream/internal/domain"
)

// TopicThresholds defines Include and Exclude cosine boundaries for semantic membership.
type TopicThresholds struct {
	// Include is the minimum accepted cosine similarity when Include criteria are present.
	Include float64 `json:"include"`
	// Exclude rejects cosine similarities at or above this boundary when Exclude criteria are present.
	Exclude float64 `json:"exclude"`
}

// TopicThresholdDefaults supplies model-versioned bootstrap gates and calibration shrinkage.
type TopicThresholdDefaults struct {
	Model    string
	Version  string
	Ordinary TopicThresholds
	Strict   TopicThresholds
	// Shrinkage weights bootstrap thresholds against observed calibration examples.
	Shrinkage float64
}

// TopicCalibrationSample pairs criterion similarities with an explicit membership judgment.
type TopicCalibrationSample struct {
	IncludeCosine float64
	ExcludeCosine float64
	Membership    domain.TopicMembership
}

// DefaultTopicThresholdDefaults returns versioned bootstrap thresholds for the supplied model.
func DefaultTopicThresholdDefaults(model string) TopicThresholdDefaults {
	return TopicThresholdDefaults{
		Model: strings.TrimSpace(model), Version: "topic-gate-bootstrap-v1", Shrinkage: 10,
		Ordinary: TopicThresholds{Include: 0.35, Exclude: 0.75},
		Strict:   TopicThresholds{Include: 0.5, Exclude: 0.6},
	}
}

// Calibrate learns include and exclude cosine thresholds only after observing at least five
// explicit examples on both sides, then shrinks them toward the model-specific defaults.
func (d TopicThresholdDefaults) Calibrate(
	mode domain.TopicMode,
	samples []TopicCalibrationSample,
) (TopicThresholds, bool, error) {
	defaults, includeBeta, excludeBeta, err := d.defaultsFor(mode)
	if err != nil {
		return TopicThresholds{}, false, err
	}
	includePositive, includeNegative := calibrationScores(samples, false)
	if len(includePositive) < 5 || len(includeNegative) < 5 {
		return defaults, false, nil
	}
	excludePositive, excludeNegative := calibrationScores(samples, true)
	weight := float64(len(includePositive) + len(includeNegative))
	result := TopicThresholds{
		Include: shrinkThreshold(bestThreshold(includePositive, includeNegative, includeBeta), defaults.Include, weight, d.Shrinkage),
		Exclude: shrinkThreshold(bestThreshold(excludePositive, excludeNegative, excludeBeta), defaults.Exclude, weight, d.Shrinkage),
	}
	return result, true, nil
}

func (d TopicThresholdDefaults) defaultsFor(mode domain.TopicMode) (TopicThresholds, float64, float64, error) {
	if strings.TrimSpace(d.Model) == "" || strings.TrimSpace(d.Version) == "" || d.Shrinkage < 0 {
		return TopicThresholds{}, 0, 0, errors.New("topic threshold defaults require model, version, and non-negative shrinkage")
	}
	var thresholds TopicThresholds
	var includeBeta, excludeBeta float64
	switch mode {
	case domain.TopicModeOrdinary:
		thresholds, includeBeta, excludeBeta = d.Ordinary, 2, 0.5
	case domain.TopicModeStrict:
		thresholds, includeBeta, excludeBeta = d.Strict, 0.5, 2
	default:
		return TopicThresholds{}, 0, 0, errors.New("topic mode must be ordinary or strict")
	}
	if err := validateThresholds(thresholds); err != nil {
		return TopicThresholds{}, 0, 0, err
	}
	return thresholds, includeBeta, excludeBeta, nil
}

func calibrationScores(samples []TopicCalibrationSample, exclusion bool) ([]float64, []float64) {
	positive := make([]float64, 0, len(samples)/2)
	negative := make([]float64, 0, len(samples)/2)
	for _, sample := range samples {
		score := sample.IncludeCosine
		positiveMembership := domain.TopicMembershipInclude
		if exclusion {
			score = sample.ExcludeCosine
			positiveMembership = domain.TopicMembershipExclude
		}
		switch sample.Membership {
		case positiveMembership:
			positive = append(positive, score)
		case domain.TopicMembershipInclude, domain.TopicMembershipExclude:
			negative = append(negative, score)
		}
	}
	return positive, negative
}

func bestThreshold(positive, negative []float64, beta float64) float64 {
	candidates := append(append([]float64(nil), positive...), negative...)
	slices.Sort(candidates)
	candidates = slices.Compact(candidates)
	best, bestScore := candidates[0], -1.0
	for _, threshold := range candidates {
		tp, fp := countAtLeast(positive, threshold), countAtLeast(negative, threshold)
		fn := len(positive) - tp
		betaSquared := beta * beta
		score := (1 + betaSquared) * float64(tp) /
			((1+betaSquared)*float64(tp) + betaSquared*float64(fn) + float64(fp))
		if score > bestScore || score == bestScore && preferThreshold(threshold, best, beta) {
			best, bestScore = threshold, score
		}
	}
	return best
}

func countAtLeast(values []float64, threshold float64) int {
	count := 0
	for _, value := range values {
		if value >= threshold {
			count++
		}
	}
	return count
}

func preferThreshold(candidate, current, beta float64) bool {
	if beta >= 1 {
		return candidate < current
	}
	return candidate > current
}

func shrinkThreshold(empirical, defaults, observations, shrinkage float64) float64 {
	return (empirical*observations + defaults*shrinkage) / (observations + shrinkage)
}

// TopicSemanticGate applies explicit Topic Membership Corrections before semantic thresholds, so a
// manual include or exclude always wins over embedding similarity.
type TopicSemanticGate struct {
	IncludeEmbedding []float64
	ExcludeEmbedding []float64
	Thresholds       TopicThresholds
	Corrections      map[string]domain.TopicMembership
}

// Apply filters retrieval lists using Topic membership rules before fusion.
func (g TopicSemanticGate) Apply(_ context.Context, lists []Retrieval) ([]Retrieval, error) {
	if err := validateThresholds(g.Thresholds); err != nil {
		return nil, err
	}
	embeddings := candidateEmbeddings(lists)
	decisions, err := g.decisions(lists, embeddings)
	if err != nil {
		return nil, err
	}
	return applyGateDecisions(lists, decisions), nil
}

func (g TopicSemanticGate) decisions(
	lists []Retrieval,
	embeddings map[string][]float64,
) (map[string]gateDecision, error) {
	decisions := make(map[string]gateDecision, len(embeddings))
	for _, list := range lists {
		for _, candidate := range list.Candidates {
			if _, decided := decisions[candidate.ItemID]; decided {
				continue
			}
			decision, err := g.decide(candidate.ItemID, embeddings[candidate.ItemID])
			if err != nil {
				return nil, err
			}
			decisions[candidate.ItemID] = decision
		}
	}
	return decisions, nil
}

func applyGateDecisions(lists []Retrieval, decisions map[string]gateDecision) []Retrieval {
	result := make([]Retrieval, len(lists))
	for listIndex, list := range lists {
		result[listIndex].Name = list.Name
		result[listIndex].Candidates = make([]Candidate, 0, len(list.Candidates))
		for _, candidate := range list.Candidates {
			decision := decisions[candidate.ItemID]
			if !decision.accepted {
				continue
			}
			candidate = cloneCandidate(candidate)
			for name, value := range decision.signals {
				ensureSignals(&candidate)[name] = value
			}
			result[listIndex].Candidates = append(result[listIndex].Candidates, candidate)
		}
	}
	return result
}

type gateDecision struct {
	accepted bool
	signals  map[string]float64
}

func (g TopicSemanticGate) decide(itemID string, embedding []float64) (gateDecision, error) {
	switch g.Corrections[itemID] {
	case domain.TopicMembershipInclude:
		return gateDecision{accepted: true, signals: map[string]float64{
			"topic.accepted": 1, "topic.manualInclude": 1,
		}}, nil
	case domain.TopicMembershipExclude:
		return gateDecision{}, nil
	}
	if (len(g.IncludeEmbedding) > 0 || len(g.ExcludeEmbedding) > 0) && len(embedding) == 0 {
		return gateDecision{}, fmt.Errorf("candidate %s has no dense embedding", itemID)
	}
	signals := map[string]float64{}
	accepted := true
	if len(g.IncludeEmbedding) > 0 {
		include := cosine(embedding, g.IncludeEmbedding)
		signals["topic.includeCosine"] = include
		accepted = include >= g.Thresholds.Include
	}
	if len(g.ExcludeEmbedding) > 0 {
		exclude := cosine(embedding, g.ExcludeEmbedding)
		signals["topic.excludeCosine"] = exclude
		accepted = accepted && exclude < g.Thresholds.Exclude
	}
	if accepted {
		signals["topic.accepted"] = 1
	}
	return gateDecision{accepted: accepted, signals: signals}, nil
}

func candidateEmbeddings(lists []Retrieval) map[string][]float64 {
	result := make(map[string][]float64)
	for _, list := range lists {
		for _, candidate := range list.Candidates {
			if len(candidate.Embedding) > 0 && len(result[candidate.ItemID]) == 0 {
				result[candidate.ItemID] = candidate.Embedding
			}
		}
	}
	return result
}

func validateThresholds(thresholds TopicThresholds) error {
	if thresholds.Include < -1 || thresholds.Include > 1 || thresholds.Exclude < -1 || thresholds.Exclude > 1 {
		return errors.New("topic cosine thresholds must be between -1 and 1")
	}
	return nil
}
