//go:build integration

package integration

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/atomicobject/rhizome/pkg/app/agentcode"
	"github.com/stretchr/testify/require"
)

func TestPersistentRecipeRunSelectsOnlyRecipeWithoutID(t *testing.T) {
	node := nodePathOrSkip(t)
	fixture := newProgressiveCodeFixture(t)
	binary := buildProgressiveBinary(t)
	ontology := filepath.Join(fixture.vault, ".rhizome", "ontology")
	require.NoError(t, os.MkdirAll(ontology, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(ontology, "test.graphql"), []byte(`type FixtureNote @node(paths: ["notes/**/*.md"]) { title: String @field(source: "title") }`), 0o644))
	recipe := filepath.Join(fixture.vault, "only-recipe.yml")
	require.NoError(t, os.WriteFile(recipe, []byte(`apiVersion: rhizome.query-recipe.v1
id: only-recipe
name: Only recipe
problem: Inspect the query root.
inputSpec:
  mode: none
  inputs:
    - name: list
      kind: string
      required: true
    - name: object
      kind: string
      required: true
query:
  graphQL: 'query($list: String!, $object: String!) { a: fixtureNote(path: $list) { title } b: fixtureNote(path: $object) { title } }'
outputContract:
  empty: No root returned.
adaptationGuidance:
  summary: Keep the query narrow.
`), 0o644))
	direct := runProgressiveCommand(t, binary, fixture.vault, "agent", "query-recipe", "run", "--path", recipe, "--inputs-json", `{"list":["a","b"],"object":{"enabled":true}}`)
	require.NoError(t, direct.err, "stdout=%s stderr=%s", direct.stdout, direct.stderr)
	var expected any
	require.NoError(t, json.Unmarshal([]byte(direct.stdout), &expected))
	generated := runProgressiveCommand(t, binary, fixture.vault, "agent", "code", "generate", "--operation", "query_recipe", "--output", t.TempDir())
	require.NoError(t, generated.err, "%s", generated.stderr)
	var manifest agentcode.Manifest
	require.NoError(t, json.Unmarshal([]byte(generated.stdout), &manifest))
	script := filepath.Join(t.TempDir(), "recipe.mjs")
	require.NoError(t, os.WriteFile(script, []byte(`const {createClient} = await import(process.env.PROGRESSIVE_MODULE);
const client = createClient({executablePath:process.env.PROGRESSIVE_BINARY,vaultPath:process.env.PROGRESSIVE_VAULT});
try { console.log(JSON.stringify(await client.queryRecipe({op:"run",path:process.env.RECIPE_PATH,inputs:{list:["a","b"],object:{enabled:true}}}))); } finally { await client.close(); }
`), 0o644))
	result := runNode(t, node, fixture.vault, map[string]string{"PROGRESSIVE_MODULE": manifest.ModulePath, "PROGRESSIVE_BINARY": binary, "PROGRESSIVE_VAULT": fixture.vault, "RECIPE_PATH": recipe}, script)
	require.NoError(t, result.err, "%s", result.stderr)
	var outcome struct {
		OK      bool `json:"ok"`
		Payload any  `json:"payload"`
	}
	require.NoError(t, json.Unmarshal([]byte(result.stdout), &outcome))
	require.True(t, outcome.OK, "%s", result.stdout)
	require.Equal(t, expected, outcome.Payload)

	// CLI parity alone would accept a shared wrong conversion, so check selection and inputs against literals.
	var run struct {
		Recipe struct {
			ID string `json:"id"`
		} `json:"recipe"`
		Inputs    map[string]string `json:"inputs"`
		Variables map[string]any    `json:"variables"`
	}
	require.NoError(t, json.Unmarshal([]byte(result.stdout), &struct {
		Payload any `json:"payload"`
	}{Payload: &run}))
	require.Equal(t, "only-recipe", run.Recipe.ID)
	require.Len(t, run.Inputs, 2)
	require.JSONEq(t, `["a","b"]`, run.Inputs["list"])
	require.JSONEq(t, `{"enabled":true}`, run.Inputs["object"])
	require.Len(t, run.Variables, 2)
	require.IsType(t, "", run.Variables["list"])
	require.IsType(t, "", run.Variables["object"])
	require.JSONEq(t, `["a","b"]`, run.Variables["list"].(string))
	require.JSONEq(t, `{"enabled":true}`, run.Variables["object"].(string))
}
