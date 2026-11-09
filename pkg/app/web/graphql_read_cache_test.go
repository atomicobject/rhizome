package web

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"
)

func newGraphQLReadCacheServer(t *testing.T) (*Server, fixtureVault) {
	t.Helper()
	fixture := prepareOntologyFixtureVault(t)
	runtime := &Runtime{IntelStore: fixture.intelStore}
	runtime.EnableIndexGate()
	runtime.MarkIndexReady()
	srv := newFixtureServer(t, fixture, runtime)
	srv.cfg.NotePathOwned = func(string) bool { return true }
	return srv, fixture
}

type graphQLReply struct {
	status     int
	generation string
	body       []byte
	decoded    map[string]any
}

func postGraphQL(t *testing.T, srv *Server, payload map[string]any) graphQLReply {
	t.Helper()
	body, err := json.Marshal(payload)
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	req := newApplicationRequest(http.MethodPost, "/api/v1/graphql", bytes.NewReader(body)).WithContext(ctx)
	req.Header.Set("Content-Type", "application/json")
	res := httptest.NewRecorder()
	srv.Handler().ServeHTTP(res, req)
	reply := graphQLReply{status: res.Code, generation: res.Header().Get(GenerationHeader), body: res.Body.Bytes()}
	_ = json.Unmarshal(reply.body, &reply.decoded)
	return reply
}

func replyGeneration(t *testing.T, reply graphQLReply) string {
	t.Helper()
	extensions, ok := reply.decoded["extensions"].(map[string]any)
	require.True(t, ok, string(reply.body))
	generation, _ := extensions["generation"].(string)
	require.NotEmpty(t, generation)
	require.Equal(t, reply.generation, generation, "header and extensions carry the same generation")
	return generation
}

const planQuery = `query { node(ref: "specs/100-demo/plan.md") { path title } }`

func TestGraphQLReadCacheServesARepeatAtOneGenerationAndRereadsAfterAWrite(t *testing.T) {
	srv, fixture := newGraphQLReadCacheServer(t)
	hash := readQueryHash("graphql", planQuery, "")
	payload := map[string]any{"query": planQuery}

	first := postGraphQL(t, srv, payload)
	require.Equal(t, http.StatusOK, first.status, string(first.body))
	second := postGraphQL(t, srv, payload)
	require.Equal(t, first.body, second.body)
	require.Equal(t, replyGeneration(t, first), replyGeneration(t, second))
	stats := srv.reads.Metrics().Queries[hash]
	require.EqualValues(t, 1, stats.Executions)
	require.EqualValues(t, 1, stats.Hits)

	// A write that changes nothing the query reads still moves the generation.
	_, err := fixture.intelStore.SetValidationRunning(context.Background())
	require.NoError(t, err)
	third := postGraphQL(t, srv, payload)
	require.NotEqual(t, replyGeneration(t, first), replyGeneration(t, third))
	require.Equal(t, first.decoded["data"], third.decoded["data"])
	require.EqualValues(t, 2, srv.reads.Metrics().Queries[hash].Executions)
}

// The panel and a page running one query cause one execution (SPEC-0136 US5).
func TestGraphQLReadCacheRunsConcurrentIdenticalReadsOnce(t *testing.T) {
	srv, _ := newGraphQLReadCacheServer(t)
	hash := readQueryHash("graphql", planQuery, "")
	var wg sync.WaitGroup
	for range 6 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			reply := postGraphQL(t, srv, map[string]any{"query": planQuery})
			require.Equal(t, http.StatusOK, reply.status, string(reply.body))
		}()
	}
	wg.Wait()
	require.EqualValues(t, 1, srv.reads.Metrics().Queries[hash].Executions)
}

func TestGraphQLReadCacheBypassesReadsTheGenerationDoesNotCover(t *testing.T) {
	cases := []struct {
		name    string
		payload map[string]any
	}{
		{"currentUser read", map[string]any{"query": `query { ontology { currentUser { title } } }`}},
		{"nextId read", map[string]any{"query": `query { ontology { nextId(type: "Spec") { ids } } }`}},
		// Embedding-only updates write through their own runtime and never move
		// the generation, so these must execute every time.
		{"semantic read after an embedding-only update", map[string]any{"query": `query { notes(semantic: ["demo"]) { nodes { path } } }`}},
		{"search read after an embedding-only update", map[string]any{"query": `query { search(query: ["demo"]) { nodes { path } } }`}},
		{"edit session by ID only", map[string]any{"query": planQuery, "editSession": map[string]any{"id": "session-1"}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv, _ := newGraphQLReadCacheServer(t)
			query := tc.payload["query"].(string)
			for range 2 {
				postGraphQL(t, srv, tc.payload)
			}
			stats := srv.reads.Metrics()
			require.EqualValues(t, 2, stats.Queries[readQueryHash("graphql", query, "")].Bypasses)
			require.Zero(t, stats.Queries[readQueryHash("graphql", query, "")].Hits)
			require.Zero(t, stats.Entries)
		})
	}
}

// writingNoteReader commits a store write the first time a read consults the
// note source, which is a publish landing while the read executes.
type writingNoteReader struct {
	obsidian.NoteReader
	write func()
	once  sync.Once
	wrote atomic.Bool
}

func (r *writingNoteReader) GetModTime(vault obsidian.VaultDefinition, path string) (time.Time, error) {
	r.once.Do(func() {
		r.write()
		r.wrote.Store(true)
	})
	return r.NoteReader.GetModTime(vault, path)
}

func TestGraphQLReadCacheDoesNotStoreAReadThatAPublishOverlapped(t *testing.T) {
	srv, fixture := newGraphQLReadCacheServer(t)
	reader := &writingNoteReader{NoteReader: &obsidian.Note{}, write: func() {
		_, err := fixture.intelStore.SetValidationRunning(context.Background())
		require.NoError(t, err)
	}}
	srv.noteReader = reader
	hash := readQueryHash("graphql", planQuery, "")

	reply := postGraphQL(t, srv, map[string]any{"query": planQuery})
	require.Equal(t, http.StatusOK, reply.status, string(reply.body))
	require.True(t, reader.wrote.Load(), "the read consulted its source during execution")
	require.Zero(t, srv.reads.Metrics().Entries, "a read whose generation changed is not stored")
	postGraphQL(t, srv, map[string]any{"query": planQuery})
	require.EqualValues(t, 2, srv.reads.Metrics().Queries[hash].Executions)
}

// Every Query root and every `ontology` field must be classified: covered by
// the generation, or bypassed. A new root fails here until someone decides.
func TestGraphQLReadCacheClassifiesEveryQueryRoot(t *testing.T) {
	srv, _ := newGraphQLReadCacheServer(t)
	defs, err := srv.ontologyDefinitions()
	require.NoError(t, err)
	require.NotNil(t, defs.exec)
	schema := defs.exec.Schema
	for _, field := range schema.Query.Fields {
		if field.Name == "__schema" || field.Name == "__type" {
			continue
		}
		covered := graphQLCoveredRoots[field.Name] || defs.exec.RootTypes[field.Name] != ""
		require.True(t, covered != graphQLBypassRoots[field.Name], "root %q must be either covered or bypassed", field.Name)
	}
	runtime := schema.Types["OntologyRuntime"]
	require.NotNil(t, runtime)
	for _, field := range runtime.Fields {
		require.True(t, graphQLCoveredOntologyFields[field.Name] != graphQLCallerDependentFields[field.Name],
			"ontology field %q must be either covered or caller-dependent", field.Name)
	}
}
