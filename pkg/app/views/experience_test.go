package views

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	ontoschema "github.com/atomicobject/rhizome/pkg/ontology/schema"
	"github.com/atomicobject/rhizome/pkg/ontology/viewconfig"
	"github.com/stretchr/testify/require"
)

func writeVaultFile(t *testing.T, vault, rel, body string) {
	t.Helper()
	path := filepath.Join(vault, filepath.FromSlash(rel))
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, []byte(body), 0o644))
}

func writeCustomView(t *testing.T, vault, dir, id, mount, entry, source string) {
	t.Helper()
	writeVaultFile(t, vault, dir+"/"+id+".yaml", "apiVersion: rhizome.view.v1\nid: "+id+"\nname: "+id+"\nsource: {kind: custom, entry: "+entry+"}\nmount: "+mount+"\n")
	writeVaultFile(t, vault, dir+"/"+entry, source)
}

func knowledgeSchema() *ontoschema.Schema {
	return &ontoschema.Schema{Types: map[string]*ontoschema.NoteType{
		"Claim":  {Name: "Claim", Role: ontoschema.TypeRoleNote, DisplayGroup: "Knowledge"},
		"Spec":   {Name: "Spec", Role: ontoschema.TypeRoleNote, DisplayGroup: "Knowledge"},
		"Quote":  {Name: "Quote", Role: ontoschema.TypeRoleEmbeddedNode},
		"Memo":   {Name: "Memo", Role: ontoschema.TypeRoleNote},
		"Broken": {Name: "Broken", Role: ontoschema.TypeRoleNote},
	}}
}

func catalogTarget(t *testing.T, catalog Catalog, kind viewconfig.MountKind, name string) ViewTarget {
	t.Helper()
	for _, target := range catalog.Targets {
		if target.Kind == kind && target.Name == name {
			return target
		}
	}
	t.Fatalf("no %s target %q", kind, name)
	return ViewTarget{}
}

func TestCatalogCarriesExperienceKeysAndPanels(t *testing.T) {
	vault := t.TempDir()
	site := ".rhizome/modules/ao-kb/views/knowledge"
	writeVaultFile(t, vault, site+"/experience.yml", "title: Knowledge\ngroup: Knowledge\npanel: knowledge.panel\n")
	writeCustomView(t, vault, site, "knowledge.home", "{kind: group, group: Knowledge}", "home.tsx", "export default function Home() { return null }")
	writeCustomView(t, vault, site, "knowledge.claim", "{kind: node, type: Claim}", "claim.tsx", "export default function Claim() { return null }")
	writeCustomView(t, vault, site, "knowledge.legacy", "{kind: node, type: Claim}", "legacy.html", "<html></html>")
	writeCustomView(t, vault, site, "knowledge.named", "{kind: node, type: Claim}", "named.tsx", "export function Named() { return null }")
	writeCustomView(t, vault, site, "knowledge.panel", "{kind: panel}", "panel.tsx", "export default function Panel() { return null }")
	// An invalid experience.yml leaves its views as ordinary views.
	writeVaultFile(t, vault, ".rhizome/views/broken/experience.yml", "group: Knowledge\n")
	writeCustomView(t, vault, ".rhizome/views/broken", "broken.page", "{kind: node, type: Broken}", "page.tsx", "export default 1")

	service := New(ServiceOptions{VaultPath: vault, Schema: knowledgeSchema()})
	catalog, err := service.Catalog(context.Background())
	require.NoError(t, err)

	keys := map[string]string{}
	for _, entry := range catalog.Views {
		keys[entry.ID] = entry.ExperienceKey
	}
	key := keys["knowledge.home"]
	require.NotEmpty(t, key)
	require.Equal(t, key, keys["knowledge.claim"])
	require.Equal(t, key, keys["knowledge.panel"], "the panel view names its experience for preference scoping")
	require.Empty(t, keys["knowledge.legacy"], "an HTML entry keeps its own frame")
	require.Empty(t, keys["knowledge.named"], "an entry without a default export keeps its own frame")
	require.Empty(t, keys["broken.page"])
	brokenFile := filepath.Join(vault, ".rhizome/views/broken/experience.yml")
	var reported bool
	for _, issue := range catalog.Issues {
		reported = reported || (issue.Path == brokenFile && issue.Field == "title")
	}
	require.True(t, reported, "%#v", catalog.Issues)

	claim := catalogTarget(t, catalog, viewconfig.MountKindNode, "Claim")
	for _, choice := range claim.Choices {
		require.NotEqual(t, "knowledge.panel", choice.ViewID, "a panel is never a view choice")
		require.Equal(t, keys[choice.ViewID], choice.ExperienceKey, choice.ID)
	}
	panel := viewconfig.Panel{Kind: viewconfig.PanelView, ViewID: "knowledge.panel", ExperienceKey: key}
	require.Equal(t, panel, claim.Panel)
	require.Equal(t, panel, catalogTarget(t, catalog, viewconfig.MountKindNode, "Spec").Panel, "another starter's type in the group")
	require.Equal(t, panel, catalogTarget(t, catalog, viewconfig.MountKindGroup, "Knowledge").Panel)
	require.Equal(t, panel, catalogTarget(t, catalog, viewconfig.MountKindType, "Claim").Panel)
	require.Equal(t, viewconfig.Panel{Kind: viewconfig.PanelFolder}, catalogTarget(t, catalog, viewconfig.MountKindNode, "Memo").Panel)
	require.Equal(t, viewconfig.Panel{Kind: viewconfig.PanelFolder}, catalogTarget(t, catalog, viewconfig.MountKindNode, "Broken").Panel)
	require.Equal(t, viewconfig.Panel{Kind: viewconfig.PanelOwner}, catalogTarget(t, catalog, viewconfig.MountKindNode, "Quote").Panel)
	require.Equal(t, viewconfig.Panel{Kind: viewconfig.PanelNone}, catalogTarget(t, catalog, viewconfig.MountKindWorkspace, "").Panel)
	for _, target := range catalog.Targets {
		require.NotEqual(t, "knowledge.panel", target.Name, "a panel is not listed among the sidebar's views")
		require.NotEmpty(t, target.Panel.Kind, "%s %q", target.Kind, target.Name)
	}

	got, err := service.Panel(context.Background(), viewconfig.MountKindNode, "Claim")
	require.NoError(t, err)
	require.Equal(t, panel, got)
}

