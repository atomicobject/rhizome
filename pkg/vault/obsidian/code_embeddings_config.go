package obsidian

import "errors"
import "github.com/atomicobject/rhizome/pkg/search/embeddings"

// LoadCodeEmbeddingsConfig returns code-embeddings config merged with defaults for the vault.
// The returned boolean indicates whether a codeEmbeddings block was explicitly configured.
// The canonical codeEmbeddings block may inherit provider/endpoint from noteEmbeddings.
func LoadCodeEmbeddingsConfig(vaultPath string) (embeddings.Config, bool, error) {
	localCfg, err := LoadLocalConfig(vaultPath)
	if err != nil {
		if errors.Is(err, ErrNoLocalConfig) {
			return embeddings.DefaultCodeConfig(vaultPath), false, nil
		}
		return embeddings.Config{}, false, err
	}

	base := embeddings.DefaultCodeConfig(vaultPath)
	base.IndexPath = unifiedIndexPath(vaultPath, localCfg.IndexPath)

	if localCfg.NoteEmbeddings != nil {
		if localCfg.NoteEmbeddings.Provider != "" {
			base.Provider = localCfg.NoteEmbeddings.Provider
		}
		if localCfg.NoteEmbeddings.Endpoint != "" {
			base.Endpoint = localCfg.NoteEmbeddings.Endpoint
		}
	}

	if localCfg.CodeEmbeddings != nil {
		withoutPath := *localCfg.CodeEmbeddings
		withoutPath.IndexPath = ""
		withoutPath.BatchSize = 0
		withoutPath.MaxConcurrency = 0
		base = base.Merge(withoutPath)
		base.Enabled = localCfg.CodeEmbeddings.Enabled
		return embeddings.ApplyProviderDefaultsForKind(base, "code"), true, nil
	}
	return embeddings.ApplyProviderDefaultsForKind(base, "code"), false, nil
}

// EffectiveCodeEmbeddingsConfig returns the code embeddings config with runtime inheritance applied.
// When the code embeddings block isn't explicit, it inherits settings from the provided semantic config.
func EffectiveCodeEmbeddingsConfig(vaultPath string, semanticCfg embeddings.Config) (embeddings.Config, bool, error) {
	codeCfg, explicit, err := LoadCodeEmbeddingsConfig(vaultPath)
	if err != nil {
		return embeddings.Config{}, false, err
	}
	if !explicit {
		codeCfg = embeddings.InheritCodeSettings(codeCfg, semanticCfg)
	}
	codeCfg = embeddings.ApplyProviderDefaultsForKind(codeCfg, "code")
	codeCfg.IndexPath = UnifiedIndexPath(vaultPath, codeCfg.IndexPath)
	return codeCfg, explicit, nil
}

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
