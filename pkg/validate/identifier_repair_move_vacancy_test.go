package validate

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/ontology/reference"
	"github.com/atomicobject/rhizome/pkg/validate/identifierreconcile"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIdentifierRepairMapperCarriesStableDestinationVacancyPrecondition(t *testing.T) {
	fixture := buildIdentifierRepairAdapterFixture(t)
	plan, err := BuildIdentifierRepairPlan(context.Background(), fixture.runCtx, fixture.assembly, fixture.bindings)
	require.NoError(t, err)

	var rename RepairOperation
	for _, operation := range plan.Operations {
		if operation.Kind == RepairOperationRename {
			rename = operation
			break
		}
	}
	require.NotEmpty(t, rename.ID)
	require.Equal(t, &RepairDestinationVacancyPrecondition{
		DestinationPath:                fixture.newPath,
		RequireExactVacancy:            true,
		RequirePortableCaseFoldVacancy: true,
	}, rename.DestinationVacancy)
	withoutVacancy := *plan
	withoutVacancy.Operations = append([]RepairOperation(nil), plan.Operations...)
	for index := range withoutVacancy.Operations {
		if withoutVacancy.Operations[index].Kind == RepairOperationRename {
			withoutVacancy.Operations[index].DestinationVacancy = nil
		}
	}
	withoutVacancy.Fingerprint = ""
	changedAuthority, err := FinalizeRepairPlan(withoutVacancy)
	require.NoError(t, err)
	require.NotEqual(t, plan.Fingerprint, changedAuthority.Fingerprint, "vacancy authority must be fingerprinted")

	// Runtime directory contents are not plan authority. They are checked while
	// preparing the affected transaction under the apply lease.
	require.NoError(t, os.Mkdir(filepath.Join(fixture.root, filepath.FromSlash(fixture.newPath)), 0o755))
	revalidated, err := FinalizeRepairPlan(*plan)
	require.NoError(t, err)
	require.Equal(t, plan.Fingerprint, revalidated.Fingerprint)
}

func TestDestinationVacancyFailureIsTransactionLocal(t *testing.T) {
	tests := []struct {
		name        string
		appear      func(t *testing.T, root string)
		wantBlocked bool
	}{
		{
			name: "exact destination directory",
			appear: func(t *testing.T, root string) {
				require.NoError(t, os.Mkdir(filepath.Join(root, "renamed.md"), 0o755))
			},
			wantBlocked: true,
		},
		{
			name: "portable uppercase extension sibling",
			appear: func(t *testing.T, root string) {
				require.NoError(t, os.WriteFile(filepath.Join(root, "RENAMED.MD"), []byte("occupied\n"), 0o640))
			},
			wantBlocked: true,
		},
		{
			name: "unrelated sibling",
			appear: func(t *testing.T, root string) {
				require.NoError(t, os.WriteFile(filepath.Join(root, "UNRELATED.MD"), []byte("unrelated\n"), 0o640))
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			source := []byte("rename source\n")
			independentBefore := []byte("independent before\n")
			independentAfter := []byte("independent after\n")
			require.NoError(t, os.WriteFile(filepath.Join(root, "source.md"), source, 0o640))
			require.NoError(t, os.WriteFile(filepath.Join(root, "independent.md"), independentBefore, 0o640))
			plan := mustRepairPlan(t, []repairPlanInput{
				{
					actionID: "action:vacancy", issueKey: "issue:vacancy",
					operations: []RepairOperation{{
						ID: "operation:a-vacancy", Kind: RepairOperationRename,
						Path: "source.md", DestinationPath: "renamed.md", SourceHash: SourceHash(source),
						DestinationVacancy: &RepairDestinationVacancyPrecondition{
							DestinationPath: "renamed.md", RequireExactVacancy: true,
							RequirePortableCaseFoldVacancy: true,
						},
					}},
				},
				{
					actionID: "action:independent", issueKey: "issue:independent",
					operations: []RepairOperation{
						repairWriteOperation("operation:z-independent", "action:independent", "independent.md", independentBefore, independentAfter),
					},
				},
			})
			fingerprint := plan.Fingerprint
			test.appear(t, root)
			revalidated, err := FinalizeRepairPlan(plan)
			require.NoError(t, err)
			require.Equal(t, fingerprint, revalidated.Fingerprint)

			execution, applyErr := ApplyFixPlan(context.Background(), repairRunContext(t, root), &plan, Options{
				Fix: true, NonInteractive: true, ReplanCommand: "rzm validate fix aliases",
			})
			if test.wantBlocked {
				require.NoError(t, applyErr)
				require.NotContains(t, execution.Failed, "action:vacancy")
				require.Contains(t, execution.Skipped, "action:vacancy")
				require.Contains(t, execution.Applied, "action:independent")
				require.Contains(t, execution.RemainingIssueKeys, "issue:vacancy")
				require.Equal(t, "rzm validate fix aliases", execution.ReplanCommand)
				require.Equal(t, source, mustReadFile(t, filepath.Join(root, "source.md")))
				require.Equal(t, independentAfter, mustReadFile(t, filepath.Join(root, "independent.md")))
				statusByPath := make(map[string]string)
				for _, transaction := range execution.Transactions {
					for _, affected := range transaction.AffectedPaths {
						statusByPath[affected] = transaction.Status
					}
				}
				require.Equal(t, "skipped_conflict", statusByPath["source.md"])
				require.Equal(t, "applied", statusByPath["independent.md"])
				return
			}

			require.NoError(t, applyErr)
			require.Contains(t, execution.Applied, "action:vacancy")
			require.Contains(t, execution.Applied, "action:independent")
			assert.NoFileExists(t, filepath.Join(root, "source.md"))
			require.Equal(t, source, mustReadFile(t, filepath.Join(root, "renamed.md")))
			require.Equal(t, independentAfter, mustReadFile(t, filepath.Join(root, "independent.md")))
		})
	}
}

