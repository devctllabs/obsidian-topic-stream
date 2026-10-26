package command

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"text/tabwriter"
	"time"

	"github.com/devctllabs/go-libs/lifecycle"
	"github.com/ethernity/obsidian-topic-stream/internal/deps"
	benchservice "github.com/ethernity/obsidian-topic-stream/internal/service/bench"
	"github.com/urfave/cli/v3"
)

//go:generate go tool mockgen -source=bench.go -destination=mocks/bench.go -package=mocks

const (
	benchJSONFlag         = "json"
	benchSinceFlag        = "since"
	benchTrainPercentFlag = "train-percent"
)

// BenchEvaluator compares retrieval pipelines against current personal feedback.
type BenchEvaluator interface {
	// Evaluate compares retrieval pipelines using the requested window and split.
	Evaluate(ctx context.Context, opts benchservice.Options) (benchservice.Report, error)
}

// BenchDeps transfers the evaluator and its cleanup to one benchmark command invocation.
type BenchDeps struct {
	Evaluator BenchEvaluator
	Shutdown  func(ctx context.Context) error
}

// BenchDepsFactory acquires command dependencies and transfers their cleanup to the caller.
type BenchDepsFactory interface {
	// Build returns ready benchmark dependencies or releases partial resources on failure.
	Build(ctx context.Context, opts BenchCommandOptions) (*BenchDeps, error)
}

// BenchCommandOptions selects benchmark output, evaluation window, and training split.
type BenchCommandOptions struct {
	JSON         bool
	Since        string
	TrainPercent int
}

type benchCommand struct {
	opts    BenchCommandOptions
	factory BenchDepsFactory
}

// NewBenchCommand constructs the benchmark CLI; dependencies are acquired only when its action runs.
func NewBenchCommand(factory BenchDepsFactory) *cli.Command {
	x := &benchCommand{factory: factory}
	return &cli.Command{
		Name: "bench", Usage: "compare retrieval pipelines against current feedback",
		Flags: []cli.Flag{
			&cli.BoolFlag{Name: benchJSONFlag, Usage: "emit only the versioned JSON report", Destination: &x.opts.JSON},
			&cli.StringFlag{Name: benchSinceFlag, Usage: "include feedback updated at or after RFC3339 timestamp", Destination: &x.opts.Since},
			&cli.IntFlag{
				Name: benchTrainPercentFlag, Usage: "percentage of temporally earliest feedback used for training",
				Value: 80, Destination: &x.opts.TrainPercent,
				Validator: func(value int) error {
					if value < 1 || value > 99 {
						return errors.New("train percent must be between 1 and 99")
					}
					return nil
				},
			},
		},
		Action: x.run,
	}
}

func (c *benchCommand) run(ctx context.Context, command *cli.Command) (resultErr error) {
	since, err := parseSince(c.opts.Since)
	if err != nil {
		return err
	}
	runtime, err := c.factory.Build(ctx, c.opts)
	if err != nil {
		return err
	}
	defer func() {
		resultErr = errors.Join(resultErr, lifecycle.Shutdown(ctx, 10*time.Second, runtime.Shutdown))
	}()
	report, err := runtime.Evaluator.Evaluate(ctx, benchservice.Options{
		Since: since, TrainPercent: c.opts.TrainPercent,
	})
	if err != nil {
		return fmt.Errorf("evaluate retrieval pipelines: %w", err)
	}
	if c.opts.JSON {
		return writeBenchJSON(command.Root().Writer, report)
	}
	return writeBenchTable(command.Root().Writer, report)
}

func parseSince(raw string) (*time.Time, error) {
	if raw == "" {
		return nil, nil
	}
	value, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		return nil, fmt.Errorf("since must be an RFC3339 timestamp: %w", err)
	}
	return &value, nil
}

func writeBenchJSON(destination io.Writer, report benchservice.Report) error {
	encoder := json.NewEncoder(destination)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(report); err != nil {
		return fmt.Errorf("write benchmark JSON: %w", err)
	}
	return nil
}

func writeBenchTable(destination io.Writer, report benchservice.Report) error {
	if _, err := fmt.Fprintf(destination, "%s  train=%d test=%d split=%d%%\n\n",
		report.Version, report.TrainingCount, report.TestCount, report.TrainPercent); err != nil {
		return err
	}
	table := tabwriter.NewWriter(destination, 0, 4, 2, ' ', 0)
	if _, err := fmt.Fprintln(table, "PIPELINE\tPRECISION@10\tRECALL@50\tMRR\tNDCG@10"); err != nil {
		return err
	}
	for _, pipeline := range report.Pipelines {
		metrics := pipeline.Metrics
		if _, err := fmt.Fprintf(table, "%s\t%.4f\t%.4f\t%.4f\t%.4f\n", pipeline.Name,
			metrics.PrecisionAt10, metrics.RecallAt50, metrics.MRR, metrics.NDCGAt10); err != nil {
			return err
		}
	}
	return table.Flush()
}

// DefaultBenchDepsFactory opens benchmark dependencies using operator configuration.
type DefaultBenchDepsFactory struct{}

// Build acquires the command runtime and transfers its shutdown function to the caller.
func (DefaultBenchDepsFactory) Build(ctx context.Context, _ BenchCommandOptions) (*BenchDeps, error) {
	runtime, err := deps.NewBenchFromEnv(ctx)
	if err != nil {
		return nil, err
	}
	return &BenchDeps{Evaluator: runtime, Shutdown: runtime.Shutdown}, nil
}
