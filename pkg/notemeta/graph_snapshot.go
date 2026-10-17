package notemeta

import (
	"context"
	"strings"
	"time"

	vaultgraph "github.com/atomicobject/rhizome/pkg/graph"
	"github.com/atomicobject/rhizome/pkg/markdownsyntax"
	"github.com/atomicobject/rhizome/pkg/paths"
	semdb "github.com/atomicobject/rhizome/pkg/store"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
)

// LoadPersistedGraphSnapshot returns a persisted graph only when the global
// metadata state and every graph source row are current for this Indexer's
// selected provider runtime. A stale or fatal row remains inspectable in the
// store but causes the caller to use its existing live-source fallback.
func (i Indexer) LoadPersistedGraphSnapshot(ctx context.Context, vaultDef obsidian.VaultDefinition, note obsidian.NoteReader, store *semdb.Store) (*vaultgraph.GraphSnapshot, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if store == nil {
		return nil, nil
	}
	ready, err := i.MetadataStateCurrent(ctx, vaultDef, note, store)
	if err != nil || !ready {
		return nil, err
	}
	return loadPersistedGraphSnapshot(ctx, store, i.projectionCurrentForRow)
}

// LoadReadyPersistedGraphSnapshot builds the canonical graph DTO from a
// metadata snapshot whose readiness has already been accepted by an indexed
// read policy. Unlike LoadPersistedGraphSnapshot, it intentionally does not
// rediscover or hash live notes to prove freshness.
func LoadReadyPersistedGraphSnapshot(ctx context.Context, store *semdb.Store) (*vaultgraph.GraphSnapshot, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if store == nil {
		return nil, nil
	}
	ready, err := MetadataStateReady(ctx, store)
	if err != nil || !ready {
		return nil, err
	}
	return loadPersistedGraphSnapshot(ctx, store, nil)
}

func loadPersistedGraphSnapshot(ctx context.Context, store *semdb.Store, currentRow func(semdb.NoteMetadataRow) bool) (*vaultgraph.GraphSnapshot, error) {
	rows, err := store.CurrentNoteMetadataRows(ctx)
	if err != nil || len(rows) == 0 {
		return nil, err
	}
	tags, err := store.CurrentNoteTags(ctx, nil)
	if err != nil {
		return nil, err
	}
	detailEdges, err := store.GraphDocNoteLinkEdges(ctx)
	if err != nil {
		return nil, err
	}
	coarseEdges, err := store.GraphDocNoteEdges(ctx)
	if err != nil {
		return nil, err
	}

	snapshot := &vaultgraph.GraphSnapshot{
		Nodes: make(map[string]vaultgraph.GraphSnapshotNode, len(rows)),
		Edges: make([]vaultgraph.GraphSnapshotEdge, 0, len(detailEdges)),
	}
	for _, row := range rows {
		if currentRow != nil && !currentRow(row) {
			return nil, nil
		}
		path := persistedGraphPath(row.Path)
		if path == "" {
			continue
		}
		node := vaultgraph.GraphSnapshotNode{Path: path, Size: row.Size}
		if row.Mtime > 0 {
			node.ContentTime = time.Unix(row.Mtime, 0)
		}
		snapshot.Nodes[path] = node
	}
	for _, row := range tags {
		path := persistedGraphPath(row.NotePath)
		node, ok := snapshot.Nodes[path]
		if !ok {
			continue
		}
		if tag := strings.TrimSpace(row.TagNorm); tag != "" {
			node.Tags = append(node.Tags, tag)
			snapshot.Nodes[path] = node
		}
	}

	detailedPairs := make(map[string]struct{}, len(detailEdges))
	for _, row := range detailEdges {
		linkType, subtype, ok := semdb.ParseNoteLinkKind(row.Kind)
		if !ok {
			continue
		}
		source := persistedGraphPath(row.SrcPath)
		target := persistedGraphPath(row.DstPath)
		detailedPairs[source+"\x00"+target] = struct{}{}
		snapshot.Edges = append(snapshot.Edges, vaultgraph.GraphSnapshotEdge{
			Source:   source,
			Target:   target,
			LinkType: linkType,
			Subtype:  markdownsyntax.BacklinkType(subtype),
		})
	}
	for _, edge := range coarseEdges {
		source := persistedGraphPath(edge.SrcPath)
		target := persistedGraphPath(edge.DstPath)
		if _, ok := detailedPairs[source+"\x00"+target]; ok {
			continue
		}
		snapshot.Edges = append(snapshot.Edges, vaultgraph.GraphSnapshotEdge{
			Source:   source,
			Target:   target,
			LinkType: "wikilink",
			Subtype:  markdownsyntax.BacklinkTypeBasic,
		})
	}
	snapshot.Normalize()
	return snapshot, nil
}

func persistedGraphPath(path string) string {
	notePath, err := paths.CleanNotePath(path)
	if err != nil {
		return ""
	}
	return notePath.String()
}
