package planner

import (
	"strings"

	"github.com/atomicobject/rhizome/pkg/ontology/noderead"
	"github.com/atomicobject/rhizome/pkg/search"
	"github.com/atomicobject/rhizome/pkg/search/retrieval"
)

// autoExpansion composes bounded graph and reference follow-ups over the base lanes.
func (p *Planner) autoExpansion(spec search.QuerySpec, opts Options, signalProfile signalProfile, base []search.Retriever, requestedSubsystemOverview, constrainedFilters bool, getNodeScope func() *noderead.Scope) (*retrieval.AutoExpandRetriever, bool) {
	intent := spec.Intent
	hasText, hasSeeds := strings.TrimSpace(spec.Text) != "", len(spec.Seeds) > 0

	// For plain search (text-driven, no explicit seeds), optionally auto-expand via graph/refs
	// using top base hits as implicit seeds. This improves bridging without "true" query understanding.
	var autoExpand *retrieval.AutoExpandRetriever
	willAutoExpand := false
	autoExpandIntent := intent == search.IntentSearch || intent == search.IntentOverview || intent == search.IntentCodeForDocs || intent == search.IntentDocsForCode
	if !requestedSubsystemOverview && autoExpandIntent && hasText && !hasSeeds && len(base) > 0 && (opts.EnableGraph || opts.EnableRefs) && !constrainedFilters {
		var graphExp search.Retriever
		// Cheap graph expansion: outgoing wikilinks from seed notes only.
		// Full graph analysis (communities/HITS) is reserved for explicit seeded graph queries.
		if opts.EnableGraph && p.Deps.IntelStore != nil {
			graphExp = &retrieval.OutgoingLinkRetriever{
				Store: p.Deps.IntelStore,
				Limit: 50,
			}
		}
		var refsExp search.Retriever
		if opts.EnableRefs && signalProfile.RefsReady && !signalProfile.SparseRefs {
			refsExp = &retrieval.CompositeRetriever{
				Label: "refs",
				Retrievers: []search.Retriever{
					&retrieval.DocLinksRetriever{Store: p.Deps.IntelStore, VaultPath: p.Deps.VaultPath},
					&retrieval.CodeAnchorNotesRetriever{Store: p.Deps.IntelStore, VaultPath: p.Deps.VaultPath},
					&retrieval.CodeAnchorRefsRetriever{Store: p.Deps.IntelStore, VaultPath: p.Deps.VaultPath},
					&retrieval.AnchorGraphRetriever{Store: p.Deps.IntelStore},
				},
			}
		}
		var ontologyExp search.Retriever
		if opts.EnableGraph && signalProfile.OntologyReady && (signalProfile.OntologyDense || !signalProfile.CodeFirstDocs) {
			ontologyExp = &retrieval.OntologyRetriever{
				Store:          p.Deps.IntelStore,
				Scope:          getNodeScope(),
				IncludeAmbient: true,
				Limit:          max(30, spec.Limits.Total),
			}
		}
		var diffusionExp search.Retriever
		if signalProfile.RefsReady && opts.EnableGraph && !signalProfile.SparseRefs && !signalProfile.ImmatureVault {
			diffusionExp = &retrieval.GraphPPRRetriever{
				Store:      p.Deps.IntelStore,
				GraphFacts: getNodeScope(),
				Options: retrieval.GraphPPROptions{
					Depth:            2,
					PerNodeEdgeLimit: 200,
					MaxNodes:         700,
					MaxEdges:         3500,
					// Heuristic: diffusion should be able to add a meaningful neighborhood,
					// but stay bounded for latency. spec.Limits.Total is the overall result
					// cap; in some callers it can be an overfetch size,
					// so we scale conservatively.
					ReturnLimit: func() int {
						base := 25
						if spec.Limits.Total > 0 {
							base = max(base, min(150, spec.Limits.Total/5))
						}
						return base
					}(),
					MinScore:         0.05,
					IncludeCalls:     true,
					IncludeSeedNodes: true,
					MaxNotes:         0,
					MaxFiles:         0,
				},
			}
		}
		willAutoExpand = graphExp != nil || refsExp != nil || ontologyExp != nil || diffusionExp != nil
		maxSeedsTotal := 8
		maxSeedsPerKind := 4
		if intent == search.IntentCodeForDocs {
			maxSeedsTotal = 12
			maxSeedsPerKind = 6
		}
		if intent == search.IntentOverview {
			maxSeedsTotal = 10
			maxSeedsPerKind = 5
		}
		autoExpand = &retrieval.AutoExpandRetriever{
			Base:            base,
			Graph:           graphExp,
			Refs:            refsExp,
			Ontology:        ontologyExp,
			Diffusion:       diffusionExp,
			MaxSeedsTotal:   maxSeedsTotal,
			MaxSeedsPerKind: maxSeedsPerKind,
		}
		return autoExpand, willAutoExpand
	}
	return nil, false
}
