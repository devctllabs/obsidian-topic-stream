package httpapi

import (
	"context"

	"github.com/ethernity/obsidian-topic-stream/gen/serverhttp"
	"github.com/ethernity/obsidian-topic-stream/internal/service/search"
)

// SearchItems validates filters and returns a bounded canonical Item search page.
func (s *Server) SearchItems(ctx context.Context, request serverhttp.SearchItemsRequestObject) (serverhttp.SearchItemsResponseObject, error) {
	query, err := searchRequest(request.Params)
	if err != nil {
		return serverhttp.SearchItemsdefaultApplicationProblemPlusJSONResponse(validationProblem(problemInvalidSearchRequest, err)), nil
	}
	page, err := s.Search.Find(ctx, query)
	if err != nil {
		failure := applicationProblem(err, problemInvalidSearchRequest)
		if failure == nil {
			return nil, err
		}
		return serverhttp.SearchItemsdefaultApplicationProblemPlusJSONResponse(*failure), nil
	}
	response, err := searchPage(page)
	if err != nil {
		return nil, err
	}
	return serverhttp.SearchItems200JSONResponse(response), nil
}

func searchRequest(params serverhttp.SearchItemsParams) (search.Request, error) {
	mode := search.Mode("")
	if params.Mode != nil {
		mode = search.Mode(*params.Mode)
	}
	return search.Request{
		Query: valueOr(params.Q, ""), Mode: mode, Limit: valueOr(params.Limit, 20),
		Cursor: valueOr(params.Cursor, ""), TopicID: params.TopicId, SourceID: params.SourceId,
		PublishedAfter: params.PublishedAfter, PublishedBefore: params.PublishedBefore,
	}, nil
}
