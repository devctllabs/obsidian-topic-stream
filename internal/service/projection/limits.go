package projection

import (
	"context"
	"net/url"
	"strings"
	"sync"

	"github.com/ethernity/obsidian-topic-stream/internal/domain"
)

// limitedFactory shares request slots across every background operation using this service graph.
type limitedFactory struct {
	delegate    Factory
	concurrency int
	mu          sync.Mutex
	slots       map[string]chan struct{}
}

// Build wraps both embedding capabilities with the same endpoint admission bound.
func (f *limitedFactory) Build(model domain.EmbeddingModel) (Embedder, Writer, error) {
	embedder, writer, err := f.delegate.Build(model)
	if err != nil {
		return nil, nil, err
	}
	key := endpointKey(model.Endpoint)
	f.mu.Lock()
	if f.slots == nil {
		f.slots = make(map[string]chan struct{})
	}
	slots, ok := f.slots[key]
	if !ok {
		slots = make(chan struct{}, f.concurrency)
		f.slots[key] = slots
	}
	f.mu.Unlock()
	return limitedEmbedder{delegate: embedder, slots: slots}, writer, nil
}

func endpointKey(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return strings.TrimRight(raw, "/")
	}
	u.Scheme = strings.ToLower(u.Scheme)
	u.Host = strings.ToLower(u.Host)
	if u.Scheme == "http" && u.Port() == "80" || u.Scheme == "https" && u.Port() == "443" {
		u.Host = strings.TrimSuffix(u.Host, ":"+u.Port())
	}
	u.Path = strings.TrimRight(u.Path, "/")
	return u.String()
}

type limitedEmbedder struct {
	delegate Embedder
	slots    chan struct{}
}

func (e limitedEmbedder) acquire(ctx context.Context) error {
	select {
	case e.slots <- struct{}{}:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Embed admits one unchanged-text request and releases its slot on every outcome.
func (e limitedEmbedder) Embed(ctx context.Context, text string) ([]float32, error) {
	if err := e.acquire(ctx); err != nil {
		return nil, err
	}
	defer func() { <-e.slots }()
	return e.delegate.Embed(ctx, text)
}

// EmbedBatch admits one batch request; caller cancellation also bounds queued admission.
func (e limitedEmbedder) EmbedBatch(ctx context.Context, texts []string) ([][]float32, error) {
	if err := e.acquire(ctx); err != nil {
		return nil, err
	}
	defer func() { <-e.slots }()
	return e.delegate.EmbedBatch(ctx, texts)
}
