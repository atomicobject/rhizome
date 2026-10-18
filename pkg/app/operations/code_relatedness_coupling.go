package operations

import (
	"context"
	"sort"

	"github.com/atomicobject/rhizome/pkg/anchors"
)

func buildCouplingEdges(
	ctx context.Context,
	intel CodeRelatednessIntel,
	docStore interface {
		DocStatsForFQNs(ctx context.Context, lang string, fqns []string) (map[string]codeanchor.DocStats, error)
	},
	importEdgeFiles map[crossCallKey]map[string]int,
	importEdgeFileSets map[crossCallKey]map[string]struct{},
	anchorByID map[int64]codeanchor.Anchor,
	noteCache map[int64]map[string]struct{},
	crossPkgCalls map[crossCallKey]int,
	edgeCalleeCounts map[crossCallKey]map[string]int,
	edgeCallerFileCounts map[crossCallKey]map[string]int,
	pkgs []discoveredPkg,
	vaultPath string,
	limit int,
	topCallees int,
	topFiles int,
) []CodeRelatednessCouplingEdge {
	if limit <= 0 {
		limit = 25
	}
	if len(crossPkgCalls) == 0 || len(pkgs) == 0 {
		return nil
	}
	if topCallees <= 0 {
		topCallees = 5
	}
	if topFiles <= 0 {
		topFiles = 5
	}

	imports := make(map[crossCallKey]bool)
	for _, p := range pkgs {
		for to := range p.ImportsTo {
			imports[crossCallKey{callerPkg: p.ImportPath, calleePkg: to}] = true
		}
	}

	type item struct {
		k crossCallKey
		n int
	}
	items := make([]item, 0, len(crossPkgCalls))
	for k, n := range crossPkgCalls {
		if k.callerPkg == "" || k.calleePkg == "" || k.callerPkg == k.calleePkg {
			continue
		}
		items = append(items, item{k: k, n: n})
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].n != items[j].n {
			return items[i].n > items[j].n
		}
		if items[i].k.callerPkg != items[j].k.callerPkg {
			return items[i].k.callerPkg < items[j].k.callerPkg
		}
		return items[i].k.calleePkg < items[j].k.calleePkg
	})
	if len(items) > limit {
		items = items[:limit]
	}

	out := make([]CodeRelatednessCouplingEdge, 0, len(items))
	type symbolMetaStore interface {
		SymbolMetaForFQNs(ctx context.Context, lang string, fqns []string) (map[string]codeanchor.SymbolMeta, error)
	}
	var symStore symbolMetaStore
	if intel != nil {
		if ss, ok := intel.(symbolMetaStore); ok {
			symStore = ss
		}
	}
	symCache := make(map[string]codeanchor.SymbolMeta)
	for _, it := range items {
		rev := crossCallKey{callerPkg: it.k.calleePkg, calleePkg: it.k.callerPkg}

		topCallee := topStringInt(edgeCalleeCounts[it.k], topCallees)
		topCallerFiles := topStringInt(edgeCallerFileCounts[it.k], topFiles)

		var calleeFQNs []string
		for _, c := range topCallee {
			calleeFQNs = append(calleeFQNs, c.key)
		}

		anchorLabelsByFQN := make(map[string][]string)
		if intel != nil && len(calleeFQNs) > 0 {
			for _, fqn := range calleeFQNs {
				matched, err := intel.AnchorsBySymbols(ctx, []string{fqn})
				if err != nil {
					continue
				}
				labels := make(map[string]struct{})
				for id := range matched {
					if a, ok := anchorByID[id]; ok && a.Label != "" {
						labels[a.Label] = struct{}{}
					}
					if _, ok := noteCache[id]; !ok {
						paths := make(map[string]struct{})
						if fetched, err := intel.NotesForAnchor(ctx, id); err == nil {
							for _, n := range fetched {
								if n.Path != "" {
									paths[n.Path] = struct{}{}
								}
							}
						}
						noteCache[id] = paths
					}
				}
				anchorLabelsByFQN[fqn] = sortedKeys(labels)
			}
		}

		docByFQN := map[string]codeanchor.DocStats{}
		if docStore != nil && len(calleeFQNs) > 0 {
			// Query across all supported languages since we don't track language per FQN.
			for lang := range langByExt {
				if m, err := docStore.DocStatsForFQNs(ctx, string(lang), calleeFQNs); err == nil {
					for k, v := range m {
						docByFQN[k] = v
					}
				}
			}
		}

		metaByFQN := make(map[string]codeanchor.SymbolMeta)
		if symStore != nil && len(calleeFQNs) > 0 {
			var missing []string
			for _, fqn := range calleeFQNs {
				if m, ok := symCache[fqn]; ok {
					metaByFQN[fqn] = m
					continue
				}
				missing = append(missing, fqn)
			}
			if len(missing) > 0 {
				// Query across all supported languages since we don't track language per FQN.
				for lang := range langByExt {
					if m, err := symStore.SymbolMetaForFQNs(ctx, string(lang), missing); err == nil {
						for fqn, meta := range m {
							symCache[fqn] = meta
							metaByFQN[fqn] = meta
						}
					}
				}
			}
		}

		var topCalleesOut []CodeRelatednessCouplingCallee
		for _, c := range topCallee {
			ds := docByFQN[c.key]
			meta := metaByFQN[c.key]
			topCalleesOut = append(topCalleesOut, CodeRelatednessCouplingCallee{
				FQN:      c.key,
				Calls:    c.val,
				Kind:     string(meta.Kind),
				File:     relToRootSlash(vaultPath, meta.File),
				Exported: meta.Exported,
				Anchors:  anchorLabelsByFQN[c.key],
				Mentions: ds.Mentions,
				Links:    ds.Links,
			})
		}

		var topCallerFilesOut []CodeRelatednessCouplingCallerFile
		for _, f := range topCallerFiles {
			topCallerFilesOut = append(topCallerFilesOut, CodeRelatednessCouplingCallerFile{
				File:  relToRootSlash(vaultPath, f.key),
				Calls: f.val,
			})
		}

		importFilesCount := 0
		if importEdgeFileSets[it.k] != nil {
			importFilesCount = len(importEdgeFileSets[it.k])
		}
		importStmts := 0
		if importEdgeFiles[it.k] != nil {
			for _, n := range importEdgeFiles[it.k] {
				importStmts += n
			}
		}
		topImportFiles := topStringInt(importEdgeFiles[it.k], topFiles)
		var topImportFilesOut []CodeRelatednessCouplingCallerFile
		for _, f := range topImportFiles {
			topImportFilesOut = append(topImportFilesOut, CodeRelatednessCouplingCallerFile{
				File:  relToRootSlash(vaultPath, f.key),
				Calls: f.val,
			})
		}

		out = append(out, CodeRelatednessCouplingEdge{
			FromPkg:        it.k.callerPkg,
			ToPkg:          it.k.calleePkg,
			CallsTo:        it.n,
			Imports:        imports[it.k],
			BothWays:       crossPkgCalls[rev] > 0,
			ImportFiles:    importFilesCount,
			ImportStmts:    importStmts,
			TopImportFiles: topImportFilesOut,
			TopCallees:     topCalleesOut,
			TopCallerFiles: topCallerFilesOut,
		})
	}
	return out
}

type stringInt struct {
	key string
	val int
}

func topStringInt(m map[string]int, n int) []stringInt {
	if n <= 0 || len(m) == 0 {
		return nil
	}
	out := make([]stringInt, 0, len(m))
	for k, v := range m {
		out = append(out, stringInt{key: k, val: v})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].val != out[j].val {
			return out[i].val > out[j].val
		}
		return out[i].key < out[j].key
	})
	if len(out) > n {
		out = out[:n]
	}
	return out
}
