package views

import (
	"bytes"
	"context"
	"fmt"
	"testing"
	"testing/fstest"

	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/ontology/viewconfig"
	"github.com/stretchr/testify/require"
)

func bundledGroupViews() fstest.MapFS {
	fsys := fstest.MapFS{}
	for i, view := range []struct{ id, name, file string }{
		{BriefingViewID, "Briefing", "briefing"},
		{TraceViewID, "Trace", "trace"},
		{SectionsViewID, "Sections", "sections"},
	} {
		fsys["group/"+view.file+".yaml"] = &fstest.MapFile{Data: fmt.Appendf(nil,
			"apiVersion: rhizome.view.v1\nid: %s\nname: %s\nsource: {kind: custom, entry: %s.tsx}\nmount: {kind: group, group: \"*\", order: %d}\n",
			view.id, view.name, view.file, i+1)}
		fsys["group/"+view.file+".tsx"] = &fstest.MapFile{Data: []byte("export default function View() { return null }\n")}
	}
	return fsys
}

// deliverySchema has a Delivery group with two roots (Spec, Effort) where an
// effort links its spec, and a Library group with one interface root whose two
// implementors never link to each other.
func deliverySchema() *ontology.Schema {
	return &ontology.Schema{
		Types: map[string]*ontology.NoteType{
			"Spec":   {Name: "Spec", Role: ontology.TypeRoleNote, DisplayGroup: "Delivery"},
			"Effort": {Name: "Effort", Role: ontology.TypeRoleNote, DisplayGroup: "Delivery", Fields: []*ontology.Field{{Name: "spec", Kind: ontology.FieldKindLink, TypeName: "Spec"}}},
			"Book":   {Name: "Book", Role: ontology.TypeRoleNote, Implements: []string{"Item"}, Fields: []*ontology.Field{{Name: "sequel", Kind: ontology.FieldKindLink, TypeName: "Book"}}},
			"Paper":  {Name: "Paper", Role: ontology.TypeRoleNote, Implements: []string{"Item"}},
		},
		Interfaces: map[string]*ontology.InterfaceType{"Item": {Name: "Item", DisplayGroup: "Library"}},
	}
}

func choiceViewIDs(target ViewTarget) []string {
	var ids []string
	for _, choice := range target.Choices {
		if choice.ViewID != "" {
			ids = append(ids, choice.ViewID)
		}
	}
	return ids
}

func TestBundledViewsJoinCatalogWithOrigin(t *testing.T) {
	repository := customDefinition("team", viewconfig.MountSpec{Kind: viewconfig.MountKindStandalone})
	catalog, err := New(ServiceOptions{Schema: deliverySchema(), Views: []viewconfig.ViewDefinition{repository}, Bundled: bundledGroupViews()}).Catalog(context.Background())
	require.NoError(t, err)
	require.Empty(t, catalog.Issues)
	origins := map[string]viewconfig.Origin{}
	for _, entry := range catalog.Views {
		origins[entry.ID] = entry.Origin
	}
	require.Equal(t, viewconfig.OriginRepository, origins["team"])
	require.Equal(t, viewconfig.OriginBundled, origins[BriefingViewID])
	require.Equal(t, viewconfig.OriginBundled, origins[SectionsViewID])
	require.Equal(t, viewconfig.OriginGenerated, origins[viewconfig.GeneratedTypeID("Spec")])
}

func TestRepositoryViewReplacesBundledViewWithSameID(t *testing.T) {
	ejected := customDefinition(BriefingViewID, viewconfig.MountSpec{Kind: viewconfig.MountKindGroup, Group: "*", Order: 1})
	ejected.Name = "Our briefing"
	service := New(ServiceOptions{Schema: deliverySchema(), Views: []viewconfig.ViewDefinition{ejected}, Bundled: bundledGroupViews()})
	catalog, err := service.Catalog(context.Background())
	require.NoError(t, err)
	require.Empty(t, catalog.Issues)
	count := 0
	for _, entry := range catalog.Views {
		if entry.ID == BriefingViewID {
			count++
		}
	}
	require.Equal(t, 1, count)
	entry, err := service.View(context.Background(), BriefingViewID)
	require.NoError(t, err)
	require.Equal(t, viewconfig.OriginRepository, entry.Origin)
	require.Equal(t, "Our briefing", entry.Name)
	delivery := findTarget(t, catalog, viewconfig.MountKindGroup, "Delivery")
	require.Equal(t, viewChoiceID(BriefingViewID, "custom"), delivery.DefaultChoiceID, "the replacement keeps the id's default-by-shape")
	var names []string
	for _, choice := range delivery.Choices {
		names = append(names, choice.Name)
	}
	require.Equal(t, []string{"Overview", "Our briefing", "Trace", "Sections"}, names, "the repository copy takes the bundled Briefing's slot, once")
}

