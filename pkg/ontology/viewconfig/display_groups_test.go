package viewconfig

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/stretchr/testify/require"
)

func TestDisplayGroupsFollowNavigationRoots(t *testing.T) {
	schema := &ontology.Schema{Types: map[string]*ontology.NoteType{
		"Effort":   {Name: "Effort", Role: ontology.TypeRoleNote, DisplayGroup: "ChildOnly", Implements: []string{"Delivery"}},
		"Spec":     {Name: "Spec", Role: ontology.TypeRoleNote, Implements: []string{"Delivery"}},
		"Embedded": {Name: "Embedded", Role: ontology.TypeRoleEmbeddedNode, DisplayGroup: "EmbeddedOnly"},
		"Material": {Name: "Material", Role: ontology.TypeRoleNote, DisplayParent: "Delivery", DisplayGroup: "AnotherChild"},
		"Plain":    {Name: "Plain", Role: ontology.TypeRoleNote},
	}, Interfaces: map[string]*ontology.InterfaceType{"Delivery": {Name: "Delivery", DisplayGroup: "Delivery"}}}
	require.Equal(t, map[string]struct{}{"Delivery": {}, "Other": {}}, DisplayGroups(schema))
}

func TestGroupMountsValidateEffectiveGroupsAndGenericTarget(t *testing.T) {
	for _, group := range []string{"Delivery", "*", "Missing"} {
		def := ViewDefinition{APIVersion: APIVersion, ID: "group", Name: "Group", SourceSpec: SourceSpec{Kind: SourceKindCustom, Entry: "index.html"}, Mount: MountSpec{Kind: MountKindGroup, Group: group}}
		issues := Validate([]ViewDefinition{def}, ValidateOptions{CheckReferences: true, GroupNames: map[string]struct{}{"Delivery": {}}}).Issues
		if group == "Missing" {
			requireIssueCode(t, issues, "invalid_mount_target")
		} else {
			require.Empty(t, issues)
		}
	}
}

func TestDisplayGroupsRespectExplicitParentAndIgnoreInvisibleInterfaces(t *testing.T) {
	schema := &ontology.Schema{Types: map[string]*ontology.NoteType{
		"Root":  {Role: ontology.TypeRoleNote, DisplayGroup: "RootGroup"},
		"Child": {Role: ontology.TypeRoleNote, DisplayParent: "Root", DisplayGroup: "ChildGroup", Implements: []string{"Collection"}},
		"Peer":  {Role: ontology.TypeRoleNote, Implements: []string{"Collection"}},
	}, Interfaces: map[string]*ontology.InterfaceType{
		"Collection": {DisplayGroup: "CollectionGroup"},
		"Invisible":  {},
	}}
	require.Equal(t, map[string]struct{}{"RootGroup": {}, "CollectionGroup": {}}, DisplayGroups(schema))
	require.Empty(t, DisplayGroups(nil))
	require.Empty(t, DisplayGroups(&ontology.Schema{}))
}

func TestDisplayGroupsResolveCyclesDeterministically(t *testing.T) {
	// A hand-built schema can bypass ontology's cycle checks. Navigation still
	// keeps a root and never follows an unbounded parent cycle.
	schema := &ontology.Schema{Interfaces: map[string]*ontology.InterfaceType{
		"A": {Label: "Same", DisplayGroup: "AGroup", DisplayParent: "B"},
		"B": {Label: "Same", DisplayGroup: "BGroup", DisplayParent: "A"},
	}}
	for i := 0; i < 50; i++ {
		require.Equal(t, map[string]struct{}{"BGroup": {}}, DisplayGroups(schema))
	}
}

