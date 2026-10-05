package validate

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"
)

func TestRunIdentifiersWithRuntimeAppliesDateTimeCollisionWithoutRenamingEffortFile(t *testing.T) {
	const (
		keeperPath = "docs/efforts/2026-08-05-14-32-alpha.md"
		loserPath  = "docs/efforts/2026-08-05-14-32-beta.md"
		inbound    = "docs/plans/inbound.md"
	)
	root, runtime := identifierCheckRuntime(t, `
type EffortNote @node(paths: ["docs/efforts/*.md"], propertyCase: AS_DEFINED) {
  id: String! @field @identifier(preferred: true, strategy: DATETIME, prefix: "EFF")
  aliases: [String!] @field
}
`, map[string]string{
		keeperPath: "---\nid: EFF-2026-08-05-14-32\naliases: [EFF-2026-08-05-14-32]\n---\n# Alpha\n",
		loserPath:  "---\nid: EFF-2026-08-05-14-32\naliases: [EFF-2026-08-05-14-32]\n---\n# Beta\n",
		inbound:    "[[../efforts/2026-08-05-14-32-beta|Beta effort]]\n",
	})
	runCtx := RunContext{
		VaultDef:     obsidian.VaultDefinition{Path: root, Links: obsidian.LinkTypeBoth},
		VaultPath:    root,
		NoteReader:   &obsidian.Note{},
		NoteMetadata: testNoteMetadata(t),
		MaxIssues:    20,
	}

	result := RunIdentifiersWithRuntime(context.Background(), runCtx, runtime)
	require.Empty(t, result.Error)
	require.Equal(t, 2, result.IssueCount)
	require.NotNil(t, result.identifierRepair)
	require.Len(t, result.identifierReconciliation.Plan.Collisions, 1)
	collision := result.identifierReconciliation.Plan.Collisions[0]
	require.Equal(t, "eff-2026-08-05-14-32", collision.Value)
	require.Len(t, collision.Losers, 1)
	require.Equal(t, "EFF-2026-08-05-14-32-2", collision.Losers[0].Replacement)

	plan, err := BuildRepairPlan(context.Background(), runCtx, []CheckResult{result})
	require.NoError(t, err)
	require.NotNil(t, plan)
	for _, operation := range plan.Operations {
		require.NotEqual(t, RepairOperationRename, operation.Kind, "DATETIME effort IDs must not rename timestamp-based filenames")
	}

	refresher := &identifierRepairRuntimeRefresher{t: t, runCtx: runCtx}
	execution, err := ApplyIdentifierRepairPlan(context.Background(), runCtx, plan, result.identifierRepair.Assembly, result.identifierRepair.Bindings, Options{
		Fix: true, Confirm: func(string) (bool, error) { return true, nil }, PostApplyRefresher: refresher,
	})
	require.NoError(t, err)
	require.NotNil(t, execution)
	require.Len(t, refresher.renamed, 1)
	require.Empty(t, refresher.renamed[0])

	for _, path := range []string{keeperPath, loserPath} {
		_, err = os.Stat(filepath.Join(root, filepath.FromSlash(path)))
		require.NoError(t, err, "timestamp-based effort filename must remain at %s", path)
	}
	loser, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(loserPath)))
	require.NoError(t, err)
	require.Contains(t, string(loser), "id: EFF-2026-08-05-14-32-2")
	require.Contains(t, string(loser), "aliases: [EFF-2026-08-05-14-32-2]")
	require.NotContains(t, string(loser), "aliases: [EFF-2026-08-05-14-32]")
	linked, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(inbound)))
	require.NoError(t, err)
	require.Equal(t, "[[../efforts/2026-08-05-14-32-beta|Beta effort]]\n", string(linked), "path-based references remain valid when the effort filename is stable")
}

