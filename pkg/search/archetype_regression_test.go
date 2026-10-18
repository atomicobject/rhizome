package search_test

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/atomicobject/rhizome/pkg/embeddings"
	"github.com/atomicobject/rhizome/pkg/noteformat/builtin"
	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/search"
	"github.com/atomicobject/rhizome/pkg/search/knowledge"
	"github.com/atomicobject/rhizome/pkg/search/planner"
	"github.com/atomicobject/rhizome/pkg/search/semantic"
	semdb "github.com/atomicobject/rhizome/pkg/store"
	"github.com/stretchr/testify/require"
)

func TestArchetypeRegression_MixedVaultDocsForCodeKeepsDocsAndCodeNearTop(t *testing.T) {
	spec := search.QuerySpec{Text: "architecture", Intent: search.IntentDocsForCode, Seeds: []knowledge.Handle{knowledge.FileHandle("pkg/search/service.go")}, Limits: search.Limits{Total: 4}}
	plan := productionArchetypePlan(t, spec, 2)

	results, err := plan.Ranker.Rank(context.Background(), spec, []search.Candidate{
		{SourceRef: search.SourceRef{Path: "notes/architecture.md"}, Handle: knowledge.NoteHandle("notes/architecture.md"),
			Owner: knowledge.NoteHandle("notes/architecture.md"),
			Type:  "note",
			Evidence: []search.Evidence{
				{Type: "doc_link", RawScore: 1.0},
				{Type: "module_doc", RawScore: 0.9},
			},
		},
		{SourceRef: search.SourceRef{Path: "pkg/search/service.go"}, Handle: knowledge.FileHandle("pkg/search/service.go"),
			Owner: knowledge.FileHandle("pkg/search/service.go"),
			Type:  "code",
			Evidence: []search.Evidence{
				{Type: "code_vector_similarity", RawScore: 0.8},
				{Type: "code_ref", RawScore: 0.6},
			},
		},
		{SourceRef: search.SourceRef{Path: "notes/glossary.md"}, Handle: knowledge.NoteHandle("notes/glossary.md"),
			Owner: knowledge.NoteHandle("notes/glossary.md"),
			Type:  "note",
			Evidence: []search.Evidence{
				{Type: "intel_fts_match", RawScore: 0.35},
			},
		},
	})
	require.NoError(t, err)
	require.GreaterOrEqual(t, len(results), 2)
	require.Equal(t, "note", results[0].Type)
	require.Equal(t, "code", results[1].Type)
}

func TestArchetypeRegression_OntologyHeavyPrefersStructuralOverAmbient(t *testing.T) {
	spec := search.QuerySpec{Intent: search.IntentRelatedToSeed, Seeds: []knowledge.Handle{knowledge.NoteHandle("notes/root.md")}, Limits: search.Limits{Total: 3}}
	plan := productionArchetypePlan(t, spec, 1)
	results, err := plan.Ranker.Rank(context.Background(), spec, []search.Candidate{
		{SourceRef: search.SourceRef{Path: "notes/decision-a.md"}, Handle: knowledge.NoteHandle("notes/decision-a.md"),
			Owner:    knowledge.NoteHandle("notes/decision-a.md"),
			Type:     "note",
			Evidence: []search.Evidence{{Type: "ontology_relation_structural", RawScore: 1.25}},
		},
		{SourceRef: search.SourceRef{Path: "notes/runbook-a.md"}, Handle: knowledge.NoteHandle("notes/runbook-a.md"),
			Owner:    knowledge.NoteHandle("notes/runbook-a.md"),
			Type:     "note",
			Evidence: []search.Evidence{{Type: "ontology_relation_ambient", RawScore: 1.05}},
		},
	})
	require.NoError(t, err)
	require.Equal(t, "notes/decision-a.md", results[0].Path)
}

type fixedRetriever struct {
	name       string
	candidates []search.Candidate
}

func (r fixedRetriever) Name() string { return r.name }

func (r fixedRetriever) Retrieve(context.Context, search.QuerySpec) ([]search.Candidate, error) {
	return r.candidates, nil
}

// A note found by the vector lane and the title lane is one source, so its
// lanes add up. Before whole-note candidates were coalesced before ranking,
// each lane's row was scored alone and a note with a slightly higher cosine
// and no title match led (dogfood query "Innovation teams", 2026-10-08).
func TestArchetypeRegression_WholeNoteLaneAgreementOutranksOneLane(t *testing.T) {
	target := "Projects/Opportunity - Volunteer drivers and rural routes.md"
	rival := "Notes/Drivers.md"
	wholeNoteChunk := func(path string, cosine float64) search.Candidate {
		return search.Candidate{
			SourceRef: search.SourceRef{
				Path:        path,
				Granularity: "node_body",
			},
			Handle:   knowledge.NodeChunkHandle("node-"+path, path, "node_body", 0),
			Owner:    knowledge.NoteHandle(path),
			Type:     "note",
			Evidence: []search.Evidence{{Type: "note_vector_similarity", RawScore: cosine}},
		}
	}
	titleMatch := search.Candidate{
		SourceRef: search.SourceRef{
			Path:       target,
			ChunkIndex: -1,
		},
		Handle:   knowledge.NoteHandle(target),
		Owner:    knowledge.NoteHandle(target),
		Type:     "note",
		Evidence: []search.Evidence{{Type: "note_title_match", RawScore: 1}},
	}
	spec := search.QuerySpec{Text: "volunteer drivers", Limits: search.Limits{Total: 10}}
	service := productionArchetypePlan(t, spec, 3)
	service.Retrievers = []search.Retriever{
		fixedRetriever{name: "vector", candidates: []search.Candidate{wholeNoteChunk(rival, 0.66), wholeNoteChunk(target, 0.62)}},
		fixedRetriever{name: "note_lexical", candidates: []search.Candidate{titleMatch}},
	}

	response, err := service.Search(context.Background(), spec)
	require.NoError(t, err)
	require.Len(t, response.Results, 2)
	require.Equal(t, target, response.Results[0].Path)
	require.Equal(t, "node_body", response.Results[0].Granularity, "the best whole-note chunk stays the displayed node")
	var types []string
	for _, ev := range response.Results[0].Evidence {
		types = append(types, ev.Type)
	}
	require.Contains(t, types, "note_vector_similarity")
	require.Contains(t, types, "note_title_match")
}

