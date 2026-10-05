package ontology

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	codeanchorsqlite "github.com/atomicobject/rhizome/pkg/anchors/sqlite"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"
)

const assessmentFlagsSchema = `
type Team @node(paths: ["teams/*.md"]) {
  name: String!
  members: [Person!] @link(inverse: "team")
}

type Person @node(paths: ["people/*.md"]) {
  name: String!
  team: Team @link(inverse: "members")
}

type Alpha @node(paths: ["shared/*.md"]) {
  name: String
}

type Beta @node(paths: ["shared/*.md"]) {
  name: String
}
`

// TestAssessmentRowsCarryMaterializedFlags proves every persisted assessment
// row's flags agree with the JSON it was serialized from, across the full
// build, the incremental sync, and the post-inverse-issue rewrite.
func TestAssessmentRowsCarryMaterializedFlags(t *testing.T) {
	root := t.TempDir()
	writeOntologyTestConfig(t, root)
	writeOntologySchema(t, root, assessmentFlagsSchema)
	writeOntologyNote(t, root, "teams/Eng.md", `---
type: Team
name: Eng
members:
  - people/Alice.md
---
`)
	writeOntologyNote(t, root, "people/Alice.md", `---
type: Person
name: Alice
team: teams/Eng.md
---
`)
	writeOntologyNote(t, root, "people/Dave.md", `---
type: Person
name: Dave
---
`)
	writeOntologyNote(t, root, "shared/ambiguous.md", `---
name: Ambiguous
---
`)
	writeOntologyNote(t, root, "people/Missing.md", `---
type: Person
team: teams/Gone.md
---
`)

	dbPath := filepath.Join(root, ".rhizome", "db.sqlite")
	require.NoError(t, os.MkdirAll(filepath.Dir(dbPath), 0o755))
	store, err := codeanchorsqlite.Open(dbPath)
	require.NoError(t, err)
	defer func() { _ = store.Close() }()

	ctx := context.Background()
	vaultDef := obsidian.VaultDefinition{Path: root}
	note := &obsidian.Note{}
	_, err = EnsureFreshRuntimeWithStore(ctx, testNoteMetadataIndexer(t), vaultDef, note, store)
	require.NoError(t, err)

	allPaths := []string{
		"teams/Eng.md",
		"people/Alice.md",
		"people/Dave.md",
		"people/Missing.md",
		"shared/ambiguous.md",
	}
	requireFlagParity(t, ctx, store, allPaths)

	rows, err := store.OntologyAssessmentsByPaths(ctx, allPaths)
	require.NoError(t, err)
	require.True(t, rows["people/Missing.md"].HasIssues, "missing required name must flag issues")
	require.True(t, rows["shared/ambiguous.md"].TypeAmbiguous, "two equally specific selectors must flag ambiguity")
	require.False(t, rows["people/Alice.md"].HasIssues)
	require.False(t, rows["people/Alice.md"].TypeAmbiguous)
	require.False(t, rows["teams/Eng.md"].HasIssues)

	// Edit the team so it claims a member that does not point back: the
	// inverse issue is appended in the incremental finaliser, after the row
	// JSON was first serialized.
	writeOntologyNote(t, root, "teams/Eng.md", `---
type: Team
name: Eng
members:
  - people/Alice.md
  - people/Dave.md
---
`)
	require.NoError(t, testNoteMetadataIndexer(t).SyncPaths(ctx, vaultDef, note, store, []string{"teams/Eng.md"}, nil))
	result, err := SyncPaths(ctx, testNoteMetadataIndexer(t), vaultDef, note, store, nil, []string{"teams/Eng.md"}, nil)
	require.NoError(t, err)
	require.False(t, result.Rebuilt)

	requireFlagParity(t, ctx, store, allPaths)
	rows, err = store.OntologyAssessmentsByPaths(ctx, allPaths)
	require.NoError(t, err)
	require.True(t, rows["teams/Eng.md"].HasIssues, "inverse issue appended after serialization must refresh the flag")
}

func requireFlagParity(t *testing.T, ctx context.Context, store *codeanchorsqlite.Store, paths []string) {
	t.Helper()
	rows, err := store.OntologyAssessmentsByPaths(ctx, paths)
	require.NoError(t, err)
	require.Len(t, rows, len(paths))
	for path, row := range rows {
		decoded, err := AssessmentFromJSON(row.AssessmentJSON)
		require.NoError(t, err, path)
		want := FlagsForAssessment(decoded)
		require.Equal(t, want, codeanchorsqlite.OntologyAssessmentFlags{
			HasIssues:     row.HasIssues,
			TypeAmbiguous: row.TypeAmbiguous,
		}, path)
	}
}

// TestEnsureIndexedRebuildsWhenMaterializationVersionIsStale proves a database
// written by the previous materializer is rebuilt with the flags populated.
func TestEnsureIndexedRebuildsWhenMaterializationVersionIsStale(t *testing.T) {
	root := t.TempDir()
	writeOntologyTestConfig(t, root)
	writeOntologySchema(t, root, assessmentFlagsSchema)
	writeOntologyNote(t, root, "people/Missing.md", `---
type: Person
team: teams/Gone.md
---
`)

	dbPath := filepath.Join(root, ".rhizome", "db.sqlite")
	require.NoError(t, os.MkdirAll(filepath.Dir(dbPath), 0o755))
	store, err := codeanchorsqlite.Open(dbPath)
	require.NoError(t, err)
	defer func() { _ = store.Close() }()

	ctx := context.Background()
	vaultDef := obsidian.VaultDefinition{Path: root}
	note := &obsidian.Note{}
	_, err = EnsureFreshRuntimeWithStore(ctx, testNoteMetadataIndexer(t), vaultDef, note, store)
	require.NoError(t, err)

	stored, err := store.OntologyAssessmentsByPaths(ctx, []string{"people/Missing.md"})
	require.NoError(t, err)
	stale := stored["people/Missing.md"]
	require.True(t, stale.HasIssues)
	stale.HasIssues = false
	stale.TypeAmbiguous = false

	schema, err := LoadSchema(root)
	require.NoError(t, err)
	noteState, err := store.GetNoteMetadataState(ctx)
	require.NoError(t, err)
	require.NoError(t, store.ReplaceOntologySnapshot(ctx, codeanchorsqlite.OntologySnapshot{
		Assessments: []codeanchorsqlite.OntologyNoteAssessmentRow{stale},
		SchemaState: codeanchorsqlite.OntologySchemaState{
			SchemaHash:             schema.Hash,
			NotesHash:              noteState.NotesHash,
			MaterializationVersion: OntologyMaterializationVersion - 1,
			LoadedAt:               noteState.LoadedAt,
			Ready:                  true,
		},
	}))

	result, err := EnsureIndexed(ctx, testNoteMetadataIndexer(t), vaultDef, note, store)
	require.NoError(t, err)
	require.True(t, result.Dirty)

	state, err := store.GetOntologySchemaState(ctx)
	require.NoError(t, err)
	require.Equal(t, OntologyMaterializationVersion, state.MaterializationVersion)

	rows, err := store.OntologyAssessmentsByPaths(ctx, []string{"people/Missing.md"})
	require.NoError(t, err)
	require.True(t, rows["people/Missing.md"].HasIssues)
}
