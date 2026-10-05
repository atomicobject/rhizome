package validate

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/atomicobject/rhizome/pkg/paths"
	"github.com/atomicobject/rhizome/pkg/validate/namespaceadmission"
)

const repairCompletionDirectory = ".rhizome/edit-receipts"

// PublishRepairCompletionArtifact durably publishes completion evidence when
// a request has no source mutation to carry the artifact. It uses the same
// lease, path validation, journal, and recovery contract as repair writes.
func PublishRepairCompletionArtifact(ctx context.Context, runCtx RunContext, artifact *RepairCompletionArtifact) (resultErr error) {
	artifact = cloneRepairCompletionArtifact(artifact)
	if artifact == nil {
		return nil
	}
	root := strings.TrimSpace(runCtx.VaultPath)
	if root == "" {
		root = strings.TrimSpace(runCtx.VaultDef.BasePath())
	}
	vaultPaths, err := paths.NewVaultPaths(root)
	if err != nil || vaultPaths.Root() == "" {
		if err == nil {
			err = fmt.Errorf("vault root is required")
		}
		return fmt.Errorf("resolve repair completion vault: %w", err)
	}
	target, _, err := repairCompletionTarget(runCtx, artifact.Path)
	if err != nil {
		return err
	}
	artifact.Path = target
	runCtx.VaultPath = vaultPaths.Root()
	lease, release, err := waitRepairIndexLockLease(ctx, filepath.Join(vaultPaths.Root(), ".rhizome", "index.lock"))
	if err != nil {
		return err
	}
	defer func() { resultErr = errors.Join(resultErr, release()) }()
	if err := lease.RequireHeld(); err != nil {
		return err
	}
	if err := namespaceadmission.Check(runCtx.VaultPath); err != nil {
		return err
	}

	recovery, recoveryErr := recoverRepairJournals(runCtx)
	if recoveryErr != nil || len(recovery.native)+len(recovery.nativeCleanup)+len(recovery.committed)+len(recovery.rolledBack)+len(recovery.noMutation)+len(recovery.partial)+len(recovery.cleanupPending) > 0 {
		return errors.Join(recoveryErr, fmt.Errorf("repair journal recovery requires completion before publishing evidence"))
	}

	transactionID := "completion:" + SourceHash(append([]byte(filepath.Clean(artifact.Path)+"\x00"), artifact.Content...))
	transaction := RepairTransaction{ID: transactionID, Checks: []string{CheckOntology}}
	prepared, err := prepareRepairTransaction(runCtx, SourceHash(artifact.Content), transaction, nil, false, nil, artifact)
	if err != nil {
		return err
	}
	if err := commitPreparedRepairTransaction(runCtx, prepared, nil); err != nil {
		var rollbackErr error
		if prepared.installed > 0 {
			_, rollbackErr = rollbackRepairJournal(runCtx, prepared.manifest)
			if rollbackErr == nil {
				rollbackErr = cleanupRepairJournal(runCtx, prepared.dir, prepared.manifest)
			}
		}
		return errors.Join(err, rollbackErr)
	}
	if err := verifyCommittedRepairJournal(runCtx, prepared.manifest); err != nil {
		return err
	}
	return cleanupRepairJournal(runCtx, prepared.dir, prepared.manifest)
}

func cloneRepairCompletionArtifact(artifact *RepairCompletionArtifact) *RepairCompletionArtifact {
	if artifact == nil {
		return nil
	}
	return &RepairCompletionArtifact{Path: artifact.Path, Content: append([]byte(nil), artifact.Content...)}
}

func prepareRepairCompletionState(runCtx RunContext, artifact *RepairCompletionArtifact) (*repairFileState, error) {
	if artifact == nil {
		return nil, nil
	}
	target, rel, err := repairCompletionTarget(runCtx, artifact.Path)
	if err != nil {
		return nil, err
	}
	if err := ensureRepairCompletionParent(runCtx, target, rel); err != nil {
		return nil, err
	}
	if _, err := os.Lstat(target); err == nil {
		return nil, fmt.Errorf("repair completion artifact already exists: %s", rel)
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("inspect repair completion artifact %s: %w", rel, err)
	}
	return &repairFileState{
		rel:          rel,
		abs:          target,
		originalRel:  rel,
		internal:     true,
		finalExists:  true,
		finalContent: append([]byte(nil), artifact.Content...),
		finalMode:    0o600,
	}, nil
}

