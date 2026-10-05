package ontology

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	semdb "github.com/atomicobject/rhizome/pkg/anchors/sqlite"
	"github.com/stretchr/testify/require"
)

func TestOntologyNodeFieldValueForValue_NormalizesTypedScalarsAndLinks(t *testing.T) {
	node := codeanchor.IntelOntologyNode{NodeID: "node:1", NotePath: "notes/spec.md", TypeName: "Spec"}
	binding := FieldBinding{SourceKind: FieldSourceFrontmatter}

	rows := []codeanchor.IntelOntologyNodeFieldValue{
		ontologyNodeFieldValueForValue(&Schema{}, node, &Field{Name: "done", Kind: FieldKindScalar, TypeName: "Boolean"}, binding, "true", 0, 123),
		ontologyNodeFieldValueForValue(&Schema{}, node, &Field{Name: "count", Kind: FieldKindScalar, TypeName: "Int"}, binding, "42", 0, 123),
		ontologyNodeFieldValueForValue(&Schema{}, node, &Field{Name: "score", Kind: FieldKindScalar, TypeName: "Float"}, binding, "3.5", 0, 123),
		ontologyNodeFieldValueForValue(&Schema{}, node, &Field{Name: "due", Kind: FieldKindScalar, TypeName: "Date"}, binding, "2026-05-06", 0, 123),
		ontologyNodeFieldValueForValue(&Schema{}, node, &Field{Name: "updated", Kind: FieldKindScalar, TypeName: "DateTime"}, binding, "2026-05-06T12:34:56-04:00", 0, 123),
		ontologyNodeFieldValueForValue(&Schema{}, node, &Field{Name: "homepage", Kind: FieldKindScalar, TypeName: "URL"}, binding, "https://example.com/a", 0, 123),
		ontologyNodeFieldValueForValue(&Schema{}, node, &Field{Name: "code", Kind: FieldKindScalar, TypeName: "ID"}, binding, "SPEC-0001", 0, 123),
		ontologyNodeFieldValueForValue(&Schema{}, node, &Field{Name: "status", Kind: FieldKindEnum, TypeName: "SpecStatus"}, binding, "Active", 0, 123),
		ontologyNodeFieldValueForValue(&Schema{}, node, &Field{Name: "tags", Kind: FieldKindScalar, TypeName: "String", List: true}, binding, "Second", 1, 123),
		ontologyNodeFieldValueForValue(&Schema{}, node, &Field{Name: "parent", Kind: FieldKindLink, TypeName: "Spec"}, binding, "[[docs/specs/root.md|Root]]", 0, 123),
	}

	require.Equal(t, "bool", rows[0].ValueKind)
	require.NotNil(t, rows[0].ValueBool)
	require.True(t, *rows[0].ValueBool)
	require.Equal(t, "int", rows[1].ValueKind)
	require.Equal(t, int64(42), *rows[1].ValueInt)
	require.Equal(t, "real", rows[2].ValueKind)
	require.Equal(t, 3.5, *rows[2].ValueReal)
	require.Equal(t, "date", rows[3].ValueKind)
	require.Equal(t, "2026-05-06", *rows[3].ValueDate)
	require.Equal(t, "datetime", rows[4].ValueKind)
	require.Equal(t, "2026-05-06T12:34:56-04:00", *rows[4].ValueDateTime)
	require.Equal(t, "url", rows[5].ValueKind)
	require.Equal(t, "id", rows[6].ValueKind)
	require.Equal(t, "enum", rows[7].ValueKind)
	require.Equal(t, "active", rows[7].ValueNorm)
	require.Equal(t, 1, rows[8].ListOrdinal)
	require.Equal(t, "link", rows[9].ValueKind)
	require.Equal(t, "docs/specs/root.md", rows[9].TargetNotePath)
	require.Equal(t, "FRONTMATTER", rows[9].SourceKind)
}

