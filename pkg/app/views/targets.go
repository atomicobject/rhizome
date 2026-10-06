package views

import (
	"cmp"
	"encoding/json"
	"slices"
	"sort"
	"strings"

	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/ontology/viewconfig"
)

type ViewTarget struct {
	Kind            viewconfig.MountKind `json:"kind"`
	Name            string               `json:"name"`
	Choices         []ViewChoice         `json:"choices"`
	DefaultChoiceID string               `json:"defaultChoiceId"`
}

type ViewChoice struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Renderer string `json:"renderer"`
	ViewID   string `json:"viewId,omitempty"`
	Variant  string `json:"variant,omitempty"`
	// Custom marks choices from authored views beyond the subject's standard
	// presentations (built-ins plus the generated or replacing native view).
	Custom bool `json:"custom,omitempty"`
}

func viewChoiceID(viewID, variant string) string {
	encoded, _ := json.Marshal([]string{viewID, variant})
	return "view:" + string(encoded)
}

func resolveTargets(schema *ontology.Schema, entries []CatalogEntry) []ViewTarget {
	targets := map[string]ViewTarget{}
	add := func(kind viewconfig.MountKind, name string) {
		if (name == "" && kind != viewconfig.MountKindWorkspace) || name == "*" {
			return
		}
		key, _ := json.Marshal([]string{string(kind), name})
		targets[string(key)] = ViewTarget{Kind: kind, Name: name}
	}
	add(viewconfig.MountKindWorkspace, "")
	if schema != nil {
		for name, noteType := range schema.Types {
			add(viewconfig.MountKindType, name)
			if viewconfig.IsNodeType(noteType) {
				add(viewconfig.MountKindNode, name)
			}
		}
		for name := range schema.Interfaces {
			add(viewconfig.MountKindInterface, name)
		}
		for name := range viewconfig.DisplayGroups(schema) {
			add(viewconfig.MountKindGroup, name)
		}
	}
	for _, entry := range entries {
		if entry.Mount.Hidden || hasBlockingIssues(entry.Issues) {
			continue
		}
		switch entry.Mount.Kind {
		case viewconfig.MountKindType, viewconfig.MountKindNode:
			add(entry.Mount.Kind, entry.Mount.Type)
		case viewconfig.MountKindInterface:
			add(entry.Mount.Kind, entry.Mount.Interface)
		case viewconfig.MountKindGroup:
			add(entry.Mount.Kind, entry.Mount.Group)
		case viewconfig.MountKindStandalone:
			add(entry.Mount.Kind, entry.ID)
		}
	}
	tree := viewconfig.DisplayTree(schema)
	listed := tree.Listed()
	out := make([]ViewTarget, 0, len(targets))
	for _, target := range targets {
		shape := targetShape{listed: listed[target.Name]}
		if target.Kind == viewconfig.MountKindGroup && schema != nil {
			shape.roots, shape.linked = len(tree.Roots[target.Name]), groupLinked(schema, tree, target.Name)
		}
		out = append(out, resolveTarget(target, entries, shape))
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Kind != out[j].Kind {
			return out[i].Kind < out[j].Kind
		}
		return out[i].Name < out[j].Name
	})
	return out
}

// Bundled group views (SPEC-0111). Their ids keep these rules after a
// repository view replaces them, so an ejected copy behaves like the original.
const (
	OverviewViewID          = "group.overview"
	WorkspaceBriefingViewID = "workspace.briefing"
	WorkspaceOverviewViewID = "workspace.overview"
	BriefingViewID          = "group.briefing"
	TraceViewID             = "group.trace"
	SectionsViewID          = "group.sections"
)

func isGroupViewID(id string) bool {
	return id == OverviewViewID || id == BriefingViewID || id == TraceViewID || id == SectionsViewID
}

// The bundled type Briefing (SPEC-0112), one definition per collection kind
// over one entry. Like the group views, the ids keep the standard slot for a
// repository copy.
const (
	TypeBriefingViewID      = "type.briefing"
	InterfaceBriefingViewID = "interface.briefing"
)

