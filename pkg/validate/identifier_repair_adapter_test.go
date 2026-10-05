package validate

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/ontology/reference"
	"github.com/atomicobject/rhizome/pkg/validate/identifierreconcile"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"
)

func TestIdentifierRepairAdapterMapsAndAppliesOneAtomicRenameTransaction(t *testing.T) {
	fixture := buildIdentifierRepairAdapterFixture(t)
	plan, err := BuildIdentifierRepairPlan(context.Background(), fixture.runCtx, fixture.assembly, fixture.bindings)
	require.NoError(t, err)
	for _, operation := range plan.Operations {
		if operation.Kind == RepairOperationWrite && operation.Path == fixture.oldPath {
			require.Contains(t, string(operation.Content), "aliases: [KEEP, SPEC-0002]")
			require.NotContains(t, string(operation.Content), "aliases: [SPEC-0001")
		}
	}
	require.True(t, plan.RequiresLeaseHeldReplan)
	require.Len(t, plan.Actions, 1, "postchecks must not become selectable repair actions")
	require.Len(t, plan.Operations, 3)
	require.Len(t, plan.Transactions, 1, "membership identity must connect loser, inbound link, and rename")
	require.Equal(t, []string{CheckBrokenLinks, CheckIdentifiers, CheckOntology}, plan.Transactions[0].Checks)
	for _, operation := range plan.Operations {
		require.Equal(t, []string{fixture.bindings[0].Action.ID}, operation.ActionIDs)
		require.Equal(t, fixture.bindings[0].Action.IssueKeys, operation.IssueKeys)
		require.Equal(t, []string{CheckBrokenLinks, CheckIdentifiers, CheckOntology}, operation.RequiredChecks)
		require.Contains(t, operation.SourceHash, "sha256:")
		require.Contains(t, operation.Identities, "identifier-membership:"+fixture.membership)
	}

	_, err = ApplyFixPlan(context.Background(), fixture.runCtx, plan, Options{Fix: true, NonInteractive: true})
	require.ErrorContains(t, err, "requires lease-held authoritative revalidation")

	refresher := &identifierRepairRuntimeRefresher{t: t, runCtx: fixture.runCtx}
	execution, err := ApplyIdentifierRepairPlan(context.Background(), fixture.runCtx, plan, fixture.assembly, fixture.bindings, Options{
		Fix: true, NonInteractive: true, PostApplyRefresher: refresher,
	})
	require.NoError(t, err)
	require.Equal(t, []string{fixture.bindings[0].Action.ID}, execution.Applied)
	require.NotNil(t, execution.IdentifierReconciliationTimings)
	require.Positive(t, execution.IdentifierReconciliationTimings.Apply)
	require.Positive(t, execution.IdentifierReconciliationTimings.PostValidation)
	require.Equal(t, 1, refresher.calls)
	require.Equal(t, []string{"notes/inbound.md", fixture.newPath}, refresher.changed[0])
	require.Equal(t, []PathRename{{From: fixture.oldPath, To: fixture.newPath}}, refresher.renamed[0])
	evidence, detectErr := DetectPendingRepairJournals(fixture.runCtx)
	require.NoError(t, detectErr)
	require.Empty(t, evidence, "successful engine-owned postchecks must precede journal cleanup")
	_, err = os.Stat(filepath.Join(fixture.root, filepath.FromSlash(fixture.oldPath)))
	require.ErrorIs(t, err, os.ErrNotExist)
	destination, err := os.ReadFile(filepath.Join(fixture.root, filepath.FromSlash(fixture.newPath)))
	require.NoError(t, err)
	require.Contains(t, string(destination), "id: SPEC-0002")
	require.Contains(t, string(destination), "aliases: [KEEP, SPEC-0002]")
	require.Contains(t, string(destination), "[[specs/SPEC-0002-loser]]")
	inbound, err := os.ReadFile(filepath.Join(fixture.root, "notes", "inbound.md"))
	require.NoError(t, err)
	require.Equal(t, "[[specs/SPEC-0002-loser]]\n", string(inbound))
}

func TestIdentifierRepairAdapterBuildRevalidatesCompleteInventory(t *testing.T) {
	fixture := buildIdentifierRepairAdapterFixture(t)
	writeIdentifierAdapterFile(t, fixture.root, "notes/omitted-inbound.md", "[[specs/SPEC-0001-loser]]\n")
	_, err := BuildIdentifierRepairPlan(context.Background(), fixture.runCtx, fixture.assembly, fixture.bindings)
	require.ErrorContains(t, err, "source inventory changed")
}

