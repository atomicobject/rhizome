package init

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"
)

func TestNormalizeLegacyAgenticEngineeringStateMigratesCoupledState(t *testing.T) {
	cfg := obsidian.LocalConfig{
		WorkflowTemplates: []string{legacyTemplateSpecDriven},
		WorkflowTemplateAddons: obsidian.WorkflowTemplateAddons{
			Enabled: []string{legacyTemplateSpecDriven},
		},
		WorkflowTemplateManagement: obsidian.WorkflowTemplateManagement{
			Ejected: []string{legacyTemplateSpecDriven},
			SourceFingerprints: map[string]string{
				"spec-driven:docs:docs/specs/README.md":          "survives",
				"spec-driven:docs:docs/specs/process/retired.md": "retires",
				"unknown:docs:README.md":                         "prune",
			},
		},
	}

	changed, err := normalizeLegacyAgenticEngineeringState(&cfg)
	require.NoError(t, err)
	require.True(t, changed)
	require.Equal(t, []string{templateAgenticEngineering}, cfg.WorkflowTemplates)
	require.Equal(t, []string{templateAgenticEngineering}, cfg.WorkflowTemplateAddons.Enabled)
	require.Equal(t, []string{templateAgenticEngineering}, cfg.WorkflowTemplateManagement.Ejected)
	require.True(t, clearRetiredWorkflowManagement(&cfg), "retired fingerprints are removed, not migrated")
	require.Nil(t, cfg.WorkflowTemplateManagement.SourceFingerprints)

	changed, err = normalizeLegacyAgenticEngineeringState(&cfg)
	require.NoError(t, err)
	require.False(t, changed)
}

func TestHasLegacyAgenticEngineeringStateIncludesDisabledAddons(t *testing.T) {
	cfg := obsidian.LocalConfig{
		WorkflowTemplateAddons: obsidian.WorkflowTemplateAddons{
			Disabled: []string{legacyTemplateSpecDriven},
		},
	}

	require.True(t, hasLegacyAgenticEngineeringState(cfg))
}

func TestMigrateLegacyWorkflowConfigRetriesAfterWorkflowWriteFailure(t *testing.T) {
	root := t.TempDir()
	rhizomeDir := filepath.Join(root, ".rhizome")
	require.NoError(t, os.MkdirAll(rhizomeDir, 0o755))
	configPath := filepath.Join(rhizomeDir, "config.yml")
	require.NoError(t, os.WriteFile(configPath, []byte(`futureConfig: preserve
workflowTemplateAddons:
  disabled:
    - spec-driven
`), 0o600))

	attempts := 0
	saveWorkflow := func(root string, workflow obsidian.LocalWorkflowConfig) error {
		attempts++
		if attempts == 1 {
			return errors.New("injected workflows write failure")
		}
		return obsidian.SaveLocalWorkflowConfig(root, workflow)
	}

	_, migrated, err := migrateLegacyWorkflowConfigWithSave(root, saveWorkflow)
	require.Error(t, err)
	require.False(t, migrated)
	configAfterFailure, err := os.ReadFile(configPath)
	require.NoError(t, err)
	require.Contains(t, string(configAfterFailure), "workflowTemplateAddons:")
	require.Contains(t, string(configAfterFailure), "futureConfig: preserve")

	_, migrated, err = migrateLegacyWorkflowConfigWithSave(root, saveWorkflow)
	require.NoError(t, err)
	require.True(t, migrated)
	workflow, exists, err := obsidian.LoadLocalWorkflowConfig(root)
	require.NoError(t, err)
	require.True(t, exists)
	require.Empty(t, workflow.Templates)
	require.Equal(t, []string{templateAgenticEngineering}, workflow.Addons.Disabled)
	configAfterRetry, err := os.ReadFile(configPath)
	require.NoError(t, err)
	require.NotContains(t, string(configAfterRetry), "workflowTemplateAddons:")
	require.Contains(t, string(configAfterRetry), "futureConfig: preserve")
	require.Equal(t, 2, attempts)
	if runtime.GOOS != "windows" {
		info, err := os.Stat(configPath)
		require.NoError(t, err)
		require.Equal(t, os.FileMode(0o600), info.Mode().Perm())
	}
}

