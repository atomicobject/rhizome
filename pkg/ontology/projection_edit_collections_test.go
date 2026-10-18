package ontology

import (
	"context"
	ontoschema "github.com/atomicobject/rhizome/pkg/ontology/schema"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEditSession_AddReorderDeleteEmbeddedNodes(t *testing.T) {
	root := t.TempDir()
	writeOntologyTestConfig(t, root)
	writeOntologySchema(t, root, embeddedProjectionSchema)
	writeOntologyNote(t, root, "specs/001-test/spec.md", `---
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

	schema, err := ontoschema.LoadSchema(root)
	require.NoError(t, err)
	note, err := ProjectNote(context.Background(), obsidian.VaultDefinition{Path: root}, &obsidian.Note{}, schema, "specs/001-test/spec.md")
	require.NoError(t, err)
	containerRef := note.Fields["userStoriesSection"].SectionNodes[0]

	session := NewEditSession(obsidian.VaultDefinition{Path: root}, &obsidian.Note{}, schema)
	require.NoError(t, session.AddEmbeddedNode(containerRef, "stories", "Story C", "status:: TODO", "story-c"))
	require.NoError(t, session.ReorderCollection(containerRef, "stories", []string{"^story-b", "^story-a", "^story-c"}))
	require.NoError(t, session.DeleteNode(NodeRef{
		NotePath: "specs/001-test/spec.md",
		Fragment: "^story-a",
		Kind:     NodeKindEmbedded,
	}))

	result, err := session.Commit(context.Background())
	require.NoError(t, err)
	require.True(t, result.Applied)

	data, err := os.ReadFile(filepath.Join(root, "specs/001-test/spec.md"))
	require.NoError(t, err)
	text := string(data)
	require.NotContains(t, text, "^story-a")
	for _, marker := range []string{"^story-b", "^story-c"} {
		require.Contains(t, text, marker)
	}
	require.Less(t, strings.Index(text, "^story-b"), strings.Index(text, "^story-c"))
}

func TestEditSession_PathDerivedRefsAddReorderDeleteEmbeddedNodes(t *testing.T) {
	root := t.TempDir()
	writeOntologyTestConfig(t, root)
	writeOntologySchema(t, root, embeddedProjectionSchema)
	writeOntologyNote(t, root, "specs/001-test/spec.md", `---
type: Spec
summary: Test
---

# Spec

## User Stories

### Story A
status:: TODO
^story-a
`)

	schema, err := ontoschema.LoadSchema(root)
	require.NoError(t, err)
	session := NewEditSession(obsidian.VaultDefinition{Path: root}, &obsidian.Note{}, schema)
	containerRef := NodeRef{
		NotePath: "specs/001-test/spec.md",
		Fragment: "User Stories",
		Kind:     NodeKindSection,
	}
	require.NoError(t, session.AddEmbeddedNode(containerRef, "stories", "Story B", "status:: TODO", "story-b"))
	require.NoError(t, session.AddEmbeddedNode(containerRef, "stories", "Story C", "status:: TODO", "story-c"))
	require.NoError(t, session.ReorderCollection(containerRef, "stories", []string{"^story-c", "^story-a", "^story-b"}))
	require.NoError(t, session.DeleteNode(NodeRef{
		NotePath: "specs/001-test/spec.md",
		Fragment: "^story-a",
		Kind:     NodeKindEmbedded,
	}))

	result, err := session.Commit(context.Background())
	require.NoError(t, err)
	if !result.Applied {
		t.Fatalf("conflicts: %+v", result.Conflicts)
	}

	data, err := os.ReadFile(filepath.Join(root, "specs/001-test/spec.md"))
	require.NoError(t, err)
	text := string(data)
	require.NotContains(t, text, "^story-a")
	require.Contains(t, text, "^story-b")
	require.Contains(t, text, "^story-c")
	require.True(t, strings.Index(text, "^story-c") < strings.Index(text, "^story-b"))
}

func TestEditSession_ReorderCollection_PreservesInterstitialContent(t *testing.T) {
	root := t.TempDir()
	writeOntologyTestConfig(t, root)
	writeOntologySchema(t, root, embeddedProjectionSchema)
	writeOntologyNote(t, root, "specs/001-test/spec.md", `---
type: Spec
summary: Test
---

# Spec

## User Stories

### Story A
status:: TODO
^story-a

<!-- keep-with-a -->

### Story B
status:: TODO
^story-b

Paragraph between stories.

### Story C
status:: TODO
^story-c
`)

	schema, err := ontoschema.LoadSchema(root)
	require.NoError(t, err)

	note, err := ProjectNote(context.Background(), obsidian.VaultDefinition{Path: root}, &obsidian.Note{}, schema, "specs/001-test/spec.md")
	require.NoError(t, err)
	containerRef := note.Fields["userStoriesSection"].SectionNodes[0]

	session := NewEditSession(obsidian.VaultDefinition{Path: root}, &obsidian.Note{}, schema)
	require.NoError(t, session.ReorderCollection(containerRef, "stories", []string{"^story-c", "^story-a", "^story-b"}))

	result, err := session.Commit(context.Background())
	require.NoError(t, err)
	require.True(t, result.Applied)

	data, err := os.ReadFile(filepath.Join(root, "specs/001-test/spec.md"))
	require.NoError(t, err)
	text := string(data)
	ordered := []string{"### Story C", "### Story A", "<!-- keep-with-a -->", "### Story B", "Paragraph between stories."}
	previous := -1
	for _, marker := range ordered {
		index := strings.Index(text, marker)
		require.GreaterOrEqual(t, index, 0, "missing %q", marker)
		require.Greater(t, index, previous, "out of order: %q", marker)
		previous = index
	}
}
