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
	"fmt"
	"os"
	"strings"

	semdb "github.com/atomicobject/rhizome/pkg/anchors/sqlite"
	"github.com/atomicobject/rhizome/pkg/app/presentation"
	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/ontology/noderead"
	"github.com/atomicobject/rhizome/pkg/paths"
	"github.com/atomicobject/rhizome/pkg/search"
	"github.com/atomicobject/rhizome/pkg/search/knowledge"
	"github.com/atomicobject/rhizome/pkg/search/relevance"
	"github.com/atomicobject/rhizome/pkg/search/retrieval"
	"github.com/atomicobject/rhizome/pkg/search/semantic"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
)

// Deps are dependencies required by the planner: semantic index, intel store, vault path,
// and note manager.
//
// Required fields:
//   - Semantic: must be non-nil if EnableVector is true (vector/seed-vector retrievers)
//   - VaultPath: vault root path for seed expansion and path normalization
//   - VaultDef: vault definition for graph/lexical retrievers
//   - NoteReader: must be non-nil if EnableGraph is true (graph/lexical retrievers)
//
// Optional fields (nil disables related retrievers):
//   - IntelStore: enables intel-based retrievers (CodeAnchorNotesRetriever, CodeAnchorRefsRetriever, etc.)
//   - CodeIndex: enables FTS chunk body retrieval when UseFTSBody is true
type Deps struct {
	Semantic   *semantic.Searcher
	IntelStore *semdb.Store
	CodeIndex  presentation.ChunkBodyReader // Optional: code embeddings index for FTS chunk body retrieval

	VaultPath       string
	VaultDef        obsidian.VaultDefinition
	NoteReader      obsidian.NoteReader
	GraphNoteReader obsidian.NoteReader
	DocPatterns     []string
	OntologySchema  *ontology.Schema
}

// DirectorySeedExpansionPolicy controls whether directory seed expansion may
// inspect the filesystem after indexed prefix lookups return no candidates.
type DirectorySeedExpansionPolicy uint8

const (
	DirectorySeedExpansionAllowFilesystemFallback DirectorySeedExpansionPolicy = iota
	DirectorySeedExpansionIndexedOnly
)

// Options control which retrieval strategies are enabled and per-owner result limits.
//
// Default behavior: if Options{} is zero-valued, all strategies are enabled with MaxPerOwner=3.
//
// Strategy flags:
//   - EnableVector: enables VectorRetriever and SeedVectorRetriever (requires Deps.Semantic)
//   - EnableIntel: enables IntelLexicalRetriever (requires Deps.IntelStore)
//   - EnableGraph: enables GraphRetriever and OutgoingLinkRetriever (requires Deps.NoteReader)
//   - EnableRefs: enables refs-based retrievers (DocLinksRetriever, CodeAnchorNotesRetriever, etc.; requires Deps.IntelStore)
//
// Packing/rendering:
//   - UseFTSBody: if true, packer reads chunk bodies from FTS index instead of filesystem (requires Deps.CodeIndex)
//
// Diversity:
//   - MaxPerOwner: limits how many results come from the same owner (note/file); default 3, minimum 1.
//     Prevents single notes/files from dominating results. See [Search (Hub)](docs/hubs/Search (Hub).md) for details.
type Options struct {
	EnableVector bool
	EnableIntel  bool
	EnableGraph  bool
	EnableRefs   bool
	UseFTSBody   bool // If true, packer uses FTS chunk body instead of reading files

	DirectorySeedExpansion DirectorySeedExpansionPolicy
	GraphSource            retrieval.GraphSourcePolicy

	MaxPerOwner int
}

// Plan is the output of planning: a list of retrievers to run, a ranker to blend scores,
// and an optional packer for rendering results.
//
// The planner constructs retrievers based on query shape (text vs seeds), available indexes,
// and enabled options. Retrievers run concurrently (or progressively with deadline) in the
// order specified. The ranker blends evidence from all retrievers using intent-tuned weights.
//
// Packer is nil unless spec.Budget.Chars > 0 (caller requested packed context rendering).
type Plan struct {
	Retrievers []search.Retriever
	Ranker     search.Ranker
	Shaper     search.Shaper
	Packer     search.Packer
}

