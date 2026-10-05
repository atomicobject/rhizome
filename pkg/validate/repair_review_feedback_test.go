package validate

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/testutil/sqlitefixture"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRunSuiteOnceFailsClosedWhenRepairJournalIsPending(t *testing.T) {
	root := t.TempDir()
	before := []byte("before\n")
	require.NoError(t, os.WriteFile(filepath.Join(root, "note.md"), before, 0o640))
	plan := mustRepairPlan(t, []repairPlanInput{{
		actionID: "action:pending-read-only", issueKey: "issue:pending-read-only",
		operations: []RepairOperation{
			repairWriteOperation("op:pending-read-only", "action:pending-read-only", "note.md", before, []byte("after\n")),
		},
	}})
	runCtx := repairRunContext(t, root)
	_, err := ApplyFixPlan(context.Background(), runCtx, &plan, Options{
		Fix: true, NonInteractive: true,
		repairHooks: &repairExecutionHooks{AfterJournalPublished: func(string) error {
			return errSimulatedRepairInterruption
		}},
	})
	require.ErrorIs(t, err, errSimulatedRepairInterruption)

	result, _, err := RunSuiteOnce(context.Background(), Options{
		Checks: []string{CheckLinkHygiene}, RunContext: &runCtx,
	})
	require.NoError(t, err)
	assert.False(t, result.OK, "pending recovery evidence must fail read-only validation")
	require.Len(t, result.RepairJournals, 1)
	assert.Empty(t, result.Checks, "checks must not run against partially repaired bytes")
	assert.Nil(t, result.FixPlan, "pending recovery must not expose an executable plan")
	require.NotNil(t, result.NextActions)
	require.Len(t, result.NextActions.Actions, 1)
	assert.Equal(t, "repair_recovery", result.NextActions.Actions[0].Category)
	assert.Contains(t, result.NextActions.Actions[0].Message, "recover")
	assert.Contains(t, result.NextActions.Actions[0].Message, "replan")
	assert.Equal(t, "rzm agent validate fix link-hygiene --apply --vault test && rzm agent validate fix ontology --apply --vault test", result.NextActions.Actions[0].Command)
}

func TestRecoveryPostcheckUnionsOriginatingChecksAcrossSelectors(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, writeFixtureFiles(root, map[string]string{
		"target.md": "# Target\n",
		"source.md": "bad extension: [[target.md]]\n",
	}))
	before := []byte("bad extension: [[target.md]]\n")
	after := []byte("clean link: [[target]]\n")
	plan := mustRepairPlan(t, []repairPlanInput{{
		actionID: "action:origin-check", issueKey: "issue:origin-check",
		operations: []RepairOperation{repairWriteOperation(
			"operation:origin-check", "action:origin-check", "source.md", before, after,
		)},
	}})
	plan.Actions[0].Check = CheckLinkHygiene
	plan, err := FinalizeRepairPlan(plan)
	require.NoError(t, err)
	runCtx := linkHygieneRunContext(t, root)
	refreshErr := errors.New("retain committed journal for cross-selector recovery")
	_, err = ApplyFixPlan(context.Background(), runCtx, &plan, Options{
		Fix: true, NonInteractive: true,
		PostApplyRefresher: &repairPostApplyProbe{t: t, refreshErr: refreshErr},
	})
	require.ErrorIs(t, err, refreshErr)

	blocked, scanCtx, err := RunSuiteOnce(context.Background(), Options{
		Checks: []string{CheckQueryRecipes}, RunContext: &runCtx,
	})
	require.NoError(t, err)
	require.Len(t, blocked.RepairJournals, 1)
	assert.Equal(t, []string{CheckLinkHygiene}, blocked.RepairJournals[0].RequiredChecks)
	require.NotNil(t, blocked.NextActions)
	assert.Contains(t, blocked.NextActions.Actions[0].Command, "rzm agent validate fix link-hygiene --apply")
	assert.Contains(t, blocked.NextActions.Actions[0].Command, "rzm agent validate fix query-recipes --apply")
	assert.Contains(t, blocked.NextActions.Actions[0].Command, " && ")

	result, _, err := ApplyRepairSession(context.Background(), blocked, scanCtx, Options{
		Fix: true, NonInteractive: true, PostApplyRefresher: &repairPostApplyProbe{t: t},
	})
	require.NoError(t, err)
	assert.True(t, result.OK, "result: %+v", result)
	assert.Equal(t, []string{CheckLinkHygiene}, result.SelectedChecks,
		"the selected check was blocked by recovery and must not be executed as a postcheck")
	checkNames := make([]string, 0, len(result.Checks))
	for _, check := range result.Checks {
		checkNames = append(checkNames, check.Name)
	}
	assert.Equal(t, []string{CheckLinkHygiene}, checkNames)
}

