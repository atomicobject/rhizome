package validate

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRepairReviewPreservesAuthorityForMultiFilePreviewAndApply(t *testing.T) {
	root := t.TempDir()
	beforeA := []byte("before a\n")
	beforeB := []byte("before b\n")
	require.NoError(t, os.WriteFile(filepath.Join(root, "a.md"), beforeA, 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, "b.md"), beforeB, 0o644))

	planned := repairReviewFixtureResult(t, []FixAction{{
		ID: "action:both", Check: CheckLinkHygiene, Kind: FixKindRewriteLinkGroup,
		Safety: FixSafetyConfirm, Title: "Repair both files", Question: "Repair both files?", IssueKeys: []string{"issue:a", "issue:b"},
		CandidatePaths: []string{"target.md"},
	}}, []RepairOperation{
		repairReviewWrite("op:a", "action:both", []string{"issue:a", "issue:b"}, "a.md", beforeA, []byte("after a\n"), "group:both"),
		repairReviewWrite("op:b", "action:both", []string{"issue:a", "issue:b"}, "b.md", beforeB, []byte("after b\n"), "group:both"),
	})
	runCtx := RunContext{
		VaultDef: obsidian.VaultDefinition{Path: root}, VaultPath: root,
		NoteReader: &obsidian.Note{}, NoteMetadata: testNoteMetadata(t), MaxIssues: 20,
	}
	store := NewRepairReviewStore(RepairReviewStoreOptions{})
	review, err := store.Create(context.Background(), RepairReviewCreateRequest{
		VaultIdentity: "vault:test", Generation: 7, PlanFingerprint: planned.FixPlan.Fingerprint,
		ActionIDs: []string{"action:both"}, Result: planned, RunContext: runCtx,
	})
	require.NoError(t, err)
	require.Len(t, review.Preview, 2)
	assert.Contains(t, review.Preview[0].Diff, "after a")
	assert.Contains(t, review.Preview[1].Diff, "after b")
	assert.Equal(t, RepairReviewPending, review.State)
	require.Equal(t, []RepairReviewConfirmation{{
		ActionID: "action:both", Question: "Repair both files?", CandidatePath: "target.md",
		AffectedPaths: []string{"a.md", "b.md"},
	}}, review.RequiredConfirmations)
	assert.Equal(t, beforeA, mustReadFile(t, filepath.Join(root, "a.md")), "preview must remain read-only")
	assert.Equal(t, beforeB, mustReadFile(t, filepath.Join(root, "b.md")), "preview must remain read-only")
	_, _, err = store.Apply(context.Background(), review.ID, RepairReviewApplyRequest{
		Generation: 7, PlanFingerprint: review.PlanFingerprint, SelectionFingerprint: review.SelectionFingerprint,
		Confirmations: []RepairReviewConfirmation{{ActionID: "action:both", Question: "Repair both files?", CandidatePath: "other.md"}},
	})
	assertRepairReviewError(t, err, RepairReviewErrorConfirmationRequired)
	// Ownership transfers to the store. Later caller mutations cannot alter the
	// reviewed operations or action identity used at apply time.
	planned.FixPlan.Operations[0].Content[0] = 'X'
	planned.FixPlan.Actions[0].ID = "action:forged-after-create"

	result, execution, err := store.Apply(context.Background(), review.ID, RepairReviewApplyRequest{
		Generation: 7, PlanFingerprint: review.PlanFingerprint,
		SelectionFingerprint: review.SelectionFingerprint,
		Confirmations:        review.RequiredConfirmations,
		Options:              Options{PostApplyRefresher: &repairPostApplyProbe{t: t}},
	})
	require.NoError(t, err)
	require.NotNil(t, execution)
	assert.Equal(t, []byte("after a\n"), mustReadFile(t, filepath.Join(root, "a.md")))
	assert.Equal(t, []byte("after b\n"), mustReadFile(t, filepath.Join(root, "b.md")))
	assert.True(t, result.OK)

	retried, retryExecution, err := store.Apply(context.Background(), review.ID, RepairReviewApplyRequest{
		Generation: 7, PlanFingerprint: review.PlanFingerprint,
		SelectionFingerprint: review.SelectionFingerprint,
		Confirmations:        review.RequiredConfirmations,
	})
	require.NoError(t, err)
	assert.Equal(t, result.IssueCount, retried.IssueCount)
	assert.Equal(t, execution.PlanFingerprint, retryExecution.PlanFingerprint)
}

