package ontology

import (
	"context"
	"github.com/atomicobject/rhizome/internal/testkit"
	ontoschema "github.com/atomicobject/rhizome/pkg/ontology/schema"
	codeanchorsqlite "github.com/atomicobject/rhizome/pkg/store"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"
	"os"
	"path/filepath"
	"testing"
)

func TestSyncPaths_UpdatesSectionValidationIncrementally(t *testing.T) {
	root := t.TempDir()
	writeOntologyTestConfig(t, root)
	writeOntologySchema(t, root, `
type RequirementsSection implements Section {
  decisions: [Decision!] @neighbors(direction: OUTBOUND, type: "Decision", scope: SUBTREE)
}

type Decision @node(paths: ["notes/decisions/*.md"]) {
  name: String!
}

	type Spec @node(paths: ["notes/specs/*.md"]) {
	  name: String!
	  requirements: RequirementsSection @contains(level: H2, heading: "Requirements", required: true)
	}
`)
	writeOntologyNote(t, root, "notes/specs/spec.md", `---
type: Spec
name: Spec
---

## Requirements

See [[decision]].
`)
	writeOntologyNote(t, root, "notes/decisions/decision.md", `---
type: Decision
name: Decision
---
`)

	dbPath := filepath.Join(root, ".rhizome", "db.sqlite")
	require.NoError(t, os.MkdirAll(filepath.Dir(dbPath), 0o755))
	store, err := codeanchorsqlite.Open(dbPath)
	require.NoError(t, err)
	defer func() { _ = store.Close() }()

	_, err = testkit.NoteMetadataIndexer(t).EnsureIndexed(context.Background(), obsidian.VaultDefinition{Path: root}, &obsidian.Note{}, store)
	require.NoError(t, err)
	_, err = ensureIndexedForTest(context.Background(), testkit.NoteMetadataIndexer(t), obsidian.VaultDefinition{Path: root}, &obsidian.Note{}, store)
	require.NoError(t, err)

	writeOntologyNote(t, root, "notes/specs/spec.md", `---
type: Spec
name: Spec
---

### Requirements
`)
	err = testkit.NoteMetadataIndexer(t).SyncPaths(context.Background(), obsidian.VaultDefinition{Path: root}, &obsidian.Note{}, store, []string{"notes/specs/spec.md"}, nil)
	require.NoError(t, err)

	result, err := syncPathsForTest(context.Background(), testkit.NoteMetadataIndexer(t), obsidian.VaultDefinition{Path: root}, &obsidian.Note{}, store, nil, []string{"notes/specs/spec.md"}, nil)
	require.NoError(t, err)
	requireIssueCode(t, result.Issues, "wrong_section_level")

	writeOntologyNote(t, root, "notes/specs/spec.md", `---
type: Spec
name: Spec
---

## Requirements

See [[decision]].
`)
	err = testkit.NoteMetadataIndexer(t).SyncPaths(context.Background(), obsidian.VaultDefinition{Path: root}, &obsidian.Note{}, store, []string{"notes/specs/spec.md"}, nil)
	require.NoError(t, err)

	result, err = syncPathsForTest(context.Background(), testkit.NoteMetadataIndexer(t), obsidian.VaultDefinition{Path: root}, &obsidian.Note{}, store, nil, []string{"notes/specs/spec.md"}, nil)
	require.NoError(t, err)
	for _, issue := range result.Issues {
		require.NotEqual(t, "wrong_section_level", issue.Code)
		require.NotEqual(t, "empty_required_section", issue.Code)
	}

	updatedState, err := store.GetOntologySchemaState(context.Background())
	require.NoError(t, err)
	require.NotContains(t, updatedState.ErrorJSON, "wrong_section_level")

	row, ok, err := store.GetOntologyAssessmentByPath(context.Background(), "notes/specs/spec.md")
	require.NoError(t, err)
	require.True(t, ok)
	assessment, err := AssessmentFromJSON(row.AssessmentJSON)
	require.NoError(t, err)
	require.NotNil(t, assessment)
	for _, issue := range assessment.Issues {
		require.NotEqual(t, "wrong_section_level", issue.Code)
		require.NotEqual(t, "empty_required_section", issue.Code)
	}
}