func TestRecoveryPostcheckRetainsOntologyJournalOnCheckErrorAcrossSelectors(t *testing.T) {
	root := t.TempDir()
	before := []byte("before\n")
	require.NoError(t, os.WriteFile(filepath.Join(root, "note.md"), before, 0o640))
	plan := mustRepairPlan(t, []repairPlanInput{{
		actionID: "action:origin-ontology", issueKey: "issue:origin-ontology",
		operations: []RepairOperation{repairWriteOperation(
			"operation:origin-ontology", "action:origin-ontology", "note.md", before, []byte("after\n"),
		)},
	}})
	plan.Actions[0].Check = CheckOntology
	plan, err := FinalizeRepairPlan(plan)
	require.NoError(t, err)
	runCtx := repairRunContext(t, root)
	retain := errors.New("retain committed ontology journal")
	_, err = ApplyFixPlan(context.Background(), runCtx, &plan, Options{
		Fix: true, NonInteractive: true,
		PostApplyRefresher: &repairPostApplyProbe{t: t, refreshErr: retain},
	})
	require.ErrorIs(t, err, retain)

	store, err := sqlitefixture.Open(filepath.Join(root, ".rhizome", "postcheck.sqlite"))
	require.NoError(t, err)
	require.NoError(t, store.Close())
	prepared := NewPostApplyRefreshResult(&ontology.Runtime{
		Schema: &ontology.Schema{Types: map[string]*ontology.NoteType{
			"Spec": {Name: "Spec", Role: ontology.TypeRoleNote},
		}},
		Store: store,
	}, repairNoopCloseOwner{})

	blocked, scanCtx, err := RunSuiteOnce(context.Background(), Options{
		Checks: []string{CheckQueryRecipes}, RunContext: &runCtx,
	})
	require.NoError(t, err)
	require.Len(t, blocked.RepairJournals, 1)
	assert.Empty(t, blocked.Checks, "the selected check must stay blocked by the ontology journal")

	result, _, err := ApplyRepairSession(context.Background(), blocked, scanCtx, Options{
		Fix: true, NonInteractive: true,
		PostApplyRefresher: &repairPostApplyProbe{t: t, result: prepared},
	})
	require.ErrorContains(t, err, "post-apply validation reported 1 check error")
	require.ErrorContains(t, err, CheckOntology+":")
	assert.Equal(t, 1, result.ErrorCount)
	assert.Equal(t, []string{CheckOntology}, result.SelectedChecks,
		"the selected check was blocked by recovery and must not be executed as a postcheck")
	var ontologyResult CheckResult
	for _, check := range result.Checks {
		if check.Name == CheckOntology {
			ontologyResult = check
		}
	}
	assert.Contains(t, ontologyResult.Error, "database is closed", "the ontology check must fail on the closed prepared store")

	evidence, detectErr := DetectPendingRepairJournals(runCtx)
	require.NoError(t, detectErr)
	require.Len(t, evidence, 1, "postcheck errors must retain committed recovery evidence")
	assert.Equal(t, repairJournalCommitted, evidence[0].State)
	assert.Equal(t, []string{CheckOntology}, evidence[0].RequiredChecks)
}

