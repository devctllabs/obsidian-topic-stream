package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/ethernity/obsidian-topic-stream/gen/serverhttp"
	"github.com/ethernity/obsidian-topic-stream/internal/service/events"
)

const eventHeartbeatInterval = 15 * time.Second

// StreamEvents streams best-effort invalidations and heartbeats until cancellation or disconnection.
func (s *Server) StreamEvents(ctx context.Context, request serverhttp.StreamEventsRequestObject) (serverhttp.StreamEventsResponseObject, error) {
	return eventStreamResponse{ctx: ctx, events: s.Events}, nil
}

type eventStreamResponse struct {
	ctx    context.Context
	events *events.Service
}

var _ serverhttp.StreamEventsResponseObject = eventStreamResponse{}

// VisitStreamEventsResponse streams events and releases the subscription when writing ends.
func (r eventStreamResponse) VisitStreamEventsResponse(response http.ResponseWriter) error {
	ctx, cancel := context.WithCancel(r.ctx)
	defer cancel()
	subscription := r.events.Subscribe(ctx)
	responseController := http.NewResponseController(response)
	response.Header().Set("Content-Type", "text/event-stream")
	response.Header().Set("Cache-Control", "no-cache")
	response.Header().Set("Connection", "keep-alive")
	response.WriteHeader(http.StatusOK)
	if _, err := response.Write([]byte(": connected\n\n")); err != nil {
		return err
	}
	if err := responseController.Flush(); err != nil {
		return err
	}
	return streamEventLoop(ctx, response, responseController, subscription)
}

func streamEventLoop(
	ctx context.Context,
	response http.ResponseWriter,
	responseController *http.ResponseController,
	subscription <-chan events.Event,
) error {
	heartbeat := time.NewTicker(eventHeartbeatInterval)
	defer heartbeat.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case event, open := <-subscription:
			if !open {
				return nil
			}
			if err := sendEvent(response, responseController, event); err != nil {
				return err
			}
		case <-heartbeat.C:
			if err := sendHeartbeat(response, responseController); err != nil {
				return err
			}
		}
	}
}

func sendEvent(response http.ResponseWriter, controller *http.ResponseController, event events.Event) error {
	if err := writeEvent(response, event); err != nil {
		return err
	}
	return controller.Flush()
}

func sendHeartbeat(response http.ResponseWriter, controller *http.ResponseController) error {
	if _, err := response.Write([]byte(": heartbeat\n\n")); err != nil {
		return err
	}
	return controller.Flush()
}

func writeEvent(response http.ResponseWriter, event events.Event) error {
	payload, err := json.Marshal(event.Data)
	if err != nil {
		return fmt.Errorf("encode SSE event: %w", err)
	}
	_, err = fmt.Fprintf(response, "event: %s\ndata: %s\n\n", event.Type, payload)
	return err
}
