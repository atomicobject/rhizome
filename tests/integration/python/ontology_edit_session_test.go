//go:build integration
// +build integration

package integration

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/atomicobject/rhizome/pkg/anchors"
	codeanchorsqlite "github.com/atomicobject/rhizome/pkg/anchors/sqlite"
	"github.com/atomicobject/rhizome/pkg/notemeta"
	"github.com/atomicobject/rhizome/pkg/ontology"
	ontologyquery "github.com/atomicobject/rhizome/pkg/ontology/query"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/atomicobject/rhizome/tests/integration/internal/fixture"
	"github.com/stretchr/testify/require"
)

func TestOntologyEditSession_ReindexesAndUpdatesGraphQLQuery(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	writeIntegrationOntologyConfig(t, root)
	writeIntegrationOntologySchema(t, root, `
type UserStory implements Section @node(locator: EMBEDDED) {
  status: String @field
}

type UserStoriesSection implements Section {
  stories: [UserStory!] @contains(level: H3)
}

type Spec @node(paths: ["notes/specs/*.md"]) {
  summary: String!
  userStories: UserStoriesSection @contains(level: H2, heading: "User Stories")
}
`)
	writeIntegrationOntologyNote(t, root, "notes/specs/spec.md", `---
type: Spec
summary: Initial summary
---

## User Stories

### Story A
status:: TODO
^story-a

### Story B
status:: IN_PROGRESS
^story-b
`)

	schema, execSchema, store := integrationOntologyEnv(t, ctx, root)
	query := `
{
  spec(path: "notes/specs/spec.md") {
    summary
    userStories {
      stories {
        title
        ... on UserStory {
          id
          notePath
          status
        }
      }
    }
  }
}
`

	before := runOntologyQuery(t, ctx, root, schema, execSchema, store, query)
	require.Equal(t, "Initial summary", persistedOntologyField(t, ctx, store, "Spec", "notes/specs/spec.md", "", "summary").ValueText)
	require.Equal(t, "TODO", persistedOntologyField(t, ctx, store, "UserStory", "notes/specs/spec.md", "^story-a", "status").ValueText)
	require.Equal(t, "IN_PROGRESS", persistedOntologyField(t, ctx, store, "UserStory", "notes/specs/spec.md", "^story-b", "status").ValueText)
	record := before["spec"].([]any)[0].(map[string]any)
	require.Equal(t, "Initial summary", record["summary"])
	stories := record["userStories"].(map[string]any)["stories"].([]any)
	require.Len(t, stories, 2)
	require.Equal(t, "TODO", stories[0].(map[string]any)["status"])
	require.Equal(t, "notes/specs/spec.md", stories[0].(map[string]any)["notePath"])

	session := ontology.NewEditSession(obsidian.VaultDefinition{Path: root}, &obsidian.Note{}, schema)
	require.NoError(t, session.SetScalarField(ontology.NodeRef{
		NotePath: "notes/specs/spec.md",
		Kind:     ontology.NodeKindNote,
	}, "summary", "Updated summary"))
	require.NoError(t, session.SetInlineField(ontology.NodeRef{
		NotePath: "notes/specs/spec.md",
		Fragment: "^story-a",
		Kind:     ontology.NodeKindEmbedded,
	}, "status", "DONE"))

	result, err := session.Commit(ctx)
	require.NoError(t, err)
	require.True(t, result.Applied)

	projected, err := ontology.ProjectNode(ctx, obsidian.VaultDefinition{Path: root}, &obsidian.Note{}, schema, ontology.NodeRef{
		NotePath: "notes/specs/spec.md",
		Fragment: "^story-a",
		Kind:     ontology.NodeKindEmbedded,
	})
	require.NoError(t, err)
	require.Equal(t, []string{"DONE"}, projected.Fields["status"].Values)

	require.NoError(t, syncIntegrationOntology(ctx, testNoteMetadataIndexer(t), root, store))
	require.Equal(t, "Updated summary", persistedOntologyField(t, ctx, store, "Spec", "notes/specs/spec.md", "", "summary").ValueText)
	require.Equal(t, "DONE", persistedOntologyField(t, ctx, store, "UserStory", "notes/specs/spec.md", "^story-a", "status").ValueText)
	require.Equal(t, "IN_PROGRESS", persistedOntologyField(t, ctx, store, "UserStory", "notes/specs/spec.md", "^story-b", "status").ValueText)

	after := runOntologyQuery(t, ctx, root, schema, execSchema, store, query)
	record = after["spec"].([]any)[0].(map[string]any)
	require.Equal(t, "Updated summary", record["summary"])
	stories = record["userStories"].(map[string]any)["stories"].([]any)
	require.Equal(t, "DONE", stories[0].(map[string]any)["status"])
	require.Equal(t, "IN_PROGRESS", stories[1].(map[string]any)["status"])

	data, err := os.ReadFile(filepath.Join(root, "notes/specs/spec.md"))
	require.NoError(t, err)
	require.Contains(t, string(data), "summary: Updated summary")
	require.Contains(t, string(data), "status:: DONE")
}

