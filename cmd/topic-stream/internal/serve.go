package command

import (
	"context"
	"time"

	"github.com/devctllabs/go-libs/lifecycle"
	"github.com/ethernity/obsidian-topic-stream/internal/deps"
	"github.com/urfave/cli/v3"
)

//go:generate go tool mockgen -source=serve.go -destination=mocks/serve.go -package=mocks

// ServeDeps contains selected runtime tasks and their common bounded shutdown owner.
type ServeDeps struct {
	Tasks           []lifecycle.Task
	Shutdown        func(ctx context.Context) error
	ShutdownTimeout time.Duration
}

// ServeDepsFactory acquires command dependencies and transfers their cleanup to the caller.
type ServeDepsFactory interface {
	// Build returns the selected runtime roles and their common shutdown owner.
	Build(ctx context.Context) (*ServeDeps, error)
}

// DefaultServeDepsFactory builds daemon roles with the supplied application version.
type DefaultServeDepsFactory struct{ Version string }

// Build acquires the command runtime and transfers its shutdown function to the caller.
func (f DefaultServeDepsFactory) Build(ctx context.Context) (*ServeDeps, error) {
	runtime, timeout, err := deps.NewServe(ctx, f.Version)
	if err != nil {
		return nil, err
	}
	return &ServeDeps{Tasks: runtime.Tasks(), Shutdown: runtime.Shutdown, ShutdownTimeout: timeout}, nil
}

type serveCommand struct{ factory ServeDepsFactory }

// NewServeCommand constructs daemon startup with the supplied dependency factory.
func NewServeCommand(factory ServeDepsFactory) *cli.Command {
	x := &serveCommand{factory: factory}
	return &cli.Command{Name: "serve", Usage: "run the HTTP API and background workers", Action: x.run}
}

func (c *serveCommand) run(ctx context.Context, _ *cli.Command) error {
	runtime, err := c.factory.Build(ctx)
	if err != nil {
		return err
	}
	return lifecycle.Run(ctx, lifecycle.Config{
		Tasks: runtime.Tasks, Shutdown: runtime.Shutdown, ShutdownTimeout: runtime.ShutdownTimeout,
	})
}
