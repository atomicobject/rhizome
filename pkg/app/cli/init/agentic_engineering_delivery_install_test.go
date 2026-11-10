package init

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/atomicobject/rhizome/internal/testkit"
	"github.com/atomicobject/rhizome/pkg/app/userstate"
	appviews "github.com/atomicobject/rhizome/pkg/app/views"
	"github.com/atomicobject/rhizome/pkg/ontology/viewconfig"
	"github.com/atomicobject/rhizome/pkg/validate"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/atomicobject/rhizome/pkg/vault/repoconfig"
	"github.com/stretchr/testify/require"
)

const deliveryInstallRoot = ".rhizome/modules/agentic-engineering/views/delivery/"

var deliveryNativeBaseline = map[string]string{
	"efforts.yaml":       "74e3810c2c89941e0c2e6f036eb71d35c762aeb06781bfc64de8715c9efec537",
	"feature-areas.yaml": "79a2c23d9da7b18da84ad3edca3088793f461648b5669491ee5c27c00fa2b567",
	"specs.yaml":         "50b804397b2ee138dfdf7e11a4e2ab9971d4c8c9cf64134b161e43bbc1e54c6b",
	"user-stories.yaml":  "249c69287538b817d599003f181317ca1fe1d7a5bfb9373975ccb0f4a8b4c091",
}

func installDeliveryFixture(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	writeProjectConfig(t, root, "notes:\n  includes: [\"**/*.md\", \"**/*.html\"]\n", repoconfig.LocalWorkflowConfig{})
	out, err := runInit(t, RunOptions{Env: testkit.Env(t), Dir: root, Workflow: "agentic-engineering", Agents: "claude,codex", Search: "off"})
	require.NoError(t, err, out)
	return root
}

func TestAgenticEngineeringDeliveryFreshInstall(t *testing.T) {
	root := installDeliveryFixture(t)
	require.FileExists(t, filepath.Join(root, deliveryInstallRoot, "experience.yml"))
	for _, skill := range []string{".agents/skills/agentic-engineering/SKILL.md", ".claude/skills/agentic-engineering/SKILL.md"} {
		installed, err := os.ReadFile(filepath.Join(root, skill))
		require.NoError(t, err)
		source, err := os.ReadFile("templates/starters/agentic-engineering/agents/skills/agentic-engineering/SKILL.md")
		require.NoError(t, err)
		require.Equal(t, normalizeTemplateNewlines(string(source)), normalizeTemplateNewlines(string(installed)), "prompt route tests must cover the installed workflow")
	}
	requireDeliveryAssetsManaged(t, root)
	requireDeliveryNativeBaseline(t, root)
	requireDeliveryRegistry(t, root)
	requireDeliveryCheck(t, root)
}

func TestAgenticEngineeringDeliveryUpdateManagedInstall(t *testing.T) {
	root := installDeliveryFixture(t)
	out, err := runInit(t, RunOptions{Env: testkit.Env(t), Dir: root, Agents: "claude"})
	require.NoError(t, err, out)
	agentPreferences := *loadConfigForTest(t, root).Agents
	requireDeliveryNativeBaseline(t, root)
	require.NoError(t, os.RemoveAll(filepath.Join(root, deliveryInstallRoot)))
	record, ok := loadGeneratedFiles(root)
	require.True(t, ok)
	for key := range record.Written {
		if strings.HasPrefix(key, deliveryInstallRoot) {
			record.forget(key)
		}
	}
	require.NoError(t, record.save(root))
	for _, def := range deliveryDefinitions(t, root) {
		require.False(t, strings.HasPrefix(def.ID, "agentic-engineering."), "old install already has Delivery registration")
	}
	authored := map[string]string{
		"docs/specs/product/team.md":         "---\ntype: ProductSpec\nid: SPEC-TEAM\nspec-status: proposed\n---\n# Team contract\n\nKeep authored scope.\n",
		"docs/efforts/team.md":               "---\ntype: EffortNote\nid: EFF-TEAM\nstatus: active\n---\n# Team effort\n\nKeep authored evidence.\n",
		"docs/engineering/testing-policy.md": "# Team tests\nUse our existing checks.\n",
		".rhizome/views/team.yaml":           "apiVersion: rhizome.view.v1\nid: team.local\nname: Team\nsource: {kind: custom, entry: team.tsx}\nmount: {kind: standalone}\n",
		".rhizome/views/team.tsx":            "export default function Team() { return <p>Team</p>; }\n",
	}
	for name, content := range authored {
		writeTestFile(t, root, name, content)
	}
	beforeConfig := readTestFile(t, root, ".rhizome/workflows.yml")
	out, err = runInit(t, RunOptions{Env: testkit.Env(t), Dir: root})
	require.NoError(t, err, out)
	require.Equal(t, agentPreferences, *loadConfigForTest(t, root).Agents, "update preserves disabled agent guidance")
	require.FileExists(t, filepath.Join(root, deliveryInstallRoot, "experience.yml"))
	for name, content := range authored {
		require.Equal(t, content, readTestFile(t, root, name), name)
	}
	require.Equal(t, beforeConfig, readTestFile(t, root, ".rhizome/workflows.yml"))
	requireDeliveryAssetsManaged(t, root)
	requireDeliveryNativeBaseline(t, root)
	requireDeliveryRegistry(t, root)
	requireDeliveryCheck(t, root)
}

