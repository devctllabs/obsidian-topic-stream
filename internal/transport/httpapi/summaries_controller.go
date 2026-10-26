package httpapi

import (
	"context"

	"github.com/ethernity/obsidian-topic-stream/gen/serverhttp"
	"github.com/ethernity/obsidian-topic-stream/internal/domain"
)

// GetItemSummary reads an annotation and never starts generation or fetches source content.
func (s *Server) GetItemSummary(ctx context.Context, request serverhttp.GetItemSummaryRequestObject) (serverhttp.GetItemSummaryResponseObject, error) {
	state, err := s.Summaries.Read(ctx, domain.SummarySource{ItemID: request.ItemId, SourceVersion: valueOr(request.Params.SourceVersion, "")})
	if err != nil {
		failure := applicationProblem(err, problemInvalidItemState)
		if failure == nil {
			return nil, err
		}
		return serverhttp.GetItemSummarydefaultApplicationProblemPlusJSONResponse(*failure), nil
	}
	return serverhttp.GetItemSummary200JSONResponse(apiItemSummary(state)), nil
}

// RequestItemSummary starts asynchronous generation only for current server-owned Item input.
func (s *Server) RequestItemSummary(ctx context.Context, request serverhttp.RequestItemSummaryRequestObject) (serverhttp.RequestItemSummaryResponseObject, error) {
	job, err := s.Summaries.Request(ctx, domain.SummarySource{ItemID: request.ItemId})
	if err != nil {
		failure := applicationProblem(err, problemInvalidItemState)
		if failure == nil {
			return nil, err
		}
		return serverhttp.RequestItemSummarydefaultApplicationProblemPlusJSONResponse(*failure), nil
	}
	return serverhttp.RequestItemSummary202JSONResponse{JobId: job.ID}, nil
}

// RequestDigestItemSummary requests the exact saved Item version without changing Issue facts.
func (s *Server) RequestDigestItemSummary(ctx context.Context, request serverhttp.RequestDigestItemSummaryRequestObject) (serverhttp.RequestDigestItemSummaryResponseObject, error) {
	job, err := s.Summaries.Request(ctx, domain.SummarySource{ItemID: request.ItemId, IssueID: &request.IssueId})
	if err != nil {
		failure := applicationProblem(err, problemInvalidItemState)
		if failure == nil {
			return nil, err
		}
		return serverhttp.RequestDigestItemSummarydefaultApplicationProblemPlusJSONResponse(*failure), nil
	}
	return serverhttp.RequestDigestItemSummary202JSONResponse{JobId: job.ID}, nil
}

func apiItemSummary(value domain.ItemSummary) serverhttp.ItemSummary {
	result := serverhttp.ItemSummary{SourceVersion: value.SourceVersion, Supported: value.Supported, CanGenerate: value.CanGenerate, State: serverhttp.ItemSummaryState(value.State), JobId: value.JobID, Failure: value.Failure, Retryable: value.Retryable}
	if value.UnavailableReason != nil {
		reason := serverhttp.ItemSummaryUnavailableReason(*value.UnavailableReason)
		result.UnavailableReason = &reason
	}
	if value.Result != nil {
		annotation := apiAISummary(*value.Result)
		result.Result = &annotation
	}
	return result
}

func apiAISummary(value domain.AISummary) serverhttp.AISummary {
	return serverhttp.AISummary{Id: value.ID, GenerationJobId: value.GenerationJobID, SourceVersion: value.SourceVersion, Text: value.Text, Model: value.Model, Language: value.Language, MaxInputChars: value.MaxInputChars, MaxSummaryCharacters: value.MaxSummaryCharacters, InstructionsVersion: value.InstructionsVersion, AlgorithmVersion: value.AlgorithmVersion, CreatedAt: value.CreatedAt}
}
