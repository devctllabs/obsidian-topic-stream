package httpapi

import (
	"context"

	"github.com/ethernity/obsidian-topic-stream/gen/serverhttp"
)

// ExportWorkspace serializes portable configuration and current feedback.
func (s *Server) ExportWorkspace(ctx context.Context, request serverhttp.ExportWorkspaceRequestObject) (serverhttp.ExportWorkspaceResponseObject, error) {
	archive, err := s.Portability.Export(ctx)
	if err != nil {
		return nil, err
	}
	output, err := apiWorkspaceArchive(archive)
	if err != nil {
		return nil, err
	}
	return serverhttp.ExportWorkspace200JSONResponse(output), nil
}

// ImportWorkspace validates a portable archive and returns its durable import Job.
func (s *Server) ImportWorkspace(ctx context.Context, request serverhttp.ImportWorkspaceRequestObject) (serverhttp.ImportWorkspaceResponseObject, error) {
	input := *request.Body
	archive, err := domainWorkspaceArchive(input)
	if err != nil {
		return serverhttp.ImportWorkspacedefaultApplicationProblemPlusJSONResponse(validationProblem(problemInvalidWorkspaceArchive, err)), nil
	}
	job, err := s.Portability.Import(ctx, archive)
	if err != nil {
		failure := applicationProblem(err, problemWorkspaceImportFailed)
		if failure == nil {
			return nil, err
		}
		return serverhttp.ImportWorkspacedefaultApplicationProblemPlusJSONResponse(*failure), nil
	}
	return serverhttp.ImportWorkspace202JSONResponse{JobId: job.ID}, nil
}