func TestSyncPaths_UpdatesNestedSectionValidationIncrementally(t *testing.T) {
	root := t.TempDir()
	writeOntologyTestConfig(t, root)
	writeOntologySchema(t, root, `
type DetailsSection implements Section {
}

type RequirementsSection implements Section {
  details: DetailsSection @contains(level: H3, heading: "Details", required: true)
}

type Spec @node(paths: ["notes/specs/*.md"]) {
  name: String!
  requirements: RequirementsSection @contains(level: H2, heading: "Requirements", required: true)
}
`)
	writeOntologyNote(t, root, "notes/specs/spec.md", `---
type: Spec
name: Spec
---

## Requirements

### Details

Ready.
`)

	dbPath := filepath.Join(root, ".rhizome", "db.sqlite")
	require.NoError(t, os.MkdirAll(filepath.Dir(dbPath), 0o755))
	store, err := codeanchorsqlite.Open(dbPath)
	require.NoError(t, err)
	defer func() { _ = store.Close() }()

	_, err = testkit.NoteMetadataIndexer(t).EnsureIndexed(context.Background(), obsidian.VaultDefinition{Path: root}, &obsidian.Note{}, store)
	require.NoError(t, err)
	_, err = ensureIndexedForTest(context.Background(), testkit.NoteMetadataIndexer(t), obsidian.VaultDefinition{Path: root}, &obsidian.Note{}, store)
	require.NoError(t, err)

	writeOntologyNote(t, root, "notes/specs/spec.md", `---
type: Spec
name: Spec
---

## Requirements

Ready.
`)
	err = testkit.NoteMetadataIndexer(t).SyncPaths(context.Background(), obsidian.VaultDefinition{Path: root}, &obsidian.Note{}, store, []string{"notes/specs/spec.md"}, nil)
	require.NoError(t, err)

	result, err := syncPathsForTest(context.Background(), testkit.NoteMetadataIndexer(t), obsidian.VaultDefinition{Path: root}, &obsidian.Note{}, store, nil, []string{"notes/specs/spec.md"}, nil)
	require.NoError(t, err)
	issue := requireIssue(t, result.Issues, "missing_required_section", "notes/specs/spec.md")
	require.Equal(t, "requirements.details", issue.FieldName)

	writeOntologyNote(t, root, "notes/specs/spec.md", `---
type: Spec
name: Spec
---

## Requirements

### Details

Ready again.
`)
	err = testkit.NoteMetadataIndexer(t).SyncPaths(context.Background(), obsidian.VaultDefinition{Path: root}, &obsidian.Note{}, store, []string{"notes/specs/spec.md"}, nil)
	require.NoError(t, err)

	result, err = syncPathsForTest(context.Background(), testkit.NoteMetadataIndexer(t), obsidian.VaultDefinition{Path: root}, &obsidian.Note{}, store, nil, []string{"notes/specs/spec.md"}, nil)
	require.NoError(t, err)
	for _, issue := range result.Issues {
		require.NotEqual(t, "requirements.details", issue.FieldName)
	}
}

func TestSyncPaths_PersistsValidationIssuesInSchemaState(t *testing.T) {
	root := t.TempDir()
	writeOntologyTestConfig(t, root)
	writeOntologySchema(t, root, `
type Project @node(paths: ["notes/*.md"]) {
  name: String!
}
`)
	writeOntologyNote(t, root, "notes/item.md", `---
type: Project
name: Item
---
`)

	dbPath := filepath.Join(root, ".rhizome", "db.sqlite")
	require.NoError(t, os.MkdirAll(filepath.Dir(dbPath), 0o755))
	store, err := codeanchorsqlite.Open(dbPath)
	require.NoError(t, err)
	defer func() { _ = store.Close() }()

	_, err = testkit.NoteMetadataIndexer(t).EnsureIndexed(context.Background(), obsidian.VaultDefinition{Path: root}, &obsidian.Note{}, store)
	require.NoError(t, err)
	_, err = ensureIndexedForTest(context.Background(), testkit.NoteMetadataIndexer(t), obsidian.VaultDefinition{Path: root}, &obsidian.Note{}, store)
	require.NoError(t, err)

	writeOntologyNote(t, root, "notes/item.md", `---
type: Project
---
`)
	err = testkit.NoteMetadataIndexer(t).SyncPaths(context.Background(), obsidian.VaultDefinition{Path: root}, &obsidian.Note{}, store, []string{"notes/item.md"}, nil)
	require.NoError(t, err)

	result, err := syncPathsForTest(context.Background(), testkit.NoteMetadataIndexer(t), obsidian.VaultDefinition{Path: root}, &obsidian.Note{}, store, nil, []string{"notes/item.md"}, nil)
	require.NoError(t, err)
	requireIssueCode(t, result.Issues, "missing_required_field")

	state, err := store.GetOntologySchemaState(context.Background())
	require.NoError(t, err)
	require.Contains(t, state.ErrorJSON, "missing_required_field")
}