func TestOntologyNodeFieldReadModelForProjection_ResolvesLinkTargetsAndDependencies(t *testing.T) {
	node := codeanchor.IntelOntologyNode{NodeID: "node:1", NotePath: "notes/spec.md", TypeName: "Spec"}
	projection := &NodeProjection{
		Type: &NoteType{Fields: []*Field{
			{Name: "owner", Kind: FieldKindLink, TypeName: "Person"},
		}},
		Fields: map[string]FieldBinding{
			"owner": {
				SourceKind: FieldSourceFrontmatter,
				Present:    true,
				Values:     []string{"[[ALICE|Alice]]"},
			},
		},
	}

	rows, deps := IntelOntologyNodeFieldReadModelForProjection(&Schema{Hash: "schema-hash"}, projection, node, 123, BuildIntelOntologyNodeReadModelOptions{
		LinkResolver: OntologyLinkTargetResolverFunc(func(field *Field, targetInput string) (OntologyLinkTarget, bool) {
			require.Equal(t, "owner", field.Name)
			require.Equal(t, "ALICE", targetInput)
			return OntologyLinkTarget{
				NotePath:      "people/alice.md",
				TypeName:      "Person",
				NodeID:        "node:alice",
				RefJSON:       `{"notePath":"people/alice.md","kind":"NOTE","typeName":"Person"}`,
				SourceLocator: "people/alice.md",
			}, true
		}),
	})

	require.Len(t, rows, 1)
	require.Equal(t, "[[ALICE|Alice]]", rows[0].ValueText)
	require.Equal(t, "alice", rows[0].ValueNorm)
	require.Equal(t, "people/alice.md", rows[0].TargetNotePath)
	require.Equal(t, "Person", rows[0].TargetTypeName)
	require.Equal(t, "node:alice", rows[0].TargetNodeID)
	require.Equal(t, "people/alice.md", rows[0].TargetSourceLocator)

	require.Len(t, deps, 1)
	require.Equal(t, "notes/spec.md", deps[0].SourceNotePath)
	require.Equal(t, "owner", deps[0].FieldName)
	require.Equal(t, "ALICE", deps[0].TargetInput)
	require.Equal(t, "alice", deps[0].TargetInputNorm)
	require.Equal(t, "people/alice.md", deps[0].ResolvedTargetNotePath)
	require.Equal(t, "Person", deps[0].ResolvedTargetTypeName)
}

func TestOntologyNodeFieldReadModelForProjection_SkipsEmptyLinkBindings(t *testing.T) {
	node := codeanchor.IntelOntologyNode{NodeID: "node:1", NotePath: "notes/spec.md", TypeName: "Spec"}
	projection := &NodeProjection{
		Type: &NoteType{Fields: []*Field{
			{Name: "owner", Kind: FieldKindLink, TypeName: "Person"},
		}},
		Fields: map[string]FieldBinding{
			"owner": {
				SourceKind: FieldSourceFrontmatter,
				Present:    true,
				// Empty value (e.g. authored as `owner:: ` with nothing after).
			},
		},
	}

	rows, deps := IntelOntologyNodeFieldReadModelForProjection(&Schema{Hash: "schema-hash"}, projection, node, 123, BuildIntelOntologyNodeReadModelOptions{})

	require.Empty(t, rows, "empty-binding Link fields must not emit a field row")
	require.Empty(t, deps, "empty-binding Link fields must not emit a link dependency")
}

// Regression for the action-items view returning zero rows: when an untyped
// note matches a global @source (e.g. a checkbox-shaped ActionItem), the
// catalog read model must emit BOTH a FallbackNote NOTE row and the embedded
// children. Without the FallbackNote row, downstream writers (semantic syncer)
// resorted to a destructive Replace to install it, wiping the embedded child
// rows the catalog had just written.
func TestBuildIntelOntologyNodeReadModel_UntypedGlobalSourceEmitsFallbackAndChildren(t *testing.T) {
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
	notePath := "notes/scratch.md"
	content := "# Scratch\n\n- [ ] Call Alice #action-item\n  assignee:: [[people/Alice]]\n- [x] Email Alice #action-item\n  assignee:: [[people/Alice]]\n"
	writeOntologyNote(t, root, notePath, content)

	schema, err := LoadSchema(root)
	require.NoError(t, err)
	snapshot, err := BuildDocumentSnapshot(notePath, content, time.Time{})
	require.NoError(t, err)
	projection, err := ProjectNodeFromSnapshot(snapshot, schema, NodeRef{NotePath: notePath, Kind: NodeKindNote})
	require.NoError(t, err)

	model, err := BuildIntelOntologyNodeReadModel(schema, projection, 1)
	require.NoError(t, err)

	var rootKinds, embeddedTypes []string
	for _, n := range model.Nodes {
		switch n.NodeKind {
		case "NOTE":
			rootKinds = append(rootKinds, n.TypeName)
		case "EMBEDDED":
			embeddedTypes = append(embeddedTypes, n.TypeName)
		}
	}
	require.Equal(t, []string{FallbackNoteTypeName}, rootKinds, "untyped note matching a global source must emit a FallbackNote NOTE row")
	require.Equal(t, []string{"ActionItem", "ActionItem"}, embeddedTypes, "global-source children must survive alongside the FallbackNote row")
	require.Equal(t, []string{notePath}, model.NotePaths)
}

