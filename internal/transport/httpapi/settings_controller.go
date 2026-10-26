package httpapi

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/ethernity/obsidian-topic-stream/gen/serverhttp"
	"github.com/ethernity/obsidian-topic-stream/internal/domain"
	"github.com/ethernity/obsidian-topic-stream/internal/service/settings"
)

// GetWorkspaceSettings returns non-secret settings and their version ETag.
func (s *Server) GetWorkspaceSettings(ctx context.Context, request serverhttp.GetWorkspaceSettingsRequestObject) (serverhttp.GetWorkspaceSettingsResponseObject, error) {
	value, err := s.Settings.GetWorkspace(ctx)
	if err != nil {
		return nil, err
	}
	return serverhttp.GetWorkspaceSettings200JSONResponse{
		Body:    apiWorkspaceSettings(value),
		Headers: serverhttp.GetWorkspaceSettings200ResponseHeaders{ETag: versionETag(value.Version)},
	}, nil
}

// UpdateWorkspaceSettings applies a conditional settings patch using the expected version.
func (s *Server) UpdateWorkspaceSettings(ctx context.Context, request serverhttp.UpdateWorkspaceSettingsRequestObject) (serverhttp.UpdateWorkspaceSettingsResponseObject, error) {
	expected, err := parseVersionETag(request.Params.IfMatch)
	if err != nil {
		return serverhttp.UpdateWorkspaceSettingsdefaultApplicationProblemPlusJSONResponse(newProblem(problemVersionPreconditionRequired)), nil
	}
	input := *request.Body
	value, err := s.Settings.UpdateWorkspace(ctx, expected, workspaceSettingsPatch(input))
	if errors.Is(err, settings.ErrConflict) {
		return serverhttp.UpdateWorkspaceSettingsdefaultApplicationProblemPlusJSONResponse(newProblem(problemSettingsChanged)), nil
	}
	if errors.Is(err, settings.ErrInvalid) {
		return serverhttp.UpdateWorkspaceSettingsdefaultApplicationProblemPlusJSONResponse(validationProblem(problemInvalidSettings, err)), nil
	}
	if err != nil {
		return nil, err
	}
	return serverhttp.UpdateWorkspaceSettings200JSONResponse{
		Body:    apiWorkspaceSettings(value),
		Headers: serverhttp.UpdateWorkspaceSettings200ResponseHeaders{ETag: versionETag(value.Version)},
	}, nil
}

// GetPreferenceProfile returns current guidance and its version ETag.
func (s *Server) GetPreferenceProfile(ctx context.Context, request serverhttp.GetPreferenceProfileRequestObject) (serverhttp.GetPreferenceProfileResponseObject, error) {
	value, err := s.Settings.GetPreferenceProfile(ctx)
	if err != nil {
		return nil, err
	}
	return serverhttp.GetPreferenceProfile200JSONResponse{
		Body:    apiPreferenceProfile(value),
		Headers: serverhttp.GetPreferenceProfile200ResponseHeaders{ETag: versionETag(value.Version)},
	}, nil
}

// UpdatePreferenceProfile applies a conditional guidance patch using the expected version.
func (s *Server) UpdatePreferenceProfile(ctx context.Context, request serverhttp.UpdatePreferenceProfileRequestObject) (serverhttp.UpdatePreferenceProfileResponseObject, error) {
	expected, err := parseVersionETag(request.Params.IfMatch)
	if err != nil {
		return serverhttp.UpdatePreferenceProfiledefaultApplicationProblemPlusJSONResponse(newProblem(problemVersionPreconditionRequired)), nil
	}
	input := *request.Body
	value, err := s.Settings.UpdatePreferenceProfile(ctx, expected, domain.PreferenceProfilePatch{Prefer: input.Prefer, Avoid: input.Avoid})
	if errors.Is(err, settings.ErrConflict) {
		return serverhttp.UpdatePreferenceProfiledefaultApplicationProblemPlusJSONResponse(newProblem(problemPreferenceProfileChanged)), nil
	}
	if errors.Is(err, settings.ErrInvalid) {
		return serverhttp.UpdatePreferenceProfiledefaultApplicationProblemPlusJSONResponse(validationProblem(problemInvalidPreferenceProfile, err)), nil
	}
	if err != nil {
		return nil, err
	}
	return serverhttp.UpdatePreferenceProfile200JSONResponse{
		Body:    apiPreferenceProfile(value),
		Headers: serverhttp.UpdatePreferenceProfile200ResponseHeaders{ETag: versionETag(value.Version)},
	}, nil
}

func parseVersionETag(value string) (int, error) {
	if len(value) < 3 || !strings.HasPrefix(value, `"`) || !strings.HasSuffix(value, `"`) {
		return 0, errors.New("If-Match must contain the quoted resource version")
	}
	version, err := strconv.Atoi(strings.Trim(value, `"`))
	if err != nil || version < 1 {
		return 0, errors.New("If-Match must contain a positive integer version")
	}
	return version, nil
}

func versionETag(version int) *string {
	value := fmt.Sprintf(`"%d"`, version)
	return &value
}

func workspaceSettingsPatch(input serverhttp.WorkspaceSettingsPatch) domain.WorkspaceSettingsPatch {
	patch := domain.WorkspaceSettingsPatch{}
	if input.EmbeddingModelId != nil {
		patch.EmbeddingModelID = input.EmbeddingModelId
	}
	if input.Generation != nil {
		value := domainGeneration(*input.Generation)
		patch.Generation = &value
	}
	if input.Summary != nil {
		value := domainSummary(*input.Summary)
		patch.Summary = &value
	}
	if input.Instructions != nil {
		value := domainInstructions(*input.Instructions)
		patch.Instructions = &value
	}
	if input.Retention != nil {
		value := domain.RetentionSettings{FullContentDays: input.Retention.FullContentDays}
		patch.Retention = &value
	}
	if input.Retrieval != nil {
		value := domainRetrieval(*input.Retrieval)
		patch.Retrieval = &value
	}
	return patch
}