func TestRunIdentifiersWithRuntimeRejectsNoncanonicalDateTimePreferredIdentifier(t *testing.T) {
	root, runtime := identifierCheckRuntime(t, `
type EffortNote @node(paths: ["docs/efforts/*.md"], propertyCase: AS_DEFINED) {
  id: String! @field @identifier(preferred: true, strategy: DATETIME, prefix: "EFF")
  aliases: [String!] @field
}
`, map[string]string{
		"docs/efforts/2026-08-05-14-32-invalid.md": "---\ntype: EffortNote\nid: EFF-2026-08-05-14-32-02\naliases: [EFF-2026-08-05-14-32-02]\n---\n# Invalid\n",
	})
	runCtx := RunContext{VaultDef: obsidian.VaultDefinition{Path: root}, VaultPath: root, NoteReader: &obsidian.Note{}, MaxIssues: 20}

	result := RunIdentifiersWithRuntime(context.Background(), runCtx, runtime)
	require.Empty(t, result.Error)
	require.Equal(t, 1, result.IssueCount)
	require.Len(t, result.Issues, 1)
	require.Equal(t, issueCodeIdentifierDateTimeFormatMismatch, result.Issues[0].Code)
	require.Equal(t, "EFF-2026-08-05-14-32-02", result.Issues[0].Target)
	require.Len(t, result.Fixes, 1)
	require.Equal(t, FixSafetyAgent, result.Fixes[0].Safety)
	require.Nil(t, result.identifierRepair)
}

func TestRunIdentifiersWithRuntimeDoesNotRevalidateDateTimeIdentifierAgainstCurrentFilename(t *testing.T) {
	root, runtime := identifierCheckRuntime(t, `
type EffortNote @node(paths: ["docs/efforts/*.md"], propertyCase: AS_DEFINED) {
  id: String! @field @identifier(preferred: true, strategy: DATETIME, prefix: "EFF")
  aliases: [String!] @field
}
`, map[string]string{
		"docs/efforts/2026-09-01-09-00-moved.md": "---\nid: EFF-2026-08-05-14-32\naliases: [EFF-2026-08-05-14-32]\n---\n# Moved\n",
	})

	result := RunIdentifiersWithRuntime(context.Background(), RunContext{
		VaultDef: obsidian.VaultDefinition{Path: root}, VaultPath: root, NoteReader: &obsidian.Note{}, MaxIssues: 20,
	}, runtime)
	require.Empty(t, result.Error)
	require.Zero(t, result.IssueCount, "the filename is allocation input, not a continuous identity constraint")
}

func TestRunIdentifiersWithRuntimeRetainsHistoricalAliasesInDateTimeCollisionDiscovery(t *testing.T) {
	root, runtime := identifierCheckRuntime(t, `
type EffortNote @node(paths: ["docs/efforts/*.md"], propertyCase: AS_DEFINED) {
  id: String! @field @identifier(preferred: true, strategy: DATETIME, prefix: "EFF")
  aliases: [String!] @field
}
`, map[string]string{
		"docs/efforts/a.md": "---\nid: EFF-2026-08-05-14-32\naliases: [EFF-2026-08-05-14-32, LEGACY]\n---\n",
		"docs/efforts/b.md": "---\nid: EFF-2026-08-05-14-33\naliases: [EFF-2026-08-05-14-33, LEGACY]\n---\n",
	})

	result := RunIdentifiersWithRuntime(context.Background(), RunContext{
		VaultDef: obsidian.VaultDefinition{Path: root}, VaultPath: root, NoteReader: &obsidian.Note{}, MaxIssues: 20,
	}, runtime)
	require.Empty(t, result.Error)
	var historicalAliasIssues int
	for _, issue := range result.Issues {
		if issue.Code == "duplicate_identifier_alias" && issue.Target == "legacy" {
			historicalAliasIssues++
		}
	}
	require.Equal(t, 2, historicalAliasIssues, "non-strategy historical aliases remain complete collision claims")
}

