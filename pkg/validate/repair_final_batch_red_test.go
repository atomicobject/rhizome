package validate

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestApplyFixPlanBlocksFoldEqualDistinctDestinationsOnCaseSensitiveFilesystem(t *testing.T) {
	root := t.TempDir()
	probe := filepath.Join(root, "CaseProbe")
	require.NoError(t, os.WriteFile(probe, []byte("probe"), 0o600))
	if _, err := os.Stat(filepath.Join(root, "caseprobe")); err == nil {
		t.Skip("filesystem is case-insensitive")
	}
	first := []byte("first\n")
	second := []byte("second\n")
	require.NoError(t, os.WriteFile(filepath.Join(root, "first.md"), first, 0o640))
	require.NoError(t, os.WriteFile(filepath.Join(root, "second.md"), second, 0o640))
	plan := mustRepairPlan(t, []repairPlanInput{
		{actionID: "action:fold-first", issueKey: "issue:fold-first", operations: []RepairOperation{{
			ID: "op-fold-first", Kind: RepairOperationRename, Path: "first.md",
			DestinationPath: "Target.md", SourceHash: SourceHash(first),
		}}},
		{actionID: "action:fold-second", issueKey: "issue:fold-second", operations: []RepairOperation{{
			ID: "op-fold-second", Kind: RepairOperationRename, Path: "second.md",
			DestinationPath: "target.md", SourceHash: SourceHash(second),
		}}},
	})
	require.Len(t, plan.Transactions, 1)
	require.NotEmpty(t, plan.Transactions[0].Conflicts)

	execution, err := ApplyFixPlan(context.Background(), repairRunContext(t, root), &plan, Options{
		Fix: true, NonInteractive: true,
	})
	require.NoError(t, err)
	require.Len(t, execution.Transactions, 1)
	assert.Equal(t, "skipped_conflict", execution.Transactions[0].Status)
	assert.Equal(t, first, mustReadFile(t, filepath.Join(root, "first.md")))
	assert.Equal(t, second, mustReadFile(t, filepath.Join(root, "second.md")))
	assert.NoFileExists(t, filepath.Join(root, "Target.md"))
	assert.NoFileExists(t, filepath.Join(root, "target.md"))
}

func TestApplyFixPlanRejectsOccupiedFoldEqualRenameDestinationUnlessSameFile(t *testing.T) {
	root := t.TempDir()
	sourcePath := filepath.Join(root, "Name.md")
	destinationPath := filepath.Join(root, "name.md")
	source := []byte("source\n")
	destination := []byte("distinct destination\n")
	require.NoError(t, os.WriteFile(sourcePath, source, 0o640))
	require.NoError(t, os.WriteFile(destinationPath, destination, 0o600))
	sourceInfo, err := os.Stat(sourcePath)
	require.NoError(t, err)
	destinationInfo, err := os.Stat(destinationPath)
	require.NoError(t, err)
	if os.SameFile(sourceInfo, destinationInfo) {
		t.Skip("filesystem is case-insensitive")
	}
	plan := mustRepairPlan(t, []repairPlanInput{{
		actionID: "action:occupied-fold-destination", issueKey: "issue:occupied-fold-destination",
		operations: []RepairOperation{{
			ID: "op-occupied-fold-destination", Kind: RepairOperationRename, Path: "Name.md",
			DestinationPath: "name.md", SourceHash: SourceHash(source),
		}},
	}})

	execution, err := ApplyFixPlan(context.Background(), repairRunContext(t, root), &plan, Options{
		Fix: true, NonInteractive: true,
	})
	require.ErrorContains(t, err, "destination collision")
	require.Len(t, execution.Transactions, 1)
	assert.Equal(t, "failed_prepare", execution.Transactions[0].Status)
	assert.Equal(t, source, mustReadFile(t, sourcePath))
	assert.Equal(t, destination, mustReadFile(t, destinationPath))
}

