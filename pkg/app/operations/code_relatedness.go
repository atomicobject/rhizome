package operations

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/atomicobject/rhizome/pkg/anchors"
	"github.com/atomicobject/rhizome/pkg/paths"
	"github.com/atomicobject/rhizome/pkg/vault/ignore"
)

func CodeRelatedness(ctx context.Context, intel CodeRelatednessIntel, opts CodeRelatednessOptions) (CodeRelatednessReport, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if opts.VaultPath == "" {
		return CodeRelatednessReport{}, fmt.Errorf("vault path is required")
	}
	if len(opts.Roots) == 0 {
		return CodeRelatednessReport{}, fmt.Errorf("at least one root is required")
	}
	if opts.MaxRelated <= 0 {
		opts.MaxRelated = 8
	}
	if opts.ClusterThreshold <= 0 {
		opts.ClusterThreshold = 3
	}
	if opts.CriticalLimit <= 0 {
		opts.CriticalLimit = 25
	}
	if opts.HotFilesLimit <= 0 {
		opts.HotFilesLimit = 6
	}

	modulePath := readGoModulePath(opts.VaultPath)
	vaultPaths, _ := paths.NewVaultPaths(opts.VaultPath)

	ignoreMatcher := ignore.LoadUnifiedMatcher(opts.VaultPath, opts.Excludes)
	pkgs, _, err := discoverPackages(ctx, opts.VaultPath, opts.Roots, ignoreMatcher, opts.IncludeTests, intel)
	if err != nil {
		return CodeRelatednessReport{}, err
	}
	if len(pkgs) == 0 {
		return CodeRelatednessReport{
			VaultPath: opts.VaultPath,
			Roots:     opts.Roots,
			Module:    modulePath,
		}, nil
	}

	// Map canonical package names and root-stripped aliases.
	aliasToCanonical := make(map[string]string, len(pkgs)*2)
	for _, p := range pkgs {
		aliasToCanonical[p.Canonical] = p.Canonical
		if trimmed := stripCommonRoots(p.Canonical); trimmed != "" && trimmed != p.Canonical {
			aliasToCanonical[trimmed] = p.Canonical
		}
	}
	canonicalToPkg := make(map[string]string, len(pkgs))
	for _, p := range pkgs {
		if _, ok := canonicalToPkg[p.Canonical]; !ok {
			canonicalToPkg[p.Canonical] = p.ImportPath
		}
	}

	// Import edges are derived from calls (language-agnostic).
	// We populate these in the calls loop below.
	importEdgeFiles := make(map[crossCallKey]map[string]int) // callerPkg->calleePkg -> file -> count
	importEdgeFileSets := make(map[crossCallKey]map[string]struct{})

	// Calls + anchors from intel store (optional).
	var anchorByID map[int64]codeanchor.Anchor
	type docStatsStore interface {
		DocStatsForFQNs(ctx context.Context, lang string, fqns []string) (map[string]codeanchor.DocStats, error)
	}
	var docStore docStatsStore
	if intel != nil {
		anchors, err := intel.Anchors(ctx)
		if err == nil {
			anchorByID = make(map[int64]codeanchor.Anchor, len(anchors))
			for _, a := range anchors {
				anchorByID[a.ID] = a
			}
		}
		if ds, ok := intel.(docStatsStore); ok {
			docStore = ds
		}
	}

	ancCache := make(map[string][]string)
	noteCache := make(map[int64]map[string]struct{})
	fileToPkg := make(map[string]string)
	for _, p := range pkgs {
		for _, f := range p.FilesAbs {
			fileToPkg[f] = p.ImportPath
		}
	}

	crossPkgCalls := make(map[crossCallKey]int)
	crossPkgSymbols := make(map[symKey]*symAgg)
	fileHot := make(map[string]map[string]*fileHotspotAgg) // callerPkg canonical -> file -> agg
	edgeCalleeCounts := make(map[crossCallKey]map[string]int)
	edgeCallerFileCounts := make(map[crossCallKey]map[string]int)
	packageLangs := make(map[string]map[string]struct{})

	for i := range pkgs {
		pkgs[i].CallsTo = make(map[string]struct{})
		pkgs[i].ImportsTo = make(map[string]struct{})
		pkgs[i].AnchorIDs = make(map[int64]struct{})
		pkgs[i].AnchorLabelsSet = make(map[string]struct{})
		pkgs[i].NotePathsSet = make(map[string]struct{})

		if intel == nil {
			continue
		}
		for _, f := range pkgs[i].FilesAbs {
			if lang := langForPath(f); lang != "" {
				if packageLangs[pkgs[i].ImportPath] == nil {
					packageLangs[pkgs[i].ImportPath] = make(map[string]struct{})
				}
				packageLangs[pkgs[i].ImportPath][string(lang)] = struct{}{}
			}
			// Calls
			callPath := f
			if vaultPaths.Root() != "" {
				if rel, err := vaultPaths.RelCodeStrict(f); err == nil && rel.String() != "" {
					callPath = rel.String()
				}
			}
			calls, err := intel.CallsFromFile(ctx, callPath)
			if err == nil {
				for _, c := range calls {
					if c.CalleeSymbol.Pkg == "" {
						continue
					}
					calleePkgCanon := normalizePkgName(c.CalleeSymbol.Pkg)
					resolvedCallee, ok := matchInternalPkg(calleePkgCanon, aliasToCanonical)
					if ok {
						pkgs[i].CallsTo[resolvedCallee] = struct{}{}
						// Derive import edges from calls (language-agnostic).
						pkgs[i].ImportsTo[resolvedCallee] = struct{}{}

						callerPkg := normalizePkgName(fileToPkg[f])
						calleePkg := resolvedCallee
						if callerPkg != "" && calleePkg != "" && callerPkg != calleePkg {
							if packageLangs[pkgs[i].ImportPath] == nil {
								packageLangs[pkgs[i].ImportPath] = make(map[string]struct{})
							}
							if c.CalleeSymbol.Lang != "" {
								packageLangs[pkgs[i].ImportPath][string(c.CalleeSymbol.Lang)] = struct{}{}
							}
							ek := crossCallKey{callerPkg: callerPkg, calleePkg: calleePkg}
							crossPkgCalls[ek]++

							// Track import edges per file (derived from calls).
							if importEdgeFiles[ek] == nil {
								importEdgeFiles[ek] = make(map[string]int)
							}
							importEdgeFiles[ek][f]++
							if importEdgeFileSets[ek] == nil {
								importEdgeFileSets[ek] = make(map[string]struct{})
							}
							importEdgeFileSets[ek][f] = struct{}{}

							if c.CalleeSymbol.Name != "" {
								fqn := calleePkg + "." + c.CalleeSymbol.Name
								if edgeCalleeCounts[ek] == nil {
									edgeCalleeCounts[ek] = make(map[string]int)
								}
								edgeCalleeCounts[ek][fqn]++
							}
							if edgeCallerFileCounts[ek] == nil {
								edgeCallerFileCounts[ek] = make(map[string]int)
							}
							edgeCallerFileCounts[ek][f]++

							// Track cross-package symbols. All cross-package calls are
							// considered "exported" since they're reachable from another package.
							key := symKey{lang: c.CalleeSymbol.Lang, pkg: calleePkg, name: c.CalleeSymbol.Name}
							agg := crossPkgSymbols[key]
							if agg == nil {
								agg = &symAgg{
									callerPkgs:  make(map[string]struct{}),
									callerFiles: make(map[string]struct{}),
									callsByPkg:  make(map[string]int),
									callsByFile: make(map[string]int),
								}
								crossPkgSymbols[key] = agg
							}
							agg.calls++
							agg.callerPkgs[callerPkg] = struct{}{}
							agg.callerFiles[f] = struct{}{}
							agg.callsByPkg[callerPkg]++
							agg.callsByFile[f]++

							if fileHot[callerPkg] == nil {
								fileHot[callerPkg] = make(map[string]*fileHotspotAgg)
							}
							if fileHot[callerPkg][f] == nil {
								fileHot[callerPkg][f] = &fileHotspotAgg{calleePkgs: make(map[string]struct{})}
							}
							fileHot[callerPkg][f].calls++
							fileHot[callerPkg][f].calleePkgs[calleePkg] = struct{}{}
						}
					}
				}
			}

			// Anchors
			ids := matchingAnchorIDsForFile(ctx, intel, opts.VaultPath, f, ancCache)
			for id := range ids {
				pkgs[i].AnchorIDs[id] = struct{}{}
				if a, ok := anchorByID[id]; ok && a.Label != "" {
					pkgs[i].AnchorLabelsSet[a.Label] = struct{}{}
				}
			}
		}
	}

	// Notes per anchor ID (best-effort).
	if intel != nil {
		for i := range pkgs {
			for anchorID := range pkgs[i].AnchorIDs {
				paths, ok := noteCache[anchorID]
				if !ok {
					paths = make(map[string]struct{})
					notes, err := intel.NotesForAnchor(ctx, anchorID)
					if err == nil {
						for _, n := range notes {
							if n.Path != "" {
								paths[n.Path] = struct{}{}
							}
						}
					}
					noteCache[anchorID] = paths
				}
				for p := range paths {
					pkgs[i].NotePathsSet[p] = struct{}{}
				}
			}
		}
	}

	// Reverse edges.
	importedBy := make(map[string]map[string]struct{}, len(pkgs))
	calledBy := make(map[string]map[string]struct{}, len(pkgs))
	for _, p := range pkgs {
		for imp := range p.ImportsTo {
			if importedBy[imp] == nil {
				importedBy[imp] = make(map[string]struct{})
			}
			importedBy[imp][p.Canonical] = struct{}{}
		}
		for callee := range p.CallsTo {
			if calledBy[callee] == nil {
				calledBy[callee] = make(map[string]struct{})
			}
			calledBy[callee][p.Canonical] = struct{}{}
		}
	}

	// Pairwise relatedness.
	type edgeKey struct{ a, b string }
	edges := make(map[edgeKey]CodeRelatednessEdge, len(pkgs)*2)
	for i := range pkgs {
		p := &pkgs[i]
		p.ImportFanIn = len(importedBy[p.Canonical])
		p.ImportFanOut = len(p.ImportsTo)
		p.CallFanIn = len(calledBy[p.Canonical])
		p.CallFanOut = len(p.CallsTo)
		p.AnchorLabels = len(p.AnchorLabelsSet)
		p.LinkedNotes = len(p.NotePathsSet)

		for j := range pkgs {
			if i == j {
				continue
			}
			q := &pkgs[j]

			importsBoth := 0
			if _, ok := p.ImportsTo[q.Canonical]; ok {
				importsBoth++
			}
			if _, ok := q.ImportsTo[p.Canonical]; ok {
				importsBoth++
			}

			callsBoth := 0
			if _, ok := p.CallsTo[q.Canonical]; ok {
				callsBoth++
			}
			if _, ok := q.CallsTo[p.Canonical]; ok {
				callsBoth++
			}

			sharedAnchors := intersectCount(p.AnchorLabelsSet, q.AnchorLabelsSet)

			callsTo := crossPkgCalls[crossCallKey{callerPkg: p.Canonical, calleePkg: q.Canonical}]

			// Weights tuned for scanability:
			// - imports/calls are strong ties
			// - shared anchors are a weak tie
			// - repeated cross-pkg callsites amplify tie strength modestly
			score := importsBoth*3 + callsBoth*2 + sharedAnchors*1
			score += min(callsTo/10, 5)
			if score <= 0 {
				continue
			}

			edges[edgeKey{a: p.Canonical, b: q.Canonical}] = CodeRelatednessEdge{
				To:                 q.ImportPath,
				Score:              score,
				ImportsBothWays:    importsBoth,
				CallsBothWays:      callsBoth,
				SharedAnchorLabels: sharedAnchors,
				CallsTo:            callsTo,
			}
		}
	}

	// Top related per package.
	for i := range pkgs {
		p := &pkgs[i]
		var rel []CodeRelatednessEdge
		for j := range pkgs {
			if i == j {
				continue
			}
			if e, ok := edges[edgeKey{a: p.Canonical, b: pkgs[j].Canonical}]; ok {
				rel = append(rel, e)
			}
		}
		sort.Slice(rel, func(i, j int) bool {
			if rel[i].Score != rel[j].Score {
				return rel[i].Score > rel[j].Score
			}
			return rel[i].To < rel[j].To
		})
		if len(rel) > opts.MaxRelated {
			rel = rel[:opts.MaxRelated]
		}
		p.TopRelated = rel
	}

	// Clusters from edges above threshold.
	// Require bidirectional edges (both A→B and B→A exceed threshold) to form tighter clusters
	// and avoid long chains from accumulating weak one-directional connections.
	uf := newUnionFind()
	for _, p := range pkgs {
		uf.add(p.Canonical)
	}
	for k, e := range edges {
		if e.Score >= opts.ClusterThreshold {
			// Check if the reverse edge also exceeds threshold
			rev := edgeKey{a: k.b, b: k.a}
			if revEdge, ok := edges[rev]; ok && revEdge.Score >= opts.ClusterThreshold {
				uf.union(k.a, k.b)
			}
		}
	}
	clusterMap := make(map[string][]string)
	for _, p := range pkgs {
		root := uf.find(p.Canonical)
		clusterMap[root] = append(clusterMap[root], p.ImportPath)
	}
	var clusters []CodeRelatednessCluster
	for _, members := range clusterMap {
		sort.Strings(members)
		if len(members) <= 1 {
			continue
		}
		clusters = append(clusters, CodeRelatednessCluster{Packages: members})
	}
	sort.Slice(clusters, func(i, j int) bool {
		if len(clusters[i].Packages) != len(clusters[j].Packages) {
			return len(clusters[i].Packages) > len(clusters[j].Packages)
		}
		return strings.Join(clusters[i].Packages, ",") < strings.Join(clusters[j].Packages, ",")
	})

	// God candidates.
	gods := make([]CodeRelatednessGodCandidate, 0, len(pkgs))
	for _, p := range pkgs {
		score := (p.ImportFanIn+p.ImportFanOut)*2 + (p.CallFanIn+p.CallFanOut)*2 + p.AnchorLabels + p.LinkedNotes
		gods = append(gods, CodeRelatednessGodCandidate{
			Package:          p.ImportPath,
			Dir:              p.DirRel,
			Files:            p.Files,
			ImportFanIn:      p.ImportFanIn,
			ImportFanOut:     p.ImportFanOut,
			CallFanIn:        p.CallFanIn,
			CallFanOut:       p.CallFanOut,
			AnchorLabels:     p.AnchorLabels,
			LinkedNotes:      p.LinkedNotes,
			CombinedGodScore: score,
		})
	}
	sort.Slice(gods, func(i, j int) bool {
		if gods[i].CombinedGodScore != gods[j].CombinedGodScore {
			return gods[i].CombinedGodScore > gods[j].CombinedGodScore
		}
		return gods[i].Package < gods[j].Package
	})
	if len(gods) > 10 {
		gods = gods[:10]
	}

	// Final package shaping for output.
	signals := computePkgGraphSignals(ctx, pkgs, intel, opts.VaultPath)
	outPkgs := make([]CodeRelatednessPackage, 0, len(pkgs))
	for _, p := range pkgs {
		hot := hotFilesForPackage(opts.VaultPath, fileHot[p.Canonical], opts.HotFilesLimit)
		var langs []string
		for lang := range packageLangs[p.ImportPath] {
			langs = append(langs, lang)
		}
		sort.Strings(langs)
		sig := signals[p.ImportPath]
		outPkgs = append(outPkgs, CodeRelatednessPackage{
			ImportPath:      p.ImportPath,
			Dir:             p.DirRel,
			Files:           p.Files,
			ImportsInternal: p.ImportFanOut,
			ImportedBy:      p.ImportFanIn,
			CallsInternal:   p.CallFanOut,
			CalledBy:        p.CallFanIn,
			Langs:           langs,
			GraphAuthority:  sig.maxAuthority,
			GraphHub:        sig.maxHub,
			AnchorPageRank:  sig.anchorPR,
			GraphSignalHint: sig.strength(),
			AnchorLabels:    p.AnchorLabels,
			LinkedNotes:     p.LinkedNotes,
			TopRelated:      p.TopRelated,
			HotFiles:        hot,
		})
	}
	sort.Slice(outPkgs, func(i, j int) bool { return outPkgs[i].ImportPath < outPkgs[j].ImportPath })

	critical := buildCriticalSymbols(ctx, intel, docStore, crossPkgSymbols, opts.VaultPath, fileToPkg, anchorByID, noteCache, opts.CriticalLimit)
	coupling := buildCouplingEdges(
		ctx,
		intel,
		docStore,
		importEdgeFiles,
		importEdgeFileSets,
		anchorByID,
		noteCache,
		crossPkgCalls,
		edgeCalleeCounts,
		edgeCallerFileCounts,
		pkgs,
		opts.VaultPath,
		25,
		5,
		5,
	)

	docGapCount := 0
	for _, sym := range critical {
		if sym.DocGap {
			docGapCount++
		}
	}

	return CodeRelatednessReport{
		VaultPath:       opts.VaultPath,
		Roots:           opts.Roots,
		Module:          modulePath,
		Packages:        outPkgs,
		Clusters:        clusters,
		GodCandidates:   gods,
		CriticalSymbols: critical,
		DocGapCount:     docGapCount,
		CouplingEdges:   coupling,
	}, nil
}
