package projection

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"time"

	"github.com/ethernity/obsidian-topic-stream/internal/domain"
)

type embeddingGroup struct {
	ctx      context.Context
	embedder Embedder
	options  BatchOptions
	requests chan embeddingRequest
	stop     chan struct{}
	mu       sync.Mutex
	failure  error
}

func encodedTextSize(text string) int { body, _ := json.Marshal(text); return len(body) }

func (g *embeddingGroup) run() {
	var executing sync.WaitGroup
	defer executing.Wait()
	slots := make(chan struct{}, g.options.Concurrency)
	var carry *embeddingRequest
	for {
		var first embeddingRequest
		if carry != nil {
			first = *carry
		} else {
			select {
			case first = <-g.requests:
			case <-g.ctx.Done():
				return
			case <-g.stop:
				return
			}
		}
		batch, next := g.collect(first)
		carry = next
		select {
		case slots <- struct{}{}:
			executing.Go(func() { defer func() { <-slots }(); g.execute(batch) })
		case <-g.ctx.Done():
			return
		case <-g.stop:
			return
		}
	}
}

func (g *embeddingGroup) collect(first embeddingRequest) ([]embeddingRequest, *embeddingRequest) {
	batch := []embeddingRequest{first}
	bytes := first.bytes
	timer := time.NewTimer(5 * time.Millisecond)
	defer timer.Stop()
	for len(batch) < g.options.Size && bytes < g.options.InputBytes {
		select {
		case next := <-g.requests:
			if next.bytes > g.options.InputBytes-bytes {
				return batch, &next
			}
			batch = append(batch, next)
			bytes += next.bytes
		case <-timer.C:
			return batch, nil
		case <-g.ctx.Done():
			return batch, nil
		case <-g.stop:
			return batch, nil
		}
	}
	return batch, nil
}

func (g *embeddingGroup) execute(batch []embeddingRequest) {
	if failure := g.currentFailure(); failure != nil {
		g.replyError(batch, failure)
		return
	}
	texts := make([]string, len(batch))
	for i, request := range batch {
		texts[i] = request.text
	}
	vectors, err := g.embed(texts, batch[0].bytes)
	if err == nil && len(vectors) != len(batch) {
		err = errors.New("embedding batch returned an incomplete vector set")
	}
	if err == nil {
		for i, request := range batch {
			request.result <- embeddingResult{vector: vectors[i]}
		}
		return
	}
	g.reject(batch, err)
}

func (g *embeddingGroup) reject(batch []embeddingRequest, err error) {
	isolatable := errors.Is(err, domain.ErrEmbeddingInputRejected) || errors.Is(err, domain.ErrEmbeddingBatchLimit)
	if isolatable && len(batch) > 1 {
		middle := len(batch) / 2
		g.execute(batch[:middle])
		g.execute(batch[middle:])
		return
	}
	if !isolatable {
		g.mu.Lock()
		if g.failure == nil {
			g.failure = err
		}
		g.mu.Unlock()
	}
	g.replyError(batch, err)
}

func (g *embeddingGroup) currentFailure() error {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.failure
}

func (g *embeddingGroup) embed(texts []string, firstBytes int) ([][]float32, error) {
	if len(texts) == 1 && (g.options.Size == 1 || firstBytes > g.options.InputBytes) {
		vector, err := g.embedder.Embed(g.ctx, texts[0])
		if err != nil {
			return nil, err
		}
		return [][]float32{vector}, nil
	}
	return g.embedder.EmbedBatch(g.ctx, texts)
}

func (*embeddingGroup) replyError(batch []embeddingRequest, err error) {
	for _, request := range batch {
		request.result <- embeddingResult{err: err}
	}
}
