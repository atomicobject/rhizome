package planner

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	codeanchorsqlite "github.com/atomicobject/rhizome/pkg/anchors/sqlite"
	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/search"
	"github.com/atomicobject/rhizome/pkg/search/embeddings"
	"github.com/atomicobject/rhizome/pkg/search/knowledge"
	"github.com/atomicobject/rhizome/pkg/search/semantic"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"
)

func TestPlanner_SkipsVectorRetrieversForTypedNilIntelStore(t *testing.T) {
	var typedNil *codeanchorsqlite.Store
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "Alice.md"), []byte("# Alice\n"), 0o644))
	prov := embeddings.NewDeterministicProvider(embeddings.ProviderConfig{Provider: "test", Dimensions: 8})
	p := Planner{Deps: Deps{
		Semantic:  &semantic.Searcher{NoteProvider: prov, IntelStore: typedNil},
		VaultPath: root, VaultDef: obsidian.VaultDefinition{Path: root}, NoteReader: &obsidian.Note{},
	}, Options: Options{EnableVector: true, EnableGraph: true, MaxPerOwner: 3}}

	spec := search.QuerySpec{Text: "Alice", Intent: search.IntentSearch, Limits: search.Limits{Total: 10}}
	plan, err := p.Plan(context.Background(), spec)
	require.NoError(t, err)
	require.False(t, planContainsLane(plan, "vector"))
	response, err := (&search.Service{Retrievers: plan.Retrievers, Ranker: plan.Ranker}).Search(context.Background(), spec)
	require.NoError(t, err, "a typed-nil store must not reach a vector retriever")
	var lexicalAlice bool
	for _, result := range response.Results {
		for _, ev := range result.Evidence {
			lexicalAlice = lexicalAlice || (result.Path == "Alice.md" && ev.Source == "note_lexical")
		}
	}
	require.True(t, lexicalAlice, "lexical fallback must still find Alice.md; got %+v", response.Results)

	seeded, err := p.Plan(context.Background(), search.QuerySpec{
		Text: "Alice", Seeds: []knowledge.Handle{knowledge.NoteHandle("Alice.md")},
		Intent: search.IntentSearch, Limits: search.Limits{Total: 10},
	})
	require.NoError(t, err)
	require.False(t, planContainsLane(seeded, "seed_vector"), "seed similarity needs a real intel store")
}

