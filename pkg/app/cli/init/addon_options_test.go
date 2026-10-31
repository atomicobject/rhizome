package init

import (
	"bytes"
	"fmt"
	"path/filepath"
	"slices"
	"testing"

	"github.com/atomicobject/rhizome/pkg/vault/repoconfig"
	"github.com/stretchr/testify/require"
)

const templateCompounding = "compounding"

func compoundingSkill(root string) string {
	return filepath.Join(root, ".agents", "skills", templateCompounding, "SKILL.md")
}

func actionItemsOntology(root string) string {
	return filepath.Join(root, ".rhizome", "ontology", "action-items.graphql")
}

func requireUpToDate(t *testing.T, root string) {
	t.Helper()
	out, err := runInit(t, RunOptions{Dir: root})
	require.NoError(t, err)
	require.Contains(t, out, "Everything is up to date.")
	_, err = runInit(t, RunOptions{Dir: root, Check: true})
	require.NoError(t, err)
}

func TestFirstRunInstallsAnOptionalAddonListedInAddons(t *testing.T) {
	root := machineRunRepo(t)

	_, err := Apply(RunOptions{Dir: root, Workflow: "agentic-engineering", Addons: "action-items,compounding", Search: "off"})
	require.NoError(t, err)

	cfg := loadConfigForTest(t, root)
	require.Equal(t, []string{templateActionItems, templateCompounding}, cfg.WorkflowTemplateAddons.Enabled)
	require.Empty(t, cfg.WorkflowTemplateAddons.Disabled)
	require.FileExists(t, compoundingSkill(root))
	require.FileExists(t, actionItemsOntology(root))

	requireUpToDate(t, root)
	out, err := runInit(t, RunOptions{Dir: root})
	require.NoError(t, err)
	require.Contains(t, out, "Add-ons   Action items and Compounding")
}

func TestOptionalAddonIsNotADefaultAndTheSetupSheetDoesNotListIt(t *testing.T) {
	registry, err := loadStarterTemplateMetadataRegistry()
	require.NoError(t, err)
	require.True(t, registry[templateCompounding].OptionalAddon)
	require.Equal(t, []string{templateCore}, registry[templateCompounding].Requires)
	require.Contains(t, addonIDs(registry), templateCompounding)
	for _, option := range addonOptions(registry, nil) {
		require.NotEqual(t, templateCompounding, option.ID)
	}

	root := machineRunRepo(t)
	_, err = Apply(RunOptions{Dir: root, Workflow: "agentic-engineering", Search: "off"})
	require.NoError(t, err)
	require.NoFileExists(t, compoundingSkill(root))
}

// addonMenuNumber is an add-on's number in the settings Add-ons section.
func addonMenuNumber(t *testing.T, id string) string {
	t.Helper()
	registry, err := loadStarterTemplateMetadataRegistry()
	require.NoError(t, err)
	return fmt.Sprint(slices.Index(addonIDs(registry), id) + 1)
}

func TestFirstRunSettingsTurnOnAnOptionalAddonAndItsHooks(t *testing.T) {
	root := firstRunRepo(t)
	n := addonMenuNumber(t, templateCompounding)

	// Default workflow, no key, e for settings, 5 for add-ons, n picks
	// Compounding, Enter turns hooks on, Enter twice leaves, Enter sets up.
	out := runFirstRunForTest(t, initTestEnv(t), root, "\n\ne\n5\n"+n+"\n\n\n\n\n")

	require.Contains(t, out, "Optional add-ons: Compounding. Press e, then Add-ons, to turn one on.")
	require.Contains(t, out, "[ ] Compounding")
	require.Contains(t, out, "Compounding uses agent hooks, which are off.")
	require.Contains(t, out, "Add-ons   Action items and Compounding")
	cfg := loadConfigForTest(t, root)
	require.Equal(t, []string{templateActionItems, templateCompounding}, cfg.WorkflowTemplateAddons.Enabled)
	require.True(t, hooksOn(*cfg))
	require.FileExists(t, compoundingSkill(root))
}

