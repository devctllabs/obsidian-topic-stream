package command

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/devctllabs/go-libs/lifecycle"
	"github.com/ethernity/obsidian-topic-stream/internal/deps"
	"github.com/ethernity/obsidian-topic-stream/internal/domain"
	"github.com/urfave/cli/v3"
)

//go:generate go tool mockgen -source=reindex.go -destination=mocks/reindex.go -package=mocks

// Reindexer submits a rebuild while preserving the serving Embedding Model.
type Reindexer interface {
	// Rebuild enqueues a durable rebuild of the active retrieval generation.
	Rebuild(ctx context.Context) (domain.Job, error)
}

// ReindexDeps transfers rebuild submission and its cleanup to one command invocation.
type ReindexDeps struct {
	Reindexer Reindexer
	Shutdown  func(ctx context.Context) error
}

// ReindexDepsFactory acquires command dependencies and transfers their cleanup to the caller.
type ReindexDepsFactory interface {
	// Build returns ready reindex dependencies or releases partial resources on failure.
	Build(ctx context.Context) (*ReindexDeps, error)
}

// DefaultReindexDepsFactory acquires configured dependencies for durable rebuild submission.
type DefaultReindexDepsFactory struct{}

// Build acquires the command runtime and transfers its shutdown function to the caller.
func (DefaultReindexDepsFactory) Build(ctx context.Context) (*ReindexDeps, error) {
	runtime, err := deps.NewReindex(ctx)
	if err != nil {
		return nil, err
	}
	return &ReindexDeps{Reindexer: runtime, Shutdown: runtime.Shutdown}, nil
}

type reindexCommand struct{ factory ReindexDepsFactory }

// NewReindexCommand constructs durable rebuild submission with command-owned cleanup.
func NewReindexCommand(factory ReindexDepsFactory) *cli.Command {
	x := &reindexCommand{factory: factory}
	return &cli.Command{
		Name:   "reindex",
		Usage:  "enqueue a rebuild of the active retrieval index",
		Action: x.run,
	}
}

func (c *reindexCommand) run(ctx context.Context, command *cli.Command) (resultErr error) {
	runtime, err := c.factory.Build(ctx)
	if err != nil {
		return err
	}
	defer func() {
		resultErr = errors.Join(resultErr, lifecycle.Shutdown(ctx, 10*time.Second, runtime.Shutdown))
	}()
	job, err := runtime.Reindexer.Rebuild(ctx)
	if err != nil {
		return fmt.Errorf("enqueue retrieval rebuild: %w", err)
	}
	_, err = fmt.Fprintf(command.Root().Writer, "queued reindex job %s\n", job.ID)
	return err
}
