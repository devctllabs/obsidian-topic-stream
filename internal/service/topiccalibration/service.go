package topiccalibration

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/ethernity/obsidian-topic-stream/internal/domain"
	"github.com/ethernity/obsidian-topic-stream/internal/service/jobs"
	"github.com/ethernity/obsidian-topic-stream/internal/service/ranking"
	"github.com/google/uuid"
)

const (
	CalibrateTopicJobKind = "calibrate-topic"
	argumentsVersion      = 1
	defaultRepairInterval = 5 * time.Minute
)

// Repository loads correction snapshots and stores compatible Topic gate thresholds.
type Repository interface {
	// GetActive returns the Workspace's serving retrieval generation and whether one exists.
	GetActive(ctx context.Context, workspaceID uuid.UUID) (domain.RetrievalIndex, bool, error)
	// GetTopicGateCalibration returns stored gate calibration for one Topic and Embedding Model.
	GetTopicGateCalibration(ctx context.Context, topicID uuid.UUID, modelID uuid.UUID) (domain.TopicGateCalibration, bool, error)
	// LoadTopicCalibrationSnapshot loads the Topic, model, and current corrections used by one calibration
	// attempt.
	LoadTopicCalibrationSnapshot(ctx context.Context, workspaceID uuid.UUID, topicID uuid.UUID, modelID uuid.UUID) (domain.TopicCalibrationSnapshot, bool, error)
	// SaveTopicGateCalibration saves thresholds only if their Topic and correction inputs remain current.
	SaveTopicGateCalibration(ctx context.Context, calibration domain.TopicGateCalibration) (bool, error)
	// ListTopicsNeedingCalibration returns enabled Topics whose calibration inputs or algorithm have changed.
	ListTopicsNeedingCalibration(ctx context.Context, workspaceID uuid.UUID, modelID uuid.UUID, algorithmVersion string) ([]uuid.UUID, error)
}

// Queue registers versioned handlers and submits durable Workspace Jobs.
type Queue interface {
	// RegisterDefinition registers a handler for a versioned Job kind.
	RegisterDefinition(definition jobs.JobDefinition, handler jobs.Handler)
	// Enqueue submits a durable Job and reuses an equivalent active idempotency key.
	Enqueue(ctx context.Context, request jobs.EnqueueRequest) (domain.Job, error)
}

var calibrateTopicJob = jobs.JobDefinition{
	Kind: CalibrateTopicJobKind, ArgumentsVersion: argumentsVersion, Queue: domain.JobQueueRetrieval,
	Retry: jobs.RetryPolicy{MaxAttempts: 5, BaseDelay: 5 * time.Second, MaxDelay: 5 * time.Minute},
}

// Runtime embeds Topic criteria and reads correction vectors for calibration.
type Runtime interface {
	// Embed returns the model's vector representation of text.
	Embed(ctx context.Context, text string) ([]float32, error)
	// DenseVectors returns stored dense vectors for the requested Items.
	DenseVectors(ctx context.Context, itemIDs []uuid.UUID) (map[uuid.UUID][]float32, error)
}

// Factory builds model-specific embedding and correction-vector access.
type Factory interface {
	// Build constructs the embedding and index capabilities for the supplied model or generation.
	Build(model domain.EmbeddingModel) (Runtime, error)
}

// Config supplies dependencies and operating settings for the topiccalibration service.
type Config struct {
	WorkspaceID    uuid.UUID
	Repository     Repository
	Jobs           Queue
	Factory        Factory
	Defaults       ranking.TopicThresholdDefaults
	Now            func() time.Time
	RepairInterval time.Duration
}

// Service reconciles model-specific Topic gates with current membership corrections.
type Service struct {
	workspaceID    uuid.UUID
	repository     Repository
	jobs           Queue
	factory        Factory
	defaults       ranking.TopicThresholdDefaults
	now            func() time.Time
	repairInterval time.Duration
	wake           chan uuid.UUID
}

// New validates bootstrap thresholds and dependencies, then registers durable Topic calibration work.
func New(config Config) (*Service, error) {
	if config.WorkspaceID == uuid.Nil || config.Repository == nil || config.Jobs == nil ||
		config.Factory == nil || config.Now == nil {
		return nil, errors.New("topic calibration dependencies are required")
	}
	if _, _, err := config.Defaults.Calibrate(domain.TopicModeOrdinary, nil); err != nil {
		return nil, fmt.Errorf("validate topic calibration defaults: %w", err)
	}
	if config.RepairInterval <= 0 {
		config.RepairInterval = defaultRepairInterval
	}
	service := &Service{
		workspaceID: config.WorkspaceID, repository: config.Repository, jobs: config.Jobs,
		factory: config.Factory, defaults: config.Defaults, now: config.Now,
		repairInterval: config.RepairInterval, wake: make(chan uuid.UUID, 128),
	}
	config.Jobs.RegisterDefinition(calibrateTopicJob, service.handleCalibration)
	return service, nil
}

