package httpapi

import (
	"context"

	"github.com/ethernity/obsidian-topic-stream/gen/serverhttp"
)

// GetSystemStatus reports current runtime readiness and configured component checks.
func (s *Server) GetSystemStatus(ctx context.Context, request serverhttp.GetSystemStatusRequestObject) (serverhttp.GetSystemStatusResponseObject, error) {
	snapshot := s.Status.Get(ctx)
	components := make([]serverhttp.ComponentStatus, len(snapshot.Components))
	for index, component := range snapshot.Components {
		components[index] = serverhttp.ComponentStatus{
			Name: component.Name, Status: serverhttp.ComponentStatusStatus(component.State),
		}
		if component.Detail != "" {
			components[index].Detail = &component.Detail
		}
	}
	return serverhttp.GetSystemStatus200JSONResponse{
		Status:      serverhttp.SystemStatusStatus(snapshot.State),
		Version:     snapshot.Version,
		WorkspaceId: snapshot.WorkspaceID,
		Components:  components,
		Now:         snapshot.Now,
	}, nil
}
