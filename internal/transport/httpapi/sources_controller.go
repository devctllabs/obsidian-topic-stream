package httpapi

import (
	"context"
	"errors"

	"github.com/ethernity/obsidian-topic-stream/gen/serverhttp"
	"github.com/ethernity/obsidian-topic-stream/internal/domain"
	"github.com/ethernity/obsidian-topic-stream/internal/service/sources"
)

// ListSources returns configured Workspace Sources.
func (s *Server) ListSources(ctx context.Context, request serverhttp.ListSourcesRequestObject) (serverhttp.ListSourcesResponseObject, error) {
	configured, err := s.Sources.List(ctx)
	if err != nil {
		return nil, err
	}
	items := make([]serverhttp.Source, len(configured))
	for index, source := range configured {
		items[index], err = apiSource(source)
		if err != nil {
			return nil, err
		}
	}
	return serverhttp.ListSources200JSONResponse{Items: items}, nil
}

// CreateSource validates and persists a new Workspace Source.
func (s *Server) CreateSource(ctx context.Context, request serverhttp.CreateSourceRequestObject) (serverhttp.CreateSourceResponseObject, error) {
	input := *request.Body
	config, err := domainSourceConfig(input.Config)
	if err != nil {
		return serverhttp.CreateSourcedefaultApplicationProblemPlusJSONResponse(validationProblem(problemInvalidSource, err)), nil
	}
	created, err := s.Sources.Create(ctx, sources.CreateInput{
		Name: input.Name, Config: config,
		Enabled: input.Enabled, PollIntervalMinutes: input.PollIntervalMinutes,
	})
	if err != nil {
		failure := applicationProblem(err, problemInvalidSource)
		if failure == nil {
			return nil, err
		}
		return serverhttp.CreateSourcedefaultApplicationProblemPlusJSONResponse(*failure), nil
	}
	response, err := apiSource(created)
	if err != nil {
		return nil, err
	}
	return serverhttp.CreateSource201JSONResponse(response), nil
}

// SyncAllSources submits durable synchronization for enabled Workspace Sources.
func (s *Server) SyncAllSources(ctx context.Context, request serverhttp.SyncAllSourcesRequestObject) (serverhttp.SyncAllSourcesResponseObject, error) {
	job, err := s.Sources.SyncAll(ctx)
	if err != nil {
		return nil, err
	}
	return serverhttp.SyncAllSources202JSONResponse{JobId: job.ID}, nil
}

// GetSource returns a configured Workspace Source.
func (s *Server) GetSource(ctx context.Context, request serverhttp.GetSourceRequestObject) (serverhttp.GetSourceResponseObject, error) {
	source, err := s.Sources.Get(ctx, request.SourceId)
	if err != nil {
		failure := applicationProblem(err, problemInvalidSource)
		if failure == nil {
			return nil, err
		}
		return serverhttp.GetSourcedefaultApplicationProblemPlusJSONResponse(*failure), nil
	}
	response, err := apiSource(source)
	if err != nil {
		return nil, err
	}
	return serverhttp.GetSource200JSONResponse(response), nil
}

// UpdateSource validates and applies supplied Source configuration fields.
func (s *Server) UpdateSource(ctx context.Context, request serverhttp.UpdateSourceRequestObject) (serverhttp.UpdateSourceResponseObject, error) {
	input := *request.Body
	var config *domain.SourceConfig
	if input.Config != nil {
		value, configErr := domainSourceConfig(*input.Config)
		if configErr != nil {
			return serverhttp.UpdateSourcedefaultApplicationProblemPlusJSONResponse(validationProblem(problemInvalidSource, configErr)), nil
		}
		config = &value
	}
	updated, err := s.Sources.Update(ctx, request.SourceId, sources.UpdateInput{
		Name: input.Name, Config: config, Enabled: input.Enabled, PollIntervalMinutes: input.PollIntervalMinutes,
	})
	if err != nil {
		failure := applicationProblem(err, problemInvalidSource)
		if failure == nil {
			return nil, err
		}
		return serverhttp.UpdateSourcedefaultApplicationProblemPlusJSONResponse(*failure), nil
	}
	response, err := apiSource(updated)
	if err != nil {
		return nil, err
	}
	return serverhttp.UpdateSource200JSONResponse(response), nil
}

// DeleteSource removes the configured Workspace Source.
func (s *Server) DeleteSource(ctx context.Context, request serverhttp.DeleteSourceRequestObject) (serverhttp.DeleteSourceResponseObject, error) {
	if err := s.Sources.Delete(ctx, request.SourceId); err != nil {
		failure := applicationProblem(err, problemInvalidSource)
		if failure == nil {
			return nil, err
		}
		return serverhttp.DeleteSourcedefaultApplicationProblemPlusJSONResponse(*failure), nil
	}
	return serverhttp.DeleteSource204Response{}, nil
}

// SyncSource returns durable, idempotent synchronization work for one Source.
func (s *Server) SyncSource(ctx context.Context, request serverhttp.SyncSourceRequestObject) (serverhttp.SyncSourceResponseObject, error) {
	job, err := s.Sources.Sync(ctx, request.SourceId)
	if errors.Is(err, sources.ErrNotFound) {
		return serverhttp.SyncSourcedefaultApplicationProblemPlusJSONResponse(newProblem(problemSourceNotFound)), nil
	}
	if err != nil {
		return nil, err
	}
	return serverhttp.SyncSource202JSONResponse{JobId: job.ID}, nil
}
