package ontology

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/atomicobject/rhizome/pkg/paths"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"
)

const editRegressionCollectionPath = "specs/001-test/spec.md"

func prepareEditRegressionCollectionFixture(t *testing.T) (string, *Schema, NodeRef) {
	t.Helper()
	root := t.TempDir()
	writeOntologyTestConfig(t, root)
	writeOntologySchema(t, root, embeddedProjectionSchema)
	writeOntologyNote(t, root, editRegressionCollectionPath, `---
type: Spec
summary: Test
---

# Spec

## User Stories

### Story A
status:: TODO
^story-a

### Story B
status:: TODO
^story-b
`)

	schema, err := LoadSchema(root)
	require.NoError(t, err)
	note, err := ProjectNote(
		context.Background(),
		obsidian.VaultDefinition{Path: root},
		&obsidian.Note{},
		schema,
		editRegressionCollectionPath,
	)
	require.NoError(t, err)
	containerRefs := note.Fields["userStoriesSection"].SectionNodes
	require.Len(t, containerRefs, 1)
	return root, schema, containerRefs[0]
}

func TestEditSession_ReorderCollectionRejectsDuplicateMembers(t *testing.T) {
	root, schema, containerRef := prepareEditRegressionCollectionFixture(t)
	vaultDef := obsidian.VaultDefinition{Path: root}
	session := NewEditSession(vaultDef, &obsidian.Note{}, schema)
	require.NoError(t, session.ReorderCollection(containerRef, "stories", []string{"^story-a", "^story-a"}))

	result, err := session.Commit(context.Background())
	require.NoError(t, err)
	require.False(t, result.Applied)
	require.Len(t, result.Conflicts, 1)
	require.Equal(t, ConflictKindCollectionDrift, result.Conflicts[0].Kind)

	content, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(editRegressionCollectionPath)))
	require.NoError(t, err)
	text := string(content)
	require.Contains(t, text, "^story-a")
	require.Contains(t, text, "^story-b")
}

func TestEditSession_AddEmbeddedNodeRejectsDuplicateBlockID(t *testing.T) {
	root, schema, containerRef := prepareEditRegressionCollectionFixture(t)
	vaultDef := obsidian.VaultDefinition{Path: root}
	session := NewEditSession(vaultDef, &obsidian.Note{}, schema)
	require.NoError(t, session.AddEmbeddedNode(containerRef, "stories", "Story C", "status:: TODO", "story-a"))

	result, err := session.Commit(context.Background())
	require.NoError(t, err)
	require.False(t, result.Applied)
	require.Len(t, result.Conflicts, 1)
	require.Equal(t, ConflictKindUnsupportedTarget, result.Conflicts[0].Kind)

	content, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(editRegressionCollectionPath)))
	require.NoError(t, err)
	require.Equal(t, 1, strings.Count(string(content), "^story-a"))
}

func TestEditSession_InsertNarrativeUsesNestedNodeBodyBoundary(t *testing.T) {
	root, schema, containerRef := prepareEditRegressionCollectionFixture(t)
	container, err := ProjectNode(context.Background(), obsidian.VaultDefinition{Path: root}, &obsidian.Note{}, schema, containerRef)
	require.NoError(t, err)
	storyRef := container.Collections["stories"].Items[0].Ref
	session := NewEditSession(obsidian.VaultDefinition{Path: root}, &obsidian.Note{}, schema)
	require.NoError(t, session.InsertNarrative(storyRef, "First story narrative."))

	result, err := session.Commit(context.Background())
	require.NoError(t, err)
	require.Equal(t, CommitOutcomeCommitted, result.Outcome)
	content, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(editRegressionCollectionPath)))
	require.NoError(t, err)
	text := string(content)
	storyStart := strings.Index(text, "### Story A")
	storyEnd := strings.Index(text, "### Story B")
	require.Greater(t, storyEnd, storyStart)
	require.Contains(t, text[storyStart:storyEnd], "First story narrative.")
}

func TestEditSession_CommitCanonicalizesPathAliasesBeforeReplay(t *testing.T) {
	root := t.TempDir()
	writeOntologyTestConfig(t, root)
	writeOntologySchema(t, root, `
type Spec @node(paths: ["notes/*.md"]) {
  summary: String!
  status: String
}
`)
	writeOntologyNote(t, root, "notes/spec.md", `---
type: Spec
summary: Original
status: draft
---

# Spec
`)
	schema, err := LoadSchema(root)
	require.NoError(t, err)
	session := NewEditSession(obsidian.VaultDefinition{Path: root}, &obsidian.Note{}, schema)
	require.NoError(t, session.SetScalarField(NodeRef{NotePath: "notes/spec.md", Kind: NodeKindNote}, "summary", "Updated"))
	require.NoError(t, session.SetScalarField(NodeRef{NotePath: "./notes/spec.md", Kind: NodeKindNote}, "status", "ready"))

	result, err := session.Commit(context.Background())
	require.NoError(t, err)
	require.True(t, result.Applied)
	require.Len(t, result.Plan.Files, 1)

	content, err := os.ReadFile(filepath.Join(root, "notes", "spec.md"))
	require.NoError(t, err)
	require.Contains(t, string(content), "summary: Updated")
	require.Contains(t, string(content), "status: ready")
}

