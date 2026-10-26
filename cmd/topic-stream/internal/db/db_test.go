package db_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strconv"
	"testing"

	"github.com/ethernity/obsidian-topic-stream/cmd/topic-stream/internal/db"
	"github.com/ethernity/obsidian-topic-stream/cmd/topic-stream/internal/db/mocks"
	"github.com/stretchr/testify/require"
	"github.com/urfave/cli/v3"
	"go.uber.org/mock/gomock"
)

func runLeaf(ctx context.Context, leaf *cli.Command, out io.Writer, args ...string) error {
	root := &cli.Command{Name: "topic-stream", Writer: out, ErrWriter: io.Discard, Commands: []*cli.Command{
		{Name: "db", Commands: []*cli.Command{leaf}},
	}}
	return root.Run(ctx, append([]string{"topic-stream", "db", leaf.Name}, args...))
}

func TestUpAppliesAllByDefault(t *testing.T) {
	ctrl := gomock.NewController(t)
	factory, migrator := mocks.NewMockUpDepsFactory(ctrl), mocks.NewMockUpMigrator(ctrl)
	closed := 0
	factory.EXPECT().Build(gomock.Any(), db.UpCommandOptions{}).Return(&db.UpDeps{
		Migrator: migrator, Shutdown: func(ctx context.Context) error { require.NoError(t, ctx.Err()); closed++; return nil },
	}, nil)
	migrator.EXPECT().Up(gomock.Any(), 0).Return(false, nil)
	version := uint(6)
	migrator.EXPECT().Status(gomock.Any()).Return(db.MigrationStatus{Version: &version}, nil)
	var out bytes.Buffer
	require.NoError(t, runLeaf(t.Context(), db.NewUpCommand(factory), &out))
	require.Contains(t, out.String(), "version=6 dirty=false noChange=false")
	require.Equal(t, 1, closed)
}

func TestUpOptionsAndJSON(t *testing.T) {
	t.Setenv("TOPIC_STREAM_DB_PRIMARY_POSTGRES_DSN", "postgres://environment/db")
	for _, scenario := range []struct {
		name     string
		args     []string
		opts     db.UpCommandOptions
		noChange bool
	}{
		{name: "no-change", args: []string{"--json"}, opts: db.UpCommandOptions{CommonOptions: db.CommonOptions{JSON: true}}, noChange: true},
		{name: "steps-url", args: []string{"--json", "--steps", "2", "--url", "postgres://override/db"}, opts: db.UpCommandOptions{
			CommonOptions: db.CommonOptions{JSON: true, URL: "postgres://override/db", URLSet: true}, Steps: 2, StepsSet: true,
		}},
		{name: "explicit-empty-url", args: []string{"--json", "--url", ""}, opts: db.UpCommandOptions{CommonOptions: db.CommonOptions{JSON: true, URLSet: true}}},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			factory, migrator := mocks.NewMockUpDepsFactory(ctrl), mocks.NewMockUpMigrator(ctrl)
			factory.EXPECT().Build(gomock.Any(), scenario.opts).Return(&db.UpDeps{Migrator: migrator, Shutdown: cleanShutdown}, nil)
			migrator.EXPECT().Up(gomock.Any(), scenario.opts.Steps).Return(scenario.noChange, nil)
			version := uint(6)
			migrator.EXPECT().Status(gomock.Any()).Return(db.MigrationStatus{Version: &version}, nil)
			var out bytes.Buffer
			require.NoError(t, runLeaf(t.Context(), db.NewUpCommand(factory), &out, scenario.args...))
			expected := `{"version":6,"dirty":false,"noChange":false}`
			if scenario.noChange {
				expected = `{"version":6,"dirty":false,"noChange":true}`
			}
			require.JSONEq(t, expected, out.String())
		})
	}
}

func TestDownDefaultsAndSteps(t *testing.T) {
	for _, steps := range []int{1, 2} {
		t.Run(strconv.Itoa(steps), func(t *testing.T) {
			ctrl := gomock.NewController(t)
			factory, migrator := mocks.NewMockDownDepsFactory(ctrl), mocks.NewMockDownMigrator(ctrl)
			opts := db.DownCommandOptions{Steps: steps, CommonOptions: db.CommonOptions{JSON: true}}
			args := []string{"--json"}
			if steps == 2 {
				args = append(args, "--steps", "2")
				opts.StepsSet = true
			}
			factory.EXPECT().Build(gomock.Any(), opts).Return(&db.DownDeps{Migrator: migrator, Shutdown: cleanShutdown}, nil)
			migrator.EXPECT().Down(gomock.Any(), steps).Return(false, nil)
			migrator.EXPECT().Status(gomock.Any()).Return(db.MigrationStatus{}, nil)
			var out bytes.Buffer
			require.NoError(t, runLeaf(t.Context(), db.NewDownCommand(factory), &out, args...))
			require.JSONEq(t, `{"version":null,"dirty":false,"noChange":false}`, out.String())
		})
	}
}

