package validate

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"testing"

	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type repairPostApplyProbe struct {
	t               *testing.T
	lease           *IndexLockLease
	changed         []string
	renamed         []PathRename
	deleted         []string
	result          PostApplyRefreshResult
	refreshErr      error
	allowNilRuntime bool
	// vault and otherVault, when set, are checked against the borrowed lease
	// while the engine still holds it.
	vault      string
	otherVault string
}

type scopedRepairPostApplyProbe struct {
	repairPostApplyProbe
	scope PostApplyRefreshScope
}

func (p *scopedRepairPostApplyProbe) RefreshScoped(
	ctx context.Context,
	lease *IndexLockLease,
	scope PostApplyRefreshScope,
) (PostApplyRefreshResult, error) {
	p.scope = scope
	return p.Refresh(ctx, lease, scope.Changed, scope.Renamed, scope.Deleted)
}

func (p *repairPostApplyProbe) Refresh(
	_ context.Context,
	lease *IndexLockLease,
	changed []string,
	renamed []PathRename,
	deleted []string,
) (PostApplyRefreshResult, error) {
	p.t.Helper()
	require.NoError(p.t, lease.RequireHeld(), "refresh must borrow the engine-owned held lease")
	if p.vault != "" {
		require.NoError(p.t, lease.RequireHeldForVault(p.vault), "the borrowed lease must authorize its own vault")
		require.ErrorContains(p.t, lease.RequireHeldForVault(p.otherVault), "different vault",
			"the borrowed lease must not authorize another vault")
	}
	p.lease = lease
	p.changed = append([]string(nil), changed...)
	p.renamed = append([]PathRename(nil), renamed...)
	p.deleted = append([]string(nil), deleted...)
	result := p.result
	if result.Runtime == nil && p.refreshErr == nil && !p.allowNilRuntime {
		prepared := NewPostApplyRefreshResult(&ontology.Runtime{}, repairNoopCloseOwner{})
		prepared.Domains, prepared.Paths, prepared.Timings = result.Domains, result.Paths, result.Timings
		result = prepared
	}
	return result, p.refreshErr
}

type repairNoopCloseOwner struct{}

func (repairNoopCloseOwner) Close() error { return nil }

func TestApplyFixPlanRefreshesExactCommittedDeltaUnderOneLease(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "changed.md"), []byte("before\n"), 0o640))
	require.NoError(t, os.WriteFile(filepath.Join(root, "old.md"), []byte("rename me\n"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(root, "remove.md"), []byte("delete me\n"), 0o644))
	require.NoError(t, os.Mkdir(filepath.Join(root, "moved"), 0o755))

	plan := mustFinalizePostApplyPlan(t,
		repairWriteOperation("op-write", "action-write", "changed.md", []byte("before\n"), []byte("after\n")),
		RepairOperation{
			ID:              "op-rename",
			ActionID:        "action-rename",
			IssueKey:        "issue-rename",
			Kind:            RepairOperationRename,
			Path:            "old.md",
			DestinationPath: "moved/new.md",
			SourceHash:      SourceHash([]byte("rename me\n")),
			Lifecycle:       LifecyclePolicyResult{Decision: LifecycleNotHistorical},
		},
		RepairOperation{
			ID:         "op-delete",
			ActionID:   "action-delete",
			IssueKey:   "issue-delete",
			Kind:       RepairOperationDelete,
			Path:       "remove.md",
			SourceHash: SourceHash([]byte("delete me\n")),
			Lifecycle:  LifecyclePolicyResult{Decision: LifecycleNotHistorical},
		},
	)
	probe := &repairPostApplyProbe{
		t: t, vault: root, otherVault: t.TempDir(),
		result: PostApplyRefreshResult{
			Domains: []string{"ontology", "note-metadata"},
			Paths:   []string{"changed.md", "moved/new.md", "old.md", "remove.md"},
		},
	}

	execution, err := ApplyFixPlan(context.Background(), RunContext{VaultPath: root}, &plan, Options{
		Fix:                true,
		NonInteractive:     true,
		PostApplyRefresher: probe,
	})
	require.NoError(t, err)
	require.NotNil(t, execution)

	assert.Equal(t, []string{"changed.md"}, probe.changed)
	assert.Equal(t, []PathRename{{From: "old.md", To: "moved/new.md"}}, probe.renamed)
	assert.Equal(t, []string{"remove.md"}, probe.deleted)
	require.NotNil(t, probe.lease)
	require.Error(t, probe.lease.RequireHeld(), "engine must invalidate the borrowed lease before returning")
	require.Error(t, probe.lease.RequireHeldForVault(root), "a released lease must not authorize its vault")
	assert.NoFileExists(t, filepath.Join(root, ".rhizome", "index.lock"))

	changed, err := os.ReadFile(filepath.Join(root, "changed.md"))
	require.NoError(t, err)
	assert.Equal(t, "after\n", string(changed))
	renamed, err := os.ReadFile(filepath.Join(root, "moved", "new.md"))
	require.NoError(t, err)
	assert.Equal(t, "rename me\n", string(renamed))
	assert.NoFileExists(t, filepath.Join(root, "old.md"))
	assert.NoFileExists(t, filepath.Join(root, "remove.md"))
}