func TestBuildIntelOntologyNodeReadModel_NestedGlobalSourceKeepsRowsFieldsAndDependenciesUnique(t *testing.T) {
	root := t.TempDir()
	writeOntologyTestConfig(t, root)
	writeOntologySchema(t, root, `
type Person @node(paths: ["people/*.md"]) {
  name: String!
}

type DailyNote @node(paths: ["notes/*.md"]) {
  actionItems: [ActionItem!] @contains(shape: CHECKBOX_ITEM, marker: "#action-item")
}

type ActionItem implements Section @node(locator: EMBEDDED) @source(shape: CHECKBOX_ITEM, marker: "#action-item", paths: ["notes/**/*.md"]) {
  done: Boolean! @field(sourceKind: CHECKBOX)
  assignee: Person @link
}
`)
	notePath := "notes/daily.md"
	content := `# Daily

- Parent list item
  - [x] Call Alice #action-item
    assignee:: [[people/Alice]]
`
	writeOntologyNote(t, root, notePath, content)

	schema, err := LoadSchema(root)
	require.NoError(t, err)
	snapshot, err := BuildDocumentSnapshot(notePath, content, time.Time{})
	require.NoError(t, err)
	projection, err := ProjectNodeFromSnapshot(snapshot, schema, NodeRef{NotePath: notePath, Kind: NodeKindNote})
	require.NoError(t, err)

	model, err := BuildIntelOntologyNodeReadModelWithOptions(schema, projection, 7, BuildIntelOntologyNodeReadModelOptions{
		LinkResolver: OntologyLinkTargetResolverFunc(func(field *Field, targetInput string) (OntologyLinkTarget, bool) {
			require.Equal(t, "assignee", field.Name)
			require.Equal(t, "people/Alice", targetInput)
			return OntologyLinkTarget{NotePath: "people/Alice.md", TypeName: "Person", NodeID: "person:alice"}, true
		}),
	})
	require.NoError(t, err)

	require.Len(t, model.Nodes, 2, "the contained global source is discovered twice but cataloged once")
	require.Equal(t, []string{"DailyNote", "ActionItem"}, []string{model.Nodes[0].TypeName, model.Nodes[1].TypeName})
	require.Equal(t, model.Nodes[0].NodeID, model.Nodes[1].ParentNodeID)
	require.Equal(t, []string{notePath}, model.NotePaths)
	require.Len(t, model.FieldValues, 2)
	require.Equal(t, []string{"done", "assignee"}, []string{model.FieldValues[0].FieldName, model.FieldValues[1].FieldName})
	require.Equal(t, "true", model.FieldValues[0].ValueNorm)
	require.Equal(t, "people/Alice.md", model.FieldValues[1].TargetNotePath)
	require.Len(t, model.LinkDependencies, 1)
	require.Equal(t, model.Nodes[1].NodeID, model.LinkDependencies[0].NodeID)
	require.Equal(t, "people/Alice", model.LinkDependencies[0].TargetInput)
	require.Equal(t, "people/Alice.md", model.LinkDependencies[0].ResolvedTargetNotePath)
}

