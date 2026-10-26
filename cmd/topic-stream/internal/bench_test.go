package command_test

import (
	"bytes"
	"encoding/json"
	"testing"
	"time"

	command "github.com/ethernity/obsidian-topic-stream/cmd/topic-stream/internal"
	"github.com/ethernity/obsidian-topic-stream/cmd/topic-stream/internal/mocks"
	"github.com/ethernity/obsidian-topic-stream/internal/service/bench"
	"github.com/stretchr/testify/require"
	"github.com/urfave/cli/v3"
	"go.uber.org/mock/gomock"
)

func TestBenchCommandEmitsJSONOnlyAndPassesWindowAndSplit(t *testing.T) {
	report := bench.Report{
		Version: bench.ReportVersion, TrainPercent: 70, TrainingCount: 7, TestCount: 3,
		Pipelines: []bench.PipelineResult{{Name: "bm25", Metrics: bench.Metrics{PrecisionAt10: 0.5}}},
	}
	ctrl := gomock.NewController(t)
	factory := mocks.NewMockBenchDepsFactory(ctrl)
	evaluator := mocks.NewMockBenchEvaluator(ctrl)
	factory.EXPECT().Build(gomock.Any(), command.BenchCommandOptions{
		JSON: true, Since: "2026-01-01T00:00:00Z", TrainPercent: 70,
	}).Return(&command.BenchDeps{Evaluator: evaluator, Shutdown: cleanShutdown}, nil)
	since := time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)
	evaluator.EXPECT().Evaluate(gomock.Any(), bench.Options{Since: &since, TrainPercent: 70}).Return(report, nil)
	var stdout, stderr bytes.Buffer
	root := &cli.Command{
		Name: "topic-stream", Writer: &stdout, ErrWriter: &stderr,
		Commands: []*cli.Command{command.NewBenchCommand(factory)},
	}

	err := root.Run(t.Context(), []string{
		"topic-stream", "bench", "--json", "--since", "2026-01-01T00:00:00Z", "--train-percent", "70",
	})

	require.NoError(t, err)
	require.Empty(t, stderr.String())
	var output bench.Report
	require.NoError(t, json.Unmarshal(stdout.Bytes(), &output), stdout.String())
	require.Equal(t, report, output)
}

func TestBenchCommandDefaultsToHumanReadableTable(t *testing.T) {
	report := bench.Report{
		Version: bench.ReportVersion, TrainPercent: 80, TrainingCount: 8, TestCount: 2,
		Pipelines: []bench.PipelineResult{{Name: "hybrid-rrf", Metrics: bench.Metrics{
			PrecisionAt10: 0.7, RecallAt50: 0.8, MRR: 0.9, NDCGAt10: 0.85,
		}}},
	}
	ctrl := gomock.NewController(t)
	factory := mocks.NewMockBenchDepsFactory(ctrl)
	evaluator := mocks.NewMockBenchEvaluator(ctrl)
	factory.EXPECT().Build(gomock.Any(), command.BenchCommandOptions{TrainPercent: 80}).Return(
		&command.BenchDeps{Evaluator: evaluator, Shutdown: cleanShutdown}, nil,
	)
	evaluator.EXPECT().Evaluate(gomock.Any(), bench.Options{TrainPercent: 80}).Return(report, nil)
	var stdout bytes.Buffer
	root := &cli.Command{
		Name: "topic-stream", Writer: &stdout,
		Commands: []*cli.Command{command.NewBenchCommand(factory)},
	}

	err := root.Run(t.Context(), []string{"topic-stream", "bench"})

	require.NoError(t, err)
	require.Contains(t, stdout.String(), "PIPELINE")
	require.Contains(t, stdout.String(), "PRECISION@10")
	require.Contains(t, stdout.String(), "hybrid-rrf")
	require.Contains(t, stdout.String(), "train=8 test=2 split=80%")
}
