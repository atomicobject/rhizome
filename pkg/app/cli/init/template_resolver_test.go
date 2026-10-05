package init

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"
)

func TestResolveTemplateSetExpandsRequiresAndDefaultAddons(t *testing.T) {
	registry := map[string]starterTemplateMetadata{
		templateCore:               {ID: templateCore},
		templateActionItems:        {ID: templateActionItems, Requires: []string{templateCore}},
		templateAgenticEngineering: {ID: templateAgenticEngineering, Requires: []string{templateCore}, ActivatesByDefault: []string{templateActionItems}},
	}

	resolved, err := resolveTemplateSet([]string{templateAgenticEngineering}, obsidian.WorkflowTemplateAddons{}, registry)
	require.NoError(t, err)
	require.Equal(t, []string{templateCore, templateAgenticEngineering, templateActionItems}, resolved.Effective)
	require.Equal(t, []string{templateAgenticEngineering}, resolved.Explicit)
	require.Equal(t, templateCauseExplicit, resolved.Causes[templateAgenticEngineering])
	require.Equal(t, templateCauseRequired, resolved.Causes[templateCore])
	require.Equal(t, templateCauseDefaultAddon, resolved.Causes[templateActionItems])
}

func TestResolveTemplateSetComplexDomainImpliesSpecDriven(t *testing.T) {
	registry := map[string]starterTemplateMetadata{
		templateCore:               {ID: templateCore},
		templateActionItems:        {ID: templateActionItems, Requires: []string{templateCore}},
		templateAgenticEngineering: {ID: templateAgenticEngineering, Requires: []string{templateCore}, ActivatesByDefault: []string{templateActionItems}},
		templateComplexDomain:      {ID: templateComplexDomain, Requires: []string{templateAgenticEngineering}},
	}

	resolved, err := resolveTemplateSet([]string{templateComplexDomain}, obsidian.WorkflowTemplateAddons{}, registry)
	require.NoError(t, err)
	require.Equal(t, []string{templateCore, templateAgenticEngineering, templateComplexDomain, templateActionItems}, resolved.Effective)
	require.Equal(t, templateCauseExplicit, resolved.Causes[templateComplexDomain])
	require.Equal(t, templateCauseRequired, resolved.Causes[templateAgenticEngineering])
}

func TestResolveTemplateSetExplicitSpecDrivenAndComplexDomainDedupes(t *testing.T) {
	registry := map[string]starterTemplateMetadata{
		templateCore:               {ID: templateCore},
		templateAgenticEngineering: {ID: templateAgenticEngineering, Requires: []string{templateCore}},
		templateComplexDomain:      {ID: templateComplexDomain, Requires: []string{templateAgenticEngineering}},
	}

	resolved, err := resolveTemplateSet([]string{templateAgenticEngineering, templateComplexDomain}, obsidian.WorkflowTemplateAddons{}, registry)
	require.NoError(t, err)
	require.Equal(t, []string{templateAgenticEngineering, templateComplexDomain}, resolved.Explicit)
	require.Equal(t, []string{templateCore, templateAgenticEngineering, templateComplexDomain}, resolved.Effective)
	require.Equal(t, templateCauseExplicit, resolved.Causes[templateAgenticEngineering])
	require.Equal(t, templateCauseExplicit, resolved.Causes[templateComplexDomain])
}

func TestResolveTemplateSetAllowsDefaultAddonOptOut(t *testing.T) {
	registry := map[string]starterTemplateMetadata{
		templateCore:        {ID: templateCore},
		templateActionItems: {ID: templateActionItems, Requires: []string{templateCore}},
		templateProjectKB:   {ID: templateProjectKB, Requires: []string{templateCore}, ActivatesByDefault: []string{templateActionItems}},
	}

	resolved, err := resolveTemplateSet([]string{templateProjectKB}, obsidian.WorkflowTemplateAddons{Disabled: []string{templateActionItems}}, registry)
	require.NoError(t, err)
	require.Equal(t, []string{templateCore, templateProjectKB}, resolved.Effective)
	require.NotContains(t, resolved.Causes, templateActionItems)
}

