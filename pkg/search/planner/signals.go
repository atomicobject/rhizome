package planner

import (
	"context"
	"fmt"

	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/search"
	"github.com/atomicobject/rhizome/pkg/search/knowledge"
	"github.com/atomicobject/rhizome/pkg/search/retrieval"
	"github.com/atomicobject/rhizome/pkg/search/semantic"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
)

func hasConstrainedFilters(filters search.Filters) bool {
	return len(filters.Types) > 0 || len(filters.PathPrefixes) > 0 || len(filters.NoteTypes) > 0 || filters.TestsOnly || filters.ExcludeTests
}

func graphNoteReader(deps Deps) obsidian.NoteReader {
	if deps.GraphNoteReader != nil {
		return deps.GraphNoteReader
	}
	return deps.NoteReader
}

func ontologyRetrieverReady(ctx context.Context, deps Deps) bool {
	if deps.IntelStore == nil || len(deps.NoteFormats.Registry().IDs()) == 0 {
		return false
	}
	publication, err := deps.IntelStore.GetOntologyReadiness(ctx, ontology.OntologyProviderHash(deps.NoteFormats))
	return err == nil && publication.Ready && publication.SchemaState.MaterializationVersion == ontology.OntologyMaterializationVersion
}

func hasNoteSeeds(seeds []knowledge.Handle) bool {
	for _, seed := range seeds {
		if seed.Kind == knowledge.KindNote || seed.Kind == knowledge.KindNoteChunk {
			return true
		}
	}
	return false
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
		OntologyReady: opts.EnableGraph && ontologyRetrieverReady(ctx, deps),
		GraphReady:    opts.EnableGraph && (graphAvailable(deps.VaultDef, deps.NoteReader) || deps.IntelStore != nil),
	}
	if deps.IntelStore == nil || !shouldInspectCorpusShape(spec) {
		return profile
	}
	// This is a cheap corpus-shape read, not planning execution. It prevents
	// over-weighting graph/ontology/refs in sparse starter repos and prevents
	// code-heavy repos from drowning targeted docs/code lexical hits in weak graph
	// neighborhoods.
	if deps.Corpus != nil {
		profile.NoteCount, profile.CodeFileCount = len(deps.Corpus.NotePaths), len(deps.Corpus.CodePaths)
		profile.SparseNotes = profile.NoteCount < 5
	} else {
		notePaths, noteErr := deps.IntelStore.IntelNotePaths(ctx)
		if noteErr == nil {
			profile.NoteCount = len(notePaths)
			profile.SparseNotes = len(notePaths) < 5
		}
		codeFiles, fileErr := deps.IntelStore.ListFiles(ctx, 100000)
		if fileErr == nil {
			profile.CodeFileCount = len(codeFiles)
		}
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