func TestRepairOperationDeltaMarksRenameToEmptyContentChanged(t *testing.T) {
	source := []byte("nonempty\n")
	operation, err := canonicalRepairOperation(RepairOperation{
		ID: "rename-empty", Kind: RepairOperationRename, Path: "old.md", DestinationPath: "new.md",
		SourceHash: SourceHash(source), Content: []byte{},
	})
	require.NoError(t, err)
	require.NotNil(t, operation.Content, "canonicalization must preserve an explicit empty payload")

	changed, renamed, deleted := repairOperationDelta([]RepairOperation{operation})
	assert.Equal(t, []string{"new.md"}, changed)
	assert.Equal(t, []PathRename{{From: "old.md", To: "new.md"}}, renamed)
	assert.Empty(t, deleted)
}

func TestRollbackRepairDeltaMarksRolledBackCreationDeleted(t *testing.T) {
	manifest := repairJournalManifest{
		Changed: []string{"created.md", "edited.md"},
		Entries: []repairJournalEntry{
			{Path: "created.md", OriginalExists: false, FinalExists: true},
			{Path: "edited.md", OriginalExists: true, FinalExists: true},
		},
	}

	changed, renamed, deleted := rollbackRepairDelta(manifest)
	assert.Equal(t, []string{"edited.md"}, changed)
	assert.Empty(t, renamed)
	assert.Equal(t, []string{"created.md"}, deleted)
}

func TestRollbackRepairDeltaMapsEditedRenameBackToOriginalPath(t *testing.T) {
	manifest := repairJournalManifest{
		Changed: []string{"new.md"},
		Renamed: []PathRename{{From: "old.md", To: "new.md"}},
		Entries: []repairJournalEntry{{
			Path: "new.md", OriginalPath: "old.md", OriginalExists: false, FinalExists: true,
		}},
	}

	changed, renamed, deleted := rollbackRepairDelta(manifest)
	assert.Equal(t, []string{"old.md"}, changed)
	assert.Equal(t, []PathRename{{From: "new.md", To: "old.md"}}, renamed)
	assert.Empty(t, deleted)
}

func TestFinalizeRepairPlanRequiresActionMembershipAndOriginatingCheck(t *testing.T) {
	operation := repairWriteOperation("operation:membership", "", "note.md", []byte("before\n"), []byte("after\n"))
	operation.ActionID = ""
	operation.ActionIDs = nil

	_, err := FinalizeRepairPlan(RepairPlan{
		IssueKeys: []string{"issue:membership"}, Operations: []RepairOperation{operation},
	})
	require.ErrorContains(t, err, "requires action membership")

	operation.ActionID = "action:membership"
	_, err = FinalizeRepairPlan(RepairPlan{
		IssueKeys:  []string{"issue:membership"},
		Actions:    []FixAction{{ID: "action:membership", Safety: FixSafetySafe, IssueKeys: []string{"issue:membership"}}},
		Operations: []RepairOperation{operation},
	})
	require.ErrorContains(t, err, "requires an originating check")
}

func TestFinalizeRepairPlanCanonicalizesOriginatingCheckBeforeFingerprint(t *testing.T) {
	before := []byte("before\n")
	operation := repairWriteOperation(
		"operation:canonical-check", "action:canonical-check", "note.md", before, []byte("after\n"),
	)
	operation.IssueKey = "issue:canonical-check"
	build := func(check string) RepairPlan {
		plan, err := FinalizeRepairPlan(RepairPlan{
			IssueKeys: []string{"issue:canonical-check"},
			Actions: []FixAction{{
				ID: "action:canonical-check", Check: check, Safety: FixSafetySafe,
				IssueKeys: []string{"issue:canonical-check"},
			}},
			Operations: []RepairOperation{operation},
		})
		require.NoError(t, err)
		return plan
	}

	fromAlias := build("link-hygiene")
	fromCanonical := build(CheckLinkHygiene)
	assert.Equal(t, CheckLinkHygiene, fromAlias.Actions[0].Check)
	assert.Equal(t, []string{CheckLinkHygiene}, fromAlias.Transactions[0].Checks)
	assert.Equal(t, fromCanonical.Fingerprint, fromAlias.Fingerprint)
}