// Planner translates QuerySpec into a Plan based on available indexes and intent.
//
// The planner does NOT execute retrieval; it only builds a Plan that search.Service.Search()
// runs. Responsibilities include:
//   - Seed expansion (directory → files, file → notes/anchors)
//   - Retriever selection (based on options, query shape, available indexes)
//   - Weight tuning (intent-specific ranking weights)
//   - Auto-expansion (text-only queries use top hits as implicit seeds)
//
// See [Search (Hub)](docs/hubs/Search (Hub).md), [Search - Seed expansion strategies](docs/reference/analysis/Search - Seed expansion strategies.md), and [Search - Intent and weight tuning](docs/reference/domain/Search - Intent and weight tuning.md) for details.
type Planner struct {
	Deps    Deps
	Options Options
}

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
		opts = Options{EnableVector: true, EnableIntel: true, EnableGraph: true, EnableRefs: true, MaxPerOwner: 3, UseFTSBody: false}
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

	var retrieversOut []search.Retriever
	var autoExpand *retrieval.AutoExpandRetriever
	var nodeScope *noderead.Scope
	getNodeScope := func() *noderead.Scope {
		if nodeScope == nil && p.Deps.IntelStore != nil {
			// Share one noderead scope across ontology/diffusion retrievers in this
			// plan. It is request-scoped, so cache hits help this search run without
			// leaking graph/profile assumptions into later searches.
			nodeScope = noderead.NewService(p.Deps.VaultDef, &obsidian.Note{}, p.Deps.IntelStore, p.Deps.OntologySchema).NewScope(ctx, noderead.ScopeOptions{})
		}
		return nodeScope
	}
	hasText := strings.TrimSpace(spec.Text) != ""
	hasSeeds := len(spec.Seeds) > 0
	seedOnly := hasSeeds && !hasText

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
					rows, err := p.Deps.IntelStore.CurrentNoteMetadataRows(ctx)
					if err != nil {
						return nil, err
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
			MaxEmptyLevels: obsidian.FileContextConfigDefaults.MaxEmptyLevels,
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
			if ontologyRetrieverReady(ctx, p.Deps.IntelStore) {
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
			Options: obsidian.GraphAnalysisOptions{
				WikilinkOptions: obsidian.WikilinkOptions{},
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

	switch intent {
	case search.IntentGoToDef:
		if p.Deps.IntelStore != nil {
			retrieversOut = append(retrieversOut, &retrieval.DefinitionRetriever{
				Store:     p.Deps.IntelStore,
				VaultPath: p.Deps.VaultPath,
				Limit:     spec.Limits.Total,
			})
		}
	case search.IntentTestsForCode:
		retrieversOut = append(retrieversOut, &retrieval.TestsForCodeRetriever{
			Store:     p.Deps.IntelStore,
			VaultPath: p.Deps.VaultPath,
			Limit:     spec.Limits.Total,
		})
	case search.IntentImplementers:
		if p.Deps.IntelStore != nil {
			retrieversOut = append(retrieversOut, &retrieval.ImplementersRetriever{
				Store: p.Deps.IntelStore,
				Limit: spec.Limits.Total,
			})
		}
	case search.IntentRefactorImpact:
		if p.Deps.IntelStore != nil {
			// Impact is target-shaped evidence collection: callers describe inbound
			// blast radius, while callees describe dependencies that constrain edits.
			retrieversOut = append(retrieversOut,
				&retrieval.CallEdgesRetriever{
					Store:        p.Deps.IntelStore,
					Mode:         retrieval.CallEdgesCallers,
					Limit:        spec.Limits.Total,
					PerSeedLimit: 8,
				},
				&retrieval.CallEdgesRetriever{
					Store:        p.Deps.IntelStore,
					Mode:         retrieval.CallEdgesCallees,
					Limit:        spec.Limits.Total,
					PerSeedLimit: 8,
				},
			)
		}
		retrieversOut = append(retrieversOut, &retrieval.TestsForCodeRetriever{
			Store:     p.Deps.IntelStore,
			VaultPath: p.Deps.VaultPath,
			Limit:     spec.Limits.Total,
		})
	case search.IntentFindUsages, search.IntentCallers, search.IntentCallees:
		if p.Deps.IntelStore != nil {
			mode := retrieval.CallEdgesCallers
			if intent == search.IntentCallees {
				mode = retrieval.CallEdgesCallees
			}
			retrieversOut = append(retrieversOut, &retrieval.CallEdgesRetriever{
				Store:        p.Deps.IntelStore,
				Mode:         mode,
				Limit:        spec.Limits.Total,
				PerSeedLimit: 8,
			})
		}
	}

	// For plain search (text-driven, no explicit seeds), optionally auto-expand via graph/refs
	// using top base hits as implicit seeds. This improves bridging without "true" query understanding.
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
		retrieversOut = append(retrieversOut, autoExpand)
	} else {
		retrieversOut = append(retrieversOut, base...)
	}

	if len(retrieversOut) == 0 {
		return Plan{}, errors.New("planner produced no retrievers (check intent, query/seeds, and enabled indexes)")
	}

	weights := adjustWeightsForSignals(weightsForIntent(intent, hasText, hasSeeds, willAutoExpand), signalProfile, intent)
	if autoExpand != nil {
		autoExpand.ApproxWeights = approxWeightsForRanker(weights)
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
	var ranker search.Ranker = &relevance.WeightedRanker{
		Weights:     weights,
		MaxPerOwner: opts.MaxPerOwner,
	}
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

	var packer search.Packer
	if spec.Budget.Chars > 0 {
		ontologySchema := p.Deps.OntologySchema
		if ontologySchema == nil && strings.TrimSpace(p.Deps.VaultPath) != "" {
			if schema, err := ontology.LoadSchema(p.Deps.VaultPath); err == nil {
				ontologySchema = schema
			}
		}
		packer = &presentation.DefaultPacker{
			VaultPath:      p.Deps.VaultPath,
			CodeRoot:       p.Deps.VaultPath,
			Intel:          p.Deps.IntelStore,
			CodeIndex:      p.Deps.CodeIndex,
			UseFTSBody:     opts.UseFTSBody,
			VaultDef:       p.Deps.VaultDef,
			NoteReader:     p.Deps.NoteReader,
			OntologySchema: ontologySchema,
		}
	}

	return Plan{
		Retrievers: retrieversOut,
		Ranker:     ranker,
		Shaper:     shape,
		Packer:     packer,
	}, nil
}

func hasConstrainedFilters(filters search.Filters) bool {
	return len(filters.Types) > 0 || len(filters.PathPrefixes) > 0 || len(filters.NoteTypes) > 0 || filters.TestsOnly || filters.ExcludeTests
}

func approxWeightsForRanker(w relevance.Weights) map[search.EvidenceChannel]float64 {
	return map[search.EvidenceChannel]float64{
		search.EvidenceChannelSemantic:           w.Semantic,
		search.EvidenceChannelLexical:            w.Lexical,
		search.EvidenceChannelGraph:              w.Graph,
		search.EvidenceChannelRefs:               w.Refs,
		search.EvidenceChannelOntologyStructural: w.OntologyStructural,
		search.EvidenceChannelOntologyAmbient:    w.OntologyAmbient,
		search.EvidenceChannelRecency:            w.Recency,
		search.EvidenceChannelSeedLocality:       w.SeedLocality,
		search.EvidenceChannelSpecificity:        w.Specificity,
	}
}

func specificityWeightForIntent(intent search.Intent) float64 {
	if search.IsPrecisionIntent(intent) {
		return 0
	}
	switch intent {
	case search.IntentSearch, search.IntentOverview, search.IntentSubsystemOverview, search.IntentDocsForCode, search.IntentCodeForDocs, search.IntentExplainSymbol:
		return 1
	default:
		return 0.25
	}
}

func graphNoteReader(deps Deps) obsidian.NoteReader {
	if deps.GraphNoteReader != nil {
		return deps.GraphNoteReader
	}
	return deps.NoteReader
}

func ontologyRetrieverReady(ctx context.Context, store *semdb.Store) bool {
	if store == nil {
		return false
	}
	ontoState, err := store.GetOntologySchemaState(ctx)
	if err != nil || !ontoState.Ready || ontoState.MaterializationVersion != ontology.OntologyMaterializationVersion {
		return false
	}
	noteState, err := store.GetNoteMetadataState(ctx)
	if err != nil || !noteState.Ready {
		return false
	}
	return ontoState.NotesHash != "" && ontoState.NotesHash == noteState.NotesHash
}

func hasNoteSeeds(seeds []knowledge.Handle) bool {
	for _, seed := range seeds {
		if seed.Kind == knowledge.KindNote || seed.Kind == knowledge.KindNoteChunk {
			return true
		}
	}
	return false
}

// weightsForIntent returns ranking weights tuned for the given intent. Used as the
// fallback when an intent profile doesn't provide explicit weights. Weights vary based
// on query shape (text-only vs seeded). See [Search - Intent and weight tuning](docs/reference/domain/Search - Intent and weight tuning.md) for details.
//
// Query shape adjustments:
//   - Seed-only (hasSeeds=true, hasText=false): Vector boosted to 0.5, Graph/Refs enabled at 0.4
//   - Text-only (hasText=true, hasSeeds=false): Auto-expansion uses implicit seeds; Graph/Refs weights reduced (0.15/0.25)
//   - Implicit seeds (implicitSeeds=true): Graph/Refs weights slightly higher (0.20/0.40) than pure text-only
//
// Intent-specific behavior:
//   - related_to_seed: disables Vector/Lexical if no text; disables Graph/Refs if no seeds
//   - docs_for_code: boosts Refs to 1.2; reduces Refs to 0.6 if no seeds
//   - code_for_docs: boosts Refs to 1.2; reduces Refs to 0.4 if no seeds
//   - consolidation: disables Vector/Lexical if no text
func weightsForIntent(intent search.Intent, hasText, hasSeeds, implicitSeeds bool) relevance.Weights {
	switch intent {
	case search.IntentRelatedToSeed:
		w := relevance.Weights{Semantic: 0.4, Lexical: 0.3, Graph: 1.0, Refs: 0.9, OntologyStructural: 0.7, OntologyAmbient: 0.4, Recency: 0.2, SeedLocality: 0.4, Specificity: 0.35}
		if !hasText {
			w.Semantic = 0
			w.Lexical = 0
		}
		if !hasSeeds {
			w.Graph = 0
			w.Refs = 0
			w.OntologyStructural = 0
			w.OntologyAmbient = 0
		}
		return w
	case search.IntentDocsForCode:
		w := relevance.Weights{Semantic: 0.5, Lexical: 0.9, Graph: 0.15, Refs: 1.2, OntologyStructural: 0.45, OntologyAmbient: 0.15, Recency: 0.2, SeedLocality: 0.55, Specificity: 0.7}
		if !hasSeeds {
			w.Refs = 0.6
			w.SeedLocality = 0.35
		}
		if !hasText {
			w.Semantic = 0.2
		}
		return w
	case search.IntentCodeForDocs:
		w := relevance.Weights{Semantic: 0.6, Lexical: 0.6, Graph: 0.2, Refs: 1.2, OntologyStructural: 0.35, OntologyAmbient: 0.12, Recency: 0.2, SeedLocality: 0.45, Specificity: 0.75}
		if !hasSeeds {
			w.Refs = 0.4
		}
		return w
	case search.IntentConsolidation:
		w := relevance.Weights{Semantic: 0.4, Lexical: 0.2, Graph: 1.0, Refs: 0.4, OntologyStructural: 0.8, OntologyAmbient: 0.45, Recency: 0.2, SeedLocality: 0.25, Specificity: 0.45}
		if !hasText {
			w.Semantic = 0
			w.Lexical = 0
		}
		return w
	case search.IntentOverview:
		w := relevance.Weights{Semantic: 1.0, Lexical: 0.3, Graph: 0.5, Refs: 0.6, OntologyStructural: 0.55, OntologyAmbient: 0.25, Recency: 0.2, SeedLocality: 0.35, Specificity: 1.0}
		if !hasText {
			w.Semantic = 0.5
			w.Lexical = 0.1
		}
		if !hasSeeds {
			w.Graph = 0.2
			w.Refs = 0.3
			w.OntologyStructural = 0.25
			w.OntologyAmbient = 0.1
		}
		return w
	case search.IntentSubsystemOverview:
		w := relevance.Weights{Semantic: 0.8, Lexical: 0.5, Graph: 0.35, Refs: 1.0, OntologyStructural: 0.65, OntologyAmbient: 0.25, Recency: 0.2, SeedLocality: 0.85, Specificity: 0.8}
		if !hasText {
			w.Semantic = 0.45
			w.Lexical = 0.15
		}
		if !hasSeeds {
			w.Graph = 0
			w.Refs = 0.4
			w.OntologyStructural = 0.2
			w.OntologyAmbient = 0.05
		}
		return w
	case search.IntentGoToDef:
		w := relevance.Weights{Semantic: 0.0, Lexical: 0.6, Graph: 0.0, Refs: 1.4, OntologyStructural: 0.1, OntologyAmbient: 0.0, Recency: 0.1, SeedLocality: 0.2, Specificity: 0.1}
		if !hasText {
			w.Lexical = 0.3
		}
		return w
	case search.IntentFindUsages, search.IntentCallers, search.IntentCallees:
		w := relevance.Weights{Semantic: 0.0, Lexical: 0.5, Graph: 0.2, Refs: 1.2, OntologyStructural: 0.15, OntologyAmbient: 0.0, Recency: 0.1, SeedLocality: 0.15, Specificity: 0.15}
		if !hasSeeds {
			w.Graph = 0.3
			w.Refs = 0.8
		}
		return w
	case search.IntentTestsForCode:
		w := relevance.Weights{Semantic: 0.0, Lexical: 1.2, Graph: 0.1, Refs: 0.6, OntologyStructural: 0.0, OntologyAmbient: 0.0, Recency: 0.1, SeedLocality: 0.25, Specificity: 0.35}
		if !hasSeeds {
			w.Graph = 0.1
			w.Refs = 0.3
		}
		return w
	case search.IntentExplainSymbol:
		w := relevance.Weights{Semantic: 0.7, Lexical: 0.8, Graph: 0.2, Refs: 1.0, OntologyStructural: 0.25, OntologyAmbient: 0.0, Recency: 0.2, SeedLocality: 0.25, Specificity: 0.8}
		if !hasText {
			w.Semantic = 0.4
		}
		return w
	case search.IntentRefactorImpact, search.IntentSecurityAudit, search.IntentDataFlow, search.IntentImplementers, search.IntentOverrides, search.IntentImports:
		w := relevance.Weights{Semantic: 0.2, Lexical: 0.3, Graph: 1.0, Refs: 0.8, OntologyStructural: 0.65, OntologyAmbient: 0.2, Recency: 0.2, SeedLocality: 0.2, Specificity: 0.35}
		if !hasSeeds {
			w.Graph = 0.4
			w.Refs = 0.4
		}
		return w
	default:
		w := relevance.DefaultWeights()
		// If we have neither explicit nor implicit seeds, graph/refs won't contribute anyway,
		// but keep them small and non-zero so auto-expansion can matter.
		if !hasSeeds && !implicitSeeds {
			w.Graph = 0.15
			w.Refs = 0.25
			w.OntologyStructural = 0.2
			w.OntologyAmbient = 0.1
		}
		if implicitSeeds {
			w.Graph = 0.20
			w.Refs = 0.40
			w.SeedLocality = 0.45
		}
		return w
	}
}

type signalProfile struct {
	SemanticReady bool
	RefsReady     bool
	OntologyReady bool
	GraphReady    bool
	SparseNotes   bool
	SparseRefs    bool
	OntologyDense bool
	ImmatureVault bool
	NotesFirst    bool
	CodeFirstDocs bool
	NoteCount     int
	CodeFileCount int
}

func inspectSignalProfile(ctx context.Context, deps Deps, opts Options, spec search.QuerySpec) signalProfile {
	profile := signalProfile{
		SemanticReady: opts.EnableVector && deps.Semantic != nil,
		// Precision retrievers read exact definitions and relationships directly
		// from Intel. Disabling optional refs expansion must not zero the evidence
		// channel that scores those persisted proofs.
		RefsReady:     deps.IntelStore != nil && (opts.EnableRefs || search.IsPrecisionIntent(spec.Intent)),
		OntologyReady: opts.EnableGraph && ontologyRetrieverReady(ctx, deps.IntelStore),
		GraphReady:    opts.EnableGraph && (graphAvailable(deps.VaultDef, deps.NoteReader) || deps.IntelStore != nil),
	}
	if deps.IntelStore == nil || !shouldInspectCorpusShape(spec) {
		return profile
	}
	// This is a cheap corpus-shape read, not planning execution. It prevents
	// over-weighting graph/ontology/refs in sparse starter repos and prevents
	// code-heavy repos from drowning targeted docs/code lexical hits in weak graph
	// neighborhoods.
	notePaths, noteErr := deps.IntelStore.IntelNotePaths(ctx)
	if noteErr == nil {
		profile.NoteCount = len(notePaths)
		profile.SparseNotes = len(notePaths) < 5
	}
	codeFiles, fileErr := deps.IntelStore.ListFiles(ctx, 100000)
	if fileErr == nil {
		profile.CodeFileCount = len(codeFiles)
	}
	if profile.NoteCount > 0 && profile.NoteCount >= max(25, profile.CodeFileCount*2) {
		profile.NotesFirst = true
	}
	if profile.CodeFileCount >= max(20, profile.NoteCount*3) && profile.NoteCount < 20 {
		profile.CodeFirstDocs = true
	}
	if profile.NoteCount < 5 || profile.CodeFileCount < 5 {
		profile.ImmatureVault = true
	}
	if profile.NoteCount < 4 || profile.CodeFileCount < 8 {
		profile.SparseRefs = true
	}
	if profile.OntologyReady && profile.NoteCount >= 20 {
		profile.OntologyDense = true
	}
	return profile
}

func shouldInspectCorpusShape(spec search.QuerySpec) bool {
	if !search.IsBroadIntent(spec.Intent) {
		return false
	}
	return true
}

func adjustWeightsForSignals(w relevance.Weights, profile signalProfile, intent search.Intent) relevance.Weights {
	if !profile.SemanticReady {
		w.Semantic = 0
	}
	if !profile.RefsReady {
		w.Refs = 0
		w.SeedLocality *= 0.75
	}
	if !profile.GraphReady {
		w.Graph = 0
	}
	if !profile.OntologyReady {
		w.OntologyStructural = 0
		w.OntologyAmbient = 0
	}
	if profile.SparseNotes && search.IsBroadIntent(intent) {
		w.Graph *= 0.7
		w.OntologyStructural *= 0.6
		w.OntologyAmbient *= 0.5
	}
	if profile.SparseRefs {
		w.Refs *= 0.6
		w.SeedLocality *= 0.8
	}
	if profile.ImmatureVault && search.IsBroadIntent(intent) {
		w.Graph *= 0.65
		w.OntologyStructural *= 0.5
		w.OntologyAmbient *= 0.4
		w.Refs *= 0.75
	}
	if profile.CodeFirstDocs {
		w.Semantic *= 1.12
		w.Lexical *= 1.18
		w.Graph *= 0.55
		w.OntologyStructural *= 0.45
		w.OntologyAmbient *= 0.3
		if intent == search.IntentDocsForCode || intent == search.IntentCodeForDocs {
			w.Lexical = maxFloat(w.Lexical, 1.0)
			w.Semantic = maxFloat(w.Semantic, 0.65)
			w.Refs = maxFloat(w.Refs, 0.7)
		}
	}
	if profile.NotesFirst && search.IsBroadIntent(intent) {
		w.Graph *= 1.12
		w.Refs *= 1.05
		w.OntologyStructural *= 1.15
		w.OntologyAmbient *= 1.08
	}
	if profile.OntologyReady && !profile.OntologyDense {
		w.OntologyStructural *= 0.75
		w.OntologyAmbient *= 0.6
	}
	return w
}

func maxFloat(a, b float64) float64 {
	if a > b {
		return a
	}
	return b
}

// vectorAvailable checks if semantic query embedding + intel store are available.
// Returns false if Semantic is nil, no provider is configured, or intel store is missing.
func vectorAvailable(s *semantic.Searcher) bool {
	if !s.HasIntelStore() {
		return false
	}
	return s.CodeProvider != nil || s.NoteProvider != nil
}

// seedVectorAvailable checks if intel embeddings are available for seed similarity.
func seedVectorAvailable(s *semantic.Searcher) bool {
	return s.HasIntelStore()
}

// graphAvailable checks if graph-based retrieval is available (vault path and note manager present).
// Returns false if vault path is empty or NoteReader is nil.
func graphAvailable(vaultDef obsidian.VaultDefinition, note obsidian.NoteReader) bool {
	return vaultDef.BasePath() != "" && note != nil
}

// expandDirectoryFileSeeds expands directory seeds into representative files (via PageRank
// if intel is available, with an optional filesystem fallback). Limits depth and honors
// vault ignore rules when fallback is allowed.
//
// Expansion strategy (in order):
//  1. If intel store available: use TopCodePathsByPageRankPrefix (high-centrality files)
//  2. When policy allows it, fallback to a filesystem walk (max depth 3, uses unified ignore
//     matcher + vault excludes). Indexed-only callers preserve the unresolved directory seed.
//
// Limits: maxFilesPerDir defaults to 12, clamped to spec.Limits.Total if provided.
// Also derives top anchor handles from expanded files (max 10, or maxFilesPerDir/2).
//
// Fragments: directory-expanded files get Fragments: ["dirseed"] for debugging.
// See [Search - Seed expansion strategies](docs/reference/analysis/Search - Seed expansion strategies.md) for details.
func expandDirectoryFileSeeds(ctx context.Context, seeds []knowledge.Handle, vaultDef obsidian.VaultDefinition, intelStore *semdb.Store, maxFilesPerDir int, policy DirectorySeedExpansionPolicy) []knowledge.Handle {
	if len(seeds) == 0 {
		return nil
	}
	if maxFilesPerDir <= 0 {
		maxFilesPerDir = 12
	}
	vaultPath := vaultDef.BasePath()
	vaultPaths, _ := paths.NewVaultPaths(vaultPath)

	out := make([]knowledge.Handle, 0, len(seeds))
	seen := make(map[string]struct{}, len(seeds))

	add := func(h knowledge.Handle) {
		if h.String() == "" {
			return
		}
		if _, ok := seen[h.String()]; ok {
			return
		}
		seen[h.String()] = struct{}{}
		out = append(out, h)
	}

	for _, seed := range seeds {
		if seed.Kind != knowledge.KindFile {
			add(seed)
			continue
		}
		seedPath := strings.TrimSpace(seed.ID)
		if seedPath == "" {
			continue
		}

		ref, err := paths.ResolveCodeRefWithVaultPaths(vaultPaths, seedPath)
		if err == nil && ref.Abs != "" {
			if info, err := os.Stat(ref.Abs.String()); err == nil && info.IsDir() {
				expanded := directorySeedFiles(ctx, ref.Abs.String(), vaultDef, intelStore, maxFilesPerDir, policy)
				if len(expanded) > 0 {
					derivedPaths := make([]string, 0, len(expanded))
					for _, rel := range expanded {
						derivedPaths = append(derivedPaths, rel)
						derived := knowledge.FileHandle(rel)
						derived.Fragments = []string{"dirseed"}
						add(derived)
					}
					if intelStore != nil {
						maxAnchors := min(10, max(2, maxFilesPerDir/2))
						if ids, err := intelStore.TopModuleAnchorIDsByPaths(ctx, derivedPaths, maxAnchors); err == nil && len(ids) > 0 {
							for _, id := range ids {
								add(knowledge.AnchorHandle(id))
							}
						}
					}
					continue
				}
			}
		}

		add(seed)
	}
	return out
}

func directorySeedFiles(ctx context.Context, absDir string, vaultDef obsidian.VaultDefinition, intelStore *semdb.Store, maxFiles int, policy DirectorySeedExpansionPolicy) []string {
	if maxFiles <= 0 {
		return nil
	}
	vaultPath := vaultDef.BasePath()
	if strings.TrimSpace(vaultPath) == "" {
		// Best-effort filesystem fallback still requires a vault root so we can return stable, relative seeds.
		return nil
	}
	vaultPaths, _ := paths.NewVaultPaths(vaultPath)

	// The indexed code paths are the ownership boundary. Do not inspect the
	// filesystem or select descendants by extension: a typed file directory does
	// not establish ownership of every file below it.
	if intelStore != nil {
		if rel, err := vaultPaths.RelCodeStrict(absDir); err == nil && rel.String() != "" {
			// Use anchor PageRank to pick a few high-centrality files within the directory. This tends to
			// select entrypoints and "important" modules without scanning everything.
			if paths, err := intelStore.TopCodePathsByPageRankPrefix(ctx, rel.String(), maxFiles); err == nil && len(paths) > 0 {
				return paths
			}
			if files, err := intelStore.FilesByPathPrefix(ctx, rel.String(), maxFiles); err == nil && len(files) > 0 {
				// FilesByPathPrefix is the durable code ownership boundary.
				return files
			}
		}
	}
	if policy == DirectorySeedExpansionIndexedOnly {
		return nil
	}

	return nil
}

// expandNoteSeedsFromDocLinks derives note seeds from code→note doc links for file seeds.
//
// Queries DocLinksFromCodePath for each file seed (and files within directory seeds).
// Limits expansion to prevent explosion: default 50 total note handles, 10 per path.
// Deduplicates by the authored typed note path.
//
// Returns nil if store is nil or no seeds provided.
func expandNoteSeedsFromDocLinks(ctx context.Context, seeds []knowledge.Handle, store *semdb.Store, vaultPath string, limit int) []knowledge.Handle {
	if store == nil || len(seeds) == 0 {
		return nil
	}
	if limit <= 0 {
		limit = 50
	}
	vaultPaths, _ := paths.NewVaultPaths(vaultPath)
	seen := make(map[string]struct{}, len(seeds))
	for _, s := range seeds {
		if s.Kind == knowledge.KindNote || s.Kind == knowledge.KindNoteChunk {
			seen[s.ID] = struct{}{}
		}
	}

	addNote := func(notePath string) []knowledge.Handle {
		ref, err := paths.ResolveNotePathRefWithVaultPaths(vaultPaths, notePath)
		if err != nil || ref.Rel == "" {
			return nil
		}
		noteID := ref.Rel.String()
		if _, ok := seen[noteID]; ok {
			return nil
		}
		seen[noteID] = struct{}{}
		return []knowledge.Handle{knowledge.NoteHandle(noteID)}
	}

	var out []knowledge.Handle
	for _, seed := range seeds {
		if len(out) >= limit {
			break
		}
		if seed.Kind != knowledge.KindFile {
			continue
		}
		seedPath := strings.TrimSpace(seed.ID)
		if seedPath == "" {
			continue
		}

		ref, err := paths.ResolveCodeRefWithVaultPaths(vaultPaths, seedPath)
		if err != nil || ref.Rel == "" {
			continue
		}

		codePaths := []string{ref.Rel.String()}
		if ref.Abs != "" {
			if info, err := os.Stat(ref.Abs.String()); err == nil && info.IsDir() {
				if files, err := store.FilesByPathPrefix(ctx, ref.Rel.String(), limit); err == nil && len(files) > 0 {
					codePaths = files
				}
			}
		}
		for _, p := range codePaths {
			if len(out) >= limit {
				break
			}
			perPath := limit - len(out)
			links, err := store.DocLinksFromCodePath(ctx, p, min(perPath, 10))
			if err != nil {
				continue
			}
			for _, l := range links {
				if strings.ToLower(strings.TrimSpace(l.DstKind)) != "note" {
					continue
				}
				added := addNote(l.DstPath)
				out = append(out, added...)
				if len(out) >= limit {
					break
				}
			}
		}
	}
	return out
}

// expandAnchorSeedsFromFiles derives anchor seeds from files via intel store lookup.
//
// Expands directory seeds into their contained files first (via FilesByPathPrefix),
// then queries IntelAnchorsByPath for each file path. Limits expansion: default 120
// total anchor handles. Deduplicates by anchor ID.
//
// Returns nil if store is nil or no file seeds provided.
func expandAnchorSeedsFromFiles(ctx context.Context, seeds []knowledge.Handle, store *semdb.Store, vaultPath string, limit int) []knowledge.Handle {
	if store == nil || len(seeds) == 0 {
		return nil
	}
	vaultPaths, _ := paths.NewVaultPaths(vaultPath)
	if limit <= 0 {
		limit = 120
	}

	seen := make(map[string]struct{}, len(seeds))
	for _, s := range seeds {
		seen[s.String()] = struct{}{}
	}

	var out []knowledge.Handle
	add := func(anchorID string) {
		h := knowledge.AnchorHandle(anchorID)
		if h.String() == "" {
			return
		}
		if _, ok := seen[h.String()]; ok {
			return
		}
		seen[h.String()] = struct{}{}
		out = append(out, h)
	}

	filePaths := make([]string, 0, len(seeds))
	for _, s := range seeds {
		if s.Kind != knowledge.KindFile {
			continue
		}
		ref, err := paths.ResolveCodeRefWithVaultPaths(vaultPaths, strings.TrimSpace(s.ID))
		if err != nil || ref.Rel == "" {
			continue
		}
		filePaths = append(filePaths, ref.Rel.String())

		if ref.Abs != "" {
			if info, err := os.Stat(ref.Abs.String()); err == nil && info.IsDir() {
				if files, err := store.FilesByPathPrefix(ctx, ref.Rel.String(), limit); err == nil && len(files) > 0 {
					filePaths = append(filePaths, files...)
				}
			}
		}
	}

	for _, p := range filePaths {
		if len(out) >= limit {
			break
		}
		anchors, err := store.IntelAnchorsByPath(ctx, p)
		if err != nil {
			continue
		}
		for _, a := range anchors {
			add(a.AnchorID)
			if len(out) >= limit {
				break
			}
		}
	}
	return out
}

func ValidateIntent(intent string) error {
	switch search.Intent(intent) {
	case search.IntentSearch,
		search.IntentRelatedToSeed,
		search.IntentDocsForCode,
		search.IntentOverview,
		search.IntentSubsystemOverview,
		search.IntentCodeForDocs,
		search.IntentConsolidation,
		search.IntentFindUsages,
		search.IntentGoToDef,
		search.IntentExplainSymbol,
		search.IntentTestsForCode,
		search.IntentRefactorImpact,
		search.IntentCallers,
		search.IntentCallees,
		search.IntentImplementers,
		search.IntentOverrides,
		search.IntentImports,
		search.IntentDataFlow,
		search.IntentSecurityAudit:
		return nil
	default:
		return fmt.Errorf("unknown intent %q", intent)
	}
}

// noteTypePathSource selects note paths by owning-note type, or nil without
// an intel store.
func (p *Planner) noteTypePathSource() retrieval.NoteTypePathSource {
	if p.Deps.IntelStore == nil {
		return nil
	}
	return retrieval.NoteTypePathSourceFunc(func(ctx context.Context, typeNames []string) ([]string, error) {
		seen := map[string]struct{}{}
		var out []string
		for _, typeName := range typeNames {
			paths, err := p.Deps.IntelStore.OntologyPathsByType(ctx, typeName, 0)
			if err != nil {
				return nil, err
			}
			for _, path := range paths {
				if _, ok := seen[path]; ok {
					continue
				}
				seen[path] = struct{}{}
				out = append(out, path)
			}
		}
		return out, nil
	})
}
