package planner

import (
	"context"

	vaultgraph "github.com/atomicobject/rhizome/pkg/graph"
	"github.com/atomicobject/rhizome/pkg/markdownsyntax"
	"github.com/atomicobject/rhizome/pkg/ontology/noderead"
	"github.com/atomicobject/rhizome/pkg/search"
	"github.com/atomicobject/rhizome/pkg/search/retrieval"
	"github.com/atomicobject/rhizome/pkg/vault/repoconfig"
)

// seedRetrievers binds explicit local, graph, and reference evidence to one node scope.
func (p *Planner) seedRetrievers(ctx context.Context, spec search.QuerySpec, opts Options, requestedSubsystemOverview, constrainedFilters bool, getNodeScope func() *noderead.Scope) []search.Retriever {
	var retrieversOut []search.Retriever
	hasSeeds := len(spec.Seeds) > 0

	explicitGraph := opts.EnableGraph && hasSeeds && graphAvailable(p.Deps.VaultDef, p.Deps.NoteReader)
	explicitRefs := opts.EnableRefs && hasSeeds && p.Deps.IntelStore != nil
	if len(spec.ExplicitSeedPaths) > 0 {
		retrieversOut = append(retrieversOut, &retrieval.ExplicitSeedRetriever{
			VaultPath:           p.Deps.VaultPath,
			Limit:               max(4, min(12, spec.Limits.Total)),
			IncludeSiblingCode:  requestedSubsystemOverview,
			IncludeSiblingTests: requestedSubsystemOverview,
		})
	}
	if requestedSubsystemOverview {
		retrieversOut = append(retrieversOut, &retrieval.LocalDocsRetriever{
			VaultPath:      p.Deps.VaultPath,
			DocPatterns:    p.Deps.DocPatterns,
			MaxEmptyLevels: repoconfig.FileContextConfigDefaults.MaxEmptyLevels,
			SubmoduleDepth: 2,
			Limit:          max(8, min(24, spec.Limits.Total)),
		})
		if len(spec.ExplicitSeedPaths) > 0 {
			retrieversOut = append(retrieversOut, &retrieval.TestsForCodeRetriever{
				Store:     p.Deps.IntelStore,
				VaultPath: p.Deps.VaultPath,
				Limit:     max(4, min(12, spec.Limits.Total)),
			})
		}
	}
	if explicitGraph && !constrainedFilters {
		if p.Deps.IntelStore != nil && hasNoteSeeds(spec.Seeds) {
			if ontologyRetrieverReady(ctx, p.Deps) {
				retrieversOut = append(retrieversOut, &retrieval.OntologyRetriever{
					Store:          p.Deps.IntelStore,
					Scope:          getNodeScope(),
					IncludeAmbient: true,
					Limit:          max(25, spec.Limits.Total),
				})
			}
		}
		retrieversOut = append(retrieversOut, &retrieval.GraphRetriever{
			VaultDef:     p.Deps.VaultDef,
			NoteReader:   graphNoteReader(p.Deps),
			Store:        p.Deps.IntelStore,
			SourcePolicy: opts.GraphSource,
			Options: vaultgraph.GraphAnalysisOptions{
				WikilinkOptions: markdownsyntax.WikilinkOptions{},
			},
		})
	}
	if explicitRefs && !constrainedFilters {
		retrieversOut = append(retrieversOut, &retrieval.CompositeRetriever{
			Label: "refs",
			Retrievers: []search.Retriever{
				&retrieval.DocLinksRetriever{Store: p.Deps.IntelStore, VaultPath: p.Deps.VaultPath},
				&retrieval.CodeAnchorNotesRetriever{Store: p.Deps.IntelStore, VaultPath: p.Deps.VaultPath},
				&retrieval.CodeAnchorRefsRetriever{Store: p.Deps.IntelStore, VaultPath: p.Deps.VaultPath},
				&retrieval.AnchorGraphRetriever{Store: p.Deps.IntelStore},
			},
		})
	}
	return retrieversOut
}
