package embeddings

import (
	"context"
	"io"
	"path/filepath"
	"reflect"
	"testing"
)

type recordingProvider struct {
	calls [][]string
	dims  int
}

func (p *recordingProvider) Dimensions() int { return p.dims }

func (p *recordingProvider) EmbedTexts(_ context.Context, texts []string) ([]Embedding, error) {
	p.calls = append(p.calls, append([]string(nil), texts...))
	out := make([]Embedding, len(texts))
	for i, text := range texts {
		vec := make(Embedding, p.dims)
		for j := range vec {
			vec[j] = float32(len(text)) + float32(j)*0.125 + float32(text[0])/3
		}
		out[i] = vec
	}
	return out, nil
}

// closeProvider releases the cache's SQLite handle. Windows cannot delete an
// open file, so a leaked handle fails t.TempDir cleanup.
func closeProvider(t *testing.T, provider Provider) {
	t.Helper()
	closer, ok := provider.(io.Closer)
	if !ok {
		t.Errorf("cached provider %T must expose Close", provider)
		return
	}
	if err := closer.Close(); err != nil {
		t.Errorf("close cached provider: %v", err)
	}
}

func TestCachedProviderReusesStoredVectors(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "cache.sqlite")
	inner := &recordingProvider{dims: 4}
	provider, err := NewCachedProvider(inner, ProviderConfig{Provider: "Voyage", Model: "voyage-3", Dimensions: 4}, path)
	if err != nil {
		t.Fatalf("new cached provider: %v", err)
	}
	defer closeProvider(t, provider)

	first, err := provider.EmbedTexts(ctx, []string{"alpha", "beta"})
	if err != nil {
		t.Fatalf("first embed: %v", err)
	}
	if len(inner.calls) != 1 || !reflect.DeepEqual(inner.calls[0], []string{"alpha", "beta"}) {
		t.Fatalf("expected one inner call with both texts, got %v", inner.calls)
	}

	second, err := provider.EmbedTexts(ctx, []string{"beta", "gamma"})
	if err != nil {
		t.Fatalf("second embed: %v", err)
	}
	if len(inner.calls) != 2 || !reflect.DeepEqual(inner.calls[1], []string{"gamma"}) {
		t.Fatalf("expected second inner call with only the miss, got %v", inner.calls)
	}
	if !reflect.DeepEqual(second[0], first[1]) {
		t.Fatalf("cached vector round-trip mismatch: %v vs %v", second[0], first[1])
	}

	otherInner := &recordingProvider{dims: 4}
	other, err := NewCachedProvider(otherInner, ProviderConfig{Provider: "Voyage", Model: "voyage-other", Dimensions: 4}, path)
	if err != nil {
		t.Fatalf("new cached provider (other model): %v", err)
	}
	defer closeProvider(t, other)
	if _, err := other.EmbedTexts(ctx, []string{"alpha"}); err != nil {
		t.Fatalf("other model embed: %v", err)
	}
	if len(otherInner.calls) != 1 || !reflect.DeepEqual(otherInner.calls[0], []string{"alpha"}) {
		t.Fatalf("a different model config must not reuse cached rows, got %v", otherInner.calls)
	}
}