func TestDisplayGroupsMatchSharedRailFixture(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "..", "testdata", "display-groups", "rail-groups.json"))
	require.NoError(t, err)
	type item struct {
		Name, Role, Label, PluralLabel, DisplayGroup, DisplayParent string
		Implements                                                  []string
	}
	var fixture struct {
		Types, Interfaces []item
		Expected          struct {
			SummaryTypes      []string
			SummaryInterfaces map[string][]string
			Parents           map[string]string
			Groups            []struct {
				Name  string
				Roots []string
			}
		}
	}
	require.NoError(t, json.Unmarshal(data, &fixture))
	schema := &ontology.Schema{Types: map[string]*ontology.NoteType{}, Interfaces: map[string]*ontology.InterfaceType{}}
	for _, it := range fixture.Types {
		schema.Types[it.Name] = &ontology.NoteType{Name: it.Name, Role: ontology.TypeRole(it.Role), Label: it.Label, PluralLabel: it.PluralLabel, DisplayGroup: it.DisplayGroup, DisplayParent: it.DisplayParent, Implements: it.Implements}
	}
	for _, it := range fixture.Interfaces {
		schema.Interfaces[it.Name] = &ontology.InterfaceType{Name: it.Name, Label: it.Label, PluralLabel: it.PluralLabel, DisplayGroup: it.DisplayGroup, DisplayParent: it.DisplayParent}
	}
	types, interfaces := NavigationMembers(schema)
	require.Equal(t, fixture.Expected.SummaryTypes, types)
	require.Equal(t, fixture.Expected.SummaryInterfaces, interfaces)
	tree := DisplayTree(schema)
	require.Equal(t, fixture.Expected.Parents, tree.Parents)
	want := map[string][]string{}
	for _, group := range fixture.Expected.Groups {
		want[group.Name] = group.Roots
	}
	require.Equal(t, want, tree.Roots)
}

func TestWildcardTypeAndInterfaceMountsApplyToEveryCollection(t *testing.T) {
	everyType := MountSpec{Kind: MountKindType, Type: "*"}
	everyInterface := MountSpec{Kind: MountKindInterface, Interface: "*"}
	require.True(t, MatchesMount(everyType, MountKindType, "Spec"))
	require.True(t, MatchesMount(everyInterface, MountKindInterface, "SpecLike"))
	require.False(t, MatchesMount(everyType, MountKindInterface, "SpecLike"), "a type wildcard is not an interface wildcard")
	require.False(t, MatchesMount(MountSpec{Kind: MountKindNode, Type: "*"}, MountKindNode, "Spec"), "node mounts name one type")

	opts := ValidateOptions{
		CheckReferences: true,
		TypeNames:       map[string]struct{}{"Spec": {}},
		InterfaceNames:  map[string]struct{}{"SpecLike": {}},
		NodeTypeNames:   map[string]struct{}{"Spec": {}},
	}
	custom := func(mount MountSpec) ViewDefinition {
		return ViewDefinition{APIVersion: APIVersion, ID: "v", Name: "V", SourceSpec: SourceSpec{Kind: SourceKindCustom, Entry: "index.html"}, Mount: mount}
	}
	require.Empty(t, Validate([]ViewDefinition{custom(everyType)}, opts).Issues)
	require.Empty(t, Validate([]ViewDefinition{custom(everyInterface)}, opts).Issues)
	requireIssueField(t, Validate([]ViewDefinition{custom(MountSpec{Kind: MountKindNode, Type: "*"})}, opts).Issues, "invalid_mount_target", "mount.type")

	// One native view cannot take every generated view's slot.
	native := ViewDefinition{APIVersion: APIVersion, ID: "n", Name: "N", SourceSpec: SourceSpec{Kind: SourceKindOntologyType, Type: "Spec"},
		Mount:    MountSpec{Kind: MountKindType, Type: "*", ReplaceGenerated: true},
		Variants: VariantSet{Table: &TableVariant{Columns: []ViewColumn{{Field: "title"}}}}}
	require.False(t, ReplacesGenerated(native))
	requireIssueField(t, Validate([]ViewDefinition{native}, opts).Issues, "unexpected_field", "mount.replaceGenerated")

	// A native view reads its own source, so on every collection it shows that one type.
	for _, mount := range []MountSpec{everyType, everyInterface} {
		native.Mount = mount
		issues := Validate([]ViewDefinition{native}, opts).Issues
		field := "mount.type"
		if mount.Kind == MountKindInterface {
			field = "mount.interface"
		}
		requireIssueField(t, issues, "generic_native_mount", field)
		require.Equal(t, IssueWarning, issues[0].Severity)
	}
}
