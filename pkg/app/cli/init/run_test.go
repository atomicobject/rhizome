package init

import (
	"bytes"
	"context"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	appupdate "github.com/atomicobject/rhizome/pkg/app/update"
	"github.com/atomicobject/rhizome/pkg/ontology"
	ontologyquery "github.com/atomicobject/rhizome/pkg/ontology/query"
	"github.com/atomicobject/rhizome/pkg/ontology/queryrecipe"
	"github.com/atomicobject/rhizome/pkg/ontology/viewconfig"
	"github.com/atomicobject/rhizome/pkg/teamkeys"
	vaultconfig "github.com/atomicobject/rhizome/pkg/vault/config"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/atomicobject/rhizome/pkg/vault/version"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

func TestMain(m *testing.M) {
	originalInstall := installPinnedBinary
	installPinnedBinary = func(context.Context, appupdate.EnsureOptions) error {
		return nil
	}
	// Results must not depend on which agent tools or keys this machine has.
	installedAgentCommand = func(string) (string, error) { return "", exec.ErrNotFound }
	for _, key := range []string{"VOYAGE_API_KEY", "RHIZOME_VOYAGE_API_KEY", "OPENAI_API_KEY", "RHIZOME_OPENAI_API_KEY", "ATOMIC_RHIZOME_KEY"} {
		_ = os.Unsetenv(key)
	}
	home, err := os.MkdirTemp("", "rzm-init-test-home")
	if err != nil {
		panic(err)
	}
	vaultconfig.UserHomeDirectory = func() (string, error) { return home, nil }
	code := m.Run()
	installPinnedBinary = originalInstall
	_ = os.RemoveAll(home)
	os.Exit(code)
}

func mockNoGlobalConfig(t *testing.T) {
	t.Helper()
	original := vaultconfig.UserHomeDirectory
	home := t.TempDir()
	vaultconfig.UserHomeDirectory = func() (string, error) { return home, nil }
	t.Setenv("ATOMIC_RHIZOME_KEY", "")
	teamkeys.ResetCache()
	t.Cleanup(func() {
		vaultconfig.UserHomeDirectory = original
		teamkeys.ResetCache()
	})
}

func writeProjectConfig(t *testing.T, root, config string, workflow obsidian.LocalWorkflowConfig) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Join(root, ".rhizome"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, ".rhizome", "config.yml"), []byte(config), 0o644))
	require.NoError(t, obsidian.SaveLocalWorkflowConfig(root, workflow))
}

func TestRun_FastPathRegeneratesAgentsOnly(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, ".rhizome"), 0o755))

	// Existing config should be preserved (fast path should not rewrite it).
	cfgPath := filepath.Join(root, ".rhizome", "config.yml")
	beforeCfg := []byte(strings.TrimSpace(`
notes:
  includes: ["**/*.md"]
`) + "\n")
	require.NoError(t, os.WriteFile(cfgPath, beforeCfg, 0o644))

	agentsPath := filepath.Join(root, "AGENTS.md")
	require.NoError(t, os.WriteFile(agentsPath, []byte("# Repository Guidelines\n\n<!--- RHIZOME START -->\n\nOLD CONTENT\n\n<!--- RHIZOME END -->\n\n## Other\nkeep me\n"), 0o644))

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	err := Run(RunOptions{
		Dir:         root,
		Interactive: true,
		Stdout:      &stdout,
		Stderr:      &stderr,
		Stdin:       bytes.NewBufferString("\n"), // apply
	})
	require.NoError(t, err)

	afterCfg, err := os.ReadFile(cfgPath)
	require.NoError(t, err)
	require.True(t, strings.HasPrefix(string(afterCfg), string(beforeCfg)), "existing settings keep their formatting")

	gitignore, err := os.ReadFile(filepath.Join(root, ".rhizome", ".gitignore"))
	require.NoError(t, err)
	require.Equal(t, rhizomeGitIgnoreBody(), string(gitignore))

	agents, err := os.ReadFile(agentsPath)
	require.NoError(t, err)
	require.NotContains(t, string(agents), "<!--- RHIZOME START -->")
	require.NotContains(t, string(agents), "<!--- RHIZOME END -->")
	require.NotContains(t, string(agents), "OLD CONTENT")
	require.Contains(t, string(agents), "# Repository Guidelines")
	require.Contains(t, string(agents), "## Other\nkeep me")
	require.NoFileExists(t, filepath.Join(root, "RHIZOME.md"))
	require.Contains(t, string(agents), managedRhizomeBlockStart)
	require.Contains(t, string(agents), "# Rhizome integration")
	require.Contains(t, string(agents), "### Repo configuration")
	require.Contains(t, string(agents), "If an installed `rhizome` skill is available to the active harness")
	require.Contains(t, string(agents), "Rhizome enablement is opt-in")
	require.Contains(t, string(agents), "Never silently substitute generic shell behavior")
	for _, retired := range retiredCoreSkillNames {
		require.NotContains(t, string(agents), retired)
	}

	require.Contains(t, stdout.String(), "✓ Created")
}

func TestRun_PreservesUnknownConfigKeys(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, ".rhizome"), 0o755))
	configPath := filepath.Join(root, ".rhizome", "config.yml")
	before := []byte("vault:\n  root: docs\nagents:\n  futureHarness: enabled\n")
	require.NoError(t, os.WriteFile(configPath, before, 0o644))

	err := Run(RunOptions{
		Dir:    root,
		Stdout: io.Discard,
		Stderr: io.Discard,
	})
	require.NoError(t, err)

	after, readErr := os.ReadFile(configPath)
	require.NoError(t, readErr)
	require.Contains(t, string(after), "vault:\n  root: docs\n")
	require.Contains(t, string(after), "futureHarness: enabled")
}

func TestRun_MigratesV049WorkflowConfigLosslesslyAndIdempotently(t *testing.T) {
	root := t.TempDir()
	rhizomeDir := filepath.Join(root, ".rhizome")
	require.NoError(t, os.MkdirAll(rhizomeDir, 0o755))
	configPath := filepath.Join(rhizomeDir, "config.yml")
	require.NoError(t, os.WriteFile(configPath, []byte(`rhizome:
  version: 0.49.0
notes:
  includes:
    - docs/**/*.md
code:
  enabled: true
workflowTemplates:
  - spec-driven
workflowTemplateAddons:
  enabled:
    - action-items
workflowTemplateManagement:
  ejected:
    - project-kb
  updatePolicy:
    docs: never
  sourceFingerprints:
    spec-driven:docs:docs/specs/README.md: abc123
`), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(rhizomeDir, "workflows.yml"), []byte(`templates:
  - stale-template
addons:
  enabled:
    - complex-domain
management:
  updatePolicy:
    skills: never
  futureManagementMode: cautious
futureWorkflowState:
  owner: user
`), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(rhizomeDir, ".gitignore"), []byte("*\n!.gitignore\n!config.yml\n!ignore\n"), 0o644))

	run := func() {
		t.Helper()
		require.NoError(t, Run(RunOptions{Dir: root, Stdout: io.Discard, Stderr: io.Discard}))
	}
	run()

	configAfterFirst, err := os.ReadFile(configPath)
	require.NoError(t, err)
	require.NotContains(t, string(configAfterFirst), "workflowTemplates:")
	require.NotContains(t, string(configAfterFirst), "workflowTemplateAddons:")
	require.NotContains(t, string(configAfterFirst), "workflowTemplateManagement:")
	require.Contains(t, string(configAfterFirst), "version: 0.49.0")

	workflowAfterFirst, exists, err := obsidian.LoadLocalWorkflowConfig(root)
	require.NoError(t, err)
	require.True(t, exists)
	require.Equal(t, []string{templateAgenticEngineering}, workflowAfterFirst.Templates)
	require.Equal(t, []string{templateActionItems}, workflowAfterFirst.Addons.Enabled)
	require.Equal(t, []string{templateProjectKB}, workflowAfterFirst.Management.Ejected)
	// Family update policies and source fingerprints are retired; the
	// generated-files record replaces them.
	require.Empty(t, workflowAfterFirst.Management.UpdatePolicy)
	require.Empty(t, workflowAfterFirst.Management.SourceFingerprints)
	workflowRawAfterFirst, err := os.ReadFile(filepath.Join(rhizomeDir, "workflows.yml"))
	require.NoError(t, err)
	require.Contains(t, string(workflowRawAfterFirst), "futureWorkflowState:\n  owner: user\n")
	require.Contains(t, string(workflowRawAfterFirst), "futureManagementMode: cautious")
	gitignoreAfterFirst, err := os.ReadFile(filepath.Join(rhizomeDir, ".gitignore"))
	require.NoError(t, err)
	require.Contains(t, string(gitignoreAfterFirst), "!workflows.yml")
	_, err = obsidian.LoadLocalConfig(root)
	require.NoError(t, err)

	workflowBytesAfterFirst, err := os.ReadFile(filepath.Join(rhizomeDir, "workflows.yml"))
	require.NoError(t, err)
	run()
	configAfterSecond, err := os.ReadFile(configPath)
	require.NoError(t, err)
	workflowAfterSecond, err := os.ReadFile(filepath.Join(rhizomeDir, "workflows.yml"))
	require.NoError(t, err)
	gitignoreAfterSecond, err := os.ReadFile(filepath.Join(rhizomeDir, ".gitignore"))
	require.NoError(t, err)
	require.Equal(t, configAfterFirst, configAfterSecond)
	require.Equal(t, workflowBytesAfterFirst, workflowAfterSecond)
	require.Equal(t, gitignoreAfterFirst, gitignoreAfterSecond)
}

func TestRun_MigratesLegacyWorkflowConfigWhenUserKeepsPreferences(t *testing.T) {
	root := t.TempDir()
	rhizomeDir := filepath.Join(root, ".rhizome")
	require.NoError(t, os.MkdirAll(rhizomeDir, 0o755))
	configPath := filepath.Join(rhizomeDir, "config.yml")
	require.NoError(t, os.WriteFile(configPath, []byte(`rhizome:
  version: 0.49.0
notes:
  includes:
    - docs/**/*.md
workflowTemplates:
  - core
workflowTemplateAddons:
  enabled:
    - action-items
workflowTemplateManagement:
  updatePolicy:
    docs: never
`), 0o644))

	var stdout bytes.Buffer
	require.NoError(t, Run(RunOptions{
		Interactive: true,
		Dir:         root,
		Stdout:      &stdout,
		Stderr:      io.Discard,
		Stdin:       bytes.NewBufferString("\n"),
	}))

	configAfter, err := os.ReadFile(configPath)
	require.NoError(t, err)
	require.NotContains(t, string(configAfter), "workflowTemplates:")
	require.NotContains(t, string(configAfter), "workflowTemplateAddons:")
	require.NotContains(t, string(configAfter), "workflowTemplateManagement:")

	workflowAfter, exists, err := obsidian.LoadLocalWorkflowConfig(root)
	require.NoError(t, err)
	require.True(t, exists)
	require.Equal(t, []string{templateCore}, workflowAfter.Templates)
	require.Equal(t, []string{templateActionItems}, workflowAfter.Addons.Enabled)
	require.Empty(t, workflowAfter.Management.UpdatePolicy)
	require.Contains(t, stdout.String(), "Apply? [Y/n/s for settings]")
	require.Contains(t, stdout.String(), "Migrated v0.49 workflow state to ")
}

func TestRun_V049SplitWorkflowKeepsExistingConfigPrompt(t *testing.T) {
	root := t.TempDir()
	writeProjectConfig(t, root, "rhizome:\n  version: 0.49.0\n", obsidian.LocalWorkflowConfig{
		Templates: []string{templateCore},
		Management: obsidian.WorkflowTemplateManagement{
			Ejected: []string{templateActionItems},
		},
	})

	var stdout bytes.Buffer
	require.NoError(t, Run(RunOptions{
		Interactive: true,
		Dir:         root,
		Stdout:      &stdout,
		Stderr:      io.Discard,
		Stdin:       bytes.NewBufferString("\n"),
	}))

	require.Contains(t, stdout.String(), "Rhizome is set up in")
	require.NotContains(t, stdout.String(), "Settings")
	workflowAfter, exists, err := obsidian.LoadLocalWorkflowConfig(root)
	require.NoError(t, err)
	require.True(t, exists)
	require.Equal(t, []string{templateCore}, workflowAfter.Templates)
	require.Equal(t, []string{templateActionItems}, workflowAfter.Management.Ejected)
}

func TestMigrateLegacyWorkflowConfigOnlyReplacesPresentLegacySections(t *testing.T) {
	root := t.TempDir()
	rhizomeDir := filepath.Join(root, ".rhizome")
	require.NoError(t, os.MkdirAll(rhizomeDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(rhizomeDir, "config.yml"), []byte(`workflowTemplates:
  - spec-driven
`), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(rhizomeDir, "workflows.yml"), []byte(`templates:
  - stale-template
addons:
  enabled:
    - action-items
management:
  updatePolicy:
    skills: never
  futureManagementMode: cautious
`), 0o644))

	_, migrated, err := migrateLegacyWorkflowConfig(root)
	require.NoError(t, err)
	require.True(t, migrated)

	workflow, exists, err := obsidian.LoadLocalWorkflowConfig(root)
	require.NoError(t, err)
	require.True(t, exists)
	require.Equal(t, []string{templateAgenticEngineering}, workflow.Templates)
	require.Equal(t, []string{templateActionItems}, workflow.Addons.Enabled)
	require.Equal(t, "never", workflow.Management.UpdatePolicy.Skills)
	raw, err := os.ReadFile(filepath.Join(rhizomeDir, "workflows.yml"))
	require.NoError(t, err)
	require.Contains(t, string(raw), "futureManagementMode: cautious")
}

func TestNormalizeTemplateNamesRejectsRetiredProjectKB(t *testing.T) {
	_, err := normalizeTemplateNames([]string{templateProjectKB})
	require.ErrorContains(t, err, "project-kb starter has been removed")
	_, err = normalizeTemplateList("project-kb,agentic-engineering")
	require.ErrorContains(t, err, "project-kb starter has been removed")
}

func TestRun_RetiredProjectKBSelectionPreservesInstalledFiles(t *testing.T) {
	root := t.TempDir()
	writeProjectConfig(t, root, "notes:\n  includes: [\"**/*.md\"]\n", obsidian.LocalWorkflowConfig{
		Templates: []string{templateProjectKB},
	})
	installed := filepath.Join(root, "docs", "projects", "my-project.md")
	require.NoError(t, os.MkdirAll(filepath.Dir(installed), 0o755))
	require.NoError(t, os.WriteFile(installed, []byte("# My project\n"), 0o644))

	err := Run(RunOptions{Dir: root, Stdout: io.Discard, Stderr: io.Discard})
	require.ErrorContains(t, err, "project-kb starter has been removed")
	content, readErr := os.ReadFile(installed)
	require.NoError(t, readErr)
	require.Equal(t, "# My project\n", string(content))
	workflow, exists, readErr := obsidian.LoadLocalWorkflowConfig(root)
	require.NoError(t, readErr)
	require.True(t, exists)
	require.Equal(t, []string{templateProjectKB}, workflow.Templates)
}

func TestNormalizeTemplateNamesAllowsComposition(t *testing.T) {
	got, err := normalizeTemplateList("action-items,spec-driven")
	require.NoError(t, err)
	require.Equal(t, []string{templateActionItems, templateAgenticEngineering}, got)

	got, err = normalizeTemplateList("spec-driven, action-items , spec-driven")
	require.NoError(t, err)
	require.Equal(t, []string{templateActionItems, templateAgenticEngineering}, got)
}

