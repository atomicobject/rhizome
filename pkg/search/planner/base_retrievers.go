package planner

import (
	"context"
	"strings"

	"github.com/atomicobject/rhizome/pkg/search"
	"github.com/atomicobject/rhizome/pkg/search/retrieval"
	semdb "github.com/atomicobject/rhizome/pkg/store"
)

// baseRetrievers selects independent text and vector lanes.
func (p *Planner) baseRetrievers(spec search.QuerySpec, opts Options) []search.Retriever {
	hasText, hasSeeds := strings.TrimSpace(spec.Text) != "", len(spec.Seeds) > 0
	intent := spec.Intent
	var base []search.Retriever
	if opts.EnableVector {
		if hasText && vectorAvailable(p.Deps.Semantic) {
			base = append(base, &retrieval.VectorRetriever{Semantic: p.Deps.Semantic})
		}
		if hasSeeds && seedVectorAvailable(p.Deps.Semantic) {
			base = append(base, &retrieval.SeedVectorRetriever{Semantic: p.Deps.Semantic})
		}
	}
	if hasText && graphAvailable(p.Deps.VaultDef, p.Deps.NoteReader) && spec.Filters.AllowsType("note") {
		var pathSource retrieval.NotePathSource
		if p.Deps.IntelStore != nil {
			pathSource = retrieval.NotePathSourceFunc(func(ctx context.Context) ([]string, error) {
				if p.Deps.Corpus != nil {
					return p.Deps.Corpus.NotePaths, nil
				}
				return p.Deps.IntelStore.IntelNotePaths(ctx)
			})
		}
		base = append(base, &retrieval.NoteLexicalRetriever{
			VaultDef:   p.Deps.VaultDef,
			NoteReader: p.Deps.NoteReader,
			PathSource: pathSource,
			CatalogSource: func() retrieval.NoteCatalogSource {
				if p.Deps.IntelStore == nil {
					return nil
				}
				return retrieval.NoteCatalogSourceFunc(func(ctx context.Context) ([]retrieval.NoteCatalogEntry, error) {
					var rows []semdb.NoteMetadataRow
					if p.Deps.Corpus != nil {
						rows = p.Deps.Corpus.Notes
					} else {
						var err error
						rows, err = p.Deps.IntelStore.CurrentNoteMetadataRows(ctx)
						if err != nil {
							return nil, err
						}
					}
					out := make([]retrieval.NoteCatalogEntry, 0, len(rows))
					for _, row := range rows {
						out = append(out, retrieval.NoteCatalogEntry{Path: row.Path, Title: row.Title})
					}
					return out, nil
				})
			}(),
			TypePathSource: p.noteTypePathSource(),
		})
	}
	if hasText && search.IsBroadIntent(intent) && p.Deps.IntelStore != nil && spec.Filters.AllowsType("note") {
		base = append(base, &retrieval.LinkTextRetriever{Store: p.Deps.IntelStore, TypePathSource: p.noteTypePathSource()})
	}
	if opts.EnableIntel && hasText && search.IsBroadIntent(intent) && p.Deps.IntelStore != nil && spec.Filters.AllowsType("code") {
		base = append(base, &retrieval.SymbolProbeRetriever{
			Store: p.Deps.IntelStore,
			Limit: max(12, min(40, spec.Limits.Total)),
		})
	}
	if opts.EnableIntel && hasText && p.Deps.IntelStore != nil {
		base = append(base, &retrieval.IntelLexicalRetriever{Store: p.Deps.IntelStore, VaultPath: p.Deps.VaultPath})
		if spec.Filters.AllowsType("code") && retrieval.ShouldQueryRationaleEvidence(spec) {
			base = append(base, &retrieval.RationaleFTSRetriever{Store: p.Deps.IntelStore, VaultPath: p.Deps.VaultPath})
		}
	}
	return base
}
