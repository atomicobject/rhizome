package actions

import (
	"context"
	"os"
	"strings"

	semdb "github.com/atomicobject/rhizome/pkg/anchors/sqlite"
	"github.com/atomicobject/rhizome/pkg/notemeta"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
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
	cfg, err := obsidian.LoadCodeConfig(basePath)
	if err != nil {
		return nil, nil, err
	}
	if strings.TrimSpace(obsidian.UnifiedIndexPath(basePath, cfg.IndexPath)) == "" {
		return nil, nil, nil
	}
	return obsidian.OpenIntelStoreForWriteFromConfig(basePath, cfg)
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
