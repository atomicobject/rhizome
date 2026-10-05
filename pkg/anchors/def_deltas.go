package codeanchor

import (
	"sort"
	"strings"
)

// DefDeltas captures added/removed definitions for an indexing run.
type DefDeltas struct {
	AddedSymbols   []SymbolRef
	RemovedSymbols []SymbolRef
	AddedModules   []string
	RemovedModules []string
}

// DefDeltaAccumulator accumulates definition deltas in map form so hot paths
// can defer sorting/dedup until finalization.
type DefDeltaAccumulator struct {
	addedSymbols   map[string]SymbolRef
	removedSymbols map[string]SymbolRef
	addedModules   map[string]struct{}
	removedModules map[string]struct{}
}

// HasChanges reports whether any deltas are present.
func (d DefDeltas) HasChanges() bool {
	return len(d.AddedSymbols) > 0 || len(d.RemovedSymbols) > 0 || len(d.AddedModules) > 0 || len(d.RemovedModules) > 0
}

// Add merges one delta set into the accumulator.
func (a *DefDeltaAccumulator) Add(delta DefDeltas) {
	if a == nil || !delta.HasChanges() {
		return
	}
	a.ensureMaps()
	for _, ref := range delta.AddedSymbols {
		a.addedSymbols[symbolRefKey(ref)] = ref
	}
	for _, ref := range delta.RemovedSymbols {
		a.removedSymbols[symbolRefKey(ref)] = ref
	}
	for _, mod := range delta.AddedModules {
		if mod == "" {
			continue
		}
		a.addedModules[mod] = struct{}{}
	}
	for _, mod := range delta.RemovedModules {
		if mod == "" {
			continue
		}
		a.removedModules[mod] = struct{}{}
	}
}

// HasChanges reports whether any deltas have been accumulated.
func (a *DefDeltaAccumulator) HasChanges() bool {
	return a != nil &&
		(len(a.addedSymbols) > 0 ||
			len(a.removedSymbols) > 0 ||
			len(a.addedModules) > 0 ||
			len(a.removedModules) > 0)
}

// Finalize returns the stable sorted delta form.
func (a *DefDeltaAccumulator) Finalize() DefDeltas {
	if a == nil || !a.HasChanges() {
		return DefDeltas{}
	}
	out := DefDeltas{
		AddedSymbols:   make([]SymbolRef, 0, len(a.addedSymbols)),
		RemovedSymbols: make([]SymbolRef, 0, len(a.removedSymbols)),
		AddedModules:   make([]string, 0, len(a.addedModules)),
		RemovedModules: make([]string, 0, len(a.removedModules)),
	}
	for _, ref := range a.addedSymbols {
		out.AddedSymbols = append(out.AddedSymbols, ref)
	}
	for _, ref := range a.removedSymbols {
		out.RemovedSymbols = append(out.RemovedSymbols, ref)
	}
	for mod := range a.addedModules {
		out.AddedModules = append(out.AddedModules, mod)
	}
	for mod := range a.removedModules {
		out.RemovedModules = append(out.RemovedModules, mod)
	}
	sort.Slice(out.AddedSymbols, func(i, j int) bool {
		return symbolRefKey(out.AddedSymbols[i]) < symbolRefKey(out.AddedSymbols[j])
	})
	sort.Slice(out.RemovedSymbols, func(i, j int) bool {
		return symbolRefKey(out.RemovedSymbols[i]) < symbolRefKey(out.RemovedSymbols[j])
	})
	sort.Strings(out.AddedModules)
	sort.Strings(out.RemovedModules)
	return out
}

func (a *DefDeltaAccumulator) ensureMaps() {
	if a.addedSymbols == nil {
		a.addedSymbols = make(map[string]SymbolRef)
	}
	if a.removedSymbols == nil {
		a.removedSymbols = make(map[string]SymbolRef)
	}
	if a.addedModules == nil {
		a.addedModules = make(map[string]struct{})
	}
	if a.removedModules == nil {
		a.removedModules = make(map[string]struct{})
	}
}