func TestPythonFixture_OntologyEditSession_TogglesItemBackedCheckboxAndReindexes(t *testing.T) {
	ctx := context.Background()
	ws := fixture.NewWorkspace(t)

	schema, execSchema, store := integrationOntologyEnv(t, ctx, ws.CodeRoot, "notes/meetings/sync.md")
	query := `
{
  conversation(path: "notes/meetings/sync.md") {
    actionItems {
      done
      assignee { path }
      ref { kind fragment typeName }
    }
  }
}
`

	before := runOntologyQuery(t, ctx, ws.CodeRoot, schema, execSchema, store, query)
	conversation := before["conversation"].([]any)[0].(map[string]any)
	actionItems := conversation["actionItems"].([]any)
	require.Len(t, actionItems, 2)
	require.Equal(t, false, actionItems[0].(map[string]any)["done"])

	firstRef := actionItems[0].(map[string]any)["ref"].(map[string]any)
	firstFragment := firstRef["fragment"].(string)
	firstDone := persistedOntologyField(t, ctx, store, "ActionItem", "notes/meetings/sync.md", firstFragment, "done")
	require.NotNil(t, firstDone.ValueBool)
	require.False(t, *firstDone.ValueBool)
	secondRef := actionItems[1].(map[string]any)["ref"].(map[string]any)
	secondFragment := secondRef["fragment"].(string)
	secondDone := persistedOntologyField(t, ctx, store, "ActionItem", "notes/meetings/sync.md", secondFragment, "done")
	require.NotNil(t, secondDone.ValueBool)
	require.True(t, *secondDone.ValueBool)
	session := ontology.NewEditSession(obsidian.VaultDefinition{Path: ws.CodeRoot}, &obsidian.Note{}, schema)
	require.NoError(t, session.SetScalarField(ontology.NodeRef{
		NotePath: "notes/meetings/sync.md",
		Fragment: firstRef["fragment"].(string),
		Kind:     ontology.NodeKindEmbedded,
	}, "done", "true"))

	result, err := session.Commit(ctx)
	require.NoError(t, err)
	require.True(t, result.Applied)

	require.NoError(t, syncIntegrationOntology(ctx, testNoteMetadataIndexer(t), ws.CodeRoot, store, "notes/meetings/sync.md"))
	firstDone = persistedOntologyField(t, ctx, store, "ActionItem", "notes/meetings/sync.md", firstFragment, "done")
	require.NotNil(t, firstDone.ValueBool)
	require.True(t, *firstDone.ValueBool)
	secondDone = persistedOntologyField(t, ctx, store, "ActionItem", "notes/meetings/sync.md", secondFragment, "done")
	require.NotNil(t, secondDone.ValueBool)
	require.True(t, *secondDone.ValueBool)

	after := runOntologyQuery(t, ctx, ws.CodeRoot, schema, execSchema, store, query)
	conversation = after["conversation"].([]any)[0].(map[string]any)
	actionItems = conversation["actionItems"].([]any)
	require.Equal(t, true, actionItems[0].(map[string]any)["done"])
	require.Equal(t, true, actionItems[1].(map[string]any)["done"])

	data, err := os.ReadFile(filepath.Join(ws.CodeRoot, "notes/meetings/sync.md"))
	require.NoError(t, err)
	text := string(data)
	require.Contains(t, text, "- [x] Update release checklist #action-item")
	require.Contains(t, text, "assignee:: [[notes/people/alice]]")
	require.Contains(t, text, "- [x] Confirm query regression #action-item ^confirmed-query-regression")
	require.Contains(t, text, "assignee:: [[notes/people/bob]]")
	require.Contains(t, text, "- [ ] Ordinary checkbox stays prose")
}