func TestEditSessionRejectsDirectIdentifierFieldEdits(t *testing.T) {
	root := t.TempDir()
	writeOntologyTestConfig(t, root)
	writeOntologySchema(t, root, `
type Spec @node(paths: ["notes/*.md"]) {
  id: String! @field @identifier(preferred: true, prefix: "SPEC")
}
`)
	writeOntologyNote(t, root, "notes/spec.md", "---\ntype: Spec\nid: SPEC-0001\naliases: [SPEC-0001]\n---\n# Spec\n")
	schema, err := LoadSchema(root)
	require.NoError(t, err)
	session := NewEditSession(obsidian.VaultDefinition{Path: root}, &obsidian.Note{}, schema)
	require.NoError(t, session.SetScalarField(NodeRef{NotePath: "notes/spec.md", Kind: NodeKindNote}, "id", "SPEC-0002"))

	_, err = session.Preview(context.Background())
	require.ErrorContains(t, err, "identifier field id requires the graph-safe identifier rename workflow")
}

func TestEditSessionRejectsDirectIdentifierFieldNamedTitle(t *testing.T) {
	root := t.TempDir()
	writeOntologyTestConfig(t, root)
	writeOntologySchema(t, root, `
type Spec @node(paths: ["notes/*.md"]) {
  title: String! @field @identifier(preferred: true, prefix: "SPEC")
}
`)
	writeOntologyNote(t, root, "notes/spec.md", "---\ntype: Spec\ntitle: SPEC-0001\n---\n# Spec\n")
	schema, err := LoadSchema(root)
	require.NoError(t, err)
	session := NewEditSession(obsidian.VaultDefinition{Path: root}, &obsidian.Note{}, schema)
	require.NoError(t, session.SetScalarField(NodeRef{NotePath: "notes/spec.md", Kind: NodeKindNote}, "title", "SPEC-0002"))

	_, err = session.Preview(context.Background())
	require.ErrorContains(t, err, "identifier field title requires the graph-safe identifier rename workflow")

	upperCaseSession := NewEditSession(obsidian.VaultDefinition{Path: root}, &obsidian.Note{}, schema)
	require.NoError(t, upperCaseSession.SetScalarField(NodeRef{NotePath: "notes/spec.md", Kind: NodeKindNote}, "TITLE", "SPEC-0002"))
	_, err = upperCaseSession.Preview(context.Background())
	require.ErrorContains(t, err, "identifier field TITLE requires the graph-safe identifier rename workflow")
}

func TestEditSession_CommitPreservesFileMode(t *testing.T) {
	root := t.TempDir()
	writeOntologyTestConfig(t, root)
	writeOntologySchema(t, root, `
type Spec @node(paths: ["notes/*.md"]) {
  summary: String!
}
`)
	writeOntologyNote(t, root, "notes/spec.md", `---
type: Spec
summary: Original
---

# Spec
`)
	path := filepath.Join(root, "notes", "spec.md")
	require.NoError(t, os.Chmod(path, 0o644))
	before, err := os.Stat(path)
	require.NoError(t, err)
	schema, err := LoadSchema(root)
	require.NoError(t, err)
	session := NewEditSession(obsidian.VaultDefinition{Path: root}, &obsidian.Note{}, schema)
	require.NoError(t, session.SetScalarField(NodeRef{NotePath: "notes/spec.md", Kind: NodeKindNote}, "summary", "Updated"))

	result, err := session.Commit(context.Background())
	require.NoError(t, err)
	require.True(t, result.Applied)
	info, err := os.Stat(path)
	require.NoError(t, err)
	require.Equal(t, before.Mode().Perm(), info.Mode().Perm())
}

type editSessionRaceNoteReader struct {
	delegate        obsidian.NoteReader
	targetPath      string
	external        string
	mutateAfterRead int

	mu    sync.Mutex
	reads int
}

func (r *editSessionRaceNoteReader) GetContents(vaultDef obsidian.VaultDefinition, notePath string) (string, error) {
	content, err := r.delegate.GetContents(vaultDef, notePath)
	if err != nil {
		return "", err
	}
	r.mu.Lock()
	r.reads++
	mutate := notePath == r.targetPath && r.reads == r.mutateAfterRead
	r.mu.Unlock()
	if mutate {
		if err := os.WriteFile(filepath.Join(vaultDef.BasePath(), filepath.FromSlash(r.targetPath)), []byte(r.external), 0o644); err != nil {
			return "", err
		}
	}
	return content, nil
}