func TestNormalizeTemplateNamesSupportsComplexDomain(t *testing.T) {
	got, err := normalizeTemplateList("complex-domain,spec-driven")
	require.NoError(t, err)
	require.Equal(t, []string{templateAgenticEngineering, templateComplexDomain}, got)
}

func TestParseWorkflowOption(t *testing.T) {
	for raw, want := range map[string][]string{
		"agentic-engineering": {templateAgenticEngineering},
		"domain":              {templateComplexDomain},
		"none":                nil,
		"core,action-items":   {templateActionItems, templateCore},
	} {
		got, err := parseWorkflowOption(raw)
		require.NoError(t, err, raw)
		require.Equal(t, want, got, raw)
	}
	_, err := parseWorkflowOption("bogus")
	require.Error(t, err)
}

func TestInferTemplateChoicesIgnoresGenericProjectDocs(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, "docs", "projects"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "docs", "projects", "README.md"), []byte("# Projects\n"), 0o644))

	got, err := inferTemplateChoices(root, &obsidian.LocalConfig{})
	require.NoError(t, err)
	require.Empty(t, got)
}

func TestInferTemplateChoicesDetectsComplexDomainSpecificSkill(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, ".agents", "skills", "complex-domain"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, ".agents", "skills", "complex-domain", "SKILL.md"), []byte("---\nname: complex-domain\n---\n"), 0o644))

	got, err := inferTemplateChoices(root, &obsidian.LocalConfig{})
	require.NoError(t, err)
	require.Equal(t, []string{templateComplexDomain}, got)
}

func TestInferTemplateChoicesKeepsComplexDomainAsExplicitWhenSpecDrivenMarkersExist(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, ".agents", "skills", "specify"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, ".agents", "skills", "specify", "SKILL.md"), []byte("---\nname: specify\n---\n"), 0o644))
	require.NoError(t, os.MkdirAll(filepath.Join(root, ".agents", "skills", "complex-domain"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, ".agents", "skills", "complex-domain", "SKILL.md"), []byte("---\nname: complex-domain\n---\n"), 0o644))

	got, err := inferTemplateChoices(root, &obsidian.LocalConfig{})
	require.NoError(t, err)
	require.Equal(t, []string{templateComplexDomain}, got)
}

func TestRun_FastPathWithExplicitTemplateStillScaffoldsStarter(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, ".rhizome"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, ".rhizome", "config.yml"), []byte("notes:\n  includes: [\"**/*.md\"]\n"), 0o644))

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	err := Run(RunOptions{
		Dir:         root,
		Interactive: true,
		Workflow:    templateAgenticEngineering,
		Stdout:      &stdout,
		Stderr:      &stderr,
		Stdin:       bytes.NewBufferString("\n"),
	})
	require.NoError(t, err)

	schema, err := os.ReadFile(filepath.Join(root, ".rhizome", "ontology", "spec-driven.graphql"))
	require.NoError(t, err)
	require.Contains(t, string(schema), "type EffortNote")
	workflowDoc, err := os.ReadFile(filepath.Join(root, "docs", "engineering", "testing-policy.md"))
	require.NoError(t, err)
	require.Contains(t, string(workflowDoc), "# Testing policy")
	require.Contains(t, stdout.String(), "✓ Created")
}

func TestRun_TemplateComplexDomainWithAgenticEngineeringDedupes(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "README.md"), []byte("# demo\n"), 0o644))

	var stdout bytes.Buffer
	err := Run(RunOptions{
		Dir:      root,
		Workflow: templateAgenticEngineering + "," + templateComplexDomain,
		Stdout:   &stdout,
		Stderr:   io.Discard,
	})
	require.NoError(t, err)

	cfg, err := obsidian.LoadLocalConfig(root)
	require.NoError(t, err)
	require.Equal(t, []string{templateAgenticEngineering, templateComplexDomain}, cfg.WorkflowTemplates)
	agents, err := os.ReadFile(filepath.Join(root, "AGENTS.md"))
	require.NoError(t, err)
	require.Equal(t, 1, strings.Count(string(agents), managedTemplateBlockStartPrefix+templateAgenticEngineering+managedTemplateBlockSuffix))
	require.Equal(t, 1, strings.Count(string(agents), managedTemplateBlockStartPrefix+templateComplexDomain+managedTemplateBlockSuffix))
	require.FileExists(t, filepath.Join(root, ".agents", "skills", "action-items", "SKILL.md"), "the default action-items addon resolves")

	complexOnlyRoot := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(complexOnlyRoot, "README.md"), []byte("# demo\n"), 0o644))
	err = Run(RunOptions{
		Dir:      complexOnlyRoot,
		Workflow: templateComplexDomain,
		Stdout:   io.Discard,
		Stderr:   io.Discard,
	})
	require.NoError(t, err)

	for _, skill := range []string{"agentic-engineering", "foundation-review", "ingest-transcript"} {
		combinedSkill, err := os.ReadFile(filepath.Join(root, ".agents", "skills", skill, "SKILL.md"))
		require.NoError(t, err)
		complexOnlySkill, err := os.ReadFile(filepath.Join(complexOnlyRoot, ".agents", "skills", skill, "SKILL.md"))
		require.NoError(t, err)
		require.Equal(t, string(complexOnlySkill), string(combinedSkill), "skill %s should render identically with explicit agentic-engineering", skill)
	}
}

func TestRun_TemplateActionItemsResolvesCore(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "README.md"), []byte("# demo\n"), 0o644))

	var stdout bytes.Buffer
	err := Run(RunOptions{
		Dir:      root,
		Workflow: templateActionItems,
		Stdout:   &stdout,
		Stderr:   io.Discard,
	})
	require.NoError(t, err)

	require.FileExists(t, filepath.Join(root, ".rhizome", "ontology", "core.graphql"))
	require.FileExists(t, filepath.Join(root, ".rhizome", "ontology", "action-items.graphql"))
	viewPath := filepath.Join(root, ".rhizome", "views", "action-items.yaml")
	require.FileExists(t, viewPath)
	viewData, err := os.ReadFile(viewPath)
	require.NoError(t, err)
	var view struct {
		Defaults struct {
			Group struct {
				Field  string `yaml:"field"`
				Values []struct {
					Value     string `yaml:"value"`
					Label     string `yaml:"label"`
					Order     int    `yaml:"order"`
					Collapsed bool   `yaml:"collapsed"`
				} `yaml:"values"`
			} `yaml:"group"`
			Sort []struct {
				Field string `yaml:"field"`
			} `yaml:"sort"`
		} `yaml:"defaults"`
	}
	require.NoError(t, yaml.Unmarshal(viewData, &view))
	require.Equal(t, "done", view.Defaults.Group.Field)
	require.Equal(t, "false", view.Defaults.Group.Values[0].Value)
	require.Equal(t, "Open", view.Defaults.Group.Values[0].Label)
	require.False(t, view.Defaults.Group.Values[0].Collapsed)
	require.Equal(t, "true", view.Defaults.Group.Values[1].Value)
	require.Equal(t, "Done", view.Defaults.Group.Values[1].Label)
	require.True(t, view.Defaults.Group.Values[1].Collapsed)
	require.Equal(t, "done", view.Defaults.Sort[0].Field)
	require.FileExists(t, filepath.Join(root, ".rhizome", "ontology", "core.graphql"), "action-items resolves core")
}

func TestRun_ExistingExplicitWorkflowDoesNotSilentlyAddDefaultAddon(t *testing.T) {
	root := t.TempDir()
	writeProjectConfig(t, root, "notes:\n  includes: [\"**/*.md\"]\n", obsidian.LocalWorkflowConfig{Templates: []string{templateAgenticEngineering}})

	var stdout bytes.Buffer
	err := Run(RunOptions{
		Dir:    root,
		Stdout: &stdout,
		Stderr: io.Discard,
	})
	require.NoError(t, err)

	require.FileExists(t, filepath.Join(root, ".rhizome", "ontology", "core.graphql"))
	require.FileExists(t, filepath.Join(root, ".rhizome", "ontology", "spec-driven.graphql"))
	require.NoFileExists(t, filepath.Join(root, ".rhizome", "ontology", "action-items.graphql"))
	require.NotContains(t, stdout.String(), "action-items")
}

func TestRun_ExistingWorkflowCanOptIntoDefaultAddon(t *testing.T) {
	root := t.TempDir()
	writeProjectConfig(t, root, "notes:\n  includes: [\"**/*.md\"]\n", obsidian.LocalWorkflowConfig{
		Templates: []string{templateAgenticEngineering},
		Addons:    obsidian.WorkflowTemplateAddons{Enabled: []string{templateActionItems}},
	})

	var stdout bytes.Buffer
	err := Run(RunOptions{
		Dir:    root,
		Stdout: &stdout,
		Stderr: io.Discard,
	})
	require.NoError(t, err)

	require.FileExists(t, filepath.Join(root, ".rhizome", "ontology", "core.graphql"))
	require.FileExists(t, filepath.Join(root, ".rhizome", "ontology", "spec-driven.graphql"))
	require.FileExists(t, filepath.Join(root, ".rhizome", "ontology", "action-items.graphql"))
}

func TestRun_InstallsPinnedBinaryWhenMissing(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, ".rhizome"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, ".rhizome", "config.yml"), []byte("rhizome:\n  version: v1.2.3\nnotes:\n  includes: [\"**/*.md\"]\n"), 0o644))

	var called bool
	installPinnedBinary = func(_ context.Context, opts appupdate.EnsureOptions) error {
		called = true
		require.NotNil(t, opts.Config)
		require.Equal(t, "v1.2.3", opts.Config.Rhizome.Version)
		expectedRoot, err := filepath.EvalSymlinks(root)
		require.NoError(t, err)
		require.Equal(t, expectedRoot, opts.CfgDir)
		return nil
	}
	t.Cleanup(func() {
		installPinnedBinary = func(context.Context, appupdate.EnsureOptions) error {
			return nil
		}
	})

	var stdout bytes.Buffer
	err := Run(RunOptions{
		Interactive: true,
		Dir:         root,
		Stdout:      &stdout,
		Stderr:      io.Discard,
		Stdin:       bytes.NewBufferString("\n"),
	})
	require.NoError(t, err)
	require.True(t, called)
	require.Contains(t, stdout.String(), "Installing Rhizome v1.2.3 for this repo")
	require.Contains(t, stdout.String(), "repo-local binary missing")
}

func TestEnsurePinnedBinarySkipsMatchingMarker(t *testing.T) {
	root := t.TempDir()
	target := pinnedBinaryPath(root)
	require.NoError(t, os.MkdirAll(filepath.Dir(target), 0o755))
	require.NoError(t, os.WriteFile(target, []byte("#!/bin/sh\n"), 0o755))
	require.NoError(t, appupdate.WriteVersionMarker(target, "v1.2.3"))

	installPinnedBinary = func(context.Context, appupdate.EnsureOptions) error {
		t.Fatal("unexpected pinned install")
		return nil
	}
	t.Cleanup(func() {
		installPinnedBinary = func(context.Context, appupdate.EnsureOptions) error {
			return nil
		}
	})

	var stdout bytes.Buffer
	err := ensurePinnedBinary(root, obsidian.LocalConfig{Rhizome: obsidian.LocalRhizomeConfig{Version: "v1.2.3"}}, RunOptions{Stdout: &stdout})
	require.NoError(t, err)
	require.Empty(t, stdout.String(), "matching marker must not print an install notice")
}

func TestEnsurePinnedBinarySkipsDevBinaryDirWithoutVersionPin(t *testing.T) {
	root := t.TempDir()

	installPinnedBinary = func(context.Context, appupdate.EnsureOptions) error {
		t.Fatal("unexpected pinned install")
		return nil
	}
	t.Cleanup(func() {
		installPinnedBinary = func(context.Context, appupdate.EnsureOptions) error {
			return nil
		}
	})

	var stdout bytes.Buffer
	err := ensurePinnedBinary(root, obsidian.LocalConfig{Rhizome: obsidian.LocalRhizomeConfig{DevBinaryDir: "bin"}}, RunOptions{Stdout: &stdout})
	require.NoError(t, err)
	require.Empty(t, stdout.String())
}

func TestRun_ExistingDevBinaryDirDoesNotAddVersionPin(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, ".rhizome"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "README.md"), []byte("# demo\n"), 0o644))
	cfgPath := filepath.Join(root, ".rhizome", "config.yml")
	require.NoError(t, os.WriteFile(cfgPath, []byte("rhizome:\n  devBinaryDir: bin\n"), 0o644))

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	err := Run(RunOptions{
		Dir:    root,
		Stdout: &stdout,
		Stderr: &stderr,
	})
	require.NoError(t, err)

	cfg, err := obsidian.LoadLocalConfig(root)
	require.NoError(t, err)
	require.Equal(t, "bin", cfg.Rhizome.DevBinaryDir)
	require.Empty(t, cfg.Rhizome.Version)
}

func TestRun_ExternalManagerWritesOwnershipWithoutPinOrInstall(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "README.md"), []byte("# demo\n"), 0o644))

	installPinnedBinary = func(context.Context, appupdate.EnsureOptions) error {
		t.Fatal("external ownership must not install a repo-local binary")
		return nil
	}
	t.Cleanup(func() {
		installPinnedBinary = func(context.Context, appupdate.EnsureOptions) error { return nil }
	})

	err := Run(RunOptions{
		Dir:           root,
		BinaryManager: obsidian.BinaryManagerExternal,
		Stdout:        io.Discard,
		Stderr:        io.Discard,
	})
	require.NoError(t, err)

	cfg, err := obsidian.LoadLocalConfig(root)
	require.NoError(t, err)
	require.Equal(t, obsidian.BinaryManagerExternal, cfg.Rhizome.BinaryManager)
	require.Empty(t, cfg.Rhizome.Version)
	require.NoDirExists(t, filepath.Join(root, ".rhizome", "bin"))
}

func TestRun_PreservesExistingExternalManagerWithoutFlag(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, obsidian.SaveLocalConfig(root, obsidian.LocalConfig{
		Rhizome: obsidian.LocalRhizomeConfig{BinaryManager: obsidian.BinaryManagerExternal},
	}))

	installPinnedBinary = func(context.Context, appupdate.EnsureOptions) error {
		t.Fatal("external ownership must not install a repo-local binary")
		return nil
	}
	t.Cleanup(func() {
		installPinnedBinary = func(context.Context, appupdate.EnsureOptions) error { return nil }
	})

	require.NoError(t, Run(RunOptions{
		Dir:    root,
		Stdout: io.Discard,
		Stderr: io.Discard,
	}))

	cfg, err := obsidian.LoadLocalConfig(root)
	require.NoError(t, err)
	require.Equal(t, obsidian.BinaryManagerExternal, cfg.Rhizome.BinaryManager)
	require.Empty(t, cfg.Rhizome.Version)
}

func TestRun_MigratesPinnedConfigToExternalManagerWithoutDeletingCache(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, obsidian.SaveLocalConfig(root, obsidian.LocalConfig{
		Rhizome: obsidian.LocalRhizomeConfig{
			Version:      "v1.2.3",
			DevBinaryDir: "dev-bin",
			BinaryDir:    "repo-bin",
			BinaryPath:   "tools/rzm",
		},
	}))
	cacheFile := filepath.Join(root, ".rhizome", "bin", "preserve-me")
	require.NoError(t, os.MkdirAll(filepath.Dir(cacheFile), 0o755))
	require.NoError(t, os.WriteFile(cacheFile, []byte("cached"), 0o644))

	require.NoError(t, Run(RunOptions{
		Dir:           root,
		BinaryManager: obsidian.BinaryManagerExternal,
		Stdout:        io.Discard,
		Stderr:        io.Discard,
	}))

	cfg, err := obsidian.LoadLocalConfig(root)
	require.NoError(t, err)
	require.Equal(t, obsidian.BinaryManagerExternal, cfg.Rhizome.BinaryManager)
	require.Empty(t, cfg.Rhizome.Version)
	require.Empty(t, cfg.Rhizome.DevBinaryDir)
	require.Empty(t, cfg.Rhizome.BinaryDir)
	require.Empty(t, cfg.Rhizome.BinaryPath)
	require.FileExists(t, cacheFile)
}

