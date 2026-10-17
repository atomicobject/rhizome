package cmd

import (
	"fmt"
	"os"
	"strings"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	"github.com/atomicobject/rhizome/pkg/app/oneshotruntime"
	"github.com/atomicobject/rhizome/pkg/indexstore"
	semdb "github.com/atomicobject/rhizome/pkg/store"
	"github.com/atomicobject/rhizome/pkg/vault/repoconfig"
)

func requireIntelStore(vaultPath string, codeCfg codeanchor.Config, access indexstore.AccessMode) (*semdb.Store, func(), error) {
	if !codeCfg.Enabled {
		return nil, nil, fmt.Errorf("code index not enabled for this vault; run `rzm code enable`, then `rzm index`")
	}
	store, cleanup, err := indexstore.Open(vaultPath, indexstore.Options{Access: access, Config: &codeCfg, RequireEnabled: true})
	if err != nil {
		return nil, nil, err
	}
	if store == nil {
		indexPath := repoconfig.UnifiedIndexPath(vaultPath, codeCfg.IndexPath)
		return nil, nil, fmt.Errorf("code index missing at %s (run `rzm index`)", indexPath)
	}
	return store, cleanup, nil
}

func openOptionalIntelStoreForAnalysis(vaultPath string) (*semdb.Store, func(), error) {
	basePath := strings.TrimSpace(vaultPath)
	if basePath == "" {
		return nil, nil, nil
	}
	info, err := os.Stat(basePath)
	if err != nil || !info.IsDir() {
		return nil, nil, nil
	}
	resolvedConfig1, configErr1 := repoconfig.Load(basePath)
	codeCfg, err := resolvedConfig1.Code, configErr1
	if err != nil {
		return nil, nil, err
	}
	return indexstore.Open(basePath, indexstore.Options{Access: indexstore.ReadOnly, Config: &codeCfg, RequireEnabled: false})
}

func openOptionalIntelStoreForPlan(vaultPath string, plan oneshotruntime.Plan) (*semdb.Store, func(), error) {
	if err := plan.Validate(); err != nil {
		return nil, nil, err
	}
	if plan.StoreAccess == oneshotruntime.StoreNone {
		return nil, nil, nil
	}
	if plan.StoreAccess != oneshotruntime.StoreLiveReadWrite && plan.StoreAccess != oneshotruntime.StoreExistingReadOnly {
		return nil, nil, fmt.Errorf("optional analysis store requires indexed read access, got %q", plan.StoreAccess)
	}
	return openOptionalIntelStoreForAnalysis(vaultPath)
}