func TestRunIdentifiersWithRuntimeSkipsGitProvenanceWithoutCollisions(t *testing.T) {
	root, runtime := identifierCheckRuntime(t, `
type Spec @node(paths: ["specs/*.md"], propertyCase: AS_DEFINED) {
  id: String! @field @identifier(preferred: true, prefix: "SPEC", separator: "-", pad: 4)
  aliases: [String!] @field
}
`, map[string]string{
		"specs/a.md": "---\nid: SPEC-0001\naliases: [SPEC-0001]\n---\n",
		"specs/b.md": "---\nid: SPEC-0002\naliases: [SPEC-0002]\n---\n",
	})

	result := RunIdentifiersWithRuntime(context.Background(), RunContext{
		VaultDef: obsidian.VaultDefinition{Path: root}, VaultPath: root, NoteReader: &obsidian.Note{}, MaxIssues: 20,
	}, runtime)

	require.Zero(t, result.IssueCount)
	require.NotNil(t, result.identifierReconciliation)
	require.NotNil(t, result.identifierReconciliation.Plan)
	require.Empty(t, result.identifierReconciliation.Plan.Collisions)
	require.NotEmpty(t, result.identifierReconciliation.Plan.Fingerprint)
	require.True(t, result.identifierReconciliation.Diagnostics.HistoryComplete)
	require.Zero(t, result.identifierReconciliation.Diagnostics.Git.Commands)
	require.Zero(t, result.identifierReconciliation.Diagnostics.Git.UniquePaths)
	require.Zero(t, result.identifierReconciliation.Diagnostics.Timings.GitProvenance)
	require.Zero(t, result.identifierReconciliation.Diagnostics.Timings.ReferenceDiscovery)
	require.Zero(t, result.identifierReconciliation.Diagnostics.Timings.TransactionConstruction)
	require.Nil(t, result.identifierRepair)
}

func TestRunIdentifiersWithRuntimeScopesGitProvenanceToCollisionClaimants(t *testing.T) {
	root, runtime := identifierCheckRuntime(t, `
type Spec @node(paths: ["specs/*.md"], propertyCase: AS_DEFINED) {
  id: String! @field @identifier(preferred: true, prefix: "SPEC", separator: "-", pad: 4)
  aliases: [String!] @field
}
`, map[string]string{
		"specs/a.md": "---\nid: SPEC-0001\naliases: [SPEC-0001]\n---\n",
		"specs/b.md": "---\nid: SPEC-0001\naliases: [SPEC-0001]\n---\n",
		"specs/c.md": "---\nid: SPEC-0002\naliases: [SPEC-0002]\n---\n",
	})
	commitIdentifierCheckFixture(t, root)

	result := RunIdentifiersWithRuntime(context.Background(), RunContext{
		VaultDef: obsidian.VaultDefinition{Path: root}, VaultPath: root, NoteReader: &obsidian.Note{}, MaxIssues: 20,
	}, runtime)

	require.Equal(t, 2, result.IssueCount)
	require.NotNil(t, result.identifierReconciliation)
	require.Len(t, result.identifierReconciliation.Plan.Collisions, 1)
	require.True(t, result.identifierReconciliation.Diagnostics.HistoryComplete)
	require.Equal(t, 2, result.identifierReconciliation.Diagnostics.Git.UniquePaths)
	require.Positive(t, result.identifierReconciliation.Diagnostics.Timings.GitProvenance)
}

