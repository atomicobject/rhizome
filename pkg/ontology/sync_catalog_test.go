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

func TestSyncPaths_ReplacesOnlyTouchedStructuralSources(t *testing.T) {
	root := t.TempDir()
	writeOntologyTestConfig(t, root)
	writeOntologySchema(t, root, `
type Decision @node(paths: ["notes/decisions/*.md"]) {
  name: String!
}

type Project @node(paths: ["notes/projects/*.md"]) {
  name: String!
  decision: Decision @link
}
`)
	writeOntologyNote(t, root, "notes/decisions/a.md", `---
type: Decision
name: A
---
`)
	writeOntologyNote(t, root, "notes/decisions/b.md", `---
type: Decision
name: B
---
`)
	writeOntologyNote(t, root, "notes/projects/one.md", `---
type: Project
name: One
decision: notes/decisions/a.md
---
`)
	writeOntologyNote(t, root, "notes/projects/two.md", `---
type: Project
name: Two
decision: notes/decisions/a.md
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

	writeOntologyNote(t, root, "notes/projects/one.md", `---
type: Project
name: One
decision: notes/decisions/b.md
---
`)

	err = testkit.NoteMetadataIndexer(t).SyncPaths(context.Background(), obsidian.VaultDefinition{Path: root}, &obsidian.Note{}, store, []string{"notes/projects/one.md"}, nil)
	require.NoError(t, err)
	result, err := syncPathsForTest(context.Background(), testkit.NoteMetadataIndexer(t), obsidian.VaultDefinition{Path: root}, &obsidian.Note{}, store, nil, []string{"notes/projects/one.md"}, nil)
	require.NoError(t, err)
	require.True(t, result.Dirty)
	require.GreaterOrEqual(t, result.Assessed, 1)

	edges, err := store.OntologyEdgesForPaths(context.Background(), []string{"notes/projects/one.md", "notes/projects/two.md"}, true, "", 0)
	require.NoError(t, err)
	requireEdge(t, edges, "notes/projects/one.md", "decision", "notes/decisions/b.md", "field", true)
	requireEdge(t, edges, "notes/projects/two.md", "decision", "notes/decisions/a.md", "field", true)
	for _, edge := range edges {
		require.False(t, edge.SrcPath == "notes/projects/one.md" && edge.DstPath == "notes/decisions/a.md")
	}
}

func TestSyncPaths_UpdatesGlobalSourceNodesForUntypedNotes(t *testing.T) {
	root := t.TempDir()
	writeOntologyTestConfig(t, root)
	writeOntologySchema(t, root, `
type Person @node(paths: ["people/*.md"]) {
  name: String!
}

type ActionItem implements Section @node(locator: EMBEDDED) @source(shape: CHECKBOX_ITEM, marker: "#action-item", paths: ["notes/**/*.md"]) {
  done: Boolean! @field(sourceKind: CHECKBOX)
  assignee: Person @link
}
`)
	writeOntologyNote(t, root, "people/Alice.md", `---
type: Person
name: Alice
---
`)
	writeOntologyNote(t, root, "notes/scratch.md", "# Scratch\n\nNo actions yet.\n")

	dbPath := filepath.Join(root, ".rhizome", "db.sqlite")
	require.NoError(t, os.MkdirAll(filepath.Dir(dbPath), 0o755))
	store, err := codeanchorsqlite.Open(dbPath)
	require.NoError(t, err)
	defer func() { _ = store.Close() }()

	ctx := context.Background()
	_, err = testkit.NoteMetadataIndexer(t).EnsureIndexed(ctx, obsidian.VaultDefinition{Path: root}, &obsidian.Note{}, store)
	require.NoError(t, err)
	_, err = ensureIndexedForTest(ctx, testkit.NoteMetadataIndexer(t), obsidian.VaultDefinition{Path: root}, &obsidian.Note{}, store)
	require.NoError(t, err)

	writeOntologyNote(t, root, "notes/scratch.md", `# Scratch

- [ ] Call Alice #action-item
  assignee:: [[people/Alice]]
`)
	require.NoError(t, testkit.NoteMetadataIndexer(t).SyncPaths(ctx, obsidian.VaultDefinition{Path: root}, &obsidian.Note{}, store, []string{"notes/scratch.md"}, nil))
	result, err := syncPathsForTest(ctx, testkit.NoteMetadataIndexer(t), obsidian.VaultDefinition{Path: root}, &obsidian.Note{}, store, nil, []string{"notes/scratch.md"}, nil)
	require.NoError(t, err)
	require.True(t, result.Dirty)

	nodes, err := store.OntologyNodesByPaths(ctx, []string{"notes/scratch.md"})
	require.NoError(t, err)
	// The catalog emits a FallbackNote NOTE row for the untyped host alongside
	// the ActionItem EMBEDDED child, so downstream writers don't re-issue a
	// destructive Replace to install the FallbackNote (which would wipe the
	// child).
	var rootKinds, embeddedTypes []string
	for _, n := range nodes {
		switch n.NodeKind {
		case "NOTE":
			rootKinds = append(rootKinds, n.TypeName)
		case "EMBEDDED":
			embeddedTypes = append(embeddedTypes, n.TypeName)
		}
	}
	require.Equal(t, []string{ontoschema.FallbackNoteTypeName}, rootKinds)
	require.Equal(t, []string{"ActionItem"}, embeddedTypes)
	edges, err := store.OntologyEdgesForPaths(ctx, []string{"notes/scratch.md"}, true, "", 0)
	require.NoError(t, err)
	requireEdge(t, edges, "notes/scratch.md", "assignee", "people/Alice.md", "field", true)

	writeOntologyNote(t, root, "notes/scratch.md", "# Scratch\n\nNo actions anymore.\n")
	require.NoError(t, testkit.NoteMetadataIndexer(t).SyncPaths(ctx, obsidian.VaultDefinition{Path: root}, &obsidian.Note{}, store, []string{"notes/scratch.md"}, nil))
	result, err = syncPathsForTest(ctx, testkit.NoteMetadataIndexer(t), obsidian.VaultDefinition{Path: root}, &obsidian.Note{}, store, nil, []string{"notes/scratch.md"}, nil)
	require.NoError(t, err)
	require.True(t, result.Dirty)

	// After the action items are removed the path still matches the global
	// source pattern, so the FallbackNote NOTE row remains; only the embedded
	// children should be gone.
	nodes, err = store.OntologyNodesByPaths(ctx, []string{"notes/scratch.md"})
	require.NoError(t, err)
	require.Len(t, nodes, 2)
	require.Equal(t, 1, countOntologyTestNodesByKindAndType(nodes, "NOTE", ontoschema.FallbackNoteTypeName))
	require.Equal(t, 1, countOntologyTestNodesByKindAndType(nodes, "SECTION", ontoschema.FallbackSectionTypeName))
	edges, err = store.OntologyEdgesForPaths(ctx, []string{"notes/scratch.md"}, true, "", 0)
	require.NoError(t, err)
	for _, edge := range edges {
		require.False(t, edge.SrcPath == "notes/scratch.md" && edge.RelationName == "assignee")
	}
}

func TestEnsureIndexedKeepsFallbackRowsForPlainUntypedNotes(t *testing.T) {
	root := t.TempDir()
	writeOntologyTestConfig(t, root)
	writeOntologySchema(t, root, `
type Person @node(paths: ["people/**/*.md"]) {
  name: String! @field
}
`)
	writeOntologyNote(t, root, "notes/plain.md", "# Plain\n\nNo ontology type here.\n")

	dbPath := filepath.Join(root, ".rhizome", "db.sqlite")
	require.NoError(t, os.MkdirAll(filepath.Dir(dbPath), 0o755))
	store, err := codeanchorsqlite.Open(dbPath)
	require.NoError(t, err)
	defer func() { _ = store.Close() }()

	ctx := context.Background()
	vaultDef := obsidian.VaultDefinition{Path: root}
	require.NoError(t, testkit.NoteMetadataIndexer(t).SyncPaths(ctx, vaultDef, &obsidian.Note{}, store, []string{"notes/plain.md"}, nil))
	result, err := syncPathsForTest(ctx, testkit.NoteMetadataIndexer(t), vaultDef, &obsidian.Note{}, store, nil, []string{"notes/plain.md"}, nil)
	require.NoError(t, err)
	require.True(t, result.Dirty)
	incremental, err := store.OntologyNodesByPaths(ctx, []string{"notes/plain.md"})
	require.NoError(t, err)
	require.Len(t, incremental, 2)
	require.Equal(t, 1, countOntologyTestNodesByKindAndType(incremental, "NOTE", ontoschema.FallbackNoteTypeName))
	require.Equal(t, 1, countOntologyTestNodesByKindAndType(incremental, "SECTION", ontoschema.FallbackSectionTypeName))

	_, err = testkit.NoteMetadataIndexer(t).EnsureIndexed(ctx, vaultDef, &obsidian.Note{}, store)
	require.NoError(t, err)
	_, err = ensureIndexedForTest(ctx, testkit.NoteMetadataIndexer(t), vaultDef, &obsidian.Note{}, store)
	require.NoError(t, err)
	full, err := store.OntologyNodesByPaths(ctx, []string{"notes/plain.md"})
	require.NoError(t, err)
	require.Len(t, full, 2)
	require.Equal(t, 1, countOntologyTestNodesByKindAndType(full, "NOTE", ontoschema.FallbackNoteTypeName))
	require.Equal(t, 1, countOntologyTestNodesByKindAndType(full, "SECTION", ontoschema.FallbackSectionTypeName))
}

func TestSyncPaths_DoesNotInventRelationsFromPlainIdentifiers(t *testing.T) {
	root := t.TempDir()
	writeOntologyTestConfig(t, root)
	writeOntologySchema(t, root, `
type FrozenStoryScopeSection implements Section {
}

type EffortNote @node(paths: ["efforts/*.md"]) {
  summary: String!
  storiesInScopeFrozen: FrozenStoryScopeSection @contains(level: H2, heading: "Stories In Scope (Frozen)", required: true)
}

type UserStory implements Section @node(locator: EMBEDDED) {
  id: ID! @field @identifier(preferred: true, derivedSuffix: "US")
  status: String @field
}

type StoriesSection implements Section {
  stories: [UserStory!] @contains(level: H3)
}

type ProcessSpec @node(paths: ["specs/*.md"]) {
  summary: String!
  stories: StoriesSection @contains(level: H2, heading: "Stories")
}
`)
	writeOntologyNote(t, root, "specs/graph.md", `---
type: ProcessSpec
summary: Graph spec
---

# Graph spec

## Stories

### Graph story
id:: SPEC-0001.US1
status:: ready
^graph-story
`)
	writeOntologyNote(t, root, "efforts/graph.md", `---
type: EffortNote
summary: Graph effort
---

# Graph effort

## Stories In Scope (Frozen)

- SPEC-0001.US1 — Graph story, ready
`)
	store, err := codeanchorsqlite.Open(filepath.Join(root, ".rhizome", "db.sqlite"))
	require.NoError(t, err)
	defer func() { _ = store.Close() }()

	ctx := context.Background()
	err = testkit.NoteMetadataIndexer(t).SyncPaths(ctx, obsidian.VaultDefinition{Path: root}, &obsidian.Note{}, store, []string{"specs/graph.md", "efforts/graph.md"}, nil)
	require.NoError(t, err)
	_, err = syncPathsForTest(ctx, testkit.NoteMetadataIndexer(t), obsidian.VaultDefinition{Path: root}, &obsidian.Note{}, store, nil, []string{"specs/graph.md", "efforts/graph.md"}, nil)
	require.NoError(t, err)

	edges, err := store.OntologyEdgesForPaths(ctx, []string{"efforts/graph.md"}, true, "frozenStories", 0)
	require.NoError(t, err)
	require.Empty(t, edges)
}

func TestSyncPathsRepairsUntouchedMissingOntologyRowsDuringIncrementalSync(t *testing.T) {
	root := t.TempDir()
	writeOntologyTestConfig(t, root)
	writeOntologySchema(t, root, `
type Project @node(paths: ["notes/*.md"]) {
  name: String!
}
`)
	writeOntologyNote(t, root, "notes/one.md", "---\nname: One\n---\n")
	writeOntologyNote(t, root, "notes/two.md", "---\nname: Two\n---\n")

	dbPath := filepath.Join(root, ".rhizome", "db.sqlite")
	require.NoError(t, os.MkdirAll(filepath.Dir(dbPath), 0o755))
	store, err := codeanchorsqlite.Open(dbPath)
	require.NoError(t, err)
	defer func() { _ = store.Close() }()

	ctx := context.Background()
	vaultDef := obsidian.VaultDefinition{Path: root}
	note := &obsidian.Note{}
	_, err = testkit.NoteMetadataIndexer(t).EnsureIndexed(ctx, vaultDef, note, store)
	require.NoError(t, err)
	_, err = ensureIndexedForTest(ctx, testkit.NoteMetadataIndexer(t), vaultDef, note, store)
	require.NoError(t, err)
	require.NoError(t, store.DeleteOntologyForPaths(ctx, []string{"notes/one.md"}))

	writeOntologyNote(t, root, "notes/two.md", "---\nname: Updated\n---\n")
	require.NoError(t, testkit.NoteMetadataIndexer(t).SyncPaths(ctx, vaultDef, note, store, []string{"notes/two.md"}, nil))

	result, err := syncPathsForTest(ctx, testkit.NoteMetadataIndexer(t), vaultDef, note, store, nil, []string{"notes/two.md"}, nil)
	require.NoError(t, err)
	require.False(t, result.Rebuilt)
	require.Equal(t, 2, result.Assessed)

	paths := []string{"notes/one.md", "notes/two.md"}
	assessments, err := store.OntologyAssessmentsByPaths(ctx, paths)
	require.NoError(t, err)
	require.Len(t, assessments, 2)
	states, err := store.OntologyNoteStatesByPaths(ctx, paths)
	require.NoError(t, err)
	require.Len(t, states, 2)
	types, err := store.OntologyTypesByPaths(ctx, paths)
	require.NoError(t, err)
	require.Len(t, types, 2)
}
