package embedindex

import "github.com/atomicobject/rhizome/pkg/embeddings"

func derivedPackerOptions(provider embeddings.Provider, batchSize int, node *SharedEmbeddingNode, opts EmbedPackerOptions) EmbedPackerOptions {
	opts = ApplyProviderPackerLimits(provider, opts)
	opts = applyExplicitBatchSize(opts, batchSize)
	if node != nil {
		cfg := node.config()
		opts.MaxTexts = min(opts.MaxTexts, cfg.opts.MaxTexts)
		opts.MaxBytes = min(opts.MaxBytes, cfg.opts.MaxBytes)
	}
	opts.MinTexts = opts.MaxTexts
	opts.AdaptiveMinTexts = 0
	opts.FlushOnSubmit = true
	if node != nil {
		node.Configure(node.config().maxConcurrent, opts)
	}
	return opts
}
