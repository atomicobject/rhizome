package sqlite

import (
	"context"
	"testing"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	"github.com/atomicobject/rhizome/pkg/search/embeddings"
	"github.com/stretchr/testify/require"
)

func TestRowOwnership_NoteMetadataWritesRawFactsOnly(t *testing.T) {
	ctx := context.Background()
	store, err := Open(currentSchemaTestDBPath(t, "ownership.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	require.NoError(t, store.ReplaceNoteMetadataSnapshot(ctx, NoteMetadataSnapshot{
		State: NoteMetadataState{NotesHash: "notes-hash", RawNotesHash: "raw-notes-hash", LoadedAt: 1, Ready: true},
		Notes: []NoteMetadataRow{{
			Path:        "notes/source.md",
			Title:       "Source",
			ContentHash: "hash",
			Mtime:       1,
		}, {
			Path:        "notes/target.md",
			Title:       "Target",
			ContentHash: "target-hash",
			Mtime:       1,
		}},
		PropertyValues: []NotePropertyValueRow{{
			NotePath:     "notes/source.md",
			PropertyName: "status",
			Source:       NotePropertySourceFrontmatter,
			ValueText:    "active",
			ValueNorm:    "active",
			ValueKind:    NotePropertyValueString,
		}},
		Tags: []NoteTagRow{{NotePath: "notes/source.md", TagNorm: "phase/five"}},
		WikilinkEdges: []GraphDocEdgeRow{{
			SrcPath:         "notes/source.md",
			DstPath:         "notes/target.md",
			Kind:            GraphDocEdgeKindWikilink,
			Confidence:      EdgeConfidenceExtracted,
			ConfidenceScore: 1,
		}},
	}))

	rows, err := store.CurrentNoteMetadataRows(ctx)
	require.NoError(t, err)
	require.Len(t, rows, 2)
	props, err := store.CurrentNotePropertyValues(ctx, []string{"notes/source.md"}, nil, 0)
	require.NoError(t, err)
	require.Len(t, props, 1)
	tags, err := store.CurrentNoteTags(ctx, []string{"notes/source.md"})
	require.NoError(t, err)
	require.Len(t, tags, 1)
	edges, err := store.GraphDocNoteEdges(ctx)
	require.NoError(t, err)
	require.Len(t, edges, 1)

	nodes, err := store.OntologyNodesByPaths(ctx, []string{"notes/source.md"})
	require.NoError(t, err)
	require.Empty(t, nodes)
	anchors, err := store.IntelAnchorsByPath(ctx, "notes/source.md")
	require.NoError(t, err)
	require.Empty(t, anchors)
	chunks, err := store.IntelChunksByOwners(ctx, []string{"node:source"})
	require.NoError(t, err)
	require.Empty(t, chunks)
}

func TestRowOwnership_DerivedWritersDoNotDeleteCanonicalFacts(t *testing.T) {
	ctx := context.Background()
	store, err := Open(currentSchemaTestDBPath(t, "ownership.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	require.NoError(t, store.ReplaceNoteMetadataSnapshot(ctx, NoteMetadataSnapshot{
		State: NoteMetadataState{NotesHash: "notes-hash", RawNotesHash: "raw-notes-hash", LoadedAt: 1, Ready: true},
		Notes: []NoteMetadataRow{{Path: "notes/spec.md", Title: "Spec", ContentHash: "hash", Mtime: 1}},
	}))
	node := codeanchor.IntelOntologyNode{
		NodeID:        "node:spec",
		NotePath:      "notes/spec.md",
		NodeRefJSON:   `{"notePath":"notes/spec.md","kind":"NOTE","typeName":"Spec"}`,
		NodeKind:      "NOTE",
		TypeName:      "Spec",
		SourceLocator: "notes/spec.md",
		UpdatedAt:     1,
	}
	require.NoError(t, store.ReplaceOntologyNodeReadModel(ctx, codeanchor.IntelOntologyNodeReadModel{
		NotePaths: []string{node.NotePath},
		Nodes:     []codeanchor.IntelOntologyNode{node},
		FieldValues: []codeanchor.IntelOntologyNodeFieldValue{{
			NodeID:    node.NodeID,
			NotePath:  node.NotePath,
			TypeName:  node.TypeName,
			FieldName: "status",
			ValueNorm: "active",
			UpdatedAt: 1,
		}},
	}))
	require.NoError(t, store.ApplyOntologyDelta(ctx, OntologyDelta{
		EdgeSources: []string{node.NotePath},
		Edges: []OntologyEdgeRow{{
			SrcPath:      node.NotePath,
			SrcNodeID:    node.NodeID,
			RelationName: "owner",
			DstPath:      "people/alice.md",
			DstType:      "Person",
			Structural:   true,
			UpdatedAt:    1,
		}},
	}))

	require.NoError(t, store.ReplaceIntelChunks(ctx, []string{node.NodeID}, []codeanchor.IntelChunk{{
		ChunkID:     "chunk:spec",
		OwnerID:     node.NodeID,
		OwnerType:   "ontology_node",
		Granularity: "node_body",
		ContentHash: "chunk-hash",
		UpdatedAt:   1,
	}}))
	require.NoError(t, store.UpsertEmbeddings(ctx, map[string]embeddings.Embedding{"chunk:spec": {0.1, 0.2}}))
	require.NoError(t, store.ReplaceIntelCodeFile(ctx, "pkg/spec.go", []codeanchor.IntelAnchor{{
		AnchorID:    "anchor:spec",
		Lang:        codeanchor.LangGo,
		Kind:        "type",
		Path:        "pkg/spec.go",
		Symbol:      "Spec",
		FQN:         "pkg.Spec",
		Fingerprint: "anchor-fp",
	}}, nil, nil))

	notes, err := store.CurrentNoteMetadataRows(ctx)
	require.NoError(t, err)
	require.Len(t, notes, 1)
	nodes, err := store.OntologyNodesByIDs(ctx, []string{node.NodeID})
	require.NoError(t, err)
	require.Contains(t, nodes, node.NodeID)
	fields, err := store.OntologyNodeFieldValuesByNodeIDs(ctx, []string{node.NodeID}, nil)
	require.NoError(t, err)
	require.Len(t, fields, 1)
	edges, err := store.OntologyEdgesForPaths(ctx, []string{node.NotePath}, true, "", 0)
	require.NoError(t, err)
	require.Len(t, edges, 1)
	chunks, err := store.IntelChunksByOwners(ctx, []string{node.NodeID})
	require.NoError(t, err)
	require.Len(t, chunks, 1)
	anchors, err := store.IntelAnchorsByPath(ctx, "pkg/spec.go")
	require.NoError(t, err)
	require.Len(t, anchors, 1)
}