func TestRunIdentifiersWithRuntimeBuildsPoolAwareExecutableRepair(t *testing.T) {
	root, runtime := identifierCheckRuntime(t, `
type Spec @node(paths: ["specs/*.md"], propertyCase: AS_DEFINED) {
  id: String! @field @identifier(preferred: true, prefix: "SPEC", separator: "-", pad: 4)
  aliases: [String!] @field
}
`, map[string]string{
		"specs/SPEC-0001-keeper.md": "---\nid: SPEC-0001\naliases: [SPEC-0001]\n---\n",
		"specs/SPEC-0001-loser.md":  "---\nid: SPEC-0001\naliases: [SPEC-0001]\n---\n",
	})

	result := RunIdentifiersWithRuntime(context.Background(), RunContext{
		VaultDef: obsidian.VaultDefinition{Path: root}, VaultPath: root, NoteReader: &obsidian.Note{}, MaxIssues: 20,
	}, runtime)

	require.Equal(t, CheckIdentifiers, result.Name)
	require.Equal(t, 2, result.IssueCount)
	require.Len(t, result.Issues, 2)
	require.True(t, result.OK)
	for _, issue := range result.Issues {
		require.Equal(t, "duplicate_preferred_identifier", issue.Code)
		var data IdentifierCollisionData
		require.NoError(t, json.Unmarshal(issue.Data, &data))
		require.Equal(t, "preferred_preferred", data.Kind)
		require.Equal(t, "spec-0001", data.Identifier)
		require.True(t, data.Materializable)
		require.Len(t, data.Claimants, 2)
		require.NotEmpty(t, data.Keeper.NodePath)
	}
	require.Len(t, result.Fixes, 1)
	require.Equal(t, "reconcile_identifier_collision", result.Fixes[0].Kind)
	require.Equal(t, FixSafetyConfirm, result.Fixes[0].Safety)
	issueKeys := []string{result.Issues[0].Key, result.Issues[1].Key}
	require.ElementsMatch(t, issueKeys, result.Fixes[0].IssueKeys)

	// Centralized truncation owns MaxIssues; the check keeps every issue and
	// the complete repair membership.
	truncated := RunIdentifiersWithRuntime(context.Background(), RunContext{
		VaultDef: obsidian.VaultDefinition{Path: root}, VaultPath: root, NoteReader: &obsidian.Note{}, MaxIssues: 1,
	}, runtime)
	require.Equal(t, 2, truncated.IssueCount)
	require.Len(t, truncated.Issues, 2)
	require.True(t, truncated.OK)
	require.Len(t, truncated.Fixes, 1)
	require.ElementsMatch(t, issueKeys, truncated.Fixes[0].IssueKeys)
	require.NotNil(t, result.identifierRepair)
	require.NotNil(t, result.identifierRepair.Assembly)
	repairPlan, err := BuildRepairPlan(context.Background(), RunContext{
		VaultDef: obsidian.VaultDefinition{Path: root}, VaultPath: root, NoteReader: &obsidian.Note{}, MaxIssues: 20,
	}, []CheckResult{result})
	require.NoError(t, err)
	require.NotNil(t, repairPlan)
	require.NotEmpty(t, repairPlan.Operations)
	require.NotNil(t, result.identifierReconciliation)
	require.NotNil(t, result.identifierReconciliation.Plan)
	require.NotZero(t, result.identifierReconciliation.Diagnostics.Timings.Inventory)
	// The remaining four required stages are present as zero-valued durations
	// until their owning orchestration executes them.
	require.Zero(t, result.identifierReconciliation.Diagnostics.Timings.Apply)
	require.Zero(t, result.identifierReconciliation.Diagnostics.Timings.PostValidation)
}

func TestRunIdentifiersWithRuntimeIncludesPreferredAliasAndEmbeddedClaimants(t *testing.T) {
	root, runtime := identifierCheckRuntime(t, `
type Spec @node(paths: ["specs/*.md"], propertyCase: AS_DEFINED) {
  id: String! @field @identifier(preferred: true, prefix: "SPEC", separator: "-", pad: 4)
  aliases: [String!] @field
  stories: Stories @contains(level: H2, heading: "Stories")
}
type Stories implements Section {
  items: [Story!] @contains(level: H3)
}
type Story implements Section @node(locator: EMBEDDED) {
  id: ID! @field @identifier(preferred: true, derivedSuffix: "US")
  aliases: [String!] @field
}
`, map[string]string{
		"specs/a.md": "---\nid: SPEC-0001\naliases: [SPEC-0001]\n---\n## Stories\n### One\nid:: SPEC-0001-US1\naliases:: SHARED\n",
		"specs/b.md": "---\nid: SPEC-0002\naliases: [SPEC-0002, SPEC-0001]\n---\n## Stories\n### Two\nid:: SPEC-0002-US1\naliases:: SHARED\n",
	})

	result := RunIdentifiersWithRuntime(context.Background(), RunContext{
		VaultDef: obsidian.VaultDefinition{Path: root}, VaultPath: root, NoteReader: &obsidian.Note{}, MaxIssues: 20,
	}, runtime)

	var preferredAlias, embedded bool
	for _, issue := range result.Issues {
		var data IdentifierCollisionData
		require.NoError(t, json.Unmarshal(issue.Data, &data))
		switch data.Identifier {
		case "spec-0001":
			preferredAlias = data.Kind == "preferred_alias" && len(data.Claimants) == 2
		case "shared":
			if len(data.Claimants) == 2 {
				embedded = data.Claimants[0].Fragment != "" && data.Claimants[1].Fragment != ""
			}
		}
	}
	require.True(t, preferredAlias, "note preferred/alias collision must be in the complete pool inventory")
	require.True(t, embedded, "embedded alias claimants must not be dropped")
}

