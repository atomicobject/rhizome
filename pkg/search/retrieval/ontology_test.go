package retrieval

import (
	"context"
	"path/filepath"
	"testing"

	codeanchorsqlite "github.com/atomicobject/rhizome/pkg/anchors/sqlite"
	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/search"
	"github.com/atomicobject/rhizome/pkg/search/knowledge"
	"github.com/stretchr/testify/require"
)

func TestOntologyRetriever_ReturnsStructuralAndAmbientNeighbors(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "db.sqlite")
	store, err := codeanchorsqlite.Open(dbPath)
	require.NoError(t, err)
	defer func() { _ = store.Close() }()

	err = store.ReplaceOntologySnapshot(context.Background(), codeanchorsqlite.OntologySnapshot{
		NoteTypes: []codeanchorsqlite.OntologyNoteTypeRow{
			{NotePath: "people/Alice.MD", TypeName: "Person", SchemaHash: "abc", UpdatedAt: 1},
			{NotePath: "teams/Eng.html", TypeName: "Team", SchemaHash: "abc", UpdatedAt: 1},
			{NotePath: "people/Bob.MD", TypeName: "Person", SchemaHash: "abc", UpdatedAt: 1},
		},
		Edges: []codeanchorsqlite.OntologyEdgeRow{
			{SrcPath: "people/Alice.MD", RelationName: "team", DstPath: "teams/Eng.html", DstType: "Team", Provenance: "field", Structural: true, SchemaHash: "abc", UpdatedAt: 1},
			{SrcPath: "people/Alice.MD", RelationName: "related", DstPath: "people/Bob.MD", DstType: "Person", Provenance: "body_link", Structural: false, SchemaHash: "abc", UpdatedAt: 1},
		},
		SchemaState: codeanchorsqlite.OntologySchemaState{SchemaHash: "abc", NotesHash: "def", LoadedAt: 1, Ready: true},
	})
	require.NoError(t, err)

	r := &OntologyRetriever{Store: store, IncludeAmbient: true, Limit: 10}
	results, err := r.Retrieve(context.Background(), search.QuerySpec{
		Seeds: []knowledge.Handle{knowledge.NoteChunkHandle("people/Alice.MD", 0)},
	})
	require.NoError(t, err)
	require.Len(t, results, 2)

	byPath := make(map[string]search.Candidate, len(results))
	for _, cand := range results {
		byPath[cand.Path] = cand
	}

	require.Contains(t, byPath, "teams/Eng.html")
	require.Equal(t, "ontology_relation_structural", byPath["teams/Eng.html"].Evidence[0].Type)
	require.Equal(t, "team", byPath["teams/Eng.html"].Evidence[0].Details["relation"])
	require.Equal(t, "field", byPath["teams/Eng.html"].Evidence[0].Details["provenance"])

	require.Contains(t, byPath, "people/Bob.MD")
	require.Equal(t, "ontology_relation_ambient", byPath["people/Bob.MD"].Evidence[0].Type)
	require.Equal(t, "body_link", byPath["people/Bob.MD"].Evidence[0].Details["provenance"])
}

func TestOntologyRetriever_DocsForCodePrefersStructuralRelations(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "db.sqlite")
	store, err := codeanchorsqlite.Open(dbPath)
	require.NoError(t, err)
	defer func() { _ = store.Close() }()

	err = store.ReplaceOntologySnapshot(context.Background(), codeanchorsqlite.OntologySnapshot{
		NoteTypes: []codeanchorsqlite.OntologyNoteTypeRow{
			{NotePath: "projects/Atlas.md", TypeName: "Project", SchemaHash: "abc", UpdatedAt: 1},
			{NotePath: "decisions/Schema.md", TypeName: "Decision", SchemaHash: "abc", UpdatedAt: 1},
			{NotePath: "notes/Loose.md", TypeName: "Note", SchemaHash: "abc", UpdatedAt: 1},
		},
		Edges: []codeanchorsqlite.OntologyEdgeRow{
			{SrcPath: "projects/Atlas.md", RelationName: "decisions", DstPath: "decisions/Schema.md", DstType: "Decision", Provenance: "field", Structural: true, SchemaHash: "abc", UpdatedAt: 1},
			{SrcPath: "projects/Atlas.md", RelationName: "related", DstPath: "notes/Loose.md", DstType: "Note", Provenance: "body_link", Structural: false, SchemaHash: "abc", UpdatedAt: 1},
		},
		SchemaState: codeanchorsqlite.OntologySchemaState{SchemaHash: "abc", NotesHash: "def", LoadedAt: 1, Ready: true},
	})
	require.NoError(t, err)

	r := &OntologyRetriever{Store: store, IncludeAmbient: true, Limit: 10}
	results, err := r.Retrieve(context.Background(), search.QuerySpec{
		Intent: search.IntentDocsForCode,
		Seeds:  []knowledge.Handle{knowledge.NoteHandle("projects/Atlas.md")},
		Text:   "decision records",
	})
	require.NoError(t, err)
	require.Len(t, results, 1)
	require.Equal(t, "decisions/Schema.md", results[0].Path)
	require.Equal(t, "ontology_relation_structural", results[0].Evidence[0].Type)
}

