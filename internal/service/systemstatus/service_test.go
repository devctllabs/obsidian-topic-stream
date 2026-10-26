package systemstatus_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ethernity/obsidian-topic-stream/internal/service/systemstatus"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestStatusReportsReadyComponents(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, time.September, 27, 13, 0, 0, 0, time.UTC)
	service := systemstatus.New(systemstatus.Config{
		Version: "0.1.0-test", WorkspaceID: uuid.New(), Now: func() time.Time { return now },
		Components: []systemstatus.Component{
			{Name: "postgres", Required: true, Check: successfulProbe},
			{Name: "qdrant", Check: successfulProbe},
		},
	})

	snapshot := service.Get(t.Context())

	require.Equal(t, systemstatus.StateReady, snapshot.State)
	require.Equal(t, []systemstatus.ComponentSnapshot{
		{Name: "postgres", State: systemstatus.StateReady},
		{Name: "qdrant", State: systemstatus.StateReady},
	}, snapshot.Components)
}

func TestOptionalComponentFailureDegradesWithoutLeakingError(t *testing.T) {
	t.Parallel()
	service := systemstatus.New(systemstatus.Config{
		Version: "test", WorkspaceID: uuid.New(), Now: time.Now,
		Components: []systemstatus.Component{
			{Name: "postgres", Required: true, Check: successfulProbe},
			{Name: "qdrant", Check: func(context.Context) error { return errors.New("dial secret.internal:6334") }},
		},
	})

	snapshot := service.Get(t.Context())

	require.Equal(t, systemstatus.StateDegraded, snapshot.State)
	require.Equal(t, systemstatus.StateUnavailable, snapshot.Components[1].State)
	require.Equal(t, "health check failed", snapshot.Components[1].Detail)
	require.NotContains(t, snapshot.Components[1].Detail, "secret.internal")
}

func TestRequiredComponentFailureMakesServiceUnavailable(t *testing.T) {
	t.Parallel()
	service := systemstatus.New(systemstatus.Config{
		Version: "test", WorkspaceID: uuid.New(), Now: time.Now,
		Components: []systemstatus.Component{
			{Name: "postgres", Required: true, Check: func(context.Context) error { return errors.New("down") }},
			{Name: "qdrant", Check: successfulProbe},
		},
	})

	snapshot := service.Get(t.Context())

	require.Equal(t, systemstatus.StateUnavailable, snapshot.State)
}

func TestNotConfiguredComponentIsOmittedWithoutDegrading(t *testing.T) {
	t.Parallel()

	service := systemstatus.New(systemstatus.Config{
		Version: "test", WorkspaceID: uuid.New(), Now: time.Now,
		Components: []systemstatus.Component{
			{Name: "generation", Check: func(context.Context) error { return systemstatus.ErrNotConfigured }},
		},
	})

	snapshot := service.Get(t.Context())

	require.Equal(t, systemstatus.StateReady, snapshot.State)
	require.Empty(t, snapshot.Components)
}

func successfulProbe(context.Context) error { return nil }