func TestFirstRunSettingsCanTurnOffAnAddonListedInAddons(t *testing.T) {
	root := firstRunRepo(t)
	n := addonMenuNumber(t, templateCompounding)

	// Default workflow, no key, e, 5, n turns Compounding off, Enter twice
	// leaves, Enter sets up.
	var out bytes.Buffer
	require.NoError(t, Run(RunOptions{Env: initTestEnv(t), Dir: root, Interactive: true, Addons: "action-items,compounding",
		Stdin: bytes.NewBufferString("\n\ne\n5\n" + n + "\n\n\n\n"), Stdout: &out, Stderr: &out}))

	require.Equal(t, []string{templateActionItems}, loadConfigForTest(t, root).WorkflowTemplateAddons.Enabled)
	require.NoFileExists(t, compoundingSkill(root))
}

func TestRerunSettingsAddAnAddonAndKeepHooksOff(t *testing.T) {
	root := rerunRepo(t)
	n := addonMenuNumber(t, templateCompounding)

	// s for settings, 5 for add-ons, n picks Compounding, n keeps hooks off,
	// Enter twice leaves, y applies.
	out, err := runInit(t, RunOptions{Dir: root, Interactive: true, Stdin: bytes.NewBufferString("s\n5\n" + n + "\nn\n\n\ny\n")})
	require.NoError(t, err)

	require.Contains(t, out, "[x] Compounding")
	require.Contains(t, out, "Hooks stay off")
	cfg := loadConfigForTest(t, root)
	require.Equal(t, []string{templateActionItems, templateCompounding}, cfg.WorkflowTemplateAddons.Enabled)
	require.False(t, hooksOn(*cfg))
	require.FileExists(t, compoundingSkill(root))
	requireUpToDate(t, root)
}

func TestAddAndRemoveAddonsChangeOnlyTheNamedAddon(t *testing.T) {
	root := rerunRepo(t)
	require.FileExists(t, actionItemsOntology(root))

	out, err := runInit(t, RunOptions{Dir: root, AddAddon: templateCompounding})
	require.NoError(t, err)
	require.Contains(t, out, "Add-ons: Action items → Action items and Compounding")
	cfg := loadConfigForTest(t, root)
	require.Equal(t, []string{templateActionItems, templateCompounding}, cfg.WorkflowTemplateAddons.Enabled)
	require.Empty(t, cfg.WorkflowTemplateAddons.Disabled)
	require.FileExists(t, compoundingSkill(root))
	requireUpToDate(t, root)

	// Removing a default add-on disables it; the optional one stays.
	_, err = runInit(t, RunOptions{Dir: root, RemoveAddon: templateActionItems})
	require.NoError(t, err)
	cfg = loadConfigForTest(t, root)
	require.Equal(t, []string{templateCompounding}, cfg.WorkflowTemplateAddons.Enabled)
	require.Equal(t, []string{templateActionItems}, cfg.WorkflowTemplateAddons.Disabled)
	require.FileExists(t, compoundingSkill(root))
	requireUpToDate(t, root)

	// Adding it back clears the disabled mark.
	_, err = runInit(t, RunOptions{Dir: root, AddAddon: templateActionItems})
	require.NoError(t, err)
	cfg = loadConfigForTest(t, root)
	require.Equal(t, []string{templateActionItems, templateCompounding}, cfg.WorkflowTemplateAddons.Enabled)
	require.Empty(t, cfg.WorkflowTemplateAddons.Disabled)

	// Removing the optional add-on drops it without disabling it.
	_, err = runInit(t, RunOptions{Dir: root, RemoveAddon: templateCompounding})
	require.NoError(t, err)
	cfg = loadConfigForTest(t, root)
	require.Equal(t, []string{templateActionItems}, cfg.WorkflowTemplateAddons.Enabled)
	require.Empty(t, cfg.WorkflowTemplateAddons.Disabled)
	require.NoFileExists(t, compoundingSkill(root))
	require.FileExists(t, actionItemsOntology(root))
	requireUpToDate(t, root)
}

