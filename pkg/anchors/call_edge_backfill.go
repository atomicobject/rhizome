package codeanchor

import (
	"sort"
	"strings"

	"github.com/atomicobject/rhizome/pkg/paths"
)

// DurableRefFootprint is the minimal per-file reverse-index footprint needed
// to decide whether a newly durable caller is ready for call-edge rebuild.
type DurableRefFootprint struct {
	Path       string
	SymbolKeys []string
	Modules    []string
}

func (f DurableRefFootprint) Empty() bool {
	return strings.TrimSpace(f.Path) == "" || (len(f.SymbolKeys) == 0 && len(f.Modules) == 0)
}

// BuildDurableRefFootprint projects stored ref rows into the compact keys used
// by the incremental call-edge ready queue.
func BuildDurableRefFootprint(srcPath string, symbolRefs []SymbolRefRow, importRefs []ImportRefRow) DurableRefFootprint {
	srcPath = string(paths.NormalizeCode(srcPath))
	if srcPath == "" {
		return DurableRefFootprint{}
	}
	symbolSet := make(map[string]struct{}, len(symbolRefs))
	for _, row := range symbolRefs {
		key := symbolRefKey(SymbolRef{
			Lang:   row.DstLang,
			Pkg:    row.DstPkg,
			Name:   row.DstName,
			Member: row.DstMember,
		})
		if key != "" {
			symbolSet[key] = struct{}{}
		}
	}
	moduleSet := make(map[string]struct{}, len(importRefs))
	for _, row := range importRefs {
		module := strings.TrimSpace(row.Module)
		if module != "" {
			moduleSet[module] = struct{}{}
		}
	}
	footprint := DurableRefFootprint{Path: srcPath}
	if len(symbolSet) > 0 {
		footprint.SymbolKeys = make([]string, 0, len(symbolSet))
		for key := range symbolSet {
			footprint.SymbolKeys = append(footprint.SymbolKeys, key)
		}
		sort.Strings(footprint.SymbolKeys)
	}
	if len(moduleSet) > 0 {
		footprint.Modules = make([]string, 0, len(moduleSet))
		for module := range moduleSet {
			footprint.Modules = append(footprint.Modules, module)
		}
		sort.Strings(footprint.Modules)
	}
	return footprint
}

func (r CallEdgeResidual) SymbolKeys() []string {
	if len(r.SymbolRefs) == 0 {
		return nil
	}
	keys := make(map[string]struct{}, len(r.SymbolRefs))
	for _, ref := range r.SymbolRefs {
		if key := symbolRefKey(ref); key != "" {
			keys[key] = struct{}{}
		}
	}
	out := make([]string, 0, len(keys))
	for key := range keys {
		out = append(out, key)
	}
	sort.Strings(out)
	return out
}

func (r CallEdgeResidual) ModuleKeys() []string {
	return uniqueStrings(append([]string(nil), r.Modules...))
}

func (r CallEdgeResidual) FallbackFiltersBySymbolKey() map[string][]func(string) bool {
	if len(r.Fallbacks) == 0 {
		return nil
	}
	out := make(map[string][]func(string) bool, len(r.Fallbacks))
	for _, fb := range r.Fallbacks {
		key := symbolRefKey(fb.Ref)
		if key == "" {
			continue
		}
		out[key] = append(out[key], fb.PathFilter)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}
