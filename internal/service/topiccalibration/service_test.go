package topiccalibration

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/ethernity/obsidian-topic-stream/internal/domain"
	"github.com/ethernity/obsidian-topic-stream/internal/service/jobs"
	"github.com/ethernity/obsidian-topic-stream/internal/service/ranking"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

type repositoryStub struct {
	active        domain.RetrievalIndex
	calibration   domain.TopicGateCalibration
	calibrated    bool
	snapshot      domain.TopicCalibrationSnapshot
	saved         []domain.TopicGateCalibration
	staleTopicIDs []uuid.UUID
}

// GetActive returns the fixture's serving generation and lookup result.
func (s *repositoryStub) GetActive(context.Context, uuid.UUID) (domain.RetrievalIndex, bool, error) {
	return s.active, true, nil
}

// GetTopicGateCalibration returns stored fixture calibration and whether it is present.
func (s *repositoryStub) GetTopicGateCalibration(
	context.Context,
	uuid.UUID,
	uuid.UUID,
) (domain.TopicGateCalibration, bool, error) {
	return s.calibration, s.calibrated, nil
}

// LoadTopicCalibrationSnapshot returns the fixture's Topic, model, and correction snapshot.
func (s *repositoryStub) LoadTopicCalibrationSnapshot(
	context.Context,
	uuid.UUID,
	uuid.UUID,
	uuid.UUID,
) (domain.TopicCalibrationSnapshot, bool, error) {
	return s.snapshot, true, nil
}

// SaveTopicGateCalibration records proposed threshold materialization and reports success.
func (s *repositoryStub) SaveTopicGateCalibration(
	_ context.Context,
	calibration domain.TopicGateCalibration,
) (bool, error) {
	s.saved = append(s.saved, calibration)
	return true, nil
}

// ListTopicsNeedingCalibration returns a copy of the fixture's stale Topic IDs.
func (s *repositoryStub) ListTopicsNeedingCalibration(
	context.Context,
	uuid.UUID,
	uuid.UUID,
	string,
) ([]uuid.UUID, error) {
	return append([]uuid.UUID(nil), s.staleTopicIDs...), nil
}

type queueStub struct {
	handler  jobs.Handler
	requests []jobs.EnqueueRequest
}

// RegisterDefinition captures the handler so tests can execute calibration work directly.
func (s *queueStub) RegisterDefinition(_ jobs.JobDefinition, handler jobs.Handler) {
	s.handler = handler
}

// Enqueue records the submitted work without executing a handler.
func (s *queueStub) Enqueue(_ context.Context, request jobs.EnqueueRequest) (domain.Job, error) {
	s.requests = append(s.requests, request)
	return domain.Job{ID: uuid.New(), Kind: request.Definition.Kind}, nil
}

type runtimeStub struct {
	vectors map[uuid.UUID][]float32
}

// Embed returns a deterministic fixture vector without contacting a provider.
func (s runtimeStub) Embed(_ context.Context, text string) ([]float32, error) {
	if text == "include" {
		return []float32{1, 0}, nil
	}
	return []float32{0, 1}, nil
}

// DenseVectors returns available fixture vectors for the requested Item IDs.
func (s runtimeStub) DenseVectors(_ context.Context, ids []uuid.UUID) (map[uuid.UUID][]float32, error) {
	result := make(map[uuid.UUID][]float32, len(ids))
	for _, id := range ids {
		if vector, found := s.vectors[id]; found {
			result[id] = vector
		}
	}
	return result, nil
}

type factoryStub struct{ runtime Runtime }

// Build returns model-specific fixture dependencies without acquiring production resources.
func (s factoryStub) Build(domain.EmbeddingModel) (Runtime, error) { return s.runtime, nil }

func TestThresholdsServeCompatibleStaleMaterializationButBootstrapAcrossSemanticRevision(t *testing.T) {
	t.Parallel()

	workspaceID, topicID, modelID := uuid.New(), uuid.New(), uuid.New()
	defaults := ranking.DefaultTopicThresholdDefaults("embedding")
	repository := &repositoryStub{
		active:     domain.RetrievalIndex{WorkspaceID: workspaceID, ModelID: modelID},
		calibrated: true,
		calibration: domain.TopicGateCalibration{
			TopicID: topicID, EmbeddingModelID: modelID, TopicRevision: 3,
			CorrectionEpoch: 2, CorrectionVersion: 8, AlgorithmVersion: defaults.Version,
			IncludeThreshold: 0.71, ExcludeThreshold: 0.82,
		},
	}
	service, err := New(Config{
		WorkspaceID: workspaceID, Repository: repository, Jobs: &queueStub{},
		Factory: factoryStub{runtime: runtimeStub{}}, Defaults: defaults, Now: time.Now,
	})
	require.NoError(t, err)

	thresholds, err := service.Thresholds(t.Context(), domain.Topic{
		ID: topicID, WorkspaceID: workspaceID, Mode: domain.TopicModeOrdinary,
		Revision: 3, CorrectionEpoch: 2, CorrectionVersion: 8,
	}, defaults)
	require.NoError(t, err)
	require.Equal(t, ranking.TopicThresholds{Include: 0.71, Exclude: 0.82}, thresholds)
	require.Empty(t, service.wake)

	thresholds, err = service.Thresholds(t.Context(), domain.Topic{
		ID: topicID, WorkspaceID: workspaceID, Mode: domain.TopicModeOrdinary,
		Revision: 3, CorrectionEpoch: 2, CorrectionVersion: 9,
	}, defaults)
	require.NoError(t, err)
	require.Equal(t, ranking.TopicThresholds{Include: 0.71, Exclude: 0.82}, thresholds)
	require.Len(t, service.wake, 1)

	thresholds, err = service.Thresholds(t.Context(), domain.Topic{
		ID: topicID, WorkspaceID: workspaceID, Mode: domain.TopicModeOrdinary,
		Revision: 4, CorrectionEpoch: 2, CorrectionVersion: 9,
	}, defaults)
	require.NoError(t, err)
	require.Equal(t, defaults.Ordinary, thresholds)
}

