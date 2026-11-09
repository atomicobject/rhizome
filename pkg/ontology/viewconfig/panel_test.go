package viewconfig

import (
	"testing"

	ontoschema "github.com/atomicobject/rhizome/pkg/ontology/schema"
	"github.com/stretchr/testify/require"
)

// panelSchema has a Knowledge group shared by AO KB types (Claim, Source),
// an Agentic Engineering type (Spec), and a compounding type (Finding); an
// Evidence interface; embedded types outside every group's tree; and a
// vault-local note type with no group.
func panelSchema() *ontoschema.Schema {
	note := func(group string, implements ...string) *ontoschema.NoteType {
		return &ontoschema.NoteType{Role: ontoschema.TypeRoleNote, DisplayGroup: group, Implements: implements}
	}
	return &ontoschema.Schema{
		Types: map[string]*ontoschema.NoteType{
			"Claim":    note("Knowledge", "Evidence"),
			"Decision": note("Knowledge", "Evidence"),
			"Source":   note("Knowledge"),
			"Spec":     note("Knowledge"),
			"Finding":  note("Knowledge"),
			"Local":    note(""),
			"Quote":    {Role: ontoschema.TypeRoleEmbeddedNode},
			"Excerpt":  {Role: ontoschema.TypeRoleEmbeddedNode},
		},
		Interfaces: map[string]*ontoschema.InterfaceType{"Evidence": {DisplayGroup: "Knowledge"}},
	}
}

func experienceViews(exp *Experience, mounts map[string]MountSpec) []ViewDefinition {
	var out []ViewDefinition
	for id, mount := range mounts {
		out = append(out, ViewDefinition{ID: id, Mount: mount, Experience: exp})
	}
	return out
}

func viewPanel(exp *Experience) Panel {
	return Panel{Kind: PanelView, ViewID: exp.Panel, ExperienceKey: exp.Key()}
}

func TestPanelResolutionLevels(t *testing.T) {
	aokb := &Experience{Title: "Knowledge", Module: "ao-kb", Folder: "knowledge", Group: "Knowledge", Types: []string{"Excerpt"}, Panel: "aokb.panel"}
	team := &Experience{Title: "Team", Folder: "team", Types: []string{"Spec"}, Panel: "team.panel"}
	evidence := &Experience{Title: "Evidence", Module: "zeta", Folder: "z", Types: []string{"Evidence"}, Panel: "z.panel"}
	noPanel := &Experience{Title: "Notes", Folder: "notes", Types: []string{"Source"}}
	var views []ViewDefinition
	views = append(views, experienceViews(aokb, map[string]MountSpec{
		"aokb.home":  {Kind: MountKindGroup, Group: "Knowledge"},
		"aokb.claim": {Kind: MountKindNode, Type: "Claim"},
		"aokb.about": {Kind: MountKindStandalone},
		"aokb.any":   {Kind: MountKindType, Type: "*"},
		"aokb.panel": {Kind: MountKindPanel},
	})...)
	views = append(views, experienceViews(team, map[string]MountSpec{"team.panel": {Kind: MountKindPanel}})...)
	views = append(views, experienceViews(evidence, map[string]MountSpec{"z.panel": {Kind: MountKindPanel}})...)
	views = append(views, experienceViews(noPanel, map[string]MountSpec{"notes.page": {Kind: MountKindStandalone}})...)
	views = append(views, ViewDefinition{ID: "loose", Mount: MountSpec{Kind: MountKindStandalone}})

	r := NewPanelResolver(panelSchema(), views)
	for _, tc := range []struct {
		kind MountKind
		name string
		want Panel
	}{
		// (1) the experience that owns a standalone view, with or without a panel.
		{MountKindStandalone, "aokb.about", viewPanel(aokb)},
		{MountKindStandalone, "notes.page", Panel{Kind: PanelNone}},
		{MountKindStandalone, "loose", Panel{Kind: PanelNone}},
		{MountKindWorkspace, "", Panel{Kind: PanelNone}},
		// (2) the concrete type by mount or types, for nodes and collections.
		{MountKindNode, "Claim", viewPanel(aokb)},
		{MountKindType, "Claim", viewPanel(aokb)},
		// A team experience's types outrank AO KB's group.
		{MountKindNode, "Spec", viewPanel(team)},
		// An embedded type listed in an experience's types keeps that panel.
		{MountKindNode, "Excerpt", viewPanel(aokb)},
		// (3) an interface of the type outranks a group; an interface
		// collection starts here.
		{MountKindNode, "Decision", viewPanel(evidence)},
		{MountKindInterface, "Evidence", viewPanel(evidence)},
		// (4) through the group, for every starter's types in it. An
		// experience without a panel never competes.
		{MountKindNode, "Source", viewPanel(aokb)},
		{MountKindNode, "Finding", viewPanel(aokb)},
		{MountKindType, "Finding", viewPanel(aokb)},
		{MountKindGroup, "Knowledge", viewPanel(aokb)},
		// (5) Folder for a note no experience presents; a collection has no note.
		{MountKindNode, "Local", Panel{Kind: PanelFolder}},
		{MountKindType, "Local", Panel{Kind: PanelNone}},
		{MountKindGroup, "Other", Panel{Kind: PanelNone}},
		// The same embedded type unlisted, in no group's tree, defers to its owner.
		{MountKindNode, "Quote", Panel{Kind: PanelOwner}},
		{MountKindType, "Quote", Panel{Kind: PanelNone}},
	} {
		require.Equal(t, tc.want, r.Resolve(tc.kind, tc.name), "%s %q", tc.kind, tc.name)
	}
	require.Empty(t, r.tieIssues())
}