func TestStatusUnsetAndDirtyAreSuccessful(t *testing.T) {
	version := uint(3)
	for _, state := range []db.MigrationStatus{{}, {Version: &version, Dirty: true}} {
		t.Run(map[bool]string{true: "dirty", false: "unset"}[state.Dirty], func(t *testing.T) {
			ctrl := gomock.NewController(t)
			factory := mocks.NewMockStatusDepsFactory(ctrl)
			// The up mock also implements the narrow status reader contract.
			reader := mocks.NewMockUpMigrator(ctrl)
			factory.EXPECT().Build(gomock.Any(), db.StatusCommandOptions{CommonOptions: db.CommonOptions{JSON: true}}).Return(
				&db.StatusDeps{Migrator: reader, Shutdown: cleanShutdown}, nil,
			)
			reader.EXPECT().Status(gomock.Any()).Return(state, nil)
			var out bytes.Buffer
			require.NoError(t, runLeaf(t.Context(), db.NewStatusCommand(factory), &out, "--json"))
			want := `{"version":null,"dirty":false}`
			if state.Dirty {
				want = `{"version":3,"dirty":true}`
			}
			require.JSONEq(t, want, out.String())
		})
	}
}

func TestDBHelpAndInvalidInputsDoNotBuild(t *testing.T) {
	for _, name := range []string{"up", "down", "status"} {
		for _, args := range [][]string{{"--help"}, {"--unknown"}, {"extra"}, {"--steps", "0"}, {"--steps", "-1"}, {"--steps", "bad"}} {
			t.Run(name+"/"+args[0]+args[len(args)-1], func(t *testing.T) {
				ctrl := gomock.NewController(t)
				var leaf *cli.Command
				switch name {
				case "up":
					leaf = db.NewUpCommand(mocks.NewMockUpDepsFactory(ctrl))
				case "down":
					leaf = db.NewDownCommand(mocks.NewMockDownDepsFactory(ctrl))
				case "status":
					leaf = db.NewStatusCommand(mocks.NewMockStatusDepsFactory(ctrl))
				}
				err := runLeaf(t.Context(), leaf, io.Discard, args...)
				if args[0] == "--help" {
					require.NoError(t, err)
				} else {
					require.Error(t, err)
				}
			})
		}
	}
}

func TestUpFailureReportsObservedStateAndJoinsCleanup(t *testing.T) {
	operationErr, cleanupErr := errors.New("migration failed"), errors.New("close failed")
	for _, statusFails := range []bool{false, true} {
		t.Run(map[bool]string{true: "unknown", false: "dirty"}[statusFails], func(t *testing.T) {
			ctrl := gomock.NewController(t)
			factory, migrator := mocks.NewMockUpDepsFactory(ctrl), mocks.NewMockUpMigrator(ctrl)
			factory.EXPECT().Build(gomock.Any(), gomock.Any()).Return(&db.UpDeps{
				Migrator: migrator, Shutdown: func(context.Context) error { return cleanupErr },
			}, nil)
			migrator.EXPECT().Up(gomock.Any(), 0).Return(false, operationErr)
			version := uint(3)
			var statusErr error
			if statusFails {
				statusErr = errors.New("status failed")
			}
			migrator.EXPECT().Status(gomock.Any()).Return(db.MigrationStatus{Version: &version, Dirty: true}, statusErr)
			var out bytes.Buffer
			err := runLeaf(t.Context(), db.NewUpCommand(factory), &out, "--json")
			require.ErrorIs(t, err, operationErr)
			require.ErrorIs(t, err, cleanupErr)
			require.Empty(t, out.String())
			if statusFails {
				require.ErrorContains(t, err, "unknown")
				require.ErrorIs(t, err, statusErr)
			} else {
				require.ErrorContains(t, err, "version=3 dirty=true")
			}
		})
	}
}