func TestCaseOnlyRenameDoesNotOverwritePreexistingCaseArtifact(t *testing.T) {
	root := t.TempDir()
	before := []byte("case source\n")
	require.NoError(t, os.WriteFile(filepath.Join(root, "Name.md"), before, 0o640))
	plan := mustRepairPlan(t, []repairPlanInput{{
		actionID: "action:case-artifact-collision", issueKey: "issue:case-artifact-collision",
		operations: []RepairOperation{{
			ID: "op:case-artifact-collision", Kind: RepairOperationRename, Path: "Name.md",
			DestinationPath: "name.md", SourceHash: SourceHash(before),
		}},
	}})
	runCtx := repairRunContext(t, root)
	states, err := composeRepairFileStates(runCtx, plan.Operations)
	require.NoError(t, err)
	shortID := strings.TrimPrefix(SourceHash([]byte(plan.Transactions[0].ID)), "sha256:")[:12]
	caseArtifact := ""
	for index, state := range states {
		if state.originalRel != "" && state.originalRel != state.rel {
			caseArtifact = repairArtifactPath(state.abs, shortID, index, "case-original")
		}
	}
	if caseArtifact == "" {
		t.Skip("filesystem does not require a case-only rename artifact")
	}
	unrelated := []byte("unrelated case artifact\n")
	require.NoError(t, os.WriteFile(caseArtifact, unrelated, 0o600))

	_, err = ApplyFixPlan(context.Background(), runCtx, &plan, Options{Fix: true, NonInteractive: true})
	require.ErrorIs(t, err, fs.ErrExist)
	assert.Equal(t, unrelated, mustReadFile(t, caseArtifact), "case-only staging must not overwrite an unowned artifact")
}

func TestPrepareArtifactExclusiveCollisionPreservesUnrelatedFileAndClassifiesFailure(t *testing.T) {
	root := t.TempDir()
	before := []byte("before\n")
	path := filepath.Join(root, "note.md")
	require.NoError(t, os.WriteFile(path, before, 0o640))
	plan := mustRepairPlan(t, []repairPlanInput{{
		actionID: "action:artifact-collision", issueKey: "issue:artifact-collision",
		operations: []RepairOperation{
			repairWriteOperation("op-artifact-collision", "action:artifact-collision", "note.md", before, []byte("after\n")),
		},
	}})
	shortID := strings.TrimPrefix(SourceHash([]byte(plan.Transactions[0].ID)), "sha256:")[:12]
	artifact := repairArtifactPath(path, shortID, 0, "stage")
	createdBeforeCollision := repairArtifactPath(path, shortID, 0, "backup")
	unrelated := []byte("unrelated preexisting artifact\n")
	require.NoError(t, os.WriteFile(artifact, unrelated, 0o600))
	runCtx := repairRunContext(t, root)

	execution, err := ApplyFixPlan(context.Background(), runCtx, &plan, Options{Fix: true, NonInteractive: true})
	require.Error(t, err)
	assert.ErrorIs(t, err, fs.ErrExist)
	assert.Equal(t, before, mustReadFile(t, path))
	assert.Equal(t, unrelated, mustReadFile(t, artifact), "O_EXCL collision must not consume an unrelated file")
	assert.FileExists(t, createdBeforeCollision, "ambiguous collision retains owned artifacts with durable evidence")
	require.Len(t, execution.Transactions, 1)
	assert.Equal(t, "failed_prepare", execution.Transactions[0].Status)
	evidence, detectErr := DetectPendingRepairJournals(runCtx)
	require.NoError(t, detectErr)
	require.Len(t, evidence, 1, "failed prepare retains evidence instead of inferring ownership from the path")
	assert.Equal(t, repairJournalPrepared, evidence[0].State)
}

