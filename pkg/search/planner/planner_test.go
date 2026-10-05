package planner

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	codeanchorsqlite "github.com/atomicobject/rhizome/pkg/anchors/sqlite"
	"github.com/atomicobject/rhizome/pkg/noteformat/builtin"
	"github.com/atomicobject/rhizome/pkg/notemeta"
	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/search"
	"github.com/atomicobject/rhizome/pkg/search/knowledge"
	"github.com/atomicobject/rhizome/pkg/search/retrieval"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"
)

type countingNoteReader struct {
	getNotesListCalls int
	getModTimeCalls   int
}

func (n *countingNoteReader) GetContents(obsidian.VaultDefinition, string) (string, error) {
	return "", nil
}

func (n *countingNoteReader) GetNotesList(obsidian.VaultDefinition) ([]string, error) {
	n.getNotesListCalls++
	return []string{"people/Alice.md"}, nil
}

func (n *countingNoteReader) GetModTime(obsidian.VaultDefinition, string) (time.Time, error) {
	n.getModTimeCalls++
	return time.Unix(1, 0), nil
}

func (n *countingNoteReader) Title(path string) (string, bool) {
	return filepath.Base(path), true
}

func TestPlannerIncludesExplicitTargetForOrdinarySearch(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "target.go"), []byte("package target\n"), 0o644))
	plan, err := (&Planner{Deps: Deps{VaultPath: root}}).Plan(context.Background(), search.QuerySpec{
		Text: "target", Intent: search.IntentSearch, Limits: search.Limits{Total: 10},
		ExplicitSeedPaths: []string{"target.go"}, HasExplicitSeeds: true,
		PathKinds: map[string]search.PathKind{"target.go": search.PathKindCode},
	})
	require.NoError(t, err)
	found := false
	for _, retriever := range plan.Retrievers {
		if _, ok := retriever.(*retrieval.ExplicitSeedRetriever); ok {
			found = true
		}
	}
	require.True(t, found)
}