func integrationOntologyEnv(t *testing.T, ctx context.Context, root string, rels ...string) (*ontology.Schema, *ontologyquery.ExecutableSchema, *codeanchorsqlite.Store) {
	t.Helper()

	schema, err := ontology.LoadSchema(root)
	require.NoError(t, err)

	execSchema, err := ontologyquery.BuildExecutableSchema(schema)
	require.NoError(t, err)

	dbPath := filepath.Join(root, ".rhizome", "db.sqlite")
	require.NoError(t, os.MkdirAll(filepath.Dir(dbPath), 0o755))
	store, err := codeanchorsqlite.Open(dbPath)
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	noteMetadata := testNoteMetadataIndexer(t)
	_, err = noteMetadata.EnsureIndexed(ctx, obsidian.VaultDefinition{Path: root}, &obsidian.Note{}, store)
	require.NoError(t, err)
	require.NoError(t, syncIntegrationOntology(ctx, noteMetadata, root, store, rels...))
	state, err := store.GetOntologySchemaState(ctx)
	require.NoError(t, err)
	require.True(t, state.Ready)
	require.Equal(t, ontology.OntologyMaterializationVersion, state.MaterializationVersion)

	return schema, execSchema, store
}

func syncIntegrationOntology(ctx context.Context, noteMetadata notemeta.Indexer, root string, store *codeanchorsqlite.Store, rels ...string) error {
	if len(rels) == 0 {
		rels = []string{"notes/specs/spec.md"}
	}
	if err := noteMetadata.SyncPaths(ctx, obsidian.VaultDefinition{Path: root}, &obsidian.Note{}, store, rels, nil); err != nil {
		return err
	}
	_, err := ontology.SyncPaths(ctx, noteMetadata, obsidian.VaultDefinition{Path: root}, &obsidian.Note{}, store, nil, rels, nil)
	return err
}

func persistedOntologyField(t *testing.T, ctx context.Context, store *codeanchorsqlite.Store, typeName, path, fragment, field string) codeanchor.IntelOntologyNodeFieldValue {
	t.Helper()
	nodes, err := store.OntologyNodesByType(ctx, typeName)
	require.NoError(t, err)
	var ids []string
	for _, node := range nodes {
		if node.NotePath == path && node.Fragment == fragment {
			ids = append(ids, node.NodeID)
		}
	}
	require.Len(t, ids, 1, "expected one %s at %s#%s", typeName, path, fragment)
	fields, err := store.OntologyNodeFieldValuesByNodeIDs(ctx, ids, []string{field})
	require.NoError(t, err)
	require.Len(t, fields, 1, "expected one %s field on %s#%s", field, path, fragment)
	return fields[0]
}

func runOntologyQuery(t *testing.T, ctx context.Context, root string, schema *ontology.Schema, execSchema *ontologyquery.ExecutableSchema, store *codeanchorsqlite.Store, raw string) map[string]any {
	t.Helper()

	prepared, errs := ontologyquery.Prepare(execSchema, raw)
	require.Empty(t, errs)

	result := ontologyquery.Execute(ctx, ontologyquery.Deps{
		VaultDef:   obsidian.VaultDefinition{Path: root},
		NoteReader: &obsidian.Note{},
		Store:      store,
		Service:    ontology.NewService(obsidian.VaultDefinition{Path: root}, &obsidian.Note{}, store, schema),
	}, schema, prepared)
	require.Empty(t, result.Errors)
	return result.Data
}

func writeIntegrationOntologyConfig(t *testing.T, root string) {
	t.Helper()
	rhizomeDir := filepath.Join(root, ".rhizome")
	require.NoError(t, os.MkdirAll(rhizomeDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(rhizomeDir, "config.yml"), []byte("notes:\n  includes: [\"**/*.md\"]\n"), 0o644))
}

func writeIntegrationOntologySchema(t *testing.T, root, body string) {
	t.Helper()
	dir := filepath.Join(root, ".rhizome", "ontology")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "schema.graphql"), []byte(body), 0o644))
}

func writeIntegrationOntologyNote(t *testing.T, root, rel, body string) {
	t.Helper()
	full := filepath.Join(root, rel)
	require.NoError(t, os.MkdirAll(filepath.Dir(full), 0o755))
	require.NoError(t, os.WriteFile(full, []byte(body), 0o644))
}
