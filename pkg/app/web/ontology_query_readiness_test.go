package web

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/atomicobject/rhizome/internal/testkit"
	ontologytest "github.com/atomicobject/rhizome/internal/testkit/ontology"
	ontologyquery "github.com/atomicobject/rhizome/pkg/ontology/query"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"
)

func TestPublicGraphQLRejectsUnpublishedSchema(t *testing.T) {
	srv, fixture := newGraphQLReadCacheServer(t)
	before := postGraphQL(t, srv, map[string]any{"query": `{ plan(first: 2) { path } }`})
	require.Equal(t, http.StatusOK, before.status, string(before.body))
	require.Contains(t, string(before.body), "specs/100-demo/plan.md")

	renameQueryFixturePlan(t, fixture)
	const query = `{ updatedPlan(first: 2) { path } }`
	payload := map[string]any{"query": query}
	pending := postGraphQL(t, srv, payload)
	require.Equal(t, http.StatusServiceUnavailable, pending.status, string(pending.body))
	require.Contains(t, string(pending.body), PublicErrorIndexInitializing)

	raw := postGraphQL(t, srv, map[string]any{"query": planQuery})
	require.Equal(t, http.StatusOK, raw.status, string(raw.body))
	require.Contains(t, string(raw.body), "specs/100-demo/plan.md")

	_, err := ontologytest.Publish(t.Context(), testkit.NoteMetadataIndexer(t), fixture.vaultDef, &obsidian.Note{}, fixture.intelStore)
	require.NoError(t, err)
	after := postGraphQL(t, srv, payload)
	require.Equal(t, http.StatusOK, after.status, string(after.body))
	require.NotContains(t, after.decoded, "errors")
	require.Contains(t, string(after.body), "specs/100-demo/plan.md")
	repeated := postGraphQL(t, srv, payload)
	require.Equal(t, after.body, repeated.body)
	require.EqualValues(t, 1, srv.reads.Metrics().Queries[readQueryHash("graphql", query, "")].Hits)
}

func TestPreparedGraphQLRejectsSchemaChangedBeforeAdmission(t *testing.T) {
	srv, fixture := newGraphQLReadCacheServer(t)
	defs, err := srv.ontologyDefinitions()
	require.NoError(t, err)
	prepared, errs := ontologyquery.Prepare(defs.exec, `{ plan(first: 2) { path } }`)
	require.Empty(t, errs)

	renameQueryFixturePlan(t, fixture)
	_, err = ontologytest.Publish(t.Context(), testkit.NoteMetadataIndexer(t), fixture.vaultDef, &obsidian.Note{}, fixture.intelStore)
	require.NoError(t, err)
	_, err = srv.ontologyQueryDepsForPrepared(t.Context(), defs, prepared)
	require.ErrorIs(t, err, errIndexInitializing)
}

func TestPublicGraphQLRejectsSchemaChangedDuringSelectedPathPolling(t *testing.T) {
	srv, fixture := newGraphQLReadCacheServer(t)
	path := "specs/100-demo/late.md"
	require.NoError(t, os.WriteFile(filepath.Join(fixture.root, path), []byte("# Late\n"), 0o644))
	var change sync.Once
	srv.noteReader = schemaChangeReadObserver{
		NoteReader: &obsidian.Note{},
		beforeStat: func() {
			change.Do(func() {
				renameQueryFixturePlan(t, fixture)
				_, err := ontologytest.Publish(t.Context(), testkit.NoteMetadataIndexer(t), fixture.vaultDef, &obsidian.Note{}, fixture.intelStore)
				require.NoError(t, err)
			})
		},
	}
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	req := newApplicationRequest(http.MethodPost, "/api/v1/graphql", strings.NewReader(`{"query":"{ node(ref: \"specs/100-demo/late.md\") { __typename path } }"}`)).WithContext(ctx)
	req.Header.Set("Content-Type", "application/json")
	res := httptest.NewRecorder()
	srv.Handler().ServeHTTP(res, req)
	require.Equal(t, http.StatusServiceUnavailable, res.Code, res.Body.String())
	require.Contains(t, res.Body.String(), PublicErrorIndexInitializing)

	after := postGraphQL(t, srv, map[string]any{"query": `{ node(ref: "specs/100-demo/late.md") { __typename path } }`})
	require.Equal(t, http.StatusOK, after.status, string(after.body))
	require.NotContains(t, after.decoded, "errors")
	require.Contains(t, string(after.body), path)
}

func renameQueryFixturePlan(t *testing.T, fixture fixtureVault) {
	t.Helper()
	path := filepath.Join(fixture.root, ".rhizome/ontology/schema.graphql")
	schema, err := os.ReadFile(path)
	require.NoError(t, err)
	updated := strings.NewReplacer("type Plan\n", "type UpdatedPlan\n", "[Plan!]", "[UpdatedPlan!]", "plan: Plan!", "plan: UpdatedPlan!").Replace(string(schema))
	require.NotEqual(t, string(schema), updated)
	require.NoError(t, os.WriteFile(path, []byte(updated), 0o644))
}

type schemaChangeReadObserver struct {
	obsidian.NoteReader
	beforeStat func()
}

func (r schemaChangeReadObserver) GetModTime(vault obsidian.VaultDefinition, path string) (time.Time, error) {
	r.beforeStat()
	return r.NoteReader.GetModTime(vault, path)
}
