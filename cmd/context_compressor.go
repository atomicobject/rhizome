package cmd

import (
	"context"
	"github.com/atomicobject/rhizome/pkg/app/contextpack"
	"github.com/atomicobject/rhizome/pkg/app/contextpack/compress"
	vaultconfig "github.com/atomicobject/rhizome/pkg/vault/config"
	"github.com/atomicobject/rhizome/pkg/vault/repoconfig"
)

func (commandOptions *vaultCommandOptions) loadContextCompressor(ctx context.Context, vaultPath string) contextpack.Compressor {
	var cfg *compress.LocalCompressionConfig
	if localCfg, err := repoconfig.LoadLocalConfig(vaultPath); err == nil && localCfg != nil && localCfg.Compression != nil {
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
	compressor := compress.NewFromLocalConfig(vaultconfig.FromContext(ctx), cfg, commandOptions.debug)
	if compressor == nil {
		return nil
	}
	return compressor
}