func TestGroupDefaultFollowsShapeWithoutAuthoredDefault(t *testing.T) {
	catalog, err := New(ServiceOptions{Schema: deliverySchema(), Bundled: bundledGroupViews()}).Catalog(context.Background())
	require.NoError(t, err)
	delivery := findTarget(t, catalog, viewconfig.MountKindGroup, "Delivery")
	require.Equal(t, viewChoiceID(BriefingViewID, "custom"), delivery.DefaultChoiceID, "two roots open Briefing")
	library := findTarget(t, catalog, viewconfig.MountKindGroup, "Library")
	require.Equal(t, viewChoiceID(SectionsViewID, "custom"), library.DefaultChoiceID, "one root opens Sections")
	for _, target := range []ViewTarget{delivery, library} {
		require.Equal(t, "builtin:overview", target.Choices[0].ID, "Overview stays selectable")
	}
}

func TestGroupDefaultFallsBackToTheOtherBundledView(t *testing.T) {
	broken := bundledGroupViews()
	delete(broken, "group/briefing.tsx")
	catalog, err := New(ServiceOptions{Schema: deliverySchema(), Bundled: broken}).Catalog(context.Background())
	require.NoError(t, err)
	require.Equal(t, viewChoiceID(SectionsViewID, "custom"), findTarget(t, catalog, viewconfig.MountKindGroup, "Delivery").DefaultChoiceID,
		"two roots with a broken Briefing open Sections")

	hidden := bundledGroupViews()
	yaml := hidden["group/sections.yaml"]
	yaml.Data = append(bytes.TrimSuffix(yaml.Data, []byte("}\n")), []byte(", hidden: true}\n")...)
	catalog, err = New(ServiceOptions{Schema: deliverySchema(), Bundled: hidden}).Catalog(context.Background())
	require.NoError(t, err)
	require.Equal(t, viewChoiceID(BriefingViewID, "custom"), findTarget(t, catalog, viewconfig.MountKindGroup, "Library").DefaultChoiceID,
		"one root with a hidden Sections opens Briefing")
}

func TestAuthoredGroupDefaultsOutrankBundledDefault(t *testing.T) {
	exact := customDefinition("delivery", viewconfig.MountSpec{Kind: viewconfig.MountKindGroup, Group: "Delivery", Default: true})
	generic := customDefinition("any-group", viewconfig.MountSpec{Kind: viewconfig.MountKindGroup, Group: "*", Default: true})
	catalog, err := New(ServiceOptions{Schema: deliverySchema(), Views: []viewconfig.ViewDefinition{exact, generic}, Bundled: bundledGroupViews()}).Catalog(context.Background())
	require.NoError(t, err)
	require.Equal(t, viewChoiceID("delivery", "custom"), findTarget(t, catalog, viewconfig.MountKindGroup, "Delivery").DefaultChoiceID)
	require.Equal(t, viewChoiceID("any-group", "custom"), findTarget(t, catalog, viewconfig.MountKindGroup, "Library").DefaultChoiceID)
}

func TestTraceIsOfferedOnlyForLinkedGroups(t *testing.T) {
	catalog, err := New(ServiceOptions{Schema: deliverySchema(), Bundled: bundledGroupViews()}).Catalog(context.Background())
	require.NoError(t, err)
	require.Equal(t, []string{BriefingViewID, TraceViewID, SectionsViewID}, choiceViewIDs(findTarget(t, catalog, viewconfig.MountKindGroup, "Delivery")),
		"an effort links its spec")
	require.Equal(t, []string{BriefingViewID, SectionsViewID}, choiceViewIDs(findTarget(t, catalog, viewconfig.MountKindGroup, "Library")),
		"a book linking another book is a self link, not a link between member types")
}

func TestTraceResolvesInterfaceRootsThroughImplementors(t *testing.T) {
	schema := deliverySchema()
	schema.Types["Review"] = &ontology.NoteType{Name: "Review", Role: ontology.TypeRoleNote, DisplayGroup: "Library",
		Fields: []*ontology.Field{{Name: "subject", Kind: ontology.FieldKindLink, TypeName: "Item"}}}
	catalog, err := New(ServiceOptions{Schema: schema, Bundled: bundledGroupViews()}).Catalog(context.Background())
	require.NoError(t, err)
	require.Contains(t, choiceViewIDs(findTarget(t, catalog, viewconfig.MountKindGroup, "Library")), TraceViewID,
		"a review links the Item root, which stands for books and papers")
}

func TestTraceIgnoresLinksWithinOneRootAndToBroadInterfaces(t *testing.T) {
	schema := deliverySchema()
	schema.Types["Paper"].Fields = []*ontology.Field{{Name: "cites", Kind: ontology.FieldKindLink, TypeName: "Item", List: true}}
	schema.Interfaces["Note"] = &ontology.InterfaceType{Name: "Note"}
	schema.Types["Spec"].Implements = []string{"Note"}
	schema.Types["Effort"].Fields = []*ontology.Field{{Name: "about", Kind: ontology.FieldKindLink, TypeName: "Note"}}
	catalog, err := New(ServiceOptions{Schema: schema, Bundled: bundledGroupViews()}).Catalog(context.Background())
	require.NoError(t, err)
	require.NotContains(t, choiceViewIDs(findTarget(t, catalog, viewconfig.MountKindGroup, "Library")), TraceViewID,
		"papers citing books stay inside the Item root")
	require.NotContains(t, choiceViewIDs(findTarget(t, catalog, viewconfig.MountKindGroup, "Delivery")), TraceViewID,
		"a link typed by a non-member interface does not connect member roots")
}