func TestRepairReviewApplyValidatesGenerationAndFingerprints(t *testing.T) {
	root := t.TempDir()
	before := []byte("before\n")
	require.NoError(t, os.WriteFile(filepath.Join(root, "note.md"), before, 0o644))
	planned := repairReviewFixtureResult(t, []FixAction{{
		ID: "action", Check: CheckLinkHygiene, Kind: FixKindRewriteLinkGroup,
		Safety: FixSafetySafe, Title: "Repair", IssueKeys: []string{"issue"},
	}}, []RepairOperation{repairReviewWrite("op", "action", []string{"issue"}, "note.md", before, []byte("after\n"), "")})
	store := NewRepairReviewStore(RepairReviewStoreOptions{})
	review, err := store.Create(context.Background(), RepairReviewCreateRequest{
		VaultIdentity: "vault:test", Generation: 9, PlanFingerprint: planned.FixPlan.Fingerprint,
		ActionIDs: []string{"action"}, Result: planned,
		RunContext: RunContext{VaultDef: obsidian.VaultDefinition{Path: root}, VaultPath: root},
	})
	require.NoError(t, err)

	tests := []struct {
		name string
		req  RepairReviewApplyRequest
		code RepairReviewErrorCode
	}{
		{name: "generation", req: RepairReviewApplyRequest{Generation: 10, PlanFingerprint: review.PlanFingerprint, SelectionFingerprint: review.SelectionFingerprint}, code: RepairReviewErrorGenerationMismatch},
		{name: "plan fingerprint", req: RepairReviewApplyRequest{Generation: 9, PlanFingerprint: "forged", SelectionFingerprint: review.SelectionFingerprint}, code: RepairReviewErrorPlanFingerprintMismatch},
		{name: "selection fingerprint", req: RepairReviewApplyRequest{Generation: 9, PlanFingerprint: review.PlanFingerprint, SelectionFingerprint: "forged"}, code: RepairReviewErrorSelectionFingerprintMismatch},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, _, err := store.Apply(context.Background(), review.ID, tt.req)
			assertRepairReviewError(t, err, tt.code)
			assert.Equal(t, before, mustReadFile(t, filepath.Join(root, "note.md")))
		})
	}
}

func TestRepairReviewRejectsForgedAndPartialActionSelection(t *testing.T) {
	root := t.TempDir()
	beforeA := []byte("a\n")
	beforeB := []byte("b\n")
	require.NoError(t, os.WriteFile(filepath.Join(root, "a.md"), beforeA, 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, "b.md"), beforeB, 0o644))
	planned := repairReviewFixtureResult(t, []FixAction{
		{ID: "action:a", Check: CheckLinkHygiene, Kind: FixKindRewriteLinkGroup, Safety: FixSafetySafe, Title: "A", IssueKeys: []string{"issue:a"}},
		{ID: "action:b", Check: CheckLinkHygiene, Kind: FixKindRewriteLinkGroup, Safety: FixSafetySafe, Title: "B", IssueKeys: []string{"issue:b"}},
	}, []RepairOperation{
		repairReviewWrite("op:a", "action:a", []string{"issue:a"}, "a.md", beforeA, []byte("A\n"), "shared"),
		repairReviewWrite("op:b", "action:b", []string{"issue:b"}, "b.md", beforeB, []byte("B\n"), "shared"),
	})
	store := NewRepairReviewStore(RepairReviewStoreOptions{})
	base := RepairReviewCreateRequest{
		VaultIdentity: "vault:test", Generation: 1, PlanFingerprint: planned.FixPlan.Fingerprint,
		Result: planned, RunContext: RunContext{VaultDef: obsidian.VaultDefinition{Path: root}, VaultPath: root},
	}

	forged := base
	forged.ActionIDs = []string{"action:forged"}
	_, err := store.Create(context.Background(), forged)
	assertRepairReviewError(t, err, RepairReviewErrorActionNotFound)

	partial := base
	partial.ActionIDs = []string{"action:a"}
	_, err = store.Create(context.Background(), partial)
	reviewErr := assertRepairReviewError(t, err, RepairReviewErrorTransactionIncomplete)
	assert.Equal(t, []string{"action:a", "action:b"}, reviewErr.RequiredActionIDs)
}

