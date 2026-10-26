package topics_test

import (
	"context"
	"testing"
	"time"

	"github.com/ethernity/obsidian-topic-stream/internal/domain"
	"github.com/ethernity/obsidian-topic-stream/internal/service/topics"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

type topicRepositoryStub struct {
	topic   domain.Topic
	command domain.TopicUpdate
	result  domain.TopicUpdateResult
}

func TestCreateCollectsIndependentTopicViolations(t *testing.T) {
	t.Parallel()
	service := topics.New(topics.Config{WorkspaceID: uuid.New(), Repository: &topicRepositoryStub{}, Now: time.Now})
	_, err := service.Create(t.Context(), topics.CreateInput{Name: " ", Mode: "invalid", ItemKinds: []domain.ItemKind{"unknown"}})
	var validation *domain.ValidationError
	require.ErrorAs(t, err, &validation)
	require.Len(t, validation.Violations, 3)
	require.Equal(t, "name", validation.Violations[0].Field)
}

// CreateTopic accepts Topic creation without persisting a separate fixture record.
func (s *topicRepositoryStub) CreateTopic(context.Context, domain.Topic) error { return nil }

// ListTopics returns the fixture's single Topic.
func (s *topicRepositoryStub) ListTopics(context.Context, uuid.UUID) ([]domain.Topic, error) {
	return []domain.Topic{s.topic}, nil
}

// GetTopic returns the fixture Topic and its lookup result.
func (s *topicRepositoryStub) GetTopic(context.Context, uuid.UUID, uuid.UUID) (domain.Topic, bool, error) {
	return s.topic, true, nil
}

// UpdateTopic records the semantic edit and correction-reset command for assertions.
func (s *topicRepositoryStub) UpdateTopic(
	_ context.Context,
	command domain.TopicUpdate,
) (domain.TopicUpdateResult, error) {
	s.command = command
	return s.result, nil
}

// DeleteTopic reports successful deletion without changing the fixture.
func (s *topicRepositoryStub) DeleteTopic(context.Context, uuid.UUID, uuid.UUID) (bool, error) {
	return true, nil
}

type projectionQueueSpy struct {
	commits []domain.ItemProjectionCommit
}

type calibrationNotifierSpy struct{ topicIDs []uuid.UUID }

// NotifyTopicChanged records a Topic calibration notification for assertions.
func (s *calibrationNotifierSpy) NotifyTopicChanged(topicID uuid.UUID) {
	s.topicIDs = append(s.topicIDs, topicID)
}

// Enqueue records the submitted work without executing a handler.
func (s *projectionQueueSpy) Enqueue(
	_ context.Context,
	commit domain.ItemProjectionCommit,
) (domain.Job, error) {
	s.commits = append(s.commits, commit)
	return domain.Job{}, nil
}

func TestUpdateCanClearCorrectionsWithoutCreatingSemanticRevision(t *testing.T) {
	t.Parallel()

	workspaceID, topicID, itemID := uuid.New(), uuid.New(), uuid.New()
	now := time.Date(2026, time.October, 1, 8, 0, 0, 0, time.UTC)
	commit := domain.ItemProjectionCommit{
		WorkspaceID: workspaceID, ItemID: itemID, ProjectionRevision: uuid.New(),
	}
	repository := &topicRepositoryStub{
		topic: domain.Topic{
			ID: topicID, WorkspaceID: workspaceID, Name: "AI", Mode: domain.TopicModeOrdinary,
			Enabled: true, Revision: 3, CorrectionEpoch: 1, CorrectionVersion: 8,
		},
		result: domain.TopicUpdateResult{ProjectionCommits: []domain.ItemProjectionCommit{commit}},
	}
	queue := &projectionQueueSpy{}
	calibration := &calibrationNotifierSpy{}
	service := topics.New(topics.Config{
		WorkspaceID: workspaceID, Repository: repository, Projections: queue,
		Calibration: calibration, Now: func() time.Time { return now },
	})

	updated, err := service.Update(t.Context(), topicID, topics.UpdateInput{ClearCorrections: true})

	require.NoError(t, err)
	require.True(t, repository.command.ClearCorrections)
	require.False(t, repository.command.SemanticChanged)
	require.Equal(t, 3, updated.Revision)
	require.Equal(t, []domain.ItemProjectionCommit{commit}, queue.commits)
	require.Equal(t, []uuid.UUID{topicID}, calibration.topicIDs)
}
