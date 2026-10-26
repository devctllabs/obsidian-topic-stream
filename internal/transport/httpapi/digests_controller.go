package httpapi

import (
	"context"

	"github.com/ethernity/obsidian-topic-stream/gen/serverhttp"
	"github.com/ethernity/obsidian-topic-stream/internal/service/digests"
)

// ListDigestIssues returns saved issues with an optional Digest Definition filter.
func (s *Server) ListDigestIssues(ctx context.Context, request serverhttp.ListDigestIssuesRequestObject) (serverhttp.ListDigestIssuesResponseObject, error) {
	page, err := s.Digests.ListIssues(ctx, digests.IssueListRequest{DigestID: request.Params.DigestId, Cursor: valueOr(request.Params.Cursor, ""), Limit: valueOr(request.Params.Limit, 20)})
	if err != nil {
		failure := applicationProblem(err, problemInvalidDigest)
		if failure == nil {
			return nil, err
		}
		return serverhttp.ListDigestIssuesdefaultApplicationProblemPlusJSONResponse(*failure), nil
	}
	result := make([]serverhttp.DigestIssueSummary, len(page.Items))
	for index, issue := range page.Items {
		result[index] = serverhttp.DigestIssueSummary{
			Id: issue.ID, DigestId: issue.DigestID, GenerationJobId: issue.GenerationJobID, Title: issue.Title,
			Status:       serverhttp.DigestIssueSummaryStatus(issue.Status),
			ScheduledFor: issue.ScheduledFor, GeneratedAt: issue.GeneratedAt,
		}
	}
	return serverhttp.ListDigestIssues200JSONResponse{Items: result, NextCursor: page.NextCursor}, nil
}

// GetDigestIssue returns one saved immutable issue or a problem response.
func (s *Server) GetDigestIssue(ctx context.Context, request serverhttp.GetDigestIssueRequestObject) (serverhttp.GetDigestIssueResponseObject, error) {
	issue, err := s.Digests.GetIssue(ctx, request.IssueId)
	if err != nil {
		failure := applicationProblem(err, problemInvalidDigest)
		if failure == nil {
			return nil, err
		}
		return serverhttp.GetDigestIssuedefaultApplicationProblemPlusJSONResponse(*failure), nil
	}
	response, err := apiDigestIssue(issue)
	if err != nil {
		failure := applicationProblem(err, problemInvalidDigest)
		if failure == nil {
			return nil, err
		}
		return serverhttp.GetDigestIssuedefaultApplicationProblemPlusJSONResponse(*failure), nil
	}
	return serverhttp.GetDigestIssue200JSONResponse(response), nil
}

// ListDigests returns the Workspace's Digest Definitions.
func (s *Server) ListDigests(ctx context.Context, request serverhttp.ListDigestsRequestObject) (serverhttp.ListDigestsResponseObject, error) {
	definitions, err := s.Digests.List(ctx)
	if err != nil {
		failure := applicationProblem(err, problemInvalidDigest)
		if failure == nil {
			return nil, err
		}
		return serverhttp.ListDigestsdefaultApplicationProblemPlusJSONResponse(*failure), nil
	}
	items := make([]serverhttp.DigestDefinition, len(definitions))
	for index, definition := range definitions {
		mapped, mapErr := apiDigestDefinition(definition)
		if mapErr != nil {
			failure := applicationProblem(mapErr, problemInvalidDigest)
			if failure == nil {
				return nil, mapErr
			}
			return serverhttp.ListDigestsdefaultApplicationProblemPlusJSONResponse(*failure), nil
		}
		items[index] = mapped
	}
	return serverhttp.ListDigests200JSONResponse{Items: items}, nil
}

