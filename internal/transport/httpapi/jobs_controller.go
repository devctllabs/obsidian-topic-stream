package httpapi

import (
	"context"
	"errors"

	"github.com/ethernity/obsidian-topic-stream/gen/serverhttp"
	"github.com/ethernity/obsidian-topic-stream/internal/domain"
	"github.com/ethernity/obsidian-topic-stream/internal/service/jobs"
)

// ListJobs validates pagination and returns Workspace-scoped durable Job state.
func (s *Server) ListJobs(ctx context.Context, request serverhttp.ListJobsRequestObject) (serverhttp.ListJobsResponseObject, error) {
	var status *domain.JobStatus
	if request.Params.Status != nil {
		parsed := domain.JobStatus(*request.Params.Status)
		if !validJobStatus(parsed) {
			return serverhttp.ListJobsdefaultApplicationProblemPlusJSONResponse(newProblem(problemInvalidStatus)), nil
		}
		status = &parsed
	}
	page, err := s.Jobs.List(ctx, jobs.ListRequest{
		WorkspaceID: s.WorkspaceID, Status: status,
		Cursor: valueOr(request.Params.Cursor, ""), Limit: valueOr(request.Params.Limit, 20),
	})
	if errors.Is(err, jobs.ErrInvalid) {
		return serverhttp.ListJobsdefaultApplicationProblemPlusJSONResponse(validationProblem(problemInvalidJobQuery, err)), nil
	}
	if err != nil {
		return nil, err
	}
	items := make([]serverhttp.Job, len(page.Items))
	for index, job := range page.Items {
		items[index] = apiJob(job)
	}
	return serverhttp.ListJobs200JSONResponse{Items: items, NextCursor: page.NextCursor}, nil
}

// GetJob returns a Job only within the selected Workspace.
func (s *Server) GetJob(ctx context.Context, request serverhttp.GetJobRequestObject) (serverhttp.GetJobResponseObject, error) {
	job, err := s.Jobs.GetForWorkspace(ctx, s.WorkspaceID, request.JobId)
	if errors.Is(err, jobs.ErrNotFound) {
		return serverhttp.GetJobdefaultApplicationProblemPlusJSONResponse(newProblem(problemJobNotFound)), nil
	}
	if err != nil {
		return nil, err
	}
	return serverhttp.GetJob200JSONResponse(apiJob(job)), nil
}

// RetryJob retries eligible dead work within the selected Workspace.
func (s *Server) RetryJob(ctx context.Context, request serverhttp.RetryJobRequestObject) (serverhttp.RetryJobResponseObject, error) {
	job, err := s.Jobs.RetryForWorkspace(ctx, s.WorkspaceID, request.JobId)
	if errors.Is(err, jobs.ErrNotFound) {
		return serverhttp.RetryJobdefaultApplicationProblemPlusJSONResponse(newProblem(problemJobNotFound)), nil
	}
	if errors.Is(err, jobs.ErrNotRetryable) {
		return serverhttp.RetryJobdefaultApplicationProblemPlusJSONResponse(newProblem(problemJobNotRetryable)), nil
	}
	if err != nil {
		return nil, err
	}
	return serverhttp.RetryJob202JSONResponse{JobId: job.ID}, nil
}

// CancelJob requests cancellation of durable Workspace work.
func (s *Server) CancelJob(ctx context.Context, request serverhttp.CancelJobRequestObject) (serverhttp.CancelJobResponseObject, error) {
	job, err := s.Jobs.CancelForWorkspace(ctx, s.WorkspaceID, request.JobId)
	if errors.Is(err, jobs.ErrNotFound) {
		return serverhttp.CancelJobdefaultApplicationProblemPlusJSONResponse(newProblem(problemJobNotFound)), nil
	}
	if err != nil {
		return nil, err
	}
	return serverhttp.CancelJob202JSONResponse(apiJob(job)), nil
}

func validJobStatus(status domain.JobStatus) bool {
	switch status {
	case domain.JobStatusQueued, domain.JobStatusRunning, domain.JobStatusRetryWait,
		domain.JobStatusSucceeded, domain.JobStatusDead, domain.JobStatusCancelled:
		return true
	default:
		return false
	}
}
