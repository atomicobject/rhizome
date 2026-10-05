package indexing

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	semdb "github.com/atomicobject/rhizome/pkg/anchors/sqlite"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
)

// PrintStatus renders index configuration plus the live coordination state a
// user needs when indexing is contended: the vault runtime and the index lock.
func PrintStatus(ctx context.Context, vaultPath string) (string, error) {
	embCfg, embErr := obsidian.LoadEmbeddingsConfig(vaultPath)
	codeCfg, codeErr := obsidian.LoadCodeConfig(vaultPath)
	if embErr != nil && codeErr != nil {
		return "", fmt.Errorf("load configs: semantic=%v code=%v", embErr, codeErr)
	}

	var b strings.Builder
	if embErr == nil {
		fmt.Fprintf(&b, "Semantic index:\n")
		fmt.Fprintf(&b, "  Enabled: %v\n", embCfg.Enabled)
		fmt.Fprintf(&b, "  Path:    %s\n", embCfg.IndexPath)
		fmt.Fprintf(&b, "  Provider:%s model:%s\n", embCfg.Provider, embCfg.Model)
	}
	if codeErr == nil {
		fmt.Fprintf(&b, "Code index:\n")
		fmt.Fprintf(&b, "  Enabled:   %v\n", codeCfg.Enabled)
		fmt.Fprintf(&b, "  Path:      %s\n", codeCfg.IndexPath)
		switch {
		case !codeCfg.Enabled:
		case codeCfg.AutomaticScope:
			fmt.Fprintf(&b, "  Folders:   whole repository (no folder limits; language from each file's extension)\n")
		default:
			fmt.Fprintf(&b, "  Folders:   %s\n", strings.Join(codeCfg.CodeRoots(), ", "))
		}
	}
	writeRuntimeStatus(ctx, &b, vaultPath)
	writeIndexLockStatus(&b, vaultPath)
	return b.String(), nil
}

func CleanupSessions(ctx context.Context, vaultPath string, verbose bool, out io.Writer) {
	if out == nil {
		out = os.Stderr
	}

	embCfg, err := obsidian.LoadEmbeddingsConfig(vaultPath)
	if err != nil {
		if verbose {
			fmt.Fprintf(out, "Warning: session cleanup skipped: %v\n", err)
		}
		return
	}
	embCfg.IndexPath = obsidian.UnifiedIndexPath(vaultPath, embCfg.IndexPath)
	indexCfg := codeanchor.Config{IndexPath: embCfg.IndexPath, Enabled: true}
	store, cleanup, err := obsidian.OpenIntelStoreForWriteFromConfig(vaultPath, indexCfg)
	if err != nil {
		if verbose {
			fmt.Fprintf(out, "Warning: session cleanup skipped: %v\n", err)
		}
		return
	}
	defer cleanup()

	removed, err := store.CleanupSessions(ctx, time.Now().Add(-semdb.DefaultSessionRetention))
	if err != nil {
		if verbose {
			fmt.Fprintf(out, "Warning: session cleanup failed: %v\n", err)
		}
		return
	}
	if verbose && removed > 0 {
		fmt.Fprintf(out, "Session cleanup removed %d sessions\n", removed)
	}
}
