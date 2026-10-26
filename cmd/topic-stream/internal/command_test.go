package command_test

import (
	"context"
	"errors"
	"io"
	"testing"
	"time"

	"github.com/devctllabs/go-libs/lifecycle"
	command "github.com/ethernity/obsidian-topic-stream/cmd/topic-stream/internal"
	"github.com/ethernity/obsidian-topic-stream/cmd/topic-stream/internal/mocks"
	"github.com/ethernity/obsidian-topic-stream/internal/domain"
	benchservice "github.com/ethernity/obsidian-topic-stream/internal/service/bench"
	"github.com/stretchr/testify/require"
	"github.com/urfave/cli/v3"
	"go.uber.org/mock/gomock"
)

func cleanShutdown(context.Context) error { return nil }

type brokenWriter struct{ err error }

// Write rejects the write with the configured fixture error.
func (w brokenWriter) Write([]byte) (int, error) { return 0, w.err }

func runCommand(ctx context.Context, leaf *cli.Command, args ...string) error {
	root := &cli.Command{Name: "topic-stream", Writer: io.Discard, ErrWriter: io.Discard, Commands: []*cli.Command{leaf}}
	return root.Run(ctx, append([]string{"topic-stream", leaf.Name}, args...))
}

func TestCommandsHelpAndInvalidInputSkipBuild(t *testing.T) {
	for _, args := range [][]string{{"--help"}, {"--unknown"}, {"--train-percent", "0"}, {"--since", "yesterday"}} {
		t.Run("bench/"+args[0]+args[len(args)-1], func(t *testing.T) {
			factory := mocks.NewMockBenchDepsFactory(gomock.NewController(t))
			err := runCommand(t.Context(), command.NewBenchCommand(factory), args...)
			if args[0] == "--help" {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
			}
		})
	}
	for _, name := range []string{"serve", "reindex"} {
		t.Run(name, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			var leaf *cli.Command
			if name == "serve" {
				leaf = command.NewServeCommand(mocks.NewMockServeDepsFactory(ctrl))
			} else {
				leaf = command.NewReindexCommand(mocks.NewMockReindexDepsFactory(ctrl))
			}
			require.NoError(t, runCommand(t.Context(), leaf, "--help"))
		})
	}
}

func TestBenchCleanupPreservesFailures(t *testing.T) {
	operationErr, outputErr, shutdownErr := errors.New("evaluate failed"), errors.New("write failed"), errors.New("close failed")
	for _, scenario := range []string{"success", "operation", "output"} {
		t.Run(scenario, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			factory, evaluator := mocks.NewMockBenchDepsFactory(ctrl), mocks.NewMockBenchEvaluator(ctrl)
			var called int
			factory.EXPECT().Build(gomock.Any(), command.BenchCommandOptions{TrainPercent: 80}).Return(&command.BenchDeps{
				Evaluator: evaluator, Shutdown: func(ctx context.Context) error {
					require.NoError(t, ctx.Err())
					_, bounded := ctx.Deadline()
					require.True(t, bounded)
					called++
					return shutdownErr
				},
			}, nil)
			var evalErr error
			if scenario == "operation" {
				evalErr = operationErr
			}
			evaluator.EXPECT().Evaluate(gomock.Any(), benchservice.Options{TrainPercent: 80}).Return(benchservice.Report{}, evalErr)
			writer := io.Discard
			if scenario == "output" {
				writer = brokenWriter{outputErr}
			}
			root := &cli.Command{Name: "topic-stream", Writer: writer, Commands: []*cli.Command{command.NewBenchCommand(factory)}}
			err := root.Run(t.Context(), []string{"topic-stream", "bench"})
			require.ErrorIs(t, err, shutdownErr)
			require.Equal(t, 1, called)
			if evalErr != nil {
				require.ErrorIs(t, err, operationErr)
			}
			if scenario == "output" {
				require.ErrorIs(t, err, outputErr)
			}
		})
	}
}

func TestCommandBuildFailures(t *testing.T) {
	failure := errors.New("build failed")
	ctrl := gomock.NewController(t)
	bench := mocks.NewMockBenchDepsFactory(ctrl)
	bench.EXPECT().Build(gomock.Any(), gomock.Any()).Return(nil, failure)
	require.ErrorIs(t, runCommand(t.Context(), command.NewBenchCommand(bench)), failure)
	reindex := mocks.NewMockReindexDepsFactory(ctrl)
	reindex.EXPECT().Build(gomock.Any()).Return(nil, failure)
	require.ErrorIs(t, runCommand(t.Context(), command.NewReindexCommand(reindex)), failure)
	serve := mocks.NewMockServeDepsFactory(ctrl)
	serve.EXPECT().Build(gomock.Any()).Return(nil, failure)
	require.ErrorIs(t, runCommand(t.Context(), command.NewServeCommand(serve)), failure)
}

func TestReindexCancellationStillCleansUp(t *testing.T) {
	ctrl := gomock.NewController(t)
	factory, reindexer := mocks.NewMockReindexDepsFactory(ctrl), mocks.NewMockReindexer(ctrl)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	closed := false
	factory.EXPECT().Build(gomock.Any()).Return(&command.ReindexDeps{
		Reindexer: reindexer, Shutdown: func(cleanup context.Context) error {
			require.NoError(t, cleanup.Err())
			closed = true
			return nil
		},
	}, nil)
	reindexer.EXPECT().Rebuild(gomock.Any()).DoAndReturn(func(operation context.Context) (domain.Job, error) {
		require.Equal(t, ctx.Done(), operation.Done())
		cancel()
		return domain.Job{}, ctx.Err()
	})
	require.ErrorIs(t, runCommand(ctx, command.NewReindexCommand(factory)), context.Canceled)
	require.True(t, closed)
}

func TestServeRunsTasksAndShutsDownOnCancellation(t *testing.T) {
	factory := mocks.NewMockServeDepsFactory(gomock.NewController(t))
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	closed := false
	factory.EXPECT().Build(gomock.Any()).Return(&command.ServeDeps{
		Tasks:           []lifecycle.Task{{Name: "test", Run: func(taskCtx context.Context) error { cancel(); <-taskCtx.Done(); return taskCtx.Err() }}},
		ShutdownTimeout: time.Second,
		Shutdown:        func(cleanup context.Context) error { require.NoError(t, cleanup.Err()); closed = true; return nil },
	}, nil)
	require.NoError(t, runCommand(ctx, command.NewServeCommand(factory)))
	require.True(t, closed)
}
