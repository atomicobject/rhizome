package query

import (
	"context"
	"github.com/atomicobject/rhizome/internal/testkit"
	"github.com/atomicobject/rhizome/pkg/noteformat"
	"github.com/atomicobject/rhizome/pkg/notemeta"
	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/ontology/readmodel"
	ontoschema "github.com/atomicobject/rhizome/pkg/ontology/schema"
	codeanchorsqlite "github.com/atomicobject/rhizome/pkg/store"
	"github.com/atomicobject/rhizome/pkg/testutil/sqlitefixture"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"
	"os"
	"path/filepath"
	"testing"
)

type queryTestEnv struct {
	root         string
	schema       *ontoschema.Schema
	execSchema   *ExecutableSchema
	store        *codeanchorsqlite.Store
	noteMetadata notemeta.Indexer
	noteFormats  noteformat.Runtime
}

func newQueryTestEnv(t *testing.T) *queryTestEnv {
	t.Helper()
	return newCustomQueryTestEnv(t, `
type Team @node(paths: ["notes/teams/*.md"]) {
  name: String!
  members: [Person!] @link(inverse: "team")
}

type Person @node(paths: ["notes/people/*.md"]) {
  """Team member name."""
  name: String!
  role: String
  team: Team @link(inverse: "members")
  projects: [Project!] @link(inverse: "owner")
}

type Decision @node(paths: ["notes/decisions/*.md"]) {
  name: String!
}

"""Project note doc."""
type Project @node(paths: ["notes/projects/*.md"]) @semantics(kind: BEHAVIORAL) {
  """Display name."""
  name: String!
  owner: Person! @link(inverse: "projects")
  decisionNotes: [Decision!] @link(source: "decisionNotes")
  decisions: [Decision!] @neighbors(direction: BOTH, type: "Decision")
}
`, map[string]string{
		"notes/teams/platform-team.md": `---
type: Team
name: Platform Team
members:
  - notes/people/alice.md
---
`,
		"notes/people/alice.md": `---
type: Person
name: Alice
role: Staff Engineer
team: notes/teams/platform-team.md
projects:
  - notes/projects/roadmap-refresh.md
---
`,
		"notes/projects/roadmap-refresh.md": `---
type: Project
name: Roadmap Refresh
owner: notes/people/alice.md
decisionNotes:
  - notes/decisions/structured-ontology-decision.md
---

We finalized follow-up work in [[ambient-linked-decision]].
`,
		"notes/decisions/structured-ontology-decision.md": `---
type: Decision
name: Structured Ontology Decision
---

Canonical structural decision note for the roadmap refresh effort.
`,
		"notes/decisions/ambient-linked-decision.md": `---
type: Decision
name: Ambient Linked Decision
---

Ambiently linked from the project body.
`,
		"notes/decisions/backlink-decision.md": `---
type: Decision
name: Backlink Decision
---

This decision references [[roadmap-refresh]] from the opposite direction.
`,
	})
}

