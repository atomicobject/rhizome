package views

import (
	"context"
	"fmt"
	"testing"
	"testing/fstest"

	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/ontology/viewconfig"
	"github.com/stretchr/testify/require"
)

// bundledViews adds the type and interface Briefings, which share one entry,
// to the bundled group views.
func bundledViews() fstest.MapFS {
	fsys := bundledGroupViews()
	for _, view := range []struct{ id, kind, file string }{
		{TypeBriefingViewID, "type", "type-briefing"},
		{InterfaceBriefingViewID, "interface", "interface-briefing"},
	} {
		fsys["group/"+view.file+".yaml"] = &fstest.MapFile{Data: fmt.Appendf(nil,
			"apiVersion: rhizome.view.v1\nid: %s\nname: Briefing\nsource: {kind: custom, entry: type-briefing.tsx}\nmount: {kind: %s, %s: \"*\"}\n",
			view.id, view.kind, view.kind)}
	}
	fsys["group/type-briefing.tsx"] = &fstest.MapFile{Data: []byte("export default function View() { return null }\n")}
	return fsys
}

func choiceNames(target ViewTarget) []string {
	var names []string
	for _, choice := range target.Choices {
		names = append(names, choice.Name)
	}
	return names
}

func TestTypeBriefingReplacesOverviewForTypesAndInterfaces(t *testing.T) {
	catalog, err := New(ServiceOptions{Schema: deliverySchema(), Bundled: bundledViews()}).Catalog(context.Background())
	require.NoError(t, err)
	require.Empty(t, catalog.Issues)

	spec := findTarget(t, catalog, viewconfig.MountKindType, "Spec")
	require.Equal(t, []string{"Briefing", "Table"}, choiceNames(spec))
	require.Equal(t, ViewChoice{ID: viewChoiceID(TypeBriefingViewID, "custom"), Name: "Briefing", Renderer: "custom", ViewID: TypeBriefingViewID}, spec.Choices[0],
		"the Briefing is a standard choice in Overview's old place")
	require.Equal(t, viewChoiceID(viewconfig.GeneratedTypeID("Spec"), "table"), spec.DefaultChoiceID, "the default still follows the type's shape")

	item := findTarget(t, catalog, viewconfig.MountKindInterface, "Item")
	require.Equal(t, []string{InterfaceBriefingViewID, viewconfig.GeneratedInterfaceID("Item")}, choiceViewIDs(item))
	require.Equal(t, viewChoiceID(viewconfig.GeneratedInterfaceID("Item"), "table"), item.DefaultChoiceID)

	for _, target := range catalog.Targets {
		for _, choice := range target.Choices {
			if target.Kind == viewconfig.MountKindType || target.Kind == viewconfig.MountKindInterface {
				require.NotEqual(t, "builtin:overview", choice.ID, "%s %s offers no Overview", target.Kind, target.Name)
			}
		}
	}
	delivery := findTarget(t, catalog, viewconfig.MountKindGroup, "Delivery")
	require.Equal(t, "builtin:overview", delivery.Choices[0].ID, "Overview stays selectable for display groups")
	require.NotContains(t, choiceViewIDs(delivery), TypeBriefingViewID)
}

func TestTypeTargetsOfferTheirLayoutsWithoutBundledViews(t *testing.T) {
	catalog, err := New(ServiceOptions{Schema: deliverySchema()}).Catalog(context.Background())
	require.NoError(t, err)
	spec := findTarget(t, catalog, viewconfig.MountKindType, "Spec")
	require.Equal(t, []string{"Table"}, choiceNames(spec))
	require.Equal(t, viewChoiceID(viewconfig.GeneratedTypeID("Spec"), "table"), spec.DefaultChoiceID)
}

func TestAuthoredCollectionDefaultsOutrankTheGeneratedDefault(t *testing.T) {
	everyType := customDefinition("every-type", viewconfig.MountSpec{Kind: viewconfig.MountKindType, Type: "*", Default: true})
	efforts := customDefinition("efforts", viewconfig.MountSpec{Kind: viewconfig.MountKindType, Type: "Effort", Default: true})
	catalog, err := New(ServiceOptions{Schema: deliverySchema(), Views: []viewconfig.ViewDefinition{everyType, efforts}, Bundled: bundledViews()}).Catalog(context.Background())
	require.NoError(t, err)
	require.Equal(t, viewChoiceID("efforts", "custom"), findTarget(t, catalog, viewconfig.MountKindType, "Effort").DefaultChoiceID, "an exact default beats a generic one")
	spec := findTarget(t, catalog, viewconfig.MountKindType, "Spec")
	require.Equal(t, viewChoiceID("every-type", "custom"), spec.DefaultChoiceID, "a generic default beats the generated one")
	require.True(t, spec.Choices[len(spec.Choices)-1].Custom)
	require.Equal(t, viewChoiceID(viewconfig.GeneratedInterfaceID("Item"), "table"), findTarget(t, catalog, viewconfig.MountKindInterface, "Item").DefaultChoiceID,
		"a type wildcard does not reach interfaces")
}

func TestEjectedTypeBriefingKeepsItsSlot(t *testing.T) {
	ejected := customDefinition(TypeBriefingViewID, viewconfig.MountSpec{Kind: viewconfig.MountKindType, Type: "*"})
	ejected.Name = "Our briefing"
	catalog, err := New(ServiceOptions{Schema: deliverySchema(), Views: []viewconfig.ViewDefinition{ejected}, Bundled: bundledViews()}).Catalog(context.Background())
	require.NoError(t, err)
	spec := findTarget(t, catalog, viewconfig.MountKindType, "Spec")
	require.Equal(t, []string{"Our briefing", "Table"}, choiceNames(spec))
	require.False(t, spec.Choices[0].Custom)
}

// The Briefing reads its collection from navigation, so a type or interface
// navigation does not list gets no Briefing: a section type, an embedded type
// with no display parent, and an interface with one implementor and no
// display metadata. An embedded type nested under a listed type keeps it.
func TestTypeBriefingOnlyWhereNavigationListsTheCollection(t *testing.T) {
	schema := deliverySchema()
	schema.Types["Steps"] = &ontology.NoteType{Name: "Steps", Role: ontology.TypeRoleSection}
	schema.Types["Step"] = &ontology.NoteType{Name: "Step", Role: ontology.TypeRoleEmbeddedNode}
	schema.Types["Task"] = &ontology.NoteType{Name: "Task", Role: ontology.TypeRoleEmbeddedNode, DisplayParent: "Effort"}
	schema.Types["Memo"] = &ontology.NoteType{Name: "Memo", Role: ontology.TypeRoleNote, Implements: []string{"Hidden"}}
	schema.Interfaces["Hidden"] = &ontology.InterfaceType{Name: "Hidden"}
	catalog, err := New(ServiceOptions{Schema: schema, Bundled: bundledViews()}).Catalog(context.Background())
	require.NoError(t, err)

	for _, name := range []string{"Steps", "Step"} {
		require.NotContains(t, choiceViewIDs(findTarget(t, catalog, viewconfig.MountKindType, name)), TypeBriefingViewID, name)
	}
	require.NotContains(t, choiceViewIDs(findTarget(t, catalog, viewconfig.MountKindInterface, "Hidden")), InterfaceBriefingViewID)
	for _, name := range []string{"Task", "Memo", "Spec"} {
		require.Contains(t, choiceViewIDs(findTarget(t, catalog, viewconfig.MountKindType, name)), TypeBriefingViewID, name)
	}
}
