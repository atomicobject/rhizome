package init

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/atomicobject/rhizome/pkg/vault/assetroots"
	"github.com/stretchr/testify/require"
)

func TestFreshInitInstallsEachStarterAsAModule(t *testing.T) {
	stubHookSupport(t, nil)
	root := machineRunRepo(t)
	_, err := Apply(RunOptions{Dir: root, Workflow: "complex-domain", Addons: "compounding,action-items", Agents: "claude,codex", Search: "off"})
	require.NoError(t, err)

	for _, id := range []string{templateCore, templateActionItems, templateAgenticEngineering, templateComplexDomain, templateCompounding} {
		files, err := starterModuleFiles(id)
		require.NoError(t, err)
		require.NotEmpty(t, files)
		for _, file := range files {
			require.True(t, strings.HasPrefix(file.target, ".rhizome/modules/"+id+"/"), file.target)
			require.FileExists(t, filepath.Join(root, filepath.FromSlash(file.target)))
		}
	}
	for _, kind := range []assetroots.Kind{assetroots.Ontology, assetroots.Recipes, assetroots.Views, assetroots.Tasks} {
		require.NoDirExists(t, filepath.Join(root, filepath.FromSlash(assetroots.FlatDir(kind))), "nothing installs into the flat folders")
	}
}

// TestReinitMigratesAFlatLayoutIntoModules starts from the layout init wrote
// before modules: starter files in the flat folders and compounding's
// scripts in every skill folder, all recorded in generated-files.yml.
func TestReinitMigratesAFlatLayoutIntoModules(t *testing.T) {
	stubHookSupport(t, nil)
	root := machineRunRepo(t)
	_, err := Apply(RunOptions{Dir: root, Workflow: "agentic-engineering", Addons: "compounding", Agents: "claude,codex", Search: "off", Hooks: "on"})
	require.NoError(t, err)
	flattenModulesForTest(t, root)

	const (
		flatSchema   = ".rhizome/ontology/spec-driven.graphql"
		moduleSchema = ".rhizome/modules/agentic-engineering/ontology/spec-driven.graphql"
		flatEdited   = ".rhizome/views/efforts.yaml"
		moduleEdited = ".rhizome/modules/agentic-engineering/views/efforts.yaml"
		teamView     = ".rhizome/views/team-board.yaml"
	)
	// An adopted identifier contract is not an edit, and it must survive.
	schema := readTestFile(t, root, flatSchema)
	adopted := strings.ReplaceAll(schema, `@identifier(strategy: DATETIME, preferred: true, prefix: "EFF")`, `@identifier(strategy: SEQUENTIAL, preferred: true, prefix: "EFF")`)
	require.NotEqual(t, schema, adopted)
	writeTestFile(t, root, flatSchema, adopted)
	writeTestFile(t, root, flatEdited, readTestFile(t, root, flatEdited)+"# team tweak\n")
	writeTestFile(t, root, teamView, "team-owned\n")

	out, err := runInit(t, RunOptions{Dir: root})
	require.NoError(t, err)

	for _, rel := range []string{flatSchema, ".rhizome/query-recipes/spec-driven.yaml", ".rhizome/views/specs.yaml", ".rhizome/ontology/compounding.graphql"} {
		require.NoFileExists(t, filepath.Join(root, rel), "an unedited flat copy moves into its module")
	}
	require.FileExists(t, filepath.Join(root, ".rhizome/modules/agentic-engineering/recipes/spec-driven.yaml"))
	contracts, err := parseIdentifierAllocationContracts("migrated schema", []byte(readTestFile(t, root, moduleSchema)))
	require.NoError(t, err)
	require.Equal(t, "SEQUENTIAL", contracts["EffortNote.id"].allocation.strategy, "the flat schema's identifier contract carries over")

	require.Contains(t, readTestFile(t, root, flatEdited), "# team tweak")
	require.NoFileExists(t, filepath.Join(root, moduleEdited), "the module copy of an edited flat file is not installed")
	require.Contains(t, out, flatEdited+" may have local edits")
	require.Contains(t, out, "move it to "+moduleEdited)
	require.Regexp(t, `Removed \d+ files Rhizome no longer ships\n`, out)
	require.Equal(t, "team-owned\n", readTestFile(t, root, teamView))

	for _, skills := range []string{".agents/skills", ".claude/skills"} {
		require.NoDirExists(t, filepath.Join(root, skills, "compounding", "scripts"))
		require.NoFileExists(t, filepath.Join(root, skills, "compounding", "models.json"))
		require.FileExists(t, filepath.Join(root, skills, "compounding", "references", "models.md"))
	}
	require.FileExists(t, filepath.Join(root, ".rhizome/modules/compounding/references/calibration-fixture.json"))
	handlers, err := HookHandlers(root)
	require.NoError(t, err)
	require.Equal(t, []string{"node", ".rhizome/modules/compounding/scripts/hook.mjs"}, handlers[0].Command)
	require.FileExists(t, filepath.Join(root, ".rhizome/modules/compounding/scripts/hook.mjs"))

	record, ok := loadGeneratedFiles(root)
	require.True(t, ok)
	require.NotContains(t, record.Written, flatSchema)
	require.Contains(t, record.Written, moduleSchema)
	_, err = runInit(t, RunOptions{Dir: root, Check: true})
	require.NoError(t, err, "nothing is pending after migration")

	// Adopting the module version, as the report says, keeps the edit an
	// edit: its record moves with it, so init does not ask whether it was
	// edited.
	require.NoError(t, os.Rename(filepath.Join(root, flatEdited), filepath.Join(root, moduleEdited)))
	out, err = runInit(t, RunOptions{Dir: root, Check: true})
	require.NoError(t, err)
	require.NotContains(t, out, "may have local edits, so Rhizome keeps it")
	require.NotContains(t, out, "can't tell")
	require.Contains(t, out, "Everything is up to date.")
	require.Contains(t, readTestFile(t, root, moduleEdited), "# team tweak")
}

// flattenModulesForTest rewrites a fresh install into the layout init wrote
// before modules, with the record to match.
func flattenModulesForTest(t *testing.T, root string) {
	t.Helper()
	record, ok := loadGeneratedFiles(root)
	require.True(t, ok)
	modules := filepath.Join(root, filepath.FromSlash(assetroots.ModulesDir))
	require.NoError(t, filepath.WalkDir(modules, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		rel, err := filepath.Rel(root, path)
		require.NoError(t, err)
		rel = filepath.ToSlash(rel)
		written := record.Written[rel]
		require.NotEmpty(t, written, rel)
		delete(record.Written, rel)
		var targets []string
		if _, kind, sub, ok := assetroots.Classify(rel); ok && assetroots.FlatDir(kind) != "" {
			targets = []string{assetroots.FlatDir(kind) + "/" + sub}
		} else {
			// Compounding's scripts, profile, and fixture lived in its skill.
			inner := strings.TrimPrefix(rel, assetroots.ModuleRoot(templateCompounding)+"/")
			targets = []string{".agents/skills/compounding/" + inner, ".claude/skills/compounding/" + inner}
		}
		data, err := os.ReadFile(path)
		require.NoError(t, err)
		for _, target := range targets {
			writeTestFile(t, root, target, string(data))
			record.Written[target] = written
		}
		return nil
	}))
	require.NoError(t, os.RemoveAll(modules))
	require.NoError(t, record.save(root))
}
