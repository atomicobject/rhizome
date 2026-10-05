package noderead

import (
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	semdb "github.com/atomicobject/rhizome/pkg/anchors/sqlite"
	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/testutil/sqlitefixture"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"
)

func TestScopeWalkPreservesStableWalkShape(t *testing.T) {
	root := t.TempDir()
	store := openWalkStore(t, root)
	ctx := context.Background()
	require.NoError(t, os.MkdirAll(filepath.Join(root, "people"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(root, "teams"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(root, "projects"), 0o755))
	aliceContent := "---\nSummary: Platform lead\nprivate: hidden\n---\n"
	teamContent := "# Engineering\n"
	projectContent := "# Rhizome\n"
	require.NoError(t, os.WriteFile(filepath.Join(root, "people", "Alice.md"), []byte(aliceContent), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, "teams", "Eng.md"), []byte(teamContent), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, "projects", "Rhizome.md"), []byte(projectContent), 0o644))
	hash := func(content string) string { return fmt.Sprintf("%x", sha256.Sum256([]byte(content))) }
	require.NoError(t, store.ReplaceNoteMetadataSnapshot(ctx, semdb.NoteMetadataSnapshot{
		State: semdb.NoteMetadataState{LoadedAt: 1, Ready: true},
		Notes: []semdb.NoteMetadataRow{
			{Path: "people/Alice.md", ContentHash: hash(aliceContent), FormatID: "markdown", Projection: semdb.NoteProjectionState{Status: semdb.NoteProjectionStatusCurrent, ProviderVersion: "test", ProjectionVersion: "test", SourceContentHash: hash(aliceContent)}},
			{Path: "teams/Eng.md", ContentHash: hash(teamContent), FormatID: "markdown", Projection: semdb.NoteProjectionState{Status: semdb.NoteProjectionStatusCurrent, ProviderVersion: "test", ProjectionVersion: "test", SourceContentHash: hash(teamContent)}},
			{Path: "projects/Rhizome.md", ContentHash: hash(projectContent), FormatID: "markdown", Projection: semdb.NoteProjectionState{Status: semdb.NoteProjectionStatusCurrent, ProviderVersion: "test", ProjectionVersion: "test", SourceContentHash: hash(projectContent)}},
		},
	}))

	require.NoError(t, store.ReplaceOntologySnapshot(ctx, semdb.OntologySnapshot{
		NoteTypes: []semdb.OntologyNoteTypeRow{
			{NotePath: "people/Alice.md", TypeName: "Person", SchemaHash: "abc", UpdatedAt: 1},
			{NotePath: "teams/Eng.md", TypeName: "Team", SchemaHash: "abc", UpdatedAt: 1},
			{NotePath: "projects/Rhizome.md", TypeName: "Project", SchemaHash: "abc", UpdatedAt: 1},
		},
		Edges: []semdb.OntologyEdgeRow{
			{SrcPath: "teams/Eng.md", RelationName: "members", DstPath: "people/Alice.md", DstType: "Person", Provenance: "inverse", Structural: true, SchemaHash: "abc", UpdatedAt: 1},
			{SrcPath: "people/Alice.md", RelationName: "team", DstPath: "teams/Eng.md", DstType: "Team", Provenance: "field", Structural: true, SchemaHash: "abc", UpdatedAt: 1},
			{SrcPath: "people/Alice.md", RelationName: "mentions", DstPath: "projects/Rhizome.md", DstType: "Project", Provenance: "body_link", Structural: false, SchemaHash: "abc", UpdatedAt: 1},
		},
		SchemaState: semdb.OntologySchemaState{SchemaHash: "abc", NotesHash: "def", LoadedAt: 1, Ready: true},
	}))

	schema := &ontology.Schema{Types: map[string]*ontology.NoteType{
		"Person":  {Name: "Person"},
		"Team":    {Name: "Team"},
		"Project": {Name: "Project"},
	}}
	vaultDef := obsidian.VaultDefinition{Path: root}
	noteReader := &obsidian.Note{}
	scope := NewService(vaultDef, noteReader, store, schema).NewScope(ctx, ScopeOptions{})

	result, err := scope.Walk(ctx, noteReader, vaultDef, "people/Alice.md", ontology.WalkOptions{
		MaxDepth:       1,
		IncludeAmbient: true,
	})
	require.NoError(t, err)
	require.Equal(t, "Person", result.SeedType)
	require.Equal(t, []ontology.WalkNode{
		{Path: "people/Alice.md", TypeName: "Person", Title: "Alice"},
		{Path: "projects/Rhizome.md", TypeName: "Project", Title: "Rhizome"},
		{Path: "teams/Eng.md", TypeName: "Team", Title: "Eng"},
	}, result.Nodes)
	require.Equal(t, []ontology.WalkEdge{
		{RelationName: "mentions", Source: "people/Alice.md", Destination: "projects/Rhizome.md", Provenance: "body_link", Structural: false},
		{RelationName: "team", Source: "people/Alice.md", Destination: "teams/Eng.md", Provenance: "field", Structural: true},
		{RelationName: "members", Source: "teams/Eng.md", Destination: "people/Alice.md", Provenance: "inverse", Structural: true},
	}, result.Edges)

	filtered, err := scope.Walk(ctx, noteReader, vaultDef, "people/Alice.md", ontology.WalkOptions{
		MaxDepth:       1,
		RelationFilter: "team",
		IncludeAmbient: true,
	})
	require.NoError(t, err)
	require.Equal(t, []ontology.WalkEdge{{RelationName: "team", Source: "people/Alice.md", Destination: "teams/Eng.md", Provenance: "field", Structural: true}}, filtered.Edges)

	structural, err := scope.Walk(ctx, noteReader, vaultDef, "people/Alice.md", ontology.WalkOptions{MaxDepth: 1})
	require.NoError(t, err)
	require.Len(t, structural.Edges, 2)
	for _, edge := range structural.Edges {
		require.True(t, edge.Structural)
	}
}

