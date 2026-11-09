package web

import (
	"context"
	"database/sql"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/atomicobject/rhizome/pkg/embeddings"
	embedsqlite "github.com/atomicobject/rhizome/pkg/embeddings/sqlite"
	ontologyquery "github.com/atomicobject/rhizome/pkg/ontology/query"
	"github.com/atomicobject/rhizome/pkg/sqliteutil"
	semdb "github.com/atomicobject/rhizome/pkg/store"
	"github.com/stretchr/testify/require"
	"github.com/vektah/gqlparser/v2/ast"
)

func replyData(t *testing.T, reply graphQLReply) map[string]any {
	t.Helper()
	require.Equal(t, http.StatusOK, reply.status, string(reply.body))
	data, _ := reply.decoded["data"].(map[string]any)
	require.NotNil(t, data, string(reply.body))
	return data
}

// The witness stats the note a ref resolves to, not the ref's text, so
// deleting the resolved note misses at once, before the watcher's write
// moves the generation (SPEC-0136 R4).
func TestGraphQLReadCacheWitnessesTheNoteARefResolvesTo(t *testing.T) {
	srv, fixture := newGraphQLReadCacheServer(t)
	const query = `query { node(ref: "plan") { path title resolvedType } }`
	hash := readQueryHash("graphql", query, "")
	payload := map[string]any{"query": query}

	first := replyData(t, postGraphQL(t, srv, payload))
	node, _ := first["node"].(map[string]any)
	require.Equal(t, "specs/100-demo/plan.md", node["path"], "a basename ref resolves to the canonical note")
	replyData(t, postGraphQL(t, srv, payload))
	require.EqualValues(t, 1, srv.reads.Metrics().Queries[hash].Hits)

	generation := srv.IndexGeneration(t.Context())
	require.NoError(t, os.Remove(filepath.Join(fixture.root, "specs", "100-demo", "plan.md")))
	require.Equal(t, generation, srv.IndexGeneration(t.Context()), "no watcher runs, so the generation has not moved")
	after := replyData(t, postGraphQL(t, srv, payload))
	require.Nil(t, after["node"], "the deleted note is not served from the cache")
	require.EqualValues(t, 2, srv.reads.Metrics().Queries[hash].Executions)
}

// A field read from note files bypasses the cache at any depth; a selection
// of index fields is cached and stays equal to a fresh read after the files
// change on disk (SPEC-0136 R4).
func TestGraphQLReadCacheBypassesFieldsReadFromNoteFiles(t *testing.T) {
	srv, fixture := newGraphQLReadCacheServer(t)
	planFile := filepath.Join(fixture.root, "specs", "100-demo", "plan.md")
	for _, query := range []string{
		`query { plan(first: 10) { path content } }`,
		`query { plan(first: 10) { path implementationPhases { title } } }`,
		`query { node(ref: "specs/100-demo/spec.md") { ... on Spec { plans { path workspace { version } } } } }`,
		`query { notes(find: "plan", first: 5) { nodes { path locator { ref { notePath } } } } }`,
	} {
		for range 2 {
			replyData(t, postGraphQL(t, srv, map[string]any{"query": query}))
		}
		stats := srv.reads.Metrics().Queries[readQueryHash("graphql", query, "")]
		require.EqualValues(t, 2, stats.Bypasses, query)
		require.Zero(t, stats.Hits, query)
	}

	contentQuery := map[string]any{"query": `query { plan(first: 10) { path content } }`}
	data, err := os.ReadFile(planFile)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(planFile, append(data, []byte("\nEdited on disk.\n")...), 0o644))
	plans, _ := replyData(t, postGraphQL(t, srv, contentQuery))["plan"].([]any)
	require.Len(t, plans, 1)
	require.Contains(t, plans[0].(map[string]any)["content"], "Edited on disk.")

	// Every hit is re-executed and compared: fields classified as index
	// fields must not change when only the files do.
	srv.reads.verifyRate = 1
	indexQuery := map[string]any{"query": `query {
	  plan(first: 10) { __typename path title summary frontmatter tags updatedAt issueCount ref { notePath kind } spec { path title summary } }
	  nodes(refs: ["specs/100-demo/spec.md"]) { items { requestedRef node { path title nodeKind } } pageInfo { returnedCount truncated } }
	  resolve(ref: "spec") { found path title resolvedType ref { notePath } }
	}`}
	before := postGraphQL(t, srv, indexQuery)
	replyData(t, before)
	require.NoError(t, os.WriteFile(planFile, []byte("---\nsummary: Changed only on disk\nspec: specs/100-demo/spec.md\n---\n# Renamed on disk\n"), 0o644))
	hit := postGraphQL(t, srv, indexQuery)
	require.Equal(t, before.body, hit.body)
	// Close waits for the verification and flushes its statistics window.
	var flushed ReadCacheMetrics
	srv.reads.record = func(metrics ReadCacheMetrics) { flushed = metrics }
	srv.reads.Close()
	stats := flushed.Queries[readQueryHash("graphql", indexQuery["query"].(string), "")]
	require.EqualValues(t, 1, stats.Hits, "%+v", stats)
	require.Zero(t, stats.Mismatches, "an index field read the changed file")
}

