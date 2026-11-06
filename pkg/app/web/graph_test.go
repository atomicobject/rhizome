package web

import (
	"context"
	"fmt"
	"github.com/atomicobject/rhizome/internal/testkit"
	"github.com/atomicobject/rhizome/pkg/app/graphpresentation"
	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/ontology/noderead"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"
	"os"
	"path/filepath"
	"testing"
)

func (s *Server) buildGlobalGraph(ctx context.Context, limit, depth int) (graphpresentation.GraphResponse, error) {
	return s.buildGlobalGraphMode(ctx, limit, depth, false, false)
}

func TestResolveLocalGraphPathUsesConfiguredOwnershipAndCanonicalPath(t *testing.T) {
	root := t.TempDir()
	rel := "notes/Decision.MD"
	require.NoError(t, os.MkdirAll(filepath.Join(root, "notes"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, filepath.FromSlash(rel)), []byte("# Decision\n"), 0o644))
	catalog, err := NewFileCatalog(obsidian.VaultDefinition{Path: root}, nil, testkit.NoteMetadataIndexer(t))
	require.NoError(t, err)
	srv := &Server{catalog: catalog}

	kind, p, err := srv.resolveLocalGraphPath(" ./notes/Decision.MD ")
	require.NoError(t, err)
	require.Equal(t, "note", kind)
	require.Equal(t, rel, p)

	kind, p, err = srv.resolveLocalGraphPath("notes/Decision.html")
	require.NoError(t, err)
	require.Equal(t, "code", kind)
	require.Equal(t, "notes/Decision.html", p)
}

func TestConfiguredDescriptorOnlyHTMLIsNotCatalogedOrGraphCode(t *testing.T) {
	root := t.TempDir()
	rel := "notes/Decision.HTML"
	require.NoError(t, os.MkdirAll(filepath.Join(root, "notes"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, filepath.FromSlash(rel)), []byte("<h1>Decision</h1>"), 0o644))
	catalog, err := NewFileCatalog(obsidian.VaultDefinition{Root: root, Includes: []string{"notes/*.html"}}, nil, testkit.DescriptorOnlyHTMLNoteMetadataIndexer(t))
	require.NoError(t, err)
	require.Empty(t, catalog.Suggest("Decision", 10))

	srv := &Server{catalog: catalog, cfg: Config{VaultPath: root}}
	_, _, err = srv.resolveLocalGraphPath(rel)
	require.ErrorContains(t, err, "format \"html\"")
	require.ErrorContains(t, err, "graph projection is not supported")
	_, err = srv.readFileView(t.Context(), rel)
	require.ErrorContains(t, err, "file view projection is not supported")
}

func TestModuleKeyDepth(t *testing.T) {
	require.Equal(t, "src/pkg", moduleKey("/src/pkg/main.go", 2))
	require.Equal(t, "src/pkg/main.go", moduleKey("/src/pkg/main.go", 8))
	require.Equal(t, "src", moduleKey("src/main.go", 1))
}

func TestCollapseGraphCollapsesLargeModules(t *testing.T) {
	nodes := map[string]graphpresentation.GraphNode{}
	edges := make([]graphpresentation.GraphEdge, 0, graphpresentation.ModuleCollapseThreshold+1)

	for i := range graphpresentation.ModuleCollapseThreshold + 1 {
		path := fmt.Sprintf("pkg/a/file-%03d.md", i)
		id := nodeID("note", path)
		nodes[id] = graphpresentation.GraphNode{
			ID:        id,
			Path:      path,
			Label:     path,
			Kind:      "note",
			Module:    "pkg/a",
			Community: "shared",
			Authority: float64(i),
			Hub:       float64(i) / 2,
		}
	}

	externalPath := "docs/readme.md"
	externalID := nodeID("note", externalPath)
	nodes[externalID] = graphpresentation.GraphNode{
		ID:     externalID,
		Path:   externalPath,
		Label:  externalPath,
		Kind:   "note",
		Module: "docs/readme.md",
	}
	edges = append(edges, graphpresentation.GraphEdge{
		Source: nodeID("note", "pkg/a/file-000.md"),
		Target: externalID,
		Kind:   "wikilink",
		Weight: 1,
	})

	outNodes, outEdges, truncated := collapseGraph(nodes, edges, 200)

	require.True(t, truncated)

	moduleID := nodeID("module", "pkg/a")
	var moduleNode *graphpresentation.GraphNode
	for i := range outNodes {
		if outNodes[i].ID == moduleID {
			moduleNode = &outNodes[i]
			break
		}
	}
	require.NotNil(t, moduleNode)
	require.True(t, moduleNode.Collapsed)
	require.Equal(t, graphpresentation.ModuleCollapseThreshold+1, moduleNode.ChildCount)
	require.Equal(t, "shared", moduleNode.Community)
	require.Equal(t, 250.0, moduleNode.Authority)
	require.Equal(t, 125.0, moduleNode.Hub)

	foundCollapsedEdge := false
	for _, e := range outEdges {
		if e.Source == moduleID && e.Target == externalID {
			foundCollapsedEdge = true
			break
		}
	}
	require.True(t, foundCollapsedEdge)
}

