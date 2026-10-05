package semantic

import (
	"context"
	"sort"
	"strings"
	"sync"

	"github.com/atomicobject/rhizome/pkg/search/embeddings"
)

// QueryEmbeddingRecord is a reusable query-vector result. Continuation
// callers can retain these records so every page searches with the same
// provider output, even when an external provider is not bit-for-bit stable.
type QueryEmbeddingRecord struct {
	Text string               `json:"text"`
	Code embeddings.Embedding `json:"code,omitempty"`
	Note embeddings.Embedding `json:"note,omitempty"`
}

type queryEmbeddingMemoKey struct{}

// QueryEmbeddingMemo shares complete query vectors within one logical search.
type QueryEmbeddingMemo struct {
	mu      sync.RWMutex
	entries map[string]QueryEmbeddings
	pending map[string]*queryEmbeddingCall
}

type queryEmbeddingCall struct {
	done        chan struct{}
	vectors     QueryEmbeddings
	err         error
	callerEnded bool
}

// WithQueryEmbeddingMemo installs a bounded request memo seeded from an
// earlier page. The returned memo can be snapshotted into the next cursor.
func WithQueryEmbeddingMemo(ctx context.Context, seeded []QueryEmbeddingRecord) (context.Context, *QueryEmbeddingMemo) {
	memo := &QueryEmbeddingMemo{entries: make(map[string]QueryEmbeddings, len(seeded))}
	for _, record := range seeded {
		text := strings.TrimSpace(record.Text)
		if text == "" {
			continue
		}
		memo.entries[text] = cloneQueryEmbeddings(QueryEmbeddings{Code: record.Code, Note: record.Note})
	}
	return context.WithValue(ctx, queryEmbeddingMemoKey{}, memo), memo
}

// Snapshot returns a deterministic copy suitable for an opaque continuation.
func (m *QueryEmbeddingMemo) Snapshot() []QueryEmbeddingRecord {
	if m == nil {
		return nil
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	texts := make([]string, 0, len(m.entries))
	for text := range m.entries {
		texts = append(texts, text)
	}
	sort.Strings(texts)
	out := make([]QueryEmbeddingRecord, 0, len(texts))
	for _, text := range texts {
		embs := cloneQueryEmbeddings(m.entries[text])
		out = append(out, QueryEmbeddingRecord{Text: text, Code: embs.Code, Note: embs.Note})
	}
	return out
}

func queryEmbeddingMemoFromContext(ctx context.Context) *QueryEmbeddingMemo {
	memo, _ := ctx.Value(queryEmbeddingMemoKey{}).(*QueryEmbeddingMemo)
	return memo
}

func (m *QueryEmbeddingMemo) embed(ctx context.Context, text string, compute func() (QueryEmbeddings, error)) (QueryEmbeddings, error) {
	key := strings.TrimSpace(text)
	if m == nil || key == "" {
		vectors, err := compute()
		if ended := ctx.Err(); ended != nil {
			return QueryEmbeddings{}, ended
		}
		return cloneQueryEmbeddings(vectors), err
	}
	for {
		if err := ctx.Err(); err != nil {
			return QueryEmbeddings{}, err
		}
		m.mu.Lock()
		if err := ctx.Err(); err != nil {
			m.mu.Unlock()
			return QueryEmbeddings{}, err
		}
		if vectors, ok := m.entries[key]; ok {
			m.mu.Unlock()
			return cloneQueryEmbeddings(vectors), nil
		}
		call, waiting := m.pending[key]
		if !waiting {
			call = &queryEmbeddingCall{done: make(chan struct{})}
			if m.pending == nil {
				m.pending = make(map[string]*queryEmbeddingCall)
			}
			m.pending[key] = call
			m.mu.Unlock()

			// Provider work stays under this caller's context, outside the lock.
			vectors, err := compute()
			m.mu.Lock()
			if ended := ctx.Err(); ended != nil {
				vectors, err = QueryEmbeddings{}, ended
				call.callerEnded = true
			}
			call.vectors, call.err = cloneQueryEmbeddings(vectors), err
			if err == nil {
				if m.entries == nil {
					m.entries = make(map[string]QueryEmbeddings)
				}
				m.entries[key] = call.vectors
			}
			delete(m.pending, key)
			close(call.done)
			m.mu.Unlock()
			return cloneQueryEmbeddings(call.vectors), call.err
		}
		m.mu.Unlock()
		select {
		case <-ctx.Done():
			return QueryEmbeddings{}, ctx.Err()
		case <-call.done:
			if err := ctx.Err(); err != nil {
				return QueryEmbeddings{}, err
			}
			if call.callerEnded {
				continue
			}
			return cloneQueryEmbeddings(call.vectors), call.err
		}
	}
}

func cloneQueryEmbeddings(in QueryEmbeddings) QueryEmbeddings {
	return QueryEmbeddings{
		Code: append(embeddings.Embedding(nil), in.Code...),
		Note: append(embeddings.Embedding(nil), in.Note...),
	}
}
