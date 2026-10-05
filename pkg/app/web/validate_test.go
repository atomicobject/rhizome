package web

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	semdb "github.com/atomicobject/rhizome/pkg/anchors/sqlite"
	"github.com/atomicobject/rhizome/pkg/validate"
	"github.com/stretchr/testify/require"
)

func TestHandlePublicValidate_MethodNotAllowedReturnsJSONError(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/api/v2/validate", nil)
	rec := httptest.NewRecorder()

	(&Server{}).handlePublicValidate(rec, req)

	require.Equal(t, http.StatusMethodNotAllowed, rec.Code)
	require.Contains(t, rec.Header().Get("Content-Type"), "application/json")
	var body ErrorResponse
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&body))
	require.Equal(t, PublicErrorMethodNotAllowed, body.Code)
	require.Equal(t, "method not allowed", body.Error)
}

func TestReadCachedValidationEnvelopePreservesPublishedSnapshotWhileRunning(t *testing.T) {
	ctx := context.Background()
	store, err := semdb.Open(filepath.Join(t.TempDir(), "validation-running.db"))
	require.NoError(t, err)
	defer func() { _ = store.Close() }()

	generation, err := store.SetValidationRunning(ctx)
	require.NoError(t, err)
	published, err := store.PublishValidationSnapshot(ctx, webValidationSnapshot(generation, 2, 0))
	require.NoError(t, err)
	require.True(t, published)
	newGeneration, err := store.SetValidationRunning(ctx)
	require.NoError(t, err)

	result := (&Server{runtime: &Runtime{IntelStore: store}}).readCachedValidationEnvelope(ctx)
	require.Equal(t, semdb.ValidationStatusRunning, result.Status)
	require.Equal(t, ValidationHealthRunning, result.Health)
	require.Equal(t, newGeneration, result.Generation)
	require.Equal(t, generation, result.PublishedGeneration)
	require.NotNil(t, result.Snapshot)
	require.Equal(t, 2, result.Snapshot.IssueCount)
}

func TestValidationEnvelopeReportsRetainedSnapshotStale(t *testing.T) {
	ctx := context.Background()
	store, err := semdb.Open(filepath.Join(t.TempDir(), "validation-invalidated.db"))
	require.NoError(t, err)
	defer func() { _ = store.Close() }()
	generation, err := store.SetValidationRunning(ctx)
	require.NoError(t, err)
	published, err := store.PublishValidationSnapshot(ctx, webValidationSnapshot(generation, 0, 0))
	require.NoError(t, err)
	require.True(t, published)
	srv := &Server{runtime: &Runtime{IntelStore: store}}

	_, err = store.MarkPublishedValidationStale(ctx, "validation inputs changed")
	require.NoError(t, err)
	envelope := srv.readCachedValidationEnvelope(ctx)
	require.Equal(t, ValidationHealthStale, envelope.Health)
	require.NotNil(t, envelope.Snapshot)
	require.Equal(t, "validation inputs changed", envelope.Snapshot.StaleReason)
}

func TestHandlePublicValidateSerializesSnapshotStateWithoutLegacyResult(t *testing.T) {
	type envelope struct {
		Status              string                    `json:"status"`
		Health              string                    `json:"health"`
		Generation          int64                     `json:"generation"`
		PublishedGeneration int64                     `json:"publishedGeneration"`
		Error               string                    `json:"error"`
		Snapshot            *semdb.ValidationSnapshot `json:"snapshot"`
	}
	get := func(t *testing.T, srv *Server) envelope {
		t.Helper()
		rec := httptest.NewRecorder()
		srv.handlePublicValidate(rec, httptest.NewRequest(http.MethodGet, "/api/v2/validate", nil))
		require.Equal(t, http.StatusOK, rec.Code)
		require.Contains(t, rec.Header().Get("Content-Type"), "application/json")
		// Typed decoding ignores unknown keys, so check the retired field raw.
		var raw map[string]json.RawMessage
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &raw))
		require.NotContains(t, raw, "result")
		var body envelope
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
		return body
	}

	t.Run("never ran without runtime", func(t *testing.T) {
		body := get(t, &Server{})
		require.Equal(t, semdb.ValidationStatusNeverRan, body.Status)
		require.Equal(t, ValidationHealthNeverChecked, body.Health)
		require.Nil(t, body.Snapshot)
		require.Empty(t, body.Error)
	})

	t.Run("published snapshot", func(t *testing.T) {
		ctx := context.Background()
		store, err := semdb.Open(filepath.Join(t.TempDir(), "validation-handler.db"))
		require.NoError(t, err)
		defer func() { _ = store.Close() }()
		generation, err := store.SetValidationRunning(ctx)
		require.NoError(t, err)
		published, err := store.PublishValidationSnapshot(ctx, webValidationSnapshot(generation, 1, 0))
		require.NoError(t, err)
		require.True(t, published)

		body := get(t, &Server{runtime: &Runtime{IntelStore: store}})
		require.Equal(t, semdb.ValidationStatusOK, body.Status)
		require.Equal(t, ValidationHealthCurrentIssues, body.Health)
		require.Equal(t, generation, body.Generation)
		require.Equal(t, generation, body.PublishedGeneration)
		require.NotNil(t, body.Snapshot)
		require.Equal(t, 1, body.Snapshot.IssueCount)
	})
}

