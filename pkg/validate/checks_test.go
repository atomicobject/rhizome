package validate

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"
)

func TestRunCheckUsesOptionsMaxIssuesWhenRunContextLimitIsUnset(t *testing.T) {
	root := t.TempDir()
	for i := range 125 {
		writeValidationTestFile(t, filepath.Join(root, ".rhizome", "views", fmt.Sprintf("bad-%03d.yaml", i)), "apiVersion: [")
	}

	result := RunCheck(context.Background(), RunContext{
		VaultDef:   obsidian.VaultDefinition{Path: root},
		VaultPath:  root,
		NoteReader: &obsidian.Note{},
	}, Options{MaxIssues: 100}, CheckViews)

	require.False(t, result.OK)
	require.Equal(t, 125, result.IssueCount)
	require.Len(t, result.Issues, 100)
	require.Len(t, result.fullIssues, 125, "truncation keeps complete evidence")
	require.Len(t, result.allIssueKeys, 125)
}

func TestRunSuiteOnceRejectsMissingNoteMetadataBeforeProjectionWork(t *testing.T) {
	root := t.TempDir()
	_, _, err := RunSuiteOnce(context.Background(), Options{
		Checks: []string{CheckOntology},
		RunContext: &RunContext{
			VaultDef:   obsidian.VaultDefinition{Path: root},
			VaultPath:  root,
			NoteReader: &obsidian.Note{},
		},
	})
	require.ErrorContains(t, err, "RunContext.NoteMetadata")
	require.NoDirExists(t, filepath.Join(root, ".rhizome"))
}

func TestMergePostApplyRemainingEvidencePreservesExecutionAndPostcheckIssues(t *testing.T) {
	execution := &FixExecution{
		RemainingFindings:  1,
		RemainingIssueKeys: []string{"issue:not-attempted"},
	}
	mergePostApplyRemainingEvidence(execution, Result{
		IssueCount: 1,
		Checks: []CheckResult{{
			Name:   CheckIdentifiers,
			Issues: []Issue{{Key: "issue:postcheck"}},
		}},
	})
	require.Equal(t, []string{"issue:not-attempted", "issue:postcheck"}, execution.RemainingIssueKeys)
	require.Equal(t, 2, execution.RemainingFindings)

	mergePostApplyRemainingEvidence(execution, Result{})
	require.Equal(t, []string{"issue:not-attempted", "issue:postcheck"}, execution.RemainingIssueKeys)
	require.Equal(t, 2, execution.RemainingFindings, "a clean postcheck must not erase skipped or not-attempted evidence")
}

func TestRunSuiteOncePreparedUsesProvidedRuntimeFailure(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	runCtx := &RunContext{
		VaultDef:     obsidian.VaultDefinition{Name: "prepared", Path: root},
		VaultPath:    root,
		NoteReader:   &obsidian.Note{},
		NoteMetadata: testNoteMetadata(t),
		MaxIssues:    20,
	}
	preparedErr := errors.New("prepared validation projection unavailable")

	result, _, err := RunSuiteOncePrepared(context.Background(), Options{
		Checks:     []string{CheckOntology},
		RunContext: runCtx,
	}, nil, preparedErr)

	require.NoError(t, err)
	require.Len(t, result.Checks, 1)
	require.ErrorContains(t, errors.New(result.Checks[0].Error), preparedErr.Error())
}

func TestRunSuiteOncePreparedNeverAcquiresReplacementRuntime(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	runCtx := &RunContext{
		VaultDef:     obsidian.VaultDefinition{Name: "prepared", Path: root},
		VaultPath:    root,
		NoteReader:   &obsidian.Note{},
		NoteMetadata: testNoteMetadata(t),
		MaxIssues:    20,
	}

	result, _, err := RunSuiteOncePrepared(context.Background(), Options{
		Checks:     []string{CheckOntology},
		RunContext: runCtx,
	}, nil, nil)

	require.NoError(t, err)
	require.Len(t, result.Checks, 1)
	require.False(t, result.Checks[0].Skipped)
	require.ErrorContains(t, errors.New(result.Checks[0].Error), "prepared ontology runtime is unavailable")
	require.NoDirExists(t, filepath.Join(root, ".rhizome"))
}

