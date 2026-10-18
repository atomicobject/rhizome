//go:build integration
// +build integration

package integration

import (
	"context"
	"github.com/atomicobject/rhizome/internal/testkit"
	ontologytest "github.com/atomicobject/rhizome/internal/testkit/ontology"
	appviews "github.com/atomicobject/rhizome/pkg/app/views"
	"github.com/atomicobject/rhizome/pkg/notemeta"
	"github.com/atomicobject/rhizome/pkg/ontology"
	ontologyquery "github.com/atomicobject/rhizome/pkg/ontology/query"
	ontoschema "github.com/atomicobject/rhizome/pkg/ontology/schema"
	"github.com/atomicobject/rhizome/pkg/ontology/viewconfig"
	codeanchorsqlite "github.com/atomicobject/rhizome/pkg/store"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/atomicobject/rhizome/tests/integration/internal/fixture"
	"github.com/stretchr/testify/require"
	"os"
	"path/filepath"
	"testing"
)

func TestPythonFixture_OntologyQuery(t *testing.T) {
	ctx := context.Background()
	ws := fixture.NewWorkspace(t)

	schema, store := indexedOntologyFixture(t, ctx, ws)
	defer func() { _ = store.Close() }()

	execSchema, err := ontologyquery.BuildExecutableSchema(schema)
	require.NoError(t, err)
	prepared, errs := ontologyquery.Prepare(execSchema, `
{
  project(path: "notes/projects/roadmap-refresh.md") {
    name
    decisions { title }
    linked(type: "Decision") { title }
    backlinked(type: "Decision") { title }
  }
}
`)
	require.Empty(t, errs)

	queryResult := ontologyquery.Execute(ctx, ontologyQueryFixtureDeps(t, ws.CodeRoot, store), schema, prepared)
	require.Empty(t, queryResult.Errors)

	rows := queryResult.Data["project"].([]any)
	require.Len(t, rows, 1)
	note := rows[0].(map[string]any)
	require.Equal(t, "Roadmap Refresh", note["name"])
	require.Len(t, note["decisions"].([]any), 2)
	require.Len(t, note["linked"].([]any), 1)
	require.Len(t, note["backlinked"].([]any), 1)
}

func TestPythonFixture_OntologyQuery_AuthoredNoteFieldsOnNoteNode(t *testing.T) {
	ctx := context.Background()
	ws := fixture.NewWorkspace(t)

	schema, store := indexedOntologyFixture(t, ctx, ws)
	defer func() { _ = store.Close() }()

	execSchema, err := ontologyquery.BuildExecutableSchema(schema)
	require.NoError(t, err)
	require.Contains(t, execSchema.SDL, "interface NoteNode implements Node & Note")

	prepared, errs := ontologyquery.Prepare(execSchema, `
{
  note(path: "notes/product-brief.md") {
    path
    title
    summary
  }
  project(path: "notes/projects/roadmap-refresh.md") {
    path
    name
    summary
  }
}
`)
	require.Empty(t, errs)

	queryResult := ontologyquery.Execute(ctx, ontologyQueryFixtureDeps(t, ws.CodeRoot, store), schema, prepared)
	require.Empty(t, queryResult.Errors)

	general := queryResult.Data["note"].(map[string]any)
	require.Equal(t, "notes/product-brief.md", general["path"])
	require.Equal(t, "Product Brief", general["title"])
	require.Equal(t, "Brief for the polyglot TODO app fixture (used by coderefs tests).", general["summary"])

	projects := queryResult.Data["project"].([]any)
	require.Len(t, projects, 1)
	project := projects[0].(map[string]any)
	require.Equal(t, "Roadmap Refresh", project["name"])
	require.Equal(t, "Project fixture proving authored Note fields are queryable through typed note roots.", project["summary"])
}

func TestPythonFixture_OntologyQuery_Sections(t *testing.T) {
	ctx := context.Background()
	ws := fixture.NewWorkspace(t)

	schema, store := indexedOntologyFixture(t, ctx, ws)
	defer func() { _ = store.Close() }()

	execSchema, err := ontologyquery.BuildExecutableSchema(schema)
	require.NoError(t, err)
	prepared, errs := ontologyquery.Prepare(execSchema, `
{
  spec(path: "notes/specs/search-rewrite.md") {
    name
    requirements {
      title
      level
      decisions { name }
      details {
        title
        content
      }
    }
  }
}
`)
	require.Empty(t, errs)

	queryResult := ontologyquery.Execute(ctx, ontologyQueryFixtureDeps(t, ws.CodeRoot, store), schema, prepared)
	require.Empty(t, queryResult.Errors)

	rows := queryResult.Data["spec"].([]any)
	require.Len(t, rows, 1)
	spec := rows[0].(map[string]any)
	require.Equal(t, "Search Rewrite", spec["name"])

	requirements := spec["requirements"].(map[string]any)
	require.Equal(t, "Requirements", requirements["title"])
	require.Equal(t, "H2", requirements["level"])
	require.ElementsMatch(t, []string{"Ambient Linked Decision", "Structured Ontology Decision"}, noteFieldValues(requirements["decisions"].([]any), "name"))

	details := requirements["details"].(map[string]any)
	require.Equal(t, "Details", details["title"])
	require.Contains(t, details["content"], "Coordinate the rollout")
}