func TestRun_ExternalManagerFlagIsItsOwnConfirmationWithoutATerminal(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, obsidian.SaveLocalConfig(root, obsidian.LocalConfig{
		Rhizome: obsidian.LocalRhizomeConfig{Version: "v1.2.3"},
	}))

	require.NoError(t, Run(RunOptions{
		Dir:           root,
		BinaryManager: obsidian.BinaryManagerExternal,
		Stdout:        io.Discard,
		Stderr:        io.Discard,
	}))

	cfg, err := obsidian.LoadLocalConfig(root)
	require.NoError(t, err)
	require.Equal(t, obsidian.BinaryManagerExternal, cfg.Rhizome.BinaryManager)
	require.Empty(t, cfg.Rhizome.Version)
}

func TestRun_BinaryManagerRejectsUnknownValue(t *testing.T) {
	for _, tt := range []struct {
		name    string
		manager string
		wantErr string
	}{
		{name: "unknown manager", manager: "other", wantErr: `--binary-manager supports only "external"`},
	} {
		t.Run(tt.name, func(t *testing.T) {
			root := t.TempDir()
			err := Run(RunOptions{
				Dir:           root,
				BinaryManager: tt.manager,
				Stdout:        io.Discard,
				Stderr:        io.Discard,
			})
			require.ErrorContains(t, err, tt.wantErr)
			require.NoDirExists(t, filepath.Join(root, ".rhizome"))
		})
	}
}

func TestEnsurePinnedBinaryInstallsOnWrongMarker(t *testing.T) {
	root := t.TempDir()
	target := pinnedBinaryPath(root)
	require.NoError(t, os.MkdirAll(filepath.Dir(target), 0o755))
	require.NoError(t, os.WriteFile(target, []byte("#!/bin/sh\n"), 0o755))
	require.NoError(t, appupdate.WriteVersionMarker(target, "v1.2.2"))

	var called bool
	installPinnedBinary = func(_ context.Context, opts appupdate.EnsureOptions) error {
		called = true
		require.NotNil(t, opts.Config)
		require.Equal(t, "v1.2.3", opts.Config.Rhizome.Version)
		require.Equal(t, root, opts.CfgDir)
		return nil
	}
	t.Cleanup(func() {
		installPinnedBinary = func(context.Context, appupdate.EnsureOptions) error {
			return nil
		}
	})

	var stdout bytes.Buffer
	err := ensurePinnedBinary(root, obsidian.LocalConfig{Rhizome: obsidian.LocalRhizomeConfig{Version: "v1.2.3"}}, RunOptions{Stdout: &stdout})
	require.NoError(t, err)
	require.True(t, called)
	require.Contains(t, stdout.String(), "expected v1.2.3")
}

func TestEnsureRhizomeGitIgnoreWritesManagedFile(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, ensureRhizomeGitIgnore(root))

	data, err := os.ReadFile(filepath.Join(root, ".rhizome", ".gitignore"))
	require.NoError(t, err)
	require.Equal(t, rhizomeGitIgnoreBody(), string(data))
	require.Contains(t, string(data), "!workflows.yml")
	require.Contains(t, string(data), "!query-recipes/")
	require.Contains(t, string(data), "!query-recipes/**")
	require.Contains(t, string(data), "!views/")
	require.Contains(t, string(data), "!views/**")
	require.Contains(t, string(data), "*")
	require.Contains(t, string(data), "!config.yml")
	require.NoError(t, os.WriteFile(filepath.Join(root, ".rhizome", ".gitignore"), []byte("db.sqlite\n"), 0o644))
	require.NoError(t, ensureRhizomeGitIgnore(root))
	data, err = os.ReadFile(filepath.Join(root, ".rhizome", ".gitignore"))
	require.NoError(t, err)
	require.Equal(t, rhizomeGitIgnoreBody(), string(data))
}

func TestRun_NonInteractiveFirstRunUsesTheRecommendedSetup(t *testing.T) {
	mockNoGlobalConfig(t)
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "README.md"), []byte("# demo\n"), 0o644))
	var stdout bytes.Buffer

	require.NoError(t, Run(RunOptions{Dir: root, Stdout: &stdout, Stderr: io.Discard}))

	cfg, err := obsidian.LoadLocalConfig(root)
	require.NoError(t, err)
	require.Equal(t, []string{templateAgenticEngineering}, cfg.WorkflowTemplates)
	require.Nil(t, cfg.NoteEmbeddings, "no key, so semantic search stays off")
	require.FileExists(t, filepath.Join(root, ".agents", "skills", "agentic-engineering", "SKILL.md"))
	require.Contains(t, stdout.String(), "Workflow  Agentic Engineering", "agents see the findings and the workflow chosen for them")
	require.Contains(t, stdout.String(), "Set VOYAGE_API_KEY, then run rzm init, to turn on semantic search.")
	require.Contains(t, stdout.String(), "rzm index  build the search index")
}

func TestRun_NewRepoDefaultsSharedAgentSkillsOn(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "README.md"), []byte("# demo\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, ".gitignore"), []byte("node_modules/\n"), 0o644))

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	err := Run(RunOptions{
		Dir:      root,
		Workflow: "none",
		Stdout:   &stdout,
		Stderr:   &stderr,
	})
	require.NoError(t, err)

	coreSkills, err := loadSkillTemplates()
	require.NoError(t, err)
	require.NotEmpty(t, coreSkills)
	for _, tmpl := range coreSkills {
		require.FileExists(t, filepath.Join(root, ".agents", "skills", tmpl.Name, "SKILL.md"))
	}
	require.FileExists(t, filepath.Join(root, ".agents", "skills", coreRhizomeSkillName, "references", "sessions.md"))
	require.FileExists(t, filepath.Join(root, ".agents", "skills", coreRhizomeSkillName, "references", "ontology-usage.md"))
	for _, retired := range retiredCoreSkillNames {
		require.NoDirExists(t, filepath.Join(root, ".agents", "skills", retired))
	}
	for _, name := range []string{"agentic-engineering", "foundation-review", "ingest-transcript"} {
		require.NoDirExists(t, filepath.Join(root, ".agents", "skills", name))
	}
	for _, path := range []string{
		filepath.Join(".rhizome", "ontology", "spec-driven.graphql"),
		filepath.Join(".rhizome", "ontology", "project-kb.graphql"),
		filepath.Join("docs", "engineering", "testing-policy.md"),
	} {
		require.NoFileExists(t, filepath.Join(root, path))
	}

	require.FileExists(t, filepath.Join(root, "AGENTS.md"))
	cfg, err := obsidian.LoadLocalConfig(root)
	require.NoError(t, err)
	require.Equal(t, version.Version, cfg.Rhizome.Version)
	require.Contains(t, stdout.String(), "skills in .agents/")
	rootIgnore, err := os.ReadFile(filepath.Join(root, ".gitignore"))
	require.NoError(t, err)
	require.Equal(t, "node_modules/\n", string(rootIgnore))
}

func TestRun_RejectsUnmarkedCanonicalRhizomeSkillBeforeWrites(t *testing.T) {
	for _, skillPath := range []string{".agents/skills", ".claude/skills"} {
		t.Run(skillPath, func(t *testing.T) {
			root := t.TempDir()
			require.NoError(t, os.WriteFile(filepath.Join(root, "README.md"), []byte("# demo\n"), 0o644))
			canonical := filepath.Join(root, filepath.FromSlash(skillPath), coreRhizomeSkillName)
			require.NoError(t, os.MkdirAll(canonical, 0o755))
			require.NoError(t, os.WriteFile(filepath.Join(canonical, "SKILL.md"), []byte("# User Rhizome skill\n"), 0o644))

			var stdout bytes.Buffer
			var stderr bytes.Buffer
			err := Run(RunOptions{
				Dir:    root,
				Stdout: &stdout,
				Stderr: &stderr,
			})
			require.Error(t, err)
			assert.Contains(t, err.Error(), "Rhizome skill collision")

			body, readErr := os.ReadFile(filepath.Join(canonical, "SKILL.md"))
			require.NoError(t, readErr)
			assert.Equal(t, "# User Rhizome skill\n", string(body))
			assert.NoFileExists(t, filepath.Join(canonical, ".rhizome-managed"))
			assert.NoFileExists(t, filepath.Join(root, ".rhizome", "config.yml"))
			assert.NoFileExists(t, filepath.Join(root, ".rhizome", ".gitignore"))
			assert.NoFileExists(t, filepath.Join(root, "AGENTS.md"))
			assert.NoFileExists(t, filepath.Join(root, "CLAUDE.md"))
			assert.NoFileExists(t, filepath.Join(root, ".agents", "skills", coreRhizomeSkillName, "references", "sessions.md"))
			assert.NoFileExists(t, filepath.Join(root, ".claude", "skills", coreRhizomeSkillName, "references", "sessions.md"))
			assert.NoFileExists(t, filepath.Join(root, "RHIZOME.md"))
			assert.NoDirExists(t, filepath.Join(root, ".cursor", "rules"))
			assert.NoDirExists(t, filepath.Join(root, ".claude", "commands"))
			assert.NoDirExists(t, filepath.Join(root, ".agents", "skills", "rhizome-onboard"))
		})
	}
}

func TestRun_IncludeIgnoredDoesNotWriteBeforeRhizomeSkillCollision(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, ".git"), 0o755))
	writeFixtureFile(t, root, ".gitignore", "app/\n")
	writeFixtureFile(t, root, "app/go.mod", "module example.com/app\n")
	writeFixtureFile(t, root, "README.md", "# demo\n")
	canonical := filepath.Join(root, ".agents", "skills", coreRhizomeSkillName)
	require.NoError(t, os.MkdirAll(canonical, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(canonical, "SKILL.md"), []byte("# User Rhizome skill\n"), 0o644))

	err := Run(RunOptions{
		Dir:            root,
		IncludeIgnored: []string{"app"},
		Stdout:         io.Discard,
		Stderr:         io.Discard,
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "Rhizome skill collision")

	body, readErr := os.ReadFile(filepath.Join(canonical, "SKILL.md"))
	require.NoError(t, readErr)
	assert.Equal(t, "# User Rhizome skill\n", string(body))
	assert.NoFileExists(t, filepath.Join(root, ".rhizome", "ignore"))
	assert.NoFileExists(t, filepath.Join(root, ".rhizome", "config.yml"))
	assert.NoFileExists(t, filepath.Join(root, "AGENTS.md"))
}

func TestRun_InteractiveReconfigureDefersIgnoredSubtreeUntilCollisionPreflight(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, ".git"), 0o755))
	writeFixtureFile(t, root, ".gitignore", "app/\n")
	writeFixtureFile(t, root, "app/go.mod", "module example.com/app\n")
	writeProjectConfig(t, root, `
agents:
  agentSkills: on
  agentsmd: off
`, obsidian.LocalWorkflowConfig{})
	canonical := filepath.Join(root, ".agents", "skills", coreRhizomeSkillName)
	require.NoError(t, os.MkdirAll(canonical, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(canonical, "SKILL.md"), []byte("# User Rhizome skill\n"), 0o644))

	err := Run(RunOptions{
		Interactive: true,
		Dir:         root,
		Stdout:      io.Discard,
		Stderr:      io.Discard,
		Stdin:       bytes.NewBufferString("y\ncode indexing\ny\n\n\n"),
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "Rhizome skill collision")
	assert.NoFileExists(t, filepath.Join(root, ".rhizome", "ignore"))
	assert.NoFileExists(t, filepath.Join(canonical, ".rhizome-managed"))
}

func TestRun_InteractiveFirstRunDefersIgnoredSubtreeUntilCollisionPreflight(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, ".git"), 0o755))
	writeFixtureFile(t, root, ".gitignore", "app/\n")
	writeFixtureFile(t, root, "app/go.mod", "module example.com/app\n")
	canonical := filepath.Join(root, ".agents", "skills", coreRhizomeSkillName)
	require.NoError(t, os.MkdirAll(canonical, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(canonical, "SKILL.md"), []byte("# User Rhizome skill\n"), 0o644))

	err := Run(RunOptions{
		Interactive: true,
		Dir:         root,
		Stdout:      io.Discard,
		Stderr:      io.Discard,
		// Choose no starter, accept the ignored subtree, enable agents, and
		// accept the recommended setup. The final preflight must reject before
		// the deferred subtree inclusion writes .rhizome/ignore.
		Stdin: bytes.NewBufferString("\n\n\n\n"),
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "Rhizome skill collision")
	assert.NoFileExists(t, filepath.Join(root, ".rhizome", "ignore"))
	assert.NoFileExists(t, filepath.Join(root, ".rhizome", "config.yml"))
	assert.NoFileExists(t, filepath.Join(canonical, ".rhizome-managed"))
}