func TestRunSuiteOncePreparedNeverOpensReplacementCodeStore(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	result, _, err := RunSuiteOncePrepared(context.Background(), Options{
		Checks: []string{CheckCodeAnchors},
		RunContext: &RunContext{
			VaultDef:     obsidian.VaultDefinition{Name: "prepared", Path: root},
			VaultPath:    root,
			NoteReader:   &obsidian.Note{},
			NoteMetadata: testNoteMetadata(t),
			MaxIssues:    20,
		},
	}, nil, nil)

	require.NoError(t, err)
	require.Len(t, result.Checks, 1)
	require.False(t, result.Checks[0].Skipped)
	require.ErrorContains(t, errors.New(result.Checks[0].Error), "prepared code index store is unavailable")
	require.NoDirExists(t, filepath.Join(root, ".rhizome"))
}

func TestPreparedCodeStoreRequirementUsesProjectionMetadata(t *testing.T) {
	t.Parallel()

	require.True(t, preparedCodeStoreRequired(registeredCheck{
		CheckDescriptor: CheckDescriptor{
			Name:              "future-code-check",
			ProjectionDomains: []ProjectionDomain{ProjectionValidation, ProjectionCode},
		},
		RuntimeRequirement: runtimeRequirementNone,
	}))
	require.False(t, preparedCodeStoreRequired(registeredCheck{
		CheckDescriptor: CheckDescriptor{
			Name:              CheckCodeAnchors,
			ProjectionDomains: []ProjectionDomain{ProjectionValidation},
		},
		RuntimeRequirement: runtimeRequirementNone,
	}))
}

func TestCanonicalCheckAcceptsQueryRecipeAliases(t *testing.T) {
	for _, raw := range []string{"query-recipes", "query_recipes", "recipes"} {
		check, ok := CanonicalCheck(raw)
		require.True(t, ok, raw)
		require.Equal(t, CheckQueryRecipes, check)
	}
}

func TestCanonicalCheckAcceptsViewAliases(t *testing.T) {
	for _, raw := range []string{"views", "view", "configured-views", "configured_views", "view-config", "view_config"} {
		check, ok := CanonicalCheck(raw)
		require.True(t, ok, raw)
		require.Equal(t, CheckViews, check)
	}
}

func TestCanonicalCheckAcceptsSkillOverlayAliases(t *testing.T) {
	for _, raw := range []string{"skill-overlays", "skill_overlays", "skilloverlay", "overlays"} {
		check, ok := CanonicalCheck(raw)
		require.True(t, ok, raw)
		require.Equal(t, CheckSkillOverlays, check)
	}
}

func TestCanonicalCheckAcceptsCompanionDocsAliases(t *testing.T) {
	for _, raw := range []string{"companion-docs", "companion_docs", "companiondocs"} {
		check, ok := CanonicalCheck(raw)
		require.True(t, ok, raw)
		require.Equal(t, CheckCompanionDocs, check)
	}
}