func TestIdentifierRepairAdapterApplyFailsClosedWithoutPostcheckOrchestration(t *testing.T) {
	fixture := buildIdentifierRepairAdapterFixture(t)
	plan, err := BuildIdentifierRepairPlan(context.Background(), fixture.runCtx, fixture.assembly, fixture.bindings)
	require.NoError(t, err)
	_, err = ApplyIdentifierRepairPlan(context.Background(), fixture.runCtx, plan, fixture.assembly, fixture.bindings, Options{Fix: true, NonInteractive: true})
	require.ErrorContains(t, err, "requires prepared refresh and held-lease postcheck")
	_, statErr := os.Stat(filepath.Join(fixture.root, filepath.FromSlash(fixture.oldPath)))
	require.NoError(t, statErr)
}

func TestIdentifierRepairAdapterRejectsDifferentSealedAssemblyWithIdenticalPhysicalOperations(t *testing.T) {
	fixture := buildIdentifierRepairAdapterFixture(t)
	reviewed, err := BuildIdentifierRepairPlan(context.Background(), fixture.runCtx, fixture.assembly, fixture.bindings)
	require.NoError(t, err)

	alternate := buildIdentifierRepairAdapterAssembly(t, fixture.root, true)
	alternatePlan, err := BuildIdentifierRepairPlan(context.Background(), fixture.runCtx, alternate, fixture.bindings)
	require.NoError(t, err)
	require.Equal(t, reviewed.Operations, alternatePlan.Operations, "provenance-only assembly drift must leave physical work unchanged")
	require.NotEqual(t, reviewed.AuthorityFingerprint, alternatePlan.AuthorityFingerprint)
	require.NotEqual(t, reviewed.Fingerprint, alternatePlan.Fingerprint, "the reviewed plan fingerprint must bind complete assembly authority")

	execution, err := ApplyIdentifierRepairPlan(context.Background(), fixture.runCtx, reviewed, alternate, fixture.bindings, Options{
		Fix: true, NonInteractive: true, PostApplyRefresher: &identifierRepairRuntimeRefresher{t: t, runCtx: fixture.runCtx},
	})
	require.Nil(t, execution)
	require.ErrorContains(t, err, "different identifier repair assembly")
	_, statErr := os.Stat(filepath.Join(fixture.root, filepath.FromSlash(fixture.oldPath)))
	require.NoError(t, statErr)
}

func TestIdentifierRepairAdapterPreservesReviewOnlyProseEvidenceInPlanAndApplyReport(t *testing.T) {
	fixture := buildIdentifierRepairAdapterFixtureWithFiles(t, map[string]string{
		"notes/review.md": "Review SPEC-0001 in prose before publishing.\n",
	})
	plan, err := BuildIdentifierRepairPlan(context.Background(), fixture.runCtx, fixture.assembly, fixture.bindings)
	require.NoError(t, err)
	require.Len(t, plan.FollowUps, 1)
	followUp := plan.FollowUps[0]
	require.Equal(t, "notes/review.md", followUp.SourcePath)
	require.NotEmpty(t, followUp.SourceHash)
	require.Equal(t, fixture.membership, followUp.MembershipKeys[0])
	require.False(t, followUp.Diagnostic.Blocking)
	require.NotNil(t, followUp.Diagnostic.Field)
	require.Equal(t, reference.IdentifierRewriteDiagnosticReviewOnly, followUp.Diagnostic.Field.Kind)
	require.Equal(t, "SPEC-0001", followUp.Diagnostic.Field.Value)
	planJSON, err := json.Marshal(plan)
	require.NoError(t, err)
	require.Contains(t, string(planJSON), `"followUps":[{`)
	require.Contains(t, string(planJSON), `"sourcePath":"notes/review.md"`)

	execution, err := ApplyIdentifierRepairPlan(context.Background(), fixture.runCtx, plan, fixture.assembly, fixture.bindings, Options{
		Fix: true, NonInteractive: true, PostApplyRefresher: &identifierRepairRuntimeRefresher{t: t, runCtx: fixture.runCtx},
	})
	require.NoError(t, err)
	require.Equal(t, plan.FollowUps, execution.FollowUps)
	executionJSON, err := json.Marshal(execution)
	require.NoError(t, err)
	require.Contains(t, string(executionJSON), `"followUps":[{`)
	require.Contains(t, string(executionJSON), `"sourcePath":"notes/review.md"`)
	reviewBytes, err := os.ReadFile(filepath.Join(fixture.root, "notes", "review.md"))
	require.NoError(t, err)
	require.Equal(t, "Review SPEC-0001 in prose before publishing.\n", string(reviewBytes), "review-only evidence must never become an executable edit")
}