func TestRunIdentifiersWithRuntimeKeepsDistinctIssueMembershipForSameNoteEmbeddedClaimants(t *testing.T) {
	root, runtime := identifierCheckRuntime(t, `
type Spec @node(paths: ["specs/*.md"], propertyCase: AS_DEFINED) {
  id: String! @field @identifier(preferred: true, prefix: "SPEC", separator: "-", pad: 4)
  aliases: [String!] @field
  stories: Stories @contains(level: H2, heading: "Stories")
}
type Stories implements Section {
  items: [Story!] @contains(level: H3)
}
type Story implements Section @node(locator: EMBEDDED) {
  id: ID! @field @identifier(preferred: true, derivedSuffix: "US")
  aliases: [String!] @field
}
`, map[string]string{
		"specs/a.md": "---\nid: SPEC-0001\naliases: [SPEC-0001]\n---\n## Stories\n### One\nid:: SPEC-0001-US1\naliases:: SHARED\n### Two\nid:: SPEC-0001-US2\naliases:: SHARED\n",
	})

	result := RunIdentifiersWithRuntime(context.Background(), RunContext{
		VaultDef: obsidian.VaultDefinition{Path: root}, VaultPath: root, NoteReader: &obsidian.Note{}, MaxIssues: 20,
	}, runtime)

	var issues []Issue
	for _, issue := range result.Issues {
		if issue.Code == "duplicate_identifier_alias" && issue.Target == "shared" {
			issues = append(issues, issue)
		}
	}
	require.Len(t, issues, 2)
	require.NotEmpty(t, issues[0].Key)
	require.NotEmpty(t, issues[1].Key)
	require.NotEqual(t, issues[0].Key, issues[1].Key, "embedded claimant fragments must remain distinct repair memberships")

	var action *FixAction
	for index := range result.Fixes {
		if result.Fixes[index].Kind == "review_identifier_collision" {
			action = &result.Fixes[index]
		}
	}
	require.NotNil(t, action)
	require.Len(t, action.IssueKeys, 2)
}

func TestRunIdentifiersWithRuntimeRetainsNonMaterializableGuidance(t *testing.T) {
	root, runtime := identifierCheckRuntime(t, `
type Manual @node(paths: ["manual/*.md"], propertyCase: AS_DEFINED) {
  id: String! @field @identifier(preferred: true)
}
`, map[string]string{
		"manual/a.md": "---\nid: SHARED\naliases: [SHARED]\n---\n",
		"manual/b.md": "---\nid: SHARED\naliases: [SHARED]\n---\n",
	})

	result := RunIdentifiersWithRuntime(context.Background(), RunContext{
		VaultDef: obsidian.VaultDefinition{Path: root}, VaultPath: root, NoteReader: &obsidian.Note{}, MaxIssues: 20,
	}, runtime)

	require.Equal(t, 2, result.IssueCount)
	require.Len(t, result.Fixes, 1)
	require.Equal(t, FixSafetyAgent, result.Fixes[0].Safety)
	require.Empty(t, result.Fixes[0].Edits)
	for _, issue := range result.Issues {
		var data IdentifierCollisionData
		require.NoError(t, json.Unmarshal(issue.Data, &data))
		require.False(t, data.Materializable)
		require.Contains(t, data.Guidance, "author-supplied")
	}
}

