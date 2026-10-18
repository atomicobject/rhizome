package ontology

import (
	"context"
	ontoschema "github.com/atomicobject/rhizome/pkg/ontology/schema"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"
)

func TestEditSessionNarrativeKeepsAdjacentInlineField(t *testing.T) {
	root := t.TempDir()
	writeOntologyTestConfig(t, root)
	writeOntologySchema(t, root, embeddedProjectionSchema)
	const notePath = "specs/001-test/spec.md"
	source := "---\ntype: Spec\nsummary: Test\n---\n\n# Spec\n\n## User Stories\n\n### Story A\nTBD\n\nstatus:: TODO\n\nTBD\n^story-a\n"
	writeOntologyNote(t, root, notePath, source)
	schema, err := ontoschema.LoadSchema(root)
	require.NoError(t, err)
	ref := NodeRef{NotePath: notePath, Fragment: "^story-a", Kind: NodeKindEmbedded}
	projection, err := ProjectNode(context.Background(), obsidian.VaultDefinition{Path: root}, &obsidian.Note{}, schema, ref)
	require.NoError(t, err)
	var target NodeBodyBlock
	for _, block := range BuildNodeBody(projection, schema) {
		if block.Kind == NodeBodyBlockKindNarrative && strings.Contains(block.Markdown, "TBD") {
			target = block
		}
	}
	require.Greater(t, target.Range.Start, strings.Index(source, "status:: TODO"))
	session := NewEditSession(obsidian.VaultDefinition{Path: root}, &obsidian.Note{}, schema)
	require.NoError(t, session.SetNarrative(ref, target.Range.Start, target.Range.End, target.Markdown, "EDITED SECOND"))
	result, err := session.Commit(context.Background())
	require.NoError(t, err)
	require.Equal(t, CommitOutcomeCommitted, result.Outcome)
	actual, err := os.ReadFile(filepath.Join(root, notePath))
	require.NoError(t, err)
	require.Equal(t, strings.Replace(source, "status:: TODO\n\nTBD", "status:: TODO\n\nEDITED SECOND", 1), string(actual))
}

func TestNarrativeReplayRequiresUnambiguousContentWitness(t *testing.T) {
	block := func(start int, text string) NodeBodyBlock {
		return NodeBodyBlock{Kind: NodeBodyBlockKindNarrative, Range: ByteRange{Start: start, End: start + len(text)}, Markdown: text}
	}
	for _, tc := range []struct {
		name         string
		blocks       []NodeBodyBlock
		previous     string
		wantStart    int
		wantConflict bool
	}{
		{name: "duplicate content after drift", blocks: []NodeBodyBlock{block(250, "TODO"), block(350, "TODO")}, previous: "TODO", wantConflict: true},
		{name: "same length concurrent replacement", blocks: []NodeBodyBlock{block(200, "BAZZ")}, previous: "FOOO", wantConflict: true},
		{name: "unique matching content rebases", blocks: []NodeBodyBlock{block(250, "TODO")}, previous: "TODO", wantStart: 250},
		{name: "missing witness", blocks: []NodeBodyBlock{block(200, "TODO")}, wantConflict: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := resolveNarrativeBlock(tc.blocks, setNarrativeOp{Range: ByteRange{Start: 200, End: 204}, PreviousMarkdown: tc.previous})
			if tc.wantConflict {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tc.wantStart, got.Range.Start)
		})
	}
}
