package repoconfig

import (
	"errors"
	"fmt"
	"strings"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	"github.com/atomicobject/rhizome/pkg/embeddings"
	"github.com/atomicobject/rhizome/pkg/paths"
)

// Resolved is one operation's repository configuration snapshot. Derivations use
// the same parsed local document; callers reload explicitly when configuration changes.
type Resolved struct {
	VaultPath              string
	Local                  *LocalConfig
	Code                   codeanchor.Config
	NoteEmbeddings         embeddings.Config
	CodeEmbeddings         embeddings.Config
	CodeEmbeddingsExplicit bool
	Graph                  LocalGraphConfig
	FileContext            LocalFileContextConfig
	BudgetChars            int
}

// Load reads repository configuration once and derives each subsystem's settings.
// Missing local configuration uses defaults. Other errors remain visible to callers.
func Load(vaultPath string) (Resolved, error) {
	var local *LocalConfig
	var err error
	if strings.TrimSpace(vaultPath) != "" {
		local, err = LoadLocalConfig(vaultPath)
	}
	if errors.Is(err, ErrNoLocalConfig) {
		err = nil
	}
	return FromLocal(vaultPath, local), err
}

// FromLocal derives subsystem settings without reading configuration files.
func FromLocal(vaultPath string, local *LocalConfig) Resolved {
	vaultPath = configurationRoot(vaultPath)
	r := Resolved{VaultPath: configurationRoot(vaultPath), Local: local, Code: codeanchor.DefaultConfig(vaultPath), NoteEmbeddings: embeddings.DefaultConfig(vaultPath), CodeEmbeddings: embeddings.DefaultCodeConfig(vaultPath), FileContext: FileContextConfigFromLocalOrDefault(local), BudgetChars: local.EffectiveBudgetChars()}
	if local == nil {
		return r
	}
	indexPath := UnifiedIndexPath(vaultPath, local.IndexPath)
	r.Code.IndexPath = indexPath
	r.Code = mergeLocalCodeToAnchorConfig(r.Code, local.Code)
	r.NoteEmbeddings.IndexPath = indexPath
	if local.NoteEmbeddings != nil {
		cfg := *local.NoteEmbeddings
		cfg.IndexPath = ""
		cfg.BatchSize = 0
		cfg.MaxConcurrency = 0
		r.NoteEmbeddings = r.NoteEmbeddings.Merge(cfg)
		r.NoteEmbeddings.Enabled = local.NoteEmbeddings.Enabled
		if cfg.Provider != "" {
			r.CodeEmbeddings.Provider = cfg.Provider
		}
		if cfg.Endpoint != "" {
			r.CodeEmbeddings.Endpoint = cfg.Endpoint
		}
	}
	r.NoteEmbeddings = embeddings.ApplyProviderDefaults(r.NoteEmbeddings)
	r.CodeEmbeddings.IndexPath = indexPath
	if local.CodeEmbeddings != nil {
		cfg := *local.CodeEmbeddings
		cfg.IndexPath = ""
		cfg.BatchSize = 0
		cfg.MaxConcurrency = 0
		r.CodeEmbeddings = r.CodeEmbeddings.Merge(cfg)
		r.CodeEmbeddings.Enabled = local.CodeEmbeddings.Enabled
		r.CodeEmbeddingsExplicit = true
	}
	r.CodeEmbeddings = embeddings.ApplyProviderDefaultsForKind(r.CodeEmbeddings, "code")
	if local.Graph != nil {
		r.Graph = LocalGraphConfig{Ignore: append([]string(nil), local.Graph.Ignore...), KeyNotePatterns: append([]string(nil), local.Graph.KeyNotePatterns...), AuthorityFactors: append([]AuthorityFactorRule(nil), local.Graph.AuthorityFactors...)}
	}
	return r
}

// CodeEmbeddingsFor applies runtime inheritance to the same configuration snapshot.
func (r Resolved) CodeEmbeddingsFor(semantic embeddings.Config) (embeddings.Config, bool) {
	cfg := r.CodeEmbeddings
	if !r.CodeEmbeddingsExplicit {
		cfg = embeddings.InheritCodeSettings(cfg, semantic)
	}
	cfg.IndexPath = UnifiedIndexPath(r.VaultPath, cfg.IndexPath)
	return embeddings.ApplyProviderDefaultsForKind(cfg, "code"), r.CodeEmbeddingsExplicit
}

// ValidateEmbeddingsProvider rejects enabled embedding blocks without a provider.
func (r Resolved) ValidateEmbeddingsProvider() error {
	if r.Local == nil {
		return nil
	}
	var missing []string
	if r.Local.NoteEmbeddings != nil && r.Local.NoteEmbeddings.Enabled && strings.TrimSpace(r.Local.NoteEmbeddings.Provider) == "" {
		missing = append(missing, "noteEmbeddings")
	}
	if r.Local.CodeEmbeddings != nil && r.Local.CodeEmbeddings.Enabled && strings.TrimSpace(r.Local.CodeEmbeddings.Provider) == "" {
		missing = append(missing, "codeEmbeddings")
	}
	if len(missing) > 0 {
		return fmt.Errorf("%w: %s (run 'rzm init' to configure)", ErrEmbeddingsProviderNotConfigured, strings.Join(missing, ", "))
	}
	return nil
}

func configurationRoot(vaultPath string) string {
	if strings.TrimSpace(vaultPath) == "" {
		return ""
	}
	vaultPaths, _ := paths.NewVaultPaths(vaultPath)
	return vaultPaths.Root()
}

// ValidateFor rejects a snapshot resolved for another repository.
func (r Resolved) ValidateFor(vaultPath string) error {
	if r.VaultPath != configurationRoot(vaultPath) {
		return fmt.Errorf("configuration belongs to %q, not %q", r.VaultPath, vaultPath)
	}
	return nil
}