func TestOntologyRetriever_SparseStructuralFallsBackToAmbient(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "db.sqlite")
	store, err := codeanchorsqlite.Open(dbPath)
	require.NoError(t, err)
	defer func() { _ = store.Close() }()

	err = store.ReplaceOntologySnapshot(context.Background(), codeanchorsqlite.OntologySnapshot{
		NoteTypes: []codeanchorsqlite.OntologyNoteTypeRow{
			{NotePath: "projects/Atlas.md", TypeName: "Project", SchemaHash: "abc", UpdatedAt: 1},
			{NotePath: "decisions/Schema.md", TypeName: "Decision", SchemaHash: "abc", UpdatedAt: 1},
			{NotePath: "notes/Roadmap.md", TypeName: "Note", SchemaHash: "abc", UpdatedAt: 1},
		},
		Edges: []codeanchorsqlite.OntologyEdgeRow{
			{SrcPath: "projects/Atlas.md", RelationName: "decisions", DstPath: "decisions/Schema.md", DstType: "Decision", Provenance: "field", Structural: true, SchemaHash: "abc", UpdatedAt: 1},
			{SrcPath: "projects/Atlas.md", RelationName: "roadmap", DstPath: "notes/Roadmap.md", DstType: "Note", Provenance: "body_link", Structural: false, SchemaHash: "abc", UpdatedAt: 1},
		},
		SchemaState: codeanchorsqlite.OntologySchemaState{SchemaHash: "abc", NotesHash: "def", LoadedAt: 1, Ready: true},
	})
	require.NoError(t, err)

	r := &OntologyRetriever{Store: store, IncludeAmbient: false, Limit: 10}
	results, err := r.Retrieve(context.Background(), search.QuerySpec{
		Intent: search.IntentSearch,
		Seeds:  []knowledge.Handle{knowledge.NoteHandle("projects/Atlas.md")},
		Text:   "roadmap",
	})
	require.NoError(t, err)
	require.Len(t, results, 2)
	require.Equal(t, "notes/Roadmap.md", results[0].Path)
}

func TestOntologyRetriever_DedupesNeighborAfterRelationScoring(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "db.sqlite")
	store, err := codeanchorsqlite.Open(dbPath)
	require.NoError(t, err)
	defer func() { _ = store.Close() }()

	err = store.ReplaceOntologySnapshot(context.Background(), codeanchorsqlite.OntologySnapshot{
		NoteTypes: []codeanchorsqlite.OntologyNoteTypeRow{
			{NotePath: "projects/Atlas.md", TypeName: "Project", SchemaHash: "abc", UpdatedAt: 1},
			{NotePath: "notes/Roadmap.md", TypeName: "Note", SchemaHash: "abc", UpdatedAt: 1},
		},
		Edges: []codeanchorsqlite.OntologyEdgeRow{
			{SrcPath: "projects/Atlas.md", RelationName: "aaa", DstPath: "notes/Roadmap.md", DstType: "Note", Provenance: "body_link", Structural: false, SchemaHash: "abc", UpdatedAt: 1},
			{SrcPath: "projects/Atlas.md", RelationName: "roadmap", DstPath: "notes/Roadmap.md", DstType: "Note", Provenance: "body_link", Structural: false, SchemaHash: "abc", UpdatedAt: 1},
		},
		SchemaState: codeanchorsqlite.OntologySchemaState{SchemaHash: "abc", NotesHash: "def", LoadedAt: 1, Ready: true},
	})
	require.NoError(t, err)

	r := &OntologyRetriever{Store: store, IncludeAmbient: true, Limit: 10}
	results, err := r.Retrieve(context.Background(), search.QuerySpec{
		Intent: search.IntentSearch,
		Seeds:  []knowledge.Handle{knowledge.NoteHandle("projects/Atlas.md")},
		Text:   "roadmap",
	})
	require.NoError(t, err)
	require.Len(t, results, 1)
	require.Equal(t, "roadmap", results[0].Evidence[0].Details["relation"])
}