func TestScopeWalkDoesNotTruncateHighDegreeNeighborhood(t *testing.T) {
	root := t.TempDir()
	store := openWalkStore(t, root)
	ctx := context.Background()

	noteTypes := []semdb.OntologyNoteTypeRow{
		{NotePath: "notes/seed.md", TypeName: "Project", SchemaHash: "abc", UpdatedAt: 1},
	}
	edges := make([]semdb.OntologyEdgeRow, 0, 140)
	for i := 0; i < 140; i++ {
		target := fmt.Sprintf("notes/decision-%03d.md", i)
		noteTypes = append(noteTypes, semdb.OntologyNoteTypeRow{
			NotePath:   target,
			TypeName:   "Decision",
			SchemaHash: "abc",
			UpdatedAt:  1,
		})
		edges = append(edges, semdb.OntologyEdgeRow{
			SrcPath:      "notes/seed.md",
			RelationName: "decisions",
			DstPath:      target,
			DstType:      "Decision",
			Provenance:   "body_link",
			Structural:   false,
			SchemaHash:   "abc",
			UpdatedAt:    1,
		})
	}
	require.NoError(t, store.ReplaceOntologySnapshot(ctx, semdb.OntologySnapshot{
		NoteTypes:   noteTypes,
		Edges:       edges,
		SchemaState: semdb.OntologySchemaState{SchemaHash: "abc", NotesHash: "def", LoadedAt: 1, Ready: true},
	}))

	schema := &ontology.Schema{Types: map[string]*ontology.NoteType{
		"Project":  {Name: "Project"},
		"Decision": {Name: "Decision"},
	}}
	vaultDef := obsidian.VaultDefinition{Path: root}
	scope := NewService(vaultDef, &obsidian.Note{}, store, schema).NewScope(ctx, ScopeOptions{})
	result, err := scope.Walk(ctx, &obsidian.Note{}, vaultDef, "notes/seed.md", ontology.WalkOptions{MaxDepth: 1, IncludeAmbient: true})
	require.NoError(t, err)
	require.Len(t, result.Nodes, 141)
	require.Len(t, result.Edges, 140)
	require.Equal(t, "notes/decision-000.md", result.Edges[0].Destination)
	require.Equal(t, "notes/decision-139.md", result.Edges[139].Destination)
}