func isCollectionViewID(kind viewconfig.MountKind, id string) bool {
	return (kind == viewconfig.MountKindType && id == TypeBriefingViewID) ||
		(kind == viewconfig.MountKindInterface && id == InterfaceBriefingViewID)
}

// isGenericMount reports whether a mount names every subject of its kind.
func isGenericMount(mount viewconfig.MountSpec) bool {
	switch mount.Kind {
	case viewconfig.MountKindType:
		return mount.Type == "*"
	case viewconfig.MountKindInterface:
		return mount.Interface == "*"
	case viewconfig.MountKindGroup:
		return mount.Group == "*"
	}
	return false
}

// targetShape is what choices and defaults read from navigation: for a
// display group, its number of roots and whether two of its member types link
// to each other (Trace's applicability); for a type or interface, whether
// navigation lists it, which the bundled Briefing needs to load.
type targetShape struct {
	roots  int
	linked bool
	listed bool
}

// layoutLabels lists native layouts in choice order.
var layoutLabels = []struct{ variant, label string }{{"table", "Table"}, {"card", "Cards"}, {"kanban", "Board"}}

// resolveTarget lists standard choices (built-ins, then the subject's own
// native layouts) before custom ones (other authored views, by order then ID).
// A type or interface's own layouts come from its generated view unless a
// valid replaceGenerated view takes that slot; its bundled Briefing, offered
// only where navigation lists it, comes first, and it has no Overview
// (SPEC-0112). For a display group, Overview leads, the bundled group views
// (matched by id, so an ejected copy keeps the role) are the standard choices,
// Trace only when the group's roots link, and the default follows the group's
// shape (SPEC-0111).
func resolveTarget(target ViewTarget, entries []CatalogEntry, shape targetShape) ViewTarget {
	target.Choices = []ViewChoice{}
	switch target.Kind {
	case viewconfig.MountKindGroup:
		target.Choices = append(target.Choices, ViewChoice{ID: "builtin:overview", Name: "Overview", Renderer: "overview"})
		target.DefaultChoiceID = "builtin:overview"
	case viewconfig.MountKindNode:
		target.Choices = append(target.Choices, ViewChoice{ID: "builtin:read", Name: "Structured", Renderer: "read"}, ViewChoice{ID: "builtin:source", Name: "Source", Renderer: "source"})
		target.DefaultChoiceID = "builtin:read"
	}
	var matching []CatalogEntry
	for _, entry := range entries {
		if entry.Mount.Hidden || hasBlockingIssues(entry.Issues) {
			continue
		}
		if !viewconfig.MatchesMount(entry.Mount, target.Kind, target.Name) {
			continue
		}
		if target.Kind == viewconfig.MountKindStandalone && entry.ID != target.Name {
			continue
		}
		if target.Kind == viewconfig.MountKindGroup && entry.ID == TraceViewID && !shape.linked {
			continue
		}
		if isCollectionViewID(target.Kind, entry.ID) && !shape.listed {
			continue
		}
		matching = append(matching, entry)
	}
	sort.SliceStable(matching, func(i, j int) bool {
		if matching[i].Mount.Order != matching[j].Mount.Order {
			return matching[i].Mount.Order < matching[j].Mount.Order
		}
		return matching[i].ID < matching[j].ID
	})
	if slices.ContainsFunc(matching, func(entry CatalogEntry) bool { return entry.ID == OverviewViewID }) && target.Kind == viewconfig.MountKindGroup {
		target.Choices = nil
		target.DefaultChoiceID = viewChoiceID(OverviewViewID, "custom")
	}
	var standard *CatalogEntry
	var generated *CatalogEntry
	var briefing []CatalogEntry
	var groupViews []CatalogEntry
	var custom []CatalogEntry
	for i, entry := range matching {
		switch {
		case isCollectionViewID(target.Kind, entry.ID):
			briefing = append(briefing, entry)
		case (target.Kind == viewconfig.MountKindGroup && isGroupViewID(entry.ID)) ||
			(target.Kind == viewconfig.MountKindWorkspace && (entry.ID == WorkspaceBriefingViewID || entry.ID == WorkspaceOverviewViewID)):
			groupViews = append(groupViews, entry)
		case entry.Generated:
			generated = &matching[i]
		case target.Kind == viewconfig.MountKindStandalone:
			standard = &matching[i]
		case standard == nil && viewconfig.ReplacesGenerated(entry.Definition):
			standard = &matching[i]
		default:
			custom = append(custom, entry)
		}
	}
	if standard == nil {
		standard = generated
	}
	for _, entry := range briefing {
		target.Choices = append(target.Choices, entryChoices(entry, false)...)
	}
	if standard != nil {
		target.Choices = append(target.Choices, entryChoices(*standard, false)...)
	}
	// Standard scope views have fixed ordering, including repository overrides.
	sort.SliceStable(groupViews, func(i, j int) bool {
		rank := func(id string) int {
			switch id {
			case OverviewViewID, WorkspaceBriefingViewID:
				return 0
			case WorkspaceOverviewViewID:
				return 1
			default:
				return 2
			}
		}
		return rank(groupViews[i].ID) < rank(groupViews[j].ID)
	})
	for _, entry := range groupViews {
		target.Choices = append(target.Choices, entryChoices(entry, false)...)
	}
	for _, entry := range custom {
		target.Choices = append(target.Choices, entryChoices(entry, true)...)
	}

	// The subject's own layouts outrank its built-in fallback; an authored
	// default outranks them, with an exact default beating a generic ("*")
	// one. Ties keep the first by order, then ID.
	defaultPriority := 0
	consider := func(entry CatalogEntry, priority int) {
		if variant := defaultVariant(entry); variant != "" && priority > defaultPriority {
			target.DefaultChoiceID = viewChoiceID(entry.ID, variant)
			defaultPriority = priority
		}
	}
	if standard != nil {
		consider(*standard, 1)
	}
	for _, entry := range matching {
		if entry.Generated || !entry.Mount.Default || target.Kind == viewconfig.MountKindStandalone {
			continue
		}
		if isGenericMount(entry.Mount) {
			consider(entry, 2)
		} else {
			consider(entry, 3)
		}
	}
	// Without an authored default, a group opens Briefing when it has several
	// roots to brief on and Sections when it has one, else whichever of the two
	// is available.
	if target.Kind == viewconfig.MountKindWorkspace && defaultPriority == 0 {
		for _, choice := range target.Choices {
			if choice.ViewID == WorkspaceBriefingViewID {
				target.DefaultChoiceID = choice.ID
				break
			}
		}
	}
	if target.Kind == viewconfig.MountKindGroup && defaultPriority == 0 {
		preference := []string{SectionsViewID, BriefingViewID}
		if shape.roots >= 2 {
			preference = []string{BriefingViewID, SectionsViewID}
		}
		for _, want := range preference {
			if i := slices.IndexFunc(target.Choices, func(choice ViewChoice) bool { return choice.ViewID == want }); i >= 0 {
				target.DefaultChoiceID = target.Choices[i].ID
				break
			}
		}
	}
	if target.DefaultChoiceID == "" && len(target.Choices) > 0 {
		target.DefaultChoiceID = target.Choices[0].ID
	}
	return target
}