func TestCalibrationJobReadsExactVectorsAndPersistsEffectiveThresholds(t *testing.T) {
	t.Parallel()

	workspaceID, topicID, modelID := uuid.New(), uuid.New(), uuid.New()
	defaults := ranking.TopicThresholdDefaults{
		Model: "embedding", Version: "gate-v1", Shrinkage: 2,
		Ordinary: ranking.TopicThresholds{Include: 0.5, Exclude: 0.5},
		Strict:   ranking.TopicThresholds{Include: 0.5, Exclude: 0.5},
	}
	vectors := make(map[uuid.UUID][]float32)
	corrections := make([]domain.TopicCorrection, 0, 10)
	for index := range 5 {
		included, excluded := uuid.New(), uuid.New()
		vectors[included] = []float32{0.9 - float32(index)*0.1, 0.1 + float32(index)*0.1}
		vectors[excluded] = []float32{0.1 + float32(index)*0.1, 0.9 - float32(index)*0.1}
		corrections = append(corrections,
			domain.TopicCorrection{ItemID: included, TopicID: topicID, Membership: domain.TopicMembershipInclude},
			domain.TopicCorrection{ItemID: excluded, TopicID: topicID, Membership: domain.TopicMembershipExclude},
		)
	}
	repository := &repositoryStub{snapshot: domain.TopicCalibrationSnapshot{
		Topic: domain.Topic{ID: topicID, WorkspaceID: workspaceID, Include: "include", Exclude: "exclude",
			Mode: domain.TopicModeOrdinary, Enabled: true, Revision: 3, CorrectionEpoch: 2, CorrectionVersion: 10},
		Model: domain.EmbeddingModel{ID: modelID}, Corrections: corrections,
	}}
	queue := &queueStub{}
	_, err := New(Config{
		WorkspaceID: workspaceID, Repository: repository, Jobs: queue,
		Factory: factoryStub{runtime: runtimeStub{vectors: vectors}}, Defaults: defaults, Now: time.Now,
	})
	require.NoError(t, err)

	payload, err := json.Marshal(jobArguments{TopicID: topicID, EmbeddingModelID: modelID})
	require.NoError(t, err)
	require.NoError(t, queue.handler(t.Context(), payload))

	require.Len(t, repository.saved, 1)
	saved := repository.saved[0]
	require.Equal(t, 3, saved.TopicRevision)
	require.Equal(t, int64(10), saved.CorrectionVersion)
	require.NotEqual(t, defaults.Ordinary.Include, saved.IncludeThreshold)
	require.NotEqual(t, defaults.Ordinary.Exclude, saved.ExcludeThreshold)
}

func TestRepairEnqueuesOneStableJobPerStaleTopic(t *testing.T) {
	t.Parallel()

	workspaceID, modelID, firstTopicID, secondTopicID := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	repository := &repositoryStub{
		active:        domain.RetrievalIndex{WorkspaceID: workspaceID, ModelID: modelID},
		staleTopicIDs: []uuid.UUID{firstTopicID, secondTopicID},
	}
	queue := &queueStub{}
	service, err := New(Config{
		WorkspaceID: workspaceID, Repository: repository, Jobs: queue,
		Factory: factoryStub{runtime: runtimeStub{}}, Defaults: ranking.DefaultTopicThresholdDefaults("embedding"),
		Now: time.Now,
	})
	require.NoError(t, err)

	require.NoError(t, service.Repair(t.Context()))

	require.Len(t, queue.requests, 2)
	require.Equal(t, "calibrate-topic:"+firstTopicID.String()+":"+modelID.String(), queue.requests[0].IdempotencyKey)
	require.Equal(t, CalibrateTopicJobKind, queue.requests[0].Definition.Kind)
	require.Equal(t, domain.JobQueueRetrieval, queue.requests[0].Definition.Queue)
}

func TestCalibrationJobDoesNoWorkWhenMaterializationIsAlreadyCurrent(t *testing.T) {
	t.Parallel()

	workspaceID, topicID, modelID := uuid.New(), uuid.New(), uuid.New()
	defaults := ranking.DefaultTopicThresholdDefaults("embedding")
	repository := &repositoryStub{
		calibrated: true,
		calibration: domain.TopicGateCalibration{
			TopicID: topicID, EmbeddingModelID: modelID, TopicRevision: 2,
			CorrectionEpoch: 1, CorrectionVersion: 4, AlgorithmVersion: defaults.Version,
		},
		snapshot: domain.TopicCalibrationSnapshot{
			Topic: domain.Topic{ID: topicID, WorkspaceID: workspaceID, Enabled: true, Revision: 2,
				Mode: domain.TopicModeOrdinary, CorrectionEpoch: 1, CorrectionVersion: 4},
			Model: domain.EmbeddingModel{ID: modelID},
		},
	}
	queue := &queueStub{}
	_, err := New(Config{
		WorkspaceID: workspaceID, Repository: repository, Jobs: queue,
		Factory: factoryStub{runtime: runtimeStub{}}, Defaults: defaults, Now: time.Now,
	})
	require.NoError(t, err)
	payload, err := json.Marshal(jobArguments{TopicID: topicID, EmbeddingModelID: modelID})
	require.NoError(t, err)

	require.NoError(t, queue.handler(t.Context(), payload))
	require.Empty(t, repository.saved)
}
