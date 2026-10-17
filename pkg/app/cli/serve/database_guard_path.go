package serve

import (
	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	"github.com/atomicobject/rhizome/pkg/vault/repoconfig"
)

// indexDatabasePath resolves the unified SQLite database this runtime opens.
func indexDatabasePath(vaultPath string) string {
	resolvedConfig1, configErr1 := repoconfig.Load(vaultPath)
	cfg, err := resolvedConfig1.Code, configErr1
	if err != nil {
		cfg = codeanchor.DefaultConfig(vaultPath)
	}
	return repoconfig.UnifiedIndexPath(vaultPath, cfg.IndexPath)
}
