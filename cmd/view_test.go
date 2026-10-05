package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	appviews "github.com/atomicobject/rhizome/pkg/app/views"
	"github.com/stretchr/testify/require"
)

func TestViewListJSON(t *testing.T) {
	ctx := viewCommandTestContext(t)
	path := writeViewCommandConfig(t, minimalViewCommandYAML("specs"))
	for _, tc := range []struct {
		name  string
		agent bool
		args  []string
	}{
		{"human json flag", false, []string{"list", "--path", path, "--json"}},
		{"agent json default", true, []string{"list", "--path", path}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cmd := newViewCmd(tc.agent)
			var out bytes.Buffer
			cmd.SetOut(&out)
			cmd.SetArgs(tc.args)
			require.NoError(t, cmd.ExecuteContext(ctx))
			var resp appviews.Catalog
			require.NoError(t, json.Unmarshal(out.Bytes(), &resp))
			requireViewCommandID(t, resp.Views, "specs")
		})
	}
}

func TestViewShowJSON(t *testing.T) {
	ctx := viewCommandTestContext(t)
	path := writeViewCommandConfig(t, minimalViewCommandYAML("specs"))
	cmd := newViewCmd(false)
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetArgs([]string{"show", "specs", "--path", path, "--json"})

	require.NoError(t, cmd.ExecuteContext(ctx))

	var entry appviews.CatalogEntry
	require.NoError(t, json.Unmarshal(out.Bytes(), &entry))
	require.Equal(t, "specs", entry.ID)
	require.Equal(t, "Specs", entry.Name)
	require.Equal(t, []string{"table"}, entry.AvailableVariants)
}

func TestViewShowHumanListsAvailableVariants(t *testing.T) {
	ctx := viewCommandTestContext(t)
	path := writeViewCommandConfig(t, `apiVersion: rhizome.view.v1
id: delivery
name: Delivery
source:
  kind: ontology_type
  type: ProductSpec
mount:
  kind: standalone
variants:
  table:
    columns:
      - field: title
  card: {}
  kanban:
    columnField: status
`)
	cmd := newViewCmd(false)
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetArgs([]string{"show", "delivery", "--path", path})

	require.NoError(t, cmd.ExecuteContext(ctx))
	require.Contains(t, out.String(), "Available variants: table, kanban, card")
}

func TestViewValidateReturnsNonZeroForIssues(t *testing.T) {
	ctx := viewCommandTestContext(t)
	path := writeViewCommandConfig(t, `apiVersion: rhizome.view.v1
id: bad
name: Bad
source:
  kind: ontology_type
  type: ProductSpec
mount:
  kind: standalone
`)
	cmd := newViewCmd(false)
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetArgs([]string{"validate", "--path", path, "--json"})

	err := cmd.ExecuteContext(ctx)
	require.Error(t, err)
	require.IsType(t, silentExitError{}, err)
	require.Contains(t, out.String(), `"issueCount":1`)
}

func TestViewValidateReportsExplicitPathLoadIssues(t *testing.T) {
	ctx := viewCommandTestContext(t)
	path := writeViewCommandConfig(t, "apiVersion: [")
	cmd := newViewCmd(false)
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetArgs([]string{"validate", "--path", path, "--json"})

	err := cmd.ExecuteContext(ctx)
	require.Error(t, err)
	require.Contains(t, out.String(), "view_yaml_decode_error")
}

func TestViewRunJSON(t *testing.T) {
	vaultRoot := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(vaultRoot, ".rhizome"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(vaultRoot, ".rhizome", "config.yml"), []byte("notes: {}\n"), 0o644))
	path := writeViewCommandConfig(t, minimalViewCommandYAML("specs")+"defaults:\n  page:\n    offset: 7\n    first: 11\n")

	ctx := contextWithCommandEnv(context.Background(), commandEnv{
		Getwd: func() (string, error) { return vaultRoot, nil },
	})
	for _, tc := range []struct {
		name   string
		flags  []string
		offset int
		first  int
	}{
		{"default page", nil, 7, 11},
		{"explicit first", []string{"--first", "20"}, 0, 20},
		{"explicit zero offset", []string{"--offset", "0"}, 0, 11},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cmd := newViewCmd(false)
			var out bytes.Buffer
			cmd.SetOut(&out)
			cmd.SetArgs(append([]string{"run", "--id", "specs", "--path", path}, tc.flags...))
			require.NoError(t, cmd.ExecuteContext(ctx))
			var resp appviews.ExecuteResponse
			require.NoError(t, json.Unmarshal(out.Bytes(), &resp))
			require.Equal(t, "specs", resp.View.ID)
			require.Equal(t, appviews.PageRequest{Offset: tc.offset, First: tc.first}, resp.State.Page)
			require.Equal(t, tc.first, resp.PageInfo.First)
			require.Empty(t, resp.Rows)
			require.Len(t, resp.Warnings, 1)
			require.Equal(t, "view_source_unavailable", resp.Warnings[0].Code)
		})
	}
}