func TestAgenticEngineeringDeliveryEjectPreservesLocalExperience(t *testing.T) {
	root := installDeliveryFixture(t)
	require.FileExists(t, filepath.Join(root, deliveryInstallRoot, "experience.yml"))
	out, err := runInit(t, RunOptions{Env: testkit.Env(t), Dir: root, Eject: "agentic-engineering"})
	require.NoError(t, err, out)
	require.Contains(t, loadConfigForTest(t, root).WorkflowTemplateManagement.Ejected, "agentic-engineering")
	defs := deliveryDefinitions(t, root)
	for _, id := range []string{"agentic-engineering.home", "agentic-engineering.panel"} {
		def := deliveryDefinition(t, defs, id)
		rel, err := filepath.Rel(root, def.Source.Path)
		require.NoError(t, err)
		content := readTestFile(t, root, rel)
		writeTestFile(t, root, rel, strings.Replace(content, def.Name, "Team "+def.Name, 1))
		entry, err := filepath.Rel(root, filepath.Join(filepath.Dir(def.Source.Path), def.SourceSpec.Entry))
		require.NoError(t, err)
		writeTestFile(t, root, entry, "// Team customization.\n"+readTestFile(t, root, entry))
	}
	manifest := deliveryInstallRoot + "experience.yml"
	writeTestFile(t, root, manifest, readTestFile(t, root, manifest)+"\n# Team experience customization.\n")
	writeTestFile(t, root, deliveryInstallRoot+"team.yaml", "apiVersion: rhizome.view.v1\nid: team.delivery\nname: Team delivery\nsource: {kind: custom, entry: team.tsx}\nmount: {kind: group, group: Delivery}\n")
	writeTestFile(t, root, deliveryInstallRoot+"team.tsx", "export default function TeamDelivery() { return <p>Team delivery</p>; }\n")
	scope := userstate.Scope{ViewID: "agentic-engineering.home", Context: userstate.Context{Kind: viewconfig.MountKindGroup, Group: "Delivery"}}
	state, err := userstate.Open(context.Background(), root)
	require.NoError(t, err)
	preference, err := state.Patch(context.Background(), scope, 0, map[string]json.RawMessage{"density": json.RawMessage(`"compact"`)}, nil)
	require.NoError(t, err)
	require.NoError(t, state.Close())
	ownership, ok := loadGeneratedFiles(root)
	require.True(t, ok)
	before := aoKBFiles(t, filepath.Join(root, ".rhizome/modules/agentic-engineering"))
	management := readTestFile(t, root, ".rhizome/workflows.yml")
	requireDeliveryCheck(t, root)
	require.Equal(t, before, aoKBFiles(t, filepath.Join(root, ".rhizome/modules/agentic-engineering")))
	require.Equal(t, management, readTestFile(t, root, ".rhizome/workflows.yml"))
	record, ok := loadGeneratedFiles(root)
	require.True(t, ok)
	registry, err := loadStarterTemplateMetadataRegistry()
	require.NoError(t, err)
	cfg := loadConfigForTest(t, root)
	managed, err := resolveManagedTemplateSet(cfg.WorkflowTemplates, cfg.WorkflowTemplateAddons, cfg.WorkflowTemplateManagement, registry)
	require.NoError(t, err)
	require.NotContains(t, managed.Effective, "agentic-engineering", "historical hashes do not confer managed ownership after eject")
	require.Equal(t, ownership.Written, record.Written, "init leaves the ejected ownership history intact")
	state, err = userstate.Open(context.Background(), root)
	require.NoError(t, err)
	after, err := state.Read(context.Background(), scope)
	require.NoError(t, err)
	require.Equal(t, preference, after)
	require.NoError(t, state.Close())
	requireDeliveryNativeBaseline(t, root)
	requireDeliveryRegistry(t, root)
	srv := serveEjectedVault(t, root)
	page := getInvocationJSON(t, srv, "team.delivery", `{"kind":"group","group":"Delivery"}`)
	panel := getInvocationJSON(t, srv, "agentic-engineering.panel", `{"kind":"group","group":"Delivery"}`)
	require.Equal(t, page.View.ExperienceKey, panel.View.ExperienceKey)
	require.True(t, strings.HasPrefix(panel.View.Name, "Team "), "registry resolves the edited panel")
}

