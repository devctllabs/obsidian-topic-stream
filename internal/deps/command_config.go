package deps

import (
	"context"
	"time"

	"github.com/ethernity/obsidian-topic-stream/internal/platform/config"
)

// NewServe resolves configuration and builds the selected daemon runtime.
func NewServe(ctx context.Context, version string) (*API, time.Duration, error) {
	cfg, err := config.Load(ctx)
	if err != nil {
		return nil, 0, err
	}
	runtime, err := NewAPI(ctx, cfg, version)
	return runtime, cfg.Runtime.ShutdownTimeout, err
}

// NewBenchFromEnv resolves configuration before acquiring benchmark dependencies.
func NewBenchFromEnv(ctx context.Context) (*Bench, error) {
	cfg, err := config.Load(ctx)
	if err != nil {
		return nil, err
	}
	return NewBench(ctx, cfg)
}