func TestRepairReviewRejectsJSONRoundTrippedIdentifierAuthority(t *testing.T) {
	root := t.TempDir()
	before := []byte("before\n")
	require.NoError(t, os.WriteFile(filepath.Join(root, "note.md"), before, 0o644))
	plan, err := FinalizeRepairPlan(RepairPlan{
		RequiresLeaseHeldReplan: true,
		Actions: []FixAction{{
			ID: "identifier-action", Check: CheckIdentifiers, Kind: "identifier-repair",
			Safety: FixSafetyConfirm, IssueKeys: []string{"issue"},
		}},
		Operations: []RepairOperation{repairReviewWrite(
			"op", "identifier-action", []string{"issue"}, "note.md", before, []byte("after\n"), "identifier",
		)},
	})
	require.NoError(t, err)
	planned := Result{FixPlan: &plan}
	encoded, err := json.Marshal(planned)
	require.NoError(t, err)
	var roundTripped Result
	require.NoError(t, json.Unmarshal(encoded, &roundTripped))

	store := NewRepairReviewStore(RepairReviewStoreOptions{})
	_, err = store.Create(context.Background(), RepairReviewCreateRequest{
		VaultIdentity: "vault:test", Generation: 2, PlanFingerprint: roundTripped.FixPlan.Fingerprint,
		ActionIDs: []string{"identifier-action"}, Result: roundTripped,
		RunContext: RunContext{VaultDef: obsidian.VaultDefinition{Path: root}, VaultPath: root},
	})
	assertRepairReviewError(t, err, RepairReviewErrorAuthorityUnavailable)
}

func TestRepairReviewPreservesAndAppliesIdentifierTransactionAuthority(t *testing.T) {
	fixture := buildIdentifierRepairAdapterFixture(t)
	plan, err := BuildIdentifierRepairPlan(context.Background(), fixture.runCtx, fixture.assembly, fixture.bindings)
	require.NoError(t, err)
	planned := Result{
		SelectedChecks: []string{CheckAliases, CheckBrokenLinks, CheckIdentifiers, CheckOntology},
		Checks: []CheckResult{{
			Name:             CheckIdentifiers,
			identifierRepair: &identifierRepairPayload{Assembly: fixture.assembly, Bindings: fixture.bindings},
		}},
		FixPlan: plan,
	}
	store := NewRepairReviewStore(RepairReviewStoreOptions{})
	review, err := store.Create(context.Background(), RepairReviewCreateRequest{
		VaultIdentity: "vault:test", Generation: 8, PlanFingerprint: plan.Fingerprint,
		ActionIDs: []string{fixture.bindings[0].Action.ID}, Result: planned, RunContext: fixture.runCtx,
	})
	require.NoError(t, err)
	require.Contains(t, review.TransactionIDs, plan.Transactions[0].ID)
	require.NotEmpty(t, review.Preview)
	result, execution, err := store.Apply(context.Background(), review.ID, RepairReviewApplyRequest{
		Generation: 8, PlanFingerprint: review.PlanFingerprint, SelectionFingerprint: review.SelectionFingerprint,
		Options: Options{PostApplyRefresher: &identifierRepairRuntimeRefresher{t: t, runCtx: fixture.runCtx}},
	})
	require.NoError(t, err)
	require.NotNil(t, execution)
	require.Contains(t, execution.Applied, fixture.bindings[0].Action.ID)
	require.Zero(t, result.ErrorCount)
	_, err = os.Stat(filepath.Join(fixture.root, filepath.FromSlash(fixture.oldPath)))
	require.ErrorIs(t, err, os.ErrNotExist)
	require.FileExists(t, filepath.Join(fixture.root, filepath.FromSlash(fixture.newPath)))
}