func TestRun_TemplateAgenticEngineeringScaffoldsDocs(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "README.md"), []byte("# demo\n"), 0o644))

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	err := Run(RunOptions{
		Dir:      root,
		Workflow: templateAgenticEngineering,
		Stdout:   &stdout,
		Stderr:   &stderr,
	})
	require.NoError(t, err)

	schema, err := os.ReadFile(filepath.Join(root, ".rhizome", "ontology", "spec-driven.graphql"))
	require.NoError(t, err)
	sourceSchema, err := os.ReadFile(filepath.Join("templates", "starters", templateAgenticEngineering, "rhizome", "ontology", "spec-driven.graphql"))
	require.NoError(t, err)
	require.Equal(t, string(sourceSchema), string(schema))
	require.Contains(t, string(schema), "type EffortNote")
	require.Contains(t, string(schema), `@identifier(strategy: DATETIME, preferred: true, prefix: "EFF")`)
	require.Contains(t, string(schema), "type FeatureArea")
	require.Contains(t, string(schema), "specs as cohesive contracts, not patch logs")
	require.Contains(t, string(schema), "Write requirements as testable MUST/SHOULD/MAY obligations")
	require.Contains(t, string(schema), "Required story identifier and block target")
	require.Contains(t, string(schema), "Do not author an `id::` field for criteria")
	require.Contains(t, string(schema), "mint a plain block locator")
	require.Contains(t, string(schema), "Write each criterion as one direct unordered list item")
	require.Contains(t, string(schema), "summary: String! @field(sourceKind: ITEM_SUMMARY)")
	require.Contains(t, string(schema), "reuse the durable wikilinks from `Stories In Scope (Frozen)`")
	require.Contains(t, string(schema), "For complex efforts, include goal/outcome, selected scope, current-state gap")
	require.Contains(t, string(schema), "Treat a sparse plan without concrete tasks, validation, documentation, and phase exit criteria as not decision-complete")
	require.NotContains(t, string(schema), "minted lazily")
	require.NoFileExists(t, filepath.Join(root, "ontology", "spec-driven.graphql"))
	require.NoFileExists(t, filepath.Join(root, ".rhizome", "ontology", "schema.graphql"))

	workflow, err := os.ReadFile(filepath.Join(root, "docs", "engineering", "testing-policy.md"))
	require.NoError(t, err)
	require.Contains(t, string(workflow), "# Testing policy")
	require.Contains(t, string(workflow), "## Layers")
	require.Contains(t, string(workflow), "## Team extensions")
	index, err := os.ReadFile(filepath.Join(root, "docs", "engineering", "README.md"))
	require.NoError(t, err)
	require.Contains(t, string(index), "## Precedence")
	for _, concern := range []string{"quality-gates.md", "documentation.md", "review-and-approval.md", "architecture.md", "release.md"} {
		require.FileExists(t, filepath.Join(root, "docs", "engineering", concern))
	}
	require.NoFileExists(t, filepath.Join(root, "docs", "engineering", "workflow.md"))
	require.NoFileExists(t, filepath.Join(root, "docs", "engineering", "efforts.md"))

	referenceHub, err := os.ReadFile(filepath.Join(root, "docs", "reference", "README.md"))
	require.NoError(t, err)
	require.Contains(t, string(referenceHub), "Reference docs preserve durable supporting context")
	require.NotContains(t, string(referenceHub), "DocumentationHub")

	agents, err := os.ReadFile(filepath.Join(root, "AGENTS.md"))
	require.NoError(t, err)
	require.Contains(t, string(agents), managedTemplateBlockStartPrefix+templateAgenticEngineering+managedTemplateBlockSuffix)
	require.Contains(t, string(agents), "## Agentic Engineering")
	require.Contains(t, string(agents), "## Route")
	require.Contains(t, string(agents), "docs/engineering/README.md")
	require.Contains(t, string(agents), "## Precedence and boundaries")
	require.Contains(t, string(agents), "`agentic-engineering implement`")
	require.Contains(t, string(agents), "`foundation-review`")
	require.Contains(t, string(agents), "## Installed Skills")
	require.NotContains(t, string(agents), "docs/specs/process/")

	cfg, err := obsidian.LoadLocalConfig(root)
	require.NoError(t, err)
	require.Equal(t, []string{templateAgenticEngineering}, cfg.WorkflowTemplates)

	// Team-owned local policy documents scaffold under docs/engineering/.
	for _, name := range []string{
		"README.md",
		"testing-policy.md",
		"quality-gates.md",
		"documentation.md",
		"review-and-approval.md",
		"architecture.md",
		"release.md",
	} {
		require.FileExists(t, filepath.Join(root, "docs", "engineering", name))
	}
	require.NoDirExists(t, filepath.Join(root, "docs", "specs", "process"))
	require.FileExists(t, filepath.Join(root, "docs", "reference", "guides", "ontology-driven-transcript-ingestion.md"))
	require.FileExists(t, filepath.Join(root, "docs", "meetings", "README.md"))

	require.Contains(t, stdout.String(), "Schema, saved queries, and views in .rhizome/")
	// Installed starter resources share this Run fixture.
	// The router and phase adapters land under .agents/skills/.
	for _, name := range []string{
		"agentic-engineering",
		"foundation-review",
		"ingest-transcript",
	} {
		require.FileExists(t, filepath.Join(root, ".agents", "skills", name, "SKILL.md"))
	}
	for _, retired := range []string{"development-loop", "alignment-audit", "backport", "code-docs", "compound", "debugging", "quality-gates-check", "refactor-planning", "rhizome-review-feedback", "specify", "effort-new", "plan", "implement", "effort-finish"} {
		require.NoDirExists(t, filepath.Join(root, ".agents", "skills", retired))
	}
	recipePath := filepath.Join(root, ".rhizome", "query-recipes", "spec-driven.yaml")
	require.FileExists(t, recipePath)
	recipeBody, err := os.ReadFile(recipePath)
	require.NoError(t, err)
	require.Contains(t, string(recipeBody), "id: effort-execution-context")
	require.Contains(t, string(recipeBody), "id: frozen-spec-index-pack")
	require.Contains(t, string(recipeBody), "id: story-acceptance-pack")
	require.Contains(t, string(recipeBody), "locator { wikilink requiresFix linkTarget { blockId } }")
	require.Contains(t, string(recipeBody), "note.technicalStories.stories.acceptanceCriteria.criteria.locator.wikilink")
	require.Contains(t, string(recipeBody), "A planned locator does not exist yet and must never be persisted as a citation")
	requireInstalledQueryRecipesValidate(t, root)
	require.NoDirExists(t, filepath.Join(root, ".agents", "skills", "audit"))
	require.NoDirExists(t, filepath.Join(root, ".agents", "skills", "rhizome-debugging"))
	require.NoDirExists(t, filepath.Join(root, ".agents", "skills", "_shared"))

	routerBody, err := os.ReadFile(filepath.Join(root, ".agents", "skills", "agentic-engineering", "SKILL.md"))
	require.NoError(t, err)
	require.Contains(t, string(routerBody), "Choose the phase, then start with its one reference")
	for _, reference := range []string{"workflow-state.md", "specification.md", "effort-setup.md", "planning.md", "batch-plan-template.md", "implementation.md", "quality-gates.md", "alignment.md", "reconciliation.md", "compounding.md", "closure.md", "closure-report-contract.md"} {
		require.FileExists(t, filepath.Join(root, ".agents", "skills", "agentic-engineering", "references", reference))
	}

	specifyBody, err := os.ReadFile(filepath.Join(root, ".agents", "skills", "agentic-engineering", "references", "specification.md"))
	require.NoError(t, err)
	require.Contains(t, string(specifyBody), "one coherent spec with observable commitments")
	require.Contains(t, string(specifyBody), "references/spec-template.md")
	specTemplateBody, err := os.ReadFile(filepath.Join(root, ".agents", "skills", "agentic-engineering", "references", "spec-template.md"))
	require.NoError(t, err)
	require.Contains(t, string(specTemplateBody), "type: ProductSpec")
	require.Contains(t, string(specTemplateBody), "## Goals")
	require.Contains(t, string(specTemplateBody), "## Non-Goals")
	require.Contains(t, string(specTemplateBody), "id:: ^SPEC-0007-US1")
	require.NotContains(t, string(specTemplateBody), "id:: ^SPEC-0007-US1-AC1")
	require.Contains(t, string(specTemplateBody), "direct acceptance-criterion bullets")
	require.Contains(t, string(specTemplateBody), "A planned locator must not be cited")
	require.Contains(t, string(specifyBody), "Record a deviation on each of those efforts or explicitly refreeze")
	require.NotContains(t, string(specifyBody), "complex-domain")
	require.NotContains(t, string(specifyBody), "domain-topic-survey")
	require.NotContains(t, string(specifyBody), "rzm:skill-slot")
	require.FileExists(t, filepath.Join(root, ".agents", "skills", "action-items", "SKILL.md"))

	planBody, err := os.ReadFile(filepath.Join(root, ".agents", "skills", "agentic-engineering", "references", "planning.md"))
	require.NoError(t, err)
	require.Contains(t, string(planBody), "an executable plan written into the effort")
	require.Contains(t, string(planBody), "request a `foundation-review` exit before dependent work")
	require.Contains(t, string(planBody), "references/batch-plan-template.md")

	effortSetupBody, err := os.ReadFile(filepath.Join(root, ".agents", "skills", "agentic-engineering", "references", "effort-setup.md"))
	require.NoError(t, err)
	require.Contains(t, string(effortSetupBody), "freezes durable links to the exact specs")
	require.Contains(t, string(effortSetupBody), "A planned block ID is a proposed source edit, not a durable link")
	require.Contains(t, string(effortSetupBody), "the human approves frozen scope")

	closureBody, err := os.ReadFile(filepath.Join(root, ".agents", "skills", "agentic-engineering", "references", "closure.md"))
	require.NoError(t, err)
	require.Contains(t, string(closureBody), "a truthful closure state")
	require.Contains(t, string(closureBody), "complete closure directly")
	for _, retired := range []string{"specify", "effort-new", "plan", "implement", "effort-finish"} {
		require.NoDirExists(t, filepath.Join(root, ".agents", "skills", retired))
	}

	ontologyBody, err := os.ReadFile(filepath.Join(root, ".agents", "skills", coreRhizomeSkillName, "references", "ontology-authoring.md"))
	require.NoError(t, err)
	require.Contains(t, string(ontologyBody), "high-risk route")
	require.Contains(t, string(ontologyBody), "content migration")
	require.Contains(t, string(ontologyBody), "Never hand-edit generated agent-surface copies")

	// Starter-only helper directories do not scaffold under the project root as
	// plain directories.
	for _, dir := range []string{"agents", "repo", "rhizome"} {
		_, err = os.Stat(filepath.Join(root, dir))
		require.ErrorIs(t, err, os.ErrNotExist)
	}
	require.FileExists(t, filepath.Join(root, ".rhizome", "ontology", "core.graphql"))
	require.FileExists(t, filepath.Join(root, ".rhizome", "ontology", "spec-driven.graphql"))
	require.FileExists(t, filepath.Join(root, ".rhizome", "ontology", "action-items.graphql"))
	require.FileExists(t, filepath.Join(root, ".rhizome", "query-recipes", "action-items.yaml"))
	require.FileExists(t, filepath.Join(root, ".rhizome", "views", "action-items.yaml"))
	require.Equal(t, []string{templateActionItems}, cfg.WorkflowTemplateAddons.Enabled)
}
func TestRun_TemplateComplexDomainScaffoldsDocs(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "README.md"), []byte("# demo\n"), 0o644))

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	err := Run(RunOptions{
		Dir:      root,
		Workflow: templateComplexDomain,
		Stdout:   &stdout,
		Stderr:   &stderr,
	})
	require.NoError(t, err)

	schema, err := os.ReadFile(filepath.Join(root, ".rhizome", "ontology", "complex-domain.graphql"))
	require.NoError(t, err)
	sourceSchema, err := os.ReadFile(filepath.Join("templates", "starters", templateComplexDomain, "rhizome", "ontology", "complex-domain.graphql"))
	require.NoError(t, err)
	require.Equal(t, string(sourceSchema), string(schema))
	require.Contains(t, string(schema), "type RequirementSource")
	require.Contains(t, string(schema), "type Requirement")
	require.Contains(t, string(schema), "type DomainContext")
	require.NotContains(t, string(schema), "type DomainQuestion")
	require.NotContains(t, string(schema), "type CoverageReview")
	require.NoFileExists(t, filepath.Join(root, "ontology", "complex-domain.graphql"))

	recipePath := filepath.Join(root, ".rhizome", "query-recipes", "complex-domain.yaml")
	require.FileExists(t, recipePath)
	recipeBody, err := os.ReadFile(recipePath)
	require.NoError(t, err)
	require.Contains(t, string(recipeBody), "id: domain-context-pack")
	require.Contains(t, string(recipeBody), "id: requirement-trace-pack")
	require.Contains(t, string(recipeBody), "id: spec-domain-context-pack")
	require.Contains(t, string(recipeBody), "id: feature-area-backlog-pack")
	require.Contains(t, string(recipeBody), "rowPath: notes.nodes")
	requireInstalledQueryRecipesValidate(t, root)

	for _, name := range []string{
		filepath.Join(".rhizome", "views", "requirements-by-feature-area.yaml"),
		filepath.Join(".rhizome", "views", "uncovered-requirements.yaml"),
		filepath.Join(".rhizome", "views", "requirements-by-process.yaml"),
		filepath.Join(".rhizome", "views", "sources-needing-review.yaml"),
		filepath.Join(".rhizome", "views", "domain-types.yaml"),
		filepath.Join(".rhizome", "views", "requirements-board.yaml"),
		filepath.Join(".rhizome", "views", "efforts.yaml"),
		filepath.Join(".rhizome", "views", "specs.yaml"),
		filepath.Join(".rhizome", "views", "user-stories.yaml"),
		filepath.Join(".rhizome", "views", "feature-areas.yaml"),
	} {
		require.FileExists(t, filepath.Join(root, name))
	}
	requireInstalledViewsValidate(t, root)

	for _, skill := range []string{
		"complex-domain",
		"requirements-ingest",
		"requirements-curation",
		"domain-modeling",
		"workflow-mapping",
		"spec-from-domain",
		"traceability-review",
		"domain-backport",
	} {
		require.FileExists(t, filepath.Join(root, ".agents", "skills", skill, "SKILL.md"))
	}

	specifySkill, err := os.ReadFile(filepath.Join(root, ".agents", "skills", "agentic-engineering", "references", "specification.md"))
	require.NoError(t, err)
	require.Contains(t, string(specifySkill), "domain-topic-survey")
	require.Contains(t, string(specifySkill), "accepted requirements and their source confidence")
	require.NotContains(t, string(specifySkill), "rzm:skill-slot")

	planSkill, err := os.ReadFile(filepath.Join(root, ".agents", "skills", "agentic-engineering", "references", "planning.md"))
	require.NoError(t, err)
	require.Contains(t, string(planSkill), "spec-domain-context-pack")
	require.Contains(t, string(planSkill), "complex-domain.uncovered-requirements")
	require.NotContains(t, string(planSkill), "rzm:skill-slot")

	implementSkill, err := os.ReadFile(filepath.Join(root, ".agents", "skills", "agentic-engineering", "references", "implementation.md"))
	require.NoError(t, err)
	require.Contains(t, string(implementSkill), "domain-backport")
	require.NotContains(t, string(implementSkill), "rzm:skill-slot")
	routerSkill, err := os.ReadFile(filepath.Join(root, ".agents", "skills", "agentic-engineering", "references", "compounding.md"))
	require.NoError(t, err)
	require.Contains(t, string(routerSkill), "For complex-domain compounding")
	require.NotContains(t, string(routerSkill), "rzm:skill-slot")

	for _, name := range []string{
		filepath.Join("docs", "reference", "guides", "complex-domain-workflow.md"),
		filepath.Join("docs", "reference", "domain", "contexts", "README.md"),
		filepath.Join("docs", "reference", "domain", "types", "README.md"),
		filepath.Join("docs", "reference", "domain", "processes", "README.md"),
		filepath.Join("docs", "reference", "domain", "workflows", "README.md"),
		filepath.Join("docs", "reference", "requirements", "requirements", "README.md"),
		filepath.Join("docs", "reference", "requirements", "sources", "README.md"),
	} {
		require.FileExists(t, filepath.Join(root, name))
	}

	agents, err := os.ReadFile(filepath.Join(root, "AGENTS.md"))
	require.NoError(t, err)
	require.Contains(t, string(agents), managedTemplateBlockStartPrefix+templateAgenticEngineering+managedTemplateBlockSuffix)
	require.Contains(t, string(agents), managedTemplateBlockStartPrefix+templateComplexDomain+managedTemplateBlockSuffix)
	require.Contains(t, string(agents), "## Complex Domain")
	require.Contains(t, string(agents), "Use `ActionItem` for ambiguity")
	require.Equal(t, 1, strings.Count(string(agents), managedTemplateBlockStartPrefix+templateAgenticEngineering+managedTemplateBlockSuffix))
	require.Equal(t, 1, strings.Count(string(agents), managedTemplateBlockStartPrefix+templateComplexDomain+managedTemplateBlockSuffix))

	cfg, err := obsidian.LoadLocalConfig(root)
	require.NoError(t, err)
	require.Equal(t, []string{templateComplexDomain}, cfg.WorkflowTemplates)

	require.Contains(t, stdout.String(), "✓ .rhizome/config.yml")
	require.FileExists(t, filepath.Join(root, ".rhizome", "ontology", "core.graphql"))
	require.FileExists(t, filepath.Join(root, ".rhizome", "ontology", "spec-driven.graphql"))
	require.FileExists(t, filepath.Join(root, ".rhizome", "ontology", "complex-domain.graphql"))
	require.FileExists(t, filepath.Join(root, ".rhizome", "views", "requirements-by-feature-area.yaml"))
	require.FileExists(t, filepath.Join(root, "docs", "reference", "guides", "complex-domain-workflow.md"))
	require.FileExists(t, filepath.Join(root, "docs", "feature-areas", "README.md"))
	require.FileExists(t, filepath.Join(root, "docs", "reference", "requirements", "requirements", "README.md"))
}

