package codeintel

import (
	"fmt"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	"github.com/atomicobject/rhizome/pkg/indexstore"
	semdb "github.com/atomicobject/rhizome/pkg/store"
	"github.com/atomicobject/rhizome/pkg/vault/repoconfig"
)

func OpenOptionalIntelStore(vaultPath string, codeCfg codeanchor.Config) (*semdb.Store, func(), string, error) {
	if !codeCfg.Enabled {
		return nil, nil, "Warning: code index disabled for this vault; proceeding without intel.", nil
	}
	store, cleanup, err := indexstore.Open(vaultPath, indexstore.Options{Access: indexstore.ReadOnly, Config: &codeCfg, RequireEnabled: true})
	if err != nil {
		return nil, nil, "", err
	}
	if store == nil {
		if cleanup != nil {
			cleanup()
		}
		path := repoconfig.UnifiedIndexPath(vaultPath, codeCfg.IndexPath)
		return nil, nil, fmt.Sprintf("Warning: code index missing at %s; proceeding without intel.", path), nil
	}
	return store, cleanup, "", nil
}