func TestPlannerScoresExactRelationshipProofWhenRefExpansionIsDisabled(t *testing.T) {
	ctx := context.Background()
	store, err := codeanchorsqlite.Open(filepath.Join(t.TempDir(), "relationship-score.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	spec := search.QuerySpec{Text: "implementers of OperationStore", Intent: search.IntentImplementers, Limits: search.Limits{Total: 10}}
	plan, err := (&Planner{Deps: Deps{IntelStore: store}, Options: Options{EnableIntel: true, EnableRefs: false}}).Plan(ctx, spec)
	require.NoError(t, err)
	result, err := plan.Ranker.Rank(ctx, spec, []search.Candidate{{
		Handle: knowledge.AnchorHandle("memory-store"),
		Owner:  knowledge.AnchorHandle("memory-store"),
		Type:   "anchor",
		Path:   "dispatch/store.go",
		FQN:    "dispatch.MemoryOperationStore",
		Evidence: []search.Evidence{{
			Type: "implements_edge", Source: "implementers", RawScore: 1,
		}},
	}})
	require.NoError(t, err)
	require.Len(t, result, 1)
	require.Greater(t, result[0].FinalScore, 0.0, "exact Intel proof must retain rank weight when optional expansion is off")
}

func TestExpandDirectoryFileSeeds_PreservesDirectoryWithoutDurableOwnership(t *testing.T) {
	t.Parallel()

	vaultRoot := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(vaultRoot, "pkg", "sub"), 0o755))

	require.NoError(t, os.WriteFile(filepath.Join(vaultRoot, "pkg", "a.go"), []byte("package pkg\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(vaultRoot, "pkg", "sub", "descriptor.html"), []byte("<h1>descriptor</h1>"), 0o644))

	seeds := []knowledge.Handle{
		knowledge.FileHandle("pkg"),
		knowledge.FileHandle("pkg/a.go"),
	}
	got := expandDirectoryFileSeeds(context.Background(), seeds, obsidian.VaultDefinition{Path: vaultRoot}, nil, 10, DirectorySeedExpansionAllowFilesystemFallback)

	containsFileID := func(id string) bool {
		for _, h := range got {
			if h.Kind == knowledge.KindFile && h.ID == id {
				return true
			}
		}
		return false
	}

	require.True(t, containsFileID("pkg"), "directory needs indexed descendants before expansion")
	require.True(t, containsFileID("pkg/a.go"))
	require.False(t, containsFileID("pkg/sub/descriptor.html"))
}

func TestExpandDirectoryFileSeeds_IndexedOnlyDoesNotWalkWhenPrefixIsEmpty(t *testing.T) {
	t.Parallel()

	vaultRoot := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(vaultRoot, "pkg"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(vaultRoot, "pkg", "only-on-filesystem.go"), []byte("package pkg\n"), 0o644))

	store, err := codeanchorsqlite.Open(filepath.Join(t.TempDir(), "intel.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	got := expandDirectoryFileSeeds(
		context.Background(),
		[]knowledge.Handle{knowledge.FileHandle("pkg")},
		obsidian.VaultDefinition{Path: vaultRoot},
		store,
		10,
		DirectorySeedExpansionIndexedOnly,
	)

	require.Equal(t, []knowledge.Handle{knowledge.FileHandle("pkg")}, got,
		"an empty indexed prefix must preserve the unresolved directory seed without walking the filesystem")
}

func TestPlanner_AddsSymbolProbeForBroadTextSearch(t *testing.T) {
	ctx := context.Background()
	store, err := codeanchorsqlite.Open(filepath.Join(t.TempDir(), "intel.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	p := Planner{
		Deps: Deps{
			IntelStore: store,
		},
		Options: Options{
			EnableIntel: true,
		},
	}

	plan, err := p.Plan(ctx, search.QuerySpec{
		Text:   "how does MakePlan work?",
		Intent: search.IntentSearch,
		Limits: search.Limits{Total: 10},
	})
	require.NoError(t, err)

	found := false
	for _, retriever := range plan.Retrievers {
		if _, ok := retriever.(*retrieval.SymbolProbeRetriever); ok {
			found = true
			break
		}
	}
	require.True(t, found, "expected broad text search to include symbol probe retriever")
}

func TestPlanner_CodeFilterSkipsNoteLexicalRetriever(t *testing.T) {
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
		Options: Options{EnableIntel: true, EnableGraph: true, MaxPerOwner: 3},
	}

	plan, err := p.Plan(context.Background(), search.QuerySpec{
		Text:    "shared search term",
		Intent:  search.IntentSearch,
		Filters: search.Filters{Types: []string{"code"}},
		Limits:  search.Limits{Total: 10},
	})
	require.NoError(t, err)
	require.False(t, planHasRetrieverNamed(plan, "note_lexical"), "code-only search must not spend a lane on note title matches")
}

func TestPlanner_AddsRationaleFTSForRationaleShapedQueries(t *testing.T) {
	ctx := context.Background()
	store, err := codeanchorsqlite.Open(filepath.Join(t.TempDir(), "intel.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	p := Planner{
		Deps: Deps{
			IntelStore: store,
		},
		Options: Options{
			EnableIntel: true,
		},
	}

	plan, err := p.Plan(ctx, search.QuerySpec{
		Text:   "why does search preserve evidence?",
		Intent: search.IntentSearch,
		Limits: search.Limits{Total: 10},
	})
	require.NoError(t, err)
	require.True(t, planHasRetrieverNamed(plan, "rationale_fts"), "expected rationale-shaped query to include rationale FTS")

	generic, err := p.Plan(ctx, search.QuerySpec{
		Text:   "search service",
		Intent: search.IntentSearch,
		Limits: search.Limits{Total: 10},
	})
	require.NoError(t, err)
	require.False(t, planHasRetrieverNamed(generic, "rationale_fts"), "generic search should not include rationale FTS")

	precision, err := p.Plan(ctx, search.QuerySpec{
		Text:   "why does search preserve evidence?",
		Intent: search.IntentGoToDef,
		Limits: search.Limits{Total: 10},
	})
	require.NoError(t, err)
	require.False(t, planHasRetrieverNamed(precision, "rationale_fts"), "precision intents should not include rationale FTS")
}

func TestPlanner_AddsOntologyRetrieverForNoteSeeds(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, ".rhizome", "ontology"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, ".rhizome", "config.yml"), []byte("notes:\n  includes: [\"**/*.md\"]\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, ".rhizome", "ontology", "schema.graphql"), []byte(`
type Team @node(paths: ["teams/*.md"]) {
  name: String!
  members: [Person!] @link(inverse: "team")
}

type Person @node(paths: ["people/*.md"]) {
  name: String!
  team: Team @link(inverse: "members")
}
`), 0o644))
	require.NoError(t, os.MkdirAll(filepath.Join(root, "people"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(root, "teams"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "teams", "Eng.md"), []byte(`---
type: Team
name: Eng
members:
  - people/Alice.md
---
`), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, "people", "Alice.md"), []byte(`---
type: Person
name: Alice
team: teams/Eng.md
---
`), 0o644))

	store, err := codeanchorsqlite.Open(filepath.Join(root, ".rhizome", "db.sqlite"))
	require.NoError(t, err)
	defer func() { _ = store.Close() }()
	require.NoError(t, store.ReplaceNoteMetadataSnapshot(context.Background(), codeanchorsqlite.NoteMetadataSnapshot{
		State: codeanchorsqlite.NoteMetadataState{
			NotesHash: "notes-hash",
			LoadedAt:  1,
			Ready:     true,
		},
	}))
	schema, err := ontology.LoadSchema(root)
	require.NoError(t, err)
	runtime, err := builtin.NewRuntime()
	require.NoError(t, err)
	noteMetadata, err := notemeta.NewIndexer(runtime)
	require.NoError(t, err)
	sources, err := noteMetadata.BuildNoteSourceSnapshots(context.Background(), obsidian.VaultDefinition{Path: root}, &obsidian.Note{})
	require.NoError(t, err)
	build, err := ontology.BuildIndexFromNoteSources(context.Background(), obsidian.VaultDefinition{Path: root}, sources, schema, "notes-hash")
	require.NoError(t, err)
	require.NoError(t, store.ReplaceOntologySnapshot(context.Background(), codeanchorsqlite.OntologySnapshot{
		NoteTypes: build.NoteTypes,
		Edges:     build.Edges,
		SchemaState: codeanchorsqlite.OntologySchemaState{
			SchemaHash:             schema.Hash,
			NotesHash:              build.NotesHash,
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
	require.Contains(t, names, "ontology")
}

func TestPlanner_TextOnlySkipsOntologyRefresh(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, ".rhizome", "ontology"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, ".rhizome", "config.yml"), []byte("notes:\n  includes: [\"**/*.md\"]\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, ".rhizome", "ontology", "schema.graphql"), []byte(`
type Person @node(paths: ["people/*.md"]) {
  name: String!
}
`), 0o644))

	store, err := codeanchorsqlite.Open(filepath.Join(root, ".rhizome", "db.sqlite"))
	require.NoError(t, err)
	defer func() { _ = store.Close() }()

	noteReader := &countingNoteReader{}
	p := Planner{
		Deps: Deps{
			IntelStore: store,
			VaultPath:  root,
			VaultDef:   obsidian.VaultDefinition{Path: root},
			NoteReader: noteReader,
		},
		Options: Options{
			EnableGraph: true,
			EnableIntel: true,
			MaxPerOwner: 3,
		},
	}

	_, err = p.Plan(context.Background(), search.QuerySpec{
		Text:   "alice team",
		Intent: search.IntentSearch,
		Limits: search.Limits{Total: 10},
	})
	require.NoError(t, err)
	require.Zero(t, noteReader.getNotesListCalls)
	require.Zero(t, noteReader.getModTimeCalls)
}

func TestShouldInspectCorpusShape_SkipsPrecisionIntents(t *testing.T) {
	require.False(t, shouldInspectCorpusShape(search.QuerySpec{Intent: search.IntentGoToDef, Text: "Service"}))
	require.False(t, shouldInspectCorpusShape(search.QuerySpec{Intent: search.IntentFindUsages, Text: "Service"}))
	require.True(t, shouldInspectCorpusShape(search.QuerySpec{Intent: search.IntentOverview, Text: "search architecture"}))
	require.True(t, shouldInspectCorpusShape(search.QuerySpec{Intent: search.IntentSubsystemOverview, Text: "search subsystem overview"}))
}

func TestPlanner_UsesGraphNoteReaderOnlyForGraphRetriever(t *testing.T) {
	baseReader := &countingNoteReader{}
	graphReader := &countingNoteReader{}
	root := t.TempDir()
	p := Planner{
		Deps: Deps{
			VaultPath:       root,
			VaultDef:        obsidian.VaultDefinition{Path: root},
			NoteReader:      baseReader,
			GraphNoteReader: graphReader,
		},
		Options: Options{
			EnableGraph: true,
			GraphSource: retrieval.GraphSourceIndexedOnly,
			MaxPerOwner: 3,
		},
	}

	plan, err := p.Plan(context.Background(), search.QuerySpec{
		Text:   "roadmap",
		Seeds:  []knowledge.Handle{knowledge.NoteHandle("notes/project.md")},
		Intent: search.IntentSearch,
		Limits: search.Limits{Total: 10},
	})
	require.NoError(t, err)

	var sawLexical bool
	var sawGraph bool
	for _, retriever := range plan.Retrievers {
		switch current := retriever.(type) {
		case *retrieval.NoteLexicalRetriever:
			sawLexical = true
			require.Same(t, baseReader, current.NoteReader)
		case *retrieval.GraphRetriever:
			sawGraph = true
			require.Same(t, graphReader, current.NoteReader)
			require.Equal(t, retrieval.GraphSourceIndexedOnly, current.SourcePolicy)
		}
	}
	require.True(t, sawLexical)
	require.True(t, sawGraph)
}

func planHasRetrieverNamed(plan Plan, name string) bool {
	for _, retriever := range plan.Retrievers {
		if retriever.Name() == name {
			return true
		}
	}
	return false
}

func TestValidateIntentAllowsOverview(t *testing.T) {
	t.Parallel()
	for _, intent := range []string{"overview", "subsystem_overview"} {
		t.Run(intent, func(t *testing.T) { require.NoError(t, ValidateIntent(intent)) })
	}
}