func TestPythonFixture_OntologyQuery_ItemBackedActionItems(t *testing.T) {
	ctx := context.Background()
	ws := fixture.NewWorkspace(t)

	schema, store := indexedOntologyFixture(t, ctx, ws)
	defer func() { _ = store.Close() }()

	execSchema, err := ontologyquery.BuildExecutableSchema(schema)
	require.NoError(t, err)
	prepared, errs := ontologyquery.Prepare(execSchema, `
{
  conversation(path: "notes/meetings/sync.md") {
    title
    actionItems {
      title
      content
      done
      due
      assignee { path title name }
      notePath
      ref { kind fragment typeName }
    }
  }
}
`)
	require.Empty(t, errs)

	queryResult := ontologyquery.Execute(ctx, ontologyQueryFixtureDeps(t, ws.CodeRoot, store), schema, prepared)
	require.Empty(t, queryResult.Errors)

	rows := queryResult.Data["conversation"].([]any)
	require.Len(t, rows, 1)
	conversation := rows[0].(map[string]any)
	require.Equal(t, "Fixture Sync", conversation["title"])

	actionItems := conversation["actionItems"].([]any)
	require.Len(t, actionItems, 2)

	first := actionItems[0].(map[string]any)
	require.Equal(t, false, first["done"])
	require.Equal(t, "2026-05-10", first["due"])
	firstAssignee := first["assignee"].(map[string]any)
	require.Equal(t, "notes/people/alice.md", firstAssignee["path"])
	require.Equal(t, "notes/meetings/sync.md", first["notePath"])
	require.Contains(t, first["content"], "Update release checklist")
	firstRef := first["ref"].(map[string]any)
	require.Equal(t, "EMBEDDED", firstRef["kind"])
	require.Equal(t, "ActionItem", firstRef["typeName"])
	require.Contains(t, firstRef["fragment"], "item-")

	second := actionItems[1].(map[string]any)
	require.Equal(t, true, second["done"])
	require.Equal(t, "2026-05-01", second["due"])
	secondAssignee := second["assignee"].(map[string]any)
	require.Equal(t, "notes/people/bob.md", secondAssignee["path"])
	secondRef := second["ref"].(map[string]any)
	require.Equal(t, "^confirmed-query-regression", secondRef["fragment"])
}

func TestPythonFixture_OntologyQuery_DirectActionItemsFilterSortAndSourceContext(t *testing.T) {
	ctx := context.Background()
	ws := fixture.NewWorkspace(t)

	schema, store := indexedOntologyFixture(t, ctx, ws)
	defer func() { _ = store.Close() }()

	execSchema, err := ontologyquery.BuildExecutableSchema(schema)
	require.NoError(t, err)
	prepared, errs := ontologyquery.Prepare(execSchema, `
{
  actionItem(
    first: 10
    filters: [
      { field: "assignee", op: eq, value: "notes/people/alice" }
      { field: "done", op: eq, value: "false" }
    ]
    sort: [
      { field: "due", direction: asc }
      { field: "notePath", direction: asc }
    ]
  ) {
    title
    done
    due
    notePath
    assignee { path title }
    locator { sourceLocator markdown }
    ref { ref typeName }
  }
}
`)
	require.Empty(t, errs)

	queryResult := ontologyquery.Execute(ctx, ontologyQueryFixtureDeps(t, ws.CodeRoot, store), schema, prepared)
	require.Empty(t, queryResult.Errors)

	rows := queryResult.Data["actionItem"].([]any)
	require.Len(t, rows, 2)
	first := rows[0].(map[string]any)
	require.Equal(t, "Review search rollout notes", first["title"])
	require.Equal(t, "2026-05-08", first["due"])
	require.Equal(t, "notes/specs/search-rewrite.md", first["notePath"])
	require.Contains(t, first["locator"].(map[string]any)["sourceLocator"], "notes/specs/search-rewrite.md#")
	second := rows[1].(map[string]any)
	require.Equal(t, "Update release checklist", second["title"])
	require.Equal(t, "2026-05-10", second["due"])

	sourcePrepared, sourceErrs := ontologyquery.Prepare(execSchema, `
{
  actionItem(first: 10, filters: [{ field: "notePath", op: eq, value: "notes/specs/search-rewrite.md" }]) {
    title
    notePath
  }
}
`)
	require.Empty(t, sourceErrs)
	sourceResult := ontologyquery.Execute(ctx, ontologyQueryFixtureDeps(t, ws.CodeRoot, store), schema, sourcePrepared)
	require.Empty(t, sourceResult.Errors)
	sourceRows := sourceResult.Data["actionItem"].([]any)
	require.Len(t, sourceRows, 1)
	require.Equal(t, "Review search rollout notes", sourceRows[0].(map[string]any)["title"])
}

