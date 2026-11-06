package web

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"testing"

	"github.com/atomicobject/rhizome/internal/testkit"
	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	"github.com/atomicobject/rhizome/pkg/ontology"
	semdb "github.com/atomicobject/rhizome/pkg/store"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"
)

func TestLocalGraphCountsThousandNeighbours(t *testing.T) {
	root := t.TempDir()
	store, err := semdb.Open(filepath.Join(root, "db.sqlite"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })
	ctx := t.Context()
	anchor := func(id, path string) []codeanchor.IntelAnchor {
		return []codeanchor.IntelAnchor{{AnchorID: id, Path: path, Lang: codeanchor.LangGo, Kind: "function", Symbol: id, FQN: id, Fingerprint: id}}
	}
	require.NoError(t, store.ReplaceIntelCodeFile(ctx, "center.go", anchor("center", "center.go"), nil, nil))
	for i := 0; i < 1000; i++ {
		id := fmt.Sprintf("neighbour%03d", i)
		path := id + ".go"
		require.NoError(t, store.ReplaceIntelCodeFile(ctx, path, anchor(id, path), []codeanchor.IntelEdge{{SrcID: id, DstID: "center", Kind: "calls"}}, nil))
	}
	runtime := &Runtime{}
	runtime.SetIntelStore(store)
	s := &Server{runtime: runtime, cfg: Config{VaultPath: root, VaultDef: obsidian.VaultDefinition{Path: root}}, noteMetadata: testkit.NoteMetadataIndexer(t)}
	graph, err := s.buildLocalGraphRefMode(ctx, ontology.NodeRef{NotePath: "center.go", Kind: ontology.NodeKind("code")}, 200, 1, false, false)
	require.NoError(t, err)
	require.True(t, graph.Truncated)
	require.Len(t, graph.Nodes, 200)
	require.Equal(t, 801, graph.OmittedNodes)
	require.Equal(t, 801, graph.OmittedEdges)
	require.Len(t, graph.Edges, 199)
	require.Len(t, graph.OmittedNodeIDs, 200)
	payload, err := json.Marshal(graph)
	require.NoError(t, err)
	require.NotContains(t, string(payload), `"omittedLinks"`)
	requireGraphNode(t, graph, "code:center.go", "code")
}
