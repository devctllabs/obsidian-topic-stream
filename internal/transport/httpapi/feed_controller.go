package httpapi

import (
	"context"

	"github.com/ethernity/obsidian-topic-stream/gen/serverhttp"
	"github.com/ethernity/obsidian-topic-stream/internal/domain"
)

//go:generate go tool mockgen -source=feed_controller.go -destination=mocks/feed.gen.go -package=mocks

// Feed exposes saved history and explicit publication to the HTTP boundary.
type Feed interface {
	// Read returns saved publications without running candidate preparation or ranking.
	Read(ctx context.Context, query domain.FeedReadQuery) (domain.FeedPage, error)
	// Refresh publishes one bounded batch and returns the latest saved page without waiting for preparation.
	Refresh(ctx context.Context, command domain.FeedRefreshCommand) (domain.FeedPage, error)
}

// RefreshFeed validates an explicit refresh, publishes a bounded batch, and returns saved history.
func (s *Server) RefreshFeed(ctx context.Context, request serverhttp.RefreshFeedRequestObject) (serverhttp.RefreshFeedResponseObject, error) {
	input := *request.Body
	limit := valueOr(input.Limit, 25)
	page, err := s.Feed.Refresh(ctx, domain.FeedRefreshCommand{
		Limit: limit, TopicID: input.TopicId,
	})
	response, failure, internal := mapFeedResponse(page, err)
	if internal != nil {
		return nil, internal
	}
	if failure != nil {
		return serverhttp.RefreshFeeddefaultApplicationProblemPlusJSONResponse(*failure), nil
	}
	return serverhttp.RefreshFeed200JSONResponse(response), nil
}

// GetFeed reads a saved Feed page under a fixed shared head without running ranking.
func (s *Server) GetFeed(ctx context.Context, request serverhttp.GetFeedRequestObject) (serverhttp.GetFeedResponseObject, error) {
	params := request.Params
	limit := valueOr(params.Limit, 25)
	page, err := s.Feed.Read(ctx, domain.FeedReadQuery{
		Limit: limit, Cursor: valueOr(params.Cursor, ""),
		Snapshot: valueOr(params.Snapshot, ""), TopicID: params.TopicId,
	})
	response, failure, internal := mapFeedResponse(page, err)
	if internal != nil {
		return nil, internal
	}
	if failure != nil {
		return serverhttp.GetFeeddefaultApplicationProblemPlusJSONResponse(*failure), nil
	}
	return serverhttp.GetFeed200JSONResponse(response), nil
}

func mapFeedResponse(page domain.FeedPage, err error) (serverhttp.FeedPage, *problemResponse, error) {
	if err != nil {
		failure := applicationProblem(err, problemInvalidFeedRequest)
		if failure == nil {
			return serverhttp.FeedPage{}, nil, err
		}
		return serverhttp.FeedPage{}, failure, nil
	}
	response, err := feedPage(page)
	return response, nil, err
}
