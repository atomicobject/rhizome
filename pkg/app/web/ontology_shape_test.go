package web

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	semdb "github.com/atomicobject/rhizome/pkg/anchors/sqlite"
	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/ontology/noderead"
	"github.com/atomicobject/rhizome/pkg/validate"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"
)

func shapeGET(t testing.TB, s *Server, target string) *httptest.ResponseRecorder {
	t.Helper()
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, newApplicationRequest(http.MethodGet, target, nil))
	return w
}

func TestOntologyShapeHTTPPartsAndRecentNotes(t *testing.T) {
	s := newSummaryParityFixture(t)
	for _, part := range []string{"members", "links", "folders", "members,links,folders"} {
		w := shapeGET(t, s, "/api/v1/ontology/shape?parts="+part)
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())
		var shape noderead.ShapeResponse
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &shape))
		require.Positive(t, shape.TotalNotes)
		require.Positive(t, shape.TypedNotes)
		require.Positive(t, shape.UntypedNotes)
		require.Equal(t, strings.Contains(part, "members"), shape.Members != nil)
		require.Equal(t, strings.Contains(part, "links"), shape.Links != nil)
		require.Equal(t, strings.Contains(part, "folders"), shape.Folders != nil)
		if shape.Members != nil {
			for _, typ := range shape.Members.Types {
				require.NotEqual(t, "Story", typ.Name)
			}
		}
	}
	w := shapeGET(t, s, "/api/v1/ontology/shape?parts=records")
	require.Equal(t, http.StatusBadRequest, w.Code)
	// An untyped note must lead the capped newest-first result, irrespective of
	// alphabetical title and the issue-first ordering of an ordinary type list.
	latest := filepath.Join(s.cfg.VaultPath, "loose.md")
	future := time.Now().Add(time.Hour)
	require.NoError(t, os.Chtimes(latest, future, future))
	_, err := ontology.EnsureFreshRuntimeWithStore(t.Context(), testNoteMetadataIndexer(t), s.cfg.VaultDef, &obsidian.Note{}, s.runtime.Intel())
	require.NoError(t, err)
	w = shapeGET(t, s, "/api/v1/ontology/types/__all__?limit=1")
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var recent OntologyTypeResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &recent))
	require.Len(t, recent.Notes, 1)
	require.Equal(t, "loose.md", recent.Notes[0].Ref.NotePath)
	require.Empty(t, recent.Notes[0].ResolvedType)
	require.Greater(t, recent.Count, 1)
	recent = OntologyTypeResponse{}
	w = shapeGET(t, s, "/api/v1/ontology/types/__all__?limit=1&notes=none")
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &recent))
	require.Empty(t, recent.Notes)
	for _, limit := range []string{"0", "501", "nope", "-1", ""} {
		w = shapeGET(t, s, "/api/v1/ontology/types/__all__?limit="+limit)
		require.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
	}
	// Metadata can publish before ontology. Old rows remain usable, but the
	// response must expose the mismatched publication witnesses as rebuilding.
	state, err := s.runtime.Intel().GetNoteMetadataState(t.Context())
	require.NoError(t, err)
	state.LoadedAt++
	state.NotesHash = "next-generation"
	require.NoError(t, s.runtime.Intel().ApplyNoteMetadataDelta(t.Context(), semdb.NoteMetadataDelta{State: state}))
	w = shapeGET(t, s, "/api/v1/ontology/shape")
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.Contains(t, w.Body.String(), `"rebuilding":true`)
	// Readiness is a response flag during republication, not an empty success
	// masquerading as a finished index.
	require.NoError(t, s.runtime.Intel().ReplaceNoteMetadataSnapshot(t.Context(), semdb.NoteMetadataSnapshot{}))
	w = shapeGET(t, s, "/api/v1/ontology/shape")
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.Contains(t, w.Body.String(), `"rebuilding":true`)
}

