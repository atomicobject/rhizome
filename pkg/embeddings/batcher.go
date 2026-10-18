package embeddings

import (
	"context"
	"sync"
)

// BatchFunc embeds a batch of texts and returns embeddings in the same order.
// Implementations should handle their own HTTP requests and error handling.
type BatchFunc func(ctx context.Context, texts []string) ([]Embedding, error)

// NextBatchSizeFunc returns the next batch size given remaining texts.
// Used for dynamic batch sizing (e.g., rate-limit-aware sizing).
type NextBatchSizeFunc func(remaining []string) int

// BatchExecutor orchestrates concurrent batch embedding.
type BatchExecutor struct {
	// BatchSize is the maximum number of texts per batch.
	// If <= 0, defaults to DefaultBatchSize.
	// Ignored if NextBatchSizeFn or MaxTokensPerBatch is set.
	BatchSize int

	// MaxTokensPerBatch enables token-aware batching. When > 0, batches are
	// packed to fit within this token budget (estimated as chars/4).
	// BatchSize still acts as a hard cap on items per batch.
	MaxTokensPerBatch int

	// NextBatchSizeFn optionally provides dynamic batch sizing.
	// When set, BatchSize and MaxTokensPerBatch are ignored.
	NextBatchSizeFn NextBatchSizeFunc

	// MaxConcurrency is the maximum number of concurrent batch requests.
	// If <= 0, defaults to 1 (sequential).
	MaxConcurrency int
}

// Execute splits texts into batches and processes them using fn.
// When MaxConcurrency > 1, batches are processed concurrently.
// Results are returned in the same order as input texts.
// On first error, remaining batches are cancelled and the error is returned.
func (b *BatchExecutor) Execute(ctx context.Context, texts []string, fn BatchFunc) ([]Embedding, error) {
	if len(texts) == 0 {
		return nil, nil
	}

	maxConc := b.MaxConcurrency
	if maxConc <= 0 {
		maxConc = 1
	}

	// Dynamic batch sizing requires sequential execution since batch sizes
	// may depend on state that changes after each request (e.g., rate limits).
	if b.NextBatchSizeFn != nil {
		return b.executeSequentialDynamic(ctx, texts, fn)
	}

	batchSize := b.BatchSize
	if batchSize <= 0 {
		batchSize = DefaultBatchSize
	}

	// Build batch ranges
	var batches []batchRange

	if b.MaxTokensPerBatch > 0 {
		// Token-aware batching: pack texts until we hit token budget
		batches = buildTokenAwareBatches(texts, b.MaxTokensPerBatch, batchSize)
	} else {
		// Fixed-size batching
		for start := 0; start < len(texts); start += batchSize {
			end := start + batchSize
			if end > len(texts) {
				end = len(texts)
			}
			batches = append(batches, batchRange{start, end})
		}
	}

	// Single batch optimization: skip orchestration overhead
	if len(batches) == 1 {
		return fn(ctx, texts[batches[0].start:batches[0].end])
	}

	// Sequential execution when concurrency is 1
	if maxConc == 1 {
		results := make([]Embedding, len(texts))
		for _, br := range batches {
			vecs, err := fn(ctx, texts[br.start:br.end])
			if err != nil {
				return nil, err
			}
			copy(results[br.start:br.end], vecs)
		}
		return results, nil
	}

	// Concurrent execution
	results := make([]Embedding, len(texts))
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	sem := make(chan struct{}, maxConc)
	errCh := make(chan error, 1)
	var wg sync.WaitGroup

	for _, br := range batches {
		br := br // capture
		wg.Add(1)
		go func() {
			defer wg.Done()

			// Acquire semaphore
			select {
			case sem <- struct{}{}:
			case <-ctx.Done():
				return
			}
			defer func() { <-sem }()

			// Check cancellation before work
			if ctx.Err() != nil {
				return
			}

			vecs, err := fn(ctx, texts[br.start:br.end])
			if err != nil {
				select {
				case errCh <- err:
				default:
				}
				cancel()
				return
			}
			copy(results[br.start:br.end], vecs)
		}()
	}

	wg.Wait()

	select {
	case err := <-errCh:
		return nil, err
	default:
	}

	return results, nil
}

// executeSequentialDynamic handles dynamic batch sizing (sequential only).
func (b *BatchExecutor) executeSequentialDynamic(ctx context.Context, texts []string, fn BatchFunc) ([]Embedding, error) {
	results := make([]Embedding, len(texts))
	for start := 0; start < len(texts); {
		remaining := texts[start:]
		n := b.NextBatchSizeFn(remaining)
		if n < 1 {
			n = 1
		}
		if n > len(remaining) {
			n = len(remaining)
		}
		end := start + n
		batch := texts[start:end]

		vecs, err := fn(ctx, batch)
		if err != nil {
			return nil, err
		}
		if len(vecs) != len(batch) {
			return nil, errBatchSizeMismatch(len(batch), len(vecs))
		}
		copy(results[start:end], vecs)
		start = end
	}
	return results, nil
}

func errBatchSizeMismatch(want, got int) error {
	return &batchSizeMismatchError{want: want, got: got}
}

type batchSizeMismatchError struct {
	want, got int
}

func (e *batchSizeMismatchError) Error() string {
	return "embedding count mismatch: want " + itoa(e.want) + " got " + itoa(e.got)
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	if n < 0 {
		return "-" + itoa(-n)
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}

// batchRange represents a slice of texts [start, end).
type batchRange struct {
	start, end int
}

// buildTokenAwareBatches packs texts into batches that fit within maxTokens.
// Tokens are estimated as len(text)/4 (rough approximation for English).
// maxItems acts as a hard cap on items per batch even if token budget allows more.
func buildTokenAwareBatches(texts []string, maxTokens, maxItems int) []batchRange {
	var batches []batchRange
	start := 0
	batchTokens := 0

	for i, text := range texts {
		// Estimate tokens: chars/4 is a rough approximation
		textTokens := (len(text) + 3) / 4
		if textTokens < 1 {
			textTokens = 1
		}

		itemsInBatch := i - start
		wouldExceedTokens := batchTokens+textTokens > maxTokens && itemsInBatch > 0
		wouldExceedItems := itemsInBatch >= maxItems

		if wouldExceedTokens || wouldExceedItems {
			// Close current batch
			batches = append(batches, batchRange{start, i})
			start = i
			batchTokens = 0
		}

		batchTokens += textTokens
	}

	// Close final batch
	if start < len(texts) {
		batches = append(batches, batchRange{start, len(texts)})
	}

	return batches
}
