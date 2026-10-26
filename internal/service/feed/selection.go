package feed

import (
	"bytes"
	"maps"
	"slices"

	"github.com/ethernity/obsidian-topic-stream/internal/domain"
	"github.com/google/uuid"
)

type candidateQueue struct {
	items    []domain.FeedCandidate
	position int
}

func (q *candidateQueue) next(seen map[uuid.UUID]bool) (domain.FeedCandidate, bool) {
	for q.position < len(q.items) {
		item := q.items[q.position]
		q.position++
		if !seen[item.ItemID] {
			return item, true
		}
	}
	return domain.FeedCandidate{}, false
}

type readySelection struct {
	candidates []domain.FeedCandidate
	matches    map[uuid.UUID][]domain.FeedCandidate
	start      int
}

func selectReady(rows []domain.FeedCandidate, query domain.FeedReadQuery, settings domain.RetrievalSettings, startChoice func(int) int) readySelection {
	grouped := make(map[uuid.UUID][]domain.FeedCandidate)
	for _, row := range rows {
		if query.TopicID == nil || row.TopicID == *query.TopicID {
			grouped[row.TopicID] = append(grouped[row.TopicID], row)
		}
	}
	topics := slices.Collect(maps.Keys(grouped))
	slices.SortFunc(topics, func(a, b uuid.UUID) int { return bytes.Compare(a[:], b[:]) })
	selection := readySelection{candidates: []domain.FeedCandidate{}, matches: make(map[uuid.UUID][]domain.FeedCandidate)}
	if len(topics) == 0 {
		return selection
	}
	selection.start = startChoice(len(topics)) % len(topics)
	if selection.start < 0 {
		selection.start += len(topics)
	}
	queues := make([]candidateQueue, 0, len(topics))
	for _, topicID := range topics {
		items := grouped[topicID]
		slices.SortFunc(items, func(a, b domain.FeedCandidate) int { return a.Position - b.Position })
		queues = append(queues, candidateQueue{items: items})
	}
	selection.candidates = roundRobin(queues, settings.CandidateLimit, selection.start)
	selection.freezeMatches(rows, query.TopicID, settings.RRFK)
	return selection
}

func roundRobin(queues []candidateQueue, limit, start int) []domain.FeedCandidate {
	result := make([]domain.FeedCandidate, 0, limit)
	seen := make(map[uuid.UUID]bool)
	for len(result) < limit {
		added := false
		for offset := range len(queues) {
			item, found := queues[(start+offset)%len(queues)].next(seen)
			if found {
				seen[item.ItemID] = true
				result = append(result, item)
				added = true
			}
			if len(result) == limit {
				break
			}
		}
		if !added {
			break
		}
	}
	return result
}

func (s *readySelection) freezeMatches(rows []domain.FeedCandidate, topicID *uuid.UUID, rrfK int) {
	selected := make(map[uuid.UUID]bool, len(s.candidates))
	for _, candidate := range s.candidates {
		selected[candidate.ItemID] = true
	}
	for _, row := range rows {
		if selected[row.ItemID] {
			s.matches[row.ItemID] = append(s.matches[row.ItemID], row)
		}
	}
	for position, candidate := range s.candidates {
		candidate.Signals = maps.Clone(candidate.Signals)
		if candidate.Signals == nil {
			candidate.Signals = make(map[string]float64)
		}
		score := 0.0
		for _, match := range s.matches[candidate.ItemID] {
			score += 1 / float64(rrfK+match.Position)
			candidate.Signals["topic.rank."+match.TopicID.String()] = float64(match.Position)
			candidate.Signals["topic.score."+match.TopicID.String()] = match.Score
		}
		if topicID == nil {
			candidate.Score = score
			candidate.Signals["selection.crossTopicRRF"] = score
		}
		candidate.Signals["selection.start"] = float64(s.start)
		s.candidates[position] = candidate
	}
}

func (s readySelection) publications(ranked []domain.FeedCandidate, settingsVersion int, modelID uuid.UUID) []domain.FeedPublication {
	result := make([]domain.FeedPublication, 0, len(ranked))
	for _, candidate := range ranked {
		publication := domain.FeedPublication{ItemID: candidate.ItemID, ModelID: modelID, Score: candidate.Score, Signals: maps.Clone(candidate.Signals), Pipeline: "shared-feed-v1/personal-knn-v1", TopicInputs: make(map[uuid.UUID]domain.FeedCandidateInputs)}
		if publication.Signals == nil {
			publication.Signals = make(map[string]float64)
		}
		publication.Signals["settings.version"] = float64(settingsVersion)
		for _, match := range s.matches[candidate.ItemID] {
			publication.MatchedTopicIDs = append(publication.MatchedTopicIDs, match.TopicID)
			publication.TopicInputs[match.TopicID] = match.Inputs
		}
		result = append(result, publication)
	}
	return result
}
