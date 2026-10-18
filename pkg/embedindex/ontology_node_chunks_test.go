package embedindex

import (
	"context"
	"github.com/atomicobject/rhizome/pkg/embeddings"
	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/ontology/readmodel"
	ontoschema "github.com/atomicobject/rhizome/pkg/ontology/schema"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBuildOntologyNodeChunks_IncludesEmbeddedNodeAndParentContext(t *testing.T) {
	root := t.TempDir()
	schemaDir := filepath.Join(root, ".rhizome", "ontology")
	require.NoError(t, os.MkdirAll(schemaDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(schemaDir, "schema.graphql"), []byte(`
type Spec @node(paths: ["notes/specs/*.md"]) {
  status: String @field
  userStories: UserStoriesSection @contains(level: H2, heading: "User Stories")
}

type UserStoriesSection implements Section {
  stories: [UserStory!] @contains(level: H3)
}

type UserStory implements Section @node(locator: EMBEDDED) {
  storyId: String @field
}
`), 0o644))
	notePath := filepath.Join(root, "notes", "specs", "checkout.md")
	require.NoError(t, os.MkdirAll(filepath.Dir(notePath), 0o755))
	content := `---
status: draft
---
# Checkout Refresh

Intro belongs to the root spec.

` + strings.Repeat("Root context must remain searchable. ", 500) + `

## User Stories

### Faster checkout
storyId:: US-1
Users need quicker checkout.
`
	require.NoError(t, os.WriteFile(notePath, []byte(content), 0o644))

	schema, err := ontoschema.LoadSchema(root)
	require.NoError(t, err)
	projection, err := ontology.ProjectNote(context.Background(), obsidian.VaultDefinition{Path: root}, &obsidian.Note{}, schema, "notes/specs/checkout.md")
	require.NoError(t, err)

	set, err := BuildOntologyNodeChunks(schema, projection, embeddings.ProviderConfig{Provider: "test", Model: "deterministic"}, 123)
	require.NoError(t, err)
	require.Contains(t, set.NotePaths, "notes/specs/checkout.md")

	var storyNodeID string
	var rootNodeID string
	var containerNodeID string
	var rootBody string
	var containerBody string
	var storyBody string
	for _, node := range set.Nodes {
		if node.TypeName == "Spec" {
			rootNodeID = node.NodeID
		}
		if node.TypeName == "UserStoriesSection" {
			containerNodeID = node.NodeID
		}
		if node.TypeName == "UserStory" {
			storyNodeID = node.NodeID
			require.Equal(t, "EMBEDDED", node.NodeKind)
			require.NotEmpty(t, node.ParentNodeID)
			require.Contains(t, node.NodeRefJSON, `"typeName":"UserStory"`)
		}
	}
	require.NotEmpty(t, rootNodeID)
	require.NotEmpty(t, containerNodeID)
	require.NotEmpty(t, storyNodeID)
	require.Equal(t, "draft", ontologyNodeFieldValue(t, set.FieldValues, rootNodeID, "status").ValueNorm)
	for _, chunk := range set.Chunks {
		if chunk.OwnerID == rootNodeID && chunk.Granularity == embeddings.GranularityOntologyNodeBody && rootBody == "" {
			rootBody = set.Texts[chunk.ChunkID]
		}
		if chunk.OwnerID == containerNodeID && chunk.Granularity == embeddings.GranularityOntologyNodeBody {
			containerBody = set.Texts[chunk.ChunkID]
		}
		if chunk.OwnerID == storyNodeID && chunk.Granularity == embeddings.GranularityOntologyNodeBody {
			storyBody = set.Texts[chunk.ChunkID]
		}
	}
	require.Contains(t, rootBody, "- status: draft")
	require.Contains(t, storyBody, "Type: UserStory")
	require.Contains(t, storyBody, "Ancestors: Spec > checkout > userStories / UserStoriesSection > User Stories > stories")
	require.Contains(t, storyBody, "Breadcrumb: notes/specs/checkout.md > UserStory > Faster checkout")
	require.Contains(t, storyBody, "storyId:: US-1")
	require.Contains(t, rootBody, "Intro belongs to the root spec.")
	require.NotContains(t, rootBody, "storyId:: US-1")
	require.NotContains(t, rootBody, "Users need quicker checkout.")
	require.NotEmpty(t, containerBody, "bodyless organizing sections retain an identity-only primary chunk")
	for _, chunk := range set.Chunks {
		require.Equal(t, embeddings.GranularityOntologyNodeBody, chunk.Granularity)
		if chunk.OwnerID == storyNodeID {
			require.Equal(t, 0, chunk.Ord)
		}
	}

	var storyState codeState
	for _, state := range set.States {
		if state.NodeID == storyNodeID && state.ChunkGranularity == embeddings.GranularityOntologyNodeBody {
			storyState = codeState{
				schemaSignature: state.EmbeddingSchemaSignature,
				textHash:        state.ChunkTextHash,
			}
		}
	}
	require.NotEmpty(t, storyState.schemaSignature)
	require.NotEmpty(t, storyState.textHash)
	require.False(t, strings.Contains(storyState.schemaSignature, "Spec|"))
	// Field ranking is observed through the projected chunk, including its
	// continuation, rather than by calling the renderer in isolation.
	fields := []*ontoschema.Field{
		{Name: "optionalZ", Kind: ontoschema.FieldKindScalar},
		{Name: "status", Kind: ontoschema.FieldKindEnum, TypeName: "Status"},
		{Name: "required", Kind: ontoschema.FieldKindScalar, Required: true},
		{Name: "context", Kind: ontoschema.FieldKindScalar, ContextInclude: true},
		{Name: "identifier", Kind: ontoschema.FieldKindScalar, IsIdentifier: true},
		{Name: "id", Kind: ontoschema.FieldKindScalar, IsIdentifier: true, IsPreferredIdentifier: true},
		{Name: "optionalA", Kind: ontoschema.FieldKindScalar},
		{Name: "identity", Kind: ontoschema.FieldKindScalar},
		{Name: "relation", Kind: ontoschema.FieldKindLink},
	}
	byName := make(map[string]*ontoschema.Field, len(fields))
	bindings := make(map[string]ontology.FieldBinding, len(fields))
	for _, field := range fields {
		byName[field.Name] = field
		bindings[field.Name] = ontology.FieldBinding{Present: true, Values: []string{field.Name + "-3", field.Name + "-1", field.Name + "-2"}}
	}
	bindings["identity"] = ontology.FieldBinding{Present: true, Values: []string{"checkout"}}
	projection.Type.Fields = append(projection.Type.Fields, fields...)
	for name, field := range byName {
		projection.Type.ByName[name] = field
	}
	for name, binding := range bindings {
		projection.Fields[name] = binding
	}
	if schema.EnumTypes == nil {
		schema.EnumTypes = map[string]*ontoschema.EnumType{}
	}
	schema.EnumTypes["Status"] = &ontoschema.EnumType{Values: []*ontoschema.EnumValue{{Name: "ACTIVE"}, {Name: "DRAFT"}}}
	enriched, err := BuildOntologyNodeChunks(schema, projection, embeddings.ProviderConfig{Provider: "test", Model: "deterministic"}, 123)
	require.NoError(t, err)
	var rootParts []string
	for _, chunk := range enriched.Chunks {
		if chunk.OwnerID == rootNodeID && chunk.Granularity == embeddings.GranularityOntologyNodeBody {
			rootParts = append(rootParts, enriched.Texts[chunk.ChunkID])
		}
	}
	require.Greater(t, len(rootParts), 1, "long root must produce continuation chunks")
	first := rootParts[0]
	for _, label := range []string{"id:", "identifier:", "context:", "status [ACTIVE|DRAFT]:", "required:", "optionalA:"} {
		require.Contains(t, first, label)
	}
	require.NotContains(t, first, "optionalZ:")
	require.NotContains(t, first, "identity:")
	require.NotContains(t, first, "relation:")
	require.NotContains(t, first, "-3")
	ordered := []string{"id:", "identifier:", "context:", "status [ACTIVE|DRAFT]:", "required:", "optionalA:"}
	for i := 1; i < len(ordered); i++ {
		require.Less(t, strings.Index(first, ordered[i-1]), strings.Index(first, ordered[i]), "field priority %s before %s", ordered[i-1], ordered[i])
	}
	for _, part := range rootParts[1:] {
		require.NotContains(t, part, "Fields:")
		require.Contains(t, part, "Ref: notes/specs/checkout.md")
	}

}

func ontologyNodeFieldValue(t *testing.T, rows []readmodel.FieldValueRow, nodeID, fieldName string) readmodel.FieldValueRow {
	t.Helper()
	for _, row := range rows {
		if row.NodeID == nodeID && row.FieldName == fieldName {
			return row
		}
	}
	t.Fatalf("expected field value %s.%s", nodeID, fieldName)
	return readmodel.FieldValueRow{}
}

func TestBuildOntologyNodeChunks_FallbackUntypedNoteIncludesSectionProse(t *testing.T) {
	root := t.TempDir()
	schemaDir := filepath.Join(root, ".rhizome", "ontology")
	require.NoError(t, os.MkdirAll(schemaDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(schemaDir, "schema.graphql"), []byte(`
type Spec @node(paths: ["docs/specs/*.md"]) {
  summary: String @field
}
`), 0o644))
	notePath := filepath.Join(root, "notes", "untyped.md")
	require.NoError(t, os.MkdirAll(filepath.Dir(notePath), 0o755))
	require.NoError(t, os.WriteFile(notePath, []byte(`# Untyped Research

Root prose should be searchable.

## Findings

The frobnicator requires a pressure valve.

### Detail

Nested section text must survive fallback chunking.
`), 0o644))

	schema, err := ontoschema.LoadSchema(root)
	require.NoError(t, err)
	projection, err := ontology.ProjectNote(context.Background(), obsidian.VaultDefinition{Path: root}, &obsidian.Note{}, schema, "notes/untyped.md")
	require.NoError(t, err)
	require.Empty(t, projection.ResolvedType)
	require.Nil(t, projection.Assessment)

	set, err := BuildOntologyNodeChunks(schema, projection, embeddings.ProviderConfig{Provider: "test", Model: "deterministic"}, 123)
	require.NoError(t, err)
	require.Len(t, set.Nodes, 3)
	var fallbackNodeID string
	var sectionNodeIDs []string
	for _, node := range set.Nodes {
		switch node.TypeName {
		case ontoschema.FallbackNoteTypeName:
			require.Equal(t, "NOTE", node.NodeKind)
			fallbackNodeID = node.NodeID
		case ontoschema.FallbackSectionTypeName:
			require.Equal(t, "SECTION", node.NodeKind)
			require.Contains(t, node.SourceLocator, "notes/untyped.md#")
			if strings.Contains(node.Title, "Detail") {
				require.NotEmpty(t, node.ParentNodeID)
			}
			sectionNodeIDs = append(sectionNodeIDs, node.NodeID)
		}
	}
	require.NotEmpty(t, fallbackNodeID)
	require.Len(t, sectionNodeIDs, 2)

	var combined string
	owners := map[string]struct{}{}
	for _, chunk := range set.Chunks {
		require.Equal(t, "ontology_node", chunk.OwnerType)
		owners[chunk.OwnerID] = struct{}{}
		if chunk.Granularity == embeddings.GranularityOntologyNodeBody {
			combined += "\n" + set.Texts[chunk.ChunkID]
		}
	}
	require.Contains(t, owners, fallbackNodeID)
	for _, nodeID := range sectionNodeIDs {
		require.Contains(t, owners, nodeID)
	}
	require.Contains(t, combined, "Root prose should be searchable.")
	require.Contains(t, combined, "Section: Findings")
	require.Contains(t, combined, "The frobnicator requires a pressure valve.")
	require.Contains(t, combined, "Section: Detail")
	require.Contains(t, combined, "Nested section text must survive fallback chunking.")
}

func TestBuildOntologyNodeChunks_DoesNotFallbackInvalidDeclaredType(t *testing.T) {
	root := t.TempDir()
	schemaDir := filepath.Join(root, ".rhizome", "ontology")
	require.NoError(t, os.MkdirAll(schemaDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(schemaDir, "schema.graphql"), []byte(`
type Spec @node(paths: ["docs/specs/*.md"]) {
  summary: String @field
}
`), 0o644))
	notePath := filepath.Join(root, "notes", "broken.md")
	require.NoError(t, os.MkdirAll(filepath.Dir(notePath), 0o755))
	require.NoError(t, os.WriteFile(notePath, []byte(`---
type: MissingType
---
# Broken

This should not become a fallback note.
`), 0o644))

	schema, err := ontoschema.LoadSchema(root)
	require.NoError(t, err)
	projection, err := ontology.ProjectNote(context.Background(), obsidian.VaultDefinition{Path: root}, &obsidian.Note{}, schema, "notes/broken.md")
	require.NoError(t, err)
	require.Empty(t, projection.ResolvedType)
	require.NotNil(t, projection.Assessment)

	set, err := BuildOntologyNodeChunks(schema, projection, embeddings.ProviderConfig{Provider: "test", Model: "deterministic"}, 123)
	require.NoError(t, err)
	require.Empty(t, set.Nodes)
	require.Empty(t, set.Chunks)
}