func TestIdentifierRepairAdapterAppliesIndependentCollisionWhenReviewedMoveDestinationBecomesOccupied(t *testing.T) {
	tests := []struct {
		name                   string
		appear                 func(t *testing.T, root, destination string)
		changesSourceInventory bool
	}{
		{
			name: "exact non-note directory",
			appear: func(t *testing.T, root, destination string) {
				require.NoError(t, os.Mkdir(filepath.Join(root, filepath.FromSlash(destination)), 0o755))
			},
		},
		{
			name: "portable uppercase note sibling",
			appear: func(t *testing.T, root, destination string) {
				sibling := strings.TrimSuffix(destination, ".md") + ".MD"
				writeIdentifierAdapterFile(t, root, sibling, "non-note sibling\n")
			},
			changesSourceInventory: true,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fixture := buildTwoCollisionIdentifierRepairFixture(t)
			plan, err := BuildIdentifierRepairPlan(context.Background(), fixture.runCtx, fixture.assembly, fixture.bindings)
			require.NoError(t, err)
			require.Len(t, plan.Transactions, 2)
			blocked, independent := fixture.components[0], fixture.components[1]
			blockedTransaction := identifierTransactionForAction(t, *plan, blocked.actionID)
			independentTransaction := identifierTransactionForAction(t, *plan, independent.actionID)

			test.appear(t, fixture.root, blocked.destination)
			lease, release, err := acquireRepairIndexLockLease(filepath.Join(fixture.root, ".rhizome", "index.lock"))
			require.NoError(t, err)
			heldPlan, err := buildIdentifierRepairPlanUnderLease(context.Background(), fixture.runCtx, lease, fixture.assembly, fixture.bindings)
			if test.changesSourceInventory {
				require.ErrorContains(t, err, "source inventory changed after planning")
				require.NoError(t, release())
				require.FileExists(t, filepath.Join(fixture.root, filepath.FromSlash(blocked.source)))
				require.FileExists(t, filepath.Join(fixture.root, filepath.FromSlash(independent.source)))
				return
			}
			require.NoError(t, err)
			require.Equal(t, plan.Fingerprint, heldPlan.Fingerprint, "runtime directory inventory must not alter held plan authority")
			require.NoError(t, release())

			execution, err := ApplyIdentifierRepairPlan(context.Background(), fixture.runCtx, plan, fixture.assembly, fixture.bindings, Options{
				Fix: true, NonInteractive: true, ReplanCommand: "rzm validate fix aliases",
				PostApplyRefresher: &identifierRepairRuntimeRefresher{t: t, runCtx: fixture.runCtx},
			})
			require.NoError(t, err, "live vacancy must not become a whole-plan fingerprint mismatch")
			require.Contains(t, execution.Skipped, blocked.actionID)
			require.NotContains(t, execution.Failed, blocked.actionID)
			require.Contains(t, execution.Applied, independent.actionID)
			require.Contains(t, execution.RemainingIssueKeys, blocked.issueKey)
			require.Equal(t, "rzm validate fix aliases", execution.ReplanCommand)
			statusByTransaction := make(map[string]string)
			for _, transaction := range execution.Transactions {
				statusByTransaction[transaction.TransactionID] = transaction.Status
			}
			require.Equal(t, "skipped_conflict", statusByTransaction[blockedTransaction])
			require.Equal(t, "applied", statusByTransaction[independentTransaction])

			assert.FileExists(t, filepath.Join(fixture.root, filepath.FromSlash(blocked.source)))
			blockedContent, err := os.ReadFile(filepath.Join(fixture.root, filepath.FromSlash(blocked.source)))
			require.NoError(t, err)
			require.NotContains(t, string(blockedContent), "id: "+blocked.replacement, "the skipped transaction must not apply its semantic write")
			assert.NoFileExists(t, filepath.Join(fixture.root, filepath.FromSlash(independent.source)))
			applied, err := os.ReadFile(filepath.Join(fixture.root, filepath.FromSlash(independent.destination)))
			require.NoError(t, err)
			require.Contains(t, string(applied), "id: "+independent.replacement)
		})
	}
}