func TestRepairReviewPreviewRepresentsWriteRenameAndDelete(t *testing.T) {
	root := t.TempDir()
	writeBefore, renameBefore, deleteBefore := []byte("write before\n"), []byte("rename before\n"), []byte("delete before\n")
	for path, content := range map[string][]byte{"write.md": writeBefore, "old.md": renameBefore, "delete.md": deleteBefore} {
		require.NoError(t, os.WriteFile(filepath.Join(root, path), content, 0o644))
	}
	action := FixAction{ID: "action", Check: CheckLinkHygiene, Kind: FixKindRewriteLinkGroup, Safety: FixSafetySafe, Title: "Repair", IssueKeys: []string{"issue"}}
	planned := repairReviewFixtureResult(t, []FixAction{action}, []RepairOperation{
		repairReviewWrite("write", "action", []string{"issue"}, "write.md", writeBefore, []byte("write after\n"), ""),
		{ID: "rename", ActionIDs: []string{"action"}, IssueKeys: []string{"issue"}, Kind: RepairOperationRename, Path: "old.md", DestinationPath: "new.md", SourceHash: SourceHash(renameBefore)},
		{ID: "delete", ActionIDs: []string{"action"}, IssueKeys: []string{"issue"}, Kind: RepairOperationDelete, Path: "delete.md", SourceHash: SourceHash(deleteBefore)},
	})
	store := NewRepairReviewStore(RepairReviewStoreOptions{})
	review, err := store.Create(context.Background(), RepairReviewCreateRequest{
		VaultIdentity: "vault:test", Generation: 3, PlanFingerprint: planned.FixPlan.Fingerprint,
		ActionIDs: []string{"action"}, Result: planned,
		RunContext: RunContext{VaultDef: obsidian.VaultDefinition{Path: root}, VaultPath: root},
	})
	require.NoError(t, err)
	byPath := make(map[string]RepairPreviewFile, len(review.Preview))
	for _, preview := range review.Preview {
		byPath[preview.Path] = preview
	}
	require.Equal(t, RepairOperationWrite, byPath["write.md"].Kind)
	require.Equal(t, RepairOperationRename, byPath["new.md"].Kind)
	require.Equal(t, "old.md", byPath["new.md"].OriginalPath)
	require.Equal(t, RepairOperationDelete, byPath["delete.md"].Kind)
}

