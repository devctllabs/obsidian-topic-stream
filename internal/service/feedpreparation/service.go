package feedpreparation

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/ethernity/obsidian-topic-stream/internal/domain"
	"github.com/ethernity/obsidian-topic-stream/internal/service/jobs"
	"github.com/google/uuid"
)

const TopicsPerExecution = 8

var Definition = jobs.JobDefinition{Kind: domain.JobKindPrepareFeed, ArgumentsVersion: 1, Queue: domain.JobQueueFeedPreparation, Retry: jobs.RetryPolicy{MaxAttempts: 5, BaseDelay: 5 * time.Second, MaxDelay: 5 * time.Minute}}

// Repository loads preparation inputs and replaces candidates with lease and version fencing.
type Repository interface {
	// LoadFeedPreparationWork loads a bounded batch of Topic preparation inputs for the Workspace.
	LoadFeedPreparationWork(ctx context.Context, query domain.FeedPreparationQuery) ([]domain.FeedPreparationWork, error)
	// ReplaceFeedCandidates replaces candidates while lease and inputs remain current; stale work returns domain.ErrConflict.
	ReplaceFeedCandidates(ctx context.Context, replacement domain.FeedCandidateReplacement) error
	// GetTopic returns the Workspace Topic and whether it exists.
	GetTopic(ctx context.Context, workspaceID uuid.UUID, topicID uuid.UUID) (domain.Topic, bool, error)
	// GetActive returns the Workspace's serving retrieval generation and whether one exists.
	GetActive(ctx context.Context, workspaceID uuid.UUID) (domain.RetrievalIndex, bool, error)
}

// Preparer computes candidates from a captured Topic preparation snapshot.
type Preparer interface {
	// PrepareFeed retrieves and gates candidates using the captured Topic, settings, and generation.
	PrepareFeed(ctx context.Context, preparation domain.FeedTopicPreparation) (domain.FeedTopicPrepared, error)
}

// Settings provides settings needed by the consuming operation.
type Settings interface {
	// GetWorkspace returns the current Workspace Settings snapshot.
	GetWorkspace(ctx context.Context) (domain.WorkspaceSettings, error)
}

// Config supplies dependencies and operating settings for the feedpreparation service.
type Config struct {
	GateVersion string
	WorkspaceID uuid.UUID
	Repository  Repository
	Preparer    Preparer
	Settings    Settings
	Jobs        jobs.Queue
	Now         func() time.Time
}

// Service registers durable preparation work for bounded Topic candidates.
type Service struct{ config Config }

// New registers the Workspace's durable Feed Preparation handler.
func New(config Config) *Service {
	s := &Service{config: config}
	config.Jobs.RegisterDefinition(Definition, s.handle)
	return s
}
func (s *Service) handle(ctx context.Context, _ json.RawMessage) error {
	execution, ok := jobs.CurrentExecution(ctx)
	if !ok || execution.WorkspaceID != s.config.WorkspaceID || execution.LeaseOwner == "" {
		return jobs.Permanent(errors.New("feed preparation requires its Workspace claim"))
	}
	settings, err := s.config.Settings.GetWorkspace(ctx)
	if err != nil {
		return err
	}
	generation, found, err := s.config.Repository.GetActive(ctx, s.config.WorkspaceID)
	if err != nil {
		return err
	}
	if !found {
		return nil
	}
	work, err := s.config.Repository.LoadFeedPreparationWork(ctx, domain.FeedPreparationQuery{WorkspaceID: s.config.WorkspaceID, At: s.config.Now(), Limit: TopicsPerExecution, GateVersion: s.config.GateVersion})
	if err != nil {
		return err
	}
	for _, item := range work {
		if err := s.prepare(ctx, item, preparationBatch{execution: execution, generation: generation, settings: settings}); err != nil {
			return err
		}
	}
	return nil
}

type preparationBatch struct {
	execution  jobs.Execution
	generation domain.RetrievalIndex
	settings   domain.WorkspaceSettings
}

func (s *Service) prepare(ctx context.Context, work domain.FeedPreparationWork, batch preparationBatch) error {
	execution, generation, settings := batch.execution, batch.generation, batch.settings
	topic, found, err := s.config.Repository.GetTopic(ctx, s.config.WorkspaceID, work.TopicID)
	if err != nil {
		return err
	}
	if !found || !topic.Enabled {
		return nil
	}
	inputs := domain.FeedCandidateInputs{TopicRevision: topic.Revision, CorrectionEpoch: topic.CorrectionEpoch, CorrectionVersion: topic.CorrectionVersion, ModelID: generation.ModelID, CandidateLimit: settings.Retrieval.CandidateLimit, RRFK: settings.Retrieval.RRFK,
		EmbeddingKey: fmt.Sprintf("%x", sha256.Sum256([]byte(fmt.Sprintf("%s:%q:%q", generation.ModelID, topic.Include, topic.Exclude))))}
	request := domain.FeedTopicPreparation{Topic: topic, Generation: generation, Settings: settings, Inputs: inputs, At: s.config.Now()}
	if work.Inputs.EmbeddingKey == inputs.EmbeddingKey {
		request.IncludeEmbedding = work.IncludeEmbedding
		request.ExcludeEmbedding = work.ExcludeEmbedding
	}
	prepared, err := s.config.Preparer.PrepareFeed(ctx, request)
	if err != nil {
		return fmt.Errorf("prepare Topic %s: %w", topic.ID, err)
	}
	work.IncludeEmbedding = prepared.IncludeEmbedding
	work.ExcludeEmbedding = prepared.ExcludeEmbedding
	err = s.config.Repository.ReplaceFeedCandidates(ctx, domain.FeedCandidateReplacement{Work: work, Inputs: prepared.Inputs, Candidates: prepared.Candidates, JobID: execution.JobID, LeaseOwner: execution.LeaseOwner, At: s.config.Now(), NextEligibleAt: work.NextEligibleAt})
	if errors.Is(err, domain.ErrConflict) {
		return nil
	}
	return err
}
