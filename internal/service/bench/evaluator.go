package bench

//go:generate go tool mockgen -source=evaluator.go -destination=mocks/evaluator.gen.go -package=mocks

import (
	"context"
	"errors"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/ethernity/obsidian-topic-stream/internal/domain"
	"github.com/ethernity/obsidian-topic-stream/internal/service/ranking"
	"github.com/google/uuid"
)

const ReportVersion = "topic-stream-bench-v1"

// Repository reads current feedback and Items for offline evaluation.
type Repository interface {
	// ListCurrentFeedback returns current personal feedback for the Workspace.
	ListCurrentFeedback(ctx context.Context, workspaceID uuid.UUID) ([]domain.ItemFeedback, error)
	// GetItem returns the Workspace Item and whether it exists.
	GetItem(ctx context.Context, workspaceID uuid.UUID, itemID uuid.UUID) (domain.Item, bool, error)
}

// Index combines text retrieval and personal-feedback evidence for evaluation.
type Index interface {
	ranking.Index
	ranking.PersonalIndex
}

// Evaluator compares retrieval pipelines against a chronological split of current personal feedback.
type Evaluator struct {
	workspaceID uuid.UUID
	repository  Repository
	index       Index
	rrfK        int
}

// Options selects an optional feedback window and the percentage used as training evidence.
type Options struct {
	Since        *time.Time
	TrainPercent int
}

// Metrics reports precision, recall, reciprocal rank, and discounted gain for held-out feedback.
type Metrics struct {
	PrecisionAt10 float64 `json:"precisionAt10"`
	RecallAt50    float64 `json:"recallAt50"`
	MRR           float64 `json:"mrr"`
	NDCGAt10      float64 `json:"ndcgAt10"`
}

// PipelineResult pairs one retrieval pipeline name with its held-out quality metrics.
type PipelineResult struct {
	Name    string  `json:"name"`
	Metrics Metrics `json:"metrics"`
}

// Report records the evaluation version, feedback split, and comparable pipeline metrics.
type Report struct {
	Version       string           `json:"version"`
	Since         *time.Time       `json:"since,omitempty"`
	TrainPercent  int              `json:"trainPercent"`
	TrainingCount int              `json:"trainingCount"`
	TestCount     int              `json:"testCount"`
	Pipelines     []PipelineResult `json:"pipelines"`
}

// New validates read-only evaluation dependencies and the reciprocal-rank fusion constant.
func New(workspaceID uuid.UUID, repository Repository, index Index, rrfK int) (*Evaluator, error) {
	if workspaceID == uuid.Nil || repository == nil || index == nil || rrfK < 1 {
		return nil, errors.New("benchmark dependencies are required")
	}
	return &Evaluator{workspaceID: workspaceID, repository: repository, index: index, rrfK: rrfK}, nil
}

// Evaluate compares dense, lexical, and fused retrieval against held-out chronological personal feedback.
func (e *Evaluator) Evaluate(ctx context.Context, options Options) (Report, error) {
	feedback, err := e.repository.ListCurrentFeedback(ctx, e.workspaceID)
	if err != nil {
		return Report{}, fmt.Errorf("list benchmark feedback: %w", err)
	}
	feedback = benchmarkWindow(feedback, options.Since)
	trainPercent := options.TrainPercent
	if trainPercent == 0 {
		trainPercent = 80
	}
	train, test, err := splitFeedback(feedback, trainPercent)
	if err != nil {
		return Report{}, err
	}
	query, err := e.trainingQuery(ctx, train)
	if err != nil {
		return Report{}, err
	}
	results := make([]PipelineResult, 0, 4)
	for _, name := range []string{"bm25", "dense", "hybrid-rrf", "hybrid-personalized"} {
		candidates, err := e.runPipeline(ctx, pipelineRequest{name: name, query: query, train: train})
		if err != nil {
			return Report{}, fmt.Errorf("evaluate %s: %w", name, err)
		}
		results = append(results, PipelineResult{Name: name, Metrics: calculateMetrics(candidates, test)})
	}
	return Report{
		Version: ReportVersion, Since: options.Since, TrainPercent: trainPercent,
		TrainingCount: len(train), TestCount: len(test), Pipelines: results,
	}, nil
}

func benchmarkWindow(feedback []domain.ItemFeedback, since *time.Time) []domain.ItemFeedback {
	result := make([]domain.ItemFeedback, 0, len(feedback))
	for _, value := range feedback {
		if since == nil || !value.Feedback.UpdatedAt.Before(*since) {
			result = append(result, value)
		}
	}
	sort.Slice(result, func(left, right int) bool {
		if result[left].Feedback.UpdatedAt.Equal(result[right].Feedback.UpdatedAt) {
			return result[left].ItemID.String() < result[right].ItemID.String()
		}
		return result[left].Feedback.UpdatedAt.Before(result[right].Feedback.UpdatedAt)
	})
	return result
}

// splitFeedback divides already chronological feedback without shuffling, so evaluation never
// trains on feedback newer than its test set.
func splitFeedback(feedback []domain.ItemFeedback, trainPercent int) ([]domain.ItemFeedback, []domain.ItemFeedback, error) {
	if trainPercent < 1 || trainPercent > 99 {
		return nil, nil, errors.New("train percent must be between 1 and 99")
	}
	if len(feedback) < 2 {
		return nil, nil, errors.New("benchmark requires at least two feedback items")
	}
	split := max(1, min(len(feedback)-1, len(feedback)*trainPercent/100))
	return feedback[:split], feedback[split:], nil
}

