package query

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	semdb "github.com/atomicobject/rhizome/pkg/anchors/sqlite"
	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/testutil/sqlitefixture"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
)

// B14 measures the bounded GraphQL selectors which currently need a complete
// metadata inventory before they can rank a find query or select an interface.
// It deliberately uses a real SQLite store and prepared public queries; fixture
// construction, schema compilation, and indexing are outside timed execution.
//
// Run with a constrained scheduler so a baseline and a changed checkout are
// comparable:
//
//	GOMAXPROCS=4 GOCACHE=<worktree>/.gocache \
//	  go test ./pkg/ontology/query -run '^$' -bench '^BenchmarkB14SelectorRoots$' \
//	  -benchtime=100ms -count=3
func BenchmarkB14SelectorRoots(b *testing.B) {
	for _, noteCount := range []int{1_000, 10_000} {
		fixture := newB14SelectorFixture(b, noteCount)
		for _, selector := range b14SelectorCases {
			b.Run(fmt.Sprintf("notes=%d/%s", noteCount, selector.name), func(b *testing.B) {
				prepared := fixture.prepared(b, selector.query)
				store := &b14CountingStore{Store: fixture.store}
				deps := fixture.deps(store)

				// Fail before timing if the fixture no longer exercises the public
				// query shape or if an ordering/result regression changes its page.
				assertB14SelectorResult(b, Execute(context.Background(), deps, fixture.schema, prepared), selector)
				store.reset()

				b.ReportAllocs()
				b.ResetTimer()
				for i := 0; i < b.N; i++ {
					result := Execute(context.Background(), deps, fixture.schema, prepared)
					if len(result.Errors) != 0 {
						b.Fatal(result.Errors)
					}
				}
				b.StopTimer()
				reportB14Counters(b, store.counters(), b.N)
			})
		}
	}
}

// TestB14SelectorRootInstrumentation locks down the fixture's discriminating
// evidence. Three identical find roots share one successful metadata inventory
// and one type lookup within an execution, while fresh executions still read
// the current inventory.
func TestB14SelectorRootInstrumentation(t *testing.T) {
	fixture := newB14SelectorFixture(t, 1_000)
	for _, selector := range b14SelectorCases {
		t.Run(selector.name, func(t *testing.T) {
			store := &b14CountingStore{Store: fixture.store}
			result := Execute(context.Background(), fixture.deps(store), fixture.schema, fixture.prepared(t, selector.query))
			assertB14SelectorResult(t, result, selector)

			counts := store.counters()
			if counts.metadataCalls != selector.metadataCalls {
				t.Fatalf("metadata calls = %d, want %d", counts.metadataCalls, selector.metadataCalls)
			}
			if counts.metadataRows != selector.metadataCalls*1_000 {
				t.Fatalf("metadata rows = %d, want %d", counts.metadataRows, selector.metadataCalls*1_000)
			}
			if counts.typeCalls != 1 {
				t.Fatalf("type calls = %d, want 1", counts.typeCalls)
			}
			if counts.typeCandidatePaths != selector.typeCandidates {
				t.Fatalf("type candidate paths = %d, want %d", counts.typeCandidatePaths, selector.typeCandidates)
			}
			if counts.typeRows != selector.typeCandidates {
				t.Fatalf("type rows = %d, want %d", counts.typeRows, selector.typeCandidates)
			}
		})
	}
}

type b14SelectorCase struct {
	name           string
	query          string
	rootNames      []string
	typedRoot      bool
	metadataCalls  int64
	typeCandidates int64
}

var b14SelectorCases = []b14SelectorCase{
	{
		name:           "generic-find-broad-single-root",
		query:          `{ notes(find: "common", first: 10) { nodes { path title } } }`,
		rootNames:      []string{"notes"},
		metadataCalls:  1,
		typeCandidates: 1_000, // adjusted by preparedB14SelectorCase for 10k
	},
	{
		name:           "typed-find-broad-single-root",
		query:          `{ decision(find: "common", first: 10) { path title } }`,
		rootNames:      []string{"decision"},
		typedRoot:      true,
		metadataCalls:  1,
		typeCandidates: 1_000,
	},
	{
		name:           "typed-find-selective-single-root",
		query:          `{ decision(find: "needle", first: 10) { path title } }`,
		rootNames:      []string{"decision"},
		typedRoot:      true,
		metadataCalls:  1,
		typeCandidates: 10,
	},
	{
		name: "generic-find-broad-three-roots",
		query: `{
  first: notes(find: "common", first: 10) { nodes { path title } }
  second: notes(find: "common", first: 10) { nodes { path title } }
  third: notes(find: "common", first: 10) { nodes { path title } }
}`,
		rootNames:      []string{"first", "second", "third"},
		metadataCalls:  1,
		typeCandidates: 1_000,
	},
	{
		name:           "interface-type-single-root",
		query:          `{ notes(type: "SummaryDoc", first: 10) { nodes { path title } } }`,
		rootNames:      []string{"notes"},
		metadataCalls:  1,
		typeCandidates: 1_000,
	},
}