func TestBuildRepairPlanMergesIdentifierAndOtherCheckOperations(t *testing.T) {
	root, runtime := identifierCheckRuntime(t, `
type Spec @node(paths: ["specs/*.md"], propertyCase: AS_DEFINED) {
  id: String! @field @identifier(preferred: true, prefix: "SPEC", separator: "-", pad: 4)
  aliases: [String!] @field
}
`, map[string]string{
		"specs/SPEC-0001-keeper.md": "---\nid: SPEC-0001\naliases: [SPEC-0001]\n---\n",
		"specs/SPEC-0001-loser.md":  "---\nid: SPEC-0001\naliases: [SPEC-0001]\n---\n",
		"notes/other.md":            "---\naliases: []\n---\n",
	})
	runCtx := RunContext{VaultDef: obsidian.VaultDefinition{Path: root}, VaultPath: root, NoteReader: &obsidian.Note{}, MaxIssues: 20}
	identifiers := RunIdentifiersWithRuntime(context.Background(), runCtx, runtime)
	require.NotNil(t, identifiers.identifierRepair)
	other := CheckResult{
		Name: CheckOntology, Issues: []Issue{{Code: "other_repair", Path: "notes/other.md", Target: "OTHER"}},
		Fixes: []FixAction{{
			ID: "other-repair", Check: CheckOntology, IssueCode: "other_repair", Kind: FixKindAppendAlias,
			Safety: FixSafetySafe, Title: "Add other alias", AffectedPaths: []string{"notes/other.md"},
			Edits: []FixEdit{{Kind: FixKindAppendAlias, NotePath: "notes/other.md", Value: "OTHER"}},
		}},
	}

	plan, err := BuildRepairPlan(context.Background(), runCtx, []CheckResult{identifiers, other})
	require.NoError(t, err)
	require.NotNil(t, plan)
	require.True(t, plan.RequiresLeaseHeldReplan)
	require.Len(t, plan.Actions, 2)
	require.GreaterOrEqual(t, len(plan.Operations), 2)
	var identifierOperation, otherOperation bool
	for _, operation := range plan.Operations {
		for _, actionID := range operation.ActionIDs {
			identifierOperation = identifierOperation || strings.HasPrefix(actionID, "identifier-reconcile:")
			otherOperation = otherOperation || actionID == "other-repair"
		}
	}
	require.True(t, identifierOperation)
	require.True(t, otherOperation)
}

func TestBuildRepairPlanFoldsLoserAliasMirrorIntoIdentifierCollision(t *testing.T) {
	root, runtime := identifierCheckRuntime(t, `
type Spec @node(paths: ["specs/*.md"], propertyCase: AS_DEFINED) {
  id: String! @field @identifier(preferred: true, prefix: "SPEC", separator: "-", pad: 4)
  aliases: [String!] @field
}
`, map[string]string{
		"specs/000-keeper.md":      "---\nid: SPEC-0001\naliases: [SPEC-0001]\n---\n",
		"specs/SPEC-0001-loser.md": "---\nid: SPEC-0001\naliases: []\n---\n",
	})
	runCtx := RunContext{VaultDef: obsidian.VaultDefinition{Path: root}, VaultPath: root, NoteReader: &obsidian.Note{}, MaxIssues: 20}

	result := RunIdentifiersWithRuntime(context.Background(), runCtx, runtime)
	plan, err := BuildRepairPlan(context.Background(), runCtx, []CheckResult{result})
	require.NoError(t, err)
	require.NotNil(t, plan)
	require.Len(t, plan.Actions, 1, "the collision action must subsume the loser's stale alias-mirror action")
	require.Equal(t, "reconcile_identifier_collision", plan.Actions[0].Kind)
	require.Len(t, plan.Actions[0].IssueKeys, 3, "two collision claimant issues plus the alias-mirror issue must retain membership")
	require.Len(t, plan.Transactions, 1)
	require.Empty(t, plan.Transactions[0].Conflicts)

	var loserWrite, loserRename *RepairOperation
	for index := range plan.Operations {
		operation := &plan.Operations[index]
		require.Equal(t, "specs/SPEC-0001-loser.md", operation.Path, "the keeper must not be rewritten")
		switch operation.Kind {
		case RepairOperationWrite:
			loserWrite = operation
		case RepairOperationRename:
			loserRename = operation
		}
	}
	require.Len(t, plan.Operations, 2)
	require.NotNil(t, loserWrite)
	require.NotNil(t, loserRename)
	require.Equal(t, "specs/SPEC-0002-loser.md", loserRename.DestinationPath)
	// The loser's single write carries both the rekey and the new alias
	// mirror; the stale loser identifier is not kept as an alias.
	const loserBefore = "---\nid: SPEC-0001\naliases: []\n---\n"
	const loserAfter = "---\nid: SPEC-0002\naliases: [SPEC-0002]\n---\n"
	require.Equal(t, []ExpectedText{{StartByte: 0, EndByte: len(loserBefore), Text: loserBefore, Replacement: loserAfter}}, loserWrite.Expected)
	require.Equal(t, loserAfter, string(loserWrite.Content))
}

