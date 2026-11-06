package cmd

import (
	"context"
	"fmt"
	"github.com/atomicobject/rhizome/pkg/app/graphpresentation"
	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/ontology/noderead"
	"github.com/atomicobject/rhizome/pkg/vault/ignore"
	"path/filepath"
	"testing"

	semdb "github.com/atomicobject/rhizome/pkg/store"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"
)

func TestResolveLocalGraphTargetUsesPersistedDocKind(t *testing.T) {
	root := t.TempDir()
	store, err := semdb.Open(filepath.Join(root, "graph.sqlite"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })
	require.NoError(t, store.ReplaceGraphDocScores(context.Background(), []semdb.GraphDocScore{
		{DocPath: "Notes/Decision.MD", DocType: "note"},
		{DocPath: "Notes/Reference.html", DocType: "note"},
	}))

	gb := &graphBuilder{store: store, vaultDef: obsidian.VaultDefinition{Path: root}}
	for _, input := range []struct {
		raw  string
		want string
	}{
		{raw: "Notes/Decision.MD", want: "Notes/Decision.MD"},
		{raw: "Notes/Reference.html", want: "Notes/Reference.html"},
		{raw: filepath.Join(root, "Notes", "Reference.html"), want: "Notes/Reference.html"},
	} {
		kind, resolved, err := gb.resolveLocalGraphTarget(context.Background(), input.raw)
		require.NoError(t, err)
		require.Equal(t, "note", kind)
		require.Equal(t, input.want, resolved)
	}
}

func TestResolveLocalGraphTargetRejectsUnknownPath(t *testing.T) {
	root := t.TempDir()
	store, err := semdb.Open(filepath.Join(root, "graph.sqlite"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	gb := &graphBuilder{store: store, vaultDef: obsidian.VaultDefinition{Path: root}}
	_, _, err = gb.resolveLocalGraphTarget(context.Background(), "Notes/Unknown.html")
	require.ErrorContains(t, err, "not in the persisted graph catalog")
}

func TestCLIGraphPresentationPreservesSurfacePolicy(t *testing.T) {
	ref := ontology.NodeRef{NotePath: "docs/spec.md", NodeID: "story-a", Kind: ontology.NodeKindEmbedded}
	graph := noderead.GraphResult{
		Nodes: []noderead.GraphEndpoint{
			{ID: "code:file.go", Kind: noderead.GraphEndpointCode, Path: "file.go"},
			{ID: "code:src/pkg/file.go", Kind: noderead.GraphEndpointCode, Path: "src/pkg/file.go"},
			{ID: "embedded:story-a", Kind: noderead.GraphEndpointEmbedded, NotePath: "docs/spec.md", NodeID: "story-a", Ref: ref, SourceLocator: "docs/spec.md#^story-a", Label: "Story", TypeName: "Story"},
		},
		Edges: []noderead.GraphReadEdge{
			{Source: "embedded:story-a", Target: "code:file.go", Kind: "ontology", Weight: 2, RelationName: "implements", RelationLabel: "Implements", Provenance: "typed", Structural: true},
			{Source: "missing", Target: "code:file.go", Kind: "wikilink"},
		},
	}
	result := (&graphBuilder{}).presentNodeReadGraph(graph, 20, 2, "embedded:story-a")
	require.Equal(t, "embedded:story-a", result.CenterID)
	require.False(t, result.Truncated)
	// CLI node order is deliberately unspecified; edge order follows the source.
	require.ElementsMatch(t, []graphpresentation.GraphNode{
		{ID: "code:file.go", Kind: "code", Path: "file.go", Label: "file", Module: "root"},
		{ID: "code:src/pkg/file.go", Kind: "code", Path: "src/pkg/file.go", Label: "file", Module: "src/pkg"},
		{ID: "embedded:story-a", Kind: "embedded", Path: "docs/spec.md", NotePath: "docs/spec.md", NodeID: "story-a", NodeRef: &ref, Label: "Story", Lang: "markdown", Module: "docs", SourceLocator: "docs/spec.md#^story-a", ResolvedType: "Story"},
	}, result.Nodes)
	require.Equal(t, []graphpresentation.GraphEdge{{Source: "embedded:story-a", Target: "code:file.go", Kind: "ontology", Weight: 2, RelationName: "implements", RelationLabel: "Implements", Provenance: "typed", Structural: true}}, result.Edges)
}

func TestCLIGraphCollapsesLargeModules(t *testing.T) {
	graph := noderead.GraphResult{}
	for i := range 251 {
		path := fmt.Sprintf("src/pkg/file-%03d.go", i)
		graph.Nodes = append(graph.Nodes, noderead.GraphEndpoint{ID: "code:" + path, Kind: noderead.GraphEndpointCode, Path: path})
	}
	graph.Nodes = append(graph.Nodes, noderead.GraphEndpoint{ID: "code:root.go", Kind: noderead.GraphEndpointCode, Path: "root.go"})
	graph.Edges = []noderead.GraphReadEdge{
		{Source: "code:src/pkg/file-000.go", Target: "code:root.go", Kind: "calls", Weight: 2, RelationName: "first"},
		{Source: "code:src/pkg/file-001.go", Target: "code:root.go", Kind: "calls", Weight: 3, RelationName: "second"},
	}
	result := (&graphBuilder{}).presentNodeReadGraph(graph, 200, 2, "")
	require.True(t, result.Truncated)
	require.ElementsMatch(t, []graphpresentation.GraphNode{
		{ID: "module:src/pkg", Path: "src/pkg", Label: "src/pkg", Kind: "module", Collapsed: true, ChildCount: 251},
		{ID: "code:root.go", Path: "root.go", Label: "root", Kind: "code", Module: "root"},
	}, result.Nodes)
	require.Equal(t, []graphpresentation.GraphEdge{{Source: "module:src/pkg", Target: "code:root.go", Kind: "calls", Weight: 5}}, result.Edges)
}

func TestCLIGraphIgnoresNotesBeforeProjectingEdges(t *testing.T) {
	graph := noderead.GraphResult{
		Nodes: []noderead.GraphEndpoint{
			{ID: "note:ignored/a.md", Kind: noderead.GraphEndpointNote, Path: "ignored/a.md", NotePath: "ignored/a.md"},
			{ID: "note:b.md", Kind: noderead.GraphEndpointNote, Path: "b.md", NotePath: "b.md"},
		},
		Edges: []noderead.GraphReadEdge{{Source: "note:ignored/a.md", Target: "note:b.md", Kind: "wikilink"}},
	}
	builder := &graphBuilder{ignoreMatcher: ignore.NewMatcher([]string{"ignored/"})}
	result := builder.presentNodeReadGraph(graph, 20, 2, "")
	require.Equal(t, []graphpresentation.GraphNode{{ID: "note:b.md", Path: "b.md", NotePath: "b.md", Label: "b", Kind: "note", Lang: "markdown", Module: "root"}}, result.Nodes)
	require.Empty(t, result.Edges)
}
