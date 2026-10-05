package unifiedsearch

import (
	"context"
	"testing"

	"github.com/atomicobject/rhizome/pkg/search/embeddings"
	"github.com/stretchr/testify/require"
)

type intentSelectionProvider struct{ name string }

func (*intentSelectionProvider) EmbedTexts(context.Context, []string) ([]embeddings.Embedding, error) {
	return nil, nil
}

func (*intentSelectionProvider) Dimensions() int { return 2 }

func TestProviderConfigsMatch(t *testing.T) {
	base := embeddings.ProviderConfig{
		Provider:   "openai",
		Model:      "text-embedding-3-small",
		Endpoint:   "https://api.openai.com/v1/embeddings",
		Dimensions: 1536,
		APIKey:     "k1",
	}

	t.Run("match with provider case/space differences", func(t *testing.T) {
		other := base
		other.Provider = " OpenAI "
		other.Model = " text-embedding-3-small "
		other.Endpoint = "https://api.openai.com/v1/embeddings"
		require.True(t, providerConfigsMatch(base, other))
	})

	t.Run("different model does not match", func(t *testing.T) {
		other := base
		other.Model = "text-embedding-3-large"
		require.False(t, providerConfigsMatch(base, other))
	})

	t.Run("different dimensions does not match", func(t *testing.T) {
		other := base
		other.Dimensions = 3072
		require.False(t, providerConfigsMatch(base, other))
	})

	t.Run("different api key does not match", func(t *testing.T) {
		other := base
		other.APIKey = "k2"
		require.False(t, providerConfigsMatch(base, other))
	})
}

func TestSelectIntentProviderPrefersCodeThenFallsBackToNote(t *testing.T) {
	noteProvider := &intentSelectionProvider{name: "note"}
	codeProvider := &intentSelectionProvider{name: "code"}
	noteCfg := embeddings.ProviderConfig{Provider: "test", Model: "note", Dimensions: 2}
	codeCfg := embeddings.ProviderConfig{Provider: "test", Model: "code", Dimensions: 2}

	provider, cfg := SelectIntentProvider(noteProvider, noteCfg, codeProvider, codeCfg)
	require.Same(t, codeProvider, provider)
	require.Equal(t, codeCfg, cfg)

	provider, cfg = SelectIntentProvider(noteProvider, noteCfg, nil, embeddings.ProviderConfig{})
	require.Same(t, noteProvider, provider)
	require.Equal(t, noteCfg, cfg)
}