// Hidden views stay out of the list and every target's choices but count
// toward panels, as validation's tie check counts them (SPEC-0136 R6).
func TestHiddenViewsTakePartInPanelResolution(t *testing.T) {
	vault := t.TempDir()
	site := ".rhizome/views/notes"
	writeVaultFile(t, vault, site+"/experience.yml", "title: Notes\npanel: notes.panel\n")
	writeCustomView(t, vault, site, "notes.panel", "{kind: panel}", "panel.tsx", "export default function Panel() { return null }")
	writeCustomView(t, vault, site, "notes.secret", "{kind: standalone, hidden: true}", "secret.tsx", "export default function Secret() { return null }")
	writeCustomView(t, vault, site, "notes.memo", "{kind: node, type: Memo, hidden: true}", "memo.tsx", "export default function Memo() { return null }")

	service := New(ServiceOptions{VaultPath: vault, Schema: knowledgeSchema()})
	catalog, err := service.Catalog(context.Background())
	require.NoError(t, err)
	panel := viewconfig.Panel{Kind: viewconfig.PanelView, ViewID: "notes.panel"}
	for _, entry := range catalog.Views {
		require.NotEqual(t, "notes.secret", entry.ID, "a hidden view is not listed")
		if entry.ID == "notes.panel" {
			panel.ExperienceKey = entry.ExperienceKey
		}
	}
	require.NotEmpty(t, panel.ExperienceKey)
	memo := catalogTarget(t, catalog, viewconfig.MountKindNode, "Memo")
	require.Equal(t, panel, memo.Panel, "a hidden view's mount presents its type")
	for _, choice := range memo.Choices {
		require.NotEqual(t, "notes.memo", choice.ViewID, "a hidden view is never a choice")
	}
	secret := catalogTarget(t, catalog, viewconfig.MountKindStandalone, "notes.secret")
	require.Equal(t, panel, secret.Panel, "a tab showing a hidden standalone view reads its experience's panel")
	require.Empty(t, secret.Choices, "a hidden standalone view is never a choice")

	got, err := service.Panel(context.Background(), viewconfig.MountKindStandalone, "notes.secret")
	require.NoError(t, err)
	require.Equal(t, panel, got, "a hidden standalone view's experience owns its panel")
}