func TestIdentifierRepairAdapterRejectsReviewedPlanWithoutLeaseMarker(t *testing.T) {
	fixture := buildIdentifierRepairAdapterFixture(t)
	plan, err := BuildIdentifierRepairPlan(context.Background(), fixture.runCtx, fixture.assembly, fixture.bindings)
	require.NoError(t, err)
	plan.RequiresLeaseHeldReplan = false
	finalized, err := FinalizeRepairPlan(*plan)
	require.NoError(t, err)
	_, err = ApplyIdentifierRepairPlan(context.Background(), fixture.runCtx, &finalized, fixture.assembly, fixture.bindings, Options{
		Fix: true, NonInteractive: true, PostApplyRefresher: &identifierRepairRuntimeRefresher{t: t, runCtx: fixture.runCtx},
	})
	require.ErrorContains(t, err, "lacks the engine-owned lease revalidation marker")
	_, statErr := os.Stat(filepath.Join(fixture.root, filepath.FromSlash(fixture.oldPath)))
	require.NoError(t, statErr)
}

func TestIdentifierRepairAdapterRejectsReviewedRequiredCheckMutation(t *testing.T) {
	fixture := buildIdentifierRepairAdapterFixture(t)
	plan, err := BuildIdentifierRepairPlan(context.Background(), fixture.runCtx, fixture.assembly, fixture.bindings)
	require.NoError(t, err)
	for index := range plan.Operations {
		plan.Operations[index].RequiredChecks = []string{CheckAliases}
	}
	finalized, err := FinalizeRepairPlan(*plan)
	require.NoError(t, err)
	_, err = ApplyIdentifierRepairPlan(context.Background(), fixture.runCtx, &finalized, fixture.assembly, fixture.bindings, Options{
		Fix: true, NonInteractive: true, PostApplyRefresher: &identifierRepairRuntimeRefresher{t: t, runCtx: fixture.runCtx},
	})
	require.ErrorContains(t, err, "lease-held repair differs from reviewed plan")
	_, statErr := os.Stat(filepath.Join(fixture.root, filepath.FromSlash(fixture.oldPath)))
	require.NoError(t, statErr)
}

func TestIdentifierRepairAdapterCanonicalizesCallerMutableDerivedTransactions(t *testing.T) {
	fixture := buildIdentifierRepairAdapterFixture(t)
	plan, err := BuildIdentifierRepairPlan(context.Background(), fixture.runCtx, fixture.assembly, fixture.bindings)
	require.NoError(t, err)
	require.Len(t, plan.Transactions, 1)
	wantChecks := append([]string(nil), plan.Transactions[0].Checks...)
	plan.Transactions[0].Checks = []string{"not-a-real-check"}

	canonical, err := canonicalReviewedIdentifierRepairPlan(plan)
	require.NoError(t, err, "derived transaction fields are rebuilt from fingerprinted operations and actions")
	require.Equal(t, wantChecks, canonical.Transactions[0].Checks)
	require.NotContains(t, canonical.Transactions[0].Checks, "not-a-real-check")

	execution, err := ApplyIdentifierRepairPlan(context.Background(), fixture.runCtx, plan, fixture.assembly, fixture.bindings, Options{
		Fix: true, NonInteractive: true,
		PostApplyRefresher: &identifierRepairRuntimeRefresher{t: t, runCtx: fixture.runCtx},
	})
	require.NoError(t, err)
	require.Equal(t, []string{fixture.bindings[0].Action.ID}, execution.Applied)
}