func TestRemoveAddonDropsAnAddonListedAmongTheWorkflows(t *testing.T) {
	root := t.TempDir()
	writeProjectConfig(t, root, "notes:\n  includes: [\"**/*.md\"]\n", repoconfig.LocalWorkflowConfig{Templates: []string{templateAgenticEngineering, templateActionItems}})
	_, err := runInit(t, RunOptions{Dir: root})
	require.NoError(t, err)
	require.FileExists(t, actionItemsOntology(root))

	_, err = runInit(t, RunOptions{Dir: root, RemoveAddon: templateActionItems})
	require.NoError(t, err)
	cfg := loadConfigForTest(t, root)
	require.Equal(t, []string{templateAgenticEngineering}, cfg.WorkflowTemplates)
	require.Equal(t, []string{templateActionItems}, cfg.WorkflowTemplateAddons.Disabled)
	require.NoFileExists(t, filepath.Join(root, ".agents", "skills", "action-items", "SKILL.md"))
	requireUpToDate(t, root)
}

func TestAddingAnAddonDoesNotTurnOnUnrecordedDefaults(t *testing.T) {
	root := t.TempDir()
	writeProjectConfig(t, root, "notes:\n  includes: [\"**/*.md\"]\n", repoconfig.LocalWorkflowConfig{Templates: []string{templateAgenticEngineering}})

	_, err := runInit(t, RunOptions{Dir: root, AddAddon: templateCompounding})
	require.NoError(t, err)

	cfg := loadConfigForTest(t, root)
	require.Equal(t, []string{templateCompounding}, cfg.WorkflowTemplateAddons.Enabled)
	require.Equal(t, []string{templateActionItems}, cfg.WorkflowTemplateAddons.Disabled, "action-items was never installed here")
	require.FileExists(t, filepath.Join(root, ".rhizome", "ontology", "compounding.graphql"))
	require.NoFileExists(t, actionItemsOntology(root))
	requireUpToDate(t, root)
}

func TestSettingsWorkflowChangeKeepsAnAddedAddon(t *testing.T) {
	root := rerunRepo(t)
	_, err := runInit(t, RunOptions{Dir: root, AddAddon: templateCompounding})
	require.NoError(t, err)

	// s opens settings, 4 the workflow section, 3 picks guidance only, 2
	// removes the old workflow's files; Enter leaves settings and applies.
	_, err = runInit(t, RunOptions{Dir: root, Interactive: true, Stdin: bytes.NewBufferString("s\n4\n3\n2\n\n\n")})
	require.NoError(t, err)

	cfg := loadConfigForTest(t, root)
	require.Empty(t, cfg.WorkflowTemplates)
	require.Contains(t, cfg.WorkflowTemplateAddons.Enabled, templateCompounding)
	require.NoFileExists(t, filepath.Join(root, ".agents", "skills", templateAgenticEngineering, "SKILL.md"))
	require.FileExists(t, compoundingSkill(root))
	requireUpToDate(t, root)
}

func TestAddAddonRefusesAnEjectedAddonAndUnknownIDs(t *testing.T) {
	root := rerunRepo(t)
	_, err := runInit(t, RunOptions{Dir: root, AddAddon: templateCompounding})
	require.NoError(t, err)
	_, err = runInit(t, RunOptions{Dir: root, Eject: templateCompounding})
	require.NoError(t, err)

	_, err = runInit(t, RunOptions{Dir: root, AddAddon: templateCompounding})
	require.ErrorContains(t, err, "use --restore compounding")
	require.Contains(t, loadConfigForTest(t, root).WorkflowTemplateManagement.Ejected, templateCompounding)

	_, err = runInit(t, RunOptions{Dir: root, AddAddon: templateCore})
	require.ErrorContains(t, err, `unknown add-on "core" for --add-addon`)

	_, err = runInit(t, RunOptions{Dir: root, Addons: templateCompounding})
	require.ErrorContains(t, err, "use --add-addon or --remove-addon")

	_, err = runInit(t, RunOptions{Dir: firstRunRepo(t), AddAddon: templateCompounding})
	require.ErrorContains(t, err, "need an existing Rhizome setup")
}
