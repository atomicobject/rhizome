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

func TestEditSession_PreviewCurrent_SeparatesBaseCurrentAndUpdatedFingerprints(t *testing.T) {
	root := t.TempDir()
	writeOntologyTestConfig(t, root)
	writeOntologySchema(t, root, embeddedProjectionSchema)
	writeOntologyNote(t, root, "specs/001-test/spec.md", `---
type: Spec
summary: Test
---

# Spec
`)

	schema, err := ontoschema.LoadSchema(root)
	require.NoError(t, err)

	session := NewEditSession(obsidian.VaultDefinition{Path: root}, &obsidian.Note{}, schema)
	require.NoError(t, session.SetScalarField(NodeRef{
		NotePath: "specs/001-test/spec.md",
		Kind:     NodeKindNote,
	}, "summary", "Better test"))
	_, err = session.Preview(context.Background())
	require.NoError(t, err)

	path := filepath.Join(root, "specs/001-test/spec.md")
	baseContent, err := os.ReadFile(path)
	require.NoError(t, err)
	currentContent := strings.Replace(string(baseContent), "# Spec", "# External Spec", 1)
	require.NoError(t, os.WriteFile(path, []byte(currentContent), 0o644))

	plan, conflicts, err := session.PreviewCurrent(context.Background())
	require.NoError(t, err)
	require.Empty(t, conflicts)
	require.Len(t, plan.Files, 1)
	file := plan.Files[0]

	require.Equal(t, hashText(string(baseContent)), file.BaseFingerprint)
	require.Equal(t, hashText(currentContent), file.CurrentFingerprint)
	require.Equal(t, hashText(file.UpdatedContentPreview), file.UpdatedFingerprint)
	require.NotEqual(t, file.BaseFingerprint, file.CurrentFingerprint)
	require.NotEqual(t, file.CurrentFingerprint, file.UpdatedFingerprint)
	require.True(t, file.Rebased)
	require.True(t, file.HasMaterialChange)
	require.Contains(t, file.UpdatedContentPreview, "summary: Better test")
	require.Contains(t, file.UpdatedContentPreview, "# External Spec")
}

func TestEditSession_Commit_AutoRebasesSimpleExternalChange(t *testing.T) {
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
		Kind:     NodeKindNote,
	}, "summary", "Updated summary"))

	_, err = session.Preview(context.Background())
	require.NoError(t, err)

	path := filepath.Join(root, "specs/001-test/spec.md")
	original, err := os.ReadFile(path)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(path, append(original, []byte("\nExternal tail.\n")...), 0o644))

	result, err := session.Commit(context.Background())
	require.NoError(t, err)
	require.True(t, result.Applied)
	require.True(t, result.Rebased)

	updated, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Contains(t, string(updated), "summary: Updated summary")
	require.Contains(t, string(updated), "External tail.")
}

func TestEditSession_SetScalarAndLinkFields_CommitPreservesBody(t *testing.T) {
	root := t.TempDir()
	writeOntologyTestConfig(t, root)
	writeOntologySchema(t, root, projectionBindingSchema)
	writeOntologyNote(t, root, "notes/specs/spec.md", `---
type: Spec
summary: Spec summary
decisions:
  - notes/decisions/alpha.md
---

Intro paragraph.

## User Stories

### Story A
status:: TODO
^story-a

Closing paragraph.
`)

	schema, err := ontoschema.LoadSchema(root)
	require.NoError(t, err)

	session := NewEditSession(obsidian.VaultDefinition{Path: root}, &obsidian.Note{}, schema)
	require.NoError(t, session.SetScalarField(NodeRef{
		NotePath: "notes/specs/spec.md",
		Kind:     NodeKindNote,
	}, "summary", "Updated summary"))
	require.NoError(t, session.SetLinkField(NodeRef{
		NotePath: "notes/specs/spec.md",
		Kind:     NodeKindNote,
	}, "decisions", []string{"notes/decisions/beta.md", "notes/decisions/gamma.md"}))

	result, err := session.Commit(context.Background())
	require.NoError(t, err)
	require.True(t, result.Applied)

	data, err := os.ReadFile(filepath.Join(root, "notes/specs/spec.md"))
	require.NoError(t, err)
	text := string(data)
	require.Contains(t, text, "summary: Updated summary")
	require.Contains(t, text, "- notes/decisions/beta.md")
	require.Contains(t, text, "- notes/decisions/gamma.md")
	require.Contains(t, text, "Intro paragraph.\n\n## User Stories")
	require.Contains(t, text, "Closing paragraph.")
}