func TestSpecDrivenStarterQueryRecipesValidateAgainstRepoSchema(t *testing.T) {
	files, err := loadStarterQueryRecipeTemplates(templateAgenticEngineering)
	require.NoError(t, err)
	require.NotEmpty(t, files)

	root := t.TempDir()
	for _, file := range files {
		target := filepath.Join(root, file.Path)
		require.NoError(t, os.MkdirAll(filepath.Dir(target), 0o755))
		require.NoError(t, os.WriteFile(target, file.Content, 0o644))
	}
	recipes, loadIssues := queryrecipe.LoadPath(root)
	require.Empty(t, loadIssues)
	require.NotEmpty(t, recipes)

	repoRoot := filepath.Clean("../../../..")
	schema, err := ontology.LoadSchema(repoRoot)
	require.NoError(t, err)
	execSchema, err := ontologyquery.BuildExecutableSchema(schema)
	require.NoError(t, err)

	result := queryrecipe.Validate(recipes, execSchema)
	require.Empty(t, result.Issues)
}

func TestBundledStarterQueryRecipesValidateAgainstEffectiveSchema(t *testing.T) {
	registry, err := loadStarterTemplateMetadataRegistry()
	require.NoError(t, err)

	starterIDs := make([]string, 0, len(registry))
	for id := range registry {
		starterIDs = append(starterIDs, id)
	}
	sort.Strings(starterIDs)

	for _, starterID := range starterIDs {
		t.Run(starterID, func(t *testing.T) {
			withDefaults, err := resolveTemplateSet([]string{starterID}, obsidian.WorkflowTemplateAddons{}, registry)
			require.NoError(t, err)
			require.NotEmpty(t, withDefaults.Effective)

			defaultAddons := make([]string, 0)
			for id, cause := range withDefaults.Causes {
				if cause == templateCauseDefaultAddon {
					defaultAddons = append(defaultAddons, id)
				}
			}
			sort.Strings(defaultAddons)

			type recipeValidationScenario struct {
				name   string
				addons obsidian.WorkflowTemplateAddons
			}
			scenarios := []recipeValidationScenario{{name: "defaults"}}
			if len(defaultAddons) > 0 {
				scenarios = append(scenarios, recipeValidationScenario{
					name:   "default-addons-disabled",
					addons: obsidian.WorkflowTemplateAddons{Disabled: defaultAddons},
				})
			}

			for _, scenario := range scenarios {
				t.Run(scenario.name, func(t *testing.T) {
					resolved, err := resolveTemplateSet([]string{starterID}, scenario.addons, registry)
					require.NoError(t, err)

					root := t.TempDir()
					recipeCount := 0
					for _, effectiveID := range resolved.Effective {
						meta := registry[effectiveID]
						ontologyFiles, err := loadStarterOntologyTemplates(effectiveID)
						require.NoError(t, err)
						if containsString(meta.InstalledAssetTypes, "ontology") {
							require.NotEmpty(t, ontologyFiles, "starter %s declares ontology assets", effectiveID)
						}
						for _, file := range ontologyFiles {
							target := filepath.Join(root, ".rhizome", "ontology", file.Path)
							require.NoError(t, os.MkdirAll(filepath.Dir(target), 0o755))
							require.NoError(t, os.WriteFile(target, file.Content, 0o644))
						}

						recipeFiles, err := loadStarterQueryRecipeTemplates(effectiveID)
						require.NoError(t, err)
						if containsString(meta.InstalledAssetTypes, "query-recipes") {
							require.NotEmpty(t, recipeFiles, "starter %s declares query-recipe assets", effectiveID)
						}
						recipeCount += len(recipeFiles)
						for _, file := range recipeFiles {
							target := filepath.Join(root, ".rhizome", "query-recipes", file.Path)
							require.NoError(t, os.MkdirAll(filepath.Dir(target), 0o755))
							require.NoError(t, os.WriteFile(target, file.Content, 0o644))
						}
					}

					if recipeCount > 0 {
						requireInstalledQueryRecipesValidate(t, root)
					}
				})
			}
		})
	}
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func requireInstalledQueryRecipesValidate(t *testing.T, root string) {
	t.Helper()

	recipes, loadIssues := queryrecipe.LoadDefaultSources(root)
	require.Empty(t, loadIssues)
	require.NotEmpty(t, recipes)

	schema, err := ontology.LoadSchema(root)
	require.NoError(t, err)
	execSchema, err := ontologyquery.BuildExecutableSchema(schema)
	require.NoError(t, err)

	result := queryrecipe.Validate(recipes, execSchema)
	require.Empty(t, result.Issues)
}

func requireInstalledViewsValidate(t *testing.T, root string) {
	t.Helper()

	views, loadIssues := viewconfig.LoadDefaultSource(root)
	require.Empty(t, loadIssues)
	require.NotEmpty(t, views)

	schema, err := ontology.LoadSchema(root)
	require.NoError(t, err)
	typeNames := make(map[string]struct{}, len(schema.Types))
	for name := range schema.Types {
		typeNames[name] = struct{}{}
	}
	interfaceNames := make(map[string]struct{}, len(schema.Interfaces))
	for name := range schema.Interfaces {
		interfaceNames[name] = struct{}{}
	}

	recipes, recipeIssues := queryrecipe.LoadDefaultSources(root)
	require.Empty(t, recipeIssues)
	recipeIDs := make(map[string]struct{}, len(recipes))
	recipeRowPaths := make(map[string]string, len(recipes))
	for _, recipe := range recipes {
		recipeIDs[recipe.ID] = struct{}{}
		recipeRowPaths[recipe.ID] = recipe.OutputContract.RowPath
	}

	result := viewconfig.Validate(views, viewconfig.ValidateOptions{
		TypeNames:           typeNames,
		InterfaceNames:      interfaceNames,
		QueryRecipeIDs:      recipeIDs,
		QueryRecipeRowPaths: recipeRowPaths,
		CheckReferences:     true,
	})
	require.Empty(t, result.Issues)
}

func TestRun_MultiTemplateInstallsBothSchemas(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "README.md"), []byte("# demo\n"), 0o644))

	err := Run(RunOptions{
		Dir:      root,
		Workflow: templateActionItems + "," + templateAgenticEngineering,
		Stdout:   io.Discard,
		Stderr:   io.Discard,
	})
	require.NoError(t, err)

	specDriven, err := os.ReadFile(filepath.Join(root, ".rhizome", "ontology", "spec-driven.graphql"))
	require.NoError(t, err)
	require.Contains(t, string(specDriven), "type EffortNote")

	actionItems, err := os.ReadFile(filepath.Join(root, ".rhizome", "ontology", "action-items.graphql"))
	require.NoError(t, err)
	require.Contains(t, string(actionItems), "type ActionItem")

	// Legacy unified filename must not appear.
	require.NoFileExists(t, filepath.Join(root, ".rhizome", "ontology", "schema.graphql"))

	cfg, err := obsidian.LoadLocalConfig(root)
	require.NoError(t, err)
	require.Equal(t, []string{templateActionItems, templateAgenticEngineering}, cfg.WorkflowTemplates)

	referenceHub, err := os.ReadFile(filepath.Join(root, "docs", "reference", "README.md"))
	require.NoError(t, err)
	require.Contains(t, string(referenceHub), "Reference docs preserve durable supporting context")
	require.NotContains(t, string(referenceHub), "DocumentationHub")
}

func TestRun_TemplateAgenticEngineeringInstallsBundledClaudeSkills(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, ".claude"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "README.md"), []byte("# demo\n"), 0o644))

	err := Run(RunOptions{
		Dir:      root,
		Workflow: templateAgenticEngineering,
		Stdout:   io.Discard,
		Stderr:   io.Discard,
	})
	require.NoError(t, err)

	for _, name := range []string{
		"agentic-engineering",
		"foundation-review",
		"ingest-transcript",
	} {
		require.FileExists(t, filepath.Join(root, ".claude", "skills", name, "SKILL.md"))
	}
	for _, retired := range []string{"development-loop", "alignment-audit", "backport", "code-docs", "compound", "debugging", "quality-gates-check", "refactor-planning", "rhizome-review-feedback", "specify", "effort-new", "plan", "implement", "effort-finish"} {
		require.NoDirExists(t, filepath.Join(root, ".claude", "skills", retired))
	}
	require.NoDirExists(t, filepath.Join(root, ".claude", "skills", "audit"))
	require.NoDirExists(t, filepath.Join(root, ".claude", "skills", "rhizome-debugging"))
	require.NoDirExists(t, filepath.Join(root, ".claude", "skills", "_shared"))
}

func TestRun_RerunWithoutTemplatePreservesAgenticEngineeringSkills(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "README.md"), []byte("# demo\n"), 0o644))

	err := Run(RunOptions{
		Dir:      root,
		Workflow: templateAgenticEngineering,
		Stdout:   io.Discard,
		Stderr:   io.Discard,
	})
	require.NoError(t, err)

	err = Run(RunOptions{
		Dir:    root,
		Stdout: io.Discard,
		Stderr: io.Discard,
	})
	require.NoError(t, err)

	for _, name := range []string{"agentic-engineering", "foundation-review", "ingest-transcript"} {
		require.FileExists(t, filepath.Join(root, ".agents", "skills", name, "SKILL.md"))
	}
}

func TestRun_ReconfigureCanPromoteDisabledDefaultAddonToExplicitTemplate(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "README.md"), []byte("# demo\n"), 0o644))
	writeProjectConfig(t, root, "notes:\n  includes: [\"**/*.md\"]\n", obsidian.LocalWorkflowConfig{
		Templates: []string{templateAgenticEngineering},
		Addons:    obsidian.WorkflowTemplateAddons{Disabled: []string{templateActionItems}},
	})

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	err := Run(RunOptions{
		Dir:      root,
		Workflow: "agentic-engineering,action-items",
		Stdout:   &stdout,
		Stderr:   &stderr,
	})
	require.NoError(t, err)

	require.FileExists(t, filepath.Join(root, ".rhizome", "ontology", "core.graphql"))
	require.FileExists(t, filepath.Join(root, ".rhizome", "ontology", "spec-driven.graphql"))
	require.FileExists(t, filepath.Join(root, ".rhizome", "ontology", "action-items.graphql"))

	cfg, err := obsidian.LoadLocalConfig(root)
	require.NoError(t, err)
	require.Equal(t, []string{templateActionItems, templateAgenticEngineering}, cfg.WorkflowTemplates)
	require.Empty(t, cfg.WorkflowTemplateAddons.Enabled)
	require.Empty(t, cfg.WorkflowTemplateAddons.Disabled)
}

func TestRun_RerunWithExplicitWorkflowScaffoldsStarter(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, ".rhizome"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, ".rhizome", "config.yml"), []byte("notes:\n  includes: [\"**/*.md\"]\n"), 0o644))

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	err := Run(RunOptions{
		Dir:      root,
		Workflow: templateAgenticEngineering,
		Stdout:   &stdout,
		Stderr:   &stderr,
	})
	require.NoError(t, err)

	schema, err := os.ReadFile(filepath.Join(root, ".rhizome", "ontology", "spec-driven.graphql"))
	require.NoError(t, err)
	require.Contains(t, string(schema), "type EffortNote")
	require.Contains(t, stdout.String(), "✓ Created")
}

func TestRun_RerunWithRefreshDocsRefreshesAgentSurfaces(t *testing.T) {
	root := t.TempDir()
	writeProjectConfig(t, root, "notes:\n  includes: [\"**/*.md\"]\n", obsidian.LocalWorkflowConfig{Templates: []string{templateAgenticEngineering}})

	err := Run(RunOptions{
		Dir:      root,
		Workflow: templateAgenticEngineering,
		Stdout:   io.Discard,
		Stderr:   io.Discard,
	})
	require.NoError(t, err)

	docPath := filepath.Join(root, "docs", "engineering", "testing-policy.md")
	require.NoError(t, os.WriteFile(docPath, []byte("# overwritten\n"), 0o644))

	agentsPath := filepath.Join(root, "AGENTS.md")
	staleAgents := "# Repository Guidelines\n\nstale\n"
	require.NoError(t, os.WriteFile(agentsPath, []byte(staleAgents), 0o644))

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	err = Run(RunOptions{
		Dir:         root,
		RefreshDocs: true,
		Stdout:      &stdout,
		Stderr:      &stderr,
	})
	require.NoError(t, err)

	afterDoc, err := os.ReadFile(docPath)
	require.NoError(t, err)
	require.Equal(t, "# overwritten\n", string(afterDoc), "a refresh never clobbers a team-edited doc")

	afterAgents, err := os.ReadFile(agentsPath)
	require.NoError(t, err)
	require.NotEqual(t, staleAgents, string(afterAgents))
	require.Contains(t, string(afterAgents), "BEGIN RZM INIT RHIZOME BLOCK")
	require.Contains(t, string(afterAgents), "BEGIN RZM INIT TEMPLATE BLOCK: agentic-engineering")
}

func TestRun_RerunPreservesEjectedLegacyStarterSkills(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "README.md"), []byte("# demo\n"), 0o644))

	err := Run(RunOptions{
		Dir:      root,
		Workflow: templateAgenticEngineering,
		Stdout:   io.Discard,
		Stderr:   io.Discard,
	})
	require.NoError(t, err)

	agentsPath := filepath.Join(root, "AGENTS.md")
	agents, err := os.ReadFile(agentsPath)
	require.NoError(t, err)
	legacyAgents := strings.ReplaceAll(string(agents), templateAgenticEngineering, legacyTemplateSpecDriven)
	require.NoError(t, os.WriteFile(agentsPath, []byte(legacyAgents), 0o644))
	legacyFenceStart := managedTemplateBlockStartPrefix + legacyTemplateSpecDriven + managedTemplateBlockSuffix
	legacyFenceEnd := managedTemplateBlockEndPrefix + legacyTemplateSpecDriven + managedTemplateBlockSuffix
	start := strings.Index(legacyAgents, legacyFenceStart)
	end := strings.Index(legacyAgents, legacyFenceEnd)
	require.GreaterOrEqual(t, start, 0)
	require.Greater(t, end, start)
	legacyFence := legacyAgents[start : end+len(legacyFenceEnd)]

	writeProjectConfig(t, root, "notes:\n  includes: [\"**/*.md\"]\n", obsidian.LocalWorkflowConfig{
		Templates: []string{legacyTemplateSpecDriven},
		Management: obsidian.WorkflowTemplateManagement{
			Ejected: []string{legacyTemplateSpecDriven},
		},
	})

	err = Run(RunOptions{
		Dir:    root,
		Stdout: io.Discard,
		Stderr: io.Discard,
	})
	require.NoError(t, err)

	require.FileExists(t, filepath.Join(root, ".agents", "skills", "agentic-engineering", "SKILL.md"))
	require.FileExists(t, filepath.Join(root, ".agents", "skills", "agentic-engineering", "references", "planning.md"))
	updatedAgents, err := os.ReadFile(agentsPath)
	require.NoError(t, err)
	require.Contains(t, string(updatedAgents), legacyFence)
}

