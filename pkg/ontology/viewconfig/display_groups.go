package viewconfig

import (
	"sort"
	"strings"

	"github.com/atomicobject/rhizome/pkg/ontology"
)

type displayNode struct {
	group, parent, label string
	embedded             bool
}

// IsNodeType reports whether a type is listed in Notes navigation and can be
// a node mount target: only note and embedded-node roles qualify.
func IsNodeType(nt *ontology.NoteType) bool {
	return nt != nil && (nt.Role == ontology.TypeRoleNote || nt.Role == ontology.TypeRoleEmbeddedNode)
}

// NavigationMembers returns the sorted node types and the visible interfaces,
// each with its sorted node implementors, that the ontology summary lists.
// The summary endpoint and DisplayGroups share it so group mounts and the web
// rail (web/src/components/noteRailGroups.ts) see the same members.
func NavigationMembers(schema *ontology.Schema) (types []string, interfaces map[string][]string) {
	interfaces = map[string][]string{}
	if schema == nil {
		return nil, interfaces
	}
	implementors := map[string][]string{}
	presentationParents := map[string]bool{}
	for name, nt := range schema.Types {
		if nt != nil && strings.TrimSpace(nt.DisplayParent) != "" {
			presentationParents[strings.TrimSpace(nt.DisplayParent)] = true
		}
		if !IsNodeType(nt) {
			continue
		}
		types = append(types, name)
		for _, iface := range nt.Implements {
			implementors[iface] = append(implementors[iface], name)
		}
	}
	for _, iface := range schema.Interfaces {
		if iface != nil && strings.TrimSpace(iface.DisplayParent) != "" {
			presentationParents[strings.TrimSpace(iface.DisplayParent)] = true
		}
	}
	for name, iface := range schema.Interfaces {
		// Trim like DisplayTree and the rail: whitespace-only presentation
		// metadata must not make an interface visible.
		if iface == nil || (len(implementors[name]) < 2 && !presentationParents[name] && strings.TrimSpace(iface.DisplayGroup) == "" && strings.TrimSpace(iface.DisplayParent) == "") {
			continue
		}
		impls := append([]string{}, implementors[name]...)
		sort.Strings(impls)
		interfaces[name] = impls
	}
	sort.Strings(types)
	return types, interfaces
}

// DisplayGroups returns the groups with roots in the Notes navigation. It uses
// the summary's visible interfaces and parent precedence, not ontology membership.
func DisplayGroups(schema *ontology.Schema) map[string]struct{} {
	groups := map[string]struct{}{}
	for group := range DisplayTree(schema).Roots {
		groups[group] = struct{}{}
	}
	return groups
}

// NavigationTree is the Notes rail's hierarchy of navigation members. Roots
// maps each group, including the "Other" pseudo-group for ungrouped roots, to
// its root members in name order; Parents maps every nested member to the
// member it appears under.
type NavigationTree struct {
	Parents map[string]string
	Roots   map[string][]string
}

// Children returns the members nested directly under parent, in name order.
func (t NavigationTree) Children(parent string) []string {
	var children []string
	for child, p := range t.Parents {
		if p == parent {
			children = append(children, child)
		}
	}
	sort.Strings(children)
	return children
}

// Members returns every member shown under group: its roots and all of their
// descendants.
func (t NavigationTree) Members(group string) []string {
	var out []string
	var visit func(name string)
	visit = func(name string) {
		out = append(out, name)
		for _, child := range t.Children(name) {
			visit(child)
		}
	}
	for _, root := range t.Roots[group] {
		visit(root)
	}
	return out
}

// Listed returns every member the tree shows: each group's roots and their
// descendants. A type or interface outside it, such as a section type or an
// embedded type with no display parent, has no place in navigation.
func (t NavigationTree) Listed() map[string]bool {
	listed := map[string]bool{}
	for group := range t.Roots {
		for _, name := range t.Members(group) {
			listed[name] = true
		}
	}
	return listed
}

// DisplayTree mirrors buildRailGroups in web/src/components/noteRailGroups.ts.
// testdata/display-groups/rail-groups.json pins both to the same answer.
func DisplayTree(schema *ontology.Schema) NavigationTree {
	tree := NavigationTree{Parents: map[string]string{}, Roots: map[string][]string{}}
	typeNames, visibleInterfaces := NavigationMembers(schema)
	nodes := map[string]displayNode{}
	for _, name := range typeNames {
		nt := schema.Types[name]
		nodes[name] = displayNode{strings.TrimSpace(nt.DisplayGroup), strings.TrimSpace(nt.DisplayParent), displayLabel(name, nt.Label, nt.PluralLabel), nt.Role == ontology.TypeRoleEmbeddedNode}
	}
	interfaces := make([]string, 0, len(visibleInterfaces))
	for name := range visibleInterfaces {
		iface := schema.Interfaces[name]
		nodes[name] = displayNode{strings.TrimSpace(iface.DisplayGroup), strings.TrimSpace(iface.DisplayParent), displayLabel(name, iface.Label, iface.PluralLabel), false}
		interfaces = append(interfaces, name)
	}
	names := make([]string, 0, len(nodes))
	for name := range nodes {
		names = append(names, name)
	}
	sort.Strings(names)
	parents := tree.Parents
	setParent := func(child, parent string) {
		if child == parent || parents[child] != "" {
			return
		}
		if _, ok := nodes[parent]; !ok {
			return
		}
		for ancestor := parent; ancestor != ""; ancestor = parents[ancestor] {
			if ancestor == child {
				return
			}
		}
		parents[child] = parent
	}
	// Explicit parents claim first in name order, then interfaces claim their
	// implementors in label order with a name tie-break. Go compares UTF-8 bytes
	// and the rail UTF-16 code units; they agree except for non-BMP characters.
	for _, name := range names {
		setParent(name, nodes[name].parent)
	}
	sort.Slice(interfaces, func(i, j int) bool {
		if nodes[interfaces[i]].label == nodes[interfaces[j]].label {
			return interfaces[i] < interfaces[j]
		}
		return nodes[interfaces[i]].label < nodes[interfaces[j]].label
	})
	for _, name := range interfaces {
		for _, child := range visibleInterfaces[name] {
			setParent(child, name)
		}
	}
	for _, name := range names {
		node := nodes[name]
		if parents[name] != "" || node.embedded {
			continue
		}
		group := node.group
		if group == "" {
			group = "Other"
		}
		tree.Roots[group] = append(tree.Roots[group], name)
	}
	return tree
}

func displayLabel(name, label, plural string) string {
	if plural != "" {
		return plural
	}
	if label != "" {
		return label
	}
	return name
}
