package graphpresentation

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/stretchr/testify/require"
)

func TestFolderOverviewMatchesBrowserProjection(t *testing.T) {
	data, err := os.ReadFile("../../../web/src/test/fixtures/folder-overview.json")
	require.NoError(t, err)
	var fixture struct {
		Graph    GraphResponse
		Overview FolderOverview
	}
	require.NoError(t, json.Unmarshal(data, &fixture))
	before, err := json.Marshal(fixture.Graph)
	require.NoError(t, err)
	require.Equal(t, fixture.Overview, ProjectFolderOverview(fixture.Graph))
	dataAfter, err := json.Marshal(fixture.Graph)
	require.NoError(t, err)
	require.Equal(t, before, dataAfter)
}

func TestFolderOverviewPreservesFileDisplayAndDropsUnusedMetadata(t *testing.T) {
	graph := GraphResponse{NeedsIndex: true, Truncated: true, Nodes: []GraphNode{{ID: "a", Kind: "note", Label: "A", Path: "a.md", NodeID: "node-a", Score: 0.3, Authority: 0.4, Community: "c", NodeRef: &ontology.NodeRef{NotePath: "a.md"}, SourceLocator: "a.md", Lang: "markdown", Module: "docs", Hub: 1, PageRank: 0.5}}, Edges: []GraphEdge{}}
	out := ProjectFolderOverview(graph)
	want := graph.Nodes[0]
	want.NodeRef = nil
	want.SourceLocator = ""
	want.Lang = ""
	want.Module = ""
	want.Hub = 0
	want.PageRank = 0
	require.Equal(t, []GraphNode{want}, out.Files)
	require.True(t, out.Truncated)
	require.True(t, out.NeedsIndex)
	require.Empty(t, out.Links)
}