// Trace's rows and columns come from the bundled view's planMembers, which
// claims each concrete node type for the first member in the API's member
// order (rail labels, not names). Applicability must claim the same way.
func TestTraceClaimsTypesInRailOrder(t *testing.T) {
	schema := &ontology.Schema{
		Types: map[string]*ontology.NoteType{
			// Shared implements both roots; the Beta root's label sorts first, so
			// Beta claims it, and a Solo link to Shared crosses roots.
			"Shared": {Name: "Shared", Role: ontology.TypeRoleNote, Implements: []string{"Alpha", "Beta"}},
			"Solo":   {Name: "Solo", Role: ontology.TypeRoleNote, Implements: []string{"Alpha"}, Fields: []*ontology.Field{{Name: "shared", Kind: ontology.FieldKindLink, TypeName: "Shared"}}},
			"Other":  {Name: "Other", Role: ontology.TypeRoleNote, Implements: []string{"Beta"}},
		},
		Interfaces: map[string]*ontology.InterfaceType{
			"Alpha": {Name: "Alpha", PluralLabel: "Zeta things", DisplayGroup: "Pair"},
			"Beta":  {Name: "Beta", PluralLabel: "Alpha things", DisplayGroup: "Pair"},
		},
	}
	catalog, err := New(ServiceOptions{Schema: schema, Bundled: bundledGroupViews()}).Catalog(context.Background())
	require.NoError(t, err)
	require.Contains(t, choiceViewIDs(findTarget(t, catalog, viewconfig.MountKindGroup, "Pair")), TraceViewID)
}

func TestTraceCountsOnlyNodeImplementors(t *testing.T) {
	schema := deliverySchema()
	schema.Types["Shelf"] = &ontology.NoteType{Name: "Shelf", Role: ontology.TypeRoleNote, DisplayGroup: "Library"}
	schema.Types["Blurb"] = &ontology.NoteType{Name: "Blurb", Role: ontology.TypeRoleSection, Implements: []string{"Item"},
		Fields: []*ontology.Field{{Name: "shelf", Kind: ontology.FieldKindLink, TypeName: "Shelf"}}}
	catalog, err := New(ServiceOptions{Schema: schema, Bundled: bundledGroupViews()}).Catalog(context.Background())
	require.NoError(t, err)
	require.NotContains(t, choiceViewIDs(findTarget(t, catalog, viewconfig.MountKindGroup, "Library")), TraceViewID,
		"a section type has no records for Trace to show")
}

func TestBundledEntriesAreValidatedInTheirOwnFilesystem(t *testing.T) {
	fsys := bundledGroupViews()
	delete(fsys, "group/trace.tsx")
	catalog, err := New(ServiceOptions{Schema: deliverySchema(), Bundled: fsys}).Catalog(context.Background())
	require.NoError(t, err)
	require.Len(t, catalog.Issues, 1)
	require.Equal(t, "custom_entry_not_found", catalog.Issues[0].Code)
	require.Equal(t, TraceViewID, catalog.Issues[0].View)
	require.NotContains(t, choiceViewIDs(findTarget(t, catalog, viewconfig.MountKindGroup, "Delivery")), TraceViewID)
}

func TestNoBundledViewsKeepsOverviewGroupDefault(t *testing.T) {
	catalog, err := New(ServiceOptions{Schema: deliverySchema()}).Catalog(context.Background())
	require.NoError(t, err)
	require.Equal(t, "builtin:overview", findTarget(t, catalog, viewconfig.MountKindGroup, "Delivery").DefaultChoiceID)
}

func TestBundledGroupViewsAreStandardChoicesBeforeCustomOnes(t *testing.T) {
	team := customDefinition("team-dashboard", viewconfig.MountSpec{Kind: viewconfig.MountKindGroup, Group: "Delivery"})
	catalog, err := New(ServiceOptions{Schema: deliverySchema(), Views: []viewconfig.ViewDefinition{team}, Bundled: bundledGroupViews()}).Catalog(context.Background())
	require.NoError(t, err)
	delivery := findTarget(t, catalog, viewconfig.MountKindGroup, "Delivery")
	var standard, custom []string
	for _, choice := range delivery.Choices {
		if choice.Custom {
			custom = append(custom, choice.ViewID)
		} else if choice.ViewID != "" {
			standard = append(standard, choice.ViewID)
		}
	}
	require.Equal(t, []string{BriefingViewID, TraceViewID, SectionsViewID}, standard, "the switcher shows group views as plain segments")
	require.Equal(t, []string{"team-dashboard"}, custom)
	require.Equal(t, "team-dashboard", delivery.Choices[len(delivery.Choices)-1].ViewID, "custom choices follow the standard ones")
}
