package semantic

import (
	"context"
	"errors"
	"testing"

	"github.com/atomicobject/rhizome/pkg/indexingperf"
	"github.com/atomicobject/rhizome/pkg/search/embeddings"
	"github.com/stretchr/testify/require"
)

// mockProvider is a simple mock for testing.
type mockProvider struct {
	dims      int
	embedding embeddings.Embedding
	err       error
	calls     int
}

func (m *mockProvider) EmbedTexts(ctx context.Context, texts []string) ([]embeddings.Embedding, error) {
	m.calls++
	if m.err != nil {
		return nil, m.err
	}
	result := make([]embeddings.Embedding, len(texts))
	for i := range texts {
		result[i] = m.embedding
	}
	return result, nil
}

func (m *mockProvider) Dimensions() int {
	return m.dims
}

func TestQueryEmbedder_BothProvidersNil(t *testing.T) {
	embedder := QueryEmbedder{}
	result, err := embedder.Embed(context.Background(), "test query")
	require.NoError(t, err)
	require.Nil(t, result.Code)
	require.Nil(t, result.Note)
}

func TestQueryEmbedder_SameProvider(t *testing.T) {
	prov := &mockProvider{
		dims:      4,
		embedding: embeddings.Embedding{0.1, 0.2, 0.3, 0.4},
	}

	embedder := QueryEmbedder{
		CodeProvider: prov,
		NoteProvider: prov, // Same instance
	}

	collector := indexingperf.NewSemanticQueryCollector()
	result, err := embedder.Embed(indexingperf.WithCollector(context.Background(), collector), "test query")
	require.NoError(t, err)

	// Both should have the same embedding.
	require.NotNil(t, result.Code)
	require.NotNil(t, result.Note)
	foundProviderCalls := false
	for _, operation := range collector.SemanticQueryDiagnostics().Operations {
		if operation.Label == indexingperf.SemanticQueryOpProviderCalls {
			foundProviderCalls = true
			require.True(t, operation.Available)
			require.EqualValues(t, 1, operation.Count)
			break
		}
	}
	require.True(t, foundProviderCalls, "provider-call diagnostic must be emitted")
	require.Equal(t, result.Code, result.Note)

	// Should only be called once (optimization).
	require.Equal(t, 1, prov.calls)
}

func TestQueryEmbedder_ContinuationMemoReusesExactProviderOutput(t *testing.T) {
	prov := &mockProvider{dims: 2, embedding: embeddings.Embedding{0.1, 0.2}}
	ctx, memo := WithQueryEmbeddingMemo(context.Background(), nil)
	embedder := QueryEmbedder{CodeProvider: prov, NoteProvider: prov}

	first, err := embedder.Embed(ctx, " query ")
	require.NoError(t, err)
	prov.embedding = embeddings.Embedding{0.8, 0.9}
	second, err := embedder.Embed(ctx, "query")
	require.NoError(t, err)

	require.Equal(t, first, second)
	require.Equal(t, 1, prov.calls)
	require.Equal(t, []QueryEmbeddingRecord{{Text: "query", Code: embeddings.Embedding{0.1, 0.2}, Note: embeddings.Embedding{0.1, 0.2}}}, memo.Snapshot())

	continued, _ := WithQueryEmbeddingMemo(context.Background(), memo.Snapshot())
	third, err := embedder.Embed(continued, "query")
	require.NoError(t, err)
	require.Equal(t, first, third)
	require.Equal(t, 1, prov.calls)
}

func TestQueryEmbedder_DifferentProviders(t *testing.T) {
	for _, tc := range []struct {
		name       string
		code, note bool
	}{
		{"different providers", true, true},
		{"code only", true, false},
		{"note only", false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			codeProv := &mockProvider{dims: 4, embedding: embeddings.Embedding{0.1, 0.2, 0.3, 0.4}}
			noteProv := &mockProvider{dims: 8, embedding: embeddings.Embedding{0.5, 0.6, 0.7, 0.8, 0.9, 1.0, 1.1, 1.2}}
			embedder := QueryEmbedder{}
			if tc.code {
				embedder.CodeProvider = codeProv
			}
			if tc.note {
				embedder.NoteProvider = noteProv
			}
			result, err := embedder.Embed(context.Background(), "test query")
			require.NoError(t, err)
			if tc.code {
				require.Equal(t, codeProv.embedding, result.Code)
				require.Equal(t, 1, codeProv.calls)
			} else {
				require.Nil(t, result.Code)
				require.Zero(t, codeProv.calls)
			}
			if tc.note {
				require.Equal(t, noteProv.embedding, result.Note)
				require.Equal(t, 1, noteProv.calls)
			} else {
				require.Nil(t, result.Note)
				require.Zero(t, noteProv.calls)
			}
		})
	}
}

func TestQueryEmbedder_CodeProviderError(t *testing.T) {
	for _, tc := range []struct {
		name, message                string
		codeError, noteError, shared bool
		wantCodeCalls, wantNoteCalls int
	}{
		{"code fails", "code embedding failed", true, false, false, 1, 1},
		{"note fails", "note embedding failed", false, true, false, 1, 1},
		{"shared fails", "shared embedding failed", false, false, true, 1, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			codeProv := &mockProvider{dims: 4, embedding: embeddings.Embedding{0.1, 0.2, 0.3, 0.4}}
			noteProv := &mockProvider{dims: 4, embedding: embeddings.Embedding{0.5, 0.6, 0.7, 0.8}}
			if tc.codeError {
				codeProv.err = errors.New(tc.message)
			}
			if tc.noteError {
				noteProv.err = errors.New(tc.message)
			}
			if tc.shared {
				codeProv.err = errors.New(tc.message)
				noteProv = codeProv
			}
			embedder := QueryEmbedder{CodeProvider: codeProv, NoteProvider: noteProv}
			_, err := embedder.Embed(context.Background(), "test query")
			require.ErrorContains(t, err, tc.message)
			require.Equal(t, tc.wantCodeCalls, codeProv.calls)
			require.Equal(t, tc.wantNoteCalls, noteProv.calls)
		})
	}
}
