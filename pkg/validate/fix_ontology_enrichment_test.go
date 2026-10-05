package validate

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"
)

func TestAttachOntologySuggestionsJoinsOnlyByExactStableIssueKey(t *testing.T) {
	left := ontology.ValidationIssue{
		Code:         "inverse_mismatch",
		NotePath:     "specs/auth.md",
		TypeName:     "Spec",
		FieldName:    "implements",
		NodeRef:      "specs/auth.md#^story-1",
		NodeID:       "story-1",
		Structural:   "structure:left",
		FixTarget:    "features/login.md",
		FixFieldName: "specs",
	}
	right := left
	right.NodeRef = "specs/auth.md#^story-2"
	right.NodeID = "story-2"
	right.Structural = "structure:right"
	right.FixTarget = "features/logout.md"
	check := CheckResult{Name: CheckOntology, Issues: []Issue{
		ontologyValidationIssue(left), ontologyValidationIssue(right),
	}}
	keyed, err := attachStableRepairIssueKeys([]CheckResult{check})
	require.NoError(t, err)
	require.Len(t, keyed[0].fullIssues, 2)
	require.NotEqual(t, keyed[0].fullIssues[0].Key, keyed[0].fullIssues[1].Key)

	err = attachOntologySuggestionActions(&keyed[0], []ontology.FixSuggestion{{
		IssueCode: left.Code,
		Issue:     left,
		NotePath:  left.NotePath,
		Ops: []ontology.FixOp{{
			Kind: "setLinkField", Path: left.FixTarget, Field: left.FixFieldName,
			Values: []string{"[[specs/auth.md]]"},
		}},
	}})
	require.NoError(t, err)
	require.Len(t, keyed[0].Fixes, 1)
	require.Equal(t, []string{keyed[0].fullIssues[0].Key}, keyed[0].Fixes[0].IssueKeys)
}

// runPreparedOntologySuite indexes the fixture, then runs the ontology check
// through the product suite path: stable keys, enrichment and repair plan.
func runPreparedOntologySuite(t *testing.T, files map[string]string) (string, *ontology.Runtime, Result) {
	t.Helper()
	root := t.TempDir()
	require.NoError(t, writeFixtureFiles(root, files))
	store := openTestStore(t, root)
	vaultDef := obsidian.VaultDefinition{Path: root, Links: obsidian.LinkTypeBoth}
	runtime, err := ontology.EnsureFreshRuntimeWithStore(context.Background(), testNoteMetadata(t), vaultDef, &obsidian.Note{}, store)
	require.NoError(t, err)
	runCtx := &RunContext{VaultDef: vaultDef, VaultPath: root, NoteReader: &obsidian.Note{}, NoteMetadata: testNoteMetadata(t), MaxIssues: 100}
	result, _, err := RunSuiteOncePrepared(context.Background(), Options{
		Checks: []string{CheckOntology}, RunContext: runCtx,
	}, runtime, nil)
	require.NoError(t, err)
	return root, runtime, result
}

func repairActionByCode(t *testing.T, plan *RepairPlan, code string) (FixAction, RepairOperation) {
	t.Helper()
	require.NotNil(t, plan)
	for _, action := range plan.Actions {
		if action.IssueCode != code {
			continue
		}
		require.Len(t, action.OperationIDs, 1)
		for _, operation := range plan.Operations {
			if operation.ID == action.OperationIDs[0] {
				return action, operation
			}
		}
		t.Fatalf("operation %s for %s not found", action.OperationIDs[0], code)
	}
	t.Fatalf("no %s action in %+v", code, plan.Actions)
	return FixAction{}, RepairOperation{}
}

func TestRunSuiteOncePreparedInitializesMissingRequiredList(t *testing.T) {
	_, _, result := runPreparedOntologySuite(t, map[string]string{
		".rhizome/ontology/schema.graphql": `
type Spec @node(paths: ["specs/*.md"], label: "Spec") {
  tags: [String!]!
}
`,
		"specs/one.md": "---\ntype: Spec\n---\n# One\n",
	})

	action, operation := repairActionByCode(t, result.FixPlan, "missing_required_field")
	require.Equal(t, FixKindSetFrontmatter, action.Kind)
	require.Equal(t, FixSafetySafe, action.Safety)
	require.Len(t, action.Edits, 1)
	require.Equal(t, "tags", action.Edits[0].Property)
	require.Empty(t, action.Edits[0].Values)
	require.Equal(t, "specs/one.md", operation.Path)
	require.Contains(t, string(operation.Content), "tags: []")
}

