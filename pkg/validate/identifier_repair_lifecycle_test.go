package validate

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/ontology/reference"
	"github.com/atomicobject/rhizome/pkg/validate/identifierreconcile"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"
)

func TestIdentifierRepairAdapterAllowsClosedEffortInboundBrokenLinkRepair(t *testing.T) {
	const closedPath = "efforts/closed.md"
	const closedContent = "---\ntype: EffortNote\nstatus: complete\n---\n# Closed\n[[specs/SPEC-0001-loser]]\n"
	fixture := buildIdentifierRepairAdapterFixtureWithFiles(t, map[string]string{
		closedPath: closedContent,
	})
	plan, err := BuildIdentifierRepairPlan(context.Background(), fixture.runCtx, fixture.assembly, fixture.bindings)
	require.NoError(t, err)

	operation := identifierRepairOperationForPath(t, plan, closedPath)
	require.Equal(t, LifecycleAllowed, operation.Lifecycle.Decision)
	require.Len(t, operation.LifecycleClaims, 1)
	claim := operation.LifecycleClaims[0]
	require.Equal(t, LifecycleEditBrokenLink, claim.Kind)
	require.Equal(t, "specs/SPEC-0001-loser", claim.ExpectedText)
	require.Equal(t, "specs/SPEC-0002-loser", claim.Replacement)
	require.Equal(t, claim.ExpectedText, closedContent[claim.StartByte:claim.EndByte])

	execution, err := ApplyIdentifierRepairPlan(context.Background(), fixture.runCtx, plan, fixture.assembly, fixture.bindings, Options{
		Fix: true, NonInteractive: true,
		PostApplyRefresher: &identifierRepairRuntimeRefresher{t: t, runCtx: fixture.runCtx},
	})
	require.NoError(t, err)
	require.Equal(t, []string{fixture.bindings[0].Action.ID}, execution.Applied)
	content, err := os.ReadFile(filepath.Join(fixture.root, filepath.FromSlash(closedPath)))
	require.NoError(t, err)
	require.Contains(t, string(content), "[[specs/SPEC-0002-loser]]")
}

func TestIdentifierRepairAdapterAllowsClosedEffortRekeyAndRenameWithHistoricalAuthority(t *testing.T) {
	fixture := buildClosedEffortIdentifierRepairFixture(t, false)
	plan, err := BuildIdentifierRepairPlan(context.Background(), fixture.runCtx, fixture.assembly, fixture.bindings)
	require.NoError(t, err)
	before, err := os.ReadFile(filepath.Join(fixture.root, filepath.FromSlash(fixture.oldPath)))
	require.NoError(t, err)

	// Without historical authority the protected rekey and rename are skipped
	// and leave the closed effort untouched.
	skipped, err := ApplyIdentifierRepairPlan(context.Background(), fixture.runCtx, plan, fixture.assembly, fixture.bindings, Options{
		Fix: true, NonInteractive: true,
		PostApplyRefresher: &identifierRepairRuntimeRefresher{t: t, runCtx: fixture.runCtx},
	})
	require.NoError(t, err)
	require.Equal(t, []string{fixture.bindings[0].Action.ID}, skipped.Skipped)
	require.Empty(t, skipped.Applied)
	require.Equal(t, fixture.bindings[0].Action.IssueKeys, skipped.RemainingIssueKeys)
	require.Len(t, skipped.Transactions, 1)
	require.Equal(t, "skipped_lifecycle", skipped.Transactions[0].Status)
	after, err := os.ReadFile(filepath.Join(fixture.root, filepath.FromSlash(fixture.oldPath)))
	require.NoError(t, err)
	require.Equal(t, before, after)
	_, err = os.Stat(filepath.Join(fixture.root, filepath.FromSlash(fixture.newPath)))
	require.ErrorIs(t, err, os.ErrNotExist)

	execution, err := ApplyIdentifierRepairPlan(context.Background(), fixture.runCtx, plan, fixture.assembly, fixture.bindings, Options{
		Fix: true, NonInteractive: true, AllowHistorical: true,
		PostApplyRefresher: &identifierRepairRuntimeRefresher{t: t, runCtx: fixture.runCtx},
	})
	require.NoError(t, err)
	require.Equal(t, []string{fixture.bindings[0].Action.ID}, execution.Applied)
	_, err = os.Stat(filepath.Join(fixture.root, filepath.FromSlash(fixture.oldPath)))
	require.ErrorIs(t, err, os.ErrNotExist)
	content, err := os.ReadFile(filepath.Join(fixture.root, filepath.FromSlash(fixture.newPath)))
	require.NoError(t, err)
	require.Contains(t, string(content), "id: EFF-0002")
	require.Contains(t, string(content), "aliases: [EFF-0002]")
	require.NotContains(t, string(content), "EFF-0001", "the collided identifier must not survive as a loser alias")
}