func TestRunReplaysLegacyProcessMigrationForComplexDomain(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, obsidian.SaveLocalConfig(root, obsidian.LocalConfig{}))
	require.NoError(t, obsidian.SaveLocalWorkflowConfig(root, obsidian.LocalWorkflowConfig{
		Templates: []string{templateComplexDomain},
	}))

	legacyRoot := filepath.Join(root, "docs", "specs", "process")
	require.NoError(t, os.MkdirAll(legacyRoot, 0o755))
	exact := []byte("shipped process README\n")
	modified := []byte("---\ntype: ProcessSpec\nspec-status: active\n---\n# Local workflow policy\n")
	require.NoError(t, os.WriteFile(filepath.Join(legacyRoot, "README.md"), exact, 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(legacyRoot, "agent-workflow.md"), modified, 0o644))

	originalLoader := legacyProcessFingerprintCatalogLoader
	legacyProcessFingerprintCatalogLoader = func() (legacyProcessFingerprintCatalog, error) {
		return legacyProcessFingerprintCatalog{
			"docs/specs/process/README.md":         {fingerprintStarterAsset(exact)},
			"docs/specs/process/agent-workflow.md": {fingerprintStarterAsset([]byte("shipped agent workflow\n"))},
		}, nil
	}
	t.Cleanup(func() { legacyProcessFingerprintCatalogLoader = originalLoader })

	run := func() {
		require.NoError(t, Run(RunOptions{
			Dir:         root,
			RefreshDocs: true,
			Agents:      "none",
			Stdout:      io.Discard,
			Stderr:      io.Discard,
		}))
	}
	run()
	cfg, err := obsidian.LoadLocalConfig(root)
	require.NoError(t, err)
	require.Equal(t, []string{templateComplexDomain}, cfg.WorkflowTemplates)
	require.FileExists(t, legacyProcessMigrationManifestPath(root))
	// A second run replays nothing new and keeps the retained document.
	run()

	require.NoFileExists(t, filepath.Join(legacyRoot, "README.md"))
	retained, err := os.ReadFile(filepath.Join(legacyRoot, "agent-workflow.md"))
	require.NoError(t, err)
	frontmatter, err := obsidian.ExtractFrontmatter(string(retained))
	require.NoError(t, err)
	require.Equal(t, "archived", frontmatter["spec-status"])
}

func TestNeedsComplexDomainLegacyProcessMigrationRequiresBothStateAndEvidence(t *testing.T) {
	root := t.TempDir()
	knownPath := filepath.Join(root, "docs", "specs", "process", "README.md")
	require.NoError(t, os.MkdirAll(filepath.Dir(knownPath), 0o755))
	require.NoError(t, os.WriteFile(knownPath, []byte("legacy evidence"), 0o644))

	require.False(t, needsComplexDomainLegacyProcessMigration(root, []string{templateAgenticEngineering}))
	require.True(t, needsComplexDomainLegacyProcessMigration(root, []string{templateAgenticEngineering, templateComplexDomain}))

	empty := t.TempDir()
	require.False(t, needsComplexDomainLegacyProcessMigration(empty, []string{templateAgenticEngineering, templateComplexDomain}))
}

func TestHasLegacyProcessMigrationPendingRecognizesPriorHumanManifest(t *testing.T) {
	root := t.TempDir()
	manifest := filepath.Join(legacyProcessMigrationDir(root), "README.md")
	require.NoError(t, os.MkdirAll(filepath.Dir(manifest), 0o755))
	require.NoError(t, os.WriteFile(manifest, []byte("pending\n"), 0o644))
	require.True(t, hasLegacyProcessMigrationPending(root))
}

func TestRunMigratesCurrentWorkflowLegacyDisabledAddon(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, obsidian.SaveLocalConfig(root, obsidian.LocalConfig{}))
	require.NoError(t, obsidian.SaveLocalWorkflowConfig(root, obsidian.LocalWorkflowConfig{
		Addons: obsidian.WorkflowTemplateAddons{Disabled: []string{legacyTemplateSpecDriven}},
	}))

	require.NoError(t, Run(RunOptions{
		Dir:    root,
		Agents: "none",
		Stdout: io.Discard,
		Stderr: io.Discard,
	}))

	workflow, exists, err := obsidian.LoadLocalWorkflowConfig(root)
	require.NoError(t, err)
	require.True(t, exists)
	require.Equal(t, []string{templateAgenticEngineering}, workflow.Addons.Disabled)
}

