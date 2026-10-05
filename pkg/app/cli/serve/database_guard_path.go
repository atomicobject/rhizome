package serve

import (
	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
)

// indexDatabasePath resolves the unified SQLite database this runtime opens.
func indexDatabasePath(vaultPath string) string {
	cfg, err := obsidian.LoadCodeConfig(vaultPath)
	if err != nil {
		cfg = codeanchor.DefaultConfig(vaultPath)
	}
	return obsidian.UnifiedIndexPath(vaultPath, cfg.IndexPath)
}