// Thresholds serves a materialization that differs only by a newer correction version. Semantic,
// correction-epoch, model, and algorithm changes fall back to bootstrap thresholds until rebuilt.
func (s *Service) Thresholds(
	ctx context.Context,
	topic domain.Topic,
	defaults ranking.TopicThresholdDefaults,
) (ranking.TopicThresholds, error) {
	bootstrap, _, err := defaults.Calibrate(topic.Mode, nil)
	if err != nil {
		return ranking.TopicThresholds{}, err
	}
	active, found, err := s.repository.GetActive(ctx, s.workspaceID)
	if err != nil {
		return ranking.TopicThresholds{}, fmt.Errorf("get serving embedding model: %w", err)
	}
	if !found {
		return bootstrap, nil
	}
	calibration, found, err := s.repository.GetTopicGateCalibration(ctx, topic.ID, active.ModelID)
	if err != nil {
		return ranking.TopicThresholds{}, fmt.Errorf("get topic gate calibration: %w", err)
	}
	if !found || !compatible(calibration, topic, active.ModelID, defaults.Version) {
		s.NotifyTopicChanged(topic.ID)
		return bootstrap, nil
	}
	if calibration.CorrectionVersion < topic.CorrectionVersion {
		s.NotifyTopicChanged(topic.ID)
	}
	return ranking.TopicThresholds{
		Include: calibration.IncludeThreshold,
		Exclude: calibration.ExcludeThreshold,
	}, nil
}

func compatible(
	calibration domain.TopicGateCalibration,
	topic domain.Topic,
	modelID uuid.UUID,
	algorithmVersion string,
) bool {
	return calibration.TopicID == topic.ID && calibration.EmbeddingModelID == modelID &&
		calibration.TopicRevision == topic.Revision && calibration.CorrectionEpoch == topic.CorrectionEpoch &&
		calibration.CorrectionVersion <= topic.CorrectionVersion && calibration.AlgorithmVersion == algorithmVersion
}

func current(
	calibration domain.TopicGateCalibration,
	topic domain.Topic,
	modelID uuid.UUID,
	algorithmVersion string,
) bool {
	return compatible(calibration, topic, modelID, algorithmVersion) &&
		calibration.CorrectionVersion == topic.CorrectionVersion
}

// NotifyTopicChanged schedules best-effort reconciliation without extending the write request.
func (s *Service) NotifyTopicChanged(topicID uuid.UUID) {
	if topicID == uuid.Nil {
		return
	}
	select {
	case s.wake <- topicID:
	default:
	}
}

// NotifyModelChanged wakes a full scan after retrieval atomically activates another model.
func (s *Service) NotifyModelChanged() {
	select {
	case s.wake <- uuid.Nil:
	default:
	}
}

// Run reconciles stale Topic calibrations and processes wakeups until ctx is cancelled.
func (s *Service) Run(ctx context.Context) error {
	if err := s.Repair(ctx); err != nil {
		return err
	}
	ticker := time.NewTicker(s.repairInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			if err := s.Repair(ctx); err != nil {
				return err
			}
		case topicID := <-s.wake:
			var err error
			if topicID == uuid.Nil {
				err = s.Repair(ctx)
			} else {
				err = s.enqueueForServingModel(ctx, topicID)
			}
			if err != nil {
				return err
			}
		}
	}
}