func TestEditSession_Preview_IncludesDiffAndFingerprints(t *testing.T) {
	root := t.TempDir()
	writeOntologyTestConfig(t, root)
	writeOntologySchema(t, root, embeddedProjectionSchema)
	writeOntologyNote(t, root, "specs/001-test/spec.md", `---
type: Spec
summary: Test
---

# Spec
`)

	schema, err := ontoschema.LoadSchema(root)
	require.NoError(t, err)

	session := NewEditSession(obsidian.VaultDefinition{Path: root}, &obsidian.Note{}, schema)
	require.NoError(t, session.SetScalarField(NodeRef{
		NotePath: "specs/001-test/spec.md",
		Kind:     NodeKindNote,
	}, "summary", "Better test"))

	plan, err := session.Preview(context.Background())
	require.NoError(t, err)
	require.Len(t, plan.Files, 1)
	file := plan.Files[0]
	require.NotEmpty(t, file.BaseFingerprint)
	require.NotEmpty(t, file.UpdatedFingerprint)
	require.NotEqual(t, file.BaseFingerprint, file.UpdatedFingerprint)
	require.True(t, file.HasMaterialChange)
	require.Contains(t, file.UpdatedContentPreview, "summary: Better test")
	require.Contains(t, file.Diff, "Better test")
}

func TestEditSession_Commit_UpdatesMultipleFiles(t *testing.T) {
	root := t.TempDir()
	writeOntologyTestConfig(t, root)
	writeOntologySchema(t, root, `
type Spec @node(paths: ["notes/specs/*.md"]) {
  summary: String!
}
`)
	writeOntologyNote(t, root, "notes/specs/one.md", `---
type: Spec
summary: One
---
`)
	writeOntologyNote(t, root, "notes/specs/two.md", `---
type: Spec
summary: Two
---
`)

	schema, err := ontoschema.LoadSchema(root)
	require.NoError(t, err)

	session := NewEditSession(obsidian.VaultDefinition{Path: root}, &obsidian.Note{}, schema)
	require.NoError(t, session.SetScalarField(NodeRef{NotePath: "notes/specs/one.md", Kind: NodeKindNote}, "summary", "One updated"))
	require.NoError(t, session.SetScalarField(NodeRef{NotePath: "notes/specs/two.md", Kind: NodeKindNote}, "summary", "Two updated"))

	result, err := session.Commit(context.Background())
	require.NoError(t, err)
	require.True(t, result.Applied)
	require.Len(t, result.Plan.Files, 2)

	one, err := os.ReadFile(filepath.Join(root, "notes/specs/one.md"))
	require.NoError(t, err)
	two, err := os.ReadFile(filepath.Join(root, "notes/specs/two.md"))
	require.NoError(t, err)
	require.Contains(t, string(one), "summary: One updated")
	require.Contains(t, string(two), "summary: Two updated")
}

func TestEditSession_PreviewOrdersFilesDeterministically(t *testing.T) {
	root := t.TempDir()
	writeOntologyTestConfig(t, root)
	writeOntologySchema(t, root, `
type Spec @node(paths: ["notes/specs/*.md"]) {
  summary: String!
}
`)
	for _, name := range []string{"a", "b", "c"} {
		writeOntologyNote(t, root, "notes/specs/"+name+".md", `---
type: Spec
summary: `+strings.ToUpper(name)+`
---
`)
	}

	schema, err := ontoschema.LoadSchema(root)
	require.NoError(t, err)

	session := NewEditSession(obsidian.VaultDefinition{Path: root}, &obsidian.Note{}, schema)
	require.NoError(t, session.SetScalarField(NodeRef{NotePath: "notes/specs/c.md", Kind: NodeKindNote}, "summary", "C updated"))
	require.NoError(t, session.SetScalarField(NodeRef{NotePath: "notes/specs/a.md", Kind: NodeKindNote}, "summary", "A updated"))
	require.NoError(t, session.SetScalarField(NodeRef{NotePath: "notes/specs/b.md", Kind: NodeKindNote}, "summary", "B updated"))

	plan, err := session.Preview(context.Background())
	require.NoError(t, err)
	require.Equal(t, []string{
		"notes/specs/a.md",
		"notes/specs/b.md",
		"notes/specs/c.md",
	}, []string{
		plan.Files[0].NotePath,
		plan.Files[1].NotePath,
		plan.Files[2].NotePath,
	})
}

func TestEditSession_CommitWithOptionsCanReturnSlimPlan(t *testing.T) {
	root := t.TempDir()
	writeOntologyTestConfig(t, root)
	writeOntologySchema(t, root, embeddedProjectionSchema)
	writeOntologyNote(t, root, "specs/001-test/spec.md", `---
type: Spec
summary: Test
---

# Spec
`)

	schema, err := ontoschema.LoadSchema(root)
	require.NoError(t, err)

	session := NewEditSession(obsidian.VaultDefinition{Path: root}, &obsidian.Note{}, schema)
	require.NoError(t, session.SetScalarField(NodeRef{
		NotePath: "specs/001-test/spec.md",
		Kind:     NodeKindNote,
	}, "summary", "Saved"))

	result, err := session.CommitWithOptions(context.Background(), CommitOptions{})
	require.NoError(t, err)
	require.True(t, result.Applied)
	require.Len(t, result.Plan.Files, 1)
	require.Empty(t, result.Plan.Files[0].Diff)
	require.Empty(t, result.Plan.Files[0].UpdatedContentPreview)
	require.NotEmpty(t, result.Plan.Files[0].UpdatedFingerprint)

	data, err := os.ReadFile(filepath.Join(root, "specs/001-test/spec.md"))
	require.NoError(t, err)
	require.Contains(t, string(data), "summary: Saved")
}