// groupLinked reports whether two distinct member roots of a group link to
// each other through a forward @link field. An interface root stands for its
// implementing node types. A link counts only when it targets a member root or
// a type inside one, so a field typed by a broad interface such as Note does
// not make every group linked, and implementors of one root linking to each
// other (a spec's successor) do not count. Members claim types in railMembers
// order, as the bundled views' planMembers does, so a type implementing two
// roots belongs to the same one in both.
func groupLinked(schema *ontology.Schema, tree viewconfig.NavigationTree, group string) bool {
	rootOf := map[string]string{}
	roots := map[string]bool{}
	for _, name := range railMembers(schema, tree, group) {
		roots[name] = true
		for _, typeName := range concreteTypes(schema, name) {
			if _, claimed := rootOf[typeName]; !claimed {
				rootOf[typeName] = name
			}
		}
	}
	for name, root := range rootOf {
		for _, field := range schema.Types[name].Fields {
			if field == nil || field.Kind != ontology.FieldKindLink {
				continue
			}
			var targets []string
			if _, member := rootOf[field.TypeName]; member {
				targets = []string{field.TypeName}
			} else if roots[field.TypeName] {
				targets = concreteTypes(schema, field.TypeName)
			}
			for _, target := range targets {
				if targetRoot, ok := rootOf[target]; ok && targetRoot != root {
					return true
				}
			}
		}
	}
	return false
}

