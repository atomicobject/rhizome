package ontology

import (
	"context"
	ontoschema "github.com/atomicobject/rhizome/pkg/ontology/schema"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestEditSession_Commit_ReportsCollectionDriftConflict(t *testing.T) {
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
	require.NoError(t, session.ReorderCollection(containerRef, "stories", []string{"^story-b", "^story-a"}))
	_, err = session.Preview(context.Background())
	require.NoError(t, err)

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

### Story C
status:: TODO
^story-c
`)

	result, err := session.Commit(context.Background())
	require.NoError(t, err)
	require.False(t, result.Applied)
	require.Len(t, result.Conflicts, 1)
	require.Equal(t, ConflictKindCollectionDrift, result.Conflicts[0].Kind)
	require.Equal(t, "stories", result.Conflicts[0].Field)
}

func TestEditSession_Commit_ReportsMissingNodeConflict(t *testing.T) {
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
	require.NoError(t, session.SetScalarField(NodeRef{
		NotePath: "specs/001-test/spec.md",
		Fragment: "^story-a",
		Kind:     NodeKindEmbedded,
	}, "status", "DONE"))
	_, err = session.Preview(context.Background())
	require.NoError(t, err)

	writeOntologyNote(t, root, "specs/001-test/spec.md", `---
type: Spec
summary: Test
---

# Spec

## User Stories

### Story Renamed
status:: TODO
^story-renamed
`)

	result, err := session.Commit(context.Background())
	require.NoError(t, err)
	require.False(t, result.Applied)
	require.Len(t, result.Conflicts, 1)
	require.Equal(t, ConflictKindMissingNode, result.Conflicts[0].Kind)
	require.Equal(t, "specs/001-test/spec.md#^story-a", result.Conflicts[0].NodeRef)
}

func TestEditSession_Commit_ReportsCollectionOrderDriftConflict(t *testing.T) {
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
	require.NoError(t, session.ReorderCollection(containerRef, "stories", []string{"^story-b", "^story-a"}))
	_, err = session.Preview(context.Background())
	require.NoError(t, err)

	writeOntologyNote(t, root, "specs/001-test/spec.md", `---
type: Spec
summary: Test
---

# Spec

## User Stories

### Story B
status:: TODO
^story-b

### Story A
status:: TODO
^story-a
`)

	result, err := session.Commit(context.Background())
	require.NoError(t, err)
	require.False(t, result.Applied)
	require.Len(t, result.Conflicts, 1)
	require.Equal(t, ConflictKindCollectionDrift, result.Conflicts[0].Kind)
}
