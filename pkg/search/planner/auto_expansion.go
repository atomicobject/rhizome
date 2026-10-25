package planner

import (
	"strings"

	"github.com/atomicobject/rhizome/pkg/search"
	"github.com/atomicobject/rhizome/pkg/search/retrieval"
)

// autoExpansion composes bounded reference follow-ups over the base lanes.
func (p *Planner) autoExpansion(spec search.QuerySpec, opts Options, signalProfile signalProfile, base []search.Retriever, requestedSubsystemOverview, constrainedFilters bool) (*retrieval.AutoExpandRetriever, bool) {
	intent := spec.Intent
	hasText, hasSeeds := strings.TrimSpace(spec.Text) != "", len(spec.Seeds) > 0

	// For plain search (text-driven, no explicit seeds), auto-expand by following
	// references from top base hits used as implicit seeds. Graph, ontology, and
	// PPR expansion are not used here: graph features stay seed-only for text
	// queries by measurement (see docs/reference/subsystems/search.md, "Graph
	// features stay seed-only for text queries").
	autoExpandIntent := intent == search.IntentSearch || intent == search.IntentOverview || intent == search.IntentCodeForDocs || intent == search.IntentDocsForCode
	if requestedSubsystemOverview || !autoExpandIntent || !hasText || hasSeeds || len(base) == 0 || !opts.EnableRefs || constrainedFilters {
		return nil, false
	}
	var refsExp search.Retriever
	if signalProfile.RefsReady && !signalProfile.SparseRefs {
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
	return &retrieval.AutoExpandRetriever{
		Base:            base,
		Refs:            refsExp,
		MaxSeedsTotal:   maxSeedsTotal,
		MaxSeedsPerKind: maxSeedsPerKind,
	}, refsExp != nil
}