func TestIdentifierRepairAdapterPostcheckErrorRecoversWithoutRemappingOrSecondMutation(t *testing.T) {
	fixture := buildIdentifierRepairAdapterFixture(t)
	plan, err := BuildIdentifierRepairPlan(context.Background(), fixture.runCtx, fixture.assembly, fixture.bindings)
	require.NoError(t, err)
	firstRefresh := identifierInvalidOwnershipRefresher{}
	firstExecution, err := ApplyIdentifierRepairPlan(context.Background(), fixture.runCtx, plan, fixture.assembly, fixture.bindings, Options{
		Fix: true, NonInteractive: true, PostApplyRefresher: firstRefresh,
	})
	require.ErrorContains(t, err, "prepared post-apply runtime has no cleanup owner")
	require.NotNil(t, firstExecution)
	evidence, detectErr := DetectPendingRepairJournals(fixture.runCtx)
	require.NoError(t, detectErr)
	require.Len(t, evidence, 1)
	require.Equal(t, []string{CheckBrokenLinks, CheckIdentifiers, CheckOntology}, evidence[0].RequiredChecks)
	require.Equal(t, repairJournalCommitted, evidence[0].State)

	destinationPath := filepath.Join(fixture.root, filepath.FromSlash(fixture.newPath))
	inboundPath := filepath.Join(fixture.root, "notes", "inbound.md")
	destinationAfterFirstApply, err := os.ReadFile(destinationPath)
	require.NoError(t, err)
	inboundAfterFirstApply, err := os.ReadFile(inboundPath)
	require.NoError(t, err)

	// Invalidate the sealed source inventory after commit. Recovery must replay
	// the manifest checks without invoking the mapper against this changed vault.
	writeIdentifierAdapterFile(t, fixture.root, "notes/added-after-commit.md", "# Added after commit\n")
	secondRefresh := &identifierRepairRuntimeRefresher{t: t, runCtx: fixture.runCtx}
	secondExecution, err := ApplyIdentifierRepairPlan(context.Background(), fixture.runCtx, plan, fixture.assembly, fixture.bindings, Options{
		Fix: true, NonInteractive: true, Checks: []string{"not-a-real-check"}, PostApplyRefresher: secondRefresh,
	})
	require.NoError(t, err)
	require.Zero(t, secondExecution.RemainingFindings)
	require.Equal(t, 1, secondRefresh.calls)
	require.Equal(t, []string{"notes/inbound.md", fixture.newPath}, secondRefresh.changed[0])
	require.Equal(t, []PathRename{{From: fixture.oldPath, To: fixture.newPath}}, secondRefresh.renamed[0])
	destinationAfterRecovery, err := os.ReadFile(destinationPath)
	require.NoError(t, err)
	require.Equal(t, destinationAfterFirstApply, destinationAfterRecovery, "committed recovery must not mutate the vault twice")
	inboundAfterRecovery, err := os.ReadFile(inboundPath)
	require.NoError(t, err)
	require.Equal(t, inboundAfterFirstApply, inboundAfterRecovery, "committed recovery must not rewrite inbound links twice")
	evidence, detectErr = DetectPendingRepairJournals(fixture.runCtx)
	require.NoError(t, detectErr)
	require.Empty(t, evidence, "committed journal is removed only after replayed postchecks succeed")
}

func TestIdentifierRepairAdapterReportsOrdinaryPostcheckFindingsWithoutRetainingJournal(t *testing.T) {
	const oldPath = "specs/SPEC-0001-loser.md"
	tests := []struct {
		name  string
		extra map[string]string
	}{
		{
			name: "unrelated note",
			extra: map[string]string{
				"specs/unrelated.md": "---\nid: SPEC-0099\naliases: [OTHER]\n---\n# Unrelated\n",
			},
		},
		{
			name: "affected note",
			extra: map[string]string{
				oldPath: "---\nid: SPEC-0001\naliases: [SPEC-0001, KEEP]\n---\n# Loser\n[[specs/SPEC-0001-loser]]\n[[missing-target]]\n",
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fixture := buildIdentifierRepairAdapterFixtureWithFiles(t, tt.extra)
			plan, err := BuildIdentifierRepairPlan(context.Background(), fixture.runCtx, fixture.assembly, fixture.bindings)
			require.NoError(t, err)
			execution, err := ApplyIdentifierRepairPlan(context.Background(), fixture.runCtx, plan, fixture.assembly, fixture.bindings, Options{
				Fix: true, NonInteractive: true,
				PostApplyRefresher: &identifierRepairRuntimeRefresher{t: t, runCtx: fixture.runCtx},
			})
			require.NoError(t, err)
			require.Positive(t, execution.RemainingFindings)
			require.NotEmpty(t, execution.RemainingIssueKeys)
			evidence, detectErr := DetectPendingRepairJournals(fixture.runCtx)
			require.NoError(t, detectErr)
			require.Empty(t, evidence, "ordinary findings remain guidance, not journal cleanup blockers")
		})
	}
}