func TestFinalizeRepairPlanRejectsUnknownOriginatingCheckBeforeWrites(t *testing.T) {
	_, err := FinalizeRepairPlan(RepairPlan{Actions: []FixAction{{
		ID: "action:unknown-check", Check: "not-a-validation-check", Safety: FixSafetyAgent,
		IssueKeys: []string{"issue:unknown-check"},
	}}})
	require.ErrorContains(t, err, "unknown originating check")
}

func TestPostcheckRemainingIssueKeysPreserveProductionEvidenceBeyondMaxIssues(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, writeFixtureFiles(root, map[string]string{
		"target.md": "# Target\n",
		"source.md": "fix: [[target.md]]\none: <one#^one>\ntwo: <two#^two>\nthree: <three#^three>\n",
	}))
	runCtx := linkHygieneRunContext(t, root)
	runCtx.MaxIssues = 1
	planned, scanCtx, err := RunSuiteOnce(context.Background(), Options{
		Checks: []string{CheckLinkHygiene}, RunContext: &runCtx,
	})
	require.NoError(t, err)
	result, _, err := ApplyRepairSession(context.Background(), planned, scanCtx, Options{
		Fix: true, NonInteractive: true, PostApplyRefresher: &repairPostApplyProbe{t: t},
	})
	require.NoError(t, err)
	require.Equal(t, 3, result.IssueCount)
	require.Len(t, result.Checks, 1)
	require.Len(t, result.Checks[0].Issues, 1, "rendered production issues remain capped")
	require.NotNil(t, result.FixExecution)
	assert.Equal(t, 3, result.FixExecution.RemainingFindings)
	assert.Len(t, result.FixExecution.RemainingIssueKeys, 3, "postcheck reporting must retain every production issue key")
	assert.Contains(t, string(mustReadFile(t, filepath.Join(root, "source.md"))), "fix: [[target]]")
}

func TestRepairPlanActionIssueKeysUseProductionEvidenceBeyondMaxIssues(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, writeFixtureFiles(root, map[string]string{
		"source.md":                                 "[[Indexing pipeline architecture]] [[semantic code index spine details]] [[transaction repair journal]]\n",
		"docs/indexing-pipeline-architecture.md":    "# Indexing\n",
		"docs/semantic-code-index-spine-details.md": "# Semantic index\n",
		"docs/transaction-repair-journal.md":        "# Repair journal\n",
	}))
	def := obsidian.VaultDefinition{Name: "test", Path: root, Links: obsidian.LinkTypeBoth}
	runCtx := RunContext{
		VaultDef: def, VaultPath: root, VaultMgr: &fixedVaultMgr{def: def},
		NoteReader: &obsidian.Note{}, NoteMetadata: testNoteMetadata(t), MaxIssues: 1,
	}
	result, _, err := RunSuiteOnce(context.Background(), Options{
		Checks: []string{CheckBrokenLinks}, RunContext: &runCtx,
	})
	require.NoError(t, err)
	require.Len(t, result.Checks, 1)
	require.Len(t, result.Checks[0].Issues, 1, "rendered issues remain capped")
	require.NotNil(t, result.FixPlan)
	require.Len(t, result.FixPlan.Actions, 3)

	var actionIssueKeys []string
	for _, action := range result.FixPlan.Actions {
		require.Len(t, action.IssueKeys, 1)
		require.Len(t, action.Edits, 1)
		var expectedKey string
		for _, issue := range result.Checks[0].fullIssues {
			if issue.Target == action.Edits[0].OldTarget {
				expectedKey = issue.Key
				break
			}
		}
		require.NotEmpty(t, expectedKey)
		assert.Equal(t, expectedKey, action.IssueKeys[0])
		actionIssueKeys = append(actionIssueKeys, action.IssueKeys[0])
	}
	assert.Len(t, sortedUnique(actionIssueKeys), 3, "each action must retain its own full-evidence issue key")
	assert.ElementsMatch(t, resultIssueKeys(result), sortedUnique(actionIssueKeys))
}