// A note node resolves the built-in content field even when its type declares
// one, so the classifier follows that precedence and never caches a body read
// from disk as an indexed field.
func TestGraphQLReadCacheFollowsBuiltinPrecedenceOverAuthoredFields(t *testing.T) {
	srv, fixture := newGraphQLReadCacheServer(t)
	schemaFile := filepath.Join(fixture.root, ".rhizome", "ontology", "schema.graphql")
	schema, err := os.ReadFile(schemaFile)
	require.NoError(t, err)
	declared := strings.Replace(string(schema), "type Plan\n  @node(paths: [\"specs/*/plan.md\"], label: \"Plan\", keyField: \"summary\") {\n", "type Plan\n  @node(paths: [\"specs/*/plan.md\"], label: \"Plan\", keyField: \"summary\") {\n  content: String @field\n", 1)
	require.NotEqual(t, string(schema), declared)
	require.NoError(t, os.WriteFile(schemaFile, []byte(declared), 0o644))

	query := map[string]any{"query": `query { plan(first: 10) { path content } }`}
	plans, _ := replyData(t, postGraphQL(t, srv, query))["plan"].([]any)
	require.Len(t, plans, 1)
	require.NotContains(t, plans[0].(map[string]any)["content"], "Edited on disk.")

	planFile := filepath.Join(fixture.root, "specs", "100-demo", "plan.md")
	data, err := os.ReadFile(planFile)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(planFile, append(data, []byte("\nEdited on disk.\n")...), 0o644))
	plans, _ = replyData(t, postGraphQL(t, srv, query))["plan"].([]any)
	require.Len(t, plans, 1)
	require.Contains(t, plans[0].(map[string]any)["content"], "Edited on disk.", "the built-in body is read fresh")
	require.Zero(t, srv.reads.Metrics().Queries[readQueryHash("graphql", query["query"].(string), "")].Hits)
}

// Every built-in field the node and note roots can reach, short of a field
// read from note files, is classified by type as an index field or a
// note-file field, and every classified field is reachable. A new field fails
// here until someone decides; until then it bypasses, even when an index
// field on another type shares its name.
func TestGraphQLReadCacheClassifiesEveryReachableField(t *testing.T) {
	srv, _ := newGraphQLReadCacheServer(t)
	defs, err := srv.ontologyDefinitions()
	require.NoError(t, err)
	schema := defs.exec.Schema
	for key := range graphQLIndexFields {
		require.False(t, graphQLNoteFileFields[key], "%q is classified twice", key)
	}
	require.False(t, graphQLIndexFields[graphQLFieldKey(&ast.Definition{Name: "FutureType"}, "title")], "a name alone classifies nothing")
	seen := map[string]bool{}
	reached := map[string]bool{}
	var visit func(typeName string)
	visit = func(typeName string) {
		if seen[typeName] {
			return
		}
		seen[typeName] = true
		def := schema.Types[typeName]
		if def == nil {
			return
		}
		for _, possible := range schema.PossibleTypes[typeName] {
			visit(possible.Name)
		}
		authoredType := defs.schema.Types[typeName]
		authoredInterface := defs.schema.Interfaces[typeName]
		for _, field := range def.Fields {
			if strings.HasPrefix(field.Name, "__") {
				continue
			}
			builtinFirst := isNoteDefinition(def) && ontologyquery.NoteBuiltinFieldPrecedes(field.Name)
			if !builtinFirst && ((authoredType != nil && authoredType.ByName[field.Name] != nil) || (authoredInterface != nil && authoredInterface.ByName[field.Name] != nil)) {
				// Authored fields follow the query layer's projection rule.
				visit(field.Type.Name())
				continue
			}
			key := graphQLFieldKey(def, field.Name)
			reached[key] = true
			require.True(t, graphQLIndexFields[key] || graphQLNoteFileFields[key], "field %s.%s (%s) must be classified as an index field or a note-file field", typeName, field.Name, key)
			if graphQLIndexFields[key] {
				visit(field.Type.Name())
			}
		}
	}
	for _, root := range schema.Query.Fields {
		if graphQLCoveredRoots[root.Name] && root.Name != "validation" && root.Name != "ontology" || defs.exec.RootTypes[root.Name] != "" {
			visit(root.Type.Name())
		}
	}
	require.True(t, seen["NoteNode"] && seen["NodeRefResolution"] && seen["Plan"], "the walk reached the node types")
	for _, classified := range []map[string]bool{graphQLIndexFields, graphQLNoteFileFields} {
		for key := range classified {
			require.True(t, reached[key], "%s is classified but unreachable", key)
		}
	}
}

