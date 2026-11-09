package viewconfig

import (
	"fmt"
	"sort"

	ontoschema "github.com/atomicobject/rhizome/pkg/ontology/schema"
)

// PanelKind says which sidebar panel a target shows (SPEC-0136 R6).
type PanelKind string

const (
	// PanelView is an experience's panel view.
	PanelView PanelKind = "view"
	// PanelFolder is the built-in Folder panel.
	PanelFolder PanelKind = "folder"
	// PanelOwner tells the host to use the panel of the Note target for the
	// node's notePath (an embedded or section node), or Folder for a note.
	PanelOwner PanelKind = "owner"
	// PanelNone shows no panel.
	PanelNone PanelKind = "none"
)

// Panel is a target's panel descriptor, as the catalog sends it.
type Panel struct {
	Kind          PanelKind `json:"kind"`
	ViewID        string    `json:"viewId,omitempty"`
	ExperienceKey string    `json:"experienceKey,omitempty"`
}

// PanelResolver resolves the one panel for a target, most specific first:
// (1) the experience owning a standalone view; (2) an experience with a panel
// presenting the concrete type by mount or `types`; (3) one presenting an
// interface of it; (4) one presenting it through a group; (5) Folder for a
// subject backed by a note; (6) none. Within a level, views-root order, then
// folder path, wins.
type PanelResolver struct {
	schema      *ontoschema.Schema
	tree        NavigationTree
	listed      map[string]bool
	experiences []*presenting
	standalone  map[string]*Experience
}

// presenting is what one experience presents: type and interface names
// (from its views' mounts and its `types`) and groups (its `group` and its
// views' group mounts). Wildcard mounts present nothing in particular.
type presenting struct {
	exp    *Experience
	names  map[string]bool
	groups map[string]bool
}

// NewPanelResolver indexes the experiences of views, which must be valid:
// only views whose Experience is set count.
func NewPanelResolver(schema *ontoschema.Schema, views []ViewDefinition) *PanelResolver {
	r := &PanelResolver{schema: schema, tree: DisplayTree(schema), standalone: map[string]*Experience{}}
	r.listed = r.tree.Listed()
	byExp := map[*Experience]*presenting{}
	for _, view := range views {
		exp := view.Experience
		if exp == nil {
			continue
		}
		p := byExp[exp]
		if p == nil {
			p = &presenting{exp: exp, names: map[string]bool{}, groups: map[string]bool{}}
			for _, name := range exp.Types {
				p.names[name] = true
			}
			if exp.Group != "" {
				p.groups[exp.Group] = true
			}
			byExp[exp] = p
			r.experiences = append(r.experiences, p)
		}
		switch view.Mount.Kind {
		case MountKindStandalone:
			r.standalone[view.ID] = exp
		case MountKindType, MountKindNode:
			if view.Mount.Type != "*" {
				p.names[view.Mount.Type] = true
			}
		case MountKindInterface:
			if view.Mount.Interface != "*" {
				p.names[view.Mount.Interface] = true
			}
		case MountKindGroup:
			if view.Mount.Group != "*" {
				p.groups[view.Mount.Group] = true
			}
		}
	}
	sort.Slice(r.experiences, func(i, j int) bool { return experienceLess(r.experiences[i].exp, r.experiences[j].exp) })
	return r
}

// Resolve returns the panel for a target. A type collection's concrete type
// is the collection's type; an interface collection starts at level (3).
func (r *PanelResolver) Resolve(kind MountKind, name string) Panel {
	panel, _ := r.resolve(kind, name)
	return panel
}