func (e *Evaluator) trainingQuery(ctx context.Context, feedback []domain.ItemFeedback) (string, error) {
	parts := make([]string, 0, len(feedback))
	for _, value := range feedback {
		if value.Feedback.Sentiment != "like" {
			continue
		}
		item, found, err := e.repository.GetItem(ctx, e.workspaceID, value.ItemID)
		if err != nil {
			return "", fmt.Errorf("get benchmark training item: %w", err)
		}
		if found {
			parts = append(parts, item.RetrievalText())
		}
	}
	query := strings.TrimSpace(strings.Join(parts, "\n\n"))
	if query == "" {
		return "", errors.New("benchmark training split has no liked item text")
	}
	return query, nil
}

type pipelineRequest struct {
	name  string
	query string
	train []domain.ItemFeedback
}

func (e *Evaluator) runPipeline(ctx context.Context, request pipelineRequest) ([]ranking.Candidate, error) {
	filter := ranking.IndexFilter{WorkspaceID: e.workspaceID}
	retrievers := benchmarkRetrievers(e.index, filter, request.name)
	scorers, err := e.benchmarkScorers(ctx, filter, request)
	if err != nil {
		return nil, err
	}
	return (ranking.Pipeline{
		Retrievers: retrievers, Fusion: ranking.ReciprocalRankFusion{K: e.rrfK}, Scorers: scorers,
		Reranker: ranking.IdentityReranker{}, Diversifier: ranking.TopN{},
	}).Recommend(ctx, ranking.Request{
		Profile: ranking.Profile{Prefer: request.query}, CandidateLimit: 100, ResultLimit: 100,
	})
}

func benchmarkRetrievers(index ranking.Index, filter ranking.IndexFilter, name string) []ranking.Retriever {
	switch name {
	case "bm25":
		return []ranking.Retriever{ranking.NewSparseRetriever(index, filter)}
	case "dense":
		return []ranking.Retriever{ranking.NewDenseRetriever(index, filter)}
	default:
		return []ranking.Retriever{
			ranking.NewDenseRetriever(index, filter), ranking.NewSparseRetriever(index, filter),
		}
	}
}

func (e *Evaluator) benchmarkScorers(
	_ context.Context,
	filter ranking.IndexFilter,
	request pipelineRequest,
) ([]ranking.Scorer, error) {
	if request.name != "hybrid-personalized" {
		return nil, nil
	}
	feedbackIDs := make([]uuid.UUID, len(request.train))
	for index, value := range request.train {
		feedbackIDs[index] = value.ItemID
	}
	return []ranking.Scorer{&ranking.PersonalKNN{
		Index: e.index, Filter: filter, NeighborLimit: 12, FeedbackIDs: feedbackIDs,
		Now: time.Now, PreserveCandidates: true,
	}}, nil
}

func calculateMetrics(candidates []ranking.Candidate, feedback []domain.ItemFeedback) Metrics {
	relevant := make(map[string]struct{})
	for _, value := range feedback {
		if value.Feedback.Sentiment == "like" {
			relevant[value.ItemID.String()] = struct{}{}
		}
	}
	return Metrics{
		PrecisionAt10: precisionAt(candidates, relevant, 10),
		RecallAt50:    recallAt(candidates, relevant, 50),
		MRR:           reciprocalRank(candidates, relevant),
		NDCGAt10:      ndcgAt(candidates, relevant, 10),
	}
}

func precisionAt(candidates []ranking.Candidate, relevant map[string]struct{}, limit int) float64 {
	limit = min(limit, len(candidates))
	if limit == 0 {
		return 0
	}
	return float64(relevantCount(candidates[:limit], relevant)) / float64(limit)
}

func recallAt(candidates []ranking.Candidate, relevant map[string]struct{}, limit int) float64 {
	if len(relevant) == 0 {
		return 0
	}
	limit = min(limit, len(candidates))
	return float64(relevantCount(candidates[:limit], relevant)) / float64(len(relevant))
}

func reciprocalRank(candidates []ranking.Candidate, relevant map[string]struct{}) float64 {
	for index, candidate := range candidates {
		if _, found := relevant[candidate.ItemID]; found {
			return 1 / float64(index+1)
		}
	}
	return 0
}

func ndcgAt(candidates []ranking.Candidate, relevant map[string]struct{}, limit int) float64 {
	limit = min(limit, len(candidates))
	var dcg float64
	for index, candidate := range candidates[:limit] {
		if _, found := relevant[candidate.ItemID]; found {
			dcg += 1 / math.Log2(float64(index+2))
		}
	}
	ideal := min(limit, len(relevant))
	var idcg float64
	for index := range ideal {
		idcg += 1 / math.Log2(float64(index+2))
	}
	if idcg == 0 {
		return 0
	}
	return dcg / idcg
}

func relevantCount(candidates []ranking.Candidate, relevant map[string]struct{}) int {
	count := 0
	for _, candidate := range candidates {
		if _, found := relevant[candidate.ItemID]; found {
			count++
		}
	}
	return count
}