// concreteTypes is the node types a member stands for: itself, or an
// interface's node implementors, the only types with records to show.
func concreteTypes(schema *ontology.Schema, name string) []string {
	if nt, ok := schema.Types[name]; ok && nt != nil {
		return []string{name}
	}
	if _, ok := schema.Interfaces[name]; !ok {
		return nil
	}
	var out []string
	for typeName, nt := range schema.Types {
		if viewconfig.IsNodeType(nt) && slices.Contains(nt.Implements, name) {
			out = append(out, typeName)
		}
	}
	return out
}

// railMembers lists a group's members in the display-groups API's order:
// each root, sorted by the label the rail shows, followed by its descendants,
// sorted the same way.
func railMembers(schema *ontology.Schema, tree viewconfig.NavigationTree, group string) []string {
	byLabel := func(names []string) []string {
		sort.SliceStable(names, func(i, j int) bool {
			left, right := railLabel(schema, names[i]), railLabel(schema, names[j])
			if left == right {
				return names[i] < names[j]
			}
			return RailLess(left, right)
		})
		return names
	}
	var out []string
	var visit func(name string)
	visit = func(name string) {
		out = append(out, name)
		for _, child := range byLabel(tree.Children(name)) {
			visit(child)
		}
	}
	for _, root := range byLabel(slices.Clone(tree.Roots[group])) {
		visit(root)
	}
	return out
}

// railLabel is the label the rail shows for a member: its plural label, else
// its label, else its name.
func railLabel(schema *ontology.Schema, name string) string {
	var label, plural string
	if nt := schema.Types[name]; nt != nil {
		label, plural = nt.Label, nt.PluralLabel
	} else if iface := schema.Interfaces[name]; iface != nil {
		label, plural = iface.Label, iface.PluralLabel
	}
	return cmp.Or(plural, label, name)
}

// RailLess orders group names and member labels as the Notes rail does.
// ponytail: approximates the rail's localeCompare with a case-insensitive
// comparison; use golang.org/x/text/collate if accented labels must match.
func RailLess(left, right string) bool {
	if l, r := strings.ToLower(left), strings.ToLower(right); l != r {
		return l < r
	}
	return left < right
}

// entryChoices names the subject's own layouts plainly and qualifies custom
// ones with the view name.
func entryChoices(entry CatalogEntry, custom bool) []ViewChoice {
	if entry.Source.Kind == viewconfig.SourceKindCustom {
		return []ViewChoice{{ID: viewChoiceID(entry.ID, "custom"), Name: entry.Name, Renderer: "custom", ViewID: entry.ID, Custom: custom}}
	}
	var out []ViewChoice
	for _, layout := range layoutLabels {
		if !slices.Contains(entry.AvailableVariants, layout.variant) {
			continue
		}
		name := layout.label
		if custom {
			name = entry.Name + " · " + layout.label
		}
		out = append(out, ViewChoice{ID: viewChoiceID(entry.ID, layout.variant), Name: name, Renderer: layout.variant, ViewID: entry.ID, Variant: layout.variant, Custom: custom})
	}
	return out
}

// defaultVariant is the choice a view opens as a default: its declared
// variant when available, else its first layout in choice order.
func defaultVariant(entry CatalogEntry) string {
	if entry.Source.Kind == viewconfig.SourceKindCustom {
		return "custom"
	}
	if slices.Contains(entry.AvailableVariants, entry.Defaults.Variant) {
		return entry.Defaults.Variant
	}
	for _, layout := range layoutLabels {
		if slices.Contains(entry.AvailableVariants, layout.variant) {
			return layout.variant
		}
	}
	return ""
}