func TestRepairOperationDeltaDoesNotMarkEqualContentRenameChanged(t *testing.T) {
	content := []byte("unchanged during rename\n")
	changed, renamed, deleted := repairOperationDelta([]RepairOperation{{
		Kind: RepairOperationRename, Path: "old.md", DestinationPath: "new.md",
		SourceHash: SourceHash(content), Content: content,
	}})

	assert.Empty(t, changed)
	assert.Equal(t, []PathRename{{From: "old.md", To: "new.md"}}, renamed)
	assert.Empty(t, deleted)
}

func TestRepairOperationDeltaMarksEditedRenameDestinationChanged(t *testing.T) {
	changed, renamed, deleted := repairOperationDelta([]RepairOperation{{
		Kind: RepairOperationRename, Path: "old.md", DestinationPath: "new.md",
		Content: []byte("edited during rename\n"),
	}})

	assert.Equal(t, []string{"new.md"}, changed)
	assert.Equal(t, []PathRename{{From: "old.md", To: "new.md"}}, renamed)
	assert.Empty(t, deleted)
}

func TestAttachStableRepairIssueKeysScopesSameCodeActionsByTargetAndPath(t *testing.T) {
	checks := []CheckResult{{
		Name: CheckBrokenLinks,
		Issues: []Issue{
			{Code: "broken_link", Path: "one.md", Source: "one.md", Target: "Old One"},
			{Code: "broken_link", Path: "two.md", Source: "two.md", Target: "Old Two"},
		},
		Fixes: []FixAction{
			{
				ID: "fix-one", Check: CheckBrokenLinks, IssueCode: "broken_link",
				AffectedPaths: []string{"one.md"},
				Edits:         []FixEdit{{Kind: FixKindRewriteLinkGroup, NotePath: "one.md", OldTarget: "Old One"}},
			},
			{
				ID: "fix-two", Check: CheckBrokenLinks, IssueCode: "broken_link",
				AffectedPaths: []string{"two.md"},
				Edits:         []FixEdit{{Kind: FixKindRewriteLinkGroup, NotePath: "two.md", OldTarget: "Old Two"}},
			},
		},
	}}

	keyed, err := attachStableRepairIssueKeys(checks)
	require.NoError(t, err)
	require.Len(t, keyed, 1)
	require.Len(t, keyed[0].Issues, 2)
	require.Len(t, keyed[0].Fixes, 2)
	assert.Equal(t, []string{keyed[0].Issues[0].Key}, keyed[0].Fixes[0].IssueKeys)
	assert.Equal(t, []string{keyed[0].Issues[1].Key}, keyed[0].Fixes[1].IssueKeys)
	assert.NotEqual(t, keyed[0].Fixes[0].IssueKeys, keyed[0].Fixes[1].IssueKeys)
}