func repairCompletionTarget(runCtx RunContext, raw string) (string, string, error) {
	if strings.TrimSpace(raw) == "" {
		return "", "", fmt.Errorf("repair completion artifact target is required")
	}
	if !filepath.IsAbs(raw) {
		return "", "", fmt.Errorf("repair completion artifact target must be absolute")
	}
	root := strings.TrimSpace(runCtx.VaultPath)
	if root == "" {
		root = strings.TrimSpace(runCtx.VaultDef.BasePath())
	}
	vaultPaths, err := paths.NewVaultPaths(root)
	if err != nil || vaultPaths.Root() == "" {
		if err == nil {
			err = fmt.Errorf("vault root is required")
		}
		return "", "", fmt.Errorf("resolve repair completion vault: %w", err)
	}
	canonicalRel, err := vaultPaths.RelStrict(filepath.Clean(raw))
	if err != nil {
		return "", "", fmt.Errorf("repair completion artifact escaped vault: %w", err)
	}
	rel := canonicalRel.String()
	prefix := repairCompletionDirectory + "/"
	if rel == repairCompletionDirectory || !strings.HasPrefix(filepath.ToSlash(rel), prefix) {
		return "", "", fmt.Errorf("repair completion artifact must be beneath %s", repairCompletionDirectory)
	}
	canonicalCtx := runCtx
	canonicalCtx.VaultPath = vaultPaths.Root()
	target, err := repairAbsPath(canonicalCtx, rel)
	if err != nil {
		return "", "", fmt.Errorf("resolve repair completion artifact %s: %w", rel, err)
	}
	allowed := filepath.Join(vaultPaths.Root(), filepath.FromSlash(repairCompletionDirectory))
	relToAllowed, err := filepath.Rel(allowed, target)
	if err != nil || relToAllowed == "." || filepath.IsAbs(relToAllowed) || strings.HasPrefix(relToAllowed, ".."+string(filepath.Separator)) || relToAllowed == ".." {
		return "", "", fmt.Errorf("repair completion artifact must be beneath %s", repairCompletionDirectory)
	}
	return target, rel, nil
}

func ensureRepairCompletionParent(runCtx RunContext, target, rel string) error {
	root := strings.TrimSpace(runCtx.VaultPath)
	if root == "" {
		root = strings.TrimSpace(runCtx.VaultDef.BasePath())
	}
	vaultPaths, err := paths.NewVaultPaths(root)
	if err != nil || vaultPaths.Root() == "" {
		if err == nil {
			err = fmt.Errorf("vault root is required")
		}
		return fmt.Errorf("resolve repair completion vault: %w", err)
	}
	base := filepath.Join(vaultPaths.Root(), filepath.FromSlash(repairCompletionDirectory))
	if err := ensureRepairDirectDirectory(filepath.Dir(base), 0o755); err != nil {
		return err
	}
	if err := ensureRepairDirectDirectory(base, 0o700); err != nil {
		return err
	}
	parent := filepath.Dir(target)
	relParent := filepath.Dir(filepath.FromSlash(rel))
	extra := strings.TrimPrefix(filepath.ToSlash(relParent), repairCompletionDirectory)
	extra = strings.TrimPrefix(extra, "/")
	current := base
	for _, component := range strings.Split(extra, "/") {
		if component == "" || component == "." {
			continue
		}
		current = filepath.Join(current, component)
		if err := ensureRepairDirectDirectory(current, 0o700); err != nil {
			return err
		}
	}
	if filepath.Clean(current) != filepath.Clean(parent) {
		return fmt.Errorf("repair completion artifact parent changed during preparation")
	}
	return nil
}

func ensureRepairDirectDirectory(dir string, mode os.FileMode) error {
	info, err := os.Lstat(dir)
	if errors.Is(err, os.ErrNotExist) {
		if err := os.Mkdir(dir, mode); err != nil && !errors.Is(err, os.ErrExist) {
			return fmt.Errorf("create repair completion directory %s: %w", dir, err)
		}
		info, err = os.Lstat(dir)
	}
	if err != nil {
		return fmt.Errorf("inspect repair completion directory %s: %w", dir, err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return fmt.Errorf("repair completion path must be a direct directory: %s", dir)
	}
	return nil
}

func completionArtifactMatchesManifest(runCtx RunContext, artifact *RepairCompletionArtifact, manifest repairJournalManifest) bool {
	if artifact == nil {
		return false
	}
	_, rel, err := repairCompletionTarget(runCtx, artifact.Path)
	if err != nil {
		return false
	}
	wantHash := SourceHash(artifact.Content)
	for _, entry := range manifest.Entries {
		if entry.Internal && entry.Path == rel && entry.FinalExists && entry.FinalMode == 0o600 && entry.FinalHash == wantHash {
			return true
		}
	}
	return false
}