func TestHandlePublicValidationDiagnosticsReturnsGeneratedPageContract(t *testing.T) {
	ctx := context.Background()
	store, err := semdb.Open(filepath.Join(t.TempDir(), "validation-page-handler.db"))
	require.NoError(t, err)
	defer func() { _ = store.Close() }()
	generation, err := store.SetValidationRunning(ctx)
	require.NoError(t, err)
	snapshot := webValidationSnapshot(generation, 2, 0)
	published, err := store.PublishValidationSnapshot(ctx, snapshot)
	require.NoError(t, err)
	require.True(t, published)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/validation/diagnostics?generation="+strconv.FormatInt(generation, 10)+"&limit=1", nil)
	rec := httptest.NewRecorder()
	(&Server{runtime: &Runtime{IntelStore: store}}).handlePublicValidationDiagnostics(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	var page semdb.ValidationDiagnosticPage
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&page))
	require.Equal(t, generation, page.Generation)
	require.Equal(t, 1, page.Returned)
	require.Equal(t, 2, page.Total)
	require.NotEmpty(t, page.FilterIdentity)
	require.NotEmpty(t, page.NextCursor)

	req = httptest.NewRequest(http.MethodGet, "/api/v1/validation/diagnostics?generation="+strconv.FormatInt(generation, 10)+"&sort=file&repairAvailability=inapplicable", nil)
	rec = httptest.NewRecorder()
	(&Server{runtime: &Runtime{IntelStore: store}}).handlePublicValidationDiagnostics(rec, req)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var filePage semdb.ValidationDiagnosticPage
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&filePage))
	require.Equal(t, semdb.ValidationDiagnosticSortFile, filePage.Sort)
	require.Equal(t, 2, filePage.Total)
	require.Len(t, filePage.FileTotals, 2)
}

func TestHandlePublicValidationDiagnosticsReturnsTypedRequestErrors(t *testing.T) {
	ctx := context.Background()
	store, err := semdb.Open(filepath.Join(t.TempDir(), "validation-page-errors.db"))
	require.NoError(t, err)
	defer func() { _ = store.Close() }()
	generation, err := store.SetValidationRunning(ctx)
	require.NoError(t, err)
	published, err := store.PublishValidationSnapshot(ctx, webValidationSnapshot(generation, 1, 0))
	require.NoError(t, err)
	require.True(t, published)
	srv := &Server{runtime: &Runtime{IntelStore: store}}

	tests := []struct {
		query  string
		status int
		code   string
	}{
		{"?generation=" + strconv.FormatInt(generation, 10) + "&limit=201", http.StatusBadRequest, PublicErrorValidationPageLimit},
		{"?generation=" + strconv.FormatInt(generation, 10) + "&cursor=garbage", http.StatusBadRequest, PublicErrorValidationPageCursor},
		{"?generation=" + strconv.FormatInt(generation, 10) + "&sort=message", http.StatusBadRequest, PublicErrorValidationPageSort},
		{"?generation=" + strconv.FormatInt(generation, 10) + "&repairAvailability=maybe", http.StatusBadRequest, PublicErrorValidationPageFilter},
		{"?generation=" + strconv.FormatInt(generation, 10) + "&check=not-a-check", http.StatusBadRequest, PublicErrorValidationPageFilter},
		{"?generation=99999", http.StatusGone, PublicErrorValidationGeneration},
	}
	for _, test := range tests {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/validation/diagnostics"+test.query, nil)
		rec := httptest.NewRecorder()
		srv.handlePublicValidationDiagnostics(rec, req)
		require.Equal(t, test.status, rec.Code, test.query)
		var response ErrorResponse
		require.NoError(t, json.NewDecoder(rec.Body).Decode(&response))
		require.Equal(t, test.code, response.Code)
	}
}