func TestRunSuiteOncePreparedBuildsSchemaAwareNestedSectionOperation(t *testing.T) {
	_, _, result := runPreparedOntologySuite(t, map[string]string{
		".rhizome/ontology/schema.graphql": `
type Leaf implements Section {}

type Requirements implements Section {
  context: Leaf @contains(level: H3, heading: "Context")
  details: Leaf @contains(level: H3, heading: "Details", required: true)
  evidence: Leaf @contains(level: H3, heading: "Evidence")
}

type Spec @node(paths: ["specs/*.md"]) {
  requirements: Requirements @contains(level: H2, heading: "Requirements", required: true)
  status: Leaf @contains(level: H2, heading: "Status")
}
`,
		"specs/one.md": `---
type: Spec
---

# One

## Requirements

### Context

Background.

### Evidence

Proof.

## Status

Draft.
`,
	})

	require.Len(t, result.FixPlan.Operations, 1)
	action, operation := repairActionByCode(t, result.FixPlan, "missing_required_section")
	require.Equal(t, FixKindOntologyAddSection, action.Kind)
	require.Contains(t, action.Title, "requirements.details")
	updated := string(operation.Content)
	require.Contains(t, updated, "### Details")
	require.Less(t, indexRequired(t, updated, "### Context"), indexRequired(t, updated, "### Details"))
	require.Less(t, indexRequired(t, updated, "### Details"), indexRequired(t, updated, "### Evidence"))
	require.Less(t, indexRequired(t, updated, "### Evidence"), indexRequired(t, updated, "## Status"))
}

// SPEC-0023 usage-driven block IDs: default validation must not diagnose an
// embedded node without a block ID, nor emit an ensure_block_id action, nor
// touch its source. Insertion belongs to EnsureLinkTargetApply on request.
// Coderefs: [[linkable-embedded-node-identifiers#^spec-0023-us2]]
func TestRunSuiteOncePreparedDoesNotEagerlyEnsureEmbeddedBlockIDs(t *testing.T) {
	const spec = `---
type: Spec
summary: One
---

# One

## Stories

### Story
id:: SPEC-1.US1
`
	root, runtime, result := runPreparedOntologySuite(t, map[string]string{
		".rhizome/ontology/schema.graphql": `
type UserStory implements Section @node(locator: EMBEDDED) {
  id: ID! @field
}

type Stories implements Section {
  stories: [UserStory!] @contains(level: H3)
}

type Spec @node(paths: ["specs/*.md"]) {
  summary: String!
  stories: Stories @contains(level: H2, heading: "Stories")
}
`,
		"specs/one.md": spec,
	})

	stories, err := runtime.Store.OntologyNodesByType(context.Background(), "UserStory")
	require.NoError(t, err)
	require.Len(t, stories, 1, "the fixture must establish the embedded node the policy is about")
	require.Len(t, result.Checks, 1)
	require.Empty(t, result.Checks[0].Error)
	for _, issue := range result.Checks[0].Issues {
		require.NotEqual(t, "missing_embedded_block_id", issue.Code)
	}
	if result.FixPlan != nil {
		for _, action := range result.FixPlan.Actions {
			require.NotEqual(t, FixKindEnsureBlockID, action.Kind)
		}
	}
	updated, err := os.ReadFile(filepath.Join(root, "specs/one.md"))
	require.NoError(t, err)
	require.Equal(t, spec, string(updated))
}

func TestRunSuiteOncePreparedWiresInverseSuggestionThroughOntologyPreview(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, writeFixtureFiles(root, map[string]string{
		".rhizome/ontology/schema.graphql": `
type Team @node(paths: ["teams/*.md"]) {
  name: String!
  members: [Person!] @link(inverse: "team")
}

type Person @node(paths: ["people/*.md"]) {
  name: String!
  team: Team @link(inverse: "members")
}
`,
		"teams/Eng.md": `---
type: Team
name: Eng
members:
  - people/Alice.md
---
`,
		"people/Alice.md": `---
type: Person
name: Alice
---
`,
	}))
	store := openTestStore(t, root)
	vaultDef := obsidian.VaultDefinition{Path: root, Links: obsidian.LinkTypeBoth}
	runtime, err := ontology.EnsureFreshRuntimeWithStore(context.Background(), testNoteMetadata(t), vaultDef, &obsidian.Note{}, store)
	require.NoError(t, err)
	runCtx := &RunContext{VaultDef: vaultDef, VaultPath: root, NoteReader: &obsidian.Note{}, NoteMetadata: testNoteMetadata(t), MaxIssues: 100}
	result, _, err := RunSuiteOncePrepared(context.Background(), Options{
		Checks: []string{CheckOntology}, RunContext: runCtx,
	}, runtime, nil)
	require.NoError(t, err)
	require.NotNil(t, result.FixPlan)

	var action *FixAction
	for index := range result.FixPlan.Actions {
		if result.FixPlan.Actions[index].IssueCode == "inverse_mismatch" {
			action = &result.FixPlan.Actions[index]
			break
		}
	}
	require.NotNil(t, action)
	require.Equal(t, FixKindOntologySetLink, action.Kind)
	require.Len(t, action.IssueKeys, 1)
	require.Len(t, action.OperationIDs, 1)

	var operation *RepairOperation
	for index := range result.FixPlan.Operations {
		if result.FixPlan.Operations[index].ID == action.OperationIDs[0] {
			operation = &result.FixPlan.Operations[index]
			break
		}
	}
	require.NotNil(t, operation)
	require.Contains(t, string(operation.Content), "teams/Eng.md")
}

func indexRequired(t *testing.T, value, fragment string) int {
	t.Helper()
	for index := 0; index+len(fragment) <= len(value); index++ {
		if value[index:index+len(fragment)] == fragment {
			return index
		}
	}
	t.Fatalf("%q not found", fragment)
	return -1
}
