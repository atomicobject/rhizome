package init

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/atomicobject/rhizome/internal/testkit"
	"github.com/atomicobject/rhizome/pkg/noteformat/builtin"
	ontologyquery "github.com/atomicobject/rhizome/pkg/ontology/query"
	"github.com/atomicobject/rhizome/pkg/ontology/queryrecipe"
	vaultconfig "github.com/atomicobject/rhizome/pkg/vault/config"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"
)

func TestAgenticEngineeringFrozenScopePublicRecipes(t *testing.T) {
	env := testkit.Env(t)
	env.LookupEnv = os.LookupEnv
	root := t.TempDir()
	opts := RunOptions{Env: env, Dir: root, Workflow: templateAgenticEngineering, Stdout: io.Discard, Stderr: io.Discard}
	require.NoError(t, Run(opts))
	const folder = "docs/efforts/selection/"
	const spec = "docs/specs/technical/selected.md"
	writeQueryRecipeExperienceNote(t, root, spec, `---
type: TechnicalSpec
id: SPEC-0001
summary: Scope fixture
spec-status: active
---
# Scope fixture

## User Stories

### Story A
id:: ^story-a
summary:: Selected A
status:: ready

#### Acceptance Criteria
- Criterion A ^criterion-a

### Story B
id:: ^story-b
summary:: Entry mention B
status:: ready

#### Acceptance Criteria
- Criterion B

### Story C
id:: ^story-c
summary:: Log snapshot C
status:: ready

#### Acceptance Criteria
- Criterion C
`)
	scope := "# Selected scope\n\n## Spec Set (Frozen)\n\n[[" + spec + "]]\n\n## Stories In Scope (Frozen)\n\n[[" + spec + "#^story-a]]\n"
	writeQueryRecipeExperienceNote(t, root, folder+"materials/scope.md", scope)
	writeQueryRecipeExperienceNote(t, root, folder+"materials/criterion.md", strings.ReplaceAll(scope, "#^story-a", "#^criterion-a"))
	writeQueryRecipeExperienceNote(t, root, folder+"materials/duplicate.md", scope+"\n## Stories In Scope (Frozen)\n\n[["+spec+"#^story-b]]\n")
	writeQueryRecipeExperienceNote(t, root, folder+"materials/missing.md", "# Missing frozen sections\n")
	writeQueryRecipeExperienceNote(t, root, folder+"materials/unsupported.html", "<html><head><title>HTML scope</title></head><body><h2>Stories In Scope (Frozen)</h2></body></html>")
	writeQueryRecipeExperienceNote(t, root, folder+"work-log.md", strings.ReplaceAll(scope, "#^story-a", "#^story-c"))
	writeQueryRecipeExperienceNote(t, root, folder+"plan.html", "<html><head><title>Plan</title></head><body>Plan</body></html>")
	cases := []struct {
		name, selector, selected string
		sections                 int
	}{
		{"explicit", folder + "materials/scope.md", "story-a", 1},
		{"absent", "", "", 0},
		{"broken", folder + "materials/broken.md", "", 0},
		{"unsupported", folder + "materials/unsupported.html", "", 0},
		{"missing", folder + "materials/missing.md", "", 0},
		{"duplicate", folder + "materials/duplicate.md", "", 2},
		{"same-source", folder + "work-log.md", "story-c", 1},
		{"criterion-only", folder + "materials/criterion.md", "criterion-a", 1},
	}
	originals := map[string][]byte{}
	for i, tc := range cases {
		metadata := map[string]any{"type": "EffortWorkspace", "id": "EFF-2026-11-09-10-0" + string(rune('0'+i)), "name": tc.name, "created-at": "2026-11-09T15:00:00Z", "status": "planned", "summary": "Selection fixture", "implementation-plan": folder + "plan.html", "work-log": folder + "work-log.md", "governing-specs": []string{spec}}
		if tc.selector != "" {
			metadata["frozen-scope"] = tc.selector
		}
		body, err := json.Marshal(metadata)
		require.NoError(t, err)
		path := folder + tc.name + "-effort.html"
		source := `<html><head><title>` + tc.name + `</title><script id="rhizome-metadata" type="application/json">` + string(body) + `</script></head><body><a href="` + spec + `#^story-b">Entry mention B</a></body></html>`
		writeQueryRecipeExperienceNote(t, root, path, source)
		originals[path] = []byte(source)
	}
	writeQueryRecipeExperienceNote(t, root, folder+"materials/neighbor.md", scope+"\n[["+folder+"absent-effort.html]]\n")
	require.NoError(t, Run(opts))
	for path, before := range originals {
		after, err := os.ReadFile(filepath.Join(root, path))
		require.NoError(t, err)
		require.Equal(t, before, after)
	}
	_, schema, execSchema, store := indexAgenticEngineeringRecipeVault(t, root)
	deps := ontologyquery.Deps{VaultDef: obsidian.VaultDefinition{Root: root, Path: root, Includes: []string{"**/*.md", "**/*.html"}}, NoteReader: &obsidian.Note{}, Store: store}
	var err error
	deps.NoteFormats, err = builtin.NewRuntime()
	require.NoError(t, err)
	recipes, issues := queryrecipe.LoadPath(filepath.Join(root, ".rhizome/modules/agentic-engineering/recipes/spec-driven.yaml"))
	require.Empty(t, issues)
	prepared, queryErrors := ontologyquery.Prepare(execSchema, `{ node(ref: "`+spec+`#^story-a") { ... on UserStory { status efforts { id } } } }`)
	require.Empty(t, queryErrors)
	mentionResult := ontologyquery.Execute(vaultconfig.WithEnv(context.Background(), env), deps, schema, prepared)
	require.Empty(t, mentionResult.Errors)
	require.Equal(t, "ready", mentionResult.Data["node"].(map[string]any)["status"])
	require.Empty(t, mentionResult.Data["node"].(map[string]any)["efforts"], "material-selected A has no inbound Effort mention; this cannot prove absent frozen coverage")
	for _, recipe := range recipes {
		if recipe.ID != "effort-execution-context" && recipe.ID != "closure-drift-pack" {
			continue
		}
		for _, tc := range cases {
			t.Run(recipe.ID+"/"+tc.name, func(t *testing.T) {
				compiled, issues := queryrecipe.Compile(recipe, execSchema, map[string]string{"path": folder + tc.name + "-effort.html"})
				require.Empty(t, issues)
				result := ontologyquery.Execute(vaultconfig.WithEnv(context.Background(), env), deps, schema, compiled.Prepared)
				if tc.name == "duplicate" {
					require.Len(t, result.Errors, 1)
					require.Contains(t, result.Errors[0].Message, "must match a single heading")
				} else {
					require.Empty(t, result.Errors)
				}
				note := result.Data["note"].(map[string]any)
				require.Nil(t, note["planApprovedBy"])
				log := note["workLog"].(map[string]any)
				require.Equal(t, folder+"work-log.md", log["path"])
				logLinks := log["storiesInScopeFrozen"].(map[string]any)["workspace"].(map[string]any)["sourceLinks"].([]any)
				require.Len(t, logLinks, 1)
				require.Equal(t, spec+"#^story-c", logLinks[0].(map[string]any)["authoredTarget"])
				state := frozenScopeField(t, note, "frozenScope")
				require.Equal(t, tc.selector != "", state["present"])
				if tc.name == "absent" || tc.name == "broken" {
					require.Nil(t, note["frozenScope"])
					if tc.name == "broken" {
						require.Equal(t, []string{tc.selector}, state["values"])
						require.Equal(t, false, state["links"].([]any)[0].(map[string]any)["resolved"])
					}
					return
				}
				selected := note["frozenScope"].(map[string]any)
				require.Equal(t, tc.selector, selected["path"])
				sectionState := frozenScopeField(t, selected, "storiesInScopeFrozen")
				require.Len(t, sectionState["sectionNodes"], tc.sections)
				if tc.name == "duplicate" {
					require.Nil(t, selected["storiesInScopeFrozen"])
					return
				}
				if tc.selected == "" {
					require.Nil(t, selected["storiesInScopeFrozen"])
					return
				}
				section := selected["storiesInScopeFrozen"].(map[string]any)
				links := section["workspace"].(map[string]any)["sourceLinks"].([]any)
				require.Len(t, links, 1)
				link := links[0].(map[string]any)
				require.Equal(t, spec+"#^"+tc.selected, link["authoredTarget"])
				require.Equal(t, true, link["resolved"])
				ref := link["resolvedRef"].(map[string]any)
				wantType := "UserStory"
				if tc.name == "criterion-only" {
					wantType = "AcceptanceCriterion"
				}
				require.Equal(t, wantType, ref["typeName"])
				if tc.name == "same-source" {
					require.Equal(t, selected["ref"], note["workLog"].(map[string]any)["ref"])
				}
			})
		}
	}
}

func frozenScopeField(t *testing.T, node map[string]any, name string) map[string]any {
	t.Helper()
	for _, raw := range node["workspace"].(map[string]any)["fields"].([]any) {
		field := raw.(map[string]any)
		if field["name"] == name {
			return field
		}
	}
	t.Fatalf("missing field state %s", name)
	return nil
}
