package init

import (
	"context"
	"encoding/json"
	"html"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/atomicobject/rhizome/pkg/noteformat/builtin"
	"github.com/atomicobject/rhizome/pkg/ontology"
	ontologyquery "github.com/atomicobject/rhizome/pkg/ontology/query"
	"github.com/atomicobject/rhizome/pkg/ontology/queryrecipe"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

func TestRun_CopiedEffortFormsAndHistoricalNoteRemainQueryable(t *testing.T) {
	root := t.TempDir()
	opts := RunOptions{Dir: root, Workflow: templateAgenticEngineering, Stdout: io.Discard, Stderr: io.Discard}
	require.NoError(t, Run(opts))
	const folder = "docs/efforts/smoke"
	const workspacePath = folder + "/2026-09-13-10-00-effort.html"
	const newNotePath = "docs/efforts/2026-09-13-10-01-new.md"
	const historicalPath = "docs/efforts/2026-09-12-10-00-historical.md"
	const specPath = "docs/specs/technical/governing"
	const title = `Smoke "quoted" & <tag> </script><script>alert(1)</script> {{summary_html}}`
	const summary = "Verify < & > text, quotes \" and backslash \\"
	jsonValue := func(value any) string {
		body, err := json.Marshal(value)
		require.NoError(t, err)
		return string(body)
	}
	replacements := strings.NewReplacer(
		"{{effort_id_json}}", jsonValue("EFF-2026-09-13-10-00"),
		"{{effort_title_json}}", jsonValue(title),
		"{{summary_json}}", jsonValue(summary),
		"{{created_at_iso_json}}", jsonValue("2026-09-13T14:00:00Z"),
		"{{implementation_plan_path_json}}", jsonValue(folder+"/plan.html"),
		"{{work_log_path_json}}", jsonValue(folder+"/work-log.md"),
		"{{governing_specs_json}}", "[]",
		"{{materials_json}}", jsonValue([]string{folder + "/materials/report.md"}),
		"{{effort_title_html}}", html.EscapeString(title),
		"{{summary_html}}", html.EscapeString(summary),
		"{{effort_id}}", "EFF-2026-09-13-10-00",
		"{{effort_title}}", "Smoke effort",
		"{{created_at_iso}}", "2026-09-13T14:00:00Z",
		"{{summary}}", "Verify copied effort assets",
		"{{effort_folder_vault_path}}", folder,
		"{{timestamp}}", "2026-09-13-10-00",
		`"{{governing_spec_vault_path}}"`, "",
		"{{selected_material_filename}}", "report.md",
	)
	for source, target := range map[string]string{
		"overview.html.template": workspacePath,
		"plan.html.template":     folder + "/plan.html",
		"work-log.md.template":   folder + "/work-log.md",
		"effort.css.template":    folder + "/effort.css",
	} {
		body, err := os.ReadFile(filepath.Join(root, "docs/efforts/templates/workspace", source))
		require.NoError(t, err)
		filled := replacements.Replace(string(body))
		require.NotContains(t, strings.ReplaceAll(filled, "{{summary_html}}", ""), "{{", source)
		if source == "overview.html.template" {
			require.Equal(t, 1, strings.Count(filled, "</script>"))
			require.Contains(t, filled, "<h1>"+html.EscapeString(title)+"</h1>")
			require.Contains(t, filled, "<p>"+html.EscapeString(summary)+"</p>")
		}
		if source == "plan.html.template" {
			require.Contains(t, filled, "<title>"+html.EscapeString(title)+": implementation plan</title>")
		}
		writeQueryRecipeExperienceNote(t, root, target, filled)
	}
	body, err := os.ReadFile(filepath.Join(root, "docs/efforts/templates/effort.md.template"))
	require.NoError(t, err)
	const markdownTitle = "Smoke \"quoted\" title with backslash \\ and\nsecond line"
	const markdownSummary = "Summary \"quoted\" with \\path\nsecond line"
	newNote := strings.NewReplacer(
		"{{effort_id_yaml}}", jsonValue("EFF-2026-09-13-10-01"),
		"{{effort_title_yaml}}", jsonValue(markdownTitle),
		"{{summary_yaml}}", jsonValue(markdownSummary),
		"{{created_at_iso_yaml}}", jsonValue("2026-09-13T14:00:00Z"),
		"{{governing_specs_yaml}}", jsonValue([]string{"[[" + specPath + "|SPEC-0001]]"}),
		"{{effort_title}}", "Smoke effort",
	).Replace(string(body))
	require.NotContains(t, newNote, "{{")
	require.Contains(t, newNote, `aliases: ["EFF-2026-09-13-10-01"]`)
	var frontmatter map[string]any
	require.NoError(t, yaml.Unmarshal([]byte(strings.SplitN(newNote, "---", 3)[1]), &frontmatter))
	require.Equal(t, []any{"EFF-2026-09-13-10-01"}, frontmatter["aliases"])
	writeQueryRecipeExperienceNote(t, root, newNotePath, newNote)
	writeQueryRecipeExperienceNote(t, root, specPath+".md", "---\ntype: TechnicalSpec\nid: SPEC-0001\nsummary: Governing spec\nspec-status: active\n---\n\n# Governing spec\n")
	writeQueryRecipeExperienceNote(t, root, folder+"/materials/report.md", "# Report\n\nSelected integration evidence.\n")
	historical := agenticEngineeringEffortNote("EFF-2026-09-12-10-00", "Historical effort", "", "historical")
	historical = strings.Replace(historical, "status: planned", "status: complete", 1)
	writeQueryRecipeExperienceNote(t, root, historicalPath, historical)
	require.NoError(t, Run(opts))
	preserved, err := os.ReadFile(filepath.Join(root, historicalPath))
	require.NoError(t, err)
	require.Equal(t, []byte(historical), preserved)

	_, schema, execSchema, store := indexAgenticEngineeringRecipeVault(t, root)
	vaultDef := obsidian.VaultDefinition{Root: root, Path: root, Includes: []string{"**/*.md", "**/*.html"}}
	deps := ontologyquery.Deps{VaultDef: vaultDef, NoteReader: &obsidian.Note{}, Store: store, Service: ontology.NewService(vaultDef, &obsidian.Note{}, store, schema)}
	deps.NoteFormats, err = builtin.NewRuntime()
	require.NoError(t, err)
	prepared, errors := ontologyquery.Prepare(execSchema, `{
  current: note(path: "`+newNotePath+`") { resolvedType ... on EffortNote { id name summary plan { content } } ... on Effort { governingSpecs { id } } }
  workspace: note(path: "`+workspacePath+`") { resolvedType ... on EffortWorkspace { id name summary implementationPlan { path } workLog { path } } }
  historical: note(path: "`+historicalPath+`") { ... on EffortNote { id name plan { content } } }
 }`)
	require.Empty(t, errors)
	result := ontologyquery.Execute(context.Background(), deps, schema, prepared)
	require.Empty(t, result.Errors)
	current := result.Data["current"].(map[string]any)
	require.Equal(t, "EffortNote", current["resolvedType"])
	require.Equal(t, "EFF-2026-09-13-10-01", current["id"])
	require.Equal(t, markdownTitle, current["name"])
	require.Equal(t, markdownSummary, current["summary"])
	require.Equal(t, []any{map[string]any{"id": "SPEC-0001"}}, current["governingSpecs"], "the filled template links its governing specs through the shared Effort field")

	require.Contains(t, narrativeContent(t, current, "plan"), "current-state gaps")
	workspace := result.Data["workspace"].(map[string]any)
	require.Equal(t, "EffortWorkspace", workspace["resolvedType"])
	require.Equal(t, "EFF-2026-09-13-10-00", workspace["id"])
	require.Equal(t, title, workspace["name"])
	require.Equal(t, summary, workspace["summary"])
	require.Equal(t, folder+"/plan.html", workspace["implementationPlan"].(map[string]any)["path"])
	require.Equal(t, folder+"/work-log.md", workspace["workLog"].(map[string]any)["path"])
	note := result.Data["historical"].(map[string]any)
	require.Equal(t, "EFF-2026-09-12-10-00", note["id"])
	require.Contains(t, narrativeContent(t, note, "plan"), "historical plan")
	recipes, issues := queryrecipe.LoadPath(filepath.Join(root, ".rhizome/query-recipes/spec-driven.yaml"))
	require.Empty(t, issues)
	exercised := 0
	for _, recipe := range recipes {
		if recipe.ID != "effort-execution-context" && recipe.ID != "closure-drift-pack" {
			continue
		}
		for _, path := range []string{newNotePath, workspacePath} {
			compiled, issues := queryrecipe.Compile(recipe, execSchema, map[string]string{"path": path})
			require.Empty(t, issues)
			result := ontologyquery.Execute(context.Background(), deps, schema, compiled.Prepared)
			require.Empty(t, result.Errors)
			effort := result.Data["note"].(map[string]any)
			require.Equal(t, path, effort["path"])
			require.Equal(t, "planned", effort["status"])
			if path == newNotePath {
				require.Contains(t, narrativeContent(t, effort, "plan"), "current-state gaps")
			} else {
				require.Equal(t, folder+"/plan.html", effort["implementationPlan"].(map[string]any)["path"])
				require.Contains(t, narrativeContent(t, effort["workLog"].(map[string]any), "closureChecklist"), "closure authority")
				materials := effort["materials"].([]any)
				require.Len(t, materials, 1)
				require.Equal(t, folder+"/materials/report.md", materials[0].(map[string]any)["path"])
			}
			exercised++
		}
	}
	require.Equal(t, 4, exercised, "both installed recipes must read both newly copied forms")
}

func TestRun_DogfoodPilotUsesSharedEffortContract(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, Run(RunOptions{Dir: root, Workflow: templateAgenticEngineering, Stdout: io.Discard, Stderr: io.Discard}))
	const folder = "docs/efforts/2026-09-13-09-55-html-effort-pilot"
	const entry = folder + "/2026-09-13-09-55-effort.html"
	for _, path := range []string{entry, folder + "/plan.html", folder + "/work-log.md"} {
		body, err := os.ReadFile(filepath.Join("../../../..", path))
		require.NoError(t, err)
		writeQueryRecipeExperienceNote(t, root, path, string(body))
	}
	_, schema, execSchema, store := indexAgenticEngineeringRecipeVault(t, root)
	vaultDef := obsidian.VaultDefinition{Root: root, Path: root, Includes: []string{"**/*.md", "**/*.html"}}
	deps := ontologyquery.Deps{VaultDef: vaultDef, NoteReader: &obsidian.Note{}, Store: store, Service: ontology.NewService(vaultDef, &obsidian.Note{}, store, schema)}
	var err error
	deps.NoteFormats, err = builtin.NewRuntime()
	require.NoError(t, err)
	prepared, errors := ontologyquery.Prepare(execSchema, `{
  notes(type: "Effort") { nodes { ... on NoteNode { path } ... on EffortWorkspace { id implementationPlan { path } workLog { path executionNotes { content } planApproval { content } actualDelivered { content } deviations { content } } } } }
 }`)
	require.Empty(t, errors)
	result := ontologyquery.Execute(context.Background(), deps, schema, prepared)
	require.Empty(t, result.Errors)
	notes := result.Data["notes"].(map[string]any)["nodes"].([]any)
	require.Len(t, notes, 1)
	effort := notes[0].(map[string]any)
	require.Equal(t, entry, effort["path"])
	require.Equal(t, "EFF-2026-09-13-09-55", effort["id"])
	require.Equal(t, folder+"/plan.html", effort["implementationPlan"].(map[string]any)["path"])
	log := effort["workLog"].(map[string]any)
	require.Equal(t, folder+"/work-log.md", log["path"])
	for _, section := range []string{"executionNotes", "planApproval", "actualDelivered", "deviations"} {
		require.NotEmpty(t, narrativeContent(t, log, section))
	}
}