func TestResolveTemplateSetRejectsDisabledRequiredDependency(t *testing.T) {
	registry := map[string]starterTemplateMetadata{
		templateCore:               {ID: templateCore},
		templateAgenticEngineering: {ID: templateAgenticEngineering, Requires: []string{templateCore}},
	}

	_, err := resolveTemplateSet([]string{templateAgenticEngineering}, obsidian.WorkflowTemplateAddons{Disabled: []string{templateCore}}, registry)
	require.Error(t, err)
	require.Contains(t, err.Error(), "required dependency")
}

func TestResolveTemplateSetRejectsEnabledAndDisabledAddon(t *testing.T) {
	registry := map[string]starterTemplateMetadata{
		templateCore:        {ID: templateCore},
		templateActionItems: {ID: templateActionItems, Requires: []string{templateCore}},
	}

	_, err := resolveTemplateSet(nil, obsidian.WorkflowTemplateAddons{
		Enabled:  []string{templateActionItems},
		Disabled: []string{templateActionItems},
	}, registry)
	require.Error(t, err)
	require.Contains(t, err.Error(), "both enabled and disabled")
}

func TestResolveTemplateSetRejectsCycles(t *testing.T) {
	registry := map[string]starterTemplateMetadata{
		"a": {ID: "a", Requires: []string{"b"}},
		"b": {ID: "b", Requires: []string{"a"}},
	}

	_, err := resolveTemplateSet([]string{"a"}, obsidian.WorkflowTemplateAddons{}, registry)
	require.Error(t, err)
	require.Contains(t, err.Error(), "cycle")
}

func TestResolveTemplateSetRejectsUnknownTemplates(t *testing.T) {
	_, err := resolveTemplateSet([]string{"missing"}, obsidian.WorkflowTemplateAddons{}, map[string]starterTemplateMetadata{})
	require.Error(t, err)
	require.Contains(t, err.Error(), "unknown template")
}

func TestResolveManagedTemplateSetSkipsEjectedAdoptedStarter(t *testing.T) {
	registry := starterManagementTestRegistry()
	cfg := obsidian.LocalConfig{
		WorkflowTemplates: []string{templateAgenticEngineering},
		WorkflowTemplateAddons: obsidian.WorkflowTemplateAddons{
			Enabled: []string{templateActionItems},
		},
		WorkflowTemplateManagement: obsidian.WorkflowTemplateManagement{
			Ejected: []string{templateAgenticEngineering, templateActionItems},
		},
	}

	resolved, err := resolveManagedTemplateSet(cfg.WorkflowTemplates, cfg.WorkflowTemplateAddons, cfg.WorkflowTemplateManagement, registry)
	require.NoError(t, err)
	require.Empty(t, resolved.Effective)
	require.Equal(t, []string{templateActionItems, templateAgenticEngineering}, resolved.Ejected)
}

func TestResolveManagedTemplateSetRejectsEjectedRequiredDependency(t *testing.T) {
	registry := starterManagementTestRegistry()
	cfg := obsidian.LocalConfig{
		WorkflowTemplates: []string{templateAgenticEngineering},
		WorkflowTemplateManagement: obsidian.WorkflowTemplateManagement{
			Ejected: []string{templateCore},
		},
	}

	_, err := resolveManagedTemplateSet(cfg.WorkflowTemplates, cfg.WorkflowTemplateAddons, cfg.WorkflowTemplateManagement, registry)
	require.Error(t, err)
	require.Contains(t, err.Error(), "required by a managed starter")
}

func TestPlanStarterEjectionEjectsDefaultAddonWithoutEjectingExplicitDependency(t *testing.T) {
	registry := starterManagementTestRegistry()
	cfg := obsidian.LocalConfig{
		WorkflowTemplates: []string{templateCore, templateAgenticEngineering},
		WorkflowTemplateAddons: obsidian.WorkflowTemplateAddons{
			Enabled: []string{templateActionItems},
		},
	}

	plan, err := planStarterEjectionWithRegistry(cfg, templateAgenticEngineering, false, registry)
	require.NoError(t, err)
	require.False(t, plan.Blocked)
	require.Equal(t, []string{templateActionItems, templateAgenticEngineering}, plan.Ejected)
	require.Equal(t, []string{templateActionItems}, plan.DefaultAddonEjections)
	require.Equal(t, []string{templateCore}, plan.RemainingManaged)
}