// Linking notes that agree on a label name the note they link to. A note
// only its linking notes call "catalog migration" ranked below a linking note
// with those words in its own title; the agreed label now counts like the
// target's title, and without it the rival leads (EFF-2026-10-08-16-23).
func TestArchetypeRegression_AgreedLinkLabelNamesTheTarget(t *testing.T) {
	target := "Projects/Project Kestrel.md"
	rival := "Log/Catalog migration kickoff.md"
	rivalText := search.Candidate{
		SourceRef: search.SourceRef{
			Path:       rival,
			ChunkIndex: -1,
		},
		Handle:   knowledge.NoteHandle(rival),
		Owner:    knowledge.NoteHandle(rival),
		Type:     "note",
		Evidence: []search.Evidence{{Type: "intel_fts_match", RawScore: 0.5}},
	}
	vector := func(path string, chunk int, cosine float64) search.Candidate {
		return search.Candidate{
			SourceRef: search.SourceRef{Path: path},
			Handle:    knowledge.NodeChunkHandle("node-"+path, path, "node_section", chunk),
			Owner:     knowledge.NoteHandle(path),
			Type:      "note",
			Evidence:  []search.Evidence{{Type: "note_vector_similarity", RawScore: cosine}},
		}
	}
	linkText := search.Candidate{
		SourceRef: search.SourceRef{
			Path:       target,
			ChunkIndex: -1,
		},
		Handle: knowledge.NoteHandle(target),
		Owner:  knowledge.NoteHandle(target),
		Type:   "note",
		Evidence: []search.Evidence{{Type: "link_text_match", RawScore: 0.875, Source: "link_text", Details: map[string]string{
			search.LinkTextAliasDetail: "catalog migration", "linking_notes": "3",
		}}},
	}
	spec := search.QuerySpec{Text: "catalog migration", Limits: search.Limits{Total: 10}}
	service := productionArchetypePlan(t, spec, 1)
	service.Retrievers = []search.Retriever{
		fixedRetriever{name: "vector", candidates: []search.Candidate{vector(rival, 0, 0.66), vector(target, 0, 0.55), vector(target, 1, 0.54)}},
		fixedRetriever{name: "link_text", candidates: []search.Candidate{linkText}},
		fixedRetriever{name: "intel", candidates: []search.Candidate{rivalText}},
	}

	response, err := service.Search(context.Background(), spec)
	require.NoError(t, err)
	var paths []string
	for _, result := range response.Results {
		paths = append(paths, result.Path)
	}
	require.Equal(t, []string{target, rival}, paths, "the named target leads, once per owner")
}

func productionArchetypePlan(t *testing.T, spec search.QuerySpec, maxPerOwner int) planner.Plan {
	t.Helper()
	ctx := context.Background()
	root := t.TempDir()
	store, err := semdb.Open(filepath.Join(root, "intel.sqlite"))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, store.Close()) })
	require.NoError(t, store.ReplaceNoteMetadataSnapshot(ctx, semdb.NoteMetadataSnapshot{
		State: semdb.NoteMetadataState{NotesHash: "archetype-notes", Ready: true, LoadedAt: 1},
	}))
	require.NoError(t, store.ReplaceOntologySnapshot(ctx, semdb.OntologySnapshot{
		SchemaState: semdb.OntologySchemaState{SchemaHash: "archetype-schema", NotesHash: "archetype-notes", MaterializationVersion: ontology.OntologyMaterializationVersion, Ready: true, LoadedAt: 1},
	}))

	formats, err := builtin.NewRuntime()
	require.NoError(t, err)
	state, err := store.GetOntologySchemaState(ctx)
	require.NoError(t, err)
	metadata, err := store.GetNoteMetadataState(ctx)
	require.NoError(t, err)
	require.NoError(t, store.ApplyOntologyDelta(ctx, semdb.OntologyDelta{Readiness: &semdb.OntologyReadinessPublication{
		SchemaState: state, MetadataState: metadata, ProviderHash: ontology.OntologyProviderHash(formats),
		Validate: func(semdb.OntologyReadinessSnapshot) (bool, error) { return true, nil },
	}}))
	provider := embeddings.NewDeterministicProvider(embeddings.ProviderConfig{Provider: "test", Dimensions: 4})
	p := planner.Planner{
		Deps:    planner.Deps{IntelStore: store, NoteFormats: formats, Semantic: &semantic.Searcher{IntelStore: store, NoteProvider: provider, CodeProvider: provider}, VaultPath: root},
		Options: planner.Options{EnableVector: true, EnableIntel: true, EnableRefs: true, EnableGraph: true, MaxPerOwner: maxPerOwner},
	}
	plan, err := p.Plan(ctx, spec)
	require.NoError(t, err)
	return plan
}