func TestBuildNextActionsClassifiesFixTiers(t *testing.T) {
	issues := []Issue{
		{Key: "issue:safe", Code: IssueCodeBrokenNoteLink, Path: "safe.md"},
		{Key: "issue:confirm", Code: "declared_type_mismatch", Path: "confirm.md"},
		{Key: "issue:agent", Code: "field_type_mismatch", Path: "agent.md"},
		{Key: "issue:registry-gap", Code: "new_unregistered_issue", Path: "unknown.md"},
	}
	actions := BuildNextActions(Result{
		IssueCount:     4,
		VaultName:      "work",
		SelectedChecks: []string{CheckBrokenLinks, CheckOntology},
		Checks: []CheckResult{
			{Name: CheckBrokenLinks, IssueCount: 1, Issues: issues[:1], fullIssues: issues[:1]},
			{Name: CheckOntology, IssueCount: 3, Issues: issues[1:], fullIssues: issues[1:]},
		},
		FixPlan: &FixPlan{
			SafeCount:         1,
			ConfirmationCount: 2,
			AgentCount:        1,
			Actions: []FixAction{
				{Safety: FixSafetySafe, InstanceCount: 1, IssueKeys: []string{"issue:safe"}},
				{Safety: FixSafetyConfirm, InstanceCount: 1, IssueKeys: []string{"issue:confirm"}},
				{Safety: FixSafetyAgent, InstanceCount: 1, IssueKeys: []string{"issue:agent"}},
			},
		},
	})

	require.NotNil(t, actions)
	require.Equal(t, "rzm agent validate fix broken-links --apply --vault work && rzm agent validate fix ontology --apply --vault work", actions.SafeAutoFixCommand)
	require.Equal(t, 1, actions.SafeFixCount)
	require.Equal(t, 2, actions.NeedsConfirmationCount)
	require.Equal(t, 1, actions.AgentRequiredCount)
	require.Equal(t, 1, actions.UnclassifiedIssueCount)
	require.True(t, actions.ClassificationRequired)
	require.Len(t, actions.Actions, 4)
	require.Equal(t, "safe_auto_fix", actions.Actions[0].Category)
	require.Equal(t, "Apply 1 safe deterministic fix(es) with the positional fix command, then re-run validation without --apply.", actions.Actions[0].Message)
	require.NotContains(t, actions.Actions[0].Message, "--fix")
	require.NotContains(t, actions.Actions[0].Message, "--non-interactive")
	require.Equal(t, "needs_confirmation", actions.Actions[1].Category)
	require.Equal(t, "agent_required", actions.Actions[2].Category)
	require.Equal(t, "registry_gap", actions.Actions[3].Category)
	require.Equal(t, 1, actions.Actions[3].Count)
	require.Equal(t, []string{"issue:registry-gap"}, actions.Actions[3].IssueKeys)
	require.Contains(t, actions.Actions[3].NonFixableReason, "new_unregistered_issue")
}

func TestBuildNextActionsOmitsClassificationWhenFixPlanCoversIssues(t *testing.T) {
	issue := Issue{Key: "issue:identifier", Code: "identifier_not_in_aliases", Path: "note.md"}
	actions := BuildNextActions(Result{
		IssueCount:     1,
		SelectedChecks: []string{CheckAliases},
		Checks: []CheckResult{{
			Name: CheckAliases, IssueCount: 1, Issues: []Issue{issue}, fullIssues: []Issue{issue},
		}},
		FixPlan: &FixPlan{
			SafeCount: 1,
			Actions: []FixAction{
				{Safety: FixSafetySafe, InstanceCount: 1, IssueKeys: []string{"issue:identifier"}},
			},
		},
	})

	require.NotNil(t, actions)
	require.Equal(t, "rzm agent validate fix identifiers --apply", actions.SafeAutoFixCommand)
	require.Zero(t, actions.UnclassifiedIssueCount)
	require.False(t, actions.ClassificationRequired)
	require.Len(t, actions.Actions, 1)
	require.Equal(t, "safe_auto_fix", actions.Actions[0].Category)
}

func TestBuildNextActionsQuotesVaultNameInSafeCommand(t *testing.T) {
	actions := BuildNextActions(Result{
		IssueCount:     1,
		VaultName:      "team vault's notes",
		SelectedChecks: []string{CheckAliases},
		FixPlan: &FixPlan{
			SafeCount: 1,
			Actions: []FixAction{
				{Safety: FixSafetySafe, InstanceCount: 1},
			},
		},
	})

	require.NotNil(t, actions)
	require.Equal(t, "rzm agent validate fix identifiers --apply --vault 'team vault'\"'\"'s notes'", actions.SafeAutoFixCommand)
}