func (r *editSessionRaceNoteReader) GetNotesList(vaultDef obsidian.VaultDefinition) ([]string, error) {
	return r.delegate.GetNotesList(vaultDef)
}

func (r *editSessionRaceNoteReader) GetModTime(vaultDef obsidian.VaultDefinition, notePath string) (time.Time, error) {
	return r.delegate.GetModTime(vaultDef, notePath)
}

func (r *editSessionRaceNoteReader) Title(path string) (string, bool) {
	return r.delegate.Title(path)
}

func TestEditSession_CommitChecksDiskBeforeReplace(t *testing.T) {
	root := t.TempDir()
	writeOntologyTestConfig(t, root)
	writeOntologySchema(t, root, `
type Spec @node(paths: ["notes/*.md"]) {
  summary: String!
}
`)
	original := `---
type: Spec
summary: Original
---

# Spec
`
	external := `---
type: Spec
summary: External
---

# Spec
`
	writeOntologyNote(t, root, "notes/spec.md", original)
	schema, err := LoadSchema(root)
	require.NoError(t, err)
	reader := &editSessionRaceNoteReader{
		delegate:        &obsidian.Note{},
		targetPath:      "notes/spec.md",
		external:        external,
		mutateAfterRead: 2,
	}
	session := NewEditSession(obsidian.VaultDefinition{Path: root}, reader, schema)
	require.NoError(t, session.SetScalarField(NodeRef{NotePath: "notes/spec.md", Kind: NodeKindNote}, "summary", "Mine"))

	result, err := session.Commit(context.Background())
	require.NoError(t, err)
	require.False(t, result.Applied)
	require.Len(t, result.Conflicts, 1)
	require.Equal(t, ConflictKindSourceChanged, result.Conflicts[0].Kind)
	require.Equal(t, "notes/spec.md", result.Conflicts[0].NotePath)

	content, err := os.ReadFile(filepath.Join(root, "notes", "spec.md"))
	require.NoError(t, err)
	require.Equal(t, external, string(content))
}

func TestWriteStatesAtomicallyPreservesExistingBackup(t *testing.T) {
	root := t.TempDir()
	writeOntologyNote(t, root, "notes/spec.md", "original\n")
	backupPath := filepath.Join(root, "notes", ".spec.md.rhizome-backup")
	require.NoError(t, os.WriteFile(backupPath, []byte("unrelated backup\n"), 0o644))
	vaultPaths, err := paths.NewVaultPaths(root)
	require.NoError(t, err)

	err = writeStatesAtomically(map[string]*documentState{
		"notes/spec.md": {
			notePath: "notes/spec.md",
			content:  "updated\n",
		},
	}, &vaultPaths, "", nil)
	require.NoError(t, err)
	backup, err := os.ReadFile(backupPath)
	require.NoError(t, err)
	require.Equal(t, "unrelated backup\n", string(backup))
}

func TestEditSession_RejectsNewInvalidTypedFieldValues(t *testing.T) {
	tests := []struct {
		name     string
		field    string
		value    string
		typeName string
	}{
		{name: "enum", field: "status", value: "UNKNOWN", typeName: "Status"},
		{name: "int", field: "count", value: "not-an-int", typeName: "Int"},
		{name: "float", field: "ratio", value: "not-a-float", typeName: "Float"},
		{name: "date", field: "due", value: "not-a-date", typeName: "Date"},
		{name: "boolean", field: "enabled", value: "not-a-boolean", typeName: "Boolean"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			writeOntologyTestConfig(t, root)
			writeOntologySchema(t, root, `
enum Status { OPEN CLOSED }

type TypedSpec @node(paths: ["notes/*.md"]) {
  status: Status
  count: Int
  ratio: Float
  due: Date
  enabled: Boolean
}
`)
			original := `---
type: TypedSpec
status: OPEN
count: 1
ratio: 1.5
due: 2026-09-11
enabled: true
---

# Spec
`
			writeOntologyNote(t, root, "notes/spec.md", original)
			schema, err := LoadSchema(root)
			require.NoError(t, err)
			session := NewEditSession(obsidian.VaultDefinition{Path: root}, &obsidian.Note{}, schema)
			require.NoError(t, session.SetScalarField(NodeRef{NotePath: "notes/spec.md", Kind: NodeKindNote}, tc.field, tc.value))

			result, err := session.Commit(context.Background())
			require.NoError(t, err)
			require.False(t, result.Applied)
			require.Len(t, result.Conflicts, 1)
			require.Equal(t, ConflictKindUnsupportedTarget, result.Conflicts[0].Kind)
			require.Equal(t, tc.field, result.Conflicts[0].Field)
			require.Contains(t, result.Conflicts[0].Message, tc.typeName)
			content, err := os.ReadFile(filepath.Join(root, "notes", "spec.md"))
			require.NoError(t, err)
			require.Equal(t, original, string(content))
		})
	}
}