func TestIdentifierRepairAdapterRequiresCurrentLeaseForExactRunContextVault(t *testing.T) {
	fixture := buildIdentifierRepairAdapterFixture(t)
	lease, release, err := acquireRepairIndexLockLease(filepath.Join(fixture.root, ".rhizome", "index.lock"))
	require.NoError(t, err)
	wrong := fixture.runCtx
	wrong.VaultDef = obsidian.VaultDefinition{Path: t.TempDir()}
	_, err = buildIdentifierRepairPlanUnderLease(context.Background(), wrong, lease, fixture.assembly, fixture.bindings)
	require.ErrorContains(t, err, "roots disagree")
	require.NoError(t, release())
	_, err = buildIdentifierRepairPlanUnderLease(context.Background(), fixture.runCtx, lease, fixture.assembly, fixture.bindings)
	require.ErrorContains(t, err, "not held")
}

func TestIdentifierRepairAdapterRequiresExactConsumerActionCoverage(t *testing.T) {
	fixture := buildIdentifierRepairAdapterFixture(t)
	_, err := BuildIdentifierRepairPlan(context.Background(), fixture.runCtx, fixture.assembly, nil)
	require.ErrorContains(t, err, "missing identifier repair binding")

	extra := append([]IdentifierRepairActionBinding(nil), fixture.bindings...)
	extra = append(extra, IdentifierRepairActionBinding{MembershipKey: "extra", Action: fixture.bindings[0].Action})
	_, err = BuildIdentifierRepairPlan(context.Background(), fixture.runCtx, fixture.assembly, extra)
	require.ErrorContains(t, err, "extraneous identifier repair binding")

	duplicate := append([]IdentifierRepairActionBinding(nil), fixture.bindings...)
	duplicate = append(duplicate, fixture.bindings[0])
	_, err = BuildIdentifierRepairPlan(context.Background(), fixture.runCtx, fixture.assembly, duplicate)
	require.ErrorContains(t, err, "duplicate identifier repair binding")
}

func TestIdentifierRepairAdapterPromptsAndReportsOnlyConsumerAction(t *testing.T) {
	fixture := buildIdentifierRepairAdapterFixture(t)
	fixture.bindings[0].Action.Safety = FixSafetyConfirm
	fixture.bindings[0].Action.Question = "Apply identifier repair?"
	plan, err := BuildIdentifierRepairPlan(context.Background(), fixture.runCtx, fixture.assembly, fixture.bindings)
	require.NoError(t, err)
	require.Len(t, plan.Actions, 1)
	prompts := 0
	execution, err := ApplyIdentifierRepairPlan(context.Background(), fixture.runCtx, plan, fixture.assembly, fixture.bindings, Options{
		Fix: true, PostApplyRefresher: &identifierRepairRuntimeRefresher{t: t, runCtx: fixture.runCtx},
		Confirm: func(question string) (bool, error) {
			prompts++
			require.Equal(t, "Apply identifier repair?", question)
			return true, nil
		},
	})
	require.NoError(t, err)
	require.Equal(t, 1, prompts)
	require.Equal(t, []string{fixture.bindings[0].Action.ID}, execution.Applied)
}

type identifierRepairAdapterFixture struct {
	root       string
	oldPath    string
	newPath    string
	membership string
	runCtx     RunContext
	assembly   *identifierreconcile.RepairAssembly
	bindings   []IdentifierRepairActionBinding
}

func buildIdentifierRepairAdapterFixture(t *testing.T) identifierRepairAdapterFixture {
	return buildIdentifierRepairAdapterFixtureWithFiles(t, nil)
}