func TestScopeWalkHonorsDepthLimitAndDefault(t *testing.T) {
	root := t.TempDir()
	store := openWalkStore(t, root)
	ctx := context.Background()
	require.NoError(t, store.ReplaceOntologySnapshot(ctx, semdb.OntologySnapshot{
		NoteTypes: []semdb.OntologyNoteTypeRow{
			{NotePath: "notes/seed.md", TypeName: "Note", SchemaHash: "abc", UpdatedAt: 1},
			{NotePath: "notes/one.md", TypeName: "Note", SchemaHash: "abc", UpdatedAt: 1},
			{NotePath: "notes/two.md", TypeName: "Note", SchemaHash: "abc", UpdatedAt: 1},
			{NotePath: "notes/three.md", TypeName: "Note", SchemaHash: "abc", UpdatedAt: 1},
		},
		Edges: []semdb.OntologyEdgeRow{
			{SrcPath: "notes/seed.md", RelationName: "next", DstPath: "notes/one.md", DstType: "Note", Provenance: "field", Structural: true, SchemaHash: "abc", UpdatedAt: 1},
			{SrcPath: "notes/one.md", RelationName: "next", DstPath: "notes/two.md", DstType: "Note", Provenance: "field", Structural: true, SchemaHash: "abc", UpdatedAt: 1},
			{SrcPath: "notes/two.md", RelationName: "next", DstPath: "notes/three.md", DstType: "Note", Provenance: "field", Structural: true, SchemaHash: "abc", UpdatedAt: 1},
		},
		SchemaState: semdb.OntologySchemaState{SchemaHash: "abc", NotesHash: "def", LoadedAt: 1, Ready: true},
	}))

	vaultDef := obsidian.VaultDefinition{Path: root}
	scope := NewService(vaultDef, &obsidian.Note{}, store, &ontology.Schema{}).NewScope(ctx, ScopeOptions{})
	depthOne, err := scope.Walk(ctx, &obsidian.Note{}, vaultDef, "notes/seed.md", ontology.WalkOptions{MaxDepth: 1})
	require.NoError(t, err)
	require.Len(t, depthOne.Nodes, 2)
	require.Len(t, depthOne.Edges, 1)

	defaultDepth, err := scope.Walk(ctx, &obsidian.Note{}, vaultDef, "notes/seed.md", ontology.WalkOptions{})
	require.NoError(t, err)
	require.Len(t, defaultDepth.Nodes, 3)
	require.Len(t, defaultDepth.Edges, 2)
}

func TestScopeWalkHonorsCancellationAndMissingSeed(t *testing.T) {
	root := t.TempDir()
	store := openWalkStore(t, root)
	vaultDef := obsidian.VaultDefinition{Path: root}
	scope := NewService(vaultDef, &obsidian.Note{}, store, &ontology.Schema{}).NewScope(context.Background(), ScopeOptions{})

	missing, err := scope.Walk(context.Background(), &obsidian.Note{}, vaultDef, "missing.md", ontology.WalkOptions{})
	require.NoError(t, err)
	require.Equal(t, &ontology.WalkResult{}, missing)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = scope.Walk(ctx, &obsidian.Note{}, vaultDef, "missing.md", ontology.WalkOptions{})
	require.ErrorIs(t, err, context.Canceled)
}

func openWalkStore(t *testing.T, root string) *semdb.Store {
	t.Helper()
	dbPath := filepath.Join(root, ".rhizome", "db.sqlite")
	require.NoError(t, os.MkdirAll(filepath.Dir(dbPath), 0o755))
	store, err := sqlitefixture.Open(dbPath)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, store.Close()) })
	return store
}