func TestRun_EjectTemplatePreservesManagedAgentBlockAndConfiguresManagement(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "README.md"), []byte("# demo\n"), 0o644))

	err := Run(RunOptions{
		Dir:      root,
		Workflow: templateAgenticEngineering,
		Stdout:   io.Discard,
		Stderr:   io.Discard,
	})
	require.NoError(t, err)

	agentsPath := filepath.Join(root, "AGENTS.md")
	specifySkillPath := filepath.Join(root, ".agents", "skills", "agentic-engineering", "SKILL.md")
	require.FileExists(t, specifySkillPath)
	agents, err := os.ReadFile(agentsPath)
	require.NoError(t, err)
	edited := strings.Replace(string(agents), "## Precedence and boundaries", "## Repository precedence and boundaries", 1)
	require.NotEqual(t, string(agents), edited)
	startMarker := managedTemplateBlockStartPrefix + templateAgenticEngineering + managedTemplateBlockSuffix
	endMarker := managedTemplateBlockEndPrefix + templateAgenticEngineering + managedTemplateBlockSuffix
	start := strings.Index(edited, startMarker)
	end := strings.Index(edited, endMarker)
	require.GreaterOrEqual(t, start, 0)
	require.Greater(t, end, start)
	editedTemplateBlock := edited[start : end+len(endMarker)]
	require.NoError(t, os.WriteFile(agentsPath, []byte(edited), 0o644))

	var stdout bytes.Buffer
	err = Run(RunOptions{
		Dir:    root,
		Eject:  templateAgenticEngineering,
		Stdout: &stdout,
		Stderr: io.Discard,
	})
	require.NoError(t, err)

	cfg, err := obsidian.LoadLocalConfig(root)
	require.NoError(t, err)
	require.Equal(t, []string{templateAgenticEngineering}, cfg.WorkflowTemplates)
	require.Equal(t, []string{templateActionItems, templateAgenticEngineering}, cfg.WorkflowTemplateManagement.Ejected)

	afterAgents, err := os.ReadFile(agentsPath)
	require.NoError(t, err)
	afterStart := strings.Index(string(afterAgents), startMarker)
	afterEnd := strings.Index(string(afterAgents), endMarker)
	require.GreaterOrEqual(t, afterStart, 0)
	require.Greater(t, afterEnd, afterStart)
	require.Equal(t, editedTemplateBlock, string(afterAgents)[afterStart:afterEnd+len(endMarker)])
	require.Contains(t, string(afterAgents), "## Repository precedence and boundaries")
	require.Contains(t, string(afterAgents), "BEGIN RZM INIT TEMPLATE BLOCK: agentic-engineering")
	require.FileExists(t, specifySkillPath)
	require.Contains(t, string(afterAgents), "## Core Identity", "the ejected starter's dependencies stay too")
	require.Contains(t, stdout.String(), "Workflow: Agentic Engineering → Search and agent guidance only; Core identity, Action items, and Agentic Engineering files kept for your team")
	require.NotContains(t, stdout.String(), "Remove")
}

func TestRun_EjectShowsInTheChangeList(t *testing.T) {
	root := t.TempDir()
	writeProjectConfig(t, root, "notes:\n  includes: [\"**/*.md\"]\n", obsidian.LocalWorkflowConfig{Templates: []string{templateCore}})

	var stdout bytes.Buffer
	err := Run(RunOptions{
		Interactive: true,
		Dir:         root,
		Eject:       templateCore,
		Agents:      "none",
		Stdin:       bytes.NewBufferString("\n"),
		Stdout:      &stdout,
		Stderr:      io.Discard,
	})
	require.NoError(t, err)

	cfg, err := obsidian.LoadLocalConfig(root)
	require.NoError(t, err)
	require.Equal(t, []string{templateCore}, cfg.WorkflowTemplates)
	require.Equal(t, []string{templateCore}, cfg.WorkflowTemplateManagement.Ejected)
	require.Contains(t, stdout.String(), "Workflow: core → Search and agent guidance only; Core identity files kept for your team")
}

func TestRun_EjectTemplateBlocksRequiredDependencyWithoutCascade(t *testing.T) {
	root := t.TempDir()
	writeProjectConfig(t, root, "notes:\n  includes: [\"**/*.md\"]\n", obsidian.LocalWorkflowConfig{Templates: []string{templateAgenticEngineering}})

	err := Run(RunOptions{
		Dir:    root,
		Eject:  templateCore,
		Stdout: io.Discard,
		Stderr: io.Discard,
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "cannot eject")
	require.Contains(t, err.Error(), templateAgenticEngineering)
}

func TestRun_EjectingDependentsTogetherSucceeds(t *testing.T) {
	root := t.TempDir()
	writeProjectConfig(t, root, "notes:\n  includes: [\"**/*.md\"]\n", obsidian.LocalWorkflowConfig{Templates: []string{templateAgenticEngineering}})

	err := Run(RunOptions{
		Dir:    root,
		Eject:  "core,agentic-engineering,action-items",
		Stdout: io.Discard,
		Stderr: io.Discard,
	})
	require.NoError(t, err)

	cfg, err := obsidian.LoadLocalConfig(root)
	require.NoError(t, err)
	require.Equal(t, []string{templateActionItems, templateAgenticEngineering, templateCore}, cfg.WorkflowTemplateManagement.Ejected)
}

func TestRun_TemplateChangeRemovesUneditedSkillsOfAnInactiveStarter(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "README.md"), []byte("# demo\n"), 0o644))

	err := Run(RunOptions{
		Dir:      root,
		Workflow: templateAgenticEngineering,
		Stdout:   io.Discard,
		Stderr:   io.Discard,
	})
	require.NoError(t, err)
	require.FileExists(t, filepath.Join(root, ".agents", "skills", "agentic-engineering", "SKILL.md"))

	err = Run(RunOptions{
		Dir:      root,
		Workflow: templateCore,
		Stdout:   io.Discard,
		Stderr:   io.Discard,
	})
	require.NoError(t, err)
	require.NoDirExists(t, filepath.Join(root, ".agents", "skills", "agentic-engineering"))
	require.FileExists(t, filepath.Join(root, "docs", "engineering", "testing-policy.md"), "team-owned docs stay")
}

func TestRun_RerunRefreshKeepsEditedTemplateDoc(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "README.md"), []byte("# demo\n"), 0o644))

	err := Run(RunOptions{
		Dir:      root,
		Workflow: templateAgenticEngineering,
		Stdout:   io.Discard,
		Stderr:   io.Discard,
	})
	require.NoError(t, err)

	docPath := filepath.Join(root, "docs", "engineering", "testing-policy.md")
	require.NoError(t, os.WriteFile(docPath, []byte("# overwritten\n"), 0o644))

	err = Run(RunOptions{
		Dir:         root,
		RefreshDocs: true,
		Workflow:    templateAgenticEngineering,
		Stdout:      io.Discard,
		Stderr:      io.Discard,
	})
	require.NoError(t, err)

	after, err := os.ReadFile(docPath)
	require.NoError(t, err)
	require.Equal(t, "# overwritten\n", string(after))
}

func TestRun_DeclinedReconfigureRefreshKeepsEditedTemplateDoc(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "README.md"), []byte("# demo\n"), 0o644))

	err := Run(RunOptions{
		Dir:      root,
		Workflow: templateAgenticEngineering,
		Stdout:   io.Discard,
		Stderr:   io.Discard,
	})
	require.NoError(t, err)

	docPath := filepath.Join(root, "docs", "engineering", "testing-policy.md")
	require.NoError(t, os.WriteFile(docPath, []byte("# overwritten\n"), 0o644))

	err = Run(RunOptions{
		Interactive: true,
		Dir:         root,
		RefreshDocs: true,
		Workflow:    templateAgenticEngineering,
		Stdout:      io.Discard,
		Stderr:      io.Discard,
		Stdin:       bytes.NewBufferString("n\n"),
	})
	require.NoError(t, err)

	after, err := os.ReadFile(docPath)
	require.NoError(t, err)
	require.Equal(t, "# overwritten\n", string(after))
}

func TestRun_WritesAgentsAlongsideOtherHarnesses(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, ".cursor", "rules"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(root, ".codex"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(root, ".claude"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "README.md"), []byte("# demo\n"), 0o644))

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	err := Run(RunOptions{
		Dir:    root,
		Stdout: &stdout,
		Stderr: &stderr,
		Stdin:  bytes.NewBufferString(""),
	})
	require.NoError(t, err)

	agents, err := os.ReadFile(filepath.Join(root, "AGENTS.md"))
	require.NoError(t, err)
	require.Contains(t, string(agents), managedRhizomeBlockStart)
	require.Contains(t, string(agents), "# Rhizome integration")
	require.Contains(t, string(agents), "If an installed `rhizome` skill is available to the active harness")

	claude, err := os.ReadFile(filepath.Join(root, "CLAUDE.md"))
	require.NoError(t, err)
	require.Contains(t, string(claude), managedRhizomeBlockStart)
	require.Contains(t, string(claude), "# Rhizome integration")
	require.Contains(t, string(claude), "If an installed `rhizome` skill is available to the active harness")

	cursorRule, err := os.ReadFile(filepath.Join(root, ".cursor", "rules", "rhizome.mdc"))
	require.NoError(t, err)
	require.Contains(t, string(cursorRule), "alwaysApply: true")
	require.Contains(t, string(cursorRule), "AGENTS.md")
	require.Contains(t, string(cursorRule), "STOP! IMPORTANT!")
	require.Contains(t, string(cursorRule), "CLAUDE.md")

	// Verify that all command templates from templates/commands are written to each agent surface
	cmdTemplates, err := loadCommandTemplates()
	require.NoError(t, err)
	for _, tmpl := range cmdTemplates {
		cursorCmd, err := os.ReadFile(filepath.Join(root, ".cursor", "commands", tmpl.Name))
		require.NoError(t, err, "cursor command %s should exist", tmpl.Name)
		require.NotEmpty(t, string(cursorCmd))

		claudeCommand, err := os.ReadFile(filepath.Join(root, ".claude", "commands", tmpl.Name))
		require.NoError(t, err, "claude command %s should exist", tmpl.Name)
		require.NotEmpty(t, string(claudeCommand))

		claudePrompt, err := os.ReadFile(filepath.Join(root, ".claude", "prompts", tmpl.Name))
		require.NoError(t, err, "claude prompt %s should exist", tmpl.Name)
		require.NotEmpty(t, string(claudePrompt))

		codexPrompt, err := os.ReadFile(filepath.Join(root, ".codex", "prompts", tmpl.Name))
		require.NoError(t, err, "codex prompt %s should exist", tmpl.Name)
		require.NotEmpty(t, string(codexPrompt))

		codexCommand, err := os.ReadFile(filepath.Join(root, ".codex", "commands", tmpl.Name))
		require.NoError(t, err, "codex command %s should exist", tmpl.Name)
		require.NotEmpty(t, string(codexCommand))
	}

	coreSkills, err := loadSkillTemplates()
	require.NoError(t, err)
	for _, tmpl := range coreSkills {
		sharedSkill, err := os.ReadFile(filepath.Join(root, ".agents", "skills", tmpl.Name, "SKILL.md"))
		require.NoError(t, err, "shared skill %s should exist", tmpl.Name)
		require.NotEmpty(t, string(sharedSkill))
	}

	require.Contains(t, stdout.String(), "skills in .agents/")
	require.FileExists(t, filepath.Join(root, ".cursor", "rules", "rhizome.mdc"))
	if len(cmdTemplates) > 0 {
		require.Contains(t, stdout.String(), ".codex (")
	}
}

func TestRun_ForceEnableAgentsCreatesArtifacts(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "README.md"), []byte("# demo\n"), 0o644))

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	err := Run(RunOptions{
		Dir:    root,
		Stdout: &stdout,
		Stderr: &stderr,
		Agents: "claude,codex,cursor",
	})
	require.NoError(t, err)

	agents, err := os.ReadFile(filepath.Join(root, "AGENTS.md"))
	require.NoError(t, err)
	require.Contains(t, string(agents), managedRhizomeBlockStart)

	// Verify that all command templates from templates/commands are written
	cmdTemplates, err := loadCommandTemplates()
	require.NoError(t, err)
	for _, tmpl := range cmdTemplates {
		cursorCmd, err := os.ReadFile(filepath.Join(root, ".cursor", "commands", tmpl.Name))
		require.NoError(t, err, "cursor command %s should exist", tmpl.Name)
		require.NotEmpty(t, string(cursorCmd))

		codexPrompt, err := os.ReadFile(filepath.Join(root, ".codex", "prompts", tmpl.Name))
		require.NoError(t, err, "codex prompt %s should exist", tmpl.Name)
		require.NotEmpty(t, string(codexPrompt))

		claudePrompt, err := os.ReadFile(filepath.Join(root, ".claude", "prompts", tmpl.Name))
		require.NoError(t, err, "claude prompt %s should exist", tmpl.Name)
		require.NotEmpty(t, string(claudePrompt))
	}

	coreSkills, err := loadSkillTemplates()
	require.NoError(t, err)
	for _, tmpl := range coreSkills {
		sharedSkill, err := os.ReadFile(filepath.Join(root, ".agents", "skills", tmpl.Name, "SKILL.md"))
		require.NoError(t, err, "shared skill %s should exist", tmpl.Name)
		require.NotEmpty(t, string(sharedSkill))
	}
}

func TestRun_ForceDisableSkipsArtifacts(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, ".cursor"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(root, ".codex"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(root, ".claude"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "README.md"), []byte("# demo\n"), 0o644))

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	err := Run(RunOptions{
		Dir:    root,
		Stdout: &stdout,
		Stderr: &stderr,
		Agents: "none",
	})
	require.NoError(t, err)

	// Verify that command templates are NOT written when agents are disabled
	cmdTemplates, err := loadCommandTemplates()
	require.NoError(t, err)
	for _, tmpl := range cmdTemplates {
		_, err = os.Stat(filepath.Join(root, ".cursor", "commands", tmpl.Name))
		require.ErrorIs(t, err, os.ErrNotExist, "cursor command %s should not exist", tmpl.Name)

		_, err = os.Stat(filepath.Join(root, ".codex", "prompts", tmpl.Name))
		require.ErrorIs(t, err, os.ErrNotExist, "codex prompt %s should not exist", tmpl.Name)

		_, err = os.Stat(filepath.Join(root, ".claude", "prompts", tmpl.Name))
		require.ErrorIs(t, err, os.ErrNotExist, "claude prompt %s should not exist", tmpl.Name)
	}

	coreSkills, err := loadSkillTemplates()
	require.NoError(t, err)
	for _, tmpl := range coreSkills {
		_, err = os.Stat(filepath.Join(root, ".agents", "skills", tmpl.Name, "SKILL.md"))
		require.ErrorIs(t, err, os.ErrNotExist, "shared skill %s should not exist", tmpl.Name)
	}

	_, err = os.Stat(filepath.Join(root, "AGENTS.md"))
	require.ErrorIs(t, err, os.ErrNotExist)
	_, err = os.Stat(filepath.Join(root, "RHIZOME.md"))
	require.ErrorIs(t, err, os.ErrNotExist)
}

func TestRun_InteractiveSkipsCodeQuestionsWhenNoCodeDetected(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, ".git"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(root, "vault", ".obsidian"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "vault", "note.md"), []byte("# Note\n"), 0o644))

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	err := Run(RunOptions{
		Dir:         root,
		Interactive: true,
		Stdout:      &stdout,
		Stderr:      &stderr,
		Stdin:       bytes.NewBufferString("\n"), // accept recommended setup
	})
	require.NoError(t, err)

	// Should not prompt about code scanning when no code is detected.
	require.NotContains(t, stdout.String(), "Enable code reference scanning?")
}