func buildIdentifierRepairAdapterFixtureWithFiles(t *testing.T, extraFiles map[string]string) identifierRepairAdapterFixture {
	t.Helper()
	root := t.TempDir()
	writeIdentifierAdapterFile(t, root, ".rhizome/ontology/identifiers.graphql", `
type Spec @node(paths: ["specs/*.md"]) {
  id: String! @field @identifier(preferred: true, prefix: "SPEC", separator: "-", pad: 4)
  aliases: [String!] @field
}
`)
	const oldPath = "specs/SPEC-0001-loser.md"
	writeIdentifierAdapterFile(t, root, "specs/000-keeper.md", "---\nid: SPEC-0001\naliases: [SPEC-0001]\n---\n# Keeper\n")
	writeIdentifierAdapterFile(t, root, oldPath, "---\nid: SPEC-0001\naliases: [SPEC-0001, KEEP]\n---\n# Loser\n[[specs/SPEC-0001-loser]]\n")
	writeIdentifierAdapterFile(t, root, "notes/inbound.md", "[[specs/SPEC-0001-loser]]\n")
	for path, content := range extraFiles {
		writeIdentifierAdapterFile(t, root, path, content)
	}
	return buildIdentifierRepairAdapterFixtureFromRoot(t, root, false)
}

func buildIdentifierRepairAdapterAssembly(t *testing.T, root string, withGitEvidence bool) *identifierreconcile.RepairAssembly {
	t.Helper()
	return buildIdentifierRepairAdapterFixtureFromRoot(t, root, withGitEvidence).assembly
}

func buildIdentifierRepairAdapterFixtureFromRoot(t *testing.T, root string, withGitEvidence bool) identifierRepairAdapterFixture {
	t.Helper()
	const oldPath = "specs/SPEC-0001-loser.md"

	format := &ontology.IdentifierFormat{Strategy: ontology.IdentifierStrategySequential, Prefix: "SPEC", Separator: "-", Pad: 4}
	pool, err := identifierreconcile.NewPoolKey(format)
	require.NoError(t, err)
	claim := func(notePath string) identifierreconcile.Claim {
		node, nodeErr := identifierreconcile.NewCanonicalNodeKey(notePath, "", "Spec", "id")
		require.NoError(t, nodeErr)
		return identifierreconcile.Claim{Node: node, Pool: pool, Value: "SPEC-0001", Kind: identifierreconcile.ClaimPreferred}
	}
	inventory, err := identifierreconcile.BuildInventory([]identifierreconcile.Claim{claim("specs/000-keeper.md"), claim(oldPath)})
	require.NoError(t, err)
	var evidence map[string]identifierreconcile.ProvenanceEvidence
	if withGitEvidence {
		evidence = map[string]identifierreconcile.ProvenanceEvidence{
			claim("specs/000-keeper.md").ID(): {
				AuthorDate: time.Date(2026, time.July, 1, 12, 0, 0, 0, time.UTC),
				FullOID:    strings.Repeat("1", 40), Complete: true,
			},
			claim(oldPath).ID(): {
				AuthorDate: time.Date(2026, time.July, 2, 12, 0, 0, 0, time.UTC),
				FullOID:    strings.Repeat("2", 40), Complete: true,
			},
		}
	}
	identifierPlan, err := identifierreconcile.BuildPlan(inventory, evidence)
	require.NoError(t, err)
	require.Len(t, identifierPlan.Collisions, 1)
	collision := identifierPlan.Collisions[0]
	require.Len(t, collision.Losers, 1)
	require.Equal(t, oldPath, collision.Losers[0].Claim.Node.NotePath)
	require.Equal(t, "SPEC-0002", collision.Losers[0].Replacement)
	newPath := "specs/SPEC-0002-loser.md"
	oldRef := ontology.NodeRef{NotePath: oldPath, TypeName: "Spec", Kind: ontology.NodeKindNote}
	newRef := ontology.NodeRef{NotePath: newPath, TypeName: "Spec", Kind: ontology.NodeKindNote}
	rewrite := reference.IdentifierRewrite{
		Mode: reference.IdentifierRewritePreferredRekey, OldRef: oldRef, NewRef: newRef,
		OldIdentifier: "SPEC-0001", NewIdentifier: "SPEC-0002", PreferredField: "id", AliasesField: "aliases",
	}
	moveRequest := obsidian.GovernedMoveRequest{
		SourcePath: oldPath, OldID: "SPEC-0001", NewID: "SPEC-0002",
		SiblingPaths: []string{"specs/000-keeper.md", oldPath},
	}
	movePlan, conflict, err := obsidian.PlanGovernedIdentifierMove(moveRequest)
	require.NoError(t, err)
	require.Nil(t, conflict)
	vaultDef := obsidian.VaultDefinition{Path: root}
	fields, err := identifierreconcile.DiscoverIdentifierFields(context.Background(), identifierreconcile.IdentifierFieldDiscoveryRequest{VaultDef: vaultDef, RootRewrites: []reference.IdentifierRewrite{rewrite}})
	require.NoError(t, err)
	links, err := identifierreconcile.DiscoverIdentifierLinks(context.Background(), identifierreconcile.IdentifierLinkDiscoveryRequest{FieldDiscovery: fields})
	require.NoError(t, err)
	assembly, err := identifierreconcile.AssembleRekeyRepairIntents(identifierreconcile.RekeyRepairAssemblyInput{
		Plan: identifierPlan,
		Collisions: []identifierreconcile.CollisionRepairInput{{
			CollisionKey: collision.Key, Rewrites: []reference.IdentifierRewrite{rewrite},
			Moves: []identifierreconcile.GovernedMoveResult{{Request: moveRequest, Plan: movePlan}},
		}},
		FieldDiscovery: fields, LinkDiscovery: links,
	})
	require.NoError(t, err)
	action := FixAction{
		ID: "action:duplicate-identifier", Check: CheckAliases, Kind: "resolve_duplicate_identifier",
		Safety: FixSafetySafe, Title: "Resolve duplicate identifier", IssueKeys: []string{"issue:duplicate-identifier"},
	}
	return identifierRepairAdapterFixture{
		root: root, oldPath: oldPath, newPath: newPath, membership: collision.Key,
		runCtx: RunContext{
			VaultDef: vaultDef, VaultPath: root,
			VaultMgr: identifierRepairVaultManager{def: vaultDef}, NoteReader: &obsidian.Note{}, NoteMetadata: testNoteMetadata(t),
		},
		assembly: assembly,
		bindings: []IdentifierRepairActionBinding{{MembershipKey: collision.Key, Action: action}},
	}
}

