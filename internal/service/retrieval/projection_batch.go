package retrieval

import (
	"context"
	"errors"
	"fmt"

	"github.com/ethernity/obsidian-topic-stream/internal/domain"
	"github.com/google/uuid"
)

type rebuildAcknowledgements struct {
	modelID      uuid.UUID
	pending      []domain.ItemProjectionDelivery
	failures     map[uuid.UUID]bool
	firstFailure error
}

func (batch *rebuildAcknowledgements) fail(itemID uuid.UUID, err error) {
	batch.failures[itemID] = true
	if batch.firstFailure == nil {
		batch.firstFailure = fmt.Errorf("deliver item %s projection for model %s: %w", itemID, batch.modelID, err)
	}
}

func (m *Manager) completeRebuildBatch(ctx context.Context, batch rebuildAcknowledgements, progress *syncProgress) (int, error) {
	if len(batch.pending) == 0 {
		return len(batch.failures), batch.firstFailure
	}
	stepCtx, cancel := progress.context(ctx)
	accepted, err := m.projector.CompleteItemProjections(stepCtx, batch.pending)
	cancel()
	if err != nil {
		err = progress.failure(ctx, err)
		for _, entry := range batch.pending {
			batch.fail(entry.ItemID, err)
		}
		return len(batch.failures), errors.Join(batch.firstFailure, err)
	}
	advanced := false
	for index, entry := range batch.pending {
		if accepted[index] {
			advanced = true
			progress.advanced()
		} else {
			batch.fail(entry.ItemID, errors.New("item projection changed during delivery"))
		}
	}
	if advanced {
		progress.announce(false)
	}
	return len(batch.failures), batch.firstFailure
}