func TestCommittedRecoveryRefusesReplacedOwnedArtifact(t *testing.T) {
	root := t.TempDir()
	before := []byte("before\n")
	path := filepath.Join(root, "note.md")
	require.NoError(t, os.WriteFile(path, before, 0o640))
	plan := mustRepairPlan(t, []repairPlanInput{{
		actionID: "action:replaced-owned-artifact", issueKey: "issue:replaced-owned-artifact",
		operations: []RepairOperation{
			repairWriteOperation("op:replaced-owned-artifact", "action:replaced-owned-artifact", "note.md", before, []byte("after\n")),
		},
	}})
	runCtx := repairRunContext(t, root)
	leaveJournal := errors.New("retain committed journal")

	_, err := ApplyFixPlan(context.Background(), runCtx, &plan, Options{
		Fix: true, NonInteractive: true,
		PostApplyRefresher: &repairPostApplyProbe{t: t, refreshErr: leaveJournal},
	})
	require.ErrorIs(t, err, leaveJournal)
	shortID := strings.TrimPrefix(SourceHash([]byte(plan.Transactions[0].ID)), "sha256:")[:12]
	backup := repairArtifactPath(path, shortID, 0, "backup")
	replacement := []byte("unrelated replacement\n")
	require.NoError(t, os.WriteFile(backup, replacement, 0o640))

	_, err = ApplyFixPlan(context.Background(), runCtx, &FixPlan{}, Options{
		Fix: true, NonInteractive: true, PostApplyRefresher: &repairPostApplyProbe{t: t},
	})
	require.ErrorContains(t, err, "artifact ownership changed")
	assert.Equal(t, replacement, mustReadFile(t, backup))
	evidence, detectErr := DetectPendingRepairJournals(runCtx)
	require.NoError(t, detectErr)
	require.Len(t, evidence, 1, "changed owned artifact and committed evidence must remain")
	assert.Equal(t, repairJournalCommitted, evidence[0].State)
}

func TestApplyFixPlanRejectsJournalRootSymlinkWithoutTouchingOutsideDigest(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink fixture requires elevated Windows privileges")
	}
	root := t.TempDir()
	outside := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, ".rhizome"), 0o755))
	require.NoError(t, os.Symlink(outside, filepath.Join(root, ".rhizome", "repair-journal")))
	before := []byte("before\n")
	require.NoError(t, os.WriteFile(filepath.Join(root, "note.md"), before, 0o640))
	plan := mustRepairPlan(t, []repairPlanInput{{
		actionID: "action:journal-symlink", issueKey: "issue:journal-symlink",
		operations: []RepairOperation{
			repairWriteOperation("op-journal-symlink", "action:journal-symlink", "note.md", before, []byte("after\n")),
		},
	}})
	digest := strings.TrimPrefix(SourceHash([]byte(plan.Transactions[0].ID)), "sha256:")
	require.NoError(t, os.Mkdir(filepath.Join(outside, digest), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(outside, digest, "sentinel"), []byte("outside\n"), 0o600))
	outsideBefore := snapshotRepairTestTree(t, outside)

	_, err := ApplyFixPlan(context.Background(), repairRunContext(t, root), &plan, Options{Fix: true, NonInteractive: true})
	require.ErrorContains(t, err, "direct vault directory")
	assert.Equal(t, outsideBefore, snapshotRepairTestTree(t, outside))
	assert.Equal(t, before, mustReadFile(t, filepath.Join(root, "note.md")))
}

func TestDiscoverRepairJournalRejectsDigestDirectoryMismatchWithoutRedirect(t *testing.T) {
	root := t.TempDir()
	runCtx := repairRunContext(t, root)
	journalRoot, err := repairJournalRoot(runCtx)
	require.NoError(t, err)
	manifestTransaction := "transaction:v1:manifest"
	directoryTransaction := "transaction:v1:directory"
	directoryDigest := strings.TrimPrefix(SourceHash([]byte(directoryTransaction)), "sha256:")
	expectedDigest := strings.TrimPrefix(SourceHash([]byte(manifestTransaction)), "sha256:")
	wrongDir := filepath.Join(journalRoot, directoryDigest)
	require.NoError(t, writeRepairJournalManifest(wrongDir, repairJournalManifest{
		TransactionID: manifestTransaction,
	}))
	before := snapshotRepairTestTree(t, journalRoot)

	_, err = DetectPendingRepairJournals(runCtx)
	require.ErrorContains(t, err, "does not match transaction id")
	assert.Equal(t, before, snapshotRepairTestTree(t, journalRoot))
	assert.NoDirExists(t, filepath.Join(journalRoot, expectedDigest), "discovery must not redirect to the manifest-derived directory")
}

