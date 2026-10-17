package repoconfig

import "github.com/atomicobject/rhizome/pkg/embeddings"

// SaveCodeEmbeddingsConfig updates codeEmbeddings while avoiding persisted defaults.
// Provider is always preserved; model/endpoint are only persisted when non-default
// or already present in the config.
func SaveCodeEmbeddingsConfig(vaultPath string, cfg embeddings.Config) error {
	localCfg, err := LoadLocalConfig(vaultPath)
	if err != nil {
		return err
	}

	// Update enabled/provider, and persist explicit overrides without writing defaults.
	updated := mergeEmbeddingsForSave(localCfg.CodeEmbeddings, cfg, "code")
	localCfg.CodeEmbeddings = &updated

	return SaveLocalConfig(vaultPath, *localCfg)
}