func TestOntologyRetriever_SparseFallbackWhenStructuralEmpty(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "db.sqlite")
	store, err := codeanchorsqlite.Open(dbPath)
	require.NoError(t, err)
	defer func() { _ = store.Close() }()

	err = store.ReplaceOntologySnapshot(context.Background(), codeanchorsqlite.OntologySnapshot{
		NoteTypes: []codeanchorsqlite.OntologyNoteTypeRow{
			{NotePath: "projects/Atlas.md", TypeName: "Project", SchemaHash: "abc", UpdatedAt: 1},
			{NotePath: "notes/Roadmap.md", TypeName: "Note", SchemaHash: "abc", UpdatedAt: 1},
		},
		Edges: []codeanchorsqlite.OntologyEdgeRow{
			{SrcPath: "projects/Atlas.md", RelationName: "roadmap", DstPath: "notes/Roadmap.md", DstType: "Note", Provenance: "body_link", Structural: false, SchemaHash: "abc", UpdatedAt: 1},
		},
		SchemaState: codeanchorsqlite.OntologySchemaState{SchemaHash: "abc", NotesHash: "def", LoadedAt: 1, Ready: true},
	})
	require.NoError(t, err)

	r := &OntologyRetriever{Store: store, IncludeAmbient: false, Limit: 10}
	results, err := r.Retrieve(context.Background(), search.QuerySpec{
		Intent: search.IntentSearch,
		Seeds:  []knowledge.Handle{knowledge.NoteHandle("projects/Atlas.md")},
		Text:   "roadmap",
	})
	require.NoError(t, err)
	require.Len(t, results, 1)
	require.Equal(t, "notes/Roadmap.md", results[0].Path)
	require.Equal(t, "ontology_relation_ambient", results[0].Evidence[0].Type)
}

func TestOntologyRetriever_TypePolicyOverridesIntentDefault(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "db.sqlite")
	store, err := codeanchorsqlite.Open(dbPath)
	require.NoError(t, err)
	defer func() { _ = store.Close() }()

	policyJSON := ontology.MarshalTypePolicy(ontology.TypePolicy{
		TypeName: "Project",
		Overrides: []ontology.TraversalOverride{{
			Intents:        []string{"docs_for_code"},
			IncludeAmbient: ptrBool(true),
		}},
	})
	require.NotEmpty(t, policyJSON)

	err = store.ReplaceOntologySnapshot(context.Background(), codeanchorsqlite.OntologySnapshot{
		NoteTypes: []codeanchorsqlite.OntologyNoteTypeRow{
			{NotePath: "projects/Atlas.md", TypeName: "Project", SchemaHash: "abc", UpdatedAt: 1},
			{NotePath: "decisions/Schema.md", TypeName: "Decision", SchemaHash: "abc", UpdatedAt: 1},
			{NotePath: "notes/Roadmap.md", TypeName: "Note", SchemaHash: "abc", UpdatedAt: 1},
		},
		Edges: []codeanchorsqlite.OntologyEdgeRow{
			{SrcPath: "projects/Atlas.md", RelationName: "decisions", DstPath: "decisions/Schema.md", DstType: "Decision", Provenance: "field", Structural: true, SchemaHash: "abc", UpdatedAt: 1},
			{SrcPath: "projects/Atlas.md", RelationName: "roadmap", DstPath: "notes/Roadmap.md", DstType: "Note", Provenance: "body_link", Structural: false, SchemaHash: "abc", UpdatedAt: 1},
		},
		TypePolicies: []codeanchorsqlite.OntologyTypePolicyRow{
			{TypeName: "Project", PolicyJSON: policyJSON, SchemaHash: "abc", UpdatedAt: 1},
		},
		SchemaState: codeanchorsqlite.OntologySchemaState{SchemaHash: "abc", NotesHash: "def", LoadedAt: 1, Ready: true},
	})
	require.NoError(t, err)

	r := &OntologyRetriever{Store: store, IncludeAmbient: false, Limit: 10}
	results, err := r.Retrieve(context.Background(), search.QuerySpec{
		Intent: search.IntentDocsForCode,
		Seeds:  []knowledge.Handle{knowledge.NoteHandle("projects/Atlas.md")},
	})
	require.NoError(t, err)
	require.Len(t, results, 2)

	byPath := make(map[string]string, len(results))
	for _, result := range results {
		byPath[result.Path] = result.Evidence[0].Type
	}
	require.Equal(t, "ontology_relation_structural", byPath["decisions/Schema.md"])
	require.Equal(t, "ontology_relation_ambient", byPath["notes/Roadmap.md"])
}

