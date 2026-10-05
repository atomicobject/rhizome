package web

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"
)

func TestResolveLocalGraphPathUsesConfiguredOwnershipAndCanonicalPath(t *testing.T) {
	root := t.TempDir()
	rel := "notes/Decision.MD"
	require.NoError(t, os.MkdirAll(filepath.Join(root, "notes"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, filepath.FromSlash(rel)), []byte("# Decision\n"), 0o644))
	catalog, err := NewFileCatalog(obsidian.VaultDefinition{Path: root}, nil, testNoteMetadataIndexer(t))
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
	catalog, err := NewFileCatalog(obsidian.VaultDefinition{Root: root, Includes: []string{"notes/*.html"}}, nil, descriptorOnlyHTMLNoteMetadataIndexer(t))
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
	nodes := map[string]GraphNode{}
	edges := make([]GraphEdge, 0, moduleCollapseThreshold+1)

	for i := range moduleCollapseThreshold + 1 {
		path := fmt.Sprintf("pkg/a/file-%03d.md", i)
		id := nodeID("note", path)
		nodes[id] = GraphNode{
			ID:     id,
			Path:   path,
			Label:  path,
			Kind:   "note",
			Module: "pkg/a",
		}
	}

	externalPath := "docs/readme.md"
	externalID := nodeID("note", externalPath)
	nodes[externalID] = GraphNode{
		ID:     externalID,
		Path:   externalPath,
		Label:  externalPath,
		Kind:   "note",
		Module: "docs/readme.md",
	}
	edges = append(edges, GraphEdge{
		Source: nodeID("note", "pkg/a/file-000.md"),
		Target: externalID,
		Kind:   "wikilink",
		Weight: 1,
	})

	outNodes, outEdges, truncated := collapseGraph(nodes, edges, 200)

	require.True(t, truncated)

	moduleID := nodeID("module", "pkg/a")
	var moduleNode *GraphNode
	for i := range outNodes {
		if outNodes[i].ID == moduleID {
			moduleNode = &outNodes[i]
			break
		}
	}
	require.NotNil(t, moduleNode)
	require.True(t, moduleNode.Collapsed)
	require.Equal(t, moduleCollapseThreshold+1, moduleNode.ChildCount)

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
	nodes := map[string]GraphNode{
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