func TestPanelTiesWarnAndViewsRootOrderWins(t *testing.T) {
	flat := &Experience{Title: "Flat", Folder: "b", Group: "Knowledge", Panel: "flat.panel", Path: "/v/b/experience.yml"}
	module := &Experience{Title: "Module", Module: "aaa", Folder: "a", Group: "Knowledge", Panel: "module.panel", Path: "/m/a/experience.yml"}
	quiet := &Experience{Title: "Quiet", Folder: "c", Group: "Knowledge", Path: "/v/c/experience.yml"}
	var views []ViewDefinition
	views = append(views, experienceViews(module, map[string]MountSpec{"module.panel": {Kind: MountKindPanel}})...)
	views = append(views, experienceViews(flat, map[string]MountSpec{"flat.panel": {Kind: MountKindPanel}})...)
	views = append(views, experienceViews(quiet, map[string]MountSpec{"quiet.page": {Kind: MountKindStandalone}})...)
	r := NewPanelResolver(panelSchema(), views)
	require.Equal(t, viewPanel(flat), r.Resolve(MountKindNode, "Source"), "the flat folder precedes modules")
	issues := r.tieIssues()
	require.Len(t, issues, 2, "one warning per experience for the pair, not one per target")
	for _, issue := range issues {
		require.Equal(t, "experience_panel_tie", issue.Code)
		require.Equal(t, IssueWarning, issue.Severity)
		require.Equal(t, "panel", issue.Field)
	}
	require.ElementsMatch(t, []string{flat.Path, module.Path}, []string{issues[0].Path, issues[1].Path})
}

func TestValidateWarnsOnPanelTiesWithASchema(t *testing.T) {
	schema := panelSchema()
	a := &Experience{Title: "A", Folder: "a", Group: "Knowledge", Panel: "a.panel", Path: "a/experience.yml"}
	b := &Experience{Title: "B", Folder: "b", Group: "Knowledge", Panel: "b.panel", Path: "b/experience.yml"}
	panel := func(id string, exp *Experience) ViewDefinition {
		return ViewDefinition{APIVersion: APIVersion, ID: id, Name: id, SourceSpec: SourceSpec{Kind: SourceKindCustom, Entry: "panel.tsx"}, Mount: MountSpec{Kind: MountKindPanel}, Experience: exp}
	}
	result := Validate([]ViewDefinition{panel("a.panel", a), panel("b.panel", b)}, SchemaValidateOptions(schema))
	requireIssueCode(t, result.Issues, "experience_panel_tie")
	for _, issue := range result.Issues {
		require.NotEqual(t, IssueFatal, issue.Severity, "%#v", issue)
	}
}