func TestAttachStableRepairIssueKeysScopesSamePathOrphanActionsByBlockID(t *testing.T) {
	checks := []CheckResult{{
		Name: CheckOrphanBlockIDs,
		Issues: []Issue{
			{Code: "orphan_block_id", Path: "note.md", Data: mustMarshal(OrphanBlockIDData{BlockID: "first"})},
			{Code: "orphan_block_id", Path: "note.md", Data: mustMarshal(OrphanBlockIDData{BlockID: "second"})},
		},
		Fixes: []FixAction{
			{ID: "remove-first", IssueCode: "orphan_block_id", AffectedPaths: []string{"note.md"}, Edits: []FixEdit{{NotePath: "note.md", BlockID: "first"}}},
			{ID: "remove-second", IssueCode: "orphan_block_id", AffectedPaths: []string{"note.md"}, Edits: []FixEdit{{NotePath: "note.md", BlockID: "second"}}},
		},
	}}

	keyed, err := attachStableRepairIssueKeys(checks)
	require.NoError(t, err)
	assert.Equal(t, []string{keyed[0].Issues[0].Key}, keyed[0].Fixes[0].IssueKeys)
	assert.Equal(t, []string{keyed[0].Issues[1].Key}, keyed[0].Fixes[1].IssueKeys)
}

func TestAttachStableRepairIssueKeysScopesSamePathIdentifierActionsByTarget(t *testing.T) {
	checks := []CheckResult{{
		Name: CheckOntology,
		Issues: []Issue{
			{Code: "identifier_block_id_migration", Path: "note.md", Target: "first-id"},
			{Code: "identifier_block_id_migration", Path: "note.md", Target: "second-id"},
		},
		Fixes: []FixAction{
			{ID: "migrate-first", IssueCode: "identifier_block_id_migration", AffectedPaths: []string{"note.md"}, Edits: []FixEdit{{NotePath: "note.md", BlockID: "first-id"}}},
			{ID: "migrate-second", IssueCode: "identifier_block_id_migration", AffectedPaths: []string{"note.md"}, Edits: []FixEdit{{NotePath: "note.md", BlockID: "second-id"}}},
		},
	}}

	keyed, err := attachStableRepairIssueKeys(checks)
	require.NoError(t, err)
	assert.Equal(t, []string{keyed[0].Issues[0].Key}, keyed[0].Fixes[0].IssueKeys)
	assert.Equal(t, []string{keyed[0].Issues[1].Key}, keyed[0].Fixes[1].IssueKeys)
}

func TestAttachStableRepairIssueKeysScopesAuthoredPropertiesByLogicalField(t *testing.T) {
	checks := []CheckResult{{
		Name: CheckAliases,
		Issues: []Issue{
			{Code: "missing_required_field", Path: "note.md", Field: "identifier"},
			{Code: "missing_required_field", Path: "note.md", Field: "category"},
		},
		Fixes: []FixAction{
			{ID: "identifier-from-path:note.md:identifier", IssueCode: "missing_required_field", AffectedPaths: []string{"note.md"}, Edits: []FixEdit{{NotePath: "note.md", Property: "id"}}},
			{ID: "identifier-from-path:note.md:category", IssueCode: "missing_required_field", AffectedPaths: []string{"note.md"}, Edits: []FixEdit{{NotePath: "note.md", Property: "type"}}},
		},
	}}

	keyed, err := attachStableRepairIssueKeys(checks)
	require.NoError(t, err)
	assert.Equal(t, []string{keyed[0].Issues[0].Key}, keyed[0].Fixes[0].IssueKeys)
	assert.Equal(t, []string{keyed[0].Issues[1].Key}, keyed[0].Fixes[1].IssueKeys)
}

func TestApplyFixPlanReportsEveryUnbackedActionWithoutCountingAppliedAction(t *testing.T) {
	root := t.TempDir()
	before := []byte("before\n")
	require.NoError(t, os.WriteFile(filepath.Join(root, "note.md"), before, 0o640))
	operation := repairWriteOperation("op:backed", "action:backed", "note.md", before, []byte("after\n"))
	operation.IssueKey = "issue:backed"
	operation.IssueKeys = []string{"issue:backed"}
	plan := mustFinalizePostApplyPlan(t, operation)
	plan.Actions = append(plan.Actions,
		FixAction{ID: "action:safe-no-op", Safety: FixSafetySafe, IssueKeys: []string{"issue:safe-no-op"}},
		FixAction{ID: "action:agent-no-op", Safety: FixSafetyAgent, IssueKeys: []string{"issue:agent-no-op"}},
	)
	finalized, err := FinalizeRepairPlan(plan)
	require.NoError(t, err)

	execution, err := ApplyFixPlan(context.Background(), repairRunContext(t, root), &finalized, Options{
		Fix: true, NonInteractive: true,
	})
	require.NoError(t, err)
	assert.Equal(t, []string{"action:agent-no-op", "action:safe-no-op"}, execution.Skipped)
	assert.Equal(t, []string{"issue:agent-no-op", "issue:safe-no-op"}, execution.RemainingIssueKeys)
	assert.Equal(t, 2, execution.RemainingFindings)
	assert.NotContains(t, execution.RemainingIssueKeys, "issue:backed")
}