func TestDBBuildFailure(t *testing.T) {
	failure := errors.New("build failed")
	ctrl := gomock.NewController(t)
	up := mocks.NewMockUpDepsFactory(ctrl)
	up.EXPECT().Build(gomock.Any(), gomock.Any()).Return(nil, failure)
	require.ErrorIs(t, runLeaf(t.Context(), db.NewUpCommand(up), io.Discard), failure)
	down := mocks.NewMockDownDepsFactory(ctrl)
	down.EXPECT().Build(gomock.Any(), gomock.Any()).Return(nil, failure)
	require.ErrorIs(t, runLeaf(t.Context(), db.NewDownCommand(down), io.Discard), failure)
	status := mocks.NewMockStatusDepsFactory(ctrl)
	status.EXPECT().Build(gomock.Any(), gomock.Any()).Return(nil, failure)
	require.ErrorIs(t, runLeaf(t.Context(), db.NewStatusCommand(status), io.Discard), failure)
}

func cleanShutdown(context.Context) error { return nil }

type brokenWriter struct{ err error }

// Write rejects the write with the configured fixture error.
func (w brokenWriter) Write([]byte) (int, error) { return 0, w.err }

func TestStatusOutputAndCleanupFailures(t *testing.T) {
	failure := errors.New("test failure")
	for _, stage := range []string{"status", "shutdown", "output"} {
		t.Run(stage, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			factory, reader := mocks.NewMockStatusDepsFactory(ctrl), mocks.NewMockUpMigrator(ctrl)
			closed := 0
			factory.EXPECT().Build(gomock.Any(), gomock.Any()).Return(&db.StatusDeps{
				Migrator: reader, Shutdown: func(ctx context.Context) error {
					require.NoError(t, ctx.Err())
					closed++
					if stage == "shutdown" {
						return failure
					}
					return nil
				},
			}, nil)
			var statusErr error
			if stage == "status" {
				statusErr = failure
			}
			reader.EXPECT().Status(gomock.Any()).Return(db.MigrationStatus{}, statusErr)
			var out bytes.Buffer
			var writer io.Writer = &out
			if stage == "output" {
				writer = brokenWriter{failure}
			}
			err := runLeaf(t.Context(), db.NewStatusCommand(factory), writer, "--json")
			require.ErrorIs(t, err, failure)
			require.Equal(t, 1, closed)
			require.Empty(t, out.String())
		})
	}
}

func TestDownCancellationCleansUpAndReportsUnknown(t *testing.T) {
	ctrl := gomock.NewController(t)
	factory, migrator := mocks.NewMockDownDepsFactory(ctrl), mocks.NewMockDownMigrator(ctrl)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	closed := false
	factory.EXPECT().Build(gomock.Any(), gomock.Any()).Return(&db.DownDeps{
		Migrator: migrator, Shutdown: func(cleanup context.Context) error { require.NoError(t, cleanup.Err()); closed = true; return nil },
	}, nil)
	migrator.EXPECT().Down(gomock.Any(), 1).DoAndReturn(func(operation context.Context, _ int) (bool, error) {
		cancel()
		return false, operation.Err()
	})
	migrator.EXPECT().Status(gomock.Any()).Return(db.MigrationStatus{}, context.Canceled)
	var out bytes.Buffer
	err := runLeaf(ctx, db.NewDownCommand(factory), &out, "--json")
	require.ErrorIs(t, err, context.Canceled)
	require.ErrorContains(t, err, "unknown")
	require.True(t, closed)
	require.Empty(t, out.String())
}

func TestSeparateCommandsKeepIndependentFlags(t *testing.T) {
	ctrl := gomock.NewController(t)
	first, second := mocks.NewMockStatusDepsFactory(ctrl), mocks.NewMockStatusDepsFactory(ctrl)
	reader := mocks.NewMockUpMigrator(ctrl)
	firstCommand, secondCommand := db.NewStatusCommand(first), db.NewStatusCommand(second)
	first.EXPECT().Build(gomock.Any(), db.StatusCommandOptions{CommonOptions: db.CommonOptions{URL: "postgres://first/db", URLSet: true, JSON: true}}).Return(
		&db.StatusDeps{Migrator: reader, Shutdown: cleanShutdown}, nil,
	)
	second.EXPECT().Build(gomock.Any(), db.StatusCommandOptions{}).Return(&db.StatusDeps{Migrator: reader, Shutdown: cleanShutdown}, nil)
	reader.EXPECT().Status(gomock.Any()).Return(db.MigrationStatus{}, nil).Times(2)
	require.NoError(t, runLeaf(t.Context(), firstCommand, io.Discard, "--json", "--url", "postgres://first/db"))
	require.NoError(t, runLeaf(t.Context(), secondCommand, io.Discard))
}