func TestBuildNextActionsPrefersCallerSuppliedExactApplyCommand(t *testing.T) {
	actions := BuildNextActions(Result{
		IssueCount:     1,
		VaultName:      "fallback-vault",
		SelectedChecks: []string{CheckBrokenLinks, CheckOntology},
		ApplyCommand:   "rzm validate fix all --apply --vault docs --scope-note 'Notes/A B.md'",
		FixPlan: &FixPlan{
			SafeCount: 1,
			Actions:   []FixAction{{Safety: FixSafetySafe, InstanceCount: 1}},
		},
	})

	require.NotNil(t, actions)
	require.Equal(t, "rzm validate fix all --apply --vault docs --scope-note 'Notes/A B.md'", actions.SafeAutoFixCommand)
	require.NotContains(t, actions.SafeAutoFixCommand, " && ")
}

func TestBuildNextActionsCleanResultIsEmpty(t *testing.T) {
	require.Empty(t, BuildNextActions(Result{OK: true}))
}

func TestRunSkillOverlaysValidatesBundledTemplates(t *testing.T) {
	result := RunSkillOverlays(RunContext{MaxIssues: 20})
	require.True(t, result.OK)
	require.Contains(t, result.Summary, "overlay fragment")
}

func TestRunQueryRecipesSkipsVaultWithoutRecipesOrOntology(t *testing.T) {
	root := t.TempDir()
	result := RunQueryRecipes(context.Background(), RunContext{
		VaultDef:   obsidian.VaultDefinition{Path: root},
		VaultPath:  root,
		NoteReader: &obsidian.Note{},
		MaxIssues:  20,
	})
	require.True(t, result.OK)
	require.True(t, result.Skipped)
	require.Contains(t, result.Summary, "no query recipes")
}

func TestRunViewsSkipsVaultWithoutViews(t *testing.T) {
	root := t.TempDir()
	result := RunViews(context.Background(), RunContext{
		VaultDef:   obsidian.VaultDefinition{Path: root},
		VaultPath:  root,
		NoteReader: &obsidian.Note{},
		MaxIssues:  20,
	})
	require.True(t, result.OK)
	require.True(t, result.Skipped)
	require.Contains(t, result.Summary, "no configured views")
}

func TestRunViewsReportsLoadIssuesWithoutOntology(t *testing.T) {
	root := t.TempDir()
	writeValidationTestFile(t, root+"/.rhizome/views/bad.yaml", "apiVersion: [")

	result := RunViews(context.Background(), RunContext{
		VaultDef:   obsidian.VaultDefinition{Path: root},
		VaultPath:  root,
		NoteReader: &obsidian.Note{},
		MaxIssues:  20,
	})

	require.False(t, result.OK)
	require.Equal(t, 1, result.IssueCount)
	require.Equal(t, "view_yaml_decode_error", result.Issues[0].Code)
}

func TestRunSuiteOnceMarksIssueCheckResultsNotOK(t *testing.T) {
	root := t.TempDir()
	writeValidationTestFile(t, root+"/.rhizome/ontology/schema.graphql", `type ProductSpec @node(paths: ["docs/specs/product/*.md"]) {
  summary: String!
}
`)
	writeValidationTestFile(t, root+"/docs/specs/product/missing.md", `---
type: ProductSpec
---
# Missing
`)

	runCtx := &RunContext{
		VaultDef:     obsidian.VaultDefinition{Path: root},
		VaultPath:    root,
		NoteReader:   &obsidian.Note{},
		NoteMetadata: testNoteMetadata(t),
		MaxIssues:    20,
	}
	result, _, err := RunSuiteOnce(context.Background(), Options{
		Checks:     []string{CheckOntology},
		MaxIssues:  20,
		RunContext: runCtx,
	})

	require.NoError(t, err)
	require.Greater(t, result.IssueCount, 0)
	require.False(t, result.OK)
	require.Len(t, result.Checks, 1)
	require.Equal(t, CheckOntology, result.Checks[0].Name)
	require.Greater(t, result.Checks[0].IssueCount, 0)
	require.False(t, result.Checks[0].OK)
}