type identifierRepairVaultManager struct {
	def obsidian.VaultDefinition
}

func (m identifierRepairVaultManager) DefaultName() (string, error) { return m.def.Name, nil }
func (m identifierRepairVaultManager) SetDefaultName(string) error  { return nil }
func (m identifierRepairVaultManager) Path() (string, error)        { return m.def.BasePath(), nil }
func (m identifierRepairVaultManager) Definition() (obsidian.VaultDefinition, error) {
	return m.def, nil
}

type identifierRepairRuntimeRefresher struct {
	t       *testing.T
	runCtx  RunContext
	calls   int
	changed [][]string
	renamed [][]PathRename
	deleted [][]string
}

func (r *identifierRepairRuntimeRefresher) Refresh(
	ctx context.Context,
	lease *IndexLockLease,
	changed []string,
	renamed []PathRename,
	deleted []string,
) (PostApplyRefreshResult, error) {
	r.t.Helper()
	require.NoError(r.t, lease.RequireHeldForVault(r.runCtx.VaultPath))
	r.calls++
	r.changed = append(r.changed, append([]string(nil), changed...))
	r.renamed = append(r.renamed, append([]PathRename(nil), renamed...))
	r.deleted = append(r.deleted, append([]string(nil), deleted...))
	runtime, cleanup, err := ontology.EnsureFreshRuntime(ctx, r.runCtx.NoteMetadata, r.runCtx.VaultDef, &obsidian.Note{})
	if err != nil {
		if cleanup != nil {
			cleanup()
		}
		return PostApplyRefreshResult{}, err
	}
	return NewPostApplyRefreshResult(runtime, identifierRepairRuntimeCleanup{cleanup: cleanup}), nil
}

type identifierRepairRuntimeCleanup struct {
	cleanup func()
}

func (c identifierRepairRuntimeCleanup) Close() error {
	if c.cleanup != nil {
		c.cleanup()
	}
	return nil
}

type identifierInvalidOwnershipRefresher struct{}

func (identifierInvalidOwnershipRefresher) Refresh(
	context.Context,
	*IndexLockLease,
	[]string,
	[]PathRename,
	[]string,
) (PostApplyRefreshResult, error) {
	return PostApplyRefreshResult{Runtime: &ontology.Runtime{}}, nil
}

func writeIdentifierAdapterFile(t *testing.T, root, rel, content string) {
	t.Helper()
	abs := filepath.Join(root, filepath.FromSlash(rel))
	require.NoError(t, os.MkdirAll(filepath.Dir(abs), 0o755))
	require.NoError(t, os.WriteFile(abs, []byte(content), 0o644))
}
