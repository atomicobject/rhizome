package query

import (
	"context"
	"encoding/json"
	"github.com/atomicobject/rhizome/pkg/diag"
	"github.com/atomicobject/rhizome/pkg/search"
	"github.com/atomicobject/rhizome/pkg/search/semantic"
	codeanchorsqlite "github.com/atomicobject/rhizome/pkg/store"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestExecute_SemanticScoreDoesNotLeakToSiblingRoot(t *testing.T) {
	env := newQueryTestEnv(t)
	prepared, errs := Prepare(env.execSchema, `
{
  semantic: project(semantic: "roadmap") {
    name
    score
  }
  exact: project(find: "Roadmap") {
    name
    score
  }
}
`)
	require.Empty(t, errs)

	result := Execute(context.Background(), env.deps(fakeSemanticSearcher{
		surveyResults: []semantic.Result{{SourceRef: search.SourceRef{NoteID: "notes/projects/roadmap-refresh.md"}, Score: 0.91}},
	}), env.schema, prepared)
	require.Empty(t, result.Errors)

	semanticRows := result.Data["semantic"].([]any)
	require.Len(t, semanticRows, 1)
	require.Equal(t, "Roadmap Refresh", semanticRows[0].(map[string]any)["name"])
	require.InDelta(t, 0.91, semanticRows[0].(map[string]any)["score"], 0.001)

	exactRows := result.Data["exact"].([]any)
	require.Len(t, exactRows, 1)
	require.Equal(t, "Roadmap Refresh", exactRows[0].(map[string]any)["name"])
	require.Nil(t, exactRows[0].(map[string]any)["score"])
}

func TestExecute_PublicConnectionBatchSearchAndValidationRoots(t *testing.T) {
	env := newQueryTestEnv(t)
	publishQueryValidationSnapshot(t, env.store, codeanchorsqlite.ValidationSnapshot{
		SelectedChecks: []string{"broken_links"}, IssueCount: 1, AffectedFileCount: 1, AffectedNoteCount: 1,
		Checks: []codeanchorsqlite.ValidationCheckSnapshot{{Check: "broken_links", Outcome: codeanchorsqlite.ValidationCheckOutcomeCompleted, IssueCount: 1, Summary: "one broken link"}},
		Diagnostics: []codeanchorsqlite.ValidationDiagnostic{{
			IssueKey: "broken:1", Check: "broken_links", Code: "broken_link", Message: "Missing target",
			Evidence: json.RawMessage(`{"target":"missing"}`), PrimaryPath: "notes/projects/roadmap-refresh.md",
			AffectedPaths: []string{"notes/projects/roadmap-refresh.md"}, AffectedNotePaths: []string{"notes/projects/roadmap-refresh.md"},
			Location: &codeanchorsqlite.ValidationDiagnosticLocation{Unit: codeanchorsqlite.ValidationLocationUnitLine, Start: 12, End: 12},
		}},
	})

	prepared, errs := Prepare(env.execSchema, `
{
  batch: nodes(refs: [
    "notes/projects/roadmap-refresh.md",
    "notes/missing.md"
  ]) {
    pageInfo { queryShapeVersion requestedFirst returnedCount truncated maxFirst }
    items {
      requestedRef
      ref { kind notePath }
      node { nodeKind title }
      error { code message }
    }
  }
  page: notes(type: "Project", first: 1) {
    pageInfo { requestedFirst returnedCount truncated maxFirst }
    nodes { title }
    warnings { code }
  }
  search(query: "roadmap", type: "Project", first: 5) {
    query
    pageInfo { returnedCount truncated }
    nodes { title score }
    warnings { code message }
  }
  validation(firstIssues: 1) {
    ok
    issueCount
    selectedChecks
    checks {
      name
      ok
      issueCount
      issues { code path message line data }
    }
  }
}
`)
	require.Empty(t, errs)

	result := Execute(context.Background(), env.depsWithSearch(&fakeNoteSearcher{
		hits: []NoteSearchHit{{Path: "notes/projects/roadmap-refresh.md", Score: 0.91}},
	}), env.schema, prepared)
	require.Empty(t, result.Errors)

	batch := result.Data["batch"].(map[string]any)
	require.Equal(t, "v1", batch["pageInfo"].(map[string]any)["queryShapeVersion"])
	require.Equal(t, 2, batch["pageInfo"].(map[string]any)["returnedCount"])
	batchItems := batch["items"].([]any)
	require.Equal(t, "NOTE", batchItems[0].(map[string]any)["node"].(map[string]any)["nodeKind"])
	require.Nil(t, batchItems[0].(map[string]any)["error"])
	require.Equal(t, "unresolved_ref", batchItems[1].(map[string]any)["error"].(map[string]any)["code"])

	page := result.Data["page"].(map[string]any)
	require.Equal(t, 1, page["pageInfo"].(map[string]any)["requestedFirst"])
	require.Equal(t, 1, page["pageInfo"].(map[string]any)["returnedCount"])
	require.Equal(t, false, page["pageInfo"].(map[string]any)["truncated"])
	require.Empty(t, page["warnings"].([]any))

	search := result.Data["search"].(map[string]any)
	require.Equal(t, []any{"roadmap"}, search["query"])
	require.Equal(t, 1, search["pageInfo"].(map[string]any)["returnedCount"])
	require.Equal(t, false, search["pageInfo"].(map[string]any)["truncated"])
	require.Empty(t, search["warnings"].([]any))
	require.InDelta(t, 0.91, search["nodes"].([]any)[0].(map[string]any)["score"], 0.001)

	validation := result.Data["validation"].(map[string]any)
	require.Equal(t, false, validation["ok"])
	require.Equal(t, 1, validation["issueCount"])
	require.Equal(t, []any{"broken_links"}, validation["selectedChecks"])
	check := validation["checks"].([]any)[0].(map[string]any)
	require.Equal(t, "broken_links", check["name"])
	issue := check["issues"].([]any)[0].(map[string]any)
	require.Equal(t, "broken_link", issue["code"])
	require.Equal(t, "missing", issue["data"].(map[string]any)["target"])
}

func TestExecute_SearchReportsUnavailableSearcher(t *testing.T) {
	env := newQueryTestEnv(t)
	prepared, errs := Prepare(env.execSchema, `
{
  search(query: "roadmap") {
    nodes { title }
    warnings { code message }
    pageInfo { returnedCount }
  }
}
`)
	require.Empty(t, errs)

	result := Execute(context.Background(), env.deps(nil), env.schema, prepared)
	require.Empty(t, result.Errors)
	search := result.Data["search"].(map[string]any)
	require.Empty(t, search["nodes"].([]any))
	warnings := search["warnings"].([]any)
	require.Len(t, warnings, 1)
	require.Equal(t, "search_unavailable", warnings[0].(map[string]any)["code"])
}

func TestExecute_SearchUsesNoteSearcherAndTypeFilter(t *testing.T) {
	env := newQueryTestEnv(t)
	prepared, errs := Prepare(env.execSchema, `
{
  search(query: ["roadmap", "refresh"], type: "Project", first: 5) {
    query
    nodes { title score }
    warnings { code message }
  }
}
`)
	require.Empty(t, errs)

	searcher := &fakeNoteSearcher{
		hits: []NoteSearchHit{
			{Path: "notes/projects/roadmap-refresh.md", Score: 0.91},
			{Path: "notes/decisions/adopt-rhizome.md", Score: 0.42},
		},
		warnings: []diag.Warning{{Code: "vector_unavailable", Message: "embeddings are unavailable"}},
	}
	result := Execute(context.Background(), env.depsWithSearch(searcher), env.schema, prepared)
	require.Empty(t, result.Errors)

	require.Len(t, searcher.requests, 1)
	require.Equal(t, []string{"roadmap", "refresh"}, searcher.requests[0].Queries)
	require.Equal(t, []string{"Project"}, searcher.requests[0].NoteTypes)
	require.Equal(t, 6, searcher.requests[0].First)

	search := result.Data["search"].(map[string]any)
	require.Equal(t, []any{"roadmap", "refresh"}, search["query"])
	nodes := search["nodes"].([]any)
	require.Len(t, nodes, 1)
	require.Equal(t, "Roadmap Refresh", nodes[0].(map[string]any)["title"])
	require.Equal(t, 0.91, nodes[0].(map[string]any)["score"])
	warnings := search["warnings"].([]any)
	require.Len(t, warnings, 1)
	require.Equal(t, "vector_unavailable", warnings[0].(map[string]any)["code"])
}

type fakeNoteSearcher struct {
	hits     []NoteSearchHit
	warnings []diag.Warning
	requests []NoteSearchRequest
}

func (f *fakeNoteSearcher) SearchNotes(_ context.Context, request NoteSearchRequest) (NoteSearchResponse, error) {
	f.requests = append(f.requests, request)
	return NoteSearchResponse{Hits: f.hits, Warnings: f.warnings}, nil
}

type fakeSemanticSearcher struct {
	results       []semantic.Result
	surveyResults []semantic.Result
}

func (f fakeSemanticSearcher) Search(context.Context, semantic.SearchRequest) ([]semantic.Result, error) {
	return f.results, nil
}

func (f fakeSemanticSearcher) SurveyNodesByType(context.Context, semantic.SurveyNodesByTypeRequest) ([]semantic.Result, error) {
	if f.surveyResults != nil {
		return f.surveyResults, nil
	}
	return f.results, nil
}
