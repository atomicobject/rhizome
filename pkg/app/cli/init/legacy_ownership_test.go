package init

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/atomicobject/rhizome/pkg/vault/repoconfig"
	"github.com/stretchr/testify/require"
)

const legacyEffortSchema = `interface Effort {
  id: String!
}
type EffortNote implements Effort {
  id: String! @identifier(preferred: true, prefix: "EFF")
}
`

const legacyEffortPath = ".rhizome/ontology/spec-driven.graphql"
const moduleEffortPath = ".rhizome/modules/agentic-engineering/ontology/spec-driven.graphql"

func legacyOwnershipRepo(t *testing.T) string {
	t.Helper()
	stubHookSupport(t, nil)
	root := machineRunRepo(t)
	_, err := Apply(RunOptions{Dir: root, Workflow: "complex-domain", Agents: "shared", Search: "off", BinaryManager: "external"})
	require.NoError(t, err)
	record, ok := loadGeneratedFiles(root)
	require.True(t, ok)
	workflow, exists, err := repoconfig.LoadLocalWorkflowConfig(root)
	require.NoError(t, err)
	require.True(t, exists)
	workflow.Management.SourceFingerprints = map[string]string{}
	for _, item := range []struct{ starter, name string }{
		{templateCore, "core.graphql"},
		{templateActionItems, "action-items.graphql"},
		{templateComplexDomain, "complex-domain.graphql"},
		{templateAgenticEngineering, "spec-driven.graphql"},
	} {
		module := ".rhizome/modules/" + item.starter + "/ontology/" + item.name
		flat := ".rhizome/ontology/" + item.name
		content := readTestFile(t, root, module)
		if item.starter == templateAgenticEngineering {
			content = legacyEffortSchema
			item.starter = legacyTemplateSpecDriven
		}
		writeTestFile(t, root, flat, content)
		require.NoError(t, os.Remove(filepath.Join(root, module)))
		delete(record.Written, module)
		workflow.Management.SourceFingerprints[item.starter+":ontology:"+item.name] = fingerprintStarterAsset([]byte(content))
	}
	require.NoError(t, record.save(root))
	require.NoError(t, repoconfig.SaveLocalWorkflowConfig(root, workflow))
	return root
}

func TestInitMigratesVerifiedLegacyOwnership(t *testing.T) {
	root := legacyOwnershipRepo(t)
	before := readTestFile(t, root, ".rhizome/workflows.yml")
	plan, err := Plan(RunOptions{Dir: root})
	require.NoError(t, err)
	require.Contains(t, plan.Writes.Files, moduleEffortPath)
	require.Contains(t, plan.Writes.Files, legacyEffortPath)
	require.Equal(t, before, readTestFile(t, root, ".rhizome/workflows.yml"), "planning preserves legacy evidence")
	require.NoFileExists(t, filepath.Join(root, moduleEffortPath))

	_, err = runInit(t, RunOptions{Dir: root})
	require.NoError(t, err)
	for _, name := range []string{"core.graphql", "action-items.graphql", "complex-domain.graphql", "spec-driven.graphql"} {
		require.NoFileExists(t, filepath.Join(root, ".rhizome/ontology", name))
	}
	updated := readTestFile(t, root, moduleEffortPath)
	require.Contains(t, updated, `group: "Delivery"`)
	contracts, err := parseIdentifierAllocationContracts("migrated", []byte(updated))
	require.NoError(t, err)
	for _, name := range []string{"EffortNote.id", "EffortWorkspace.id"} {
		contract := contracts[name].allocation
		require.Equal(t, "SEQUENTIAL", contract.strategy, name)
		require.Equal(t, "EFF", contract.prefix, name)
		require.Equal(t, "-", contract.separator, name)
		require.Equal(t, 4, contract.pad, name)
	}
	workflow, _, err := repoconfig.LoadLocalWorkflowConfig(root)
	require.NoError(t, err)
	require.Empty(t, workflow.Management.SourceFingerprints)
	record, ok := loadGeneratedFiles(root)
	require.True(t, ok)
	require.Contains(t, record.Written, moduleEffortPath)
	require.NotContains(t, record.Written, legacyEffortPath)

	plan, err = Plan(RunOptions{Dir: root})
	require.NoError(t, err)
	require.Empty(t, plan.Writes.Files)
	out, err := runInit(t, RunOptions{Dir: root})
	require.NoError(t, err)
	require.Contains(t, out, "Everything is up to date.")
	require.Equal(t, updated, readTestFile(t, root, moduleEffortPath))
}