func TestRunIdentifiersWithRuntimeDoesNotMergeUnrelatedManualFamilies(t *testing.T) {
	root, runtime := identifierCheckRuntime(t, `
type ManualA @node(paths: ["a/*.md"]) { id: String! @field @identifier(preferred: true) }
type ManualB @node(paths: ["b/*.md"]) { id: String! @field @identifier(preferred: true) }
`, map[string]string{
		"a/one.md": "---\nid: SHARED\naliases: [SHARED]\n---\n",
		"b/two.md": "---\nid: SHARED\naliases: [SHARED]\n---\n",
	})

	result := RunIdentifiersWithRuntime(context.Background(), RunContext{
		VaultDef: obsidian.VaultDefinition{Path: root}, VaultPath: root, NoteReader: &obsidian.Note{}, MaxIssues: 20,
	}, runtime)
	require.Zero(t, result.IssueCount)
}

func TestRunIdentifiersWithRuntimeMoveConflictDowngradesToGuidedReview(t *testing.T) {
	root, runtime := identifierCheckRuntime(t, `
type Spec @node(paths: ["specs/*.md"], propertyCase: AS_DEFINED) {
  id: String! @field @identifier(preferred: true, prefix: "SPEC", separator: "-", pad: 4)
  aliases: [String!] @field
}
`, map[string]string{
		"specs/000-keeper.md":      "---\nid: SPEC-0001\naliases: [SPEC-0001]\n---\n",
		"specs/SPEC-0001-loser.md": "---\nid: SPEC-0001\naliases: [SPEC-0001]\n---\n",
		"specs/SPEC-0002-loser.md": "---\ntitle: Existing destination\n---\n",
	})

	result := RunIdentifiersWithRuntime(context.Background(), RunContext{
		VaultDef: obsidian.VaultDefinition{Path: root}, VaultPath: root, NoteReader: &obsidian.Note{}, MaxIssues: 20,
	}, runtime)
	var collision *FixAction
	for index := range result.Fixes {
		if result.Fixes[index].Kind == "reconcile_identifier_collision" {
			collision = &result.Fixes[index]
		}
	}
	require.NotNil(t, collision)
	require.Equal(t, FixSafetyAgent, collision.Safety)
	require.Contains(t, collision.Summary, "cannot be materialized deterministically")
	require.NotNil(t, result.identifierRepair, "blocked components must retain their sealed diagnostic evidence")
}

func TestRunIdentifiersWithRuntimeKeepsIndependentRepairWhenAnotherCollisionIsBlocked(t *testing.T) {
	root, runtime := identifierCheckRuntime(t, `
type Spec @node(paths: ["specs/*.md"], propertyCase: AS_DEFINED) {
  id: String! @field @identifier(preferred: true, prefix: "SPEC", separator: "-", pad: 4)
  aliases: [String!] @field
}
`, map[string]string{
		"specs/000-a-keeper.md":    "---\nid: SPEC-0001\naliases: [SPEC-0001]\n---\n",
		"specs/SPEC-0001-loser.md": "---\nid: SPEC-0001\naliases: [SPEC-0001]\n---\n",
		"specs/002-b-keeper.md":    "---\nid: SPEC-0003\naliases: [SPEC-0003]\n---\n",
		"specs/SPEC-0003-loser.md": "---\nid: SPEC-0003\naliases: [SPEC-0003]\n---\n",
		"specs/SPEC-0004-loser.md": "---\ntitle: Existing destination\n---\n",
	})
	runCtx := RunContext{VaultDef: obsidian.VaultDefinition{Path: root}, VaultPath: root, NoteReader: &obsidian.Note{}, MaxIssues: 20}

	result := RunIdentifiersWithRuntime(context.Background(), runCtx, runtime)

	require.NotNil(t, result.identifierRepair)
	var agentActions, confirmationActions int
	for _, action := range result.Fixes {
		if action.Kind != "reconcile_identifier_collision" {
			continue
		}
		if action.Safety == FixSafetyAgent {
			agentActions++
		}
		if action.Safety == FixSafetyConfirm {
			confirmationActions++
		}
	}
	require.Equal(t, 1, agentActions)
	require.Equal(t, 1, confirmationActions)
	plan, err := BuildRepairPlan(context.Background(), runCtx, []CheckResult{result})
	require.NoError(t, err)
	require.NotNil(t, plan)
	var executable bool
	for _, operation := range plan.Operations {
		if len(operation.PlanningConflicts) == 0 {
			executable = true
		}
	}
	require.True(t, executable, "one blocked collision must not suppress an independent executable component")
}