func TestLocalGraphCenterIDMatchesTypedEmbeddedNodeRef(t *testing.T) {
	ref := ontology.NodeRef{
		NotePath: "docs/spec.md",
		Fragment: "^story-a",
		NodeID:   "story-a",
		Kind:     ontology.NodeKindEmbedded,
	}
	nodes := map[string]graphpresentation.GraphNode{
		"embedded:story-a": {
			ID:            "embedded:story-a",
			Kind:          "embedded",
			NotePath:      "docs/spec.md",
			NodeID:        "story-a",
			NodeRef:       &ref,
			SourceLocator: "docs/spec.md#^story-a",
		},
	}

	got := localGraphCenterID(nodes, ontology.NodeRef{
		NotePath: "docs/spec.md",
		NodeID:   "story-a",
		Kind:     ontology.NodeKindEmbedded,
	}, "note:docs/spec.md")

	require.Equal(t, "embedded:story-a", got)
}

func TestWebGraphPresentationPreservesSurfacePolicy(t *testing.T) {
	ref := ontology.NodeRef{NotePath: "docs/spec.md", NodeID: "story-a", Kind: ontology.NodeKindEmbedded}
	nodes := map[string]graphpresentation.GraphNode{}
	server := &Server{}
	for _, node := range []noderead.GraphEndpoint{
		{ID: "code:file.go", Kind: noderead.GraphEndpointCode, Path: "file.go"},
		{ID: "code:src/pkg/file.go", Kind: noderead.GraphEndpointCode, Path: "src/pkg/file.go"},
		{ID: "embedded:story-a", Kind: noderead.GraphEndpointEmbedded, NotePath: "docs/spec.md", NodeID: "story-a", Ref: ref, SourceLocator: "docs/spec.md#^story-a", Label: "Story", TypeName: "Story"},
	} {
		server.mergeReadGraphNode(nodes, node, 2)
	}
	result, edges, truncated := finalizeGraph(nodes, []graphpresentation.GraphEdge{{Source: "embedded:story-a", Target: "code:file.go", Kind: "ontology", Weight: 2, RelationName: "implements", RelationLabel: "Implements", Provenance: "typed", Structural: true}}, 20, false)
	require.False(t, truncated)
	require.Equal(t, []graphpresentation.GraphNode{
		{ID: "code:file.go", Kind: "code", Path: "file.go", Label: "file", Lang: "go", Module: "file.go"},
		{ID: "code:src/pkg/file.go", Kind: "code", Path: "src/pkg/file.go", Label: "file", Lang: "go", Module: "src/pkg"},
		{ID: "embedded:story-a", Kind: "embedded", Path: "docs/spec.md", NotePath: "docs/spec.md", NodeID: "story-a", NodeRef: &ref, Label: "Story", Lang: "markdown", Module: "embedded:story-a", SourceLocator: "docs/spec.md#^story-a", ResolvedType: "Story"},
	}, result)
	require.Equal(t, []graphpresentation.GraphEdge{{Source: "embedded:story-a", Target: "code:file.go", Kind: "ontology", Weight: 2, RelationName: "implements", RelationLabel: "Implements", Provenance: "typed", Structural: true}}, edges)
}

func TestWebGraphNotesOnlyNeverCollapses(t *testing.T) {
	nodes := map[string]graphpresentation.GraphNode{}
	for i := range 251 {
		id := fmt.Sprintf("note:docs/note-%03d.md", i)
		nodes[id] = graphpresentation.GraphNode{ID: id, Module: "docs", Kind: "note"}
	}
	result, _, truncated := finalizeGraph(nodes, nil, 200, true)
	require.False(t, truncated)
	require.Len(t, result, 251)
	require.Equal(t, "note:docs/note-000.md", result[0].ID)
	require.Equal(t, "note:docs/note-250.md", result[250].ID)
}

func TestWebCollapsedGraphPreservesEmptyEdgeArray(t *testing.T) {
	nodes := map[string]graphpresentation.GraphNode{}
	for i := range 251 {
		id := fmt.Sprintf("note:docs/note-%03d.md", i)
		nodes[id] = graphpresentation.GraphNode{ID: id, Module: "docs", Kind: "note"}
	}
	result, edges, truncated := finalizeGraph(nodes, nil, 200, false)
	require.True(t, truncated)
	require.Len(t, result, 1)
	require.NotNil(t, edges)
	require.Empty(t, edges)
}
