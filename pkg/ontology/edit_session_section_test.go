package ontology

import (
	"context"
	"strings"
	"testing"

	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"
)

func TestEditSessionAddSectionFieldUsesSchemaOrder(t *testing.T) {
	root := t.TempDir()
	writeOntologyTestConfig(t, root)
	writeOntologySchema(t, root, `
type EmptySection implements Section {}

type Spec @node(paths: ["specs/*.md"]) {
  summary: EmptySection @contains(level: H2, heading: "Summary")
  requirements: EmptySection @contains(level: H2, heading: "Requirements", required: true)
  status: EmptySection @contains(level: H2, heading: "Status")
}
`)
	writeOntologyNote(t, root, "specs/one.md", `---
type: Spec
---

# One

## Summary

Existing summary.

## Status

Draft.
`)

	schema, err := LoadSchema(root)
	require.NoError(t, err)
	session := NewEditSession(obsidian.VaultDefinition{Path: root}, &obsidian.Note{}, schema)
	require.NoError(t, session.AddSectionField(NodeRef{NotePath: "specs/one.md", Kind: NodeKindNote}, "requirements"))
	plan, conflicts, err := session.PreviewCurrent(context.Background())
	require.NoError(t, err)
	require.Empty(t, conflicts)
	require.Len(t, plan.Files, 1)
	updated := plan.Files[0].UpdatedContentPreview
	previous := -1
	for _, heading := range []string{"## Summary", "## Requirements", "## Status"} {
		position := strings.Index(updated, "\n"+heading+"\n")
		require.GreaterOrEqual(t, position, 0, "missing heading %q", heading)
		require.Greater(t, position, previous, "heading %q is out of order", heading)
		previous = position
	}
}

func TestEditSessionAddSectionFieldUsesResolvedNestedParentAndIsIdempotent(t *testing.T) {
	root := t.TempDir()
	writeOntologyTestConfig(t, root)
	writeOntologySchema(t, root, `
type DetailsSection implements Section {}

type RequirementsSection implements Section {
  context: DetailsSection @contains(level: H3, heading: "Context")
  details: DetailsSection @contains(level: H3, heading: "Details", required: true)
  evidence: DetailsSection @contains(level: H3, heading: "Evidence")
}

type Spec @node(paths: ["specs/*.md"]) {
  requirements: RequirementsSection @contains(level: H2, heading: "Requirements", required: true)
  status: DetailsSection @contains(level: H2, heading: "Status")
}
`)
	writeOntologyNote(t, root, "specs/one.md", `---
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
`)

	schema, err := LoadSchema(root)
	require.NoError(t, err)
	ref := NodeRef{NotePath: "specs/one.md", Kind: NodeKindNote}
	session := NewEditSession(obsidian.VaultDefinition{Path: root}, &obsidian.Note{}, schema)
	require.NoError(t, session.AddSectionField(ref, "requirements.details"))
	plan, conflicts, err := session.PreviewCurrent(context.Background())
	require.NoError(t, err)
	require.Empty(t, conflicts)
	require.Len(t, plan.Files, 1)
	updated := plan.Files[0].UpdatedContentPreview
	previous := -1
	for _, heading := range []string{"## Requirements", "### Context", "### Details", "### Evidence", "## Status"} {
		position := strings.Index(updated, "\n"+heading+"\n")
		require.GreaterOrEqual(t, position, 0, "missing heading %q", heading)
		require.Greater(t, position, previous, "heading %q is out of order", heading)
		previous = position
	}

	writeOntologyNote(t, root, "specs/one.md", updated)
	idempotent := NewEditSession(obsidian.VaultDefinition{Path: root}, &obsidian.Note{}, schema)
	require.NoError(t, idempotent.AddSectionField(ref, "requirements.details"))
	second, secondConflicts, err := idempotent.PreviewCurrent(context.Background())
	require.NoError(t, err)
	require.Empty(t, secondConflicts)
	require.Len(t, second.Files, 1)
	require.False(t, second.Files[0].HasMaterialChange)
}

func TestEditSessionAddSectionFieldConflictPreservesRefAndFieldPath(t *testing.T) {
	root := t.TempDir()
	writeOntologyTestConfig(t, root)
	writeOntologySchema(t, root, `
type Leaf implements Section {}

type Spec @node(paths: ["specs/*.md"]) {
  status: Leaf @contains(level: H2, heading: "Status")
}
`)
	writeOntologyNote(t, root, "specs/one.md", `---
type: Spec
---

# One
`)

	schema, err := LoadSchema(root)
	require.NoError(t, err)
	ref := NodeRef{NotePath: "specs/one.md", Kind: NodeKindNote}
	session := NewEditSession(obsidian.VaultDefinition{Path: root}, &obsidian.Note{}, schema)
	require.NoError(t, session.AddSectionField(ref, "missing.details"))

	_, conflicts, err := session.PreviewCurrent(context.Background())
	require.NoError(t, err)
	require.Len(t, conflicts, 1)
	require.Equal(t, ConflictKindMissingField, conflicts[0].Kind)
	require.Equal(t, ref.String(), conflicts[0].NodeRef)
	require.Equal(t, "missing.details", conflicts[0].Field)
}
