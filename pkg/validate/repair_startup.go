package validate

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/atomicobject/rhizome/pkg/paths"
)

// StabilizePendingRepairJournals restores every interrupted prepared
// transaction before startup readers can observe a partial connected write.
// Committed and restored journals remain for the normal repair-session refresh,
// postcheck, and cleanup boundary once projection dependencies are available.
// Later edits to committed files do not block startup or get overwritten.
func StabilizePendingRepairJournals(runCtx RunContext) error {
	return stabilizePendingRepairJournals(runCtx, nil)
}

// StabilizePendingRepairJournalsContext waits for an earlier writer to finish
// before the newly elected runtime inspects repair journals. Other callers
// retain the nonblocking form above so they cannot wait on their own lease.
func StabilizePendingRepairJournalsContext(ctx context.Context, runCtx RunContext) error {
	return stabilizePendingRepairJournals(runCtx, ctx)
}

func stabilizePendingRepairJournals(runCtx RunContext, ctx context.Context) error {
	root := strings.TrimSpace(runCtx.VaultPath)
	if root == "" {
		root = strings.TrimSpace(runCtx.VaultDef.BasePath())
	}
	vaultPaths, err := paths.NewVaultPaths(root)
	if err != nil || vaultPaths.Root() == "" {
		if err == nil {
			err = fmt.Errorf("vault root is required")
		}
		return fmt.Errorf("resolve repair startup vault: %w", err)
	}
	runCtx.VaultPath = vaultPaths.Root()
	lockPath := filepath.Join(vaultPaths.Root(), ".rhizome", "index.lock")
	var lease *IndexLockLease
	var release func() error
	if ctx == nil {
		lease, release, err = acquireRepairIndexLockLease(lockPath)
	} else {
		lease, release, err = waitRepairIndexLockLease(ctx, lockPath)
	}
	if err != nil {
		return err
	}
	defer func() { _ = release() }()
	if err := lease.RequireHeld(); err != nil {
		return err
	}
	journals, err := discoverRepairJournals(runCtx)
	if err != nil {
		return err
	}
	_, err = stabilizeRepairJournals(runCtx, journals)
	return err
}
