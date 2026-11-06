package init

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/atomicobject/rhizome/pkg/sourceownership"
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
				sourceownership.DeclarationPath,
			} {
				require.FileExists(t, filepath.Join(root, path))
			}
			// Ownership is established at install, before any binding or index exists.
			var refusal *sourceownership.Refusal
			for _, owned := range []string{"assertions/claim.md", ".kb/curation/_conflicts.md", "sources/_raw/input.md", "products/billing/concepts/term.md"} {
				require.ErrorAs(t, sourceownership.Check(root, []string{owned}), &refusal, owned)
				require.Equal(t, "AO KB", refusal.Owner)
			}
			require.NoError(t, sourceownership.Check(root, []string{"docs/reference/guides/ao-kb.md", "notes/team.md"}))
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
				require.FileExists(t, filepath.Join(root, sourceownership.DeclarationPath), "protection ends only when the declaration is deleted explicitly")
			} else {
				_, err = runInit(t, RunOptions{Dir: root, Eject: "ao-kb"})
				require.NoError(t, err)
				require.Contains(t, loadConfigForTest(t, root).WorkflowTemplateManagement.Ejected, "ao-kb")
				require.FileExists(t, filepath.Join(root, ".agents/skills/ao-kb-setup/SKILL.md"))
				require.FileExists(t, filepath.Join(root, sourceownership.DeclarationPath), "ejection keeps protection")
				_, err = runInit(t, RunOptions{Dir: root, AddAddon: "ao-kb"})
				require.ErrorContains(t, err, "use --restore ao-kb")
			}
			after := aoKBFiles(t, root)
			require.Equal(t, before[edited]+"\n// Local edit.\n", after[edited], "edited assets survive removal/ejection")
			for path, data := range authored {
				require.Equal(t, data, after[path], path)
			}
			require.Contains(t, after["AGENTS.md"], "# Existing team guidance\nKeep this prose.\n")
		})
	}
}

func TestAOKBStarterFirstInit(t *testing.T) {
	root := machineRunRepo(t)
	_, err := Apply(RunOptions{Dir: root, Workflow: "agentic-engineering", Addons: "ao-kb", Search: "off"})
	require.NoError(t, err)
	require.Contains(t, loadConfigForTest(t, root).WorkflowTemplateAddons.Enabled, "ao-kb")
	require.FileExists(t, filepath.Join(root, ".rhizome/modules/ao-kb/actions/check.yaml"))
	requireUpToDate(t, root)
}

func TestAOKBStarterPreservesAnEditedOwnershipDeclaration(t *testing.T) {
	root := t.TempDir()
	writeProjectConfig(t, root, "notes:\n  includes: [\"**/*.md\"]\n", repoconfig.LocalWorkflowConfig{})
	_, err := runInit(t, RunOptions{Dir: root, AddAddon: "ao-kb"})
	require.NoError(t, err)
	declaration := filepath.Join(root, sourceownership.DeclarationPath)
	data, err := os.ReadFile(declaration)
	require.NoError(t, err)
	edited := string(data) + "      - knowledge/**\n"
	require.NoError(t, os.WriteFile(declaration, []byte(edited), 0o644))
	_, err = runInit(t, RunOptions{Dir: root})
	require.NoError(t, err)
	data, err = os.ReadFile(declaration)
	require.NoError(t, err)
	require.Equal(t, edited, string(data))
	require.Error(t, sourceownership.Check(root, []string{"knowledge/extra.md"}))
}