func TestOntologyRetriever_MaxDepthTraversesBeyondDirectNeighbors(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "db.sqlite")
	store, err := codeanchorsqlite.Open(dbPath)
	require.NoError(t, err)
	defer func() { _ = store.Close() }()

	policyJSON := ontology.MarshalTypePolicy(ontology.TypePolicy{
		TypeName: "Project",
		Overrides: []ontology.TraversalOverride{{
			Intents:  []string{"related_to_seed"},
			MaxDepth: ptrInt(2),
		}},
	})
	require.NotEmpty(t, policyJSON)

	err = store.ReplaceOntologySnapshot(context.Background(), codeanchorsqlite.OntologySnapshot{
		NoteTypes: []codeanchorsqlite.OntologyNoteTypeRow{
			{NotePath: "projects/Atlas.md", TypeName: "Project", SchemaHash: "abc", UpdatedAt: 1},
			{NotePath: "decisions/Schema.md", TypeName: "Decision", SchemaHash: "abc", UpdatedAt: 1},
			{NotePath: "runbooks/Migrate.md", TypeName: "Runbook", SchemaHash: "abc", UpdatedAt: 1},
		},
		Edges: []codeanchorsqlite.OntologyEdgeRow{
			{SrcPath: "projects/Atlas.md", RelationName: "decision", DstPath: "decisions/Schema.md", DstType: "Decision", Provenance: "field", Structural: true, SchemaHash: "abc", UpdatedAt: 1},
			{SrcPath: "decisions/Schema.md", RelationName: "runbook", DstPath: "runbooks/Migrate.md", DstType: "Runbook", Provenance: "field", Structural: true, SchemaHash: "abc", UpdatedAt: 1},
		},
		TypePolicies: []codeanchorsqlite.OntologyTypePolicyRow{
			{TypeName: "Project", PolicyJSON: policyJSON, SchemaHash: "abc", UpdatedAt: 1},
		},
		SchemaState: codeanchorsqlite.OntologySchemaState{SchemaHash: "abc", NotesHash: "def", LoadedAt: 1, Ready: true},
	})
	require.NoError(t, err)

	r := &OntologyRetriever{Store: store, IncludeAmbient: false, Limit: 10}
	results, err := r.Retrieve(context.Background(), search.QuerySpec{
		Intent: search.IntentRelatedToSeed,
		Seeds:  []knowledge.Handle{knowledge.NoteHandle("projects/Atlas.md")},
	})
	require.NoError(t, err)
	require.Len(t, results, 2)

	byPath := make(map[string]search.Candidate, len(results))
	for _, result := range results {
		byPath[result.Path] = result
	}
	require.Contains(t, byPath, "decisions/Schema.md")
	require.Contains(t, byPath, "runbooks/Migrate.md")
	require.Equal(t, "1", byPath["decisions/Schema.md"].Evidence[0].Details["depth"])
	require.Equal(t, "2", byPath["runbooks/Migrate.md"].Evidence[0].Details["depth"])
	require.Equal(t, "decisions/Schema.md", byPath["runbooks/Migrate.md"].Evidence[0].Details["via"])
	require.Less(t, byPath["runbooks/Migrate.md"].Evidence[0].RawScore, byPath["decisions/Schema.md"].Evidence[0].RawScore)
}

func ptrBool(v bool) *bool { return &v }

func ptrInt(v int) *int { return &v }
