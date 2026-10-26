package systemstatus

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
)

var ErrNotConfigured = errors.New("component is not configured")
var errMissingCheck = errors.New("component health check is missing")

// State classifies overall or component health as ready, degraded, or unavailable.
type State string

const (
	StateReady       State = "ready"
	StateDegraded    State = "degraded"
	StateUnavailable State = "unavailable"
)

// Component supplies a named health check and whether its failure prevents readiness.
type Component struct {
	Name string
	// Required turns a failed check into unavailable status; optional failures degrade readiness.
	Required bool
	Check    func(context.Context) error
}

// ComponentSnapshot records the outcome of one configured health check.
type ComponentSnapshot struct {
	Name   string
	State  State
	Detail string
}

// Snapshot reports bounded runtime health, selected Workspace, build version, and observation time.
type Snapshot struct {
	State       State
	Version     string
	WorkspaceID uuid.UUID
	Components  []ComponentSnapshot
	Now         time.Time
}

// Config supplies dependencies and operating settings for the systemstatus service.
type Config struct {
	Version     string
	WorkspaceID uuid.UUID
	Now         func() time.Time
	Timeout     time.Duration
	Components  []Component
}

// Service reports runtime component health with bounded checks.
type Service struct {
	config Config
}

// New binds configured health checks, defaulting their timeout to two seconds when omitted.
func New(config Config) Service {
	if config.Timeout <= 0 {
		config.Timeout = 2 * time.Second
	}
	return Service{config: config}
}

// Get omits unconfigured components, degrades for optional failures, and becomes unavailable for a
// required component failure.
func (s Service) Get(ctx context.Context) Snapshot {
	snapshot := Snapshot{
		State: StateReady, Version: s.config.Version, WorkspaceID: s.config.WorkspaceID,
		Components: make([]ComponentSnapshot, 0, len(s.config.Components)), Now: s.config.Now(),
	}
	for _, component := range s.config.Components {
		status := ComponentSnapshot{Name: component.Name, State: StateReady}
		probeContext, cancel := context.WithTimeout(ctx, s.config.Timeout)
		err := errMissingCheck
		if component.Check != nil {
			err = component.Check(probeContext)
		}
		cancel()
		if errors.Is(err, ErrNotConfigured) {
			continue
		}
		if err != nil {
			status.State = StateUnavailable
			status.Detail = "health check failed"
			if component.Required {
				snapshot.State = StateUnavailable
			} else if snapshot.State == StateReady {
				snapshot.State = StateDegraded
			}
		}
		snapshot.Components = append(snapshot.Components, status)
	}
	return snapshot
}
