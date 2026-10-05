package web

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"
)

func TestWorkspaceNavigationWaitsForChangedNoteCatalog(t *testing.T) {
	for _, mode := range []string{"full", "incremental"} {
		t.Run(mode, func(t *testing.T) {
			fixture := prepareOntologyFixtureVault(t)
			runtime := &Runtime{IntelStore: fixture.intelStore}
			runtime.EnableIndexGate()
			runtime.MarkIndexReady()
			srv := newFixtureServer(t, fixture, runtime)
			srv.cfg.NotePathOwned = func(string) bool { return true }
			path := "specs/100-demo/spec.md"
			content, err := os.ReadFile(filepath.Join(fixture.root, path))
			require.NoError(t, err)
			require.NoError(t, os.WriteFile(filepath.Join(fixture.root, path), []byte(strings.ReplaceAll(string(content), "^validation", "^renamed")), 0o644))
			indexer := testNoteMetadataIndexer(t)
			require.NoError(t, indexer.SyncPaths(context.Background(), fixture.vaultDef, &obsidian.Note{}, fixture.intelStore, []string{path}, nil))
			timeout := 100 * time.Millisecond
			query := func(ref string) *httptest.ResponseRecorder {
				t.Helper()
				body, err := json.Marshal(map[string]string{"query": `query { node(ref: "` + ref + `") { path resolvedType workspace { version } } }`})
				require.NoError(t, err)
				ctx, cancel := context.WithTimeout(context.Background(), timeout)
				defer cancel()
				req := newApplicationRequest(http.MethodPost, "/api/v1/graphql", bytes.NewReader(body)).WithContext(ctx)
				req.Header.Set("Content-Type", "application/json")
				res := httptest.NewRecorder()
				srv.Handler().ServeHTTP(res, req)
				return res
			}
			res := query(path + "#^renamed")
			require.Equal(t, http.StatusServiceUnavailable, res.Code, res.Body.String())
			require.Contains(t, res.Body.String(), "INDEX_INITIALIZING")
			if mode == "full" {
				_, err = ontology.EnsureFreshRuntimeWithStore(context.Background(), indexer, fixture.vaultDef, &obsidian.Note{}, fixture.intelStore)
			} else {
				_, err = ontology.SyncPublishedPaths(context.Background(), indexer, fixture.vaultDef, &obsidian.Note{}, fixture.intelStore, nil, []string{path}, nil)
			}
			require.NoError(t, err)
			timeout = 5 * time.Second
			res = query(path + "#^renamed")
			require.Equal(t, http.StatusOK, res.Code, res.Body.String())
			require.Contains(t, res.Body.String(), `"resolvedType":"Story"`)
			require.NotContains(t, res.Body.String(), `"errors"`)
			res = query(path + "#^missing")
			require.Equal(t, http.StatusOK, res.Code, "a genuinely missing fragment must not wait for publication")
			require.Contains(t, res.Body.String(), `"node":null`)
		})
	}
}

func TestWorkspaceNavigationReloadsPublishedSchema(t *testing.T) {
	fixture := prepareOntologyFixtureVault(t)
	runtime := &Runtime{IntelStore: fixture.intelStore}
	runtime.EnableIndexGate()
	runtime.MarkIndexReady()
	srv := newFixtureServer(t, fixture, runtime)
	srv.cfg.NotePathOwned = func(string) bool { return true }

	query := func() *httptest.ResponseRecorder {
		t.Helper()
		ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
		defer cancel()
		req := newApplicationRequest(http.MethodPost, "/api/v1/graphql", strings.NewReader(`{"query":"{ node(ref: \"specs/100-demo/plan.md\") { resolvedType workspace { version } } }"}`)).WithContext(ctx)
		req.Header.Set("Content-Type", "application/json")
		res := httptest.NewRecorder()
		srv.Handler().ServeHTTP(res, req)
		return res
	}
	before := query()
	require.Equal(t, http.StatusOK, before.Code, before.Body.String())
	require.Contains(t, before.Body.String(), `"resolvedType":"Plan"`)

	schemaPath := filepath.Join(fixture.root, ".rhizome/ontology/schema.graphql")
	schema, err := os.ReadFile(schemaPath)
	require.NoError(t, err)
	updated := strings.NewReplacer("type Plan\n", "type UpdatedPlan\n", "[Plan!]", "[UpdatedPlan!]", "plan: Plan!", "plan: UpdatedPlan!").Replace(string(schema))
	require.NotEqual(t, string(schema), updated)
	require.NoError(t, os.WriteFile(schemaPath, []byte(updated), 0o644))
	_, err = ontology.EnsureFreshRuntimeWithStore(context.Background(), testNoteMetadataIndexer(t), fixture.vaultDef, &obsidian.Note{}, fixture.intelStore)
	require.NoError(t, err)

	res := query()
	require.Equal(t, http.StatusOK, res.Code, res.Body.String())
	require.Contains(t, res.Body.String(), `"resolvedType":"UpdatedPlan"`)
	require.NotContains(t, res.Body.String(), `"errors"`)
}