// CreateDigest validates request data and creates a Digest Definition.
func (s *Server) CreateDigest(ctx context.Context, request serverhttp.CreateDigestRequestObject) (serverhttp.CreateDigestResponseObject, error) {
	input := *request.Body
	domainInput, err := domainDigestInput(input)
	if err != nil {
		return serverhttp.CreateDigestdefaultApplicationProblemPlusJSONResponse(validationProblem(problemInvalidDigest, err)), nil
	}
	created, err := s.Digests.Create(ctx, domainInput)
	if err != nil {
		failure := applicationProblem(err, problemInvalidDigest)
		if failure == nil {
			return nil, err
		}
		return serverhttp.CreateDigestdefaultApplicationProblemPlusJSONResponse(*failure), nil
	}
	response, err := apiDigestDefinition(created)
	if err != nil {
		failure := applicationProblem(err, problemInvalidDigest)
		if failure == nil {
			return nil, err
		}
		return serverhttp.CreateDigestdefaultApplicationProblemPlusJSONResponse(*failure), nil
	}
	return serverhttp.CreateDigest201JSONResponse{
		Body:    response,
		Headers: serverhttp.CreateDigest201ResponseHeaders{ETag: versionETag(created.Version)},
	}, nil
}

// DeleteDigest applies conditional deletion using the expected version.
func (s *Server) DeleteDigest(ctx context.Context, request serverhttp.DeleteDigestRequestObject) (serverhttp.DeleteDigestResponseObject, error) {
	expected, err := parseVersionETag(request.Params.IfMatch)
	if err != nil {
		return serverhttp.DeleteDigestdefaultApplicationProblemPlusJSONResponse(newProblem(problemVersionPreconditionRequired)), nil
	}
	if err := s.Digests.Delete(ctx, request.DigestId, expected); err != nil {
		failure := applicationProblem(err, problemInvalidDigest)
		if failure == nil {
			return nil, err
		}
		return serverhttp.DeleteDigestdefaultApplicationProblemPlusJSONResponse(*failure), nil
	}
	return serverhttp.DeleteDigest204Response{}, nil
}

// UpdateDigest applies a conditional patch using the expected version.
func (s *Server) UpdateDigest(ctx context.Context, request serverhttp.UpdateDigestRequestObject) (serverhttp.UpdateDigestResponseObject, error) {
	expected, err := parseVersionETag(request.Params.IfMatch)
	if err != nil {
		return serverhttp.UpdateDigestdefaultApplicationProblemPlusJSONResponse(newProblem(problemVersionPreconditionRequired)), nil
	}
	input := *request.Body
	patch, err := domainDigestPatch(input)
	if err != nil {
		return serverhttp.UpdateDigestdefaultApplicationProblemPlusJSONResponse(validationProblem(problemInvalidDigest, err)), nil
	}
	updated, err := s.Digests.Update(ctx, request.DigestId, expected, patch)
	if err != nil {
		failure := applicationProblem(err, problemInvalidDigest)
		if failure == nil {
			return nil, err
		}
		return serverhttp.UpdateDigestdefaultApplicationProblemPlusJSONResponse(*failure), nil
	}
	response, err := apiDigestDefinition(updated)
	if err != nil {
		failure := applicationProblem(err, problemInvalidDigest)
		if failure == nil {
			return nil, err
		}
		return serverhttp.UpdateDigestdefaultApplicationProblemPlusJSONResponse(*failure), nil
	}
	return serverhttp.UpdateDigest200JSONResponse{
		Body:    response,
		Headers: serverhttp.UpdateDigest200ResponseHeaders{ETag: versionETag(updated.Version)},
	}, nil
}

// GenerateDigest enqueues on-demand issue generation and returns the durable Job.
func (s *Server) GenerateDigest(ctx context.Context, request serverhttp.GenerateDigestRequestObject) (serverhttp.GenerateDigestResponseObject, error) {
	job, err := s.Digests.Generate(ctx, request.DigestId, request.Params.IdempotencyKey)
	if err != nil {
		failure := applicationProblem(err, problemInvalidDigest)
		if failure == nil {
			return nil, err
		}
		return serverhttp.GenerateDigestdefaultApplicationProblemPlusJSONResponse(*failure), nil
	}
	return serverhttp.GenerateDigest202JSONResponse{JobId: job.ID}, nil
}