func TestBuildIntelOntologyNodeReadModel_UntypedNoteEmitsFallbackSections(t *testing.T) {
	root := t.TempDir()
	writeOntologyTestConfig(t, root)
	writeOntologySchema(t, root, `
type Spec @node(paths: ["specs/*.md"]) {
  summary: String @field
}
`)
	notePath := "notes/research.md"
	content := `# Research

Root fallback prose.

## Findings

Section fallback prose.

### Detail

Nested fallback prose.
`
	writeOntologyNote(t, root, notePath, content)

	schema, err := LoadSchema(root)
	require.NoError(t, err)
	snapshot, err := BuildDocumentSnapshot(notePath, content, time.Time{})
	require.NoError(t, err)
	projection, err := ProjectNodeFromSnapshot(snapshot, schema, NodeRef{NotePath: notePath, Kind: NodeKindNote})
	require.NoError(t, err)

	model, err := BuildIntelOntologyNodeReadModel(schema, projection, 1)
	require.NoError(t, err)

	var noteRows, sectionRows []codeanchor.IntelOntologyNode
	for _, node := range model.Nodes {
		switch node.TypeName {
		case FallbackNoteTypeName:
			noteRows = append(noteRows, node)
		case FallbackSectionTypeName:
			sectionRows = append(sectionRows, node)
		}
	}
	require.Len(t, noteRows, 1)
	require.Len(t, sectionRows, 2)
	require.Equal(t, "NOTE", noteRows[0].NodeKind)
	for _, section := range sectionRows {
		require.Equal(t, "SECTION", section.NodeKind)
		if section.Title == "Detail" {
			require.NotEmpty(t, section.ParentNodeID)
		}
		require.Contains(t, section.SourceLocator, notePath+"#")
		require.Greater(t, section.EndByte, section.StartByte)
		require.NotEmpty(t, section.StructuralFingerprint)
	}
}

func TestBuildIntelOntologyNodeReadModel_CatalogRowsPreserveCanonicalIdentity(t *testing.T) {
	root := t.TempDir()
	writeOntologyTestConfig(t, root)
	writeOntologySchema(t, root, `
type Spec @node(paths: ["specs/*.md"]) {
  status: String @field
  stories: StoriesSection @contains(level: H2, heading: "Stories")
}

type StoriesSection implements Section {
  items: [UserStory!] @contains(level: H3)
}

type UserStory implements Section @node(locator: EMBEDDED) {
  storyId: String @field
}
`)
	notePath := "specs/product.md"
	content := `---
status: active
---
# Product

## Stories

### Story A
storyId:: US-1
Body.
`
	writeOntologyNote(t, root, notePath, content)

	schema, err := LoadSchema(root)
	require.NoError(t, err)
	snapshot, err := BuildDocumentSnapshot(notePath, content, time.Time{})
	require.NoError(t, err)
	projection, err := ProjectNodeFromSnapshot(snapshot, schema, NodeRef{NotePath: notePath, Kind: NodeKindNote})
	require.NoError(t, err)

	model, err := BuildIntelOntologyNodeReadModel(schema, projection, 99)
	require.NoError(t, err)

	var rootNode, storyNode codeanchor.IntelOntologyNode
	for _, node := range model.Nodes {
		switch node.TypeName {
		case "Spec":
			rootNode = node
		case "UserStory":
			storyNode = node
		}
	}
	require.NotEmpty(t, rootNode.NodeID)
	require.Equal(t, notePath, rootNode.NotePath)
	require.Contains(t, rootNode.NodeRefJSON, `"typeName":"Spec"`)
	require.Equal(t, "NOTE", rootNode.NodeKind)
	require.Equal(t, "Spec", rootNode.TypeName)
	require.Empty(t, rootNode.ParentNodeID)
	require.Equal(t, notePath, rootNode.SourceLocator)
	require.NotEmpty(t, rootNode.SchemaHash)
	require.NotZero(t, rootNode.EndByte)
	require.Equal(t, int64(99), rootNode.UpdatedAt)

	require.NotEmpty(t, storyNode.NodeID)
	require.Equal(t, notePath, storyNode.NotePath)
	require.Contains(t, storyNode.NodeRefJSON, `"typeName":"UserStory"`)
	require.Equal(t, "EMBEDDED", storyNode.NodeKind)
	require.Equal(t, "UserStory", storyNode.TypeName)
	require.NotEmpty(t, storyNode.ParentNodeID)
	require.NotEqual(t, rootNode.NodeID, storyNode.NodeID)
	require.Contains(t, storyNode.SourceLocator, notePath+"#")
	require.NotZero(t, storyNode.StartByte)
	require.NotZero(t, storyNode.EndByte)
	require.Equal(t, rootNode.SchemaHash, storyNode.SchemaHash)
}

