package init

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	ontologyquery "github.com/atomicobject/rhizome/pkg/ontology/query"
	"github.com/atomicobject/rhizome/pkg/ontology/queryrecipe"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"
)

func TestOpenEffortsRecipeIncludesBothFormsAndPagesOldestFirst(t *testing.T) {
	notes := map[string]string{}
	base := time.Date(2026, 9, 7, 10, 0, 0, 0, time.UTC)
	for i := range 101 {
		created := base.Add(time.Duration(i) * time.Minute)
		id := "EFF-" + created.Format("2006-01-02-15-04")
		body := agenticEngineeringEffortNote(id, fmt.Sprintf("Effort %03d", 101-i), "", "fixture")
		body = strings.Replace(body, "2026-09-07T10:00:00Z", created.Format(time.RFC3339), 1)
		notes["docs/efforts/"+created.Format("2006-01-02-15-04")+"-fixture.md"] = body
	}
	notes["docs/efforts/2026-09-01-00-00-folder/2026-09-01-00-00-effort.html"] = `<html><head><script id="rhizome-metadata" type="application/json">{"type":"EffortWorkspace","id":"EFF-2026-09-01-00-00","name":"Oldest workspace","summary":"Fixture","status":"active","created-at":"2026-09-01T00:00:00Z","governing-specs":[],"implementation-plan":"docs/efforts/plan.html","work-log":"docs/efforts/log.md"}</script></head><body>Fixture</body></html>`
	for i, status := range []string{"complete", "archived"} {
		id := fmt.Sprintf("EFF-2026-08-01-00-%02d", i)
		notes[fmt.Sprintf("docs/efforts/2026-08-01-00-%02d-closed.md", i)] = strings.Replace(agenticEngineeringEffortNote(id, status, "", "fixture"), "status: planned", "status: "+status, 1)
	}
	root, schema, executable, store := setupAgenticEngineeringRecipeVault(t, notes)
	recipes, issues := queryrecipe.LoadPath(filepath.Join(root, ".rhizome/query-recipes/spec-driven.yaml"))
	require.Empty(t, issues)
	var recipe queryrecipe.Recipe
	for _, candidate := range recipes {
		if candidate.ID == "open-efforts" {
			recipe = candidate
		}
	}
	require.Equal(t, "open-efforts", recipe.ID)
	deps := ontologyquery.Deps{VaultDef: obsidian.VaultDefinition{Root: root, Path: root, Includes: []string{"**/*.md", "**/*.html"}}, NoteReader: &obsidian.Note{}, Store: store}
	run := func(offset string) []any {
		t.Helper()
		compiled, issues := queryrecipe.Compile(recipe, executable, map[string]string{"offset": offset})
		require.Empty(t, issues)
		result := ontologyquery.ExecutePrepared(context.Background(), deps, schema, executable, compiled.Prepared)
		require.Empty(t, result.Errors)
		return result.Data["effort"].([]any)
	}
	first := run("0")
	require.Len(t, first, 100)
	require.Equal(t, "Oldest workspace", first[0].(map[string]any)["name"])
	require.Equal(t, "Effort 101", first[1].(map[string]any)["name"])
	last := run("100")
	require.Len(t, last, 2, "closed efforts must not appear on either page")
	require.Equal(t, "Effort 001", last[1].(map[string]any)["name"])
	for _, row := range append(first, last...) {
		require.NotEmpty(t, row.(map[string]any)["path"])
	}
}
