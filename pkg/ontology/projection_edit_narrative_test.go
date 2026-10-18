package ontology

import (
	"context"
	ontoschema "github.com/atomicobject/rhizome/pkg/ontology/schema"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"
	"os"
	"path/filepath"
	"testing"
)

func TestEditSession_SetNarrative_CommitRebasesByMatchingNarrativeBlock(t *testing.T) {
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
This story starts with narrative text.

status:: TODO
^story-a
`)

	schema, err := ontoschema.LoadSchema(root)
	require.NoError(t, err)

	storyRef := NodeRef{
		NotePath: "specs/001-test/spec.md",
		Fragment: "^story-a",
		Kind:     NodeKindEmbedded,
	}
	projection, err := ProjectNode(context.Background(), obsidian.VaultDefinition{Path: root}, &obsidian.Note{}, schema, storyRef)
	require.NoError(t, err)
	blocks := BuildNodeBody(projection, schema)
	require.NotEmpty(t, blocks)
	require.Equal(t, NodeBodyBlockKindNarrative, blocks[0].Kind)

	session := NewEditSession(obsidian.VaultDefinition{Path: root}, &obsidian.Note{}, schema)
	require.NoError(t, session.SetNarrative(
		storyRef,
		blocks[0].Range.Start,
		blocks[0].Range.End,
		blocks[0].Markdown,
		"Updated narrative text for Story A.",
	))
	_, err = session.Preview(context.Background())
	require.NoError(t, err)

	path := filepath.Join(root, "specs/001-test/spec.md")
	original, err := os.ReadFile(path)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(path, append([]byte("Intro line added before the note.\n\n"), original...), 0o644))

	result, err := session.Commit(context.Background())
	require.NoError(t, err)
	require.True(t, result.Applied)
	require.True(t, result.Rebased)

	updated, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Contains(t, string(updated), "Updated narrative text for Story A.")
	require.Contains(t, string(updated), "status:: TODO")
	require.Contains(t, string(updated), "Intro line added before the note.")
}