func TestPlanOntologySync_ExpandsChangedTargetsThroughLinkDependencies(t *testing.T) {
	ctx := context.Background()
	store, err := semdb.Open(filepath.Join(t.TempDir(), "ontology-link-deps-plan.db"))
	require.NoError(t, err)
	defer func() { _ = store.Close() }()

	source := codeanchor.IntelOntologyNode{NodeID: "node:source", NotePath: "docs/source.md", NodeRefJSON: "{}", NodeKind: "NOTE", TypeName: "ActionItem", UpdatedAt: 1}
	require.NoError(t, store.ReplaceOntologyNodeReadModel(ctx, codeanchor.IntelOntologyNodeReadModel{
		NotePaths: []string{source.NotePath},
		Nodes:     []codeanchor.IntelOntologyNode{source},
		LinkDependencies: []codeanchor.IntelOntologyNodeLinkDependency{
			{
				SourceNotePath:         source.NotePath,
				NodeID:                 source.NodeID,
				TypeName:               source.TypeName,
				FieldName:              "assignee",
				TargetInput:            "ALICE",
				TargetInputNorm:        "alice",
				ResolvedTargetNotePath: "people/alice.md",
				ResolvedTargetTypeName: "Person",
				UpdatedAt:              1,
			},
		},
	}))

	plan, err := planOntologySync(ctx, store, []string{"people/alice.md"}, nil)
	require.NoError(t, err)
	require.Contains(t, plan.LoadPaths, "people/alice.md")
	require.Contains(t, plan.LoadPaths, source.NotePath)
	require.Contains(t, plan.ReplacePaths, source.NotePath)
	require.Contains(t, plan.ForcePaths, source.NotePath)
}

// Regression: when a target note is deleted (or renamed away), dependents
// whose link was unresolved at index time must still be re-evaluated. The
// dependency row only carries target_input_norm in that case, and prior to
// the fix planOntologySync only fed changedPaths into target-norm planning
// so a Spec referencing `[[alice]]` while alice.md disappears was missed.
func TestPlanOntologySync_ExpandsDeletedTargetsThroughLinkDependencies(t *testing.T) {
	ctx := context.Background()
	store, err := semdb.Open(filepath.Join(t.TempDir(), "ontology-link-deps-plan-deleted.db"))
	require.NoError(t, err)
	defer func() { _ = store.Close() }()

	source := codeanchor.IntelOntologyNode{NodeID: "node:source", NotePath: "docs/source.md", NodeRefJSON: "{}", NodeKind: "NOTE", TypeName: "ActionItem", UpdatedAt: 1}
	require.NoError(t, store.ReplaceOntologyNodeReadModel(ctx, codeanchor.IntelOntologyNodeReadModel{
		NotePaths: []string{source.NotePath},
		Nodes:     []codeanchor.IntelOntologyNode{source},
		LinkDependencies: []codeanchor.IntelOntologyNodeLinkDependency{
			{
				SourceNotePath:  source.NotePath,
				NodeID:          source.NodeID,
				TypeName:        source.TypeName,
				FieldName:       "assignee",
				TargetInput:     "alice",
				TargetInputNorm: "alice",
				// ResolvedTarget* intentionally blank: the link was unresolved
				// when indexed, so only target_input_norm bridges to dependents.
				UpdatedAt: 1,
			},
		},
	}))

	plan, err := planOntologySync(ctx, store, nil, []string{"people/alice.md"})
	require.NoError(t, err)
	require.Contains(t, plan.LoadPaths, source.NotePath)
	require.Contains(t, plan.ReplacePaths, source.NotePath)
	require.Contains(t, plan.ForcePaths, source.NotePath)
	require.NotContains(t, plan.LoadPaths, "people/alice.md")
	require.Contains(t, plan.DeletePaths, "people/alice.md")
}
