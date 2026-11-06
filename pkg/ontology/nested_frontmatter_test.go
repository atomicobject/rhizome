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

func TestNestedFrontmatterProjectionAndReadOnlyEdit(t *testing.T) {
	root := t.TempDir()
	writeOntologyTestConfig(t, root)
	writeOntologySchema(t, root, `type Assertion @node(paths: ["notes/*.md"]) {
 speaker: String @field(path: "provenance.speaker")
 literal: String @field(source: "provenance.speaker")
 }`)
	const content = "---\ntype: Assertion\nprovenance.speaker: Literal\nprovenance:\n  speaker: Nested\n---\n# Assertion\n"
	writeOntologyNote(t, root, "notes/one.md", content)
	schema, err := ontoschema.LoadSchema(root)
	require.NoError(t, err)
	vault := obsidian.VaultDefinition{Path: root}
	ref := NodeRef{NotePath: "notes/one.md", Kind: NodeKindNote}
	projection, err := ProjectNode(context.Background(), vault, &obsidian.Note{}, schema, ref)
	require.NoError(t, err)
	require.Equal(t, []string{"Nested"}, projection.Fields["speaker"].Values)
	require.Equal(t, []string{"Literal"}, projection.Fields["literal"].Values)
	capability := buildNodeFieldCapability(schema, projection, schema.Types["Assertion"].ByName["speaker"], projection.Fields["speaker"])
	require.Equal(t, "Nested frontmatter paths are read-only.", capability.ReadOnlyReason)
	require.Empty(t, capability.WriteOperation)
	require.False(t, projection.Fields["speaker"].ValueRangesExact)
	session := NewEditSession(vault, &obsidian.Note{}, schema)
	require.NoError(t, session.SetScalarField(ref, "speaker", "Changed"))
	_, conflicts, err := session.PreviewCurrent(context.Background())
	require.True(t, err != nil || len(conflicts) > 0)
	result, err := session.Commit(context.Background())
	require.NoError(t, err)
	require.Equal(t, CommitOutcomeConflicted, result.Outcome)
	require.Contains(t, result.Conflicts[0].Message, "read-only")
	bytes, err := os.ReadFile(filepath.Join(root, "notes/one.md"))
	require.NoError(t, err)
	require.Equal(t, content, string(bytes))
}