func requireDeliveryCheck(t *testing.T, root string) {
	t.Helper()
	out, err := runInit(t, RunOptions{Env: testkit.Env(t), Dir: root})
	require.NoError(t, err, out)
	before := aoKBFiles(t, root)
	out, err = runInit(t, RunOptions{Env: testkit.Env(t), Dir: root, Check: true})
	require.NoError(t, err, out)
	require.Equal(t, before, aoKBFiles(t, root), "check mode changes no installed or authored file")
}

func requireDeliveryNativeBaseline(t *testing.T, root string) {
	t.Helper()
	for name, hash := range deliveryNativeBaseline {
		content := strings.ReplaceAll(readTestFile(t, root, ".rhizome/modules/agentic-engineering/views/"+name), "\r\n", "\n")
		require.Equal(t, hash, fmt.Sprintf("%x", sha256.Sum256([]byte(content))), name)
	}
}

func requireDeliveryAssetsManaged(t *testing.T, root string) {
	t.Helper()
	sources := aoKBFiles(t, "templates/starters/agentic-engineering/module/views/delivery")
	require.Contains(t, sources, "experience.yml")
	for _, name := range []string{"spec.tsx", "effort.tsx"} {
		require.Contains(t, sources, name)
	}
	var hasCSS, hasShared bool
	record, ok := loadGeneratedFiles(root)
	require.True(t, ok)
	for name, source := range sources {
		hasCSS = hasCSS || strings.HasSuffix(name, ".css")
		hasShared = hasShared || strings.HasPrefix(name, "lib/")
		installed := readTestFile(t, root, deliveryInstallRoot+name)
		require.Equal(t, source, installed, name)
		require.Equal(t, contentFingerprint([]byte(installed)), record.Written[deliveryInstallRoot+name], name)
	}
	require.True(t, hasCSS, "experience carries its stylesheet")
	require.True(t, hasShared, "experience carries shared logic")
}

func deliveryDefinitions(t *testing.T, root string) []viewconfig.ViewDefinition {
	t.Helper()
	defs, issues := viewconfig.LoadDefaultSource(root)
	require.Empty(t, issues)
	seen := map[string]bool{}
	for _, def := range defs {
		require.False(t, seen[def.ID], "duplicate view %s", def.ID)
		seen[def.ID] = true
	}
	return defs
}

func deliveryDefinition(t *testing.T, defs []viewconfig.ViewDefinition, id string) viewconfig.ViewDefinition {
	t.Helper()
	for _, def := range defs {
		if def.ID == id {
			return def
		}
	}
	t.Fatalf("installed view %s missing", id)
	return viewconfig.ViewDefinition{}
}