func TestPlanStarterEjectionBlocksWhenRequiredByManagedStarter(t *testing.T) {
	registry := starterManagementTestRegistry()
	cfg := obsidian.LocalConfig{
		WorkflowTemplates: []string{templateComplexDomain},
	}

	plan, err := planStarterEjectionWithRegistry(cfg, templateAgenticEngineering, false, registry)
	require.NoError(t, err)
	require.True(t, plan.Blocked)
	require.Equal(t, []string{templateComplexDomain}, plan.BlockingDependents)
	require.Equal(t, []string{templateCore, templateAgenticEngineering, templateComplexDomain, templateActionItems}, plan.RemainingManaged)
}

func TestPlanStarterEjectionCascadeEjectsDependents(t *testing.T) {
	registry := starterManagementTestRegistry()
	cfg := obsidian.LocalConfig{
		WorkflowTemplates: []string{templateComplexDomain},
	}

	plan, err := planStarterEjectionWithRegistry(cfg, templateAgenticEngineering, true, registry)
	require.NoError(t, err)
	require.False(t, plan.Blocked)
	require.Equal(t, []string{templateActionItems, templateAgenticEngineering, templateComplexDomain}, plan.Ejected)
	require.Equal(t, []string{templateActionItems}, plan.DefaultAddonEjections)
	require.Empty(t, plan.RemainingManaged)
}

func TestPlanStarterEjectionBlocksCoreWhileSpecDrivenManaged(t *testing.T) {
	registry := starterManagementTestRegistry()
	cfg := obsidian.LocalConfig{
		WorkflowTemplates: []string{templateAgenticEngineering},
	}

	plan, err := planStarterEjectionWithRegistry(cfg, templateCore, false, registry)
	require.NoError(t, err)
	require.True(t, plan.Blocked)
	require.Equal(t, []string{templateActionItems, templateAgenticEngineering}, plan.BlockingDependents)
}

func TestApplyStarterEjectionPlanPersistsOnlyManagementState(t *testing.T) {
	cfg := obsidian.LocalConfig{
		WorkflowTemplates: []string{templateAgenticEngineering},
	}
	changed := applyStarterEjectionPlan(&cfg, StarterEjectionPlan{
		Target:  templateAgenticEngineering,
		Ejected: []string{templateActionItems, templateAgenticEngineering},
	})

	require.True(t, changed)
	require.Equal(t, []string{templateAgenticEngineering}, cfg.WorkflowTemplates)
	require.Equal(t, []string{templateActionItems, templateAgenticEngineering}, cfg.WorkflowTemplateManagement.Ejected)
}

func TestScaffoldCreatesFreshStarterFilesWithoutAsking(t *testing.T) {
	root := t.TempDir()

	report := applyScaffoldForTest(t, root, failUI{t})

	require.Contains(t, report.Created, ".rhizome/ontology/spec-driven.graphql")
	require.Contains(t, report.Created, ".rhizome/query-recipes/spec-driven.yaml")
	require.FileExists(t, filepath.Join(root, ".rhizome", "ontology", "spec-driven.graphql"))
}

func TestScaffoldAsksBeforeReplacingAStarterFileItNeverWrote(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, ".rhizome", "ontology", "spec-driven.graphql")
	require.NoError(t, os.MkdirAll(filepath.Dir(target), 0o755))
	require.NoError(t, os.WriteFile(target, []byte("user-owned schema\n"), 0o644))
	ui := &scriptedUI{unknown: "keep"}

	report := applyScaffoldForTest(t, root, ui)

	require.Equal(t, []string{"unknown:.rhizome/ontology/spec-driven.graphql"}, ui.asked)
	require.NotContains(t, report.changedPaths(), ".rhizome/ontology/spec-driven.graphql")
	content, err := os.ReadFile(target)
	require.NoError(t, err)
	require.Equal(t, "user-owned schema\n", string(content))
}

func starterManagementTestRegistry() map[string]starterTemplateMetadata {
	return map[string]starterTemplateMetadata{
		templateCore:               {ID: templateCore},
		templateActionItems:        {ID: templateActionItems, Requires: []string{templateCore}},
		templateAgenticEngineering: {ID: templateAgenticEngineering, Requires: []string{templateCore}, ActivatesByDefault: []string{templateActionItems}},
		templateComplexDomain:      {ID: templateComplexDomain, Requires: []string{templateAgenticEngineering}},
	}
}