// Repair submits bounded calibration Jobs for stale Topics under the current serving model.
func (s *Service) Repair(ctx context.Context) error {
	active, found, err := s.repository.GetActive(ctx, s.workspaceID)
	if err != nil {
		return fmt.Errorf("get serving embedding model: %w", err)
	}
	if !found {
		return nil
	}
	topicIDs, err := s.repository.ListTopicsNeedingCalibration(
		ctx, s.workspaceID, active.ModelID, s.defaults.Version,
	)
	if err != nil {
		return fmt.Errorf("list topics needing calibration: %w", err)
	}
	for _, topicID := range topicIDs {
		if err := s.enqueue(ctx, topicID, active.ModelID); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) enqueueForServingModel(ctx context.Context, topicID uuid.UUID) error {
	active, found, err := s.repository.GetActive(ctx, s.workspaceID)
	if err != nil {
		return fmt.Errorf("get serving embedding model: %w", err)
	}
	if !found {
		return nil
	}
	return s.enqueue(ctx, topicID, active.ModelID)
}

func (s *Service) enqueue(ctx context.Context, topicID, modelID uuid.UUID) error {
	_, err := s.jobs.Enqueue(ctx, jobs.EnqueueRequest{
		WorkspaceID: s.workspaceID, Definition: calibrateTopicJob,
		Arguments:      jobArguments{TopicID: topicID, EmbeddingModelID: modelID},
		IdempotencyKey: "calibrate-topic:" + topicID.String() + ":" + modelID.String(),
	})
	if err != nil {
		return fmt.Errorf("enqueue topic calibration: %w", err)
	}
	return nil
}

type jobArguments struct {
	TopicID          uuid.UUID `json:"topicId"`
	EmbeddingModelID uuid.UUID `json:"embeddingModelId"`
}

func (s *Service) handleCalibration(ctx context.Context, payload json.RawMessage) error {
	arguments, err := decodeJobArguments(payload)
	if err != nil {
		return err
	}
	snapshot, found, err := s.repository.LoadTopicCalibrationSnapshot(
		ctx, s.workspaceID, arguments.TopicID, arguments.EmbeddingModelID,
	)
	if err != nil {
		return fmt.Errorf("load topic calibration snapshot: %w", err)
	}
	if !found || !snapshot.Topic.Enabled {
		return nil
	}
	calibration, found, err := s.repository.GetTopicGateCalibration(
		ctx, snapshot.Topic.ID, snapshot.Model.ID,
	)
	if err != nil {
		return fmt.Errorf("get current topic gate calibration: %w", err)
	}
	if found && current(calibration, snapshot.Topic, snapshot.Model.ID, s.defaults.Version) {
		return nil
	}
	return s.recalculate(ctx, snapshot)
}

func decodeJobArguments(payload json.RawMessage) (jobArguments, error) {
	var arguments jobArguments
	if err := json.Unmarshal(payload, &arguments); err != nil ||
		arguments.TopicID == uuid.Nil || arguments.EmbeddingModelID == uuid.Nil {
		return jobArguments{}, jobs.Permanent(errors.New("invalid calibrate-topic arguments"))
	}
	return arguments, nil
}

func (s *Service) recalculate(ctx context.Context, snapshot domain.TopicCalibrationSnapshot) error {
	runtime, err := s.factory.Build(snapshot.Model)
	if err != nil {
		return fmt.Errorf("build topic calibration runtime: %w", err)
	}
	thresholds, err := s.calculate(ctx, runtime, snapshot)
	if err != nil {
		return err
	}
	_, err = s.repository.SaveTopicGateCalibration(ctx, domain.TopicGateCalibration{
		TopicID: snapshot.Topic.ID, EmbeddingModelID: snapshot.Model.ID,
		TopicRevision: snapshot.Topic.Revision, CorrectionEpoch: snapshot.Topic.CorrectionEpoch,
		CorrectionVersion: snapshot.Topic.CorrectionVersion, AlgorithmVersion: s.defaults.Version,
		IncludeThreshold: thresholds.Include, ExcludeThreshold: thresholds.Exclude, UpdatedAt: s.now(),
	})
	if err != nil {
		return fmt.Errorf("save topic gate calibration: %w", err)
	}
	return nil
}

func (s *Service) calculate(
	ctx context.Context,
	runtime Runtime,
	snapshot domain.TopicCalibrationSnapshot,
) (ranking.TopicThresholds, error) {
	ids := make([]uuid.UUID, len(snapshot.Corrections))
	for index, correction := range snapshot.Corrections {
		ids[index] = correction.ItemID
	}
	vectors, err := runtime.DenseVectors(ctx, ids)
	if err != nil {
		return ranking.TopicThresholds{}, fmt.Errorf("load correction vectors: %w", err)
	}
	include, err := embedOptional(ctx, runtime, snapshot.Topic.Include)
	if err != nil {
		return ranking.TopicThresholds{}, fmt.Errorf("embed topic include: %w", err)
	}
	exclude, err := embedOptional(ctx, runtime, snapshot.Topic.Exclude)
	if err != nil {
		return ranking.TopicThresholds{}, fmt.Errorf("embed topic exclude: %w", err)
	}
	samples := make([]ranking.TopicCalibrationSample, 0, len(snapshot.Corrections))
	for _, correction := range snapshot.Corrections {
		vector, found := vectors[correction.ItemID]
		if !found {
			return ranking.TopicThresholds{}, fmt.Errorf("correction item %s has no dense vector", correction.ItemID)
		}
		samples = append(samples, ranking.TopicCalibrationSample{
			IncludeCosine: cosine(vector, include), ExcludeCosine: cosine(vector, exclude),
			Membership: correction.Membership,
		})
	}
	thresholds, _, err := s.defaults.Calibrate(snapshot.Topic.Mode, samples)
	return thresholds, err
}

func embedOptional(ctx context.Context, runtime Runtime, text string) ([]float32, error) {
	if text == "" {
		return nil, nil
	}
	return runtime.Embed(ctx, text)
}

func cosine(left, right []float32) float64 {
	if len(left) == 0 || len(left) != len(right) {
		return 0
	}
	var dot, leftNorm, rightNorm float64
	for index := range left {
		leftValue, rightValue := float64(left[index]), float64(right[index])
		dot += leftValue * rightValue
		leftNorm += leftValue * leftValue
		rightNorm += rightValue * rightValue
	}
	if leftNorm == 0 || rightNorm == 0 {
		return 0
	}
	return dot / (math.Sqrt(leftNorm) * math.Sqrt(rightNorm))
}
