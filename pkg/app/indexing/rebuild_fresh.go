package indexing

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	appruntime "github.com/atomicobject/rhizome/pkg/app/runtime"
	"github.com/atomicobject/rhizome/pkg/vault/indexlock"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
)

// PrepareFreshRebuild clobbers the current SQLite index database so the next
// indexing pass recreates every domain from scratch. Callers must hold the
// vault's index lock; selecting --rebuild is the operator's authorization to
// remove the database and its WAL/SHM sidecars.
//
// It refuses while a vault runtime is live: the database must never be
// unlinked under an open handle, so a rebuild stops the runtime first
// (SPEC-0104 US2).
func PrepareFreshRebuild(vaultPath, _ string) error {
	if err := refuseRebuildUnderLiveRuntime(vaultPath); err != nil {
		return err
	}
	paths, err := fullRebuildPaths(vaultPath)
	if err != nil {
		return err
	}
	for _, path := range paths {
		fmt.Fprintf(os.Stderr, "Indexes: clobbering %s for full rebuild\n", path)
		if err := removeSQLiteIndex(path); err != nil {
			return fmt.Errorf("prepare full rebuild at %s: %w", path, err)
		}
	}
	fmt.Fprintln(os.Stderr, "Indexes: rebuilding from scratch")
	return nil
}

func refuseRebuildUnderLiveRuntime(vaultPath string) error {
	manifest, err := appruntime.ReadManifest(vaultPath)
	if err != nil {
		// No manifest, or an unreadable one: no runtime to protect.
		return nil
	}
	if manifest.PID <= 0 || manifest.PID == os.Getpid() || !indexlock.PIDExists(manifest.PID) {
		return nil
	}
	return fmt.Errorf(
		"a vault runtime (pid %d, %s) has this index open; stop it with `rzm stop` before rebuilding",
		manifest.PID, manifest.Mode,
	)
}

func removeSQLiteIndex(path string) error {
	for _, candidate := range []string{path + "-wal", path + "-shm", path} {
		if err := os.Remove(candidate); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("remove %s: %w", candidate, err)
		}
	}
	return nil
}

func fullRebuildPaths(vaultPath string) ([]string, error) {
	embCfg, err := obsidian.LoadEmbeddingsConfig(vaultPath)
	if err != nil {
		return nil, err
	}
	codeCfg, codeErr := obsidian.LoadCodeConfig(vaultPath)
	if codeErr != nil {
		codeCfg = codeanchor.DefaultConfig(vaultPath)
	}
	codeEmbCfg, _, err := obsidian.EffectiveCodeEmbeddingsConfig(vaultPath, embCfg)
	if err != nil {
		return nil, err
	}

	embCfg.IndexPath = obsidian.UnifiedIndexPath(vaultPath, embCfg.IndexPath)
	codeCfg.IndexPath = obsidian.UnifiedIndexPath(vaultPath, codeCfg.IndexPath)
	codeEmbCfg.IndexPath = obsidian.UnifiedIndexPath(vaultPath, codeEmbCfg.IndexPath)

	unique := map[string]struct{}{}
	add := func(path string) {
		if path == "" {
			return
		}
		unique[filepath.Clean(path)] = struct{}{}
	}

	add(embCfg.IndexPath)
	add(codeCfg.IndexPath)
	add(codeEmbCfg.IndexPath)

	paths := make([]string, 0, len(unique))
	for path := range unique {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	return paths, nil
}
