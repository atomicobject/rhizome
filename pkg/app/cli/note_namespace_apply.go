package actions

import (
	"context"

	"github.com/atomicobject/rhizome/pkg/notemeta"
	"github.com/atomicobject/rhizome/pkg/paths"
	"github.com/atomicobject/rhizome/pkg/validate"
	"github.com/atomicobject/rhizome/pkg/vault/coderefs"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
)

func applyNoteNamespace(ctx context.Context, vault obsidian.VaultManager, metadata notemeta.Indexer, refresher validate.PostApplyRefresher, moves []MoveRequest, overwrite, backlinks bool, codeRefConfig *coderefs.Config) (MoveSummary, error) {
	summary := MoveSummary{Mutation: validate.NamespaceMutationResult{Current: validate.NamespaceOutcome{Decision: validate.NamespaceNotStarted}}}
	if ctx == nil {
		ctx = context.Background()
	}
	root, err := vault.Path()
	if err != nil {
		return summary, err
	}
	vaultPaths, err := paths.NewVaultPaths(root)
	if err != nil {
		return summary, err
	}
	root = vaultPaths.Root()
	definition, err := vault.Definition()
	if err != nil {
		return summary, err
	}
	planner := &noteNamespacePlanner{
		root: root, definition: definition, metadata: metadata, moves: moves,
		overwrite: overwrite, updateBacklinks: backlinks,
	}
	reader := &obsidian.Note{}
	run := validate.RunContext{VaultPath: root, VaultDef: definition, VaultMgr: vault, NoteMetadata: metadata, NoteReader: reader}
	var rewritten coderefs.RewriteResult
	var postCommit validate.NamespacePostCommit
	if backlinks && codeRefConfig != nil && codeRefConfig.Enabled {
		postCommit = func(ctx context.Context, lease *validate.IndexLockLease) {
			if ctx.Err() != nil || lease.RequireHeldForVault(root) != nil {
				return
			}
			if result, err := coderefs.RewriteBatch(root, codeRefConfig, planner.codeMappings); err == nil {
				rewritten = result
			}
		}
	}
	mutation, applyErr := validate.ApplyNamespaceMutation(ctx, run, planner, refresher, postCommit)
	summary.Mutation = mutation
	if mutation.Current.Decision == validate.NamespaceCommitted {
		planned := planner.summary
		summary.Results = planned.Results
		summary.TotalLinkUpdates = planned.TotalLinkUpdates
		summary.Skipped = planned.Skipped
		summary.HeadingPointers = planned.HeadingPointers
		summary.TotalCodeRefUpdates, summary.CodeFilesUpdated = rewritten.RefsUpdated, rewritten.FilesUpdated
		for i := range summary.Results {
			for _, move := range mutation.Current.GitMoves {
				if move.From == summary.Results[i].Source && move.To == summary.Results[i].Target {
					summary.Results[i].GitHistoryPreserved = true
					break
				}
			}
		}
	}
	return summary, applyErr
}

func namespaceOptionalEffectsReady(result validate.NamespaceMutationResult, err error) bool {
	return err == nil && result.Current.Decision == validate.NamespaceCommitted && !result.Current.RecoveryPending
}