func TestRepairReviewRejectsManualOverlapAtCreateAndApply(t *testing.T) {
	root := t.TempDir()
	before := []byte("before\n")
	require.NoError(t, os.WriteFile(filepath.Join(root, "note.md"), before, 0o644))
	planned := repairReviewFixtureResult(t, []FixAction{{
		ID: "action", Check: CheckLinkHygiene, Kind: FixKindRewriteLinkGroup,
		Safety: FixSafetySafe, Title: "Repair", IssueKeys: []string{"issue"},
	}}, []RepairOperation{repairReviewWrite("op", "action", []string{"issue"}, "note.md", before, []byte("after\n"), "")})
	coordinator := NewRepairPathCoordinator()
	store := NewRepairReviewStore(RepairReviewStoreOptions{PathReservations: coordinator})
	request := RepairReviewCreateRequest{
		VaultIdentity: "vault:test", Generation: 3, PlanFingerprint: planned.FixPlan.Fingerprint,
		ActionIDs: []string{"action"}, Result: planned,
		RunContext: RunContext{VaultDef: obsidian.VaultDefinition{Path: root}, VaultPath: root},
	}

	require.NoError(t, coordinator.SetManualPaths("vault:test", "manual", []string{"note.md"}))
	_, err := store.Create(context.Background(), request)
	reviewErr := assertRepairReviewError(t, err, RepairReviewErrorManualOverlap)
	assert.Equal(t, []string{"note.md"}, reviewErr.OverlappingPaths)

	coordinator.ReleaseManualSession("vault:test", "manual")
	review, err := store.Create(context.Background(), request)
	require.NoError(t, err)
	require.NoError(t, coordinator.SetManualPaths("vault:test", "manual", []string{"note.md"}))
	_, _, err = store.Apply(context.Background(), review.ID, RepairReviewApplyRequest{
		Generation: 3, PlanFingerprint: review.PlanFingerprint,
		SelectionFingerprint: review.SelectionFingerprint,
	})
	assertRepairReviewError(t, err, RepairReviewErrorManualOverlap)
	assert.Equal(t, before, mustReadFile(t, filepath.Join(root, "note.md")))
}

func TestRepairReviewExpiresAndCapacityRemainsBounded(t *testing.T) {
	now := time.Date(2026, 9, 11, 12, 0, 0, 0, time.UTC)
	store := NewRepairReviewStore(RepairReviewStoreOptions{
		TTL: time.Minute, MaxPerVault: 1, Now: func() time.Time { return now },
	})
	root := t.TempDir()
	before := []byte("before\n")
	require.NoError(t, os.WriteFile(filepath.Join(root, "note.md"), before, 0o644))
	planned := repairReviewFixtureResult(t, []FixAction{{
		ID: "action", Check: CheckLinkHygiene, Kind: FixKindRewriteLinkGroup,
		Safety: FixSafetySafe, Title: "Repair", IssueKeys: []string{"issue"},
	}}, []RepairOperation{repairReviewWrite("op", "action", []string{"issue"}, "note.md", before, []byte("after\n"), "")})
	request := RepairReviewCreateRequest{
		VaultIdentity: "vault:test", Generation: 4, PlanFingerprint: planned.FixPlan.Fingerprint,
		ActionIDs: []string{"action"}, Result: planned,
		RunContext: RunContext{VaultDef: obsidian.VaultDefinition{Path: root}, VaultPath: root},
	}
	first, err := store.Create(context.Background(), request)
	require.NoError(t, err)
	second, err := store.Create(context.Background(), request)
	require.NoError(t, err)
	_, err = store.Get(first.ID)
	assertRepairReviewError(t, err, RepairReviewErrorNotFound)
	_, err = store.Get(second.ID)
	require.NoError(t, err)

	now = now.Add(2 * time.Minute)
	_, _, err = store.Apply(context.Background(), second.ID, RepairReviewApplyRequest{
		Generation: 4, PlanFingerprint: second.PlanFingerprint,
		SelectionFingerprint: second.SelectionFingerprint,
	})
	assertRepairReviewError(t, err, RepairReviewErrorExpired)
}

func repairReviewFixtureResult(t *testing.T, actions []FixAction, operations []RepairOperation) Result {
	t.Helper()
	plan, err := FinalizeRepairPlan(RepairPlan{Actions: actions, Operations: operations})
	require.NoError(t, err)
	return Result{
		SelectedChecks: []string{CheckLinkHygiene},
		Checks:         []CheckResult{{Name: CheckLinkHygiene, OK: false}},
		FixPlan:        &plan,
	}
}

func repairReviewWrite(id, actionID string, issueKeys []string, path string, before, after []byte, identity string) RepairOperation {
	identities := []string(nil)
	if identity != "" {
		identities = []string{identity}
	}
	return RepairOperation{
		ID: id, ActionIDs: []string{actionID}, IssueKeys: issueKeys,
		Kind: RepairOperationWrite, Path: path, SourceHash: SourceHash(before),
		Expected: []ExpectedText{{StartByte: 0, EndByte: len(before), Text: string(before), Replacement: string(after)}},
		Content:  after, Identities: identities,
	}
}