func TestWorkspaceNavigationWaitsForChangedNoteType(t *testing.T) {
	fixture := prepareOntologyFixtureVault(t)
	schemaPath := filepath.Join(fixture.root, ".rhizome/ontology/schema.graphql")
	schema, err := os.ReadFile(schemaPath)
	require.NoError(t, err)
	schema = append(schema, []byte(`
type Before @node(matches: ["tag:before"]) { summary: String }
type After @node(matches: ["tag:after"]) { summary: String }
`)...)
	require.NoError(t, os.WriteFile(schemaPath, schema, 0o644))
	path := "specs/100-demo/changing.md"
	write := func(tag string) {
		t.Helper()
		require.NoError(t, os.WriteFile(filepath.Join(fixture.root, path), []byte("---\ntags: ["+tag+"]\n---\n# Changing\n"), 0o644))
	}
	write("before")
	indexer := testNoteMetadataIndexer(t)
	_, err = ontology.EnsureFreshRuntimeWithStore(context.Background(), indexer, fixture.vaultDef, &obsidian.Note{}, fixture.intelStore)
	require.NoError(t, err)
	runtime := &Runtime{IntelStore: fixture.intelStore}
	runtime.EnableIndexGate()
	runtime.MarkIndexReady()
	srv := newFixtureServer(t, fixture, runtime)
	srv.cfg.NotePathOwned = func(string) bool { return true }
	write("after")
	require.NoError(t, indexer.SyncPaths(context.Background(), fixture.vaultDef, &obsidian.Note{}, fixture.intelStore, []string{path}, nil))
	timeout := 100 * time.Millisecond
	query := func() *httptest.ResponseRecorder {
		ctx, cancel := context.WithTimeout(context.Background(), timeout)
		defer cancel()
		req := newApplicationRequest(http.MethodPost, "/api/v1/graphql", strings.NewReader(`{"query":"{ node(ref: \"specs/100-demo/changing.md\") { resolvedType workspace { version } } }"}`)).WithContext(ctx)
		req.Header.Set("Content-Type", "application/json")
		res := httptest.NewRecorder()
		srv.Handler().ServeHTTP(res, req)
		return res
	}
	res := query()
	require.Equal(t, http.StatusServiceUnavailable, res.Code, res.Body.String())
	_, err = ontology.SyncPublishedPaths(context.Background(), indexer, fixture.vaultDef, &obsidian.Note{}, fixture.intelStore, nil, []string{path}, nil)
	require.NoError(t, err)
	timeout = 5 * time.Second
	res = query()
	require.Equal(t, http.StatusOK, res.Code, res.Body.String())
	require.Contains(t, res.Body.String(), `"resolvedType":"After"`)
}