func TestRunIdentifiersWithRuntimeManualReviewActionsHaveCollisionSpecificIDs(t *testing.T) {
	root, runtime := identifierCheckRuntime(t, `
type Manual @node(paths: ["manual/*.md"], propertyCase: AS_DEFINED) {
  id: String! @field @identifier(preferred: true)
  aliases: [String!] @field
}
`, map[string]string{
		"manual/a.md": "---\nid: A\naliases: [SHARED, OTHER]\n---\n",
		"manual/b.md": "---\nid: B\naliases: [SHARED, OTHER]\n---\n",
	})

	result := RunIdentifiersWithRuntime(context.Background(), RunContext{
		VaultDef: obsidian.VaultDefinition{Path: root}, VaultPath: root, NoteReader: &obsidian.Note{}, MaxIssues: 20,
	}, runtime)

	var reviewIDs []string
	for _, action := range result.Fixes {
		if action.Kind == "review_identifier_collision" {
			reviewIDs = append(reviewIDs, action.ID)
		}
	}
	require.Len(t, reviewIDs, 2)
	require.NotEqual(t, reviewIDs[0], reviewIDs[1])
}

func TestRunSuiteOncePreparedExposesIdentifierReconciliationAndRepairPlan(t *testing.T) {
	root, runtime := identifierCheckRuntime(t, `
type Spec @node(paths: ["specs/*.md"], propertyCase: AS_DEFINED) {
  id: String! @field @identifier(preferred: true, prefix: "SPEC", separator: "-", pad: 4)
  aliases: [String!] @field
}
`, map[string]string{
		"specs/a.md": "---\nid: SPEC-0001\naliases: [SPEC-0001]\n---\n",
		"specs/b.md": "---\nid: SPEC-0001\naliases: [SPEC-0001]\n---\n",
	})
	runCtx := &RunContext{VaultDef: obsidian.VaultDefinition{Path: root}, VaultPath: root, NoteReader: &obsidian.Note{}, NoteMetadata: testNoteMetadata(t), MaxIssues: 20}

	result, _, err := RunSuiteOncePrepared(context.Background(), Options{Checks: []string{CheckIdentifiers}, RunContext: runCtx}, runtime, nil)
	require.NoError(t, err)
	require.NotNil(t, result.IdentifierReconciliation)
	require.NotNil(t, result.IdentifierReconciliation.Plan)
	require.NotNil(t, result.FixPlan)
	require.NotEmpty(t, result.FixPlan.Operations)
}

func identifierCheckRuntime(t *testing.T, schema string, files map[string]string) (string, *ontology.Runtime) {
	t.Helper()
	root := t.TempDir()
	allFiles := map[string]string{".rhizome/ontology/schema.graphql": schema}
	for path, content := range files {
		allFiles[path] = content
	}
	require.NoError(t, writeFixtureFiles(root, allFiles))
	store := openTestStore(t, root)
	vaultDef := obsidian.VaultDefinition{Path: root, Links: obsidian.LinkTypeBoth}
	_, err := testNoteMetadata(t).EnsureIndexed(context.Background(), vaultDef, &obsidian.Note{}, store)
	require.NoError(t, err)
	runtime, err := ontology.EnsureFreshRuntimeWithStore(context.Background(), testNoteMetadata(t), vaultDef, &obsidian.Note{}, store)
	require.NoError(t, err)
	return root, runtime
}

func commitIdentifierCheckFixture(t *testing.T, root string) {
	t.Helper()
	for _, args := range [][]string{
		{"init", "-q"},
		{"add", "."},
		{"-c", "user.name=Rhizome Tests", "-c", "user.email=tests@rhizome.invalid", "commit", "-q", "-m", "add identifier fixtures"},
	} {
		cmd := exec.Command("git", append([]string{"-C", root}, args...)...)
		output, err := cmd.CombinedOutput()
		require.NoError(t, err, "%s", output)
	}
}
