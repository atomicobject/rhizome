package semanticruntime

import (
	"io"
	"path/filepath"
	"testing"

	"github.com/atomicobject/rhizome/pkg/search/embeddings"
	"github.com/stretchr/testify/require"
)

func TestSharedLanesBorrowCachedProviders(t *testing.T) {
	cfg := embeddings.ProviderConfig{Provider: "test", Model: "synthetic", Dimensions: 4}
	newProvider := func() embeddings.Provider {
		provider, err := embeddings.NewCachedProvider(embeddings.NewDeterministicProvider(cfg), cfg, filepath.Join(t.TempDir(), "cache.sqlite"))
		require.NoError(t, err)
		t.Cleanup(func() { require.NoError(t, provider.(io.Closer).Close()) })
		return provider
	}
	noteProvider, codeProvider := newProvider(), newProvider()
	runtime := New()
	notes, err := runtime.EnsureLane(t.Context(), LaneRequest{Kind: LaneKindNote, Provider: noteProvider, ProviderInfo: cfg})
	require.NoError(t, err)
	code, err := runtime.EnsureLane(t.Context(), LaneRequest{Kind: LaneKindCode, Provider: codeProvider, ProviderInfo: cfg})
	require.NoError(t, err)
	require.Same(t, notes.Node, code.Node)
	runtime.Close()
	runtime.Close()
	for _, provider := range []embeddings.Provider{noteProvider, codeProvider} {
		_, err = provider.EmbedTexts(t.Context(), []string{"caller still owns cache"})
		require.NoError(t, err)
		require.NoError(t, provider.(io.Closer).Close())
		_, err = provider.EmbedTexts(t.Context(), []string{"caller explicitly closed cache"})
		require.ErrorContains(t, err, "database is closed")
	}
}
