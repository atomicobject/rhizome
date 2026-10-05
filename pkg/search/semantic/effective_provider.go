package semantic

import "github.com/atomicobject/rhizome/pkg/search/embeddings"

type defaultBatchSizeProvider interface {
	DefaultBatchSize() int
}

type defaultMaxConcurrencyProvider interface {
	DefaultMaxConcurrency() int
}

type defaultMaxBatchBytesProvider interface {
	DefaultMaxBatchBytes() int
}

// EffectiveBatchSize resolves an explicit override, then the provider default,
// then Rhizome's generic fallback.
func EffectiveBatchSize(provider embeddings.Provider, override int) int {
	if override > 0 {
		return override
	}
	if p, ok := provider.(defaultBatchSizeProvider); ok {
		if v := p.DefaultBatchSize(); v > 0 {
			return v
		}
	}
	return embeddings.DefaultBatchSize
}

// EffectiveMaxConcurrent resolves an explicit override, then the provider
// default, then Rhizome's generic fallback.
func EffectiveMaxConcurrent(provider embeddings.Provider, override int) int {
	if override > 0 {
		return override
	}
	if p, ok := provider.(defaultMaxConcurrencyProvider); ok {
		if v := p.DefaultMaxConcurrency(); v > 0 {
			return v
		}
	}
	return embeddings.DefaultMaxConcurrent
}

// ApplyProviderPackerLimits raises full-scan packer ceilings to the provider's
// advertised request limits while preserving watcher/low-latency packers.
func ApplyProviderPackerLimits(provider embeddings.Provider, opts EmbedPackerOptions) EmbedPackerOptions {
	opts = normalizeEmbedPackerOptions(opts)
	if opts.MinTexts < 256 || opts.MaxTexts < 512 {
		return opts
	}
	if v := EffectiveBatchSize(provider, 0); v > opts.MaxTexts {
		opts.MaxTexts = v
	}
	if p, ok := provider.(defaultMaxBatchBytesProvider); ok {
		if v := p.DefaultMaxBatchBytes(); v > opts.MaxBytes {
			opts.MaxBytes = v
		}
	}
	if opts.MinTexts > opts.MaxTexts {
		opts.MinTexts = opts.MaxTexts
	}
	adaptiveFloor := opts.AdaptiveMinTexts
	if adaptiveFloor <= 0 {
		adaptiveFloor = FullScanAdaptivePackerFloor
	}
	if opts.MaxTexts >= adaptiveFloor && opts.MinTexts < adaptiveFloor {
		opts.MinTexts = adaptiveFloor
	}
	return opts
}
