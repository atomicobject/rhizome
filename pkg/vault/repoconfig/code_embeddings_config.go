package repoconfig

import (
	"context"

	"github.com/atomicobject/rhizome/pkg/embeddings"
)

// SaveCodeEmbeddingsConfig updates codeEmbeddings while avoiding persisted defaults.
// Provider is always preserved; model/endpoint are only persisted when non-default
// or already present in the config.
func SaveCodeEmbeddingsConfig(vaultPath string, cfg embeddings.Config) error {
	return UpdateLocalConfig(context.Background(), vaultPath, func(localCfg *LocalConfig) error {
		updated := mergeEmbeddingsForSave(localCfg.CodeEmbeddings, cfg, "code")
		localCfg.CodeEmbeddings = &updated
		return nil
	})
}
