package httpapi

import (
	"context"

	"github.com/ethernity/obsidian-topic-stream/gen/serverhttp"
	"github.com/ethernity/obsidian-topic-stream/internal/domain"
	"github.com/ethernity/obsidian-topic-stream/internal/service/topics"
	"github.com/google/uuid"
)

// ListTopics returns Workspace Topics and their current revisions.
func (s *Server) ListTopics(ctx context.Context, request serverhttp.ListTopicsRequestObject) (serverhttp.ListTopicsResponseObject, error) {
	configured, err := s.Topics.List(ctx)
	if err != nil {
		return nil, err
	}
	items := make([]serverhttp.Topic, len(configured))
	for index, topic := range configured {
		items[index] = apiTopic(topic)
	}
	return serverhttp.ListTopics200JSONResponse{Items: items}, nil
}

// CreateTopic validates and persists a new thematic subscription.
func (s *Server) CreateTopic(ctx context.Context, request serverhttp.CreateTopicRequestObject) (serverhttp.CreateTopicResponseObject, error) {
	input := *request.Body
	created, err := s.Topics.Create(ctx, topics.CreateInput{
		Name: input.Name, SourceIDs: input.SourceIds, ItemKinds: domainItemKinds(input.ItemKinds),
		Include: input.Include, Exclude: input.Exclude, Mode: domain.TopicMode(input.Mode), Enabled: input.Enabled,
	})
	if err != nil {
		failure := applicationProblem(err, problemInvalidTopic)
		if failure == nil {
			return nil, err
		}
		return serverhttp.CreateTopicdefaultApplicationProblemPlusJSONResponse(*failure), nil
	}
	return serverhttp.CreateTopic201JSONResponse(apiTopic(created)), nil
}

// UpdateTopic applies supplied fields and any explicit correction reset.
func (s *Server) UpdateTopic(ctx context.Context, request serverhttp.UpdateTopicRequestObject) (serverhttp.UpdateTopicResponseObject, error) {
	input := *request.Body
	updated, err := s.Topics.Update(ctx, request.TopicId, topicUpdate(input))
	if err != nil {
		failure := applicationProblem(err, problemInvalidTopic)
		if failure == nil {
			return nil, err
		}
		return serverhttp.UpdateTopicdefaultApplicationProblemPlusJSONResponse(*failure), nil
	}
	return serverhttp.UpdateTopic200JSONResponse(apiTopic(updated)), nil
}

// DeleteTopic removes a Workspace Topic.
func (s *Server) DeleteTopic(ctx context.Context, request serverhttp.DeleteTopicRequestObject) (serverhttp.DeleteTopicResponseObject, error) {
	if err := s.Topics.Delete(ctx, request.TopicId); err != nil {
		failure := applicationProblem(err, problemInvalidTopic)
		if failure == nil {
			return nil, err
		}
		return serverhttp.DeleteTopicdefaultApplicationProblemPlusJSONResponse(*failure), nil
	}
	return serverhttp.DeleteTopic204Response{}, nil
}

func topicUpdate(input serverhttp.TopicPatch) topics.UpdateInput {
	result := topics.UpdateInput{
		Name: input.Name, SourceIDs: input.SourceIds, Include: input.Include, Exclude: input.Exclude, Enabled: input.Enabled,
	}
	if input.ItemKinds != nil {
		value := domainItemKinds(*input.ItemKinds)
		result.ItemKinds = &value
	}
	if input.Mode != nil {
		value := domain.TopicMode(*input.Mode)
		result.Mode = &value
	}
	result.ClearCorrections = input.CorrectionPolicy != nil && *input.CorrectionPolicy == serverhttp.Clear
	return result
}

func domainItemKinds(values []serverhttp.ItemKind) []domain.ItemKind {
	result := make([]domain.ItemKind, len(values))
	for index, value := range values {
		result[index] = domain.ItemKind(value)
	}
	return result
}

func apiTopic(topic domain.Topic) serverhttp.Topic {
	sourceIDs := make([]uuid.UUID, len(topic.SourceIDs))
	copy(sourceIDs, topic.SourceIDs)
	itemKinds := make([]serverhttp.ItemKind, len(topic.ItemKinds))
	for index, kind := range topic.ItemKinds {
		itemKinds[index] = serverhttp.ItemKind(kind)
	}
	return serverhttp.Topic{
		Id: topic.ID, Name: topic.Name, SourceIds: sourceIDs,
		ItemKinds: itemKinds, Include: topic.Include, Exclude: topic.Exclude, Mode: serverhttp.TopicMode(topic.Mode),
		Enabled: topic.Enabled, Revision: topic.Revision, CreatedAt: topic.CreatedAt, UpdatedAt: topic.UpdatedAt,
	}
}