// resolve also returns every experience with a panel that qualified at the
// deciding level, the winner first.
func (r *PanelResolver) resolve(kind MountKind, name string) (Panel, []*Experience) {
	switch kind {
	case MountKindStandalone:
		return panelOf(r.standalone[name]), nil
	case MountKindType, MountKindNode:
		levels := []func(*presenting) bool{
			func(p *presenting) bool { return p.names[name] },
			func(p *presenting) bool { return r.presentsInterfaceOf(p, name) },
			func(p *presenting) bool { return r.presentsThroughGroup(p, name) },
		}
		if panel, ties, ok := r.firstLevel(levels); ok {
			return panel, ties
		}
		if kind == MountKindType {
			return Panel{Kind: PanelNone}, nil
		}
		if !r.listed[name] {
			return Panel{Kind: PanelOwner}, nil
		}
		return Panel{Kind: PanelFolder}, nil
	case MountKindInterface:
		levels := []func(*presenting) bool{
			func(p *presenting) bool { return p.names[name] },
			func(p *presenting) bool { return r.presentsThroughGroup(p, name) },
		}
		panel, ties, _ := r.firstLevel(levels)
		return panel, ties
	case MountKindGroup:
		panel, ties, _ := r.firstLevel([]func(*presenting) bool{func(p *presenting) bool { return p.groups[name] }})
		return panel, ties
	}
	return Panel{Kind: PanelNone}, nil
}

// firstLevel returns the panel of the first level any experience with a
// panel qualifies at, with every experience that qualified there.
func (r *PanelResolver) firstLevel(levels []func(*presenting) bool) (Panel, []*Experience, bool) {
	for _, qualifies := range levels {
		var found []*Experience
		for _, p := range r.experiences {
			if p.exp.Panel != "" && qualifies(p) {
				found = append(found, p.exp)
			}
		}
		if len(found) > 0 {
			return panelOf(found[0]), found, true
		}
	}
	return Panel{Kind: PanelNone}, nil, false
}

func (r *PanelResolver) presentsInterfaceOf(p *presenting, typeName string) bool {
	if r.schema == nil || r.schema.Types[typeName] == nil {
		return false
	}
	for _, iface := range r.schema.Types[typeName].Implements {
		if p.names[iface] {
			return true
		}
	}
	return false
}

func (r *PanelResolver) presentsThroughGroup(p *presenting, name string) bool {
	for group := range p.groups {
		for _, member := range r.tree.Members(group) {
			if member == name {
				return true
			}
		}
	}
	return false
}

// panelOf is an experience's panel, or none for an experience without one
// (or no experience).
func panelOf(exp *Experience) Panel {
	if exp == nil || exp.Panel == "" {
		return Panel{Kind: PanelNone}
	}
	return Panel{Kind: PanelView, ViewID: exp.Panel, ExperienceKey: exp.Key()}
}

// tieIssues warns once for each pair of experiences with panels that tie at
// the level that decides some type, interface, or group.
func (r *PanelResolver) tieIssues() []Issue {
	if r.schema == nil {
		return nil
	}
	type target struct {
		kind MountKind
		name string
	}
	var targets []target
	for name := range r.schema.Types {
		targets = append(targets, target{MountKindType, name})
	}
	for name := range r.schema.Interfaces {
		targets = append(targets, target{MountKindInterface, name})
	}
	for name := range r.tree.Roots {
		targets = append(targets, target{MountKindGroup, name})
	}
	sort.Slice(targets, func(i, j int) bool {
		if targets[i].kind != targets[j].kind {
			return targets[i].kind < targets[j].kind
		}
		return targets[i].name < targets[j].name
	})
	seen := map[[2]*Experience]bool{}
	var out []Issue
	for _, t := range targets {
		_, contenders := r.resolve(t.kind, t.name)
		if len(contenders) < 2 {
			continue
		}
		winner := contenders[0]
		for _, loser := range contenders[1:] {
			if seen[[2]*Experience{winner, loser}] {
				continue
			}
			seen[[2]*Experience{winner, loser}] = true
			message := fmt.Sprintf("the experiences at %s and %s both declare a panel and present %s %q at the same level; %s's panel wins by views-root order, then folder path", winner.Path, loser.Path, t.kind, t.name, winner.Path)
			out = append(out,
				experienceIssue(winner, "experience_panel_tie", "panel", message, IssueWarning),
				experienceIssue(loser, "experience_panel_tie", "panel", message, IssueWarning))
		}
	}
	return out
}
