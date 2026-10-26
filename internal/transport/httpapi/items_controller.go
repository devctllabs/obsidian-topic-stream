package httpapi

import (
	"bytes"
	"context"
	"errors"

	"github.com/ethernity/obsidian-topic-stream/gen/serverhttp"
	"github.com/ethernity/obsidian-topic-stream/internal/domain"
)

// GetItem returns canonical Item content or a problem response.
func (s *Server) GetItem(ctx context.Context, request serverhttp.GetItemRequestObject) (serverhttp.GetItemResponseObject, error) {
	item, err := s.Items.Get(ctx, request.ItemId)
	if err != nil {
		failure := applicationProblem(err, problemInvalidItemState)
		if failure == nil {
			return nil, err
		}
		return serverhttp.GetItemdefaultApplicationProblemPlusJSONResponse(*failure), nil
	}
	response, err := apiItem(item)
	if err != nil {
		return nil, err
	}
	return serverhttp.GetItem200JSONResponse(response), nil
}

// SetItemFeedback validates and stores current personal feedback.
func (s *Server) SetItemFeedback(ctx context.Context, request serverhttp.SetItemFeedbackRequestObject) (serverhttp.SetItemFeedbackResponseObject, error) {
	input := *request.Body
	feedback, err := s.Items.SetFeedback(ctx, request.ItemId, string(input.Sentiment), input.Comment)
	if err != nil {
		if pending := projectionPending(err); pending != nil {
			return serverhttp.SetItemFeedback503ApplicationProblemPlusJSONResponse{
				ProjectionPendingApplicationProblemPlusJSONResponse: serverhttp.ProjectionPendingApplicationProblemPlusJSONResponse(*pending),
			}, nil
		}
		failure := applicationProblem(err, problemInvalidItemState)
		if failure == nil {
			return nil, err
		}
		return serverhttp.SetItemFeedbackdefaultApplicationProblemPlusJSONResponse(*failure), nil
	}
	return serverhttp.SetItemFeedback200JSONResponse(apiFeedback(feedback)), nil
}

// ClearItemFeedback removes current personal feedback from the Item.
func (s *Server) ClearItemFeedback(ctx context.Context, request serverhttp.ClearItemFeedbackRequestObject) (serverhttp.ClearItemFeedbackResponseObject, error) {
	if err := s.Items.ClearFeedback(ctx, request.ItemId); err != nil {
		if pending := projectionPending(err); pending != nil {
			return serverhttp.ClearItemFeedback503ApplicationProblemPlusJSONResponse{
				ProjectionPendingApplicationProblemPlusJSONResponse: serverhttp.ProjectionPendingApplicationProblemPlusJSONResponse(*pending),
			}, nil
		}
		failure := applicationProblem(err, problemInvalidItemState)
		if failure == nil {
			return nil, err
		}
		return serverhttp.ClearItemFeedbackdefaultApplicationProblemPlusJSONResponse(*failure), nil
	}
	return serverhttp.ClearItemFeedback204Response{}, nil
}

// SetItemReadingState validates and updates the Item's read or unread state.
func (s *Server) SetItemReadingState(ctx context.Context, request serverhttp.SetItemReadingStateRequestObject) (serverhttp.SetItemReadingStateResponseObject, error) {
	input := *request.Body
	if err := s.Items.SetReadingState(ctx, request.ItemId, domain.ReadingState(input.State)); err != nil {
		failure := applicationProblem(err, problemInvalidItemState)
		if failure == nil {
			return nil, err
		}
		return serverhttp.SetItemReadingStatedefaultApplicationProblemPlusJSONResponse(*failure), nil
	}
	return serverhttp.SetItemReadingState204Response{}, nil
}

// SetItemVisibility validates and updates visibility independently of personal preference.
func (s *Server) SetItemVisibility(ctx context.Context, request serverhttp.SetItemVisibilityRequestObject) (serverhttp.SetItemVisibilityResponseObject, error) {
	input := *request.Body
	if err := s.Items.SetVisibility(ctx, request.ItemId, domain.ItemVisibility(input.Visibility)); err != nil {
		failure := applicationProblem(err, problemInvalidItemState)
		if failure == nil {
			return nil, err
		}
		return serverhttp.SetItemVisibilitydefaultApplicationProblemPlusJSONResponse(*failure), nil
	}
	return serverhttp.SetItemVisibility204Response{}, nil
}

// SetItemTopicCorrection applies an explicit Topic membership judgment.
func (s *Server) SetItemTopicCorrection(ctx context.Context, request serverhttp.SetItemTopicCorrectionRequestObject) (serverhttp.SetItemTopicCorrectionResponseObject, error) {
	input := *request.Body
	if err := s.Items.SetTopicCorrection(
		ctx, request.ItemId, input.TopicId, domain.TopicMembership(input.Membership),
	); err != nil {
		if pending := projectionPending(err); pending != nil {
			return serverhttp.SetItemTopicCorrection503ApplicationProblemPlusJSONResponse{
				ProjectionPendingApplicationProblemPlusJSONResponse: serverhttp.ProjectionPendingApplicationProblemPlusJSONResponse(*pending),
			}, nil
		}
		failure := applicationProblem(err, problemInvalidItemState)
		if failure == nil {
			return nil, err
		}
		return serverhttp.SetItemTopicCorrectiondefaultApplicationProblemPlusJSONResponse(*failure), nil
	}
	return serverhttp.SetItemTopicCorrection204Response{}, nil
}

// DownloadItemImage serves a validated image referenced by canonical Item content.
func (s *Server) DownloadItemImage(ctx context.Context, request serverhttp.DownloadItemImageRequestObject) (serverhttp.DownloadItemImageResponseObject, error) {
	download, err := s.Items.DownloadImage(ctx, request.ItemId, request.ImageId)
	if err != nil {
		failure := applicationProblem(err, problemInvalidItemState)
		if failure == nil {
			return nil, err
		}
		return serverhttp.DownloadItemImagedefaultApplicationProblemPlusJSONResponse(*failure), nil
	}
	return serverhttp.DownloadItemImage200ImageResponse{
		Body: bytes.NewReader(download.Bytes), ContentType: download.ContentType,
		Headers: serverhttp.DownloadItemImage200ResponseHeaders{XContentTypeOptions: "nosniff"},
	}, nil
}

func projectionPending(err error) *serverhttp.ProjectionPendingProblem {
	var pending *domain.ProjectionPendingError
	if !errors.As(err, &pending) {
		return nil
	}
	detail := "The canonical change was saved, but its retrieval projection is still pending."
	return &serverhttp.ProjectionPendingProblem{
		Type: serverhttp.UrnTopicStreamProblemProjectionPending, Title: serverhttp.RetrievalProjectionPending,
		Status: serverhttp.N503, Code: serverhttp.ProjectionPendingProblemCodeProjectionPending,
		CanonicalCommitted: serverhttp.True, ProjectionRevision: pending.Revision, Detail: &detail,
	}
}