func TestApplyFixPlanPreservesFullSupportedModeBits(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("special Unix mode bits are unavailable")
	}
	for _, test := range []struct {
		name string
		bit  os.FileMode
	}{
		{name: "setgid", bit: os.ModeSetgid},
		{name: "sticky", bit: os.ModeSticky},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			before := []byte("before\n")
			path := filepath.Join(root, "note.md")
			require.NoError(t, os.WriteFile(path, before, 0o640))
			want := os.FileMode(0o640) | test.bit
			require.NoError(t, os.Chmod(path, want))
			info, err := os.Stat(path)
			require.NoError(t, err)
			if repairModeBits(info.Mode()) != repairModeBits(want) {
				t.Skipf("filesystem does not preserve requested %s bit: got %v", test.name, info.Mode())
			}
			plan := mustRepairPlan(t, []repairPlanInput{{
				actionID: "action:special-mode-" + test.name, issueKey: "issue:special-mode-" + test.name,
				operations: []RepairOperation{repairWriteOperation(
					"op-special-mode-"+test.name, "action:special-mode-"+test.name,
					"note.md", before, []byte("after\n"),
				)},
			}})

			_, err = ApplyFixPlan(context.Background(), repairRunContext(t, root), &plan, Options{
				Fix: true, NonInteractive: true, PostApplyRefresher: &repairPostApplyProbe{t: t},
			})
			require.NoError(t, err)
			info, err = os.Stat(path)
			require.NoError(t, err)
			assert.Equal(t, repairModeBits(want), repairModeBits(info.Mode()))
		})
	}
}

func TestPostApplyRefreshResultCopiesShareOneClose(t *testing.T) {
	closeErr := errors.New("close failure")
	owner := &finalBatchCloseOwner{err: closeErr}
	runtime := &ontology.Runtime{}
	result := NewPostApplyRefreshResult(runtime, owner)
	assert.Same(t, runtime, result.Runtime)
	copyResult := result
	require.ErrorIs(t, result.Close(), closeErr)
	require.ErrorIs(t, result.Close(), closeErr, "a repeated Close must return the cached owner error")
	require.ErrorIs(t, copyResult.Close(), closeErr, "a copied result must share the cached owner error")
	assert.Equal(t, 1, owner.calls)
}

func TestApplyFixPlanClosesRefreshResultOnceWhenRefreshReturnsResultAndError(t *testing.T) {
	root := t.TempDir()
	before := []byte("before\n")
	require.NoError(t, os.WriteFile(filepath.Join(root, "note.md"), before, 0o640))
	plan := mustRepairPlan(t, []repairPlanInput{{
		actionID: "action:refresh-error-close", issueKey: "issue:refresh-error-close",
		operations: []RepairOperation{
			repairWriteOperation("op-refresh-error-close", "action:refresh-error-close", "note.md", before, []byte("after\n")),
		},
	}})
	refreshErr := errors.New("refresh returned partial result")
	owner := &finalBatchCloseOwner{}
	refresher := &finalBatchRefresher{
		result: NewPostApplyRefreshResult(&ontology.Runtime{}, owner),
		err:    refreshErr,
	}
	runCtx := repairRunContext(t, root)

	_, err := ApplyFixPlan(context.Background(), runCtx, &plan, Options{
		Fix: true, NonInteractive: true, PostApplyRefresher: refresher,
	})
	require.ErrorIs(t, err, refreshErr)
	assert.Equal(t, 1, owner.calls)
	evidence, detectErr := DetectPendingRepairJournals(runCtx)
	require.NoError(t, detectErr)
	require.Len(t, evidence, 1)
	assert.Equal(t, repairJournalCommitted, evidence[0].State)
}