func TestAgentSurfaceIncludesViewCommand(t *testing.T) {
	surface := buildAgentSurface()
	for _, command := range surface.Commands {
		if command.Name == "view" {
			require.Empty(t, command.ToolName)
			require.Equal(t, "ontology", command.Category)
			return
		}
	}
	t.Fatalf("view command not found in agent surface")
}

func minimalViewCommandYAML(id string) string {
	return `apiVersion: rhizome.view.v1
id: ` + id + `
name: Specs
source:
  kind: ontology_type
  type: ProductSpec
mount:
  kind: standalone
variants:
  table:
    columns:
      - field: title
`
}

func writeViewCommandConfig(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "view.yaml")
	require.NoError(t, os.WriteFile(path, []byte(body), 0o644))
	return path
}

func viewCommandTestContext(t *testing.T) context.Context {
	t.Helper()
	vaultRoot := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(vaultRoot, ".rhizome"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(vaultRoot, ".rhizome", "config.yml"), []byte("notes: {}\n"), 0o644))
	return contextWithCommandEnv(context.Background(), commandEnv{
		Getwd: func() (string, error) { return vaultRoot, nil },
	})
}

func requireViewCommandID(t *testing.T, views []appviews.CatalogEntry, id string) {
	t.Helper()
	for _, view := range views {
		if view.ID == id {
			return
		}
	}
	t.Fatalf("view id %q not found in %#v", id, views)
}

func TestViewRunResolvesHTMLSectionBacklinks(t *testing.T) {
	vault := setupAgentTestVault(t, map[string]string{
		".rhizome/config.yml": "notes:\n  includes: [\"**/*.md\", \"**/*.html\"]\n",
		".rhizome/ontology/schema.graphql": `
type EffortWorkspace @node(paths: ["docs/efforts/*.html"]) { summary: String }
type StorySection implements Section {
  efforts: [EffortWorkspace!] @neighbors(direction: INBOUND, type: "EffortWorkspace", scope: SUBTREE)
}
type Spec @node(paths: ["docs/spec.md"]) {
  story: StorySection @contains(level: H2, heading: "Story")
}
`,
		"docs/spec.md":           "# Spec\n## Story\n\n^story-one\n\nA selected story\n",
		"docs/efforts/work.html": `<html><head><title>HTML effort</title><script id="rhizome-metadata" type="application/json">{"summary":"Linked effort"}</script></head><body><a href="../spec.md#^story-one">Selected story</a></body></html>`,
		".rhizome/query-recipes/efforts.yaml": `apiVersion: rhizome.query-recipe.v1
id: story-efforts
name: Story efforts
problem: Find efforts linking to a story.
inputSpec:
  mode: none
query:
  graphQL: '{ spec { path title story { efforts { path title } } } }'
outputContract:
  rowPath: spec
  empty: No linked efforts.
adaptationGuidance:
  summary: Select a story.
`,
		".rhizome/views/efforts.yaml": `apiVersion: rhizome.view.v1
id: story-efforts
name: Story efforts
source:
  kind: query_recipe
  queryRecipe: story-efforts
mount:
  kind: standalone
variants:
  table:
    columns:
      - field: title
`,
	})
	t.Chdir(vault.path)
	_, stderr, err := runRootCLI(t, nil, []string{"index"})
	require.NoError(t, err, stderr)
	vaultName = ""
	ctx := contextWithCommandEnv(context.Background(), commandEnv{
		Getwd: func() (string, error) { return vault.path, nil },
	})
	for _, agent := range []bool{false, true} {
		command := newViewCmd(agent)
		var out bytes.Buffer
		command.SetOut(&out)
		command.SetArgs([]string{"run", "--id", "story-efforts"})
		require.NoError(t, command.ExecuteContext(ctx))
		stdout := out.String()
		var response appviews.ExecuteResponse
		require.NoError(t, json.Unmarshal([]byte(stdout), &response))
		require.Empty(t, response.Warnings)
		require.Len(t, response.Rows, 1)
		story := response.Rows[0].Fields["story"].(map[string]any)
		efforts := story["efforts"].([]any)
		require.Len(t, efforts, 1)
		require.Equal(t, "docs/efforts/work.html", efforts[0].(map[string]any)["path"])
	}
}
