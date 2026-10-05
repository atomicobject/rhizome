package codeintel

import (
	"fmt"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	semdb "github.com/atomicobject/rhizome/pkg/anchors/sqlite"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
)

func OpenOptionalIntelStore(vaultPath string, codeCfg codeanchor.Config) (*semdb.Store, func(), string, error) {
	if !codeCfg.Enabled {
		return nil, nil, "Warning: code index disabled for this vault; proceeding without intel.", nil
	}
	store, cleanup, err := obsidian.OpenIntelStoreFromConfig(vaultPath, codeCfg, true)
	if err != nil {
		return nil, nil, "", err
	}
	if store == nil {
		if cleanup != nil {
			cleanup()
		}
		path := obsidian.UnifiedIndexPath(vaultPath, codeCfg.IndexPath)
		return nil, nil, fmt.Sprintf("Warning: code index missing at %s; proceeding without intel.", path), nil
	}
	return store, cleanup, "", nil
}
