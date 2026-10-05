package codeanchor

import "sort"

// BuildScopeReadyFootprint extracts the dependency keys that can make anchors
// ready for exact scope recomputation once a code file's intel rows are durable.
func BuildScopeReadyFootprint(path string, summary FileSummary) ScopeReadyFootprint {
	out := ScopeReadyFootprint{Path: path}
	symbols := make(map[string]struct{}, len(summary.Symbols)+len(summary.Calls))
	annotations := make(map[string]struct{}, len(summary.Annotations))

	for _, sym := range summary.Symbols {
		key := symbolRefKey(SymbolRef{
			Lang: sym.Lang,
			Pkg:  sym.Pkg,
			Name: sym.Name,
		})
		if key != "" {
			symbols[key] = struct{}{}
		}
	}
	for _, call := range summary.Calls {
		key := symbolRefKey(call.CalleeSymbol)
		if key != "" {
			symbols[key] = struct{}{}
		}
	}
	for _, ann := range summary.Annotations {
		key := symbolRefKey(ann.AnnSymbol)
		if key != "" {
			annotations[key] = struct{}{}
		}
	}

	if len(symbols) > 0 {
		out.SymbolKeys = make([]string, 0, len(symbols))
		for key := range symbols {
			out.SymbolKeys = append(out.SymbolKeys, key)
		}
		sort.Strings(out.SymbolKeys)
	}
	if len(annotations) > 0 {
		out.AnnotationKeys = make([]string, 0, len(annotations))
		for key := range annotations {
			out.AnnotationKeys = append(out.AnnotationKeys, key)
		}
		sort.Strings(out.AnnotationKeys)
	}
	return out
}