// SymbolRefsFromSymbols extracts unique SymbolRefs from symbol definitions.
func SymbolRefsFromSymbols(symbols []Symbol) []SymbolRef {
	seen := make(map[string]SymbolRef)
	for _, sym := range symbols {
		if sym.Name == "" || sym.Lang == "" {
			continue
		}
		ref := SymbolRef{Lang: sym.Lang, Pkg: sym.Pkg, Name: sym.Name, Member: sym.Lang == LangPhp && (sym.Kind == SymMethod || sym.Kind == SymField)}
		key := symbolRefKey(ref)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = ref
	}
	refs := make([]SymbolRef, 0, len(seen))
	for _, ref := range seen {
		refs = append(refs, ref)
	}
	sort.Slice(refs, func(i, j int) bool {
		if refs[i].Lang != refs[j].Lang {
			return refs[i].Lang < refs[j].Lang
		}
		if refs[i].Pkg != refs[j].Pkg {
			return refs[i].Pkg < refs[j].Pkg
		}
		return refs[i].Name < refs[j].Name
	})
	return refs
}

// SymbolRefsFromFQNs converts stored symbol FQNs into SymbolRefs with a known language.
func SymbolRefsFromFQNs(fqns []string, lang Lang) []SymbolRef {
	if len(fqns) == 0 || lang == "" {
		return nil
	}
	seen := make(map[string]SymbolRef)
	for _, fqn := range fqns {
		pkg, name, member := splitDefinitionFQN(fqn, lang)
		if name == "" {
			continue
		}
		ref := SymbolRef{Lang: lang, Pkg: pkg, Name: name, Member: member}
		key := symbolRefKey(ref)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = ref
	}
	if len(seen) == 0 {
		return nil
	}
	refs := make([]SymbolRef, 0, len(seen))
	for _, ref := range seen {
		refs = append(refs, ref)
	}
	sort.Slice(refs, func(i, j int) bool {
		if refs[i].Lang != refs[j].Lang {
			return refs[i].Lang < refs[j].Lang
		}
		if refs[i].Pkg != refs[j].Pkg {
			return refs[i].Pkg < refs[j].Pkg
		}
		return refs[i].Name < refs[j].Name
	})
	return refs
}

func splitDefinitionFQN(fqn string, lang Lang) (pkg, name string, member bool) {
	fqn = strings.TrimSpace(fqn)
	if lang == LangPhp {
		if sep := strings.LastIndex(fqn, "::"); sep >= 0 {
			return fqn[:sep], fqn[sep+2:], true
		}
		if sep := strings.LastIndex(fqn, `\`); sep >= 0 {
			return fqn[:sep], fqn[sep+1:], false
		}
	}
	pkg, name = splitFQN(fqn)
	return pkg, name, false
}

// ModulesFromDefs extracts module strings from module-def rows.
func ModulesFromDefs(defs []ModuleDefRow) []string {
	seen := make(map[string]struct{})
	for _, def := range defs {
		if def.Module == "" {
			continue
		}
		seen[def.Module] = struct{}{}
	}
	out := make([]string, 0, len(seen))
	for mod := range seen {
		out = append(out, mod)
	}
	sort.Strings(out)
	return out
}

// DiffSymbolRefs computes added/removed refs between two sets.
func DiffSymbolRefs(oldRefs, newRefs []SymbolRef) (added []SymbolRef, removed []SymbolRef) {
	oldSet := make(map[string]SymbolRef, len(oldRefs))
	for _, ref := range oldRefs {
		oldSet[symbolRefKey(ref)] = ref
	}
	newSet := make(map[string]SymbolRef, len(newRefs))
	for _, ref := range newRefs {
		newSet[symbolRefKey(ref)] = ref
	}
	for key, ref := range newSet {
		if _, ok := oldSet[key]; !ok {
			added = append(added, ref)
		}
	}
	for key, ref := range oldSet {
		if _, ok := newSet[key]; !ok {
			removed = append(removed, ref)
		}
	}
	sort.Slice(added, func(i, j int) bool {
		return symbolRefKey(added[i]) < symbolRefKey(added[j])
	})
	sort.Slice(removed, func(i, j int) bool {
		return symbolRefKey(removed[i]) < symbolRefKey(removed[j])
	})
	return added, removed
}

// DiffModules computes added/removed modules between two sets.
func DiffModules(oldMods, newMods []string) (added []string, removed []string) {
	oldSet := make(map[string]struct{}, len(oldMods))
	for _, mod := range oldMods {
		if mod == "" {
			continue
		}
		oldSet[mod] = struct{}{}
	}
	newSet := make(map[string]struct{}, len(newMods))
	for _, mod := range newMods {
		if mod == "" {
			continue
		}
		newSet[mod] = struct{}{}
	}
	for mod := range newSet {
		if _, ok := oldSet[mod]; !ok {
			added = append(added, mod)
		}
	}
	for mod := range oldSet {
		if _, ok := newSet[mod]; !ok {
			removed = append(removed, mod)
		}
	}
	sort.Strings(added)
	sort.Strings(removed)
	return added, removed
}