func TestRunViewsReportsUnknownSchemaTargets(t *testing.T) {
	root := t.TempDir()
	writeValidationTestFile(t, root+"/.rhizome/ontology/schema.graphql", `interface SpecLike {
  summary: String!
}

type ProductSpec implements SpecLike @node(paths: ["docs/specs/product/*.md"]) {
  summary: String!
}
`)
	writeValidationTestFile(t, root+"/.rhizome/views/missing.yaml", `apiVersion: rhizome.view.v1
id: missing-type
name: Missing type
source:
  kind: ontology_type
  type: MissingType
mount:
  kind: type
  type: MissingType
variants:
  table:
    columns:
      - field: title
`)

	result := RunViews(context.Background(), RunContext{
		VaultDef:   obsidian.VaultDefinition{Path: root},
		VaultPath:  root,
		NoteReader: &obsidian.Note{},
		MaxIssues:  20,
	})

	require.False(t, result.OK)
	requireIssue(t, result.Issues, "unknown_ontology_type")
	requireIssue(t, result.Issues, "invalid_mount_target")
	for _, issue := range result.Issues {
		if issue.Code == "unknown_ontology_type" {
			require.Equal(t, &IssueVariant{Key: "MissingType", Label: "MissingType"}, issue.Variant)
			continue
		}
		require.Nil(t, issue.Variant, "per-instance %s stays variant-free", issue.Code)
	}
}

func TestRunViewsValidatesRepositoryViews(t *testing.T) {
	root := findValidationRepoRoot(t)

	result := RunViews(context.Background(), RunContext{
		VaultDef:   obsidian.VaultDefinition{Name: "rhizome", Path: root, Links: obsidian.LinkTypeBoth},
		VaultPath:  root,
		NoteReader: &obsidian.Note{},
		MaxIssues:  40,
	})

	require.True(t, result.OK, "repository views should validate: %#v", result.Issues)
	require.False(t, result.Skipped)
	require.Contains(t, result.Summary, "views checked")
}

func TestRunViewsIgnoresUnrelatedQueryRecipeLoadIssues(t *testing.T) {
	root := t.TempDir()
	writeValidationTestFile(t, root+"/.rhizome/ontology/schema.graphql", `type ProductSpec @node(paths: ["docs/specs/product/*.md"]) {
  summary: String!
}
`)
	writeValidationTestFile(t, root+"/.rhizome/views/specs.yaml", `apiVersion: rhizome.view.v1
id: specs
name: Specs
source:
  kind: ontology_type
  type: ProductSpec
mount:
  kind: standalone
variants:
  table:
    columns:
      - field: title
`)
	writeValidationTestFile(t, root+"/.rhizome/query-recipes/bad.yaml", "id: [")

	result := RunViews(context.Background(), RunContext{
		VaultDef:   obsidian.VaultDefinition{Path: root},
		VaultPath:  root,
		NoteReader: &obsidian.Note{},
		MaxIssues:  20,
	})

	require.True(t, result.OK)
	require.Empty(t, result.Issues)
}

func TestRunViewsReportsGeneratedIDCollision(t *testing.T) {
	root := t.TempDir()
	writeValidationTestFile(t, root+"/.rhizome/ontology/schema.graphql", `type ProductSpec @node(paths: ["docs/specs/product/*.md"]) {
  summary: String!
}
`)
	writeValidationTestFile(t, root+"/.rhizome/views/collision.yaml", `apiVersion: rhizome.view.v1
id: generated.type.ProductSpec.table
name: Collision
source:
  kind: ontology_type
  type: ProductSpec
mount:
  kind: standalone
variants:
  table:
    columns:
      - field: title
`)

	result := RunViews(context.Background(), RunContext{
		VaultDef:   obsidian.VaultDefinition{Path: root},
		VaultPath:  root,
		NoteReader: &obsidian.Note{},
		MaxIssues:  20,
	})

	require.False(t, result.OK)
	requireIssue(t, result.Issues, "duplicate_generated_view_id")
}

func writeValidationTestFile(t *testing.T, path string, body string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, []byte(body), 0o644))
}

func findValidationRepoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	require.NoError(t, err)
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("could not find repository root")
		}
		dir = parent
	}
}

func requireIssue(t *testing.T, issues []Issue, code string) {
	t.Helper()
	for _, issue := range issues {
		if issue.Code == code {
			return
		}
	}
	t.Fatalf("issue code %q not found in %#v", code, issues)
}