func TestWorkspaceNavigationWaitsForNewNotePublication(t *testing.T) {
	fixture := prepareOntologyFixtureVault(t)
	runtime := &Runtime{IntelStore: fixture.intelStore}
	runtime.EnableIndexGate()
	runtime.MarkIndexReady()
	srv := newFixtureServer(t, fixture, runtime)
	srv.cfg.NotePathOwned = func(string) bool { return true }
	checks := make(chan struct{}, 1)
	srv.noteReader = navigationReadObserver{NoteReader: &obsidian.Note{}, checks: checks}
	require.NoError(t, os.WriteFile(filepath.Join(fixture.root, "specs/100-demo/late.md"), []byte("# Late\n"), 0o644))
	body := `{"query":"query { node(ref: \"specs/100-demo/late.md\") { path title workspace { version } } }"}`
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	req := newApplicationRequest(http.MethodPost, "/api/v1/graphql", bytes.NewBufferString(body)).WithContext(ctx)
	req.Header.Set("Content-Type", "application/json")
	res := httptest.NewRecorder()
	done := make(chan struct{})
	go func() { srv.Handler().ServeHTTP(res, req); close(done) }()
	assertPending := func() {
		t.Helper()
		select {
		case <-done:
			t.Fatalf("navigation returned before publication: %s", res.Body.String())
		case <-checks:
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		}
	}
	assertPending()
	_, err := testNoteMetadataIndexer(t).EnsureIndexed(ctx, fixture.vaultDef, &obsidian.Note{}, fixture.intelStore)
	require.NoError(t, err)
	assertPending()
	_, err = ontology.EnsureFreshRuntimeWithStore(ctx, testNoteMetadataIndexer(t), fixture.vaultDef, &obsidian.Note{}, fixture.intelStore)
	require.NoError(t, err)
	select {
	case <-done:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	require.Equal(t, http.StatusOK, res.Code)
	var response map[string]any
	require.NoError(t, json.Unmarshal(res.Body.Bytes(), &response))
	require.NotContains(t, response, "errors")
	node := response["data"].(map[string]any)["node"].(map[string]any)
	require.Equal(t, "Late", node["title"])
	require.NotEmpty(t, node["workspace"])
}

// Observe the real source-existence check so publication cannot race ahead of
// the request reaching admission, even under a heavily loaded test runner.
type navigationReadObserver struct {
	obsidian.NoteReader
	checks chan struct{}
}

func (r navigationReadObserver) GetModTime(vault obsidian.VaultDefinition, path string) (time.Time, error) {
	modified, err := r.NoteReader.GetModTime(vault, path)
	select {
	case r.checks <- struct{}{}:
	default:
	}
	return modified, err
}

// The index keeps a deleted note's rows until the watcher publishes the
// deletion. A read in that window must report the node as missing instead of
// serving the stale, body-less indexed record.
func TestNodeReadReportsDeletedSourceBeforeIndexCatchesUp(t *testing.T) {
	fixture := prepareOntologyFixtureVault(t)
	runtime := &Runtime{IntelStore: fixture.intelStore}
	runtime.EnableIndexGate()
	runtime.MarkIndexReady()
	srv := newFixtureServer(t, fixture, runtime)
	srv.cfg.NotePathOwned = func(string) bool { return true }
	query := func(root string) map[string]any {
		t.Helper()
		body, err := json.Marshal(map[string]string{"query": `query { ` + root + ` { path title } }`})
		require.NoError(t, err)
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		req := newApplicationRequest(http.MethodPost, "/api/v1/graphql", bytes.NewReader(body)).WithContext(ctx)
		req.Header.Set("Content-Type", "application/json")
		res := httptest.NewRecorder()
		srv.Handler().ServeHTTP(res, req)
		require.Equal(t, http.StatusOK, res.Code, res.Body.String())
		var response map[string]any
		require.NoError(t, json.Unmarshal(res.Body.Bytes(), &response))
		require.NotContains(t, response, "errors", res.Body.String())
		return response["data"].(map[string]any)
	}
	path := "specs/100-demo/plan.md"
	require.NotNil(t, query(`node(ref: "` + path + `")`)["node"])

	require.NoError(t, os.Remove(filepath.Join(fixture.root, path)))

	require.Nil(t, query(`node(ref: "` + path + `")`)["node"])
	require.Nil(t, query(`note(path: "` + path + `")`)["note"])
}
