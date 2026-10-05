package query

import (
	"context"
	"errors"
	"os"

	semdb "github.com/atomicobject/rhizome/pkg/anchors/sqlite"
	"github.com/atomicobject/rhizome/pkg/noteformat"
	"github.com/atomicobject/rhizome/pkg/notemeta"
	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
)

// Sources outside the legacy snapshot contribute provider-authored links.
func (e *executor) addProviderInboundSectionNeighbors(ctx context.Context, cache *obsidian.NotePathCache, scannedSources map[string]*noteRecord, add func(string, string)) error {
	if e == nil || e.deps.Store == nil || cache == nil || add == nil {
		return nil
	}
	rows, err := e.deps.Store.CurrentNoteMetadataRows(ctx)
	if err != nil {
		return err
	}
	refs := make([]ontology.NodeRef, 0, len(rows))
	for _, row := range rows {
		if _, available := cache.NotePaths[row.Path]; !available {
			continue
		}
		if _, scanned := scannedSources[row.Path]; scanned {
			continue
		}
		provider, registered := e.deps.NoteFormats.Provider(noteformat.FormatID(row.FormatID))
		if !registered || !provider.Descriptor().Capabilities.Has(noteformat.CapabilityAuthoredLinkExtraction) || row.Projection.Status != semdb.NoteProjectionStatusCurrent || !e.deps.NoteFormats.CanProject(noteformat.FormatID(row.FormatID)) {
			continue
		}
		refs = append(refs, ontology.NodeRef{NotePath: row.Path, Kind: ontology.NodeKindNote})
	}
	projections, err := e.loaders.scope.Projections(ctx, refs)
	if err != nil {
		if err := ctx.Err(); err != nil {
			return err
		}
		if !isMissingProviderSource(err) {
			return err
		}
		// Keep the batched host reads and reuse successful projections, but
		// isolate a source that disappeared after indexing.
		projections = make([]*ontology.NodeProjection, len(refs))
		for i, ref := range refs {
			projection, err := e.loaders.scope.Projection(ctx, ref)
			if ctxErr := ctx.Err(); ctxErr != nil {
				return ctxErr
			}
			if err != nil {
				if isMissingProviderSource(err) {
					continue
				}
				return err
			}
			projections[i] = projection
		}
	}
	for i, projection := range projections {
		sourcePath := refs[i].NotePath
		if projection == nil || projection.RootSnapshot == nil {
			continue
		}
		facts := projection.RootSnapshot.Projection.Facts
		for _, link := range facts.Links {
			targetPath, ok := notemeta.ResolveProjectedLink(cache, sourcePath, facts.DocumentBase, link)
			if !ok {
				continue
			}
			key := resolvedTargetSectionKey(obsidian.ResolvedNoteTarget{Path: targetPath, Fragment: link.Fragment})
			if key != "" {
				add(inboundSectionNeighborKey(targetPath, key), sourcePath)
			}
		}
	}
	return nil
}

func isMissingProviderSource(err error) bool {
	return err != nil && (errors.Is(err, os.ErrNotExist) || err.Error() == obsidian.NoteDoesNotExistError)
}
