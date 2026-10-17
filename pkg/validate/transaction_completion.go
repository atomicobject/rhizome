package validate

import (
	"context"
	"errors"
	"fmt"

	"path/filepath"
	"strings"

	"github.com/atomicobject/rhizome/pkg/paths"
	"github.com/atomicobject/rhizome/pkg/sourcetxn"
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
	if err := lease.Validate(runCtx.VaultPath); err != nil {
		return err
	}
	runCtx.sourceLease = lease

	writer, recovered, recoveryErr := sourcetxn.Open(runCtx.VaultPath, lease, sourcetxn.WriterOptions{Role: sourcetxn.RepairOwner, RepairPolicy: validateSourceJournalChecks})
	if writer != nil {
		defer writer.Close()
	}
	runCtx.sourceWriter = writer
	recovery := repairRecovery(recovered)
	if recoveryErr != nil || len(recovery.native)+len(recovery.nativeCleanup)+len(recovery.committed)+len(recovery.rolledBack)+len(recovery.noMutation)+len(recovery.partial)+len(recovery.cleanupPending) > 0 {
		return errors.Join(recoveryErr, fmt.Errorf("repair journal recovery requires completion before publishing evidence"))
	}

	transactionID := "completion:" + SourceHash(append([]byte(filepath.Clean(artifact.Path)+"\x00"), artifact.Content...))
	transaction := RepairTransaction{ID: transactionID, Checks: []string{CheckOntology}}
	prepared, err := prepareRepairTransaction(runCtx, SourceHash(artifact.Content), transaction, nil, false, artifact)
	if err != nil {
		return err
	}
	publication, err := writer.Publish(prepared)
	if err != nil {
		return err
	}
	return writer.Complete(publication)
}

func cloneRepairCompletionArtifact(artifact *RepairCompletionArtifact) *RepairCompletionArtifact {
	if artifact == nil {
		return nil
	}
	return &RepairCompletionArtifact{Path: artifact.Path, Content: append([]byte(nil), artifact.Content...)}
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