func TestApplyFixPlanCloseErrorRetainsCommittedJournal(t *testing.T) {
	root := t.TempDir()
	before := []byte("before\n")
	require.NoError(t, os.WriteFile(filepath.Join(root, "note.md"), before, 0o640))
	plan := mustRepairPlan(t, []repairPlanInput{{
		actionID: "action:close-error", issueKey: "issue:close-error",
		operations: []RepairOperation{
			repairWriteOperation("op-close-error", "action:close-error", "note.md", before, []byte("after\n")),
		},
	}})
	closeErr := errors.New("runtime close failed")
	owner := &finalBatchCloseOwner{err: closeErr}
	refresher := &finalBatchRefresher{result: NewPostApplyRefreshResult(&ontology.Runtime{}, owner)}
	runCtx := repairRunContext(t, root)

	_, err := ApplyFixPlan(context.Background(), runCtx, &plan, Options{
		Fix: true, NonInteractive: true, PostApplyRefresher: refresher,
	})
	require.ErrorIs(t, err, closeErr)
	assert.Equal(t, 1, owner.calls)
	evidence, detectErr := DetectPendingRepairJournals(runCtx)
	require.NoError(t, detectErr)
	require.Len(t, evidence, 1, "close failure must retain recoverable committed evidence")
	assert.Equal(t, repairJournalCommitted, evidence[0].State)
}

func TestApplyFixPlanConfiguredRefresherRejectsNilPreparedRuntime(t *testing.T) {
	root := t.TempDir()
	before := []byte("before\n")
	require.NoError(t, os.WriteFile(filepath.Join(root, "note.md"), before, 0o640))
	plan := mustRepairPlan(t, []repairPlanInput{{
		actionID: "action:nil-runtime", issueKey: "issue:nil-runtime",
		operations: []RepairOperation{
			repairWriteOperation("op:nil-runtime", "action:nil-runtime", "note.md", before, []byte("after\n")),
		},
	}})
	runCtx := repairRunContext(t, root)

	_, err := ApplyFixPlan(context.Background(), runCtx, &plan, Options{
		Fix: true, NonInteractive: true, PostApplyRefresher: &finalBatchRefresher{},
	})
	require.ErrorContains(t, err, "prepared post-apply runtime is absent")
	evidence, detectErr := DetectPendingRepairJournals(runCtx)
	require.NoError(t, detectErr)
	require.Len(t, evidence, 1, "nil runtime must retain committed evidence instead of cleaning it")
	assert.Equal(t, repairJournalCommitted, evidence[0].State)
}

func TestApplyFixPlanStopsAfterRollbackFailure(t *testing.T) {
	root := t.TempDir()
	first := []byte("first before\n")
	second := []byte("second before\n")
	firstPath := filepath.Join(root, "a-first.md")
	secondPath := filepath.Join(root, "z-second.md")
	require.NoError(t, os.WriteFile(firstPath, first, 0o640))
	require.NoError(t, os.WriteFile(secondPath, second, 0o600))
	plan := mustRepairPlan(t, []repairPlanInput{
		{actionID: "action:first", issueKey: "issue:first", operations: []RepairOperation{
			repairWriteOperation("op:first", "action:first", "a-first.md", first, []byte("first after\n")),
		}},
		{actionID: "action:second", issueKey: "issue:second", operations: []RepairOperation{
			repairWriteOperation("op:second", "action:second", "z-second.md", second, []byte("second after\n")),
		}},
	})
	var firstTransaction RepairTransaction
	for _, transaction := range plan.Transactions {
		if len(transaction.AffectedPaths) == 1 && transaction.AffectedPaths[0] == "a-first.md" {
			firstTransaction = transaction
		}
	}
	require.NotEmpty(t, firstTransaction.ID)
	shortID := strings.TrimPrefix(SourceHash([]byte(firstTransaction.ID)), "sha256:")[:12]
	backup := repairArtifactPath(firstPath, shortID, 0, "backup")
	injected := errors.New("commit failed after first install")
	fired := false

	execution, err := ApplyFixPlan(context.Background(), repairRunContext(t, root), &plan, Options{
		Fix: true, NonInteractive: true,
		repairHooks: &repairExecutionHooks{AfterInstall: func(_ int, path string) error {
			if fired || path != "a-first.md" {
				return nil
			}
			fired = true
			require.NoError(t, os.WriteFile(backup, []byte("corrupt backup\n"), 0o640))
			return injected
		}},
	})
	require.ErrorIs(t, err, injected)
	require.ErrorContains(t, err, "artifact ownership changed")
	assert.Equal(t, second, mustReadFile(t, secondPath), "later independent transactions must not commit")
	statuses := map[string]string{}
	for _, transaction := range execution.Transactions {
		for _, path := range transaction.AffectedPaths {
			statuses[path] = transaction.Status
		}
	}
	assert.Equal(t, "not_attempted", statuses["z-second.md"])
}