func TestHandlePublicValidationScopeSummariesAndScopedDetailAgree(t *testing.T) {
	ctx := context.Background()
	store, err := semdb.Open(filepath.Join(t.TempDir(), "validation-scope-handler.db"))
	require.NoError(t, err)
	defer func() { _ = store.Close() }()
	generation, err := store.SetValidationRunning(ctx)
	require.NoError(t, err)
	snapshot := webValidationSnapshot(generation, 2, 0)
	snapshot.Diagnostics[0].AffectedNodeIDs = []string{"node-a"}
	snapshot.Diagnostics[1].AffectedNodeIDs = []string{"node-b"}
	published, err := store.PublishValidationSnapshot(ctx, snapshot)
	require.NoError(t, err)
	require.True(t, published)
	srv := &Server{runtime: &Runtime{IntelStore: store}}

	body := `{"generation":` + strconv.FormatInt(generation, 10) + `,"scopes":[{"kind":"global"},{"kind":"node","key":"node-a"}]}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/validation/summaries", strings.NewReader(body))
	rec := httptest.NewRecorder()
	srv.handlePublicValidationSummaries(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)
	var summaries semdb.ValidationScopeSummaryResponse
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&summaries))
	require.Equal(t, 2, summaries.Summaries[0].IssueCount)
	require.Equal(t, 1, summaries.Summaries[1].IssueCount)

	req = httptest.NewRequest(http.MethodGet, "/api/v1/validation/diagnostics?generation="+strconv.FormatInt(generation, 10)+"&scopeKind=node&scopeKey=node-a", nil)
	rec = httptest.NewRecorder()
	srv.handlePublicValidationDiagnostics(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)
	var page semdb.ValidationDiagnosticPage
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&page))
	require.Equal(t, summaries.Summaries[1].IssueCount, page.Total)
	require.Equal(t, "node-a", page.Diagnostics[0].AffectedNodeIDs[0])

	body = `{"generation":` + strconv.FormatInt(generation, 10) + `,"filter":{"text":"notes/a.md"},"scopes":[{"kind":"global"}]}`
	req = httptest.NewRequest(http.MethodPost, "/api/v1/validation/summaries", strings.NewReader(body))
	rec = httptest.NewRecorder()
	srv.handlePublicValidationSummaries(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&summaries))
	require.Equal(t, 1, summaries.Summaries[0].IssueCount)

	req = httptest.NewRequest(http.MethodGet, "/api/v1/validation/diagnostics?generation="+strconv.FormatInt(generation, 10)+"&text=notes%2Fa.md", nil)
	rec = httptest.NewRecorder()
	srv.handlePublicValidationDiagnostics(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&page))
	require.Equal(t, summaries.Summaries[0].IssueCount, page.Total)
}

func TestHandlePublicValidationGroupsCountsVariantsAndFiltersDiagnostics(t *testing.T) {
	ctx := context.Background()
	store, err := semdb.Open(filepath.Join(t.TempDir(), "validation-groups-handler.db"))
	require.NoError(t, err)
	defer func() { _ = store.Close() }()
	generation, err := store.SetValidationRunning(ctx)
	require.NoError(t, err)
	snapshot := webValidationSnapshot(generation, 3, 0)
	for i := range snapshot.Diagnostics {
		snapshot.Diagnostics[i].Code = "type_ambiguous"
	}
	snapshot.Diagnostics[0].Variant = &semdb.ValidationIssueVariant{Key: "A+B", Label: "A, B"}
	snapshot.Diagnostics[1].Variant = &semdb.ValidationIssueVariant{Key: "C+D", Label: "C, D"}
	snapshot.Diagnostics[2].Variant = &semdb.ValidationIssueVariant{Key: "C+D", Label: "C, D"}
	published, err := store.PublishValidationSnapshot(ctx, snapshot)
	require.NoError(t, err)
	require.True(t, published)
	srv := &Server{runtime: &Runtime{IntelStore: store}}
	gen := strconv.FormatInt(generation, 10)
	post := func(body string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		srv.handlePublicValidationGroups(rec, httptest.NewRequest(http.MethodPost, "/api/v1/validation/groups", strings.NewReader(body)))
		return rec
	}

	rec := post(`{"generation":` + gen + `,"filter":{"check":"ontology"}}`)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var groups semdb.ValidationIssueGroupResponse
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&groups))
	require.Equal(t, []semdb.ValidationIssueGroup{
		{Check: validate.CheckOntology, Code: "type_ambiguous", Variant: &semdb.ValidationIssueVariant{Key: "C+D", Label: "C, D"}, IssueCount: 2, AffectedFileCount: 2},
		{Check: validate.CheckOntology, Code: "type_ambiguous", Variant: &semdb.ValidationIssueVariant{Key: "A+B", Label: "A, B"}, IssueCount: 1, AffectedFileCount: 1},
	}, groups.Groups)

	for _, test := range []struct {
		body   string
		status int
		code   string
	}{
		{`{"generation":99999}`, http.StatusGone, PublicErrorValidationGeneration},
		{`{"generation":` + gen + `,"filter":{"variant":"C+D"}}`, http.StatusBadRequest, PublicErrorValidationScope},
		{`{"generation":` + gen + `,"scope":{"kind":"file"}}`, http.StatusBadRequest, PublicErrorValidationScope},
		{`{"generation":` + gen + `,"scopes":[]}`, http.StatusBadRequest, PublicErrorValidationScope},
	} {
		rec := post(test.body)
		require.Equal(t, test.status, rec.Code, test.body)
		var response ErrorResponse
		require.NoError(t, json.NewDecoder(rec.Body).Decode(&response))
		require.Equal(t, test.code, response.Code, test.body)
	}

	rec = httptest.NewRecorder()
	srv.handlePublicValidationDiagnostics(rec, httptest.NewRequest(http.MethodGet, "/api/v1/validation/diagnostics?generation="+gen+"&code=type_ambiguous&variant=C%2BD", nil))
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var page semdb.ValidationDiagnosticPage
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&page))
	require.Equal(t, groups.Groups[0].IssueCount, page.Total)
	require.Equal(t, "C+D", page.Diagnostics[0].Variant.Key)

	rec = httptest.NewRecorder()
	srv.handlePublicValidationDiagnostics(rec, httptest.NewRequest(http.MethodGet, "/api/v1/validation/diagnostics?generation="+gen+"&variant=C%2BD", nil))
	require.Equal(t, http.StatusBadRequest, rec.Code)
	var response ErrorResponse
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&response))
	require.Equal(t, PublicErrorValidationPageFilter, response.Code)
}

func TestValidationHealthRequiresCompleteSuccessfulCheckEvidenceForClean(t *testing.T) {
	clean := webValidationSnapshot(1, 0, 0)
	require.Equal(t, ValidationHealthCurrentClean, validationHealth(semdb.ValidationStatusOK, &clean))

	missing := clean
	missing.Checks = nil
	require.Equal(t, ValidationHealthFailed, validationHealth(semdb.ValidationStatusOK, &missing))

	failed := clean
	failed.ErrorCount = 1
	failed.Checks[0].Outcome = semdb.ValidationCheckOutcomeFailed
	require.Equal(t, ValidationHealthFailed, validationHealth(semdb.ValidationStatusOK, &failed))

	incomplete := clean
	incomplete.Completion = semdb.ValidationCompletionIncomplete
	require.Equal(t, ValidationHealthIncomplete, validationHealth(semdb.ValidationStatusOK, &incomplete))

	require.Equal(t, ValidationHealthIncomplete, validationHealth(semdb.ValidationStatusOK, nil))
}

func webValidationSnapshot(generation int64, issueCount, errorCount int) semdb.ValidationSnapshot {
	diagnostics := make([]semdb.ValidationDiagnostic, issueCount)
	for i := range diagnostics {
		path := "notes/" + string(rune('a'+i)) + ".md"
		diagnostics[i] = semdb.ValidationDiagnostic{
			IssueKey: "issue-" + string(rune('a'+i)), Check: validate.CheckOntology, Code: "missing_type",
			PrimaryPath: path, AffectedPaths: []string{path},
		}
	}
	return semdb.ValidationSnapshot{
		VaultIdentity: "vault", Generation: generation, Scope: "default",
		SelectedChecks: []string{validate.CheckOntology}, Completion: semdb.ValidationCompletionComplete,
		IssueCount: issueCount, ErrorCount: errorCount, AffectedFileCount: issueCount,
		Checks:      []semdb.ValidationCheckSnapshot{{Check: validate.CheckOntology, Outcome: semdb.ValidationCheckOutcomeCompleted, IssueCount: issueCount}},
		Diagnostics: diagnostics,
	}
}

func TestHandlePublicValidationInterfaceScopeCoversNotesOfImplementingTypes(t *testing.T) {
	ctx := context.Background()
	vault := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(vault, ".rhizome", "ontology"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(vault, ".rhizome", "ontology", "schema.graphql"), []byte(`
interface Work { title: String! }
type Bug implements Work @node(paths: ["bugs/*.md"]) { title: String! }
type Task implements Work @node(paths: ["tasks/*.md"]) { title: String! }
`), 0o644))
	store, err := semdb.Open(filepath.Join(t.TempDir(), "validation-interface-scope.db"))
	require.NoError(t, err)
	defer func() { _ = store.Close() }()
	require.NoError(t, store.UpsertOntologyTypes(ctx, []semdb.OntologyNoteTypeRow{
		{NotePath: "bugs/b.md", TypeName: "Bug", SchemaHash: "hash"},
		{NotePath: "tasks/t.md", TypeName: "Task", SchemaHash: "hash"},
	}))
	generation, err := store.SetValidationRunning(ctx)
	require.NoError(t, err)
	snapshot := webValidationSnapshot(generation, 3, 0)
	// An issue in a Bug note, one in a Task note, and one in neither.
	for index, path := range []string{"bugs/b.md", "tasks/t.md"} {
		snapshot.Diagnostics[index].PrimaryPath = path
		snapshot.Diagnostics[index].AffectedPaths = []string{path}
		snapshot.Diagnostics[index].AffectedNotePaths = []string{path}
	}
	snapshot.AffectedNoteCount = 2
	published, err := store.PublishValidationSnapshot(ctx, snapshot)
	require.NoError(t, err)
	require.True(t, published)
	srv := &Server{cfg: Config{VaultPath: vault}, runtime: &Runtime{IntelStore: store}}
	gen := strconv.FormatInt(generation, 10)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/validation/diagnostics?generation="+gen+"&scopeKind=interface&scopeKey=Work", nil)
	rec := httptest.NewRecorder()
	srv.handlePublicValidationDiagnostics(rec, req)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var page semdb.ValidationDiagnosticPage
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&page))
	require.Equal(t, 2, page.Total)

	body := `{"generation":` + gen + `,"scopes":[{"kind":"interface","key":"Work"},{"kind":"type","key":"Bug"}]}`
	req = httptest.NewRequest(http.MethodPost, "/api/v1/validation/summaries", strings.NewReader(body))
	rec = httptest.NewRecorder()
	srv.handlePublicValidationSummaries(rec, req)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var summaries semdb.ValidationScopeSummaryResponse
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&summaries))
	require.Equal(t, page.Total, summaries.Summaries[0].IssueCount)
	require.Equal(t, 1, summaries.Summaries[1].IssueCount)

	body = `{"generation":` + gen + `,"scope":{"kind":"interface","key":"Work"}}`
	req = httptest.NewRequest(http.MethodPost, "/api/v1/validation/groups", strings.NewReader(body))
	rec = httptest.NewRecorder()
	srv.handlePublicValidationGroups(rec, req)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var groups semdb.ValidationIssueGroupResponse
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&groups))
	require.Len(t, groups.Groups, 1)
	require.Equal(t, page.Total, groups.Groups[0].IssueCount)
}
