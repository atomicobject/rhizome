package mcp

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	semdb "github.com/atomicobject/rhizome/pkg/anchors/sqlite"
	"github.com/atomicobject/rhizome/pkg/noteformat/builtin"
	"github.com/atomicobject/rhizome/pkg/notemeta"
	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/search/graphalg"
	"github.com/atomicobject/rhizome/pkg/testutil/sqlitefixture"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/stretchr/testify/require"
)

func TestGraphPathToolUsesOntologyOnlyFacts(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	store, err := sqlitefixture.Open(filepath.Join(root, ".rhizome", "db.sqlite"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })
	storyRef := ontology.NodeRef{NotePath: "docs/spec.md", Kind: ontology.NodeKindEmbedded, TypeName: "UserStory", NodeID: "story-a", Fragment: "^story-a"}
	storyJSON, err := json.Marshal(storyRef)
	require.NoError(t, err)
	require.NoError(t, store.ReplaceOntologySnapshot(ctx, semdb.OntologySnapshot{
		Edges: []semdb.OntologyEdgeRow{
			{SrcPath: "docs/effort.md", RelationName: "frozenSpecs", DstPath: "docs/spec.md", DstType: "TechnicalSpec", Provenance: "field", Structural: true},
			{SrcPath: "docs/effort.md", RelationName: "frozenStories", DstPath: "docs/spec.md", DstNodeID: "story-a", DstType: "UserStory", Provenance: "field", Structural: true},
			{SrcPath: "docs/effort.md", RelationName: "brief", DstPath: "docs/Brief.HTML", DstType: "Brief", Provenance: "field", Structural: true},
		},
		SchemaState: semdb.OntologySchemaState{SchemaHash: "schema", NotesHash: "notes", LoadedAt: 1, Ready: true},
	}))
	require.NoError(t, store.ReplaceOntologyNodes(ctx, []string{"docs/spec.md"}, []codeanchor.IntelOntologyNode{{
		NodeID: ontology.OntologyNodeID(storyRef), NotePath: "docs/spec.md", NodeRefJSON: string(storyJSON),
		NodeKind: string(ontology.NodeKindEmbedded), TypeName: "UserStory", DisplayLabel: "Story A",
		SourceLocator: "docs/spec.md#^story-a", BlockID: "story-a", LocatorStatus: string(ontology.NodeLocatorLinkable), UpdatedAt: 1,
	}}))

	tool := GraphPathTool(Config{
		VaultDef:   obsidian.VaultDefinition{Name: "vault", Path: root},
		IntelStore: store,
	})
	shortest := func(t *testing.T, from, to string) graphalg.PathResult {
		t.Helper()
		resp, err := tool(ctx, mcp.CallToolRequest{
			Params: mcp.CallToolParams{
				Name:      "graph_path",
				Arguments: map[string]interface{}{"from": from, "to": to, "maxHops": float64(4)},
			},
		})
		require.NoError(t, err)
		require.NotNil(t, resp)
		require.Len(t, resp.Content, 1)
		text, ok := resp.Content[0].(mcp.TextContent)
		require.True(t, ok)
		require.False(t, resp.IsError, text.Text)
		var path graphalg.PathResult
		require.NoError(t, json.Unmarshal([]byte(text.Text), &path))
		return path
	}

	path := shortest(t, "effort", "docs/spec.md")
	require.Equal(t, 1, path.Hops)
	require.Len(t, path.Path, 1)
	require.Equal(t, "frozenSpecs", path.Path[0].EdgeKind)

	// Embedded endpoints resolve by their fragment locator to the embedded node.
	path = shortest(t, "docs/effort.md", "docs/spec.md#^story-a")
	require.Equal(t, 1, path.Hops)
	require.Len(t, path.Path, 1)
	require.Equal(t, "frozenStories", path.Path[0].EdgeKind)
	require.Equal(t, "embedded:"+ontology.OntologyNodeID(storyRef), path.To)

	// Authored non-Markdown extensions and case survive as the endpoint path.
	path = shortest(t, "docs/effort.md", "docs/Brief.HTML")
	require.Equal(t, 1, path.Hops)
	require.Equal(t, "brief", path.Path[0].EdgeKind)
	require.Contains(t, path.To, "docs/Brief.HTML")
}

func TestAuthorityScoresToPayloadUsesCurrentProviderProjection(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	path := filepath.Join(root, "docs", "overview.md")
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, []byte("---\nsummary: Indexed summary\n---\n\n# Indexed title\n"), 0o644))

	runtime, err := builtin.NewRuntime()
	require.NoError(t, err)
	indexer, err := notemeta.NewIndexer(runtime)
	require.NoError(t, err)
	store := newIntelStore(t)
	vaultDef := obsidian.VaultDefinition{Path: root}
	_, err = indexer.EnsureIndexed(ctx, vaultDef, &obsidian.Note{}, store)
	require.NoError(t, err)

	payload := authorityScoresToPayload([]obsidian.AuthorityScore{{Path: "docs/overview.md", Authority: 1}}, 0, Config{
		VaultDef:     vaultDef,
		IntelStore:   store,
		NoteMetadata: indexer,
	}, &obsidian.Note{})
	require.Len(t, payload, 1)
	require.Equal(t, "Indexed title", payload[0].Title)
	require.Equal(t, "Indexed summary", payload[0].Frontmatter["summary"])
}