func TestInitKeepsUnprovenLegacyOwnership(t *testing.T) {
	for _, scenario := range []string{"modified", "missing evidence", "unsupported family", "edit after plan", "existing record", "declined version"} {
		t.Run(scenario, func(t *testing.T) {
			root := legacyOwnershipRepo(t)
			workflow, _, err := repoconfig.LoadLocalWorkflowConfig(root)
			require.NoError(t, err)
			key := "spec-driven:ontology:spec-driven.graphql"
			switch scenario {
			case "modified":
				writeTestFile(t, root, legacyEffortPath, legacyEffortSchema+"type Custom { name: String }\n")
			case "missing evidence":
				delete(workflow.Management.SourceFingerprints, key)
			case "unsupported family":
				workflow.Management.SourceFingerprints["spec-driven:unknown:spec-driven.graphql"] = workflow.Management.SourceFingerprints[key]
				delete(workflow.Management.SourceFingerprints, key)
			case "edit after plan":
				plan, err := Plan(RunOptions{Dir: root})
				require.NoError(t, err)
				require.Contains(t, plan.Writes.Files, moduleEffortPath)
				writeTestFile(t, root, legacyEffortPath, legacyEffortSchema+"type Custom { name: String }\n")
			case "declined version":
				record, _ := loadGeneratedFiles(root)
				record.Declined[legacyEffortPath] = "declined update"
				require.NoError(t, record.save(root))
			case "existing record":
				record, _ := loadGeneratedFiles(root)
				record.Written[legacyEffortPath] = "a different baseline"
				require.NoError(t, record.save(root))
			}
			require.NoError(t, repoconfig.SaveLocalWorkflowConfig(root, workflow))
			original := readTestFile(t, root, legacyEffortPath)
			plan, err := Plan(RunOptions{Dir: root})
			require.NoError(t, err)
			require.NotContains(t, plan.Writes.Files, moduleEffortPath)
			_, err = Apply(RunOptions{Dir: root})
			require.NoError(t, err)
			require.Equal(t, original, readTestFile(t, root, legacyEffortPath))
			require.NoFileExists(t, filepath.Join(root, moduleEffortPath))
		})
	}
}

func TestInitTransfersLegacyAssetFamilies(t *testing.T) {
	for _, item := range []struct{ family, folder, name, target string }{
		{"queryRecipes", "query-recipes", "spec-driven.yaml", ".rhizome/modules/agentic-engineering/recipes/spec-driven.yaml"},
		{"views", "views", "efforts.yaml", ".rhizome/modules/agentic-engineering/views/efforts.yaml"},
		{"docs", "docs", "docs/engineering/architecture.md", "docs/engineering/architecture.md"},
	} {
		t.Run(item.family, func(t *testing.T) {
			root := legacyOwnershipRepo(t)
			current := readTestFile(t, root, item.target)
			previous := current + "\n# Previous generated version\n"
			flat := ".rhizome/" + item.folder + "/" + item.name
			if item.family == "docs" {
				flat = item.target
			}
			writeTestFile(t, root, flat, previous)
			record, _ := loadGeneratedFiles(root)
			delete(record.Written, item.target)
			if flat != item.target {
				require.NoError(t, os.Remove(filepath.Join(root, item.target)))
			}
			require.NoError(t, record.save(root))
			workflow, _, err := repoconfig.LoadLocalWorkflowConfig(root)
			require.NoError(t, err)
			workflow.Management.SourceFingerprints["retired-starter:"+item.family+":"+item.name] = fingerprintStarterAsset([]byte(previous))
			require.NoError(t, repoconfig.SaveLocalWorkflowConfig(root, workflow))

			_, err = Apply(RunOptions{Dir: root, RefreshDocs: true})
			require.NoError(t, err)
			require.Equal(t, current, readTestFile(t, root, item.target))
			if flat != item.target {
				require.NoFileExists(t, filepath.Join(root, flat))
			}
		})
	}
}

func TestInitMigratesLegacyOwnershipWithoutGeneratedRecord(t *testing.T) {
	root := legacyOwnershipRepo(t)
	require.NoError(t, os.Remove(generatedFilesPath(root)))
	plan, err := Plan(RunOptions{Dir: root})
	require.NoError(t, err)
	require.Contains(t, plan.Writes.Files, moduleEffortPath)
	_, err = Apply(RunOptions{Dir: root})
	require.NoError(t, err)
	require.NoFileExists(t, filepath.Join(root, legacyEffortPath))
	require.Contains(t, readTestFile(t, root, moduleEffortPath), `group: "Delivery"`)
	record, ok := loadGeneratedFiles(root)
	require.True(t, ok)
	require.Contains(t, record.Written, moduleEffortPath)
}
