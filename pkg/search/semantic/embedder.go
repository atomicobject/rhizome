package semantic

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/atomicobject/rhizome/pkg/indexingperf"
	"github.com/atomicobject/rhizome/pkg/search/embeddings"
)

// QueryEmbedder computes query embeddings across code and note providers.
// It optimizes for the common case where both providers are the same (compute once),
// and runs providers in parallel when they differ.
type QueryEmbedder struct {
	CodeProvider embeddings.Provider
	NoteProvider embeddings.Provider
}

// QueryEmbeddings holds computed embeddings for a query.
type QueryEmbeddings struct {
	Code embeddings.Embedding // nil if no code provider
	Note embeddings.Embedding // nil if no note provider
}

// Embed computes query embeddings, calling providers in parallel when they differ.
// If both providers are the same instance, the embedding is computed once and shared.
func (e *QueryEmbedder) Embed(ctx context.Context, text string) (QueryEmbeddings, error) {
	if err := ctx.Err(); err != nil {
		return QueryEmbeddings{}, err
	}
	return queryEmbeddingMemoFromContext(ctx).embed(ctx, text, func() (QueryEmbeddings, error) {
		return e.compute(ctx, text)
	})
}

func (e *QueryEmbedder) compute(ctx context.Context, text string) (QueryEmbeddings, error) {
	hasCode := e.CodeProvider != nil
	hasNote := e.NoteProvider != nil

	if !hasCode && !hasNote {
		return QueryEmbeddings{}, nil
	}

	// Optimization: if both providers are the same, compute once.
	if hasCode && hasNote && e.CodeProvider == e.NoteProvider {
		started := time.Now()
		vec, err := embedOnce(ctx, e.CodeProvider, text)
		addTiming(ctx, TimingEvent{
			Name:     "semantic.embed.shared",
			Kind:     "retriever",
			Started:  started,
			Duration: time.Since(started),
			Status:   timingStatus(err),
			Err:      timingErr(err),
		})
		if err != nil {
			return QueryEmbeddings{}, err
		}
		result := QueryEmbeddings{Code: vec, Note: vec}
		return result, nil
	}

	// Only one provider, or providers differ - compute separately.
	var result QueryEmbeddings
	var wg sync.WaitGroup

	ctx, cancel := context.WithCancelCause(ctx)
	defer cancel(nil)

	if hasCode {
		wg.Add(1)
		go func() {
			defer wg.Done()
			started := time.Now()
			vec, err := embedOnce(ctx, e.CodeProvider, text)
			addTiming(ctx, TimingEvent{
				Name:     "semantic.embed.code",
				Kind:     "retriever",
				Started:  started,
				Duration: time.Since(started),
				Status:   timingStatus(err),
				Err:      timingErr(err),
			})
			if err != nil {
				cancel(fmt.Errorf("embed code query: %w", err))
				return
			}
			result.Code = vec
		}()
	}

	if hasNote {
		wg.Add(1)
		go func() {
			defer wg.Done()
			started := time.Now()
			vec, err := embedOnce(ctx, e.NoteProvider, text)
			addTiming(ctx, TimingEvent{
				Name:     "semantic.embed.note",
				Kind:     "retriever",
				Started:  started,
				Duration: time.Since(started),
				Status:   timingStatus(err),
				Err:      timingErr(err),
			})
			if err != nil {
				cancel(fmt.Errorf("embed note query: %w", err))
				return
			}
			result.Note = vec
		}()
	}

	wg.Wait()

	// The cause is recorded before cancellation wakes the sibling provider.
	if err := context.Cause(ctx); err != nil {
		return QueryEmbeddings{}, err
	}

	return result, nil
}

// embedOnce embeds a single text and returns the embedding.
func embedOnce(ctx context.Context, provider embeddings.Provider, text string) (embeddings.Embedding, error) {
	indexingperf.AddCount(ctx, "provider.calls", 1)
	vecs, err := provider.EmbedTexts(ctx, []string{text})
	if err != nil {
		return nil, err
	}
	if len(vecs) != 1 {
		return nil, fmt.Errorf("unexpected vector count: want 1, got %d", len(vecs))
	}
	return vecs[0], nil
}
