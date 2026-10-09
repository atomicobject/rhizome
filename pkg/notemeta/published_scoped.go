package notemeta

import (
	"context"
	"fmt"
	"strings"

	semdb "github.com/atomicobject/rhizome/pkg/anchors/sqlite"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
)

// PublishedBaseline retains pre-transition metadata. Ownership controls can
// remove rows and invalidate the published state, so a scoped publication must
// capture this under the same indexing lease before any control is submitted.
// It contains database facts only; capturing it never reads authored files.
// Complete row coverage lets fixed-point dependency expansion reuse the same
// baseline even when a transition identifies another incident source.
type PublishedBaseline struct {
	state    semdb.NoteMetadataState
	paths    []string
	rows     map[string]semdb.NoteMetadataRow
	aliases  map[string][]string
	edges    []semdb.GraphDocEdgeRow
	eligible bool
}

func (i Indexer) CapturePublishedBaseline(ctx context.Context, vaultDef obsidian.VaultDefinition, store Store) (PublishedBaseline, error) {
	if store == nil {
		return PublishedBaseline{}, fmt.Errorf("note metadata store is required")
	}
	state, err := store.GetNoteMetadataState(ctx)
	if err != nil {
		return PublishedBaseline{}, err
	}
	paths, err := store.CurrentNoteMetadataPaths(ctx)
	if err != nil {
		return PublishedBaseline{}, err
	}
	rows, err := store.CurrentNoteMetadataRowsByPaths(ctx, paths)
	if err != nil {
		return PublishedBaseline{}, err
	}
	aliases, err := store.CurrentNoteAliases(ctx)
	if err != nil {
		return PublishedBaseline{}, err
	}
	expected, err := metadataStateHashWithSelectedProviders(vaultDef, state.RawNotesHash, i.formats, paths)
	if err != nil {
		return PublishedBaseline{}, err
	}
	eligible := state.Ready && strings.TrimSpace(state.RawNotesHash) != "" && state.NotesHash == expected && len(rows) == len(paths)
	formats, err := i.runtime()
	if err != nil {
		return PublishedBaseline{}, err
	}
	for _, row := range rows {
		if !i.projectionCurrentForRow(row) && !publishedFatalProjection(formats, row) && !descriptorOnlyStaleProjection(formats, row) {
			eligible = false
		}
	}
	// Retain incident note-link facts because ownership removes incoming edges
	// when it clears the edited target. Stable topology permits restoring these
	// facts without projecting or reading unchanged authored sources.
	var edges []semdb.GraphDocEdgeRow
	if graph, ok := store.(interface {
		GraphDocEdgesWithConfidenceForPaths(context.Context, []string) ([]semdb.GraphDocEdge, error)
	}); ok {
		incident, err := graph.GraphDocEdgesWithConfidenceForPaths(ctx, paths)
		if err != nil {
			return PublishedBaseline{}, err
		}
		for _, edge := range incident {
			if edge.Kind == semdb.GraphDocEdgeKindWikilink || edge.Kind == semdb.GraphDocEdgeKindMarkdownLink || strings.HasPrefix(edge.Kind, "note_link:") {
				edges = append(edges, semdb.GraphDocEdgeRow{SrcPath: edge.SrcPath, DstPath: edge.DstPath, Kind: edge.Kind, Confidence: edge.Confidence, ConfidenceScore: edge.ConfidenceScore, LinkText: edge.LinkText})
			}
		}
	} else {
		eligible = false
	}
	return PublishedBaseline{state: state, paths: paths, rows: rows, aliases: aliases, edges: edges, eligible: eligible}, nil
}

// BuildPublishedPathMetadataDeltaFromPreparation publishes only sealed changed
// sources when the pre-transition baseline and link topology permit it. The
// caller explicitly falls back to complete publication when needsFull is true.
// No untouched authored source is read or projected in this operation.
func (i Indexer) BuildPublishedPathMetadataDeltaFromPreparation(ctx context.Context, vaultDef obsidian.VaultDefinition, store Store, baseline PublishedBaseline, preparation PublishedMetadataPreparation, deletedPaths []string) (delta *MetadataDelta, needsFull bool, err error) {
	if store == nil {
		return nil, false, fmt.Errorf("note metadata store is required")
	}
	if err := ctx.Err(); err != nil {
		return nil, false, err
	}
	formats, err := i.runtime()
	if err != nil {
		return nil, false, err
	}
	if err := validatePublishedMetadataPreparation(formats, preparation); err != nil {
		return nil, false, err
	}
	if !baseline.eligible {
		return nil, true, nil
	}
	entries := preparation.sealedEntries
	durable := make(map[string]semdb.NoteMetadataRow, len(entries))
	for _, entry := range entries {
		path := entry.Source.Path().String()
		if row, ok := baseline.rows[path]; ok {
			durable[path] = row
		}
	}
	prior := publishedBaselineStore{Store: store, baseline: baseline}
	topology, aliases, err := projectionLinkTopologyChanged(ctx, prior, vaultDef, durable, entries, deletedPaths)
	if err != nil {
		return nil, false, err
	}
	if topology {
		return nil, true, nil
	}
	changed, touches := partitionProjectedEntries(durable, entries)
	if len(changed) == 0 && len(deletedPaths) == 0 {
		if len(touches) == 0 {
			return nil, false, nil
		}
		return &MetadataDelta{State: baseline.state, SourceTouches: touches}, false, nil
	}
	// The transition intentionally closes the global metadata read barrier,
	// including SQL link lookup. Resolve against the captured path/alias
	// inventory instead of treating that pending state as an empty target set.
	cache := obsidian.BuildNotePathCacheWithAliases(baseline.paths, overlayProjectionAliasesForDelta(aliases, changed, deletedPaths))

	rawHash, err := incrementalProjectedNotesHash(ctx, prior, baseline.state.RawNotesHash, changed, deletedPaths)
	if err != nil {
		return nil, false, err
	}
	delta, err = i.buildDelta(vaultDef, cache, changed, deletedPaths, nextLoadedAt(baseline.state.LoadedAt), rawHash, selectedPathsAfterDelta(baseline.paths, changed, deletedPaths))
	if delta != nil {
		delta.SourceTouches = touches
		// Only unchanged source facts are retained, and only when the target's
		// path and aliases have survived the topology check above. Changed
		// sources publish their newly resolved links instead.
		changedSources := make(map[string]bool, len(changed))
		for _, entry := range changed {
			changedSources[entry.Source.Path().String()] = true
		}
		for _, edge := range baseline.edges {
			if _, touched := durable[edge.DstPath]; touched && !changedSources[edge.SrcPath] {
				delta.WikilinkEdges = append(delta.WikilinkEdges, edge)
			}
		}
	}
	return delta, false, err
}

type publishedBaselineStore struct {
	Store
	baseline PublishedBaseline
}

func (s publishedBaselineStore) CurrentNoteAliases(context.Context) (map[string][]string, error) {
	return s.baseline.aliases, nil
}
func (s publishedBaselineStore) CurrentNoteMetadataPaths(context.Context) ([]string, error) {
	return s.baseline.paths, nil
}
func (s publishedBaselineStore) CurrentNoteMetadataRowsByPaths(_ context.Context, paths []string) (map[string]semdb.NoteMetadataRow, error) {
	rows := make(map[string]semdb.NoteMetadataRow, len(paths))
	for _, path := range paths {
		if row, ok := s.baseline.rows[path]; ok {
			rows[path] = row
		}
	}
	return rows, nil
}
