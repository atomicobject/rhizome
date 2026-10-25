// Package planner translates QuerySpec inputs into executable Plans.
//
// Responsibilities:
//   - Seed expansion: directory → files, file → linked notes, file → anchors
//   - Retriever selection: based on intent, available indexes, and query shape
//   - Weight tuning: intent-specific ranking weights
//   - Auto-expansion: text-only queries use top base hits as implicit seeds
//
// The planner does NOT execute retrieval; it produces a Plan that the search.Service runs.
//
// Docs: [Search (Hub)](docs/hubs/Search (Hub).md), [Search - Seed expansion strategies](docs/reference/analysis/Search - Seed expansion strategies.md)
package planner

import (
	"context"
	"errors"
	"strings"

	"github.com/atomicobject/rhizome/pkg/ontology/noderead"
	"github.com/atomicobject/rhizome/pkg/search"
	"github.com/atomicobject/rhizome/pkg/search/relevance"
	"github.com/atomicobject/rhizome/pkg/search/rerank"
	vaultconfig "github.com/atomicobject/rhizome/pkg/vault/config"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
)

// Plan builds a retrieval plan for the given query.
//
// It repairs/normalizes target state, expands bounded seeds, selects retrievers,
// configures intent weights, and wires optional shapers/packers. Returns an
// error if no retrievers can be constructed.
func (p *Planner) Plan(ctx context.Context, spec search.QuerySpec) (Plan, error) {
	// Docs: [[unified-search-answer-architecture#^spec-0035-us3-ac1]].
	// RATIONALE: planner owns query repair, seed expansion, retriever selection,
	// ranking weights, shapers, and optional packer wiring; search.Service owns execution.
	opts := p.Options
	if opts == (Options{}) {
		opts = Options{EnableVector: true, EnableIntel: true, EnableGraph: true, EnableRefs: true, MaxPerOwner: 3}
	}
	if opts.MaxPerOwner <= 0 {
		opts.MaxPerOwner = 3
	}
	intent := spec.Intent
	if intent == "" {
		intent = search.IntentSearch
	}
	requestedSubsystemOverview := intent == search.IntentSubsystemOverview
	spec.Intent = intent
	spec, _ = search.RepairQuerySpec(p.Deps.VaultPath, spec)
	spec, _ = search.ResolveQuerySpecTargets(ctx, p.Deps.VaultPath, p.Deps.IntelStore, spec)
	intent = spec.Intent
	signalProfile := inspectSignalProfile(ctx, p.Deps, opts, spec)
	if profile, ok := resolveIntentProfile(intent); ok {
		opts = applyIntentProfile(opts, profile)
	}

	// Directory seeds can explode into very large match sets (especially for code embeddings).
	// Docs: [[Search - Seed expansion strategies#^search-seed-explicit-expansion-contract]]
	// RATIONALE: expand them early into a small representative file/anchor set
	// so downstream retrievers stay fast, targeted, and explainable.
	if len(spec.Seeds) > 0 {
		maxFilesPerDir := 12
		if spec.Limits.Total > 0 {
			maxFilesPerDir = min(maxFilesPerDir, max(1, spec.Limits.Total))
		}
		spec.Seeds = expandDirectoryFileSeeds(ctx, spec.Seeds, p.Deps.VaultDef, p.Deps.IntelStore, maxFilesPerDir, opts.DirectorySeedExpansion)
	}

	// Expand seeds with note handles derived from code doc links (e.g., file/directory seeds -> linked notes)
	constrainedFilters := hasConstrainedFilters(spec.Filters)
	if opts.EnableRefs && p.Deps.IntelStore != nil && len(spec.Seeds) > 0 && !constrainedFilters {
		expanded := expandNoteSeedsFromDocLinks(ctx, spec.Seeds, p.Deps.IntelStore, p.Deps.VaultPath, 50)
		spec.Seeds = append(spec.Seeds, expanded...)
	}
	if p.Deps.IntelStore != nil && len(spec.Seeds) > 0 && !constrainedFilters {
		expanded := expandAnchorSeedsFromFiles(ctx, spec.Seeds, p.Deps.IntelStore, p.Deps.VaultPath, 120)
		spec.Seeds = append(spec.Seeds, expanded...)
	}

	var nodeScope *noderead.Scope
	getNodeScope := func() *noderead.Scope {
		if nodeScope == nil && p.Deps.IntelStore != nil {
			// Build the seeded ontology retriever's noderead scope lazily. It is
			// request-scoped, so cache hits help this search run without leaking
			// graph/profile assumptions into later searches.
			nodeScope = noderead.NewService(p.Deps.VaultDef, &obsidian.Note{}, p.Deps.IntelStore, p.Deps.OntologySchema).WithNoteFormats(p.Deps.NoteFormats).NewScope(noderead.ScopeOptions{})
		}
		return nodeScope
	}
	hasText := strings.TrimSpace(spec.Text) != ""
	hasSeeds := len(spec.Seeds) > 0
	seedOnly := hasSeeds && !hasText

	base := p.baseRetrievers(spec, opts)
	retrieversOut := p.seedRetrievers(ctx, spec, opts, requestedSubsystemOverview, constrainedFilters, getNodeScope)
	retrieversOut = append(retrieversOut, p.targetRetrievers(spec)...)
	autoExpand, willAutoExpand := p.autoExpansion(spec, opts, signalProfile, base, requestedSubsystemOverview, constrainedFilters)
	if autoExpand != nil {
		retrieversOut = append(retrieversOut, autoExpand)
	} else {
		retrieversOut = append(retrieversOut, base...)
	}

	if len(retrieversOut) == 0 {
		return Plan{}, errors.New("planner produced no retrievers (check intent, query/seeds, and enabled indexes)")
	}

	weights := adjustWeightsForSignals(weightsForIntent(intent, hasText, hasSeeds, willAutoExpand), signalProfile, intent)
	if autoExpand != nil {
		autoExpand.ApproxWeights = (&search.WeightedRanker{Weights: weights}).ChannelWeights()
		autoExpand.MinSeedSpecificity = 0.25
	}
	if seedOnly {
		if weights.Semantic < 0.5 {
			weights.Semantic = 0.5
		}
		if weights.Graph == 0 {
			weights.Graph = 0.4
		}
		if weights.Refs == 0 {
			weights.Refs = 0.4
		}
	}
	scorer := &search.WeightedRanker{
		Weights:     weights,
		MaxPerOwner: opts.MaxPerOwner,
	}
	var ranker search.Ranker = scorer
	if hasText && specificityWeightForIntent(intent) > 0 {
		ranker = &relevance.SpecificityRanker{Base: ranker}
	}
	var shape search.Shaper
	if intent == search.IntentSubsystemOverview {
		ranker = &relevance.LocalityRanker{Base: ranker}
		shape = &search.CompositeShaper{Shapers: []search.Shaper{
			&search.SubsystemOverviewShaper{},
			&search.OverviewEvidenceShaper{},
		}}
	} else if intent == search.IntentOverview {
		shape = &search.OverviewEvidenceShaper{}
	}

	var enrichers []search.CandidateAdjuster
	if p.Deps.IntelStore != nil && weights.Graph > 0 {
		enrichers = []search.CandidateAdjuster{&relevance.GraphDocAdjuster{Store: p.Deps.IntelStore}, &relevance.GraphAnchorAdjuster{Store: p.Deps.IntelStore}}
	}
	var adjusters []search.ResultAdjuster
	if reranker, cfg, enabled := rerank.NewFromEnv(vaultconfig.FromContext(ctx)); enabled {
		adjusters = append(adjusters, &relevance.RerankAdjuster{Reranker: reranker, Model: cfg.Model})
	}
	var rollupper search.Rollupper
	if p.Deps.IntelStore != nil {
		rollup := &search.CodeRollupper{Intel: p.Deps.IntelStore, Root: p.Deps.VaultPath}
		if intent == search.IntentOverview || intent == search.IntentSubsystemOverview {
			rollup.SmallFileBytes, rollup.SpreadRatio, rollup.SpreadCount = 20000, 0.7, 2
		}
		rollupper = rollup
	}

	var packer search.Packer
	if spec.Budget.Chars > 0 {
		packer = p.Deps.Packer
	}

	return Plan{
		Retrievers: retrieversOut,
		Ranker:     ranker,
		Scorer:     scorer,
		Rollupper:  rollupper,
		Adjusters:  adjusters,
		Enrichers:  enrichers,
		Shaper:     shape,
		Packer:     packer,
	}, nil
}
