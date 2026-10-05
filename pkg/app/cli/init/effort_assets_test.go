package init

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"
)

func TestRun_EffortAssetsScaffoldIdempotentlyWithoutActiveEfforts(t *testing.T) {
	root := t.TempDir()
	opts := RunOptions{Dir: root, Workflow: templateAgenticEngineering, Stdout: io.Discard, Stderr: io.Discard}
	require.NoError(t, Run(opts))
	cfg, err := obsidian.LoadLocalConfig(root)
	require.NoError(t, err)
	require.Contains(t, cfg.Notes.Includes, "docs/efforts/**/*.html")
	require.True(t, pathMatchesAnyGlob("docs/efforts/historical.md", cfg.Notes.Includes))
	require.True(t, pathMatchesAnyGlob("docs/efforts/example/overview.html", cfg.Notes.Includes))
	require.False(t, pathMatchesAnyGlob("docs/other/page.html", cfg.Notes.Includes))
	paths := []string{
		"docs/efforts/templates/effort.md.template",
		"docs/efforts/templates/workspace/overview.html.template",
		"docs/efforts/templates/workspace/plan.html.template",
		"docs/efforts/templates/workspace/effort.css.template",
		"docs/efforts/templates/workspace/work-log.md.template",
		"docs/efforts/templates/workspace/materials/material.md.template",
	}
	before := make(map[string][]byte)
	for _, path := range paths {
		body, err := os.ReadFile(filepath.Join(root, path))
		require.NoError(t, err)
		require.NotEmpty(t, body)
		before[path] = body
	}
	for _, name := range []string{"overview.html.template", "plan.html.template"} {
		require.Contains(t, string(before["docs/efforts/templates/workspace/"+name]), `href="effort.css"`)
	}
	require.Contains(t, string(before["docs/efforts/templates/workspace/plan.html.template"]), `aria-label="Plan sequence"`)
	guidance, err := os.ReadFile(filepath.Join(root, ".agents", "skills", "agentic-engineering", "references", "effort-setup.md"))
	require.NoError(t, err)
	for _, contract := range []string{"EffortNote", "EffortWorkspace", "ref.typeName", "shared `EFF` pool", "JSON serializer", `\u003c/script>`, "quoted attributes", "Markdown note mutation", "%Y-%m-%dT%H:%M:%SZ"} {
		require.Contains(t, string(guidance), contract)
	}
	require.NoError(t, Run(opts))
	refreshed, err := obsidian.LoadLocalConfig(root)
	require.NoError(t, err)
	require.Equal(t, cfg.Notes.Includes, refreshed.Notes.Includes)
	for _, path := range paths {
		body, err := os.ReadFile(filepath.Join(root, path))
		require.NoError(t, err)
		require.Equal(t, before[path], body, "rerunning init must preserve asset %s", path)
	}
	schema, err := ontology.LoadSchema(root)
	require.NoError(t, err)
	sources, err := testNoteMetadataIndexer(t).BuildNoteSourceSnapshots(context.Background(), obsidian.VaultDefinition{Path: root}, &obsidian.Note{})
	require.NoError(t, err)
	build, err := ontology.BuildIndexFromNoteSources(context.Background(), obsidian.VaultDefinition{Path: root}, sources, schema, "inert-effort-assets")
	require.NoError(t, err)
	for _, node := range build.Nodes {
		require.NotEqual(t, "EffortNote", node.TypeName, "starter must not create an effort: %s", node.NotePath)
		require.NotEqual(t, "EffortWorkspace", node.TypeName, "starter must not create an effort: %s", node.NotePath)
	}
}

func TestRun_ResumedEffortInstallRepairsNoteIncludes(t *testing.T) {
	for _, includes := range [][]string{{"notes/**/*.md"}, {"docs/**/*"}} {
		t.Run(includes[0], func(t *testing.T) {
			root := t.TempDir()
			opts := RunOptions{Dir: root, Workflow: templateAgenticEngineering, Stdout: io.Discard, Stderr: io.Discard}
			require.NoError(t, Run(opts))
			cfg, err := obsidian.LoadLocalConfig(root)
			require.NoError(t, err)
			cfg.Notes.Includes = includes
			cfg.Notes.Excludes = []string{"docs/efforts/private/**"}
			require.NoError(t, obsidian.SaveLocalConfig(root, *cfg))

			// Assets already landed before include reconciliation was interrupted.
			require.NoError(t, Run(opts))
			repaired, err := obsidian.LoadLocalConfig(root)
			require.NoError(t, err)
			want := append([]string(nil), includes...)
			if includes[0] == "notes/**/*.md" {
				want = append(want, "docs/**/*.md")
			}
			want = append(want, "docs/efforts/**/*.html")
			require.Equal(t, want, repaired.Notes.Includes)
			for _, path := range []string{"docs/efforts/example.md", "docs/efforts/example/work-log.md", "docs/efforts/example/materials/research.md"} {
				require.True(t, pathMatchesAnyGlob(path, repaired.Notes.Includes), path)
			}
			require.Equal(t, cfg.Notes.Excludes, repaired.Notes.Excludes)
			require.NoError(t, Run(opts))
			rerun, err := obsidian.LoadLocalConfig(root)
			require.NoError(t, err)
			require.Equal(t, repaired.Notes, rerun.Notes)
		})
	}
}

func TestRun_WorkspaceRequiresExplicitGoverningSpecs(t *testing.T) {
	for _, selection := range []string{"", `,"governing-specs":[]`} {
		t.Run(selection, func(t *testing.T) {
			root := t.TempDir()
			require.NoError(t, Run(RunOptions{Dir: root, Workflow: templateAgenticEngineering, Stdout: io.Discard, Stderr: io.Discard}))
			const path = "docs/efforts/example/2026-09-13-10-00-effort.html"
			writeTemplateNote(t, root, path, `<html><head><title>Example</title><script id="rhizome-metadata" type="application/json">{"type":"EffortWorkspace","id":"EFF-2026-09-13-10-00","aliases":["EFF-2026-09-13-10-00"],"name":"Example","created-at":"2026-09-13T14:00:00Z","status":"planned","summary":"Example","implementation-plan":"docs/efforts/example/plan.html","work-log":"docs/efforts/example/work-log.md"`+selection+`}</script></head><body></body></html>`)
			writeTemplateNote(t, root, "docs/efforts/example/plan.html", "<html><head><title>Plan</title></head><body></body></html>")
			writeTemplateNote(t, root, "docs/efforts/example/work-log.md", "# Work log\n")
			schema, err := ontology.LoadSchema(root)
			require.NoError(t, err)
			sources, err := testNoteMetadataIndexer(t).BuildNoteSourceSnapshots(context.Background(), obsidian.VaultDefinition{Root: root, Path: root, Includes: []string{"**/*.md", "**/*.html"}}, &obsidian.Note{})
			require.NoError(t, err)
			build, err := ontology.BuildIndexFromNoteSources(context.Background(), obsidian.VaultDefinition{Root: root, Path: root, Includes: []string{"**/*.md", "**/*.html"}}, sources, schema, "workspace-spec-selection")
			require.NoError(t, err)
			requireStarterNode(t, build.Nodes, "EffortWorkspace", "Example")
			issues := starterIssuesForPath(build.ValidationIssues, path, "missing_required_field")
			if selection == `,"governing-specs":[]` {
				require.Empty(t, issues)
			} else {
				require.Len(t, issues, 1)
				require.Equal(t, "governingSpecs", issues[0].FieldName)
			}
		})
	}
}