type twoCollisionIdentifierRepairFixture struct {
	root       string
	runCtx     RunContext
	assembly   *identifierreconcile.RepairAssembly
	bindings   []IdentifierRepairActionBinding
	components []twoCollisionIdentifierComponent
}

type twoCollisionIdentifierComponent struct {
	membership  string
	actionID    string
	issueKey    string
	source      string
	destination string
	replacement string
}

func buildTwoCollisionIdentifierRepairFixture(t *testing.T) twoCollisionIdentifierRepairFixture {
	t.Helper()
	root := t.TempDir()
	writeIdentifierAdapterFile(t, root, ".rhizome/ontology/identifiers.graphql", `
type Spec @node(paths: ["specs/*.md"]) {
  id: String! @field @identifier(preferred: true, prefix: "SPEC", separator: "-", pad: 4)
  aliases: [String!] @field
}
`)
	notePaths := []string{
		"specs/000-a-keeper.md",
		"specs/SPEC-0001-a-loser.md",
		"specs/001-b-keeper.md",
		"specs/SPEC-0002-b-loser.md",
	}
	writeIdentifierAdapterFile(t, root, notePaths[0], "---\nid: SPEC-0001\naliases: [SPEC-0001]\n---\n# A Keeper\n")
	writeIdentifierAdapterFile(t, root, notePaths[1], "---\nid: SPEC-0001\naliases: [SPEC-0001]\n---\n# A Loser\n")
	writeIdentifierAdapterFile(t, root, notePaths[2], "---\nid: SPEC-0002\naliases: [SPEC-0002]\n---\n# B Keeper\n")
	writeIdentifierAdapterFile(t, root, notePaths[3], "---\nid: SPEC-0002\naliases: [SPEC-0002]\n---\n# B Loser\n")

	format := &ontology.IdentifierFormat{Strategy: ontology.IdentifierStrategySequential, Prefix: "SPEC", Separator: "-", Pad: 4}
	pool, err := identifierreconcile.NewPoolKey(format)
	require.NoError(t, err)
	claim := func(notePath, identifier string) identifierreconcile.Claim {
		node, nodeErr := identifierreconcile.NewCanonicalNodeKey(notePath, "", "Spec", "id")
		require.NoError(t, nodeErr)
		return identifierreconcile.Claim{Node: node, Pool: pool, Value: identifier, Kind: identifierreconcile.ClaimPreferred}
	}
	inventory, err := identifierreconcile.BuildInventory([]identifierreconcile.Claim{
		claim(notePaths[0], "SPEC-0001"), claim(notePaths[1], "SPEC-0001"),
		claim(notePaths[2], "SPEC-0002"), claim(notePaths[3], "SPEC-0002"),
	})
	require.NoError(t, err)
	identifierPlan, err := identifierreconcile.BuildPlan(inventory, nil)
	require.NoError(t, err)
	require.Len(t, identifierPlan.Collisions, 2)

	var rewrites []reference.IdentifierRewrite
	var collisionInputs []identifierreconcile.CollisionRepairInput
	var bindings []IdentifierRepairActionBinding
	var components []twoCollisionIdentifierComponent
	for index, collision := range identifierPlan.Collisions {
		require.Len(t, collision.Losers, 1)
		loser := collision.Losers[0]
		moveRequest := obsidian.GovernedMoveRequest{
			SourcePath:   loser.Claim.Node.NotePath,
			OldID:        loser.Claim.Value,
			NewID:        loser.Replacement,
			SiblingPaths: append([]string(nil), notePaths...),
		}
		movePlan, conflict, moveErr := obsidian.PlanGovernedIdentifierMove(moveRequest)
		require.NoError(t, moveErr)
		require.Nil(t, conflict)
		require.True(t, movePlan.Renamed, "loser=%s old=%s new=%s", loser.Claim.Node.NotePath, loser.Claim.Value, loser.Replacement)
		rewrite := reference.IdentifierRewrite{
			Mode:          reference.IdentifierRewritePreferredRekey,
			OldRef:        ontology.NodeRef{NotePath: movePlan.SourcePath, TypeName: "Spec", Kind: ontology.NodeKindNote},
			NewRef:        ontology.NodeRef{NotePath: movePlan.DestinationPath, TypeName: "Spec", Kind: ontology.NodeKindNote},
			OldIdentifier: loser.Claim.Value, NewIdentifier: loser.Replacement,
			PreferredField: "id", AliasesField: "aliases",
		}
		rewrites = append(rewrites, rewrite)
		collisionInputs = append(collisionInputs, identifierreconcile.CollisionRepairInput{
			CollisionKey: collision.Key,
			Rewrites:     []reference.IdentifierRewrite{rewrite},
			Moves:        []identifierreconcile.GovernedMoveResult{{Request: moveRequest, Plan: movePlan}},
		})
		actionID := fmt.Sprintf("action:duplicate-identifier-%d", index+1)
		issueKey := fmt.Sprintf("issue:duplicate-identifier-%d", index+1)
		bindings = append(bindings, IdentifierRepairActionBinding{MembershipKey: collision.Key, Action: FixAction{
			ID: actionID, Check: CheckAliases, Kind: "resolve_duplicate_identifier",
			Safety: FixSafetySafe, Title: "Resolve duplicate identifier", IssueKeys: []string{issueKey},
		}})
		components = append(components, twoCollisionIdentifierComponent{
			membership: collision.Key, actionID: actionID, issueKey: issueKey,
			source: movePlan.SourcePath, destination: movePlan.DestinationPath, replacement: loser.Replacement,
		})
	}
	vaultDef := obsidian.VaultDefinition{Path: root}
	fields, err := identifierreconcile.DiscoverIdentifierFields(context.Background(), identifierreconcile.IdentifierFieldDiscoveryRequest{
		VaultDef: vaultDef, RootRewrites: rewrites,
	})
	require.NoError(t, err)
	links, err := identifierreconcile.DiscoverIdentifierLinks(context.Background(), identifierreconcile.IdentifierLinkDiscoveryRequest{FieldDiscovery: fields})
	require.NoError(t, err)
	assembly, err := identifierreconcile.AssembleRekeyRepairIntents(identifierreconcile.RekeyRepairAssemblyInput{
		Plan: identifierPlan, Collisions: collisionInputs, FieldDiscovery: fields, LinkDiscovery: links,
	})
	require.NoError(t, err)
	runCtx := RunContext{
		VaultDef: vaultDef, VaultPath: root,
		VaultMgr: identifierRepairVaultManager{def: vaultDef}, NoteReader: &obsidian.Note{}, NoteMetadata: testNoteMetadata(t),
	}
	return twoCollisionIdentifierRepairFixture{
		root: root, runCtx: runCtx, assembly: assembly, bindings: bindings, components: components,
	}
}

func identifierTransactionForAction(t *testing.T, plan RepairPlan, actionID string) string {
	t.Helper()
	operations := make(map[string]RepairOperation, len(plan.Operations))
	for _, operation := range plan.Operations {
		operations[operation.ID] = operation
	}
	for _, transaction := range plan.Transactions {
		for _, operationID := range transaction.OperationIDs {
			for _, candidate := range operations[operationID].ActionIDs {
				if candidate == actionID {
					return transaction.ID
				}
			}
		}
	}
	t.Fatalf("transaction for action %s not found", actionID)
	return ""
}