func TestSyncPaths_PersistsSectionValidationIssuesInSchemaState(t *testing.T) {
	root := t.TempDir()
	writeOntologyTestConfig(t, root)
	writeOntologySchema(t, root, `
type RequirementsSection implements Section {
}

type Spec @node(paths: ["notes/*.md"]) {
  name: String!
  requirements: RequirementsSection @contains(level: H2, heading: "Requirements", required: true)
}
`)
	writeOntologyNote(t, root, "notes/item.md", `---
type: Spec
name: Item
---

## Requirements

Ready.
`)

	dbPath := filepath.Join(root, ".rhizome", "db.sqlite")
	require.NoError(t, os.MkdirAll(filepath.Dir(dbPath), 0o755))
	store, err := codeanchorsqlite.Open(dbPath)
	require.NoError(t, err)
	defer func() { _ = store.Close() }()

	_, err = testkit.NoteMetadataIndexer(t).EnsureIndexed(context.Background(), obsidian.VaultDefinition{Path: root}, &obsidian.Note{}, store)
	require.NoError(t, err)
	_, err = ensureIndexedForTest(context.Background(), testkit.NoteMetadataIndexer(t), obsidian.VaultDefinition{Path: root}, &obsidian.Note{}, store)
	require.NoError(t, err)

	writeOntologyNote(t, root, "notes/item.md", `---
type: Spec
name: Item
---

### Requirements
`)
	err = testkit.NoteMetadataIndexer(t).SyncPaths(context.Background(), obsidian.VaultDefinition{Path: root}, &obsidian.Note{}, store, []string{"notes/item.md"}, nil)
	require.NoError(t, err)

	result, err := syncPathsForTest(context.Background(), testkit.NoteMetadataIndexer(t), obsidian.VaultDefinition{Path: root}, &obsidian.Note{}, store, nil, []string{"notes/item.md"}, nil)
	require.NoError(t, err)
	requireIssueCode(t, result.Issues, "wrong_section_level")

	state, err := store.GetOntologySchemaState(context.Background())
	require.NoError(t, err)
	require.Contains(t, state.ErrorJSON, "wrong_section_level")
}

func TestSyncPaths_PersistsTraversalPoliciesOnSchemaRebuild(t *testing.T) {
	root := t.TempDir()
	writeOntologyTestConfig(t, root)
	writeOntologySchema(t, root, `
type Project
  @node(paths: ["notes/*.md"])
  @traversal(intents: ["docs_for_code"], includeAmbient: true, minStructuralHits: 3) {
  name: String!
}
`)
	writeOntologyNote(t, root, "notes/item.md", `---
type: Project
name: Item
---
`)

	dbPath := filepath.Join(root, ".rhizome", "db.sqlite")
	require.NoError(t, os.MkdirAll(filepath.Dir(dbPath), 0o755))
	store, err := codeanchorsqlite.Open(dbPath)
	require.NoError(t, err)
	defer func() { _ = store.Close() }()

	_, err = testkit.NoteMetadataIndexer(t).EnsureIndexed(context.Background(), obsidian.VaultDefinition{Path: root}, &obsidian.Note{}, store)
	require.NoError(t, err)

	result, err := syncPathsForTest(context.Background(), testkit.NoteMetadataIndexer(t), obsidian.VaultDefinition{Path: root}, &obsidian.Note{}, store, nil, nil, nil)
	require.NoError(t, err)
	require.True(t, result.Rebuilt)

	rows, err := store.OntologyTypePoliciesForPaths(context.Background(), []string{"notes/item.md"})
	require.NoError(t, err)
	row, ok := rows["notes/item.md"]
	require.True(t, ok)

	policy, err := ontoschema.UnmarshalTypePolicy(row.PolicyJSON)
	require.NoError(t, err)
	override, ok := policy.OverrideForIntent("docs_for_code")
	require.True(t, ok)
	require.NotNil(t, override.IncludeAmbient)
	require.True(t, *override.IncludeAmbient)
	require.NotNil(t, override.MinStructuralHits)
	require.Equal(t, 3, *override.MinStructuralHits)
}
