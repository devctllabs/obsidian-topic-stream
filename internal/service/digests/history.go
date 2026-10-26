package digests

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/ethernity/obsidian-topic-stream/internal/domain"
	"github.com/google/uuid"
)

// IssueListRequest selects saved Issue summaries with an optional Digest filter.
type IssueListRequest struct {
	DigestID *uuid.UUID
	Cursor   string
	Limit    int
}

// IssuePage contains a bounded history page and its continuation.
type IssuePage struct {
	Items      []domain.DigestIssueSummary
	NextCursor *string
}

type issueCursor struct {
	Version     int
	WorkspaceID uuid.UUID
	DigestID    *uuid.UUID
	After       domain.DigestIssueCursor
}

// ListIssues validates pagination and reads summaries without immutable Item snapshots.
func (s *Service) ListIssues(ctx context.Context, request IssueListRequest) (IssuePage, error) {
	after, err := s.decodeIssueCursor(request)
	if failure := domain.JoinValidation(domain.CheckRange("limit", float64(request.Limit), 1, 100),
		domain.CheckCondition("cursor", domain.ValidationFormat, err == nil)); failure != nil {
		return IssuePage{}, fmt.Errorf("%w: %w", ErrInvalid, failure)
	}
	result, err := s.repository.ListDigestIssues(ctx, domain.DigestIssueList{
		WorkspaceID: s.workspaceID, DigestID: request.DigestID, Cursor: after, Limit: request.Limit + 1,
	})
	if err != nil {
		return IssuePage{}, fmt.Errorf("list digest issues: %w", err)
	}
	page := IssuePage{Items: result}
	if len(result) > request.Limit {
		page.Items = result[:request.Limit]
		last := page.Items[len(page.Items)-1]
		payload, _ := json.Marshal(issueCursor{Version: 1, WorkspaceID: s.workspaceID, DigestID: request.DigestID,
			After: domain.DigestIssueCursor{GeneratedAt: last.GeneratedAt, ID: last.ID}})
		next := base64.RawURLEncoding.EncodeToString(payload)
		page.NextCursor = &next
	}
	return page, nil
}

func (s *Service) decodeIssueCursor(request IssueListRequest) (*domain.DigestIssueCursor, error) {
	if request.Cursor == "" {
		return nil, nil
	}
	if len(request.Cursor) > 2048 {
		return nil, errors.New("cursor too long")
	}
	payload, err := base64.RawURLEncoding.DecodeString(request.Cursor)
	if err != nil {
		return nil, err
	}
	var cursor issueCursor
	if err := json.Unmarshal(payload, &cursor); err != nil {
		return nil, err
	}
	if !cursor.valid(s.workspaceID, request.DigestID) {
		return nil, errors.New("invalid cursor")
	}
	return &cursor.After, nil
}

func (cursor issueCursor) valid(workspaceID uuid.UUID, digestID *uuid.UUID) bool {
	if cursor.DigestID == nil {
		if digestID != nil {
			return false
		}
	} else if digestID == nil || *cursor.DigestID != *digestID {
		return false
	}
	return cursor.Version == 1 && cursor.WorkspaceID == workspaceID &&
		cursor.After.ID != uuid.Nil && !cursor.After.GeneratedAt.IsZero()
}