func TestApplyFixPlanRejectsSafetyMutationWithPreservedCustomActionID(t *testing.T) {
	root := t.TempDir()
	before := []byte("before\n")
	path := filepath.Join(root, "note.md")
	require.NoError(t, os.WriteFile(path, before, 0o640))
	action := FixAction{
		ID: "custom-action-id", Check: CheckLinkHygiene, IssueCode: "review-test",
		Kind: "review-test", Safety: FixSafetyConfirm, IssueKeys: []string{"issue:safety"},
	}
	operation := repairWriteOperation("op:safety", action.ID, "note.md", before, []byte("after\n"))
	operation.IssueKey = "issue:safety"
	operation.IssueKeys = []string{"issue:safety"}
	plan, err := FinalizeRepairPlan(RepairPlan{
		IssueKeys: []string{"issue:safety"}, Actions: []FixAction{action}, Operations: []RepairOperation{operation},
	})
	require.NoError(t, err)
	plan.Actions[0].Safety = FixSafetySafe

	_, err = ApplyFixPlan(context.Background(), repairRunContext(t, root), &plan, Options{
		Fix: true, NonInteractive: true,
	})
	require.ErrorContains(t, err, "fingerprint changed")
	assert.Equal(t, before, mustReadFile(t, path))
}

func TestApplyFixPlanRejectsPayloadMutationWithPreservedCustomActionID(t *testing.T) {
	root := t.TempDir()
	before := []byte("before\n")
	require.NoError(t, os.WriteFile(filepath.Join(root, "note.md"), before, 0o640))
	plan, err := FinalizeRepairPlan(RepairPlan{
		IssueKeys: []string{"issue:payload"},
		Actions: []FixAction{{
			ID: "custom-action-id", Check: CheckLinkHygiene, IssueCode: "review-test",
			Kind: "review-test", Safety: FixSafetySafe, IssueKeys: []string{"issue:payload"},
			Edits: []FixEdit{{Kind: FixKindRewriteLinkGroup, NotePath: "note.md", OldTarget: "old"}},
		}},
	})
	require.NoError(t, err)
	plan.Actions[0].Edits[0].OldTarget = "mutated"

	_, err = ApplyFixPlan(context.Background(), repairRunContext(t, root), &plan, Options{
		Fix: true, NonInteractive: true,
	})
	require.ErrorContains(t, err, "fingerprint changed")
}

func TestApplyFixPlanRejectsActionOnlySafetyMutationWithPreservedCustomActionID(t *testing.T) {
	plan, err := FinalizeRepairPlan(RepairPlan{
		IssueKeys: []string{"issue:action-only"},
		Actions: []FixAction{{
			ID: "custom-action-only", Check: CheckLinkHygiene, IssueCode: "review-test",
			Kind: "review-test", Safety: FixSafetyAgent, IssueKeys: []string{"issue:action-only"},
		}},
	})
	require.NoError(t, err)
	plan.Actions[0].Safety = FixSafetySafe

	_, err = ApplyFixPlan(context.Background(), repairRunContext(t, t.TempDir()), &plan, Options{
		Fix: true, NonInteractive: true,
	})
	require.ErrorContains(t, err, "fingerprint changed")
}