func assertRepairReviewError(t *testing.T, err error, code RepairReviewErrorCode) *RepairReviewError {
	t.Helper()
	var reviewErr *RepairReviewError
	require.ErrorAs(t, err, &reviewErr)
	assert.Equal(t, code, reviewErr.Code)
	return reviewErr
}

func TestRepairReviewManualPathProbeFailureFailsClosed(t *testing.T) {
	root := t.TempDir()
	before := []byte("before\n")
	require.NoError(t, os.WriteFile(filepath.Join(root, "note.md"), before, 0o644))
	planned := repairReviewFixtureResult(t, []FixAction{{
		ID: "action", Check: CheckLinkHygiene, Kind: FixKindRewriteLinkGroup,
		Safety: FixSafetySafe, Title: "Repair", IssueKeys: []string{"issue"},
	}}, []RepairOperation{repairReviewWrite("op", "action", []string{"issue"}, "note.md", before, []byte("after\n"), "")})
	store := NewRepairReviewStore(RepairReviewStoreOptions{PathReservations: repairReviewReservationSourceFunc(func(context.Context, string, []string) (RepairPathReservation, error) {
		return nil, errors.New("manual session store unavailable")
	})})
	_, err := store.Create(context.Background(), RepairReviewCreateRequest{
		VaultIdentity: "vault", Generation: 1, PlanFingerprint: planned.FixPlan.Fingerprint,
		ActionIDs: []string{"action"}, Result: planned,
		RunContext: RunContext{VaultDef: obsidian.VaultDefinition{Path: root}, VaultPath: root},
	})
	require.ErrorContains(t, err, "manual session store unavailable")
}

type repairReviewReservationSourceFunc func(context.Context, string, []string) (RepairPathReservation, error)

func (f repairReviewReservationSourceFunc) ReserveRepairPaths(ctx context.Context, vault string, paths []string) (RepairPathReservation, error) {
	return f(ctx, vault, paths)
}

func TestRepairReviewConcurrentApplyWaitsAndReturnsTerminalResult(t *testing.T) {
	root, planned, runCtx := repairReviewApplyFixture(t)
	provider := &blockingRepairReservationSource{entered: make(chan struct{}), release: make(chan struct{})}
	store := NewRepairReviewStore(RepairReviewStoreOptions{PathReservations: provider})
	review, err := store.Create(context.Background(), RepairReviewCreateRequest{
		VaultIdentity: "vault:test", Generation: 11, PlanFingerprint: planned.FixPlan.Fingerprint,
		ActionIDs: []string{"action"}, Result: planned, RunContext: runCtx,
	})
	require.NoError(t, err)
	request := RepairReviewApplyRequest{
		Generation: 11, PlanFingerprint: review.PlanFingerprint, SelectionFingerprint: review.SelectionFingerprint,
		Options: Options{PostApplyRefresher: &repairPostApplyProbe{t: t}},
	}
	type applyResult struct {
		result Result
		exec   *FixExecution
		err    error
	}
	results := make(chan applyResult, 2)
	apply := func() {
		result, execution, err := store.Apply(context.Background(), review.ID, request)
		results <- applyResult{result, execution, err}
	}
	go apply()
	<-provider.entered
	go apply()
	select {
	case <-results:
		t.Fatal("concurrent retry returned before the applying review completed")
	case <-time.After(25 * time.Millisecond):
	}
	close(provider.release)
	first := <-results
	second := <-results
	require.NoError(t, first.err)
	require.NoError(t, second.err)
	assert.Equal(t, first.exec.PlanFingerprint, second.exec.PlanFingerprint)
	assert.Equal(t, first.result.IssueCount, second.result.IssueCount)
	assert.Equal(t, []byte("after\n"), mustReadFile(t, filepath.Join(root, "note.md")))
}