func TestRefreshFailureRetainsCommittedJournalForReadOnlyDetection(t *testing.T) {
	root := t.TempDir()
	before := []byte("before\n")
	after := []byte("after\n")
	require.NoError(t, os.WriteFile(filepath.Join(root, "note.md"), before, 0o640))
	plan := mustFinalizePostApplyPlan(t,
		repairWriteOperation("op-write", "action-write", "note.md", before, after),
	)
	refreshErr := errors.New("refresh barrier failed")
	probe := &repairPostApplyProbe{t: t, refreshErr: refreshErr}
	runCtx := RunContext{VaultPath: root, NoteMetadata: testNoteMetadata(t)}

	execution, err := ApplyFixPlan(context.Background(), runCtx, &plan, Options{
		Fix:                true,
		NonInteractive:     true,
		PostApplyRefresher: probe,
	})
	require.ErrorIs(t, err, refreshErr)
	require.NotNil(t, execution, "post-commit refresh failure must retain execution evidence")
	require.NotNil(t, probe.lease)
	require.Error(t, probe.lease.RequireHeld(), "lease must still be released on refresh failure")

	committed, err := os.ReadFile(filepath.Join(root, "note.md"))
	require.NoError(t, err)
	assert.Equal(t, after, committed, "refresh failure happens after the filesystem commit")
	journalRoot := filepath.Join(root, ".rhizome", "repair-journal", "v1")
	beforeReadOnly := repairJournalTree(t, journalRoot)
	require.NotEmpty(t, beforeReadOnly, "committed journal must remain recoverable until refresh succeeds")

	evidence, err := DetectPendingRepairJournals(runCtx)
	require.NoError(t, err)
	require.Len(t, evidence, 1)
	assert.Equal(t, plan.Transactions[0].ID, evidence[0].TransactionID)
	assert.Equal(t, plan.Fingerprint, evidence[0].PlanFingerprint)
	assert.Equal(t, "committed", evidence[0].State)
	assert.Equal(t, []string{"note.md"}, evidence[0].AffectedPaths)
	assert.Equal(t, beforeReadOnly, repairJournalTree(t, journalRoot), "journal detection must be read-only")

	result, _, err := RunSuiteOnce(context.Background(), Options{
		Checks:     []string{CheckViews},
		MaxIssues:  1,
		RunContext: &runCtx,
	})
	require.NoError(t, err)
	assert.Equal(t, evidence, result.RepairJournals)
	assert.Equal(t, beforeReadOnly, repairJournalTree(t, journalRoot), "read-only validation must not recover or delete journals")
}

func TestApplyRepairSessionRejectsNilPreparedRuntimeWithoutLegacyFallback(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, writeFixtureFiles(root, map[string]string{
		".rhizome/ontology/schema.graphql": `
type SpecSection implements Section { notes: String @field }
type Spec @node(paths: ["notes/*.md"], propertyCase: AS_DEFINED) {
  name: String!
  requirements: SpecSection @contains(level: H2, heading: "Requirements", required: true)
}
`,
		"notes/spec.md": "---\ntype: Spec\nname: Missing\n---\n\n# Missing\n",
	}))
	runCtx := RunContext{
		VaultDef: obsidian.VaultDefinition{Path: root}, VaultPath: root,
		NoteReader: &obsidian.Note{}, NoteMetadata: testNoteMetadata(t), MaxIssues: 20,
	}
	probe := &repairPostApplyProbe{t: t, allowNilRuntime: true}

	planned, scanCtx, err := RunSuiteOnce(context.Background(), Options{
		Checks: []string{CheckOntology}, RunContext: &runCtx,
	})
	require.NoError(t, err)
	require.NotNil(t, planned.FixPlan, "the missing required section must produce a reviewed repair")

	_, execution, err := ApplyRepairSession(context.Background(), planned, scanCtx, Options{
		Fix: true, NonInteractive: true, PostApplyRefresher: probe,
	})
	require.ErrorContains(t, err, "prepared post-apply runtime is absent")
	require.NotNil(t, execution, "the committed repair must report its execution evidence")
	evidence, detectErr := DetectPendingRepairJournals(runCtx)
	require.NoError(t, detectErr)
	assert.NotEmpty(t, evidence, "fail-closed prepared mode retains committed evidence")
}

func repairWriteOperation(id, actionID, path string, before, after []byte) RepairOperation {
	return RepairOperation{
		ID:         id,
		ActionID:   actionID,
		IssueKey:   "issue-" + id,
		Kind:       RepairOperationWrite,
		Path:       path,
		SourceHash: SourceHash(before),
		Expected: []ExpectedText{{
			StartByte:   0,
			EndByte:     len(before),
			Text:        string(before),
			Replacement: string(after),
		}},
		Content:   append([]byte(nil), after...),
		Lifecycle: LifecyclePolicyResult{Decision: LifecycleNotHistorical},
	}
}

func mustFinalizePostApplyPlan(t *testing.T, operations ...RepairOperation) RepairPlan {
	t.Helper()
	actions := make([]FixAction, 0, len(operations))
	issueKeys := make([]string, 0, len(operations))
	for _, operation := range operations {
		issueKeys = append(issueKeys, operation.IssueKey)
		actions = append(actions, FixAction{
			ID:        operation.ActionID,
			Check:     CheckLinkHygiene,
			Safety:    FixSafetySafe,
			IssueKeys: []string{operation.IssueKey},
		})
	}
	plan, err := FinalizeRepairPlan(RepairPlan{
		TotalCount: len(actions),
		SafeCount:  len(actions),
		IssueKeys:  issueKeys,
		Actions:    actions,
		Operations: operations,
	})
	require.NoError(t, err)
	return plan
}

func repairJournalTree(t *testing.T, root string) []string {
	t.Helper()
	var paths []string
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		paths = append(paths, filepath.ToSlash(rel))
		return nil
	})
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	require.NoError(t, err)
	sort.Strings(paths)
	return paths
}
