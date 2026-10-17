package planner

import (
	"github.com/atomicobject/rhizome/pkg/search"
)

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
func weightsForIntent(intent search.Intent, hasText, hasSeeds, implicitSeeds bool) search.Weights {
	switch intent {
	case search.IntentRelatedToSeed:
		w := search.Weights{Semantic: 0.4, Lexical: 0.3, Graph: 1.0, Refs: 0.9, OntologyStructural: 0.7, OntologyAmbient: 0.4, SeedLocality: 0.4, Specificity: 0.35}
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
		w := search.Weights{Semantic: 0.5, Lexical: 0.9, Graph: 0.15, Refs: 1.2, OntologyStructural: 0.45, OntologyAmbient: 0.15, SeedLocality: 0.55, Specificity: 0.7}
		if !hasSeeds {
			w.Refs = 0.6
			w.SeedLocality = 0.35
		}
		if !hasText {
			w.Semantic = 0.2
		}
		return w
	case search.IntentCodeForDocs:
		w := search.Weights{Semantic: 0.6, Lexical: 0.6, Graph: 0.2, Refs: 1.2, OntologyStructural: 0.35, OntologyAmbient: 0.12, SeedLocality: 0.45, Specificity: 0.75}
		if !hasSeeds {
			w.Refs = 0.4
		}
		return w
	case search.IntentConsolidation:
		w := search.Weights{Semantic: 0.4, Lexical: 0.2, Graph: 1.0, Refs: 0.4, OntologyStructural: 0.8, OntologyAmbient: 0.45, SeedLocality: 0.25, Specificity: 0.45}
		if !hasText {
			w.Semantic = 0
			w.Lexical = 0
		}
		return w
	case search.IntentOverview:
		w := search.Weights{Semantic: 1.0, Lexical: 0.3, Graph: 0.5, Refs: 0.6, OntologyStructural: 0.55, OntologyAmbient: 0.25, SeedLocality: 0.35, Specificity: 1.0}
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
		w := search.Weights{Semantic: 0.8, Lexical: 0.5, Graph: 0.35, Refs: 1.0, OntologyStructural: 0.65, OntologyAmbient: 0.25, SeedLocality: 0.85, Specificity: 0.8}
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
		w := search.Weights{Semantic: 0.0, Lexical: 0.6, Graph: 0.0, Refs: 1.4, OntologyStructural: 0.1, OntologyAmbient: 0.0, SeedLocality: 0.2, Specificity: 0.1}
		if !hasText {
			w.Lexical = 0.3
		}
		return w
	case search.IntentFindUsages, search.IntentCallers, search.IntentCallees:
		w := search.Weights{Semantic: 0.0, Lexical: 0.5, Graph: 0.2, Refs: 1.2, OntologyStructural: 0.15, OntologyAmbient: 0.0, SeedLocality: 0.15, Specificity: 0.15}
		if !hasSeeds {
			w.Graph = 0.3
			w.Refs = 0.8
		}
		return w
	case search.IntentTestsForCode:
		w := search.Weights{Semantic: 0.0, Lexical: 1.2, Graph: 0.1, Refs: 0.6, OntologyStructural: 0.0, OntologyAmbient: 0.0, SeedLocality: 0.25, Specificity: 0.35}
		if !hasSeeds {
			w.Graph = 0.1
			w.Refs = 0.3
		}
		return w
	case search.IntentExplainSymbol:
		w := search.Weights{Semantic: 0.7, Lexical: 0.8, Graph: 0.2, Refs: 1.0, OntologyStructural: 0.25, OntologyAmbient: 0.0, SeedLocality: 0.25, Specificity: 0.8}
		if !hasText {
			w.Semantic = 0.4
		}
		return w
	case search.IntentRefactorImpact, search.IntentSecurityAudit, search.IntentDataFlow, search.IntentImplementers, search.IntentOverrides, search.IntentImports:
		w := search.Weights{Semantic: 0.2, Lexical: 0.3, Graph: 1.0, Refs: 0.8, OntologyStructural: 0.65, OntologyAmbient: 0.2, SeedLocality: 0.2, Specificity: 0.35}
		if !hasSeeds {
			w.Graph = 0.4
			w.Refs = 0.4
		}
		return w
	default:
		w := search.DefaultWeights()
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

func adjustWeightsForSignals(w search.Weights, profile signalProfile, intent search.Intent) search.Weights {
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
