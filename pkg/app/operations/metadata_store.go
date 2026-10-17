package operations

import (
	"context"
	"os"
	"strings"

	"github.com/atomicobject/rhizome/pkg/indexstore"
	"github.com/atomicobject/rhizome/pkg/notemeta"
	semdb "github.com/atomicobject/rhizome/pkg/store"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/atomicobject/rhizome/pkg/vault/repoconfig"
)

func openableVaultBasePath(vaultPath string) string {
	basePath := strings.TrimSpace(vaultPath)
	if basePath == "" {
		return ""
	}
	info, err := os.Stat(basePath)
	if err != nil || !info.IsDir() {
		return ""
	}
	return basePath
}

func openMetadataStore(vaultDef obsidian.VaultDefinition) (*semdb.Store, func(), error) {
	basePath := openableVaultBasePath(vaultDef.BasePath())
	if basePath == "" {
		return nil, nil, nil
	}
	resolvedConfig1, configErr1 := repoconfig.Load(basePath)
	cfg, err := resolvedConfig1.Code, configErr1
	if err != nil {
		return nil, nil, err
	}
	if strings.TrimSpace(repoconfig.UnifiedIndexPath(basePath, cfg.IndexPath)) == "" {
		return nil, nil, nil
	}
	return indexstore.Open(basePath, indexstore.Options{Access: indexstore.ReadWrite, Config: &cfg, Create: true})
}

func metadataStoreReady(ctx context.Context, vaultDef obsidian.VaultDefinition, noteMgr obsidian.NoteReader, store *semdb.Store) bool {
	_ = vaultDef
	_ = noteMgr
	if store == nil {
		return false
	}
	ready, err := notemeta.MetadataStateReady(ctx, store)
	return err == nil && ready
}
