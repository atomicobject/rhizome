package indexing

import (
	"sort"
	"strings"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
)

type callEdgeMembershipIndex struct {
	pathFootprints map[string]codeanchor.DurableRefFootprint
	symbolPaths    map[string]map[string]struct{}
	modulePaths    map[string]map[string]struct{}
}

func newCallEdgeMembershipIndex() callEdgeMembershipIndex {
	return callEdgeMembershipIndex{
		pathFootprints: make(map[string]codeanchor.DurableRefFootprint),
		symbolPaths:    make(map[string]map[string]struct{}),
		modulePaths:    make(map[string]map[string]struct{}),
	}
}

func (m *callEdgeMembershipIndex) upsertFootprints(footprints []codeanchor.DurableRefFootprint) {
	if m == nil {
		return
	}
	for _, footprint := range mergeDurableRefFootprints(footprints) {
		path := normalizeMembershipPath(footprint.Path)
		if path == "" {
			continue
		}
		if prev, ok := m.pathFootprints[path]; ok {
			m.removeFootprint(prev)
		}
		footprint.Path = path
		m.pathFootprints[path] = footprint
		for _, key := range footprint.SymbolKeys {
			key = normalizeMembershipPath(key)
			if key == "" {
				continue
			}
			if m.symbolPaths[key] == nil {
				m.symbolPaths[key] = make(map[string]struct{})
			}
			m.symbolPaths[key][path] = struct{}{}
		}
		for _, module := range footprint.Modules {
			module = normalizeMembershipPath(module)
			if module == "" {
				continue
			}
			if m.modulePaths[module] == nil {
				m.modulePaths[module] = make(map[string]struct{})
			}
			m.modulePaths[module][path] = struct{}{}
		}
	}
}

func (m *callEdgeMembershipIndex) resolveResidual(residual codeanchor.CallEdgeResidual, progress interface {
	Start(total int)
	Advance(delta int)
}) []string {
	if m == nil || !residual.HasPending() {
		return nil
	}
	symbolKeys := residual.SymbolKeys()
	moduleKeys := residual.ModuleKeys()
	fallbacks := residual.FallbackFiltersBySymbolKey()
	total := len(symbolKeys) + len(moduleKeys)
	if progress != nil && total > 0 {
		progress.Start(total)
	}
	paths := make(map[string]struct{})
	for _, key := range symbolKeys {
		for path := range m.symbolPaths[key] {
			filters := fallbacks[key]
			if !fallbackPathAllowed(path, filters) {
				continue
			}
			paths[path] = struct{}{}
		}
		if progress != nil {
			progress.Advance(1)
		}
	}
	for _, module := range moduleKeys {
		for path := range m.modulePaths[module] {
			paths[path] = struct{}{}
		}
		if progress != nil {
			progress.Advance(1)
		}
	}
	if len(paths) == 0 {
		return nil
	}
	out := make([]string, 0, len(paths))
	for path := range paths {
		out = append(out, path)
	}
	sort.Strings(out)
	return out
}

func (m *callEdgeMembershipIndex) removeFootprint(footprint codeanchor.DurableRefFootprint) {
	path := normalizeMembershipPath(footprint.Path)
	if path == "" {
		return
	}
	delete(m.pathFootprints, path)
	for _, key := range footprint.SymbolKeys {
		key = normalizeMembershipPath(key)
		if key == "" {
			continue
		}
		members := m.symbolPaths[key]
		delete(members, path)
		if len(members) == 0 {
			delete(m.symbolPaths, key)
		}
	}
	for _, module := range footprint.Modules {
		module = normalizeMembershipPath(module)
		if module == "" {
			continue
		}
		members := m.modulePaths[module]
		delete(members, path)
		if len(members) == 0 {
			delete(m.modulePaths, module)
		}
	}
}

func normalizeMembershipPath(value string) string {
	return strings.TrimSpace(strings.ReplaceAll(value, "\\", "/"))
}
