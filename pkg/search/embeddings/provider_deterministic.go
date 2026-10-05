package embeddings

// Docs: [Embeddings - providers + configuration](docs/reference/guides/Embeddings - providers + configuration.md)

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
)

const defaultDeterministicDims = 8

// deterministicProvider is a test/dev helper that returns stable embeddings without network calls.
type deterministicProvider struct {
	dims int
}

// NewDeterministicProvider constructs a provider that hashes text into fixed-size vectors.
func NewDeterministicProvider(cfg ProviderConfig) Provider {
	dims := cfg.Dimensions
	if dims <= 0 {
		dims = defaultDeterministicDims
	}
	return &deterministicProvider{dims: dims}
}

func (p *deterministicProvider) Dimensions() int {
	return p.dims
}

func (p *deterministicProvider) EmbedTexts(ctx context.Context, texts []string) ([]Embedding, error) {
	vecs := make([]Embedding, len(texts))
	for i, t := range texts {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}
		vecs[i] = p.hashToVec(t)
	}
	return vecs, nil
}

func (p *deterministicProvider) hashToVec(text string) Embedding {
	sum := sha256.Sum256([]byte(text))
	vec := make(Embedding, p.dims)
	// Fill vec using 4-byte chunks of hash material repeated as needed.
	for i := 0; i < p.dims; i++ {
		offset := (i * 4) % len(sum)
		chunk := binary.BigEndian.Uint32(sum[offset : offset+4])
		// Map to [-1,1] range to keep magnitudes reasonable.
		vec[i] = float32(int32(chunk)) / 2_147_483_648.0
	}
	return vec
}