func requireDeliveryRegistry(t *testing.T, root string) {
	t.Helper()
	defs := deliveryDefinitions(t, root)
	home := deliveryDefinition(t, defs, "agentic-engineering.home")
	require.Equal(t, viewconfig.MountKindGroup, home.Mount.Kind)
	require.Equal(t, "Delivery", home.Mount.Group)
	require.True(t, home.Mount.Default)
	stage := deliveryDefinition(t, defs, "agentic-engineering.stage")
	require.Equal(t, viewconfig.MountKindStandalone, stage.Mount.Kind)
	require.True(t, stage.Mount.Hidden)
	requireDeliveryParam(t, stage, "stage", true)
	for suffix, typeName := range map[string]string{
		"spec.process": "ProcessSpec", "spec.product": "ProductSpec", "spec.technical": "TechnicalSpec",
		"spec.experience": "ExperienceSpec", "spec.operations": "OperationsSpec",
		"effort.note": "EffortNote", "effort.workspace": "EffortWorkspace",
	} {
		def := deliveryDefinition(t, defs, "agentic-engineering."+suffix)
		require.Equal(t, viewconfig.MountKindNode, def.Mount.Kind)
		require.Equal(t, typeName, def.Mount.Type)
		require.True(t, def.Mount.Default)
		entry := "effort.tsx"
		if strings.HasPrefix(suffix, "spec.") {
			entry = "spec.tsx"
			requireDeliveryParam(t, def, "story", false)
		}
		require.Equal(t, entry, def.SourceSpec.Entry)
	}
	result := validate.RunViews(context.Background(), validate.RunContext{
		VaultDef: obsidian.VaultDefinition{Path: root}, VaultPath: root, NoteReader: &obsidian.Note{}, MaxIssues: 100,
	})
	require.True(t, result.OK, "%+v", result.Issues)
	srv := serveEjectedVault(t, root)
	var catalog appviews.Catalog
	getJSON(t, srv, "/api/v1/views", &catalog)
	require.Empty(t, catalog.Issues)
	entries := map[string]appviews.CatalogEntry{}
	for _, entry := range catalog.Views {
		entries[entry.ID] = entry
	}
	key := entries[home.ID].ExperienceKey
	require.NotEmpty(t, key, "default exports participate in the experience host")
	require.Equal(t, key, entries["agentic-engineering.panel"].ExperienceKey)
	require.NotContains(t, entries, stage.ID, "hidden stage stays out of public discovery")
	for _, id := range []string{"spec-driven.specs", "spec-driven.efforts", "spec-driven.user-stories", "spec-driven.feature-areas"} {
		require.Contains(t, entries, id)
		require.Empty(t, entries[id].Issues, id)
	}
	var group *appviews.ViewTarget
	for i := range catalog.Targets {
		if catalog.Targets[i].Kind == viewconfig.MountKindGroup && catalog.Targets[i].Name == "Delivery" {
			group = &catalog.Targets[i]
		}
	}
	require.NotNil(t, group)
	require.Equal(t, viewconfig.Panel{Kind: viewconfig.PanelView, ViewID: "agentic-engineering.panel", ExperienceKey: key}, group.Panel)
	var defaultFound bool
	for _, choice := range group.Choices {
		defaultFound = defaultFound || choice.ID == group.DefaultChoiceID && choice.ViewID == home.ID
	}
	require.True(t, defaultFound, "Delivery opens home")
	page := getInvocationJSON(t, srv, home.ID, `{"kind":"group","group":"Delivery"}`)
	resp := get(t, srv, page.Entry)
	require.Equal(t, http.StatusOK, resp.StatusCode, readBody(t, resp))
}

func requireDeliveryParam(t *testing.T, def viewconfig.ViewDefinition, name string, identity bool) {
	t.Helper()
	for _, param := range def.Params {
		if param.Name == name {
			require.Equal(t, viewconfig.ParamKindString, param.Kind)
			require.Equal(t, identity, param.Identity)
			return
		}
	}
	t.Fatalf("view %s lacks parameter %s", def.ID, name)
}