func TestRun_Interactive_DefaultsIndexCodeWithoutFolderLimits(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, ".git"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "main.py"), []byte("print('hi')\n"), 0o644))

	var stdout bytes.Buffer
	var stderr bytes.Buffer

	// Accept defaults for vault selection; accept default-yes for code refs; accept default-yes for python anchors.
	err := Run(RunOptions{
		Dir:         root,
		Interactive: true,
		Stdout:      &stdout,
		Stderr:      &stderr,
		Stdin:       bytes.NewBufferString("\n"),
	})
	require.NoError(t, err)

	cfgBytes, err := os.ReadFile(filepath.Join(root, ".rhizome", "config.yml"))
	require.NoError(t, err)
	// Code indexing covers the whole repository; no language folders.
	var cfg obsidian.LocalConfig
	require.NoError(t, yaml.Unmarshal(cfgBytes, &cfg))
	require.Equal(t, obsidian.LocalCodeConfig{Enabled: true}, cfg.Code)
}

func TestRun_PersistsAgentModesToConfig(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, ".git"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(root, ".obsidian"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "README.md"), []byte("# demo\n"), 0o644))

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	err := Run(RunOptions{
		Dir:    root,
		Stdout: &stdout,
		Stderr: &stderr,
		Agents: "codex",
	})
	require.NoError(t, err)

	data, err := os.ReadFile(filepath.Join(root, ".rhizome", "config.yml"))
	require.NoError(t, err)

	// New format: agents preferences are at top level in LocalConfig
	var cfg obsidian.LocalConfig
	require.NoError(t, yaml.Unmarshal(data, &cfg))
	require.NotNil(t, cfg.Agents)
	require.Equal(t, agentModeOff, cfg.Agents.Cursor)
	require.Equal(t, agentModeOn, cfg.Agents.Codex)
	require.Equal(t, agentModeOff, cfg.Agents.Claude)
	require.Equal(t, agentModeOn, cfg.Agents.AgentSkills, "shared skills follow the chosen agents")

	agents, err := os.ReadFile(filepath.Join(root, "AGENTS.md"))
	require.NoError(t, err)
	require.Contains(t, string(agents), "If an installed `rhizome` skill is available to the active harness")
	require.FileExists(t, filepath.Join(root, ".agents", "skills", "rhizome", "SKILL.md"))
}

func TestRun_RepairCandidateEnablesManagedBlockAndSharedSkill(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "README.md"), []byte("# demo\n"), 0o644))

	err := Run(RunOptions{
		Dir:    root,
		Stdout: io.Discard,
		Stderr: io.Discard,
		Agents: "none",
	})
	require.NoError(t, err)
	require.NoFileExists(t, filepath.Join(root, "AGENTS.md"))
	require.NoFileExists(t, filepath.Join(root, ".agents", "skills", "rhizome", "SKILL.md"))

	err = Run(RunOptions{
		Dir:    root,
		Stdout: io.Discard,
		Stderr: io.Discard,
		Agents: "codex",
	})
	require.NoError(t, err)
	require.FileExists(t, filepath.Join(root, "AGENTS.md"))
	require.FileExists(t, filepath.Join(root, ".agents", "skills", "rhizome", "SKILL.md"))
}

func TestRun_PersistsAgentModesWhenReconfiguring(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "README.md"), []byte("# demo\n"), 0o644))

	// Seed an existing config so init would normally ask "Reconfigure anyway?"
	require.NoError(t, writeConfigNew(root, obsidian.LocalConfig{}))

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	err := Run(RunOptions{
		Dir:         root,
		Interactive: true,
		Stdout:      &stdout,
		Stderr:      &stderr,
		Stdin:       bytes.NewBufferString("y\n\n"),
		Agents:      "codex",
	})
	require.NoError(t, err)

	data, err := os.ReadFile(filepath.Join(root, ".rhizome", "config.yml"))
	require.NoError(t, err)
	// New format: agents preferences are at top level in LocalConfig
	var cfg obsidian.LocalConfig
	require.NoError(t, yaml.Unmarshal(data, &cfg))
	require.NotNil(t, cfg.Agents)
	require.Equal(t, agentModeOff, cfg.Agents.Claude)
}

func TestRun_IdempotentAcceptDefaults(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, ".rhizome"), 0o755))

	cfgPath := filepath.Join(root, ".rhizome", "config.yml")
	beforeCfg := []byte(strings.TrimSpace(`
notes:
  includes: ["**/*.md"]
fileContext:
  includeDocsInGraph: false
noteEmbeddings:
  enabled: false
codeEmbeddings:
  enabled: true
  model: text-embedding-3-small
graph:
  ignore: ["drafts/**"]
agents:
  cursor: off
indexPath: .rhizome/custom.sqlite
rhizome:
  version: v0.38.0
`) + "\n")
	require.NoError(t, os.WriteFile(cfgPath, beforeCfg, 0o644))

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	err := Run(RunOptions{
		Dir:         root,
		Interactive: true,
		Stdout:      &stdout,
		Stderr:      &stderr,
		Stdin:       bytes.NewBufferString("\n"),
	})
	require.NoError(t, err)

	// Enabled code embeddings without a provider cannot load, so the rerun
	// records the provider the model implies and changes nothing else.
	afterCfg, err := os.ReadFile(cfgPath)
	require.NoError(t, err)
	// An older setup without a code section also gets code indexing turned on.
	require.Equal(t, strings.Replace(string(beforeCfg), "  model: text-embedding-3-small\n", "  model: text-embedding-3-small\n  provider: openai\n", 1)+"code:\n  enabled: true\n", string(afterCfg))
	require.Contains(t, stdout.String(), "Record OpenAI as the semantic search provider")
	require.Contains(t, stdout.String(), "+ Turn on code indexing")

	gitignore, err := os.ReadFile(filepath.Join(root, ".rhizome", ".gitignore"))
	require.NoError(t, err)
	require.Equal(t, rhizomeGitIgnoreBody(), string(gitignore))
}

func TestRun_PartialChangePreservesOtherSections(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, ".rhizome"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(root, "docs"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "docs", "note.md"), []byte("# Note\n"), 0o644))

	cfgPath := filepath.Join(root, ".rhizome", "config.yml")
	beforeCfg := []byte(strings.TrimSpace(`
notes:
  includes: ["notes/**/*.md"]
fileContext:
  includeDocsInGraph: false
noteEmbeddings:
  enabled: false
agents:
  cursor: off
`) + "\n")
	require.NoError(t, os.WriteFile(cfgPath, beforeCfg, 0o644))

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	err := Run(RunOptions{
		Dir:         root,
		Interactive: true,
		Stdout:      &stdout,
		Stderr:      &stderr,
		Stdin:       bytes.NewBufferString("y\n1\nn\n2\n\nn\n\n"),
	})
	require.NoError(t, err)

	afterCfg, err := os.ReadFile(cfgPath)
	require.NoError(t, err)
	body := string(afterCfg)

	require.Contains(t, body, "notes:")
	require.Contains(t, body, "'**/*.md'", "confirming the change list removes the notes folder limit")
	require.Contains(t, body, "noteEmbeddings:\n  enabled: false")
	require.Contains(t, body, "fileContext:\n  includeDocsInGraph: false")
	require.Contains(t, body, "agents:\n  cursor: off")
}

func TestRun_YesIncludeIgnoredWritesNegationBeforeDetection(t *testing.T) {
	mockNoGlobalConfig(t)
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, ".git"), 0o755))
	writeFixtureFile(t, root, ".gitignore", "app/\n")
	writeFixtureFile(t, root, "app/go.mod", "module example.com/app\n")
	writeFixtureFile(t, root, "app/main.go", "package main\n")
	writeFixtureFile(t, root, "README.md", "# demo\n")
	t.Setenv("OPENAI_API_KEY", "")
	t.Setenv("RHIZOME_OPENAI_API_KEY", "")
	t.Setenv("VOYAGE_API_KEY", "")
	t.Setenv("RHIZOME_VOYAGE_API_KEY", "")
	t.Setenv("RHIZOME_DISABLE_OLLAMA", "1")

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	err := Run(RunOptions{
		Dir:            root,
		Workflow:       "none",
		IncludeIgnored: []string{"app"},
		Stdout:         &stdout,
		Stderr:         &stderr,
		Stdin:          bytes.NewBuffer(nil),
	})
	require.NoError(t, err)

	require.Contains(t, readIgnoreFile(t, root), "!/app/\n")
	cfg, err := obsidian.LoadLocalConfig(root)
	require.NoError(t, err)
	require.Equal(t, obsidian.LocalCodeConfig{Enabled: true}, cfg.Code, "the included folder is inside the whole-repository code scope")
	require.NotContains(t, stdout.String(), "Ignored subtrees detected")
}

func TestRun_YesSurfacesUnincludedIgnoredCandidates(t *testing.T) {
	mockNoGlobalConfig(t)
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, ".git"), 0o755))
	writeFixtureFile(t, root, ".gitignore", "app/\n")
	writeFixtureFile(t, root, "app/go.mod", "module example.com/app\n")
	writeFixtureFile(t, root, "app/main.go", "package main\n")
	writeFixtureFile(t, root, "README.md", "# demo\n")
	t.Setenv("OPENAI_API_KEY", "")
	t.Setenv("RHIZOME_OPENAI_API_KEY", "")
	t.Setenv("VOYAGE_API_KEY", "")
	t.Setenv("RHIZOME_VOYAGE_API_KEY", "")
	t.Setenv("RHIZOME_DISABLE_OLLAMA", "1")

	var stdout bytes.Buffer
	err := Run(RunOptions{
		Dir:      root,
		Workflow: "none",
		Stdout:   &stdout,
		Stderr:   io.Discard,
		Stdin:    bytes.NewBuffer(nil),
	})
	require.NoError(t, err)

	out := stdout.String()
	require.Contains(t, out, "Ignored subtrees detected")
	require.Contains(t, out, "app: ")
	require.Contains(t, out, "--include-ignored app")
	// Notice only; nothing is written without the flag.
	require.NotContains(t, readIgnoreFile(t, root), "!/app/")
}

func TestRun_FirstRunCanAcceptIgnoredSubtreeCandidate(t *testing.T) {
	mockNoGlobalConfig(t)
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, ".git"), 0o755))
	writeFixtureFile(t, root, ".gitignore", "app/\n")
	writeFixtureFile(t, root, "app/package.json", "{}\n")
	writeFixtureFile(t, root, "app/src/index.ts", "export const answer = 42\n")
	writeFixtureFile(t, root, "README.md", "# demo\n")
	t.Setenv("OPENAI_API_KEY", "")
	t.Setenv("RHIZOME_OPENAI_API_KEY", "")
	t.Setenv("VOYAGE_API_KEY", "")
	t.Setenv("RHIZOME_VOYAGE_API_KEY", "")
	t.Setenv("RHIZOME_DISABLE_OLLAMA", "1")

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	err := Run(RunOptions{
		Dir:         root,
		Interactive: true,
		Stdout:      &stdout,
		Stderr:      &stderr,
		// include the candidate (default yes), workflow none, set up search
		// later, confirm.
		Stdin: bytes.NewBufferString("\n3\n\n\n"),
	})
	require.NoError(t, err)

	require.Contains(t, stdout.String(), "Ignored subtrees")
	require.Contains(t, readIgnoreFile(t, root), "!/app/\n")
	require.Contains(t, stdout.String(), "TypeScript", "the included folder's code shows in the findings")
	cfg, err := obsidian.LoadLocalConfig(root)
	require.NoError(t, err)
	require.True(t, cfg.Code.Enabled)
}

func TestRun_NewInitPrunesDefaults(t *testing.T) {
	mockNoGlobalConfig(t)
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, "docs"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "docs", "note.md"), []byte("# Note\n"), 0o644))
	t.Setenv("OPENAI_API_KEY", "")
	t.Setenv("RHIZOME_OPENAI_API_KEY", "")
	t.Setenv("VOYAGE_API_KEY", "")
	t.Setenv("RHIZOME_VOYAGE_API_KEY", "")
	t.Setenv("ATOMIC_RHIZOME_KEY", "") // Disable team keys
	t.Setenv("RHIZOME_DISABLE_OLLAMA", "1")

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	err := Run(RunOptions{
		Dir:    root,
		Stdout: &stdout,
		Stderr: &stderr,
	})
	require.NoError(t, err)

	cfgBytes, err := os.ReadFile(filepath.Join(root, ".rhizome", "config.yml"))
	require.NoError(t, err)
	body := string(cfgBytes)
	require.NotContains(t, body, "fileContext:")
	require.NotContains(t, body, "noteEmbeddings:")
	require.NotContains(t, body, "codeEmbeddings:")
	require.NotContains(t, body, "graph:")
	require.Contains(t, body, "rhizome:")
	require.Contains(t, body, "version: "+version.Version)
}

func TestRun_FastPath_EmbeddingsProviderUpgrade(t *testing.T) {
	mockNoGlobalConfig(t)
	// Test that when the fast path triggers embeddings provider upgrade,
	// the config is saved correctly (regression test for SaveLocalConfig
	// being called with file path instead of directory).
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, ".rhizome"), 0o755))

	// Create config with embeddings enabled but no provider.
	cfgPath := filepath.Join(root, ".rhizome", "config.yml")
	oldCfg := []byte(strings.TrimSpace(`
notes:
  includes: ["**/*.md"]
noteEmbeddings:
  enabled: true
  model: voyage-3-large
`) + "\n")
	require.NoError(t, os.WriteFile(cfgPath, oldCfg, 0o644))

	// Set up environment: Voyage key available for provider selection
	t.Setenv("VOYAGE_API_KEY", "test-key")
	t.Setenv("RHIZOME_DISABLE_OLLAMA", "1")

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	err := Run(RunOptions{
		Dir:         root,
		Interactive: true,
		Stdout:      &stdout,
		Stderr:      &stderr,
		Stdin:       bytes.NewBufferString("\n"),
	})
	require.NoError(t, err)

	// Verify config was updated with provider
	cfgBytes, err := os.ReadFile(cfgPath)
	require.NoError(t, err)
	require.Contains(t, string(cfgBytes), "provider: voyage")

	// Verify the config file is valid YAML
	var cfg obsidian.LocalConfig
	require.NoError(t, yaml.Unmarshal(cfgBytes, &cfg))
	require.NotNil(t, cfg.NoteEmbeddings)
	require.Equal(t, "voyage", cfg.NoteEmbeddings.Provider)
	require.Contains(t, stdout.String(), "Record Voyage AI as the semantic search provider")
}

func TestRun_InteractiveFirstRun_EnablesSharedAndDetectedAgents(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, ".claude"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "README.md"), []byte("# demo\n"), 0o644))

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	err := Run(RunOptions{
		Dir:         root,
		Interactive: true,
		Stdout:      &stdout,
		Stderr:      &stderr,
		Stdin:       bytes.NewBufferString("y\ny\n"),
	})
	require.NoError(t, err)

	agents, err := os.ReadFile(filepath.Join(root, "AGENTS.md"))
	require.NoError(t, err)
	require.Contains(t, string(agents), managedRhizomeBlockStart)
	require.FileExists(t, filepath.Join(root, "CLAUDE.md"))
	require.DirExists(t, filepath.Join(root, ".agents", "skills"))
}