// Synthetic persisted inputs mirror a 2,500-note, 15-type vault with seven
// links per note. This times the real HTTP endpoint, including JSON encoding.
// newShapeLatencyServer serves the shape of a generated vault of `notes` notes
// across 15 types, each note linking to the next seven by wikilink and to the
// first of them by a typed relation.
func newShapeLatencyServer(t testing.TB, notes int) *Server {
	t.Helper()
	root := t.TempDir()
	ctx := context.Background()
	dir := filepath.Join(root, ".rhizome", "ontology")
	require.NoError(t, os.MkdirAll(dir, 0755))
	schemaText := "enum Stage { open @view(stage: open) active @view(stage: active) }\n"
	for i := 0; i < 15; i++ {
		schemaText += fmt.Sprintf("type Type%02d @node(paths: [\"Type%02d/*.md\"]) { title: String @field stage: Stage @field nextStep: String @field @display(importance: KEY) peers: [Type%02d!] @link related: [Note!] @link }\n", i, i, (i+1)%15)
	}
	require.NoError(t, os.WriteFile(filepath.Join(dir, "schema.graphql"), []byte(schemaText), 0600))
	schema, err := ontology.LoadSchema(root)
	require.NoError(t, err)
	store, err := semdb.Open(filepath.Join(root, "fixture.db"))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, store.Close()) })
	metadata := semdb.NoteMetadataSnapshot{State: semdb.NoteMetadataState{Ready: true, LoadedAt: 1, NotesHash: "fixture"}}
	snapshot := semdb.OntologySnapshot{SchemaState: semdb.OntologySchemaState{Ready: true, LoadedAt: 1, NotesHash: "fixture", SchemaHash: schema.Hash, MaterializationVersion: ontology.OntologyMaterializationVersion}}
	model := codeanchor.IntelOntologyNodeReadModel{}
	pathFor := func(i int) string { return fmt.Sprintf("Type%02d/n%05d.md", i%15, i) }
	for i := 0; i < notes; i++ {
		path := pathFor(i)
		typ := fmt.Sprintf("Type%02d", i%15)
		id := fmt.Sprintf("note-%d", i)
		metadata.Notes = append(metadata.Notes, semdb.NoteMetadataRow{Path: path, Title: fmt.Sprintf("Note %d", i), Mtime: int64(i + 1), IndexedAt: 1, FormatID: "markdown"})
		snapshot.NoteTypes = append(snapshot.NoteTypes, semdb.OntologyNoteTypeRow{NotePath: path, TypeName: typ, SchemaHash: schema.Hash, UpdatedAt: 1})
		model.NotePaths = append(model.NotePaths, path)
		model.Nodes = append(model.Nodes, codeanchor.IntelOntologyNode{NodeID: id, NotePath: path, NodeRefJSON: "{}", NodeKind: "NOTE", TypeName: typ, SchemaHash: schema.Hash, UpdatedAt: 1})
		model.FieldValues = append(model.FieldValues, codeanchor.IntelOntologyNodeFieldValue{NodeID: id, NotePath: path, TypeName: typ, FieldName: "stage", ValueKind: "enum", ValueText: "open", ValueNorm: "open", SchemaHash: schema.Hash, UpdatedAt: 1})
		for j := 1; j <= 7; j++ {
			target := (i + j) % notes
			metadata.WikilinkEdges = append(metadata.WikilinkEdges, semdb.GraphDocEdgeRow{SrcPath: path, DstPath: pathFor(target), Kind: "wikilink"})
			if j == 1 {
				snapshot.Edges = append(snapshot.Edges, semdb.OntologyEdgeRow{SrcPath: path, SrcNodeID: id, DstPath: pathFor(target), DstNodeID: fmt.Sprintf("note-%d", target), RelationName: "peers", DstType: fmt.Sprintf("Type%02d", target%15), Structural: true, SchemaHash: schema.Hash, UpdatedAt: 1})
			}
		}
	}
	require.NoError(t, store.ReplaceNoteMetadataSnapshot(ctx, metadata))
	require.NoError(t, store.ReplaceOntologySnapshot(ctx, snapshot))
	require.NoError(t, store.ReplaceOntologyNodeReadModel(ctx, model))
	s := &Server{cfg: Config{VaultPath: root, VaultDef: obsidian.VaultDefinition{Path: root}}, runtime: &Runtime{IntelStore: store}, mux: http.NewServeMux()}
	s.mux.HandleFunc("/api/v1/ontology/shape", publicGET(s.requireIndexReady(s.handleOntologyShape)))
	return s
}

// warmShapeLatency requests the whole shape once to warm it, then `runs`
// times, and returns the mean and slowest request.
func warmShapeLatency(t testing.TB, s *Server, runs int) (mean, worst time.Duration) {
	t.Helper()
	require.Equal(t, http.StatusOK, shapeGET(t, s, "/api/v1/ontology/shape").Code)
	var total time.Duration
	for range runs {
		start := time.Now()
		w := shapeGET(t, s, "/api/v1/ontology/shape")
		elapsed := time.Since(start)
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())
		total += elapsed
		worst = max(worst, elapsed)
	}
	return total / time.Duration(runs), worst
}

func TestOntologyShapeWarmLatency2500Notes(t *testing.T) {
	s := newShapeLatencyServer(t, 2500)
	w := shapeGET(t, s, "/api/v1/ontology/shape")
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var shape noderead.ShapeResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &shape))
	require.Equal(t, 2500, shape.TotalNotes)
	require.Len(t, shape.Members.Types, 15)
	require.False(t, shape.Rebuilding)
	mean, worst := warmShapeLatency(t, s, 10)
	t.Logf("2500 notes, 15 types, 17500 note pairs: warm HTTP mean %s, max %s (10 runs)", mean, worst)
	// The budget is measured in the focused run; competing package tests can
	// contend for the CPU in make check, so timing is reported, not asserted.
	s.cfg.VaultPath = t.TempDir()
	w = shapeGET(t, s, "/api/v1/ontology/shape")
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &shape))
	require.Equal(t, 2500, shape.UntypedNotes)
	require.False(t, shape.Rebuilding, "a schema-less vault has usable untyped shape")
}