// A store write from another process, which this process's write sequence
// cannot see, moves the generation through the store's write counter.
func TestIndexGenerationMovesForAStoreWriteFromAnotherProcess(t *testing.T) {
	srv, fixture := newGraphQLReadCacheServer(t)
	hash := readQueryHash("graphql", planQuery, "")
	replyData(t, postGraphQL(t, srv, map[string]any{"query": planQuery}))
	before := srv.IndexGeneration(t.Context())

	// A second handle has its own pool and write mutex, as another process's
	// store does; only the process-wide sequence is shared, so compare the
	// counter alone.
	other, err := semdb.Open(filepath.Join(fixture.root, ".rhizome", "db.sqlite"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = other.Close() })
	_, err = other.SetValidationRunning(t.Context())
	require.NoError(t, err)

	after := srv.IndexGeneration(t.Context())
	require.NotEqual(t, writeCounter(before), writeCounter(after), "the counter witnesses the other handle's write")
	replyData(t, postGraphQL(t, srv, map[string]any{"query": planQuery}))
	require.EqualValues(t, 2, srv.reads.Metrics().Queries[hash].Executions, "the cached read is not served after another process's write")
}

// The embedding stores share the database file but write on their own
// connections; their commits leave the generation alone, so an embedding
// pass does not make pages refetch (SPEC-0136 R4).
func TestIndexGenerationIgnoresEmbeddingCommits(t *testing.T) {
	srv, fixture := newGraphQLReadCacheServer(t)
	ctx := t.Context()
	notes, err := embedsqlite.OpenNotes(ctx, embedsqlite.File(filepath.Join(fixture.root, ".rhizome", "db.sqlite")), embedsqlite.Options{Access: embedsqlite.WriteIndex, Dimensions: 4})
	require.NoError(t, err)
	t.Cleanup(func() { _ = notes.Close() })
	before := srv.IndexGeneration(ctx)

	sequence := semdb.WriteSequence()
	require.NoError(t, notes.UpsertNoteMeta(ctx, embeddings.NoteFileInfo{ID: "note", Title: "Note", Path: "note.md"}))
	require.NoError(t, notes.UpsertNoteChunks(ctx, "note", []embeddings.ChunkInput{embeddings.NewChunkInput(0, "text", "Note", "Heading")}, []embeddings.Embedding{{1, 0, 0, 0}}))
	require.Equal(t, sequence, semdb.WriteSequence(), "the embedding write bypassed this process's stores")
	require.Equal(t, before, srv.IndexGeneration(ctx))
}

func writeCounter(generation string) string {
	writes := strings.Split(generation, ".")[1]
	return writes[strings.Index(writes, "-")+1:]
}

// While a store write is open, a read neither hits nor stores, so no request
// can pair a committed write with the generation from before it.
func TestGraphQLReadCacheIsNotUsedWhileAWriteIsOpen(t *testing.T) {
	srv, fixture := newGraphQLReadCacheServer(t)
	hash := readQueryHash("graphql", planQuery, "")
	replyData(t, postGraphQL(t, srv, map[string]any{"query": planQuery}))
	require.EqualValues(t, 1, srv.reads.Metrics().Entries)

	// Another connection holds the database's write lock, so the store's
	// write starts and then waits inside its transaction.
	locker := openFixtureDB(t, fixture, sqliteutil.TxLockImmediate)
	tx, err := locker.BeginTx(t.Context(), nil)
	require.NoError(t, err)
	written := make(chan error, 1)
	go func() {
		_, err := fixture.intelStore.SetValidationRunning(context.Background())
		written <- err
	}()
	require.Eventually(t, func() bool { return semdb.WritesOpen(semdb.WriteSequence()) }, 10*time.Second, time.Millisecond)

	replyData(t, postGraphQL(t, srv, map[string]any{"query": planQuery}))
	stats := srv.reads.Metrics()
	require.Zero(t, stats.Queries[hash].Hits, "no hit while a write is open")
	require.EqualValues(t, 1, stats.Queries[hash].Bypasses)
	require.EqualValues(t, 1, stats.Entries, "nothing stored while a write is open")

	require.NoError(t, tx.Rollback())
	require.NoError(t, <-written)
	require.False(t, semdb.WritesOpen(semdb.WriteSequence()))
	replyData(t, postGraphQL(t, srv, map[string]any{"query": planQuery}))
	require.EqualValues(t, 2, srv.reads.Metrics().Queries[hash].Executions, "the write moved the generation")
}

// Cache keys carry the full SHA-256 digest of the request.
func TestReadCacheKeysUseTheFullDigest(t *testing.T) {
	srv, _ := newGraphQLReadCacheServer(t)
	replyData(t, postGraphQL(t, srv, map[string]any{"query": planQuery}))
	srv.reads.mu.Lock()
	defer srv.reads.mu.Unlock()
	require.Len(t, srv.reads.entries, 1)
	for key := range srv.reads.entries {
		digest := key[strings.LastIndexByte(key, '|')+1:]
		require.Len(t, digest, 64, key)
	}
}

func openFixtureDB(t *testing.T, fixture fixtureVault, txLock string) *sql.DB {
	t.Helper()
	db, err := sqliteutil.OpenDSN(sqliteutil.DSNWithOptions(filepath.Join(fixture.root, ".rhizome", "db.sqlite"), sqliteutil.DSNOptions{TxLockMode: txLock}), sqliteutil.Options{MaxOpenConns: 1})
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	return db
}