func TestEditSession_CommitReportsSameFieldDrift(t *testing.T) {
	root := t.TempDir()
	writeOntologyTestConfig(t, root)
	writeOntologySchema(t, root, `
type Spec @node(paths: ["notes/*.md"]) {
  summary: String!
}
`)
	writeOntologyNote(t, root, "notes/spec.md", `---
type: Spec
summary: Original
---

# Spec
`)
	schema, err := LoadSchema(root)
	require.NoError(t, err)
	session := NewEditSession(obsidian.VaultDefinition{Path: root}, &obsidian.Note{}, schema)
	require.NoError(t, session.SetScalarField(NodeRef{NotePath: "notes/spec.md", Kind: NodeKindNote}, "summary", "Mine"))
	_, err = session.Preview(context.Background())
	require.NoError(t, err)
	writeOntologyNote(t, root, "notes/spec.md", `---
type: Spec
summary: Theirs
---

# Spec
`)

	result, err := session.Commit(context.Background())
	require.NoError(t, err)
	require.False(t, result.Applied)
	require.Len(t, result.Conflicts, 1)
	require.Equal(t, ConflictKind("FIELD_CHANGED"), result.Conflicts[0].Kind)
	require.Equal(t, "summary", result.Conflicts[0].Field)
	require.Contains(t, result.Conflicts[0].Message, "Original")
	require.Contains(t, result.Conflicts[0].Message, "Theirs")
	require.Contains(t, result.Conflicts[0].Message, "Mine")

	content, err := os.ReadFile(filepath.Join(root, "notes", "spec.md"))
	require.NoError(t, err)
	require.Contains(t, string(content), "summary: Theirs")
}

func TestEditSession_CommitReportsDriftWhenExplicitEditRevertsToWitness(t *testing.T) {
	root := t.TempDir()
	writeOntologyTestConfig(t, root)
	writeOntologySchema(t, root, `
type Spec @node(paths: ["notes/*.md"]) {
  summary: String!
}
`)
	writeOntologyNote(t, root, "notes/spec.md", `---
type: Spec
summary: Original
---

# Spec
`)
	schema, err := LoadSchema(root)
	require.NoError(t, err)
	session := NewEditSession(obsidian.VaultDefinition{Path: root}, &obsidian.Note{}, schema)
	require.NoError(t, session.SetFieldValueWithWitness(
		NodeRef{NotePath: "notes/spec.md", Kind: NodeKindNote},
		"summary", []string{"Original"}, []string{"Original"}, "scalar", false, false, false,
	))
	writeOntologyNote(t, root, "notes/spec.md", `---
type: Spec
summary: External
---

# Spec
`)

	result, err := session.Commit(context.Background())
	require.NoError(t, err)
	require.False(t, result.Applied)
	require.Len(t, result.Conflicts, 1)
	require.Equal(t, ConflictKindFieldChanged, result.Conflicts[0].Kind)
	content, err := os.ReadFile(filepath.Join(root, "notes", "spec.md"))
	require.NoError(t, err)
	require.Contains(t, string(content), "summary: External")
}

func TestEditSession_SetFrontmatterFieldPreservesUnchangedYAMLSource(t *testing.T) {
	root := t.TempDir()
	writeOntologyTestConfig(t, root)
	writeOntologySchema(t, root, `
type Spec @node(paths: ["notes/*.md"]) {
  summary: String!
}
`)
	writeOntologyNote(t, root, "notes/spec.md", `---
type: Spec
summary: "Original summary"
meta:
  # keep comment
  quoted: "value"
  folded: >
    long
    text
tags: [one, two]
---

# Spec
`)
	schema, err := LoadSchema(root)
	require.NoError(t, err)
	session := NewEditSession(obsidian.VaultDefinition{Path: root}, &obsidian.Note{}, schema)
	require.NoError(t, session.SetScalarField(NodeRef{NotePath: "notes/spec.md", Kind: NodeKindNote}, "summary", "Updated summary"))
	plan, err := session.Preview(context.Background())
	require.NoError(t, err)
	require.Len(t, plan.Files, 1)

	updated := plan.Files[0].UpdatedContentPreview
	require.Contains(t, updated, `summary: "Updated summary"`)
	require.Contains(t, updated, "meta:\n  # keep comment\n  quoted: \"value\"\n  folded: >\n    long\n    text\ntags: [one, two]")
}
