package httpapi

import (
	"context"

	"github.com/ethernity/obsidian-topic-stream/gen/serverhttp"
	"github.com/ethernity/obsidian-topic-stream/internal/domain"
)

// ListEmbeddingModels returns the local model registry.
func (s *Server) ListEmbeddingModels(ctx context.Context, request serverhttp.ListEmbeddingModelsRequestObject) (serverhttp.ListEmbeddingModelsResponseObject, error) {
	models, err := s.EmbeddingModels.List(ctx)
	if err != nil {
		return nil, err
	}
	items := make([]serverhttp.EmbeddingModel, len(models))
	for index, model := range models {
		items[index] = apiEmbeddingModel(model)
	}
	return serverhttp.ListEmbeddingModels200JSONResponse{Items: items}, nil
}

// CreateEmbeddingModel validates and initializes a new model registry entry.
func (s *Server) CreateEmbeddingModel(ctx context.Context, request serverhttp.CreateEmbeddingModelRequestObject) (serverhttp.CreateEmbeddingModelResponseObject, error) {
	input := *request.Body
	model, err := s.EmbeddingModels.Create(ctx, domain.EmbeddingModelDraft{
		Name: input.Name, Endpoint: input.Endpoint, ProviderModel: input.ProviderModel, Dimensions: input.Dimensions,
	})
	if err != nil {
		failure := applicationProblem(err, problemInvalidEmbeddingModel)
		if failure == nil {
			return nil, err
		}
		return serverhttp.CreateEmbeddingModeldefaultApplicationProblemPlusJSONResponse(*failure), nil
	}
	return serverhttp.CreateEmbeddingModel201JSONResponse{
		Body:    apiEmbeddingModel(model),
		Headers: serverhttp.CreateEmbeddingModel201ResponseHeaders{ETag: versionETag(model.Version)},
	}, nil
}

// GetEmbeddingModel returns a model registry entry with its version ETag.
func (s *Server) GetEmbeddingModel(ctx context.Context, request serverhttp.GetEmbeddingModelRequestObject) (serverhttp.GetEmbeddingModelResponseObject, error) {
	model, err := s.EmbeddingModels.Get(ctx, request.ModelId)
	if err != nil {
		failure := applicationProblem(err, problemInvalidEmbeddingModel)
		if failure == nil {
			return nil, err
		}
		return serverhttp.GetEmbeddingModeldefaultApplicationProblemPlusJSONResponse(*failure), nil
	}
	return serverhttp.GetEmbeddingModel200JSONResponse{
		Body:    apiEmbeddingModel(model),
		Headers: serverhttp.GetEmbeddingModel200ResponseHeaders{ETag: versionETag(model.Version)},
	}, nil
}

// UpdateEmbeddingModel applies a conditional edit using the expected version.
func (s *Server) UpdateEmbeddingModel(ctx context.Context, request serverhttp.UpdateEmbeddingModelRequestObject) (serverhttp.UpdateEmbeddingModelResponseObject, error) {
	expected, err := parseVersionETag(request.Params.IfMatch)
	if err != nil {
		return serverhttp.UpdateEmbeddingModeldefaultApplicationProblemPlusJSONResponse(newProblem(problemVersionPreconditionRequired)), nil
	}
	input := *request.Body
	model, err := s.EmbeddingModels.Update(ctx, request.ModelId, expected, domain.EmbeddingModelUpdate{
		Name: input.Name, Endpoint: input.Endpoint, Enabled: input.Enabled,
	})
	if err != nil {
		failure := applicationProblem(err, problemInvalidEmbeddingModel)
		if failure == nil {
			return nil, err
		}
		return serverhttp.UpdateEmbeddingModeldefaultApplicationProblemPlusJSONResponse(*failure), nil
	}
	return serverhttp.UpdateEmbeddingModel200JSONResponse{
		Body:    apiEmbeddingModel(model),
		Headers: serverhttp.UpdateEmbeddingModel200ResponseHeaders{ETag: versionETag(model.Version)},
	}, nil
}

// InitializeEmbeddingModel retries initialization of a registered model.
func (s *Server) InitializeEmbeddingModel(ctx context.Context, request serverhttp.InitializeEmbeddingModelRequestObject) (serverhttp.InitializeEmbeddingModelResponseObject, error) {
	model, err := s.EmbeddingModels.Initialize(ctx, request.ModelId)
	if err != nil {
		failure := applicationProblem(err, problemInvalidEmbeddingModel)
		if failure == nil {
			return nil, err
		}
		return serverhttp.InitializeEmbeddingModeldefaultApplicationProblemPlusJSONResponse(*failure), nil
	}
	return serverhttp.InitializeEmbeddingModel200JSONResponse{
		Body:    apiEmbeddingModel(model),
		Headers: serverhttp.InitializeEmbeddingModel200ResponseHeaders{ETag: versionETag(model.Version)},
	}, nil
}

// ListEmbeddingModelOptions returns model readiness and selection availability.
func (s *Server) ListEmbeddingModelOptions(ctx context.Context, request serverhttp.ListEmbeddingModelOptionsRequestObject) (serverhttp.ListEmbeddingModelOptionsResponseObject, error) {
	options, err := s.EmbeddingModels.Options(ctx)
	if err != nil {
		return nil, err
	}
	items := make([]serverhttp.EmbeddingModelOption, len(options))
	for index, option := range options {
		items[index] = serverhttp.EmbeddingModelOption{Model: apiEmbeddingModel(option.Model), Selectable: option.Selectable}
	}
	return serverhttp.ListEmbeddingModelOptions200JSONResponse{Items: items}, nil
}

// GetRetrievalStatus reports desired and serving models and any transition Job.
func (s *Server) GetRetrievalStatus(ctx context.Context, request serverhttp.GetRetrievalStatusRequestObject) (serverhttp.GetRetrievalStatusResponseObject, error) {
	status, err := s.EmbeddingModels.RetrievalStatus(ctx)
	if err != nil {
		return nil, err
	}
	return serverhttp.GetRetrievalStatus200JSONResponse{
		LatestTransition: apiModelTransition(status.LatestTransition),
		DesiredModelId:   status.DesiredModelID,
		ServingModelId:   status.ServingModelID,
		TransitionJobId:  status.TransitionJobID,
	}, nil
}

func apiModelTransition(value *domain.ModelTransition) *serverhttp.ModelTransition {
	if value == nil {
		return nil
	}
	return &serverhttp.ModelTransition{
		ModelId: value.ModelID, SelectionGeneration: value.SelectionGeneration,
		JobId: value.JobID, Status: serverhttp.ModelTransitionStatus(value.Status),
		IsCurrent: value.IsCurrent, CanRetry: value.CanRetry,
	}
}

func apiEmbeddingModel(value domain.EmbeddingModel) serverhttp.EmbeddingModel {
	return serverhttp.EmbeddingModel{
		Id: value.ID, Name: value.Name, Endpoint: value.Endpoint, ProviderModel: value.ProviderModel,
		Dimensions: value.Dimensions, Enabled: value.Enabled,
		InitializationStatus: serverhttp.EmbeddingModelInitializationStatus(value.InitializationStatus),
		FailureReason:        value.FailureReason, Version: value.Version, CreatedAt: value.CreatedAt, UpdatedAt: value.UpdatedAt,
	}
}