func TestIdentifierRepairAdapterKeepsMixedClosedEffortLinkAndIdentifierWriteProtected(t *testing.T) {
	fixture := buildClosedEffortIdentifierRepairFixture(t, true)
	plan, err := BuildIdentifierRepairPlan(context.Background(), fixture.runCtx, fixture.assembly, fixture.bindings)
	require.NoError(t, err)

	operation := identifierRepairOperationForPath(t, plan, fixture.oldPath)
	require.Equal(t, RepairOperationWrite, operation.Kind)
	require.Equal(t, LifecycleProtected, operation.Lifecycle.Decision)
	require.Contains(t, operation.Lifecycle.Reason, "uncovered output changes")
	require.Len(t, operation.LifecycleClaims, 1)
	require.Equal(t, LifecycleEditBrokenLink, operation.LifecycleClaims[0].Kind)
	require.Contains(t, string(operation.Content), "id: EFF-0002")
	require.Contains(t, string(operation.Content), "[[efforts/EFF-0002-loser]]")

	execution, err := ApplyIdentifierRepairPlan(context.Background(), fixture.runCtx, plan, fixture.assembly, fixture.bindings, Options{
		Fix: true, NonInteractive: true,
		PostApplyRefresher: &identifierRepairRuntimeRefresher{t: t, runCtx: fixture.runCtx},
	})
	require.NoError(t, err)
	require.Len(t, execution.Transactions, 1)
	require.Equal(t, "skipped_lifecycle", execution.Transactions[0].Status)
	content, err := os.ReadFile(filepath.Join(fixture.root, filepath.FromSlash(fixture.oldPath)))
	require.NoError(t, err)
	require.Contains(t, string(content), "id: EFF-0001")
	require.Contains(t, string(content), "[[efforts/EFF-0001-loser]]")
}

func identifierRepairOperationForPath(t *testing.T, plan *RepairPlan, notePath string) RepairOperation {
	t.Helper()
	for _, operation := range plan.Operations {
		if operation.Path == notePath && operation.Kind == RepairOperationWrite {
			return operation
		}
	}
	require.FailNow(t, "identifier repair write operation not found", notePath)
	return RepairOperation{}
}

func buildClosedEffortIdentifierRepairFixture(t *testing.T, selfLink bool) identifierRepairAdapterFixture {
	t.Helper()
	root := t.TempDir()
	writeIdentifierAdapterFile(t, root, ".rhizome/ontology/identifiers.graphql", `
enum EffortStatus { planned active complete archived }
type EffortNote @node(paths: ["efforts/*.md"]) {
  id: String! @field @identifier(preferred: true, prefix: "EFF", separator: "-", pad: 4)
  aliases: [String!] @field
  status: EffortStatus!
  createdAt: DateTime! @field(source: "created-at")
  summary: String!
}
`)
	const oldPath = "efforts/EFF-0001-loser.md"
	writeIdentifierAdapterFile(t, root, "efforts/000-keeper.md", "---\ntype: EffortNote\nid: EFF-0001\naliases: [EFF-0001]\nstatus: active\ncreated-at: 2026-07-15T00:00:00Z\nsummary: Keeper\n---\n# Keeper\n")
	loser := "---\ntype: EffortNote\nid: EFF-0001\naliases: [EFF-0001]\nstatus: complete\ncreated-at: 2026-07-15T00:00:00Z\nsummary: Loser\n---\n# Loser\n"
	if selfLink {
		loser += "[[efforts/EFF-0001-loser]]\n"
	}
	writeIdentifierAdapterFile(t, root, oldPath, loser)
	if !selfLink {
		writeIdentifierAdapterFile(t, root, "notes/inbound.md", "[[efforts/EFF-0001-loser]]\n")
	}

	format := &ontology.IdentifierFormat{Strategy: ontology.IdentifierStrategySequential, Prefix: "EFF", Separator: "-", Pad: 4}
	pool, err := identifierreconcile.NewPoolKey(format)
	require.NoError(t, err)
	claim := func(notePath string) identifierreconcile.Claim {
		node, nodeErr := identifierreconcile.NewCanonicalNodeKey(notePath, "", "EffortNote", "id")
		require.NoError(t, nodeErr)
		return identifierreconcile.Claim{Node: node, Pool: pool, Value: "EFF-0001", Kind: identifierreconcile.ClaimPreferred}
	}
	inventory, err := identifierreconcile.BuildInventory([]identifierreconcile.Claim{claim("efforts/000-keeper.md"), claim(oldPath)})
	require.NoError(t, err)
	identifierPlan, err := identifierreconcile.BuildPlan(inventory, nil)
	require.NoError(t, err)
	require.Len(t, identifierPlan.Collisions, 1)
	collision := identifierPlan.Collisions[0]
	require.Len(t, collision.Losers, 1)
	require.Equal(t, oldPath, collision.Losers[0].Claim.Node.NotePath)
	require.Equal(t, "EFF-0002", collision.Losers[0].Replacement)

	const newPath = "efforts/EFF-0002-loser.md"
	oldRef := ontology.NodeRef{NotePath: oldPath, TypeName: "EffortNote", Kind: ontology.NodeKindNote}
	newRef := ontology.NodeRef{NotePath: newPath, TypeName: "EffortNote", Kind: ontology.NodeKindNote}
	rewrite := reference.IdentifierRewrite{
		Mode: reference.IdentifierRewritePreferredRekey, OldRef: oldRef, NewRef: newRef,
		OldIdentifier: "EFF-0001", NewIdentifier: "EFF-0002", PreferredField: "id", AliasesField: "aliases",
	}
	moveRequest := obsidian.GovernedMoveRequest{
		SourcePath: oldPath, OldID: "EFF-0001", NewID: "EFF-0002",
		SiblingPaths: []string{"efforts/000-keeper.md", oldPath},
	}
	movePlan, conflict, err := obsidian.PlanGovernedIdentifierMove(moveRequest)
	require.NoError(t, err)
	require.Nil(t, conflict)
	vaultDef := obsidian.VaultDefinition{Path: root}
	fields, err := identifierreconcile.DiscoverIdentifierFields(context.Background(), identifierreconcile.IdentifierFieldDiscoveryRequest{
		VaultDef: vaultDef, RootRewrites: []reference.IdentifierRewrite{rewrite},
	})
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
		ID: "action:duplicate-effort-identifier", Check: CheckAliases, Kind: "resolve_duplicate_identifier",
		Safety: FixSafetySafe, Title: "Resolve duplicate effort identifier", IssueKeys: []string{"issue:duplicate-effort-identifier"},
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