func TestPlanner_RefactorImpactUsesCallsRefsAndTestsWithoutVector(t *testing.T) {
	root := t.TempDir()
	store, err := codeanchorsqlite.Open(filepath.Join(root, "intel.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	provider := embeddings.NewDeterministicProvider(embeddings.ProviderConfig{Provider: "test", Dimensions: 8})
	semanticSearcher := &semantic.Searcher{CodeProvider: provider, NoteProvider: provider, IntelStore: store}
	p := Planner{
		Deps: Deps{
			IntelStore: store,
			Semantic:   semanticSearcher,
			VaultPath:  root,
		},
		Options: Options{
			EnableIntel:  true,
			EnableRefs:   true,
			EnableVector: true,
			MaxPerOwner:  3,
		},
	}

	plan, err := p.Plan(context.Background(), search.QuerySpec{
		Text:   "refactor impact of Service",
		Seeds:  []knowledge.Handle{knowledge.AnchorHandle("service-anchor")},
		Intent: search.IntentRefactorImpact,
		Limits: search.Limits{Total: 10},
	})
	require.NoError(t, err)

	var callEdges int
	var retrieverNames []string
	for _, retriever := range plan.Retrievers {
		retrieverNames = append(retrieverNames, retriever.Name())
		if retriever.Name() == "call_edges" {
			callEdges++
		}
	}
	require.Equal(t, 2, callEdges, "impact search needs incoming and outgoing call evidence; got %v", retrieverNames)
	require.True(t, planHasRetrieverNamed(plan, "refs"))
	require.True(t, planHasRetrieverNamed(plan, "tests_for_code"))
	require.False(t, planContainsLane(plan, "vector"))
	ordinary, err := p.Plan(context.Background(), search.QuerySpec{Text: "search", Intent: search.IntentSearch, Limits: search.Limits{Total: 10}})
	require.NoError(t, err)
	require.True(t, planContainsLane(ordinary, "vector"), "the backend must be available before profile suppression is tested")
}

func TestPlanner_SkipsOntologyRetrieverWhenOntologyStateIsStale(t *testing.T) {
	root := t.TempDir()
	store, err := codeanchorsqlite.Open(filepath.Join(root, ".rhizome", "db.sqlite"))
	require.NoError(t, err)
	defer func() { _ = store.Close() }()

	require.NoError(t, store.ReplaceNoteMetadataSnapshot(context.Background(), codeanchorsqlite.NoteMetadataSnapshot{
		State: codeanchorsqlite.NoteMetadataState{
			NotesHash: "current-notes-hash",
			LoadedAt:  2,
			Ready:     true,
		},
	}))
	require.NoError(t, store.ReplaceOntologySnapshot(context.Background(), codeanchorsqlite.OntologySnapshot{
		SchemaState: codeanchorsqlite.OntologySchemaState{
			SchemaHash:             "schema-hash",
			NotesHash:              "stale-notes-hash",
			MaterializationVersion: ontology.OntologyMaterializationVersion,
			LoadedAt:               1,
			Ready:                  true,
		},
	}))

	p := Planner{
		Deps: Deps{
			IntelStore: store,
			VaultPath:  root,
			VaultDef:   obsidian.VaultDefinition{Path: root},
			NoteReader: &obsidian.Note{},
		},
		Options: Options{
			EnableGraph: true,
			MaxPerOwner: 3,
		},
	}

	plan, err := p.Plan(context.Background(), search.QuerySpec{
		Seeds:  []knowledge.Handle{knowledge.NoteHandle("people/Alice.md")},
		Intent: search.IntentRelatedToSeed,
		Limits: search.Limits{Total: 10},
	})
	require.NoError(t, err)

	names := make([]string, 0, len(plan.Retrievers))
	for _, retriever := range plan.Retrievers {
		names = append(names, retriever.Name())
	}
	require.NotContains(t, names, "ontology")

	require.NoError(t, store.ReplaceOntologySnapshot(context.Background(), codeanchorsqlite.OntologySnapshot{
		SchemaState: codeanchorsqlite.OntologySchemaState{
			SchemaHash: "schema-hash", NotesHash: "current-notes-hash",
			MaterializationVersion: ontology.OntologyMaterializationVersion - 1,
			LoadedAt:               2, Ready: true,
		},
	}))
	plan, err = p.Plan(context.Background(), search.QuerySpec{
		Seeds:  []knowledge.Handle{knowledge.NoteHandle("people/Alice.md")},
		Intent: search.IntentRelatedToSeed, Limits: search.Limits{Total: 10},
	})
	require.NoError(t, err)
	require.False(t, planHasRetrieverNamed(plan, "ontology"), "matching hash must not admit an old materializer")
}

func TestPlanner_WiresOverviewEvidenceShaperForOverviewModes(t *testing.T) {
	root := t.TempDir()
	store, err := codeanchorsqlite.Open(filepath.Join(root, "intel.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	p := Planner{
		Deps: Deps{
			IntelStore: store,
			VaultPath:  root,
			VaultDef:   obsidian.VaultDefinition{Path: root},
			NoteReader: &countingNoteReader{},
		},
		Options: Options{
			EnableGraph: true,
			MaxPerOwner: 3,
		},
	}

	overview, err := p.Plan(context.Background(), search.QuerySpec{
		Text:   "search architecture",
		Intent: search.IntentOverview,
		Limits: search.Limits{Total: 10},
	})
	require.NoError(t, err)
	require.NotNil(t, overview.Shaper)
	results := make([]search.RankedResult, 0, 9)
	for i := 0; i < 8; i++ {
		path := fmt.Sprintf("docs/guide-%d.md", i)
		results = append(results, search.RankedResult{Candidate: search.Candidate{Handle: knowledge.NoteHandle(path), Owner: knowledge.NoteHandle(path), Type: "note", Path: path}, FinalScore: 1 - float64(i)*.02})
	}
	results = append(results, search.RankedResult{Candidate: search.Candidate{
		Handle: knowledge.FileHandle("pkg/search/service.go"), Owner: knowledge.FileHandle("pkg/search/service.go"),
		Type: "code", Path: "pkg/search/service.go", Evidence: []search.Evidence{{Type: "symbol_match", RawScore: .85}},
	}, FinalScore: .8})
	overviewSpec := search.QuerySpec{Text: "search architecture", Intent: search.IntentOverview, Limits: search.Limits{Total: 10}}
	shaped, err := overview.Shaper.Shape(context.Background(), overviewSpec, results)
	require.NoError(t, err)
	require.Equal(t, "pkg/search/service.go", shaped[2].Path)

	subsystem, err := p.Plan(context.Background(), search.QuerySpec{
		Text:              "search architecture",
		Intent:            search.IntentSubsystemOverview,
		ExplicitSeedPaths: []string{"pkg/search/service.go"},
		HasExplicitSeeds:  true,
		Limits:            search.Limits{Total: 10},
	})
	require.NoError(t, err)
	require.NotNil(t, subsystem.Shaper)
	localDoc := search.RankedResult{Candidate: search.Candidate{Handle: knowledge.NoteHandle("pkg/search/CONTEXT.md"), Owner: knowledge.NoteHandle("pkg/search/CONTEXT.md"), Type: "note", Path: "pkg/search/CONTEXT.md", DocClass: search.DocClassModule, PrimaryDoc: true}, FinalScore: .75}
	subsystemSpec := search.QuerySpec{Text: "search architecture", Intent: search.IntentSubsystemOverview, ExplicitSeedPaths: []string{"pkg/search/service.go"}, HasExplicitSeeds: true, Limits: search.Limits{Total: 10}}
	shaped, err = subsystem.Shaper.Shape(context.Background(), subsystemSpec, append(append([]search.RankedResult(nil), results...), localDoc))
	require.NoError(t, err)
	require.Equal(t, localDoc.Path, shaped[0].Path)
	require.Contains(t, []string{shaped[1].Path, shaped[2].Path}, "pkg/search/service.go")

	precision, err := p.Plan(context.Background(), search.QuerySpec{
		Text:   "Service.Search",
		Intent: search.IntentGoToDef,
		Limits: search.Limits{Total: 10},
	})
	require.NoError(t, err)
	require.Nil(t, precision.Shaper)
}

func planContainsLane(plan Plan, name string) bool {
	var has func(search.Retriever) bool
	has = func(r search.Retriever) bool {
		if r.Name() == name {
			return true
		}
		if nested, ok := r.(interface{ NestedRetrievers() []search.Retriever }); ok {
			for _, child := range nested.NestedRetrievers() {
				if has(child) {
					return true
				}
			}
		}
		return false
	}
	for _, r := range plan.Retrievers {
		if has(r) {
			return true
		}
	}
	return false
}