func TestInferTemplateChoicesMapsCanonicalAndLegacyEvidence(t *testing.T) {
	for _, rel := range []string{
		"docs/engineering/testing-policy.md",
		"docs/specs/process/development-loop.md",
		".agents/skills/agentic-engineering/SKILL.md",
	} {
		t.Run(rel, func(t *testing.T) {
			root := t.TempDir()
			path := filepath.Join(root, rel)
			require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
			require.NoError(t, os.WriteFile(path, []byte("evidence"), 0o644))
			got, err := inferTemplateChoices(root, nil)
			require.NoError(t, err)
			require.Equal(t, []string{templateAgenticEngineering}, got)
		})
	}
}

func TestPreservedTemplateBlockIDsKeepsFrozenLegacyFence(t *testing.T) {
	require.ElementsMatch(t,
		[]string{templateAgenticEngineering, legacyTemplateSpecDriven},
		preservedTemplateBlockIDs([]string{legacyTemplateSpecDriven}),
	)
}

func TestRunMigratesManagedLegacyFenceButPreservesEjectedFence(t *testing.T) {
	legacyBlock := managedTemplateBlockStartPrefix + legacyTemplateSpecDriven + managedTemplateBlockSuffix + "\nlocal frozen guidance\n" + managedTemplateBlockEndPrefix + legacyTemplateSpecDriven + managedTemplateBlockSuffix
	for _, tc := range []struct {
		name     string
		ejected  bool
		contains string
	}{
		{name: "managed", contains: managedTemplateBlockStartPrefix + templateAgenticEngineering + managedTemplateBlockSuffix},
		{name: "ejected", ejected: true, contains: legacyBlock},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			require.NoError(t, os.MkdirAll(filepath.Join(root, ".rhizome"), 0o755))
			require.NoError(t, obsidian.SaveLocalConfig(root, obsidian.LocalConfig{}))
			workflow := obsidian.LocalWorkflowConfig{Templates: []string{legacyTemplateSpecDriven}}
			if tc.ejected {
				workflow.Management.Ejected = []string{legacyTemplateSpecDriven}
			}
			require.NoError(t, obsidian.SaveLocalWorkflowConfig(root, workflow))
			require.NoError(t, os.WriteFile(filepath.Join(root, "AGENTS.md"), []byte("# Repository Guidelines\n\n"+legacyBlock+"\n"), 0o644))
			require.NoError(t, Run(RunOptions{
				Dir:    root,
				Agents: "codex",
				Stdout: io.Discard,
				Stderr: io.Discard,
			}))
			content, err := os.ReadFile(filepath.Join(root, "AGENTS.md"))
			require.NoError(t, err)
			require.Contains(t, string(content), tc.contains)
			if tc.ejected {
				require.Contains(t, string(content), legacyBlock)
			} else {
				require.NotContains(t, string(content), legacyTemplateSpecDriven+managedTemplateBlockSuffix)
			}
		})
	}
}

func TestInsertLegacyProcessRetirementNoticePlacementAndIdempotence(t *testing.T) {
	notice := legacyProcessRetirementSentinel + "\nThis is a historical record"
	for _, tc := range []struct {
		name    string
		content string
		prefix  string
	}{
		{
			name:    "front matter",
			content: "---\ntitle: Legacy\n---\n\n# Legacy\n",
			prefix:  "---\ntitle: Legacy\n---\n" + notice,
		},
		{
			name:    "front matter at eof",
			content: "---\ntitle: Legacy\n---",
			prefix:  "---\ntitle: Legacy\n---\n" + notice,
		},
		{
			name:    "heading",
			content: "intro\n\n# Legacy\n\nbody\n",
			prefix:  "intro\n\n# Legacy\n" + notice,
		},
		{
			name:    "no heading",
			content: "plain policy\n",
			prefix:  notice,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			first := string(insertLegacyProcessRetirementNotice([]byte(tc.content)))
			require.True(t, strings.HasPrefix(first, tc.prefix))
			require.Equal(t, 1, strings.Count(first, legacyProcessRetirementSentinel))
			second := string(insertLegacyProcessRetirementNotice([]byte(first)))
			require.Equal(t, first, second)
		})
	}
}
