package cmd

import (
	"github.com/atomicobject/rhizome/pkg/app/contextpack"
	"github.com/atomicobject/rhizome/pkg/app/contextpack/compress"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
)

func loadContextCompressor(vaultPath string) contextpack.Compressor {
	var cfg *compress.LocalCompressionConfig
	if localCfg, err := obsidian.LoadLocalConfig(vaultPath); err == nil && localCfg != nil && localCfg.Compression != nil {
		cfg = &compress.LocalCompressionConfig{
			Enabled:               localCfg.Compression.Enabled,
			Provider:              localCfg.Compression.Provider,
			Model:                 localCfg.Compression.Model,
			TimeoutMS:             localCfg.Compression.TimeoutMS,
			MaxInputTokens:        localCfg.Compression.MaxInputTokens,
			MaxOutputTokens:       localCfg.Compression.MaxOutputTokens,
			ReasoningTokenReserve: localCfg.Compression.ReasoningTokenReserve,
			ChunkChars:            localCfg.Compression.ChunkChars,
			Parallelism:           localCfg.Compression.Parallelism,
			ReasoningEffort:       localCfg.Compression.ReasoningEffort,
		}
	}
	compressor := compress.NewFromLocalConfig(cfg, debug)
	if compressor == nil {
		return nil
	}
	return compressor
}
