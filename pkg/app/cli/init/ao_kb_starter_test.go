package init

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/atomicobject/rhizome/pkg/sourceownership"
	"github.com/atomicobject/rhizome/pkg/validate"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/atomicobject/rhizome/pkg/vault/repoconfig"
	"github.com/stretchr/testify/require"
)

func aoKBFiles(t *testing.T, root string) map[string]string {
	t.Helper()
	files := map[string]string{}
	require.NoError(t, filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		files[filepath.ToSlash(rel)] = string(data)
		return nil
	}))
	return files
}

func TestAOKBStarterLifecyclePreservesAOFiles(t *testing.T) {
	for _, mode := range []string{"remove", "eject"} {
		t.Run(mode, func(t *testing.T) {
			root := t.TempDir()
			writeProjectConfig(t, root, "notes:\n  includes: [\"**/*.md\"]\n", repoconfig.LocalWorkflowConfig{})
			authored := map[string]string{
				".kb/config/vault.md":                   "---\nschemaVersion: 8\nproductMode: single\n---\n",
				".kb/curation/_conflicts.md":            "# Existing conflicts\n",
				".kb/index/kb-weights.json":             "{\"meta\":{\"asOf\":\"2026-10-01\"}}\n",
				"assertions/claim.md":                   "---\nid: CLM-01\n---\nAn illustrative claim.\n",
				"sources/_raw/input.md":                 "Synthetic source.\n",
				".agents/skills/ao-kb-process/SKILL.md": "# Existing AO skill\n",
				".claude/skills/ao-kb-process/SKILL.md": "# Existing Claude AO skill\n",
			}
			for path, data := range authored {
				writeTestFile(t, root, path, data)
			}
			writeTestFile(t, root, "AGENTS.md", "# Existing team guidance\nKeep this prose.\n")
			_, err := runInit(t, RunOptions{Dir: root, AddAddon: "ao-kb"})
			require.NoError(t, err)
			for _, path := range []string{
				".rhizome/modules/ao-kb/actions/check.yaml",
				".rhizome/modules/ao-kb/scripts/check.mjs",
				".agents/skills/ao-kb-setup/SKILL.md",
				".claude/skills/ao-kb-setup/SKILL.md",
				"docs/reference/guides/ao-kb.md",
				".rhizome/modules/ao-kb/scripts/lib/bindings-check.mjs",
				".rhizome/modules/ao-kb/actions/refresh-bindings.yaml",
				".rhizome/modules/ao-kb/bindings/cli.mjs",
				".rhizome/modules/ao-kb/bindings/adapter.py",
				".rhizome/modules/ao-kb/bindings/toolkit-pin.json",
			} {
				require.FileExists(t, filepath.Join(root, path))
			}
			// The compiler's selection, manifest, and generated schema are produced in the
			// repository by refresh-bindings, never shipped.
			for _, path := range []string{
				".rhizome/modules/ao-kb/bindings/selection.json",
				".rhizome/modules/ao-kb/bindings/manifest.json",
				".rhizome/modules/ao-kb/ontology/ao-model.generated.graphql",
			} {
				require.NoFileExists(t, filepath.Join(root, path))
			}
			// The starter declares no ownership: AO KB notes are edited like any other note.
			require.NoFileExists(t, filepath.Join(root, sourceownership.DeclarationPath))
			require.NoError(t, sourceownership.Check(root, []string{"assertions/claim.md", ".kb/curation/_conflicts.md", "products/billing/concepts/term.md", "notes/team.md"}))
			// A declaration the repository writes for another tool is its own; init never touches it.
			writeTestFile(t, root, sourceownership.DeclarationPath, repositoryDeclaration)
			generated := map[string]string{
				".rhizome/modules/ao-kb/ontology/ao-model.generated.graphql":           "# Synthetic runtime model generation\n",
				".rhizome/modules/ao-kb/ontology/ao-knowledge.generated.graphql":       "# Synthetic runtime admission generation\n",
				".rhizome/modules/ao-kb/bindings/manifest.json":                        "{\"status\":\"ready\"}\n",
				".rhizome/modules/ao-kb/views/knowledge/AOKBModel_fixture_single.yaml": "# Synthetic runtime view generation\n",
			}
			for name, content := range generated {
				writeTestFile(t, root, name, content)
			}
			before := aoKBFiles(t, root)
			requireUpToDate(t, root)
			require.Equal(t, before, aoKBFiles(t, root), "re-init and --check change no bytes")
			edited := ".rhizome/modules/ao-kb/scripts/check.mjs"
			writeTestFile(t, root, edited, before[edited]+"\n// Local edit.\n")
			_, err = runInit(t, RunOptions{Dir: root})
			require.NoError(t, err)
			require.Equal(t, before[edited]+"\n// Local edit.\n", aoKBFiles(t, root)[edited])
			if mode == "remove" {
				_, err = runInit(t, RunOptions{Dir: root, RemoveAddon: "ao-kb"})
				require.NoError(t, err)
				require.NoFileExists(t, filepath.Join(root, ".agents/skills/ao-kb-setup/SKILL.md"))
				require.Equal(t, repositoryDeclaration, aoKBFiles(t, root)[sourceownership.DeclarationPath], "removal leaves the repository's own declaration")
			} else {
				_, err = runInit(t, RunOptions{Dir: root, Eject: "ao-kb"})
				require.NoError(t, err)
				require.Contains(t, loadConfigForTest(t, root).WorkflowTemplateManagement.Ejected, "ao-kb")
				require.FileExists(t, filepath.Join(root, ".agents/skills/ao-kb-setup/SKILL.md"))
				require.Equal(t, repositoryDeclaration, aoKBFiles(t, root)[sourceownership.DeclarationPath], "ejection leaves the repository's own declaration")
				_, err = runInit(t, RunOptions{Dir: root, AddAddon: "ao-kb"})
				require.ErrorContains(t, err, "use --restore ao-kb")
			}
			after := aoKBFiles(t, root)
			require.Equal(t, before[edited]+"\n// Local edit.\n", after[edited], "edited assets survive removal/ejection")
			for path, data := range authored {
				require.Equal(t, data, after[path], path)
			}
			for name, content := range generated {
				require.Equal(t, content, after[name], "generated outputs survive removal/ejection")
			}
			require.Contains(t, after["AGENTS.md"], "# Existing team guidance\nKeep this prose.\n")
		})
	}
}