func newCustomQueryTestEnv(t *testing.T, schemaBody string, notes map[string]string) *queryTestEnv {
	t.Helper()
	root := t.TempDir()
	writeQueryTestConfig(t, root)
	writeQueryTestSchema(t, root, schemaBody)
	for path, body := range notes {
		writeQueryTestNote(t, root, path, body)
	}

	schema, err := ontoschema.LoadSchema(root)
	require.NoError(t, err)
	execSchema, err := BuildExecutableSchema(schema)
	require.NoError(t, err)

	dbPath := filepath.Join(root, ".rhizome", "db.sqlite")
	require.NoError(t, os.MkdirAll(filepath.Dir(dbPath), 0o755))
	store, err := sqlitefixture.Open(dbPath)
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	noteMetadata := testkit.NoteMetadataIndexer(t)
	noteFormats, err := noteMetadata.FormatRuntime()
	require.NoError(t, err)
	_, err = noteMetadata.EnsureIndexed(context.Background(), obsidian.VaultDefinition{Path: root}, &obsidian.Note{}, store)
	require.NoError(t, err)

	result, err := ontology.BuildIndexWithStore(context.Background(), noteMetadata, obsidian.VaultDefinition{Path: root}, &obsidian.Note{}, store, schema, "notes-hash")
	require.NoError(t, err)

	require.NoError(t, store.ReplaceOntologySnapshot(context.Background(), codeanchorsqlite.OntologySnapshot{
		Assessments: result.AssessmentRows,
		NoteTypes:   result.NoteTypes,
		Edges:       result.Edges,
		SchemaState: codeanchorsqlite.OntologySchemaState{
			SchemaHash: schema.Hash,
			NotesHash:  result.NotesHash,
			LoadedAt:   1,
			Ready:      true,
		},
	}))
	require.NoError(t, store.ReplaceOntologyNodeReadModel(context.Background(), readmodel.NodeReadModel{
		NotePaths:   result.NodePaths,
		Nodes:       result.Nodes,
		FieldValues: result.NodeFieldValues,
	}))

	return &queryTestEnv{
		root:         root,
		schema:       schema,
		execSchema:   execSchema,
		store:        store,
		noteMetadata: noteMetadata,
		noteFormats:  noteFormats,
	}
}

func (e *queryTestEnv) deps(searcher SemanticSearcher) Deps {
	return Deps{
		VaultDef:         obsidian.VaultDefinition{Path: e.root},
		NoteReader:       &obsidian.Note{},
		NoteFormats:      e.noteFormats,
		Store:            e.store,
		SemanticSearcher: searcher,

		ExactNoteMetadataRows: func(ctx context.Context, paths []string) (map[string]codeanchorsqlite.NoteMetadataRow, error) {
			return e.noteMetadata.UsableNoteMetadataRowsByPaths(ctx, e.store, paths)
		},
	}
}

func (e *queryTestEnv) depsWithSearch(searcher NoteSearcher) Deps {
	deps := e.deps(nil)
	deps.NoteSearcher = searcher
	return deps
}

func writeQueryTestConfig(t *testing.T, root string) {
	t.Helper()
	rhizomeDir := filepath.Join(root, ".rhizome")
	require.NoError(t, os.MkdirAll(rhizomeDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(rhizomeDir, "config.yml"), []byte("notes:\n  includes: [\"**/*.md\"]\n"), 0o644))
}

func writeQueryTestSchema(t *testing.T, root, body string) {
	t.Helper()
	dir := filepath.Join(root, ".rhizome", "ontology")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "schema.graphql"), []byte(body), 0o644))
}

func writeQueryTestNote(t *testing.T, root, rel, body string) {
	t.Helper()
	full := filepath.Join(root, rel)
	require.NoError(t, os.MkdirAll(filepath.Dir(full), 0o755))
	require.NoError(t, os.WriteFile(full, []byte(body), 0o644))
}

func titles(rows []any) []string {
	out := make([]string, 0, len(rows))
	for _, row := range rows {
		item := row.(map[string]any)
		out = append(out, item["title"].(string))
	}
	return out
}

func warningCodes(rows []any) []string {
	out := make([]string, 0, len(rows))
	for _, row := range rows {
		item := row.(map[string]any)
		if code, ok := item["code"].(string); ok {
			out = append(out, code)
		}
	}
	return out
}

func ontologyFieldByName(rows []any, name string) map[string]any {
	for _, row := range rows {
		item := row.(map[string]any)
		if item["name"] == name {
			return item
		}
	}
	return nil
}

func graphQLListContainsNestedValue(items []any, parent string, field string, expected any) bool {
	for _, item := range items {
		current, ok := item.(map[string]any)
		if !ok {
			continue
		}
		nested, ok := current[parent].(map[string]any)
		if ok && nested[field] == expected {
			return true
		}
	}
	return false
}