// BenchmarkOntologyShapeWarm20000Notes measures SPEC-0117's 20,000-note budget
// (under one second warm): go test ./pkg/app/web -run '^$' -bench Shape -benchtime 1x.
func BenchmarkOntologyShapeWarm20000Notes(b *testing.B) {
	s := newShapeLatencyServer(b, 20000)
	for b.Loop() {
		mean, worst := warmShapeLatency(b, s, 10)
		b.ReportMetric(float64(mean.Milliseconds()), "mean-ms")
		b.ReportMetric(float64(worst.Milliseconds()), "max-ms")
	}
}

func TestOntologyShapeIndexInitializingUsesPublic503(t *testing.T) {
	runtime := &Runtime{}
	runtime.EnableIndexGate()
	s := &Server{runtime: runtime, mux: http.NewServeMux()}
	s.registerRoutes()
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	request := newApplicationRequest(http.MethodGet, "/api/v1/ontology/shape", nil).WithContext(ctx)
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, request)
	require.Equal(t, http.StatusServiceUnavailable, w.Code)
	require.Contains(t, w.Body.String(), `"code":"INDEX_INITIALIZING"`)
	require.Equal(t, "5", w.Header().Get("Retry-After"))
}

func TestOntologyShapeCountsPublishedIssues(t *testing.T) {
	s := newSummaryParityFixture(t)
	plan := "specs/100-demo/plan.md"
	link := func(key string) semdb.ValidationDiagnostic {
		return semdb.ValidationDiagnostic{IssueKey: key, Check: validate.CheckBrokenLinks, Code: "broken_link",
			PrimaryPath: plan, AffectedPaths: []string{plan}, AffectedNotePaths: []string{plan}}
	}
	publishTestDiagnostics(t, s.runtime.Intel(), link("link-a"), link("link-b"),
		semdb.ValidationDiagnostic{IssueKey: "cycle", Check: validate.CheckOntology, Code: "parent_cycle", AffectedTypes: []string{"Spec"}})
	w := shapeGET(t, s, "/api/v1/ontology/shape?parts=members")
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var shape noderead.ShapeResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &shape))
	counts := map[string]int{}
	for _, member := range shape.Members.Types {
		counts[member.Name] = member.IssueCount
	}
	require.Equal(t, 2, counts["Plan"], "separate issues on one note count separately")
	require.Equal(t, 1, counts["Spec"], "type-level issues count without an affected note")
}

func TestOntologyShapeEmbeddedRelationsKeepHostDocumentEvidencePlain(t *testing.T) {
	fixture := prepareInterfaceFixtureVault(t)
	path := filepath.Join(fixture.root, ".rhizome", "ontology", "schema.graphql")
	schemaText, err := os.ReadFile(path)
	require.NoError(t, err)
	schemaText = []byte(strings.Replace(string(schemaText), "summary: String! @field", "summary: String! @field\n  related: ProcessSpec @link", 1))
	require.NoError(t, os.WriteFile(path, schemaText, 0600))
	path = filepath.Join(fixture.root, "specs", "prod.md")
	body, err := os.ReadFile(path)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(path, append(body, []byte("related:: [[specs/proc]]\n")...), 0600))
	_, err = ontology.EnsureFreshRuntimeWithStore(t.Context(), testNoteMetadataIndexer(t), fixture.vaultDef, &obsidian.Note{}, fixture.intelStore)
	require.NoError(t, err)
	s := newFixtureServer(t, fixture, nil)
	w := shapeGET(t, s, "/api/v1/ontology/shape")
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var shape noderead.ShapeResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &shape))
	require.Equal(t, 2, shape.TotalNotes)
	require.Len(t, shape.Members.Types, 2, "embedded types are not members")
	require.Len(t, shape.Members.Interfaces, 1)
	iface := shape.Members.Interfaces[0]
	require.Equal(t, "SpecLike", iface.Name)
	require.Equal(t, []string{"ProcessSpec", "ProductSpec"}, iface.Implementors)
	require.Equal(t, 2, iface.Count)
	require.Zero(t, iface.IssueCount, "counts stay zero before validation publication")
	require.Equal(t, []noderead.ShapePair{{A: "ProcessSpec", B: "ProductSpec", Links: 1, PlainLinks: 1, Fields: []noderead.ShapePairField{}}}, shape.Links.Pairs,
		"independent host document links stay plain; embedded relation fields never become host fields")
	publishTestDiagnostics(t, s.runtime.Intel(),
		semdb.ValidationDiagnostic{IssueKey: "note", Check: validate.CheckBrokenLinks, Code: "broken_link", PrimaryPath: "specs/proc.md", AffectedPaths: []string{"specs/proc.md"}, AffectedNotePaths: []string{"specs/proc.md"}},
		semdb.ValidationDiagnostic{IssueKey: "fragment", Check: validate.CheckOntology, Code: "parent_cycle", AffectedTypes: []string{"SpecMetric"}})
	w = shapeGET(t, s, "/api/v1/ontology/shape?parts=members")
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &shape))
	require.Equal(t, 1, shape.Members.Interfaces[0].IssueCount, "interface issues cover only note-type implementors")
}
