package projection

import (
	"context"
	"errors"
	"sync"

	"github.com/ethernity/obsidian-topic-stream/internal/domain"
	"github.com/google/uuid"
)

func (s *Service) writePage(ctx context.Context, commits []domain.ItemProjectionCommit, onWritten func()) ([]domain.ItemProjectionWriteResult, error) {
	if len(commits) == 0 {
		return nil, nil
	}
	if len(commits) > 100 {
		return nil, errors.New("projection write page exceeds 100 Items")
	}
	workCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	factory := &batchFactory{delegate: s.factory, ctx: workCtx, options: s.batchOptions}
	defer factory.close()
	writer := &Service{repository: s.repository, factory: factory, batchOptions: s.batchOptions}
	results := make([]domain.ItemProjectionWriteResult, len(commits))
	indices := make(chan int, len(commits))
	for i := range commits {
		indices <- i
	}
	close(indices)
	var workers sync.WaitGroup
	for range min(len(commits), s.batchOptions.Size*s.batchOptions.Concurrency) {
		workers.Go(func() {
			for i := range indices {
				results[i].Deliveries, results[i].Err = writer.WriteItemProjection(workCtx, commits[i])
				if len(results[i].Deliveries) > 0 && onWritten != nil {
					onWritten()
				}
			}
		})
	}
	workers.Wait()
	return results, ctx.Err()
}

type batchModel struct {
	id         uuid.UUID
	descriptor domain.EmbeddingModelDescriptor
}

// batchFactory scopes collectors to one bounded page; its delegate owns the process-wide limiter.
type batchFactory struct {
	delegate Factory
	ctx      context.Context
	options  BatchOptions
	mu       sync.Mutex
	groups   map[batchModel]*embeddingGroup
	workers  sync.WaitGroup
}

// Build retains the existing per-Item writer and groups only identical model descriptors.
func (f *batchFactory) Build(model domain.EmbeddingModel) (Embedder, Writer, error) {
	embedder, writer, err := f.delegate.Build(model)
	if err != nil {
		return nil, nil, err
	}
	key := batchModel{id: model.ID, descriptor: model.EmbeddingModelDescriptor}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.groups == nil {
		f.groups = make(map[batchModel]*embeddingGroup)
	}
	group, ok := f.groups[key]
	if !ok {
		group = &embeddingGroup{ctx: f.ctx, embedder: embedder, options: f.options, requests: make(chan embeddingRequest, 100), stop: make(chan struct{})}
		f.groups[key] = group
		f.workers.Go(group.run)
	}
	return queuedEmbedder{group: group}, writer, nil
}

func (f *batchFactory) close() {
	for _, group := range f.groups {
		close(group.stop)
	}
	f.workers.Wait()
}

type embeddingResult struct {
	vector []float32
	err    error
}
type embeddingRequest struct {
	text   string
	bytes  int
	result chan embeddingResult
}
type queuedEmbedder struct{ group *embeddingGroup }

// Embed waits for this input's own vector, even when a provider batch is split after rejection.
func (e queuedEmbedder) Embed(ctx context.Context, text string) ([]float32, error) {
	request := embeddingRequest{text: text, bytes: encodedTextSize(text), result: make(chan embeddingResult, 1)}
	select {
	case e.group.requests <- request:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	select {
	case result := <-request.result:
		return result.vector, result.err
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// EmbedBatch preserves the capability when a consumer explicitly requests already grouped text.
func (e queuedEmbedder) EmbedBatch(ctx context.Context, texts []string) ([][]float32, error) {
	return e.group.embedder.EmbedBatch(ctx, texts)
}