func TestApplyFixPlanRefreshesCommittedRecoveryBeforeReplanningNewWork(t *testing.T) {
	root := t.TempDir()
	oldBefore := []byte("old before\n")
	newBefore := []byte("new before\n")
	require.NoError(t, os.WriteFile(filepath.Join(root, "old.md"), oldBefore, 0o640))
	require.NoError(t, os.WriteFile(filepath.Join(root, "new.md"), newBefore, 0o640))
	runCtx := repairRunContext(t, root)
	oldPlan := mustRepairPlan(t, []repairPlanInput{{
		actionID: "action:old", issueKey: "issue:old",
		operations: []RepairOperation{
			repairWriteOperation("op-old", "action:old", "old.md", oldBefore, []byte("old after\n")),
		},
	}})
	leaveJournal := errors.New("leave committed recovery")
	_, err := ApplyFixPlan(context.Background(), runCtx, &oldPlan, Options{
		Fix: true, NonInteractive: true,
		PostApplyRefresher: &repairPostApplyProbe{t: t, refreshErr: leaveJournal},
	})
	require.ErrorIs(t, err, leaveJournal)

	newPlan := mustRepairPlan(t, []repairPlanInput{{
		actionID: "action:new", issueKey: "issue:new",
		operations: []RepairOperation{
			repairWriteOperation("op-new", "action:new", "new.md", newBefore, []byte("new after\n")),
		},
	}})
	runtimeResult := &ontology.Runtime{}
	owner := &finalBatchCloseOwner{}
	refresher := &finalBatchRefresher{result: NewPostApplyRefreshResult(runtimeResult, owner)}
	postchecks := 0

	execution, err := ApplyFixPlan(context.Background(), runCtx, &newPlan, Options{
		Fix: true, NonInteractive: true, PostApplyRefresher: refresher,
		postApplyCheck: func(_ context.Context, lease *IndexLockLease, prepared *ontology.Runtime, _ repairPostApplyScope) error {
			require.NoError(t, lease.RequireHeldForVault(root))
			assert.Same(t, runtimeResult, prepared)
			assert.Equal(t, 0, owner.calls, "runtime must remain open through the single postcheck")
			postchecks++
			return nil
		},
	})
	require.ErrorContains(t, err, "replan")
	require.NotNil(t, execution)
	assert.Equal(t, 1, refresher.calls)
	assert.Equal(t, []string{"old.md"}, refresher.changed)
	assert.Equal(t, 1, postchecks)
	assert.Equal(t, 1, owner.calls)
	assert.Equal(t, newBefore, mustReadFile(t, filepath.Join(root, "new.md")))
	var newStatus string
	for _, transaction := range execution.Transactions {
		if slices.Contains(transaction.AffectedPaths, "new.md") {
			newStatus = transaction.Status
		}
	}
	assert.Equal(t, "not_attempted", newStatus)
	evidence, detectErr := DetectPendingRepairJournals(runCtx)
	require.NoError(t, detectErr)
	assert.Empty(t, evidence)
}

type finalBatchCloseOwner struct {
	calls int
	err   error
}

func (o *finalBatchCloseOwner) Close() error {
	o.calls++
	return o.err
}

type finalBatchRefresher struct {
	result  PostApplyRefreshResult
	err     error
	calls   int
	changed []string
	renamed []PathRename
	deleted []string
}

func (r *finalBatchRefresher) Refresh(
	_ context.Context,
	lease *IndexLockLease,
	changed []string,
	renamed []PathRename,
	deleted []string,
) (PostApplyRefreshResult, error) {
	if err := lease.RequireHeld(); err != nil {
		return PostApplyRefreshResult{}, err
	}
	r.calls++
	r.changed = append([]string(nil), changed...)
	r.renamed = append([]PathRename(nil), renamed...)
	r.deleted = append([]string(nil), deleted...)
	return r.result, r.err
}
