package retrieval

import (
	"context"
	"errors"
	"time"

	"github.com/ethernity/obsidian-topic-stream/internal/domain"
)

// syncProgress bounds each blocking step by the remaining no-progress window. Successful work
// extends the next step's deadline, so healthy indexing has no total-duration limit.
type syncProgress struct {
	timeout  time.Duration
	deadline time.Time
	// changed is called only after persisted work, from the rebuild owner goroutine.
	changed func(force bool)
}

func newSyncProgress(timeout time.Duration) *syncProgress {
	progress := &syncProgress{timeout: timeout}
	progress.advanced()
	return progress
}

func (p *syncProgress) context(parent context.Context) (context.Context, context.CancelFunc) {
	return context.WithDeadline(parent, p.deadline)
}

func (p *syncProgress) advanced() { p.deadline = time.Now().Add(p.timeout) }

func (p *syncProgress) announce(force bool) {
	if p.changed != nil {
		p.changed(force)
	}
}

func (p *syncProgress) failure(parent context.Context, err error) error {
	if parent.Err() != nil {
		return errors.Join(err, parent.Err())
	}
	if !time.Now().Before(p.deadline) {
		return errors.Join(domain.ErrModelSyncStalled, err)
	}
	return err
}