func TestAOKBUpgradeUpdatesOldAssetsAndPreservesLocalEdits(t *testing.T) {
	root := t.TempDir()
	writeProjectConfig(t, root, "notes:\n  includes: [\"**/*.md\"]\n", repoconfig.LocalWorkflowConfig{})
	_, err := runInit(t, RunOptions{Dir: root, AddAddon: "ao-kb"})
	require.NoError(t, err)
	asset := ".rhizome/modules/ao-kb/bindings/render.mjs"
	current := readTestFile(t, root, asset)
	old := "// Synthetic previous starter release\n"
	writeTestFile(t, root, asset, old)
	setWritten(t, root, asset, old)
	edited := ".rhizome/modules/ao-kb/scripts/check.mjs"
	custom := readTestFile(t, root, edited) + "\n// Project customization\n"
	writeTestFile(t, root, edited, custom)
	_, err = runInit(t, RunOptions{Dir: root})
	require.NoError(t, err)
	require.Equal(t, current, readTestFile(t, root, asset))
	require.Equal(t, custom, readTestFile(t, root, edited))
}

func TestAOKBStarterFirstInit(t *testing.T) {
	root := machineRunRepo(t)
	_, err := Apply(RunOptions{Dir: root, Workflow: "agentic-engineering", Addons: "ao-kb", Search: "off"})
	require.NoError(t, err)
	require.Contains(t, loadConfigForTest(t, root).WorkflowTemplateAddons.Enabled, "ao-kb")
	require.FileExists(t, filepath.Join(root, ".rhizome/modules/ao-kb/actions/check.yaml"))
	requireUpToDate(t, root)
}

func TestAOKBFreshStaticInstallValidatesBeforeRefresh(t *testing.T) {
	root := installAOKBFixture(t)
	for _, name := range []string{"ao-model.generated.graphql", "ao-knowledge.generated.graphql"} {
		require.NoFileExists(t, filepath.Join(root, ".rhizome/modules/ao-kb/ontology", name))
	}
	result := validate.RunViews(context.Background(), validate.RunContext{
		VaultDef: obsidian.VaultDefinition{Path: root}, VaultPath: root, NoteReader: &obsidian.Note{}, MaxIssues: 100,
	})
	require.True(t, result.OK, "%+v", result.Issues)
	requireUpToDate(t, root)
}

// A declaration a repository writes for a tool of its own, beside the AO KB add-on.
const repositoryDeclaration = "sources:\n  - owner: Model tool\n    reason: Generated rows. Change them in the model tool.\n    paths:\n      - knowledge/**\n"

func TestAOKBStarterKeepsTheRepositorysOwnDeclaration(t *testing.T) {
	root := t.TempDir()
	writeProjectConfig(t, root, "notes:\n  includes: [\"**/*.md\"]\n", repoconfig.LocalWorkflowConfig{})
	writeTestFile(t, root, sourceownership.DeclarationPath, repositoryDeclaration)
	_, err := runInit(t, RunOptions{Dir: root, AddAddon: "ao-kb"})
	require.NoError(t, err)
	_, err = runInit(t, RunOptions{Dir: root})
	require.NoError(t, err)
	data, err := os.ReadFile(filepath.Join(root, sourceownership.DeclarationPath))
	require.NoError(t, err)
	require.Equal(t, repositoryDeclaration, string(data))
	var refusal *sourceownership.Refusal
	require.ErrorAs(t, sourceownership.Check(root, []string{"knowledge/extra.md"}), &refusal)
	require.Equal(t, "Model tool", refusal.Owner)
	require.NoError(t, sourceownership.Check(root, []string{"assertions/claim.md"}), "the add-on itself owns nothing")
}