func TestPythonFixture_ActionItemsConfiguredViewDefaultsAndMinePreset(t *testing.T) {
	ctx := context.Background()
	ws := fixture.NewWorkspace(t)

	schema, store := indexedOntologyFixture(t, ctx, ws)
	defer func() { _ = store.Close() }()
	execSchema, err := ontologyquery.BuildExecutableSchema(schema)
	require.NoError(t, err)
	defs, issues := viewconfig.LoadDefaultSource(ws.CodeRoot)
	require.Empty(t, issues)

	queryDeps := ontologyQueryFixtureDeps(t, ws.CodeRoot, store)
	queryDeps.OntologyRuntime = fixtureCurrentUser{ref: "notes/people/alice"}

	service := appviews.New(appviews.ServiceOptions{
		VaultPath:  ws.CodeRoot,
		VaultDef:   obsidian.VaultDefinition{Path: ws.CodeRoot},
		NoteReader: &obsidian.Note{},
		Store:      store,
		Schema:     schema,
		ExecSchema: execSchema,
		QueryDeps:  queryDeps,
		Views:      defs,
	})

	allOpen, err := service.Execute(ctx, "action-items", appviews.ExecuteRequest{})
	require.NoError(t, err)
	require.Empty(t, allOpen.Warnings)
	require.True(t, allOpen.ConstraintPlan)
	require.ElementsMatch(t, []viewconfig.FilterSpec{{Field: "done", Op: "eq", Value: "false"}}, allOpen.PushedConstraints.Filters)
	require.Equal(t, []viewconfig.SortSpec{{Field: "due", Direction: "asc"}, {Field: "notePath", Direction: "asc"}}, allOpen.PushedConstraints.Sort)
	require.Len(t, allOpen.Rows, 3)
	require.Equal(t, "Review search rollout notes", allOpen.Rows[0].Title)
	require.Equal(t, "Update release checklist", allOpen.Rows[1].Title)
	require.Equal(t, "Draft launch framing", allOpen.Rows[2].Title)

	mine, err := service.Execute(ctx, "action-items", appviews.ExecuteRequest{FilterPreset: "mine"})
	require.NoError(t, err)
	require.Empty(t, mine.Warnings)
	require.ElementsMatch(t, []viewconfig.FilterSpec{
		{Field: "done", Op: "eq", Value: "false"},
		{Field: "assignee", Op: "eq", ValueFrom: "ontology.currentUser.resolvedRef"},
	}, mine.State.Filters, "execution state retains the authored current-user binding")
	require.ElementsMatch(t, []viewconfig.FilterSpec{
		{Field: "done", Op: "eq", Value: "false"},
		{Field: "assignee", Op: "eq", Value: "notes/people/alice"},
	}, mine.PushedConstraints.Filters, "pushed constraints report the bound literal predicate")
	require.Equal(t, []viewconfig.SortSpec{{Field: "due", Direction: "asc"}, {Field: "notePath", Direction: "asc"}}, mine.PushedConstraints.Sort)
	require.Len(t, mine.Rows, 2)
	require.Equal(t, "Review search rollout notes", mine.Rows[0].Title)
	require.Equal(t, "Update release checklist", mine.Rows[1].Title)
}

func TestPythonFixture_OntologyBuild_ReportsInvalidActionItemDue(t *testing.T) {
	ctx := context.Background()
	ws := fixture.NewWorkspace(t)

	notePath := filepath.Join(ws.CodeRoot, "notes", "bad-action-item.md")
	require.NoError(t, os.WriteFile(notePath, []byte(`# Bad action item

- [ ] Broken date #action-item
  assignee:: [[notes/people/alice]]
  due:: tomorrow
`), 0o644))

	schema, err := ontoschema.LoadSchema(ws.CodeRoot)
	require.NoError(t, err)
	result, err := buildFixtureOntologyFromSources(ctx, testkit.NoteMetadataIndexer(t), ws.CodeRoot, schema)
	require.NoError(t, err)

	var found bool
	for _, issue := range result.ValidationIssues {
		if issue.Code == "field_type_mismatch" && issue.NotePath == "notes/bad-action-item.md" && issue.FieldName == "due" {
			found = true
			break
		}
	}
	require.True(t, found, "expected invalid due date issue, got %#v", result.ValidationIssues)
}

