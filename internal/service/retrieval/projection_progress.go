package retrieval

import (
	"context"
	"errors"
	"time"

	"github.com/ethernity/obsidian-topic-stream/internal/domain"
)

type projectionPageResult struct {
	items []domain.ItemProjectionWriteResult
	err   error
}

// writeRebuildPage owns the writer goroutine and the stall clock. Its callback only signals;
// workers never mutate syncProgress, and useful writes renew the clock during a long page.
func (m *Manager) writeRebuildPage(ctx context.Context, commits []domain.ItemProjectionCommit, progress *syncProgress) ([]domain.ItemProjectionWriteResult, error) {
	writeCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	advanced := make(chan struct{}, 1)
	done := make(chan projectionPageResult, 1)
	go func() {
		items, err := m.projector.WriteItemProjections(writeCtx, commits, func() {
			select {
			case advanced <- struct{}{}:
			default:
			}
		})
		done <- projectionPageResult{items: items, err: err}
	}()
	timer := time.NewTimer(time.Until(progress.deadline))
	defer timer.Stop()
	for {
		select {
		case result := <-done:
			return result.items, result.err
		case <-advanced:
			progress.advanced()
			timer.Reset(time.Until(progress.deadline))
		case <-ctx.Done():
			cancel()
			result := <-done
			return result.items, errors.Join(ctx.Err(), result.err)
		case <-timer.C:
			select {
			case <-advanced:
				progress.advanced()
				timer.Reset(time.Until(progress.deadline))
			default:
				cancel()
				result := <-done
				return result.items, errors.Join(domain.ErrModelSyncStalled, result.err)
			}
		}
	}
}