type b14SelectorFixture struct {
	root       string
	schema     *ontology.Schema
	execSchema *ExecutableSchema
	store      *semdb.Store
	noteCount  int
}

func newB14SelectorFixture(t testing.TB, noteCount int) *b14SelectorFixture {
	t.Helper()
	root := t.TempDir()
	mustWriteB14(t, filepath.Join(root, ".rhizome", "config.yml"), "notes:\n  includes: [\"**/*.md\"]\n")
	mustWriteB14(t, filepath.Join(root, ".rhizome", "ontology", "schema.graphql"), `
interface SummaryDoc {
  name: String!
}

type Decision implements SummaryDoc @node(paths: ["notes/decisions/*.md"]) {
  name: String!
}

type Project implements SummaryDoc @node(paths: ["notes/projects/*.md"]) {
  name: String!
}
`)
	notes := make([]semdb.NoteMetadataRow, 0, noteCount)
	noteTypes := make([]semdb.OntologyNoteTypeRow, 0, noteCount)
	for i := 0; i < noteCount; i++ {
		typeName, directory := "Decision", "decisions"
		if i%2 == 1 {
			typeName, directory = "Project", "projects"
		}
		title := fmt.Sprintf("Common %s %05d", typeName, i)
		// One percent of notes is selective at both 1k and 10k, while keeping
		// more than a page of matching Decision records for first=10.
		if i%100 == 0 {
			title = fmt.Sprintf("Common Needle %s %05d", typeName, i)
		}
		path := filepath.ToSlash(filepath.Join("notes", directory, fmt.Sprintf("%05d.md", i)))
		notes = append(notes, semdb.NoteMetadataRow{
			Path: path, Title: title, ContentHash: fmt.Sprintf("b14-%d", i),
			IndexerVersion: "b14", Mtime: 1, Size: 1, IndexedAt: 1, FormatID: "markdown",
			Projection: semdb.NoteProjectionState{
				ProviderVersion: "b14", ProjectionVersion: "b14", SourceContentHash: fmt.Sprintf("b14-%d", i),
				Status: semdb.NoteProjectionStatusCurrent, UpdatedAt: 1,
			},
		})
		noteTypes = append(noteTypes, semdb.OntologyNoteTypeRow{NotePath: path, TypeName: typeName, SchemaHash: "b14-schema", UpdatedAt: 1})
	}

	schema, err := ontology.LoadSchema(root)
	if err != nil {
		t.Fatal(err)
	}
	execSchema, err := BuildExecutableSchema(schema)
	if err != nil {
		t.Fatal(err)
	}
	dbPath := filepath.Join(root, ".rhizome", "db.sqlite")
	store, err := sqlitefixture.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	if err := store.ReplaceNoteMetadataSnapshot(context.Background(), semdb.NoteMetadataSnapshot{
		State: semdb.NoteMetadataState{NotesHash: "b14-notes-hash", RawNotesHash: "b14-notes-hash", LoadedAt: 1, Ready: true},
		Notes: notes,
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.ReplaceOntologySnapshot(context.Background(), semdb.OntologySnapshot{
		NoteTypes: noteTypes,
		SchemaState: semdb.OntologySchemaState{
			SchemaHash: schema.Hash, NotesHash: "b14-notes-hash", LoadedAt: 1, Ready: true,
		},
	}); err != nil {
		t.Fatal(err)
	}
	return &b14SelectorFixture{root: root, schema: schema, execSchema: execSchema, store: store, noteCount: noteCount}
}

func mustWriteB14(t testing.TB, path, contents string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatal(err)
	}
}

func (f *b14SelectorFixture) deps(store Store) Deps {
	return Deps{
		VaultDef:   obsidian.VaultDefinition{Path: f.root},
		NoteReader: &obsidian.Note{},
		Store:      store,
		Service:    ontology.NewService(obsidian.VaultDefinition{Path: f.root}, &obsidian.Note{}, f.store, f.schema),
	}
}

func (f *b14SelectorFixture) prepared(t testing.TB, raw string) *PreparedQuery {
	t.Helper()
	prepared, errs := Prepare(f.execSchema, raw)
	if len(errs) != 0 {
		t.Fatal(errs)
	}
	return prepared
}

func assertB14SelectorResult(t testing.TB, result Result, selector b14SelectorCase) {
	t.Helper()
	if len(result.Errors) != 0 {
		t.Fatal(result.Errors)
	}
	for _, rootName := range selector.rootNames {
		value, ok := result.Data[rootName]
		if !ok {
			t.Fatalf("missing result root %q", rootName)
		}
		if selector.typedRoot {
			if nodes, ok := value.([]any); !ok || len(nodes) != 10 {
				t.Fatalf("typed root %q returned %#v, want 10 nodes", rootName, value)
			} else {
				assertB14SelectorPaths(t, rootName, nodes, selector)
			}
			continue
		}
		connection, ok := value.(map[string]any)
		if !ok {
			t.Fatalf("root %q returned %#v, want connection", rootName, value)
		}
		if nodes, ok := connection["nodes"].([]any); !ok || len(nodes) != 10 {
			t.Fatalf("root %q returned %#v, want 10 nodes", rootName, value)
		} else {
			assertB14SelectorPaths(t, rootName, nodes, selector)
		}
	}
}

func assertB14SelectorPaths(t testing.TB, rootName string, nodes []any, selector b14SelectorCase) {
	t.Helper()
	step := 2
	if selector.name == "typed-find-selective-single-root" {
		step = 100
	}
	for i, node := range nodes {
		record, ok := node.(map[string]any)
		if !ok {
			t.Fatalf("root %q node %d = %#v, want record", rootName, i, node)
		}
		want := fmt.Sprintf("notes/decisions/%05d.md", i*step)
		if got, _ := record["path"].(string); got != want {
			t.Fatalf("root %q node %d path = %q, want %q", rootName, i, got, want)
		}
	}
}

type b14Counters struct {
	metadataCalls, metadataRows, metadataNanos         int64
	typeCalls, typeCandidatePaths, typeRows, typeNanos int64
}

type b14CountingStore struct {
	*semdb.Store
	counts b14Counters
}

func (s *b14CountingStore) CurrentNoteMetadataRows(ctx context.Context) ([]semdb.NoteMetadataRow, error) {
	started := time.Now()
	rows, err := s.Store.CurrentNoteMetadataRows(ctx)
	s.counts.metadataCalls++
	s.counts.metadataRows += int64(len(rows))
	s.counts.metadataNanos += time.Since(started).Nanoseconds()
	return rows, err
}

func (s *b14CountingStore) OntologyTypesByPaths(ctx context.Context, pathsList []string) (map[string]semdb.OntologyNoteTypeRow, error) {
	started := time.Now()
	rows, err := s.Store.OntologyTypesByPaths(ctx, pathsList)
	s.counts.typeCalls++
	s.counts.typeCandidatePaths += int64(len(pathsList))
	s.counts.typeRows += int64(len(rows))
	s.counts.typeNanos += time.Since(started).Nanoseconds()
	return rows, err
}

func (s *b14CountingStore) reset() { s.counts = b14Counters{} }

func (s *b14CountingStore) counters() b14Counters { return s.counts }

func reportB14Counters(b *testing.B, counters b14Counters, operations int) {
	if operations == 0 {
		return
	}
	perOp := float64(operations)
	b.ReportMetric(float64(counters.metadataCalls)/perOp, "metadata-calls/op")
	b.ReportMetric(float64(counters.metadataRows)/perOp, "metadata-rows/op")
	b.ReportMetric(float64(counters.metadataNanos)/perOp, "metadata-ns/op")
	b.ReportMetric(float64(counters.typeCalls)/perOp, "type-calls/op")
	b.ReportMetric(float64(counters.typeCandidatePaths)/perOp, "type-candidates/op")
	b.ReportMetric(float64(counters.typeRows)/perOp, "type-rows/op")
	b.ReportMetric(float64(counters.typeNanos)/perOp, "type-ns/op")
}
