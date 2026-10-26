package retrieval

import (
	"context"
	"fmt"
	"time"

	"github.com/ethernity/obsidian-topic-stream/internal/domain"
	"github.com/google/uuid"
)

// jobProgressNotifier belongs to one execution. It coalesces fast durable ACKs without
// a timer goroutine; phase changes always flush an advisory for the authoritative Job.
func (m *Manager) jobProgressNotifier(jobID uuid.UUID) func(bool) {
	var last time.Time
	return func(force bool) {
		if m.progressNotifier == nil || (!force && time.Since(last) < time.Second) {
			return
		}
		last = time.Now()
		m.progressNotifier.NotifyJobChanged(m.workspaceID, jobID)
	}
}

func (m *Manager) activateRebuild(ctx context.Context, cutover domain.ModelTransitionCutover, progress *syncProgress) error {
	stepCtx, cancel := progress.context(ctx)
	err := m.catalog.PrepareActivation(stepCtx, cutover)
	cancel()
	if err != nil {
		return fmt.Errorf("prepare retrieval activation: %w", progress.failure(ctx, err))
	}
	progress.advanced()
	progress.announce(true)
	stepCtx, cancel = progress.context(ctx)
	err = m.catalog.Activate(stepCtx, cutover)
	cancel()
	if err != nil {
		return fmt.Errorf("activate retrieval model: %w", progress.failure(ctx, err))
	}
	progress.announce(true)
	return nil
}