func TestPythonFixture_OntologyBuild_ReportsNestedSectionValidationIssues(t *testing.T) {
	ctx := context.Background()
	ws := fixture.NewWorkspace(t)

	specPath := filepath.Join(ws.CodeRoot, "notes", "specs", "search-rewrite.md")
	require.NoError(t, os.WriteFile(specPath, []byte(`---
type: Spec
name: Search Rewrite
---

## Requirements

Keep ontology-backed query results stable while the search pipeline changes.
`), 0o644))

	schema, err := ontoschema.LoadSchema(ws.CodeRoot)
	require.NoError(t, err)

	result, err := buildFixtureOntologyFromSources(ctx, testkit.NoteMetadataIndexer(t), ws.CodeRoot, schema)
	require.NoError(t, err)
	require.NotEmpty(t, result.ValidationIssues)

	var found bool
	for _, issue := range result.ValidationIssues {
		if issue.Code == "missing_required_section" && issue.NotePath == "notes/specs/search-rewrite.md" && issue.FieldName == "requirements.details" {
			found = true
			break
		}
	}
	require.True(t, found, "expected nested section validation issue for requirements.details, got %#v", result.ValidationIssues)
}

func ontologyQueryFixtureDeps(t *testing.T, root string, store *codeanchorsqlite.Store) ontologyquery.Deps {
	t.Helper()
	formats, err := testkit.NoteMetadataIndexer(t).FormatRuntime()
	require.NoError(t, err)
	return ontologyquery.Deps{
		VaultDef: obsidian.VaultDefinition{Path: root}, NoteReader: &obsidian.Note{},
		Store: store, NoteFormats: formats,
	}
}

func indexedOntologyFixture(t *testing.T, ctx context.Context, ws *fixture.Workspace) (*ontoschema.Schema, *codeanchorsqlite.Store) {
	t.Helper()
	dbPath := filepath.Join(ws.CodeRoot, ".rhizome", "db.sqlite")
	require.NoError(t, os.MkdirAll(filepath.Dir(dbPath), 0o755))
	store, err := codeanchorsqlite.Open(dbPath)
	require.NoError(t, err)
	runtime, err := ontologytest.Publish(ctx, testkit.NoteMetadataIndexer(t), obsidian.VaultDefinition{Path: ws.CodeRoot}, &obsidian.Note{}, store)
	require.NoError(t, err)
	require.NotNil(t, runtime)
	require.NotNil(t, runtime.Schema)
	require.Empty(t, runtime.Issues)
	return runtime.Schema, store
}

func buildFixtureOntologyFromSources(ctx context.Context, noteMetadata notemeta.Indexer, root string, schema *ontoschema.Schema) (*ontology.BuildResult, error) {
	vaultDef := obsidian.VaultDefinition{Path: root}
	sources, err := noteMetadata.BuildNoteSourceSnapshots(ctx, vaultDef, &obsidian.Note{})
	if err != nil {
		return nil, err
	}
	return ontology.BuildIndexFromNoteSources(ctx, vaultDef, sources, schema, "fixture-notes")
}

func noteFieldValues(rows []any, field string) []string {
	out := make([]string, 0, len(rows))
	for _, row := range rows {
		record, ok := row.(map[string]any)
		if !ok {
			continue
		}
		value, _ := record[field].(string)
		out = append(out, value)
	}
	return out
}

type fixtureCurrentUser struct {
	ref string
}

func (f fixtureCurrentUser) AuthoringGuide(context.Context, ontologyquery.OntologyAuthoringGuideRequest) (ontologyquery.OntologyAuthoringGuide, error) {
	return ontologyquery.OntologyAuthoringGuide{}, nil
}

func (f fixtureCurrentUser) NextID(context.Context, ontologyquery.OntologyNextIDRequest) (ontologyquery.OntologyNextID, error) {
	return ontologyquery.OntologyNextID{}, nil
}

func (f fixtureCurrentUser) CurrentUser(context.Context) (ontologyquery.OntologyCurrentUser, error) {
	return ontologyquery.OntologyCurrentUser{
		Configured:  true,
		Found:       true,
		Ref:         f.ref,
		ResolvedRef: f.ref,
		Path:        f.ref,
		Title:       "Alice",
		TypeName:    "Person",
	}, nil
}
