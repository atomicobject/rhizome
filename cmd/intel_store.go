package cmd

import (
	"fmt"
	"os"
	"strings"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	semdb "github.com/atomicobject/rhizome/pkg/anchors/sqlite"
	"github.com/atomicobject/rhizome/pkg/app/oneshotruntime"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
)

func requireIntelStore(vaultPath string, codeCfg codeanchor.Config) (*semdb.Store, func(), error) {
	if !codeCfg.Enabled {
		return nil, nil, fmt.Errorf("code index not enabled for this vault; run `rzm code enable`, then `rzm index`")
	}
	store, cleanup, err := obsidian.OpenIntelStoreFromConfig(vaultPath, codeCfg, true)
	if err != nil {
		return nil, nil, err
	}
	if store == nil {
		indexPath := obsidian.UnifiedIndexPath(vaultPath, codeCfg.IndexPath)
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
	codeCfg, err := obsidian.LoadCodeConfig(basePath)
	if err != nil {
		return nil, nil, err
	}
	return obsidian.OpenIntelStoreFromConfig(basePath, codeCfg, false)
}

// openOptionalIntelStoreForPlan keeps direct human services aligned with the
// one-shot declaration. The generic opener preserves its established schema
// and migration behavior, so callers must declare live read-write access.
func openOptionalIntelStoreForPlan(vaultPath string, plan oneshotruntime.Plan) (*semdb.Store, func(), error) {
	if err := plan.Validate(); err != nil {
		return nil, nil, err
	}
	if plan.StoreAccess == oneshotruntime.StoreNone {
		return nil, nil, nil
	}
	if plan.StoreAccess != oneshotruntime.StoreLiveReadWrite {
		return nil, nil, fmt.Errorf("optional analysis store requires live read-write access, got %q", plan.StoreAccess)
	}
	return openOptionalIntelStoreForAnalysis(vaultPath)
}