func TestRun_TemplateAgenticEngineeringRefreshesManagedAgentBlockOnRerun(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "README.md"), []byte("# demo\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, "AGENTS.md"), []byte(strings.TrimSpace(`
# Repository Guidelines

Custom intro.

<!-- BEGIN RZM INIT MANAGED AGENT BLOCK -->
stale block
<!-- END RZM INIT MANAGED AGENT BLOCK -->
`)+"\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, "CLAUDE.md"), []byte(strings.TrimSpace(`
User note.

<!-- BEGIN RZM INIT MANAGED AGENT BLOCK -->
stale block
<!-- END RZM INIT MANAGED AGENT BLOCK -->
`)+"\n"), 0o644))

	err := Run(RunOptions{
		Dir:      root,
		Workflow: templateAgenticEngineering,
		Stdout:   io.Discard,
		Stderr:   io.Discard,
	})
	require.NoError(t, err)

	agents, err := os.ReadFile(filepath.Join(root, "AGENTS.md"))
	require.NoError(t, err)
	agentsText := string(agents)
	require.Contains(t, agentsText, "Custom intro.")
	require.NotContains(t, agentsText, "stale block")
	require.Equal(t, 1, strings.Count(agentsText, managedRhizomeBlockStart))
	require.Equal(t, 1, strings.Count(agentsText, managedTemplateBlockStartPrefix+templateAgenticEngineering+managedTemplateBlockSuffix))
	require.Contains(t, agentsText, "## Agentic Engineering")
	require.Contains(t, agentsText, "## Installed Skills")
	require.Contains(t, agentsText, "`agentic-engineering`")
	require.NotContains(t, agentsText, "quality-gates-check")
	require.NotContains(t, agentsText, "compound")

	claude, err := os.ReadFile(filepath.Join(root, "CLAUDE.md"))
	require.NoError(t, err)
	claudeText := string(claude)
	require.Contains(t, claudeText, "User note.")
	require.NotContains(t, claudeText, "stale block")
	require.Equal(t, 1, strings.Count(claudeText, managedRhizomeBlockStart))
	require.Equal(t, 1, strings.Count(claudeText, managedTemplateBlockStartPrefix+templateAgenticEngineering+managedTemplateBlockSuffix))
	require.Contains(t, claudeText, "## Agentic Engineering")
}

func TestRun_ClaudeMdIncludingAgentsMdGetsPointerNotFullBlock(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "README.md"), []byte("# demo\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, "AGENTS.md"), []byte("# Repository Guidelines\n\nCustom intro.\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, "CLAUDE.md"), []byte(strings.TrimSpace(`
@AGENTS.md

User note.

<!-- BEGIN RZM INIT MANAGED AGENT BLOCK -->
stale block
<!-- END RZM INIT MANAGED AGENT BLOCK -->
`)+"\n"), 0o644))

	err := Run(RunOptions{
		Dir:      root,
		Workflow: templateAgenticEngineering,
		Stdout:   io.Discard,
		Stderr:   io.Discard,
	})
	require.NoError(t, err)

	agents, err := os.ReadFile(filepath.Join(root, "AGENTS.md"))
	require.NoError(t, err)
	require.Contains(t, string(agents), "## Route through the Rhizome skill")
	require.Contains(t, string(agents), "## Agentic Engineering")

	claude, err := os.ReadFile(filepath.Join(root, "CLAUDE.md"))
	require.NoError(t, err)
	claudeText := string(claude)
	require.Contains(t, claudeText, "@AGENTS.md")
	require.Contains(t, claudeText, "User note.")
	require.NotContains(t, claudeText, "stale block")
	require.Equal(t, 1, strings.Count(claudeText, managedRhizomeBlockStart))
	require.Contains(t, claudeText, claudeAgentsMdPointer)
	require.NotContains(t, claudeText, "## Route through the Rhizome skill")
	require.NotContains(t, claudeText, "## Agentic Engineering")
	require.NotContains(t, claudeText, managedTemplateBlockStartPrefix+templateAgenticEngineering+managedTemplateBlockSuffix)
}

func TestRun_ClaudeMdInertAgentsMdMentionKeepsFullBlock(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "README.md"), []byte("# demo\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, "AGENTS.md"), []byte("# Repository Guidelines\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, "CLAUDE.md"), []byte(strings.TrimSpace(`
User note.

<!-- not active:
@AGENTS.md
-->

`+"```"+`
@AGENTS.md
`+"```"+`

`+"````markdown"+`
~~~
@AGENTS.md
`+"````"+`
`)+"\n"), 0o644))

	err := Run(RunOptions{
		Dir:      root,
		Workflow: templateAgenticEngineering,
		Stdout:   io.Discard,
		Stderr:   io.Discard,
	})
	require.NoError(t, err)

	claude, err := os.ReadFile(filepath.Join(root, "CLAUDE.md"))
	require.NoError(t, err)
	claudeText := string(claude)
	require.NotContains(t, claudeText, claudeAgentsMdPointer)
	require.Contains(t, claudeText, "## Route through the Rhizome skill")
	require.Contains(t, claudeText, "## Agentic Engineering")
}

func TestRun_ClaudeMdPointerModeRemovesDuplicateStarterBlock(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "README.md"), []byte("# demo\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, "CLAUDE.md"), []byte("@AGENTS.md\n\nUser note.\n"), 0o644))
	require.NoError(t, Run(RunOptions{Dir: root, Workflow: templateAgenticEngineering, Stdout: io.Discard, Stderr: io.Discard}))

	claude, err := os.ReadFile(filepath.Join(root, "CLAUDE.md"))
	require.NoError(t, err)
	starterBlock := managedTemplateBlockStartPrefix + templateAgenticEngineering + managedTemplateBlockSuffix + "\nstale copy\n" + managedTemplateBlockEndPrefix + templateAgenticEngineering + managedTemplateBlockSuffix + "\n"
	require.NoError(t, os.WriteFile(filepath.Join(root, "CLAUDE.md"), append(claude, []byte("\n"+starterBlock)...), 0o644))
	require.NoError(t, Run(RunOptions{Dir: root, Stdout: io.Discard, Stderr: io.Discard}))

	claude, err = os.ReadFile(filepath.Join(root, "CLAUDE.md"))
	require.NoError(t, err)
	require.NotContains(t, string(claude), "stale copy", "AGENTS.md carries the starter block, so CLAUDE.md drops its copy")
	require.NotContains(t, string(claude), managedTemplateBlockStartPrefix+templateAgenticEngineering+managedTemplateBlockSuffix)
	require.Contains(t, string(claude), claudeAgentsMdPointer)
}

func TestRun_PrunesOnlyRecordedUneditedReferences(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "README.md"), []byte("# demo\n"), 0o644))
	require.NoError(t, Run(RunOptions{Dir: root, Workflow: templateAgenticEngineering, Stdout: io.Discard, Stderr: io.Discard}))

	coreRefs := filepath.Join(root, ".agents", "skills", coreRhizomeSkillName, "references")
	// A file rzm init wrote earlier and the template no longer ships: recorded, unchanged.
	writeTestFile(t, root, ".agents/skills/rhizome/references/retired-managed.md", "# retired managed reference\n")
	setWritten(t, root, ".agents/skills/rhizome/references/retired-managed.md", "# retired managed reference\n")
	// The same situation, but the user edited the file after install.
	writeTestFile(t, root, ".agents/skills/rhizome/references/retired-edited.md", "# team notes added\n")
	setWritten(t, root, ".agents/skills/rhizome/references/retired-edited.md", "# original\n")
	// A file the user created; never recorded.
	writeTestFile(t, root, ".agents/skills/rhizome/references/team-owned.md", "# ours\n")
	// An unmanaged skill directory is never touched.
	writeTestFile(t, root, ".agents/skills/team-owned/references/mine.md", "# mine\n")

	require.NoError(t, Run(RunOptions{Dir: root, Stdout: io.Discard, Stderr: io.Discard}))

	require.NoFileExists(t, filepath.Join(coreRefs, "retired-managed.md"), "recorded and unchanged managed file is pruned")
	require.FileExists(t, filepath.Join(coreRefs, "retired-edited.md"), "a recorded file the user edited is kept")
	require.FileExists(t, filepath.Join(coreRefs, "team-owned.md"), "a user-created file is kept")
	require.FileExists(t, filepath.Join(coreRefs, "sessions.md"))
	require.FileExists(t, filepath.Join(root, ".agents", "skills", "team-owned", "references", "mine.md"))
	record, _ := loadGeneratedFiles(root)
	require.NotContains(t, record.Written, ".agents/skills/rhizome/references/retired-managed.md")
}

func TestDroppedReferenceStaysWhileItsSkillKeepsAnEditedRouter(t *testing.T) {
	root := t.TempDir()
	syncForTest(t, root, nil, nil)
	ref := ".agents/skills/rhizome/references/retired.md"
	writeTestFile(t, root, ref, "# retired\n")
	setWritten(t, root, ref, "# retired\n")
	router := ".agents/skills/rhizome/SKILL.md"
	writeTestFile(t, root, router, "edited router that still links references/retired.md\n")
	setWritten(t, root, router, "an older Rhizome router\n")

	report := syncForTest(t, root, nil, nil)

	require.Contains(t, report.Kept, router)
	require.FileExists(t, filepath.Join(root, filepath.FromSlash(ref)), "the kept router still links it")
}

func TestRun_TemplateAgenticEngineeringRerunAddsMissingDocsWithoutRefreshingExistingOnes(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "README.md"), []byte("# demo\n"), 0o644))

	err := Run(RunOptions{
		Dir:      root,
		Workflow: templateAgenticEngineering,
		Stdout:   io.Discard,
		Stderr:   io.Discard,
	})
	require.NoError(t, err)

	keptPath := filepath.Join(root, "docs", "engineering", "testing-policy.md")
	customDoc := strings.TrimSpace(`
# Custom workflow

Local edits should survive init reruns.
`) + "\n"
	require.NoError(t, os.WriteFile(keptPath, []byte(customDoc), 0o644))

	missingPath := filepath.Join(root, "docs", "engineering", "quality-gates.md")
	require.NoError(t, os.Remove(missingPath))

	err = Run(RunOptions{
		Dir:    root,
		Stdout: io.Discard,
		Stderr: io.Discard,
	})
	require.NoError(t, err)

	keptBody, err := os.ReadFile(keptPath)
	require.NoError(t, err)
	require.Equal(t, customDoc, string(keptBody))

	restoredBody, err := os.ReadFile(missingPath)
	require.NoError(t, err)
	require.Contains(t, string(restoredBody), "# Quality gates")
}

func TestRun_RefreshTemplateDocsPreservesLocallyEditedDocsInBatch(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "README.md"), []byte("# demo\n"), 0o644))

	err := Run(RunOptions{
		Dir:      root,
		Workflow: templateAgenticEngineering,
		Stdout:   io.Discard,
		Stderr:   io.Discard,
	})
	require.NoError(t, err)

	docPath := filepath.Join(root, "docs", "engineering", "testing-policy.md")
	require.NoError(t, os.WriteFile(docPath, []byte("# overwritten\n"), 0o644))

	var stdout bytes.Buffer
	err = Run(RunOptions{
		Dir:         root,
		RefreshDocs: true,
		Stdout:      &stdout,
		Stderr:      io.Discard,
	})
	require.NoError(t, err)

	after, err := os.ReadFile(docPath)
	require.NoError(t, err)
	require.Equal(t, "# overwritten\n", string(after), "batch refresh must not clobber a team-edited doc")
}

func TestRun_RefreshTemplateDocsUpdatesUneditedDocsFromRecordedBaseline(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "README.md"), []byte("# demo\n"), 0o644))

	err := Run(RunOptions{
		Dir:      root,
		Workflow: templateAgenticEngineering,
		Stdout:   io.Discard,
		Stderr:   io.Discard,
	})
	require.NoError(t, err)

	// Simulate an older shipped version: the on-disk doc matches the recorded
	// baseline fingerprint but not the current template.
	docPath := filepath.Join(root, "docs", "engineering", "testing-policy.md")
	older := []byte("# Testing policy\n\nolder shipped text\n")
	require.NoError(t, os.WriteFile(docPath, older, 0o644))
	setWritten(t, root, "docs/engineering/testing-policy.md", string(older))

	var stdout bytes.Buffer
	err = Run(RunOptions{
		Dir:         root,
		RefreshDocs: true,
		Stdout:      &stdout,
		Stderr:      io.Discard,
	})
	require.NoError(t, err)

	after, err := os.ReadFile(docPath)
	require.NoError(t, err)
	require.Contains(t, string(after), "## Layers")
	require.NotContains(t, string(after), "older shipped text")
}

func TestRun_TemplateSkillsDoNotRefreshWithoutAuthority(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "README.md"), []byte("# demo\n"), 0o644))

	err := Run(RunOptions{
		Dir:      root,
		Workflow: templateAgenticEngineering,
		Stdout:   io.Discard,
		Stderr:   io.Discard,
	})
	require.NoError(t, err)

	skillPath := filepath.Join(root, ".agents", "skills", "agentic-engineering", "SKILL.md")
	require.NoError(t, os.WriteFile(skillPath, []byte("# stale skill\n"), 0o644))

	err = Run(RunOptions{
		Dir:    root,
		Stdout: io.Discard,
		Stderr: io.Discard,
	})
	require.NoError(t, err)

	after, err := os.ReadFile(skillPath)
	require.NoError(t, err)
	require.Equal(t, "# stale skill\n", string(after))
}

func TestWriteConfigNewUsesTwoSpaceIndent(t *testing.T) {
	root := t.TempDir()
	err := writeConfigNew(root, obsidian.LocalConfig{
		Rhizome:           obsidian.LocalRhizomeConfig{Version: "v1.2.3"},
		WorkflowTemplates: []string{templateAgenticEngineering},
		WorkflowTemplateManagement: obsidian.WorkflowTemplateManagement{
			SourceFingerprints: map[string]string{"spec-driven:docs:README.md": "abc123"},
		},
		Code: obsidian.LocalCodeConfig{
			Enabled: true,
			Python:  &obsidian.LocalCodeLangConfig{Roots: []string{"src"}},
		},
	})
	require.NoError(t, err)

	data, err := os.ReadFile(filepath.Join(root, ".rhizome", "config.yml"))
	require.NoError(t, err)
	body := string(data)
	require.Contains(t, body, "rhizome:\n  version: v1.2.3\n")
	require.Contains(t, body, "code:\n  enabled: true\n  python:\n    roots:\n      - src\n")
	require.NotContains(t, body, "rhizome:\n    version:")
	require.NotContains(t, body, "code:\n    enabled:")
	require.NotContains(t, body, "workflowTemplates:")
	require.NotContains(t, body, "workflowTemplateManagement:")
	workflow, exists, err := obsidian.LoadLocalWorkflowConfig(root)
	require.NoError(t, err)
	require.True(t, exists)
	require.Equal(t, []string{templateAgenticEngineering}, workflow.Templates)
	require.Equal(t, "abc123", workflow.Management.SourceFingerprints["spec-driven:docs:README.md"])
}

func TestWriteConfigPatchedCanonicalizesToTwoSpaceIndent(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, ".rhizome"), 0o755))
	path := filepath.Join(root, ".rhizome", "config.yml")
	require.NoError(t, os.WriteFile(path, []byte("rhizome:\n    devBinaryDir: bin\ncode:\n    enabled: true\n"), 0o644))

	err := writeConfigPatched(root, obsidian.LocalConfig{
		Rhizome: obsidian.LocalRhizomeConfig{Version: "v1.2.3"},
	}, changeSet{sectionRhizome: true})
	require.NoError(t, err)

	data, err := os.ReadFile(path)
	require.NoError(t, err)
	body := string(data)
	require.Contains(t, body, "rhizome:\n  version: v1.2.3\n")
	require.Contains(t, body, "code:\n  enabled: true\n")
	require.NotContains(t, body, "rhizome:\n    version:")
	require.NotContains(t, body, "code:\n    enabled:")
}