func TestRepairReservationBlocksOverlappingManualAdmissionUntilApplyCompletes(t *testing.T) {
	_, planned, runCtx := repairReviewApplyFixture(t)
	coordinator := NewRepairPathCoordinator()
	refresher := &blockingRepairRefresher{
		repairPostApplyProbe: repairPostApplyProbe{t: t},
		entered:              make(chan struct{}),
		release:              make(chan struct{}),
	}
	store := NewRepairReviewStore(RepairReviewStoreOptions{PathReservations: coordinator})
	review, err := store.Create(context.Background(), RepairReviewCreateRequest{
		VaultIdentity: "vault:test", Generation: 12, PlanFingerprint: planned.FixPlan.Fingerprint,
		ActionIDs: []string{"action"}, Result: planned, RunContext: runCtx,
	})
	require.NoError(t, err)
	done := make(chan error, 1)
	go func() {
		_, _, err := store.Apply(context.Background(), review.ID, RepairReviewApplyRequest{
			Generation: 12, PlanFingerprint: review.PlanFingerprint, SelectionFingerprint: review.SelectionFingerprint,
			Options: Options{PostApplyRefresher: refresher},
		})
		done <- err
	}()
	<-refresher.entered
	err = coordinator.SetManualPaths("vault:test", "manual-overlap", []string{"note.md"})
	assertRepairReviewError(t, err, RepairReviewErrorManualOverlap)
	require.NoError(t, coordinator.SetManualPaths("vault:test", "manual-disjoint", []string{"other.md"}))
	close(refresher.release)
	require.NoError(t, <-done)
	require.NoError(t, coordinator.SetManualPaths("vault:test", "manual-overlap", []string{"note.md"}))
}

func repairReviewApplyFixture(t *testing.T) (string, Result, RunContext) {
	t.Helper()
	root := t.TempDir()
	before := []byte("before\n")
	require.NoError(t, os.WriteFile(filepath.Join(root, "note.md"), before, 0o644))
	planned := repairReviewFixtureResult(t, []FixAction{{
		ID: "action", Check: CheckLinkHygiene, Kind: FixKindRewriteLinkGroup,
		Safety: FixSafetySafe, Title: "Repair", IssueKeys: []string{"issue"},
	}}, []RepairOperation{repairReviewWrite("op", "action", []string{"issue"}, "note.md", before, []byte("after\n"), "")})
	return root, planned, RunContext{
		VaultDef: obsidian.VaultDefinition{Path: root}, VaultPath: root,
		NoteReader: &obsidian.Note{}, NoteMetadata: testNoteMetadata(t), MaxIssues: 20,
	}
}

type blockingRepairReservationSource struct {
	mu      sync.Mutex
	calls   int
	entered chan struct{}
	release chan struct{}
}

func (p *blockingRepairReservationSource) ReserveRepairPaths(ctx context.Context, _ string, _ []string) (RepairPathReservation, error) {
	p.mu.Lock()
	p.calls++
	call := p.calls
	p.mu.Unlock()
	if call == 1 {
		return noopRepairPathReservation{}, nil
	}
	if call == 2 {
		close(p.entered)
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-p.release:
		}
	}
	return noopRepairPathReservation{}, nil
}

type blockingRepairRefresher struct {
	repairPostApplyProbe
	entered chan struct{}
	release chan struct{}
}

func (p *blockingRepairRefresher) Refresh(ctx context.Context, lease *IndexLockLease, changed []string, renamed []PathRename, deleted []string) (PostApplyRefreshResult, error) {
	close(p.entered)
	select {
	case <-ctx.Done():
		return PostApplyRefreshResult{}, ctx.Err()
	case <-p.release:
	}
	return p.repairPostApplyProbe.Refresh(ctx, lease, changed, renamed, deleted)
}
