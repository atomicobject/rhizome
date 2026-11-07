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

	"github.com/atomicobject/rhizome/internal/testkit"
	ontologytest "github.com/atomicobject/rhizome/internal/testkit/ontology"
	"github.com/atomicobject/rhizome/pkg/ontology"
	semdb "github.com/atomicobject/rhizome/pkg/store"
	vaultconfig "github.com/atomicobject/rhizome/pkg/vault/config"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/atomicobject/rhizome/pkg/vault/repoconfig"
	"github.com/stretchr/testify/require"
)

// Owned structured hosts must be refused with the ownership reason before any
// request-shape check (record projection, source witness, Markdown-only source
// validation) can answer with a planner error. The shapes are the ones worker P
// sent against AO model YAML in Batch 5.
func TestOwnedStructuredHostEditsAreRefusedWithOwnerReasonForEveryRequestShape(t *testing.T) {
	env := testkit.Env(t)
	env.LookupEnv = os.LookupEnv
	root := t.TempDir()
	write := func(path, body string) {
		t.Helper()
		require.NoError(t, os.MkdirAll(filepath.Dir(filepath.Join(root, path)), 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(root, path), []byte(body), 0o644))
	}
	write(".rhizome/ontology/schema.graphql", `
type Service @node(locator: EMBEDDED) @source(shape: RECORD, paths: ["model/services.yaml"], select: "services", key: {field: "id"}, title: ["name", "$key"], space: "selected", logicalKind: "service") {
  id: ID @field(path: "id")
  name: String @field(path: "name")
}
`)
	const services = "# Services\nservices:\n  - id: A\n    name: Alpha\n"
	write("model/services.yaml", services)
	write("note.md", "# Note\n")
	write(".rhizome/config.yml", "notes:\n  includes: [\"*.md\", \"model/*.yaml\"]\n")
	write(".rhizome/source-ownership.yml", "sources:\n  - owner: AO KB\n    reason: Managed by AO KB.\n    paths:\n      - model/**\n")

	store, err := semdb.Open(filepath.Join(root, ".rhizome", "db.sqlite"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })
	vaultDef := obsidian.VaultDefinition{Name: "owned-records", Root: root, Links: obsidian.LinkTypeBoth, Includes: []string{"*.md", "model/*.yaml"}}
	_, err = ontologytest.Publish(vaultconfig.WithEnv(context.Background(), env), testkit.NoteMetadataIndexer(t), vaultDef, &obsidian.Note{}, store)
	require.NoError(t, err)
	srv := newFixtureServer(t, fixtureVault{root: root, vault: &repoconfig.Vault{Env: env, Name: "owned-records"}, vaultDef: vaultDef, intelStore: store}, nil)
	httpSrv := httptest.NewServer(srv.Handler())
	defer httpSrv.Close()

	post := func(path string, body any) (int, map[string]any) {
		t.Helper()
		raw, err := json.Marshal(body)
		require.NoError(t, err)
		resp, err := http.Post(httpSrv.URL+path, "application/json", bytes.NewReader(raw))
		require.NoError(t, err)
		defer resp.Body.Close()
		out := map[string]any{}
		_ = json.NewDecoder(resp.Body).Decode(&out)
		return resp.StatusCode, out
	}
	status, query := post("/api/v1/graphql", map[string]any{"query": `{ service(first: 1) { ref { notePath fragment nodeId } } }`})
	require.Equal(t, http.StatusOK, status, query)
	rows, _ := query["data"].(map[string]any)["service"].([]any)
	require.NotEmpty(t, rows, query)
	ref := rows[0].(map[string]any)["ref"].(map[string]any)
	fragment, nodeID := ref["fragment"].(string), ref["nodeId"].(string)
	require.NotEmpty(t, fragment)

	snapshot, err := buildMarkdownDocumentSnapshotCompat("model/services.yaml", services, time.Time{})
	require.NoError(t, err)
	setSource := OntologyEditOp{Kind: "setSource", Path: "model/services.yaml", PreviousMarkdown: services,
		Markdown: strings.Replace(services, "# Services", "# Services edited", 1),
		Expected: &OntologyEditExpected{SourceHash: snapshot.ContentFingerprint, SourceContent: services}}
	shapes := map[string]OntologyEditOp{
		"record fragment setField": {Kind: "setField", Path: "model/services.yaml#" + strings.TrimPrefix(fragment, "#"), Field: "name", Value: "Edited"},
		"nodeId setField":          {Kind: "setField", Path: "model/services.yaml", NodeID: nodeID, Field: "name", Value: "Edited"},
		"record pointer setField":  {Kind: "setField", Path: "model/services.yaml#/services/0", Field: "name", Value: "Edited"},
		"witnessed setSource":      setSource,
		"unwitnessed setSource":    {Kind: "setSource", Path: "model/services.yaml", Markdown: "services: []\n"},
	}
	owner := "model/services.yaml is read-only (AO KB)"
	for name, op := range shapes {
		t.Run(name, func(t *testing.T) {
			status, created := post("/api/v1/edit-sessions", map[string]any{})
			require.Equal(t, http.StatusOK, status, created)
			status, staged := post("/api/v1/edit-sessions/"+created["sessionId"].(string)+"/stage", OntologyEditSessionStageRequest{Ops: []OntologyEditOp{op}})
			require.Equal(t, http.StatusBadRequest, status)
			require.Contains(t, staged["error"], owner, "stage")

			status, direct := post("/api/v1/edit-sessions", OntologyEditSessionCreateRequest{Ops: []OntologyEditOp{op}})
			require.Equal(t, http.StatusBadRequest, status)
			require.Contains(t, direct["error"], owner, "create with ops")
		})
	}
	t.Run("commit from client snapshot", func(t *testing.T) {
		status, restored := post("/api/v1/edit-sessions/sess-owned-restore/commit", OntologyEditSessionCommitRequest{Snapshot: &OntologyEditSessionSnapshot{
			Version: 3, SessionID: "sess-owned-restore", Ops: []OntologyEditOp{setSource},
			BaseDocuments: []ontology.EditBaseDocument{{NotePath: "model/services.yaml", Fingerprint: snapshot.ContentFingerprint, Content: services}},
		}})
		require.GreaterOrEqual(t, status, http.StatusBadRequest)
		require.Contains(t, restored["error"], owner)
	})

	after, err := os.ReadFile(filepath.Join(root, "model/services.yaml"))
	require.NoError(t, err)
	require.Equal(t, services, string(after))
}
