package actions

import (
	"context"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/atomicobject/rhizome/pkg/anchors"
	semstore "github.com/atomicobject/rhizome/pkg/anchors/sqlite"
	"github.com/atomicobject/rhizome/pkg/codefile"
	"github.com/atomicobject/rhizome/pkg/paths"
	"github.com/atomicobject/rhizome/pkg/vault/ignore"
)

type CodeRelatednessIntel interface {
	CallsFromFile(ctx context.Context, file string) ([]codeanchor.CallSite, error)
	SymbolsByFile(ctx context.Context, file string) ([]string, error)
	Ancestors(ctx context.Context, fqn string) ([]string, error)
	AnchorsBySymbols(ctx context.Context, symbols []string) (map[int64][]string, error)
	AnchorsByCallFiles(ctx context.Context, files []string) ([]int64, error)
	AnchorsByPathPrefix(ctx context.Context, target string) ([]int64, error)
	AnchorsByGlobMatch(ctx context.Context, target string) ([]int64, error)
	Anchors(ctx context.Context) ([]codeanchor.Anchor, error)
	NotesForAnchor(ctx context.Context, anchorID int64) ([]codeanchor.Note, error)
	IndexedFilePaths(ctx context.Context) ([]string, error)
	GraphDocScoresByPaths(ctx context.Context, paths []string) (map[string]semstore.GraphDocScore, error)
	AnchorScoresByIDs(ctx context.Context, anchorIDs []string) (map[string]float64, error)
}

type CodeRelatednessOptions struct {
	VaultPath        string
	Roots            []string
	Excludes         []string
	MaxRelated       int
	ClusterThreshold int
	IncludeTests     bool
	CriticalLimit    int
	HotFilesLimit    int
}

type CodeRelatednessReport struct {
	VaultPath string   `json:"vaultPath"`
	Roots     []string `json:"roots"`
	Module    string   `json:"module"`

	Packages []CodeRelatednessPackage `json:"packages"`
	Clusters []CodeRelatednessCluster `json:"clusters"`

	GodCandidates []CodeRelatednessGodCandidate `json:"godCandidates"`

	// CriticalSymbols ranks exported symbols that are called from other packages.
	CriticalSymbols []CodeRelatednessSymbol `json:"criticalSymbols,omitempty"`

	// DocGapCount is the number of critical symbols with no documentation coverage.
	DocGapCount int `json:"docGapCount,omitempty"`

	// CouplingEdges lists the strongest directed cross-package coupling edges.
	CouplingEdges []CodeRelatednessCouplingEdge `json:"couplingEdges,omitempty"`
}

type CodeRelatednessCluster struct {
	Packages []string `json:"packages"`
}

type CodeRelatednessGodCandidate struct {
	Package          string `json:"package"`
	Dir              string `json:"dir"`
	Files            int    `json:"files"`
	ImportFanIn      int    `json:"importFanIn"`
	ImportFanOut     int    `json:"importFanOut"`
	CallFanIn        int    `json:"callFanIn"`
	CallFanOut       int    `json:"callFanOut"`
	AnchorLabels     int    `json:"anchorLabels"`
	LinkedNotes      int    `json:"linkedNotes"`
	CombinedGodScore int    `json:"combinedGodScore"`
}

type CodeRelatednessPackage struct {
	ImportPath string `json:"importPath"`
	Dir        string `json:"dir"`
	Files      int    `json:"files"`

	ImportsInternal int `json:"importsInternal"`
	ImportedBy      int `json:"importedBy"`
	CallsInternal   int `json:"callsInternal"`
	CalledBy        int `json:"calledBy"`

	AnchorLabels int `json:"anchorLabels"`
	LinkedNotes  int `json:"linkedNotes"`

	Langs           []string `json:"langs,omitempty"`
	GraphAuthority  float64  `json:"graphAuthority,omitempty"`
	GraphHub        float64  `json:"graphHub,omitempty"`
	AnchorPageRank  float64  `json:"anchorPageRank,omitempty"`
	GraphSignalHint float64  `json:"-"`

	TopRelated []CodeRelatednessEdge `json:"topRelated"`

	// HotFiles highlights individual files that drive cross-package coupling.
	HotFiles []CodeRelatednessFileHotspot `json:"hotFiles,omitempty"`
}

type CodeRelatednessEdge struct {
	To                 string `json:"to"`
	Score              int    `json:"score"`
	ImportsBothWays    int    `json:"importsBothWays"`
	CallsBothWays      int    `json:"callsBothWays"`
	SharedAnchorLabels int    `json:"sharedAnchorLabels"`

	// CallsTo counts cross-package callsites from this package to the destination package.
	CallsTo int `json:"callsTo,omitempty"`
}

type CodeRelatednessCouplingEdge struct {
	FromPkg string `json:"fromPkg"`
	ToPkg   string `json:"toPkg"`

	CallsTo  int  `json:"callsTo"`
	Imports  bool `json:"imports"`
	BothWays bool `json:"bothWays"`

	ImportFiles    int                                 `json:"importFiles,omitempty"`
	ImportStmts    int                                 `json:"importStmts,omitempty"`
	TopImportFiles []CodeRelatednessCouplingCallerFile `json:"topImportFiles,omitempty"`

	TopCallees     []CodeRelatednessCouplingCallee     `json:"topCallees,omitempty"`
	TopCallerFiles []CodeRelatednessCouplingCallerFile `json:"topCallerFiles,omitempty"`
}

type CodeRelatednessCouplingCallee struct {
	FQN      string   `json:"fqn"`
	Calls    int      `json:"calls"`
	Kind     string   `json:"kind,omitempty"`
	File     string   `json:"file,omitempty"`
	Exported bool     `json:"exported,omitempty"`
	Anchors  []string `json:"anchors,omitempty"`
	Mentions int      `json:"mentions,omitempty"`
	Links    int      `json:"links,omitempty"`
}

type CodeRelatednessCouplingCallerFile struct {
	File  string `json:"file"`
	Calls int    `json:"calls"`
}

type CodeRelatednessFileHotspot struct {
	File            string   `json:"file"`
	CallsOutOfPkg   int      `json:"callsOutOfPkg"`
	DistinctCallees int      `json:"distinctCalleePkgs"`
	CalleePkgs      []string `json:"calleePkgs,omitempty"`
}

type CodeRelatednessCallerPkg struct {
	Pkg   string `json:"pkg"`
	Calls int    `json:"calls"`
	Files int    `json:"files"`
}

type CodeRelatednessCallerFile struct {
	File  string `json:"file"`
	Pkg   string `json:"pkg"`
	Calls int    `json:"calls"`
}

type CodeRelatednessSymbol struct {
	Lang string `json:"lang"`
	FQN  string `json:"fqn"`
	Pkg  string `json:"pkg"`
	Name string `json:"name"`

	Calls          int `json:"calls"`
	CallerPackages int `json:"callerPackages"`
	CallerFiles    int `json:"callerFiles"`

	TopCallerPackages []CodeRelatednessCallerPkg  `json:"topCallerPackages,omitempty"`
	TopCallerFiles    []CodeRelatednessCallerFile `json:"topCallerFiles,omitempty"`
	TopCallerShare    float64                     `json:"topCallerShare,omitempty"`

	MatchingAnchors int      `json:"matchingAnchors,omitempty"`
	MatchingNotes   int      `json:"matchingNotes,omitempty"`
	AnchorLabels    []string `json:"anchorLabels,omitempty"`
	NotePaths       []string `json:"notePaths,omitempty"`

	DocMentions int    `json:"docMentions,omitempty"`
	DocLinks    int    `json:"docLinks,omitempty"`
	DocResolved bool   `json:"docResolved,omitempty"`
	DocKind     string `json:"docKind,omitempty"`
	DocPath     string `json:"docPath,omitempty"`

	// DocGap indicates a high-fanin symbol with no documentation coverage.
	// True when the symbol has multiple caller packages but no anchors, notes, or doc links.
	DocGap bool `json:"docGap,omitempty"`
}

type crossCallKey struct {
	callerPkg string
	calleePkg string
}

type symKey struct {
	lang codeanchor.Lang
	pkg  string
	name string
}

type symAgg struct {
	calls       int
	callerPkgs  map[string]struct{}
	callerFiles map[string]struct{}
	callsByPkg  map[string]int
	callsByFile map[string]int
}

type fileHotspotAgg struct {
	calls      int
	calleePkgs map[string]struct{}
}

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

type discoveredPkg struct {
	ImportPath string
	Canonical  string
	DirAbs     string
	DirRel     string
	FilesAbs   []string
	Files      int

	ImportsTo map[string]struct{}
	CallsTo   map[string]struct{}

	ImportFanIn  int
	ImportFanOut int
	CallFanIn    int
	CallFanOut   int

	AnchorIDs       map[int64]struct{}
	AnchorLabelsSet map[string]struct{}
	AnchorLabels    int

	NotePathsSet map[string]struct{}
	LinkedNotes  int

	TopRelated []CodeRelatednessEdge
}

func discoverPackages(ctx context.Context, vaultPath string, roots []string, matcher *ignore.Matcher, includeTests bool, intel CodeRelatednessIntel) ([]discoveredPkg, int, error) {
	if intel == nil {
		return discoverCodePackages(vaultPath, roots, matcher, includeTests)
	}

	vaultPaths, _ := paths.NewVaultPaths(vaultPath)
	files, err := intel.IndexedFilePaths(ctx)
	if err != nil {
		return nil, 0, err
	}
	if len(files) == 0 {
		return discoverCodePackages(vaultPath, roots, matcher, includeTests)
	}

	type dirInfo struct {
		abs   string
		rel   string
		files []string
	}

	dirs := make(map[string]*dirInfo)
	filesTotal := 0

	rootAbs := make([]string, 0, len(roots))
	for _, r := range roots {
		if r == "" {
			continue
		}
		if ref, err := paths.ResolveCodeRefWithVaultPaths(vaultPaths, r); err == nil && ref.Abs != "" {
			rootAbs = append(rootAbs, ref.Abs.String())
		}
	}

	addFile := func(abs string) {
		dir := filepath.Dir(abs)
		relDir := filepath.ToSlash(dir)
		if rel, err := vaultPaths.RelStrict(dir); err == nil {
			relDir = rel.String()
		}
		if relDir == "" {
			relDir = "."
		}
		info := dirs[dir]
		if info == nil {
			info = &dirInfo{abs: dir, rel: relDir}
			dirs[dir] = info
		}
		info.files = append(info.files, abs)
		filesTotal++
	}

	for _, file := range files {
		if strings.TrimSpace(file) == "" {
			continue
		}
		ref, err := paths.ResolveCodeRefWithVaultPaths(vaultPaths, file)
		if err != nil || ref.Abs == "" || ref.Rel == "" {
			continue
		}
		abs := ref.Abs.String()
		rel := ref.Rel.String()
		if matcher != nil && matcher.IsIgnored(rel, false) {
			continue
		}
		if len(rootAbs) > 0 && !withinRoots(abs, rootAbs) {
			continue
		}
		if !includeTests && isTestFile(abs) {
			continue
		}
		addFile(abs)
	}

	modulePath := readGoModulePath(vaultPath)

	var pkgs []discoveredPkg
	for _, d := range dirs {
		sort.Strings(d.files)
		importPath := d.rel
		if modulePath != "" {
			if d.rel == "." {
				importPath = modulePath
			} else {
				importPath = modulePath + "/" + strings.TrimPrefix(d.rel, "./")
				importPath = strings.TrimSuffix(importPath, "/.")
			}
		}
		pkgs = append(pkgs, discoveredPkg{
			ImportPath: importPath,
			Canonical:  normalizePkgName(importPath),
			DirAbs:     d.abs,
			DirRel:     d.rel,
			FilesAbs:   d.files,
			Files:      len(d.files),
		})
	}
	sort.Slice(pkgs, func(i, j int) bool { return pkgs[i].ImportPath < pkgs[j].ImportPath })
	return pkgs, filesTotal, nil
}

func discoverCodePackages(vaultPath string, roots []string, matcher *ignore.Matcher, includeTests bool) ([]discoveredPkg, int, error) {
	type dirInfo struct {
		abs   string
		rel   string
		files []string
	}

	vaultPaths, _ := paths.NewVaultPaths(vaultPath)
	dirs := make(map[string]*dirInfo)
	filesTotal := 0

	addFile := func(abs string) {
		dir := filepath.Dir(abs)
		relDir := filepath.ToSlash(dir)
		if rel, err := vaultPaths.RelStrict(dir); err == nil {
			relDir = rel.String()
		}
		if relDir == "." {
			relDir = "."
		}
		info := dirs[dir]
		if info == nil {
			info = &dirInfo{abs: dir, rel: relDir}
			dirs[dir] = info
		}
		info.files = append(info.files, abs)
		filesTotal++
	}

	for _, root := range roots {
		if root == "" {
			continue
		}
		ref, err := paths.ResolveCodeRefWithVaultPaths(vaultPaths, root)
		if err != nil || ref.Abs == "" {
			continue
		}
		rootAbs := ref.Abs.String()

		st, err := os.Stat(rootAbs)
		if err != nil {
			return nil, 0, err
		}
		if !st.IsDir() {
			if isCodeFile(rootAbs, includeTests) && !isIgnored(vaultPath, rootAbs, false, matcher) {
				addFile(rootAbs)
			}
			continue
		}

		err = filepath.WalkDir(rootAbs, func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			if isIgnored(vaultPath, path, d.IsDir(), matcher) {
				if d.IsDir() {
					return filepath.SkipDir
				}
				return nil
			}
			if d.IsDir() {
				return nil
			}
			if isCodeFile(path, includeTests) {
				addFile(path)
			}
			return nil
		})
		if err != nil {
			return nil, 0, err
		}
	}

	modulePath := readGoModulePath(vaultPath)

	var pkgs []discoveredPkg
	for _, d := range dirs {
		sort.Strings(d.files)
		importPath := d.rel
		if modulePath != "" {
			if d.rel == "." {
				importPath = modulePath
			} else {
				importPath = modulePath + "/" + strings.TrimPrefix(d.rel, "./")
				importPath = strings.TrimSuffix(importPath, "/.")
			}
		}
		pkgs = append(pkgs, discoveredPkg{
			ImportPath: importPath,
			Canonical:  normalizePkgName(importPath),
			DirAbs:     d.abs,
			DirRel:     d.rel,
			FilesAbs:   d.files,
			Files:      len(d.files),
		})
	}
	sort.Slice(pkgs, func(i, j int) bool { return pkgs[i].ImportPath < pkgs[j].ImportPath })
	return pkgs, filesTotal, nil
}

// isCodeFile returns true if the path has a recognized code extension.
func isCodeFile(path string, includeTests bool) bool {
	ext := strings.ToLower(filepath.Ext(path))
	if _, ok := langByExt[ext]; !ok {
		return false
	}
	base := filepath.Base(path)
	if strings.HasPrefix(base, ".") {
		return false
	}
	if !includeTests && isTestFile(path) {
		return false
	}
	return true
}

// isTestFile returns true if the path looks like a test file.
func isTestFile(path string) bool {
	base := strings.ToLower(filepath.Base(path))
	// Go: _test.go
	if strings.HasSuffix(base, "_test.go") {
		return true
	}
	// Python: test_*.py or *_test.py
	if strings.HasSuffix(base, ".py") {
		if strings.HasPrefix(base, "test_") || strings.HasSuffix(base, "_test.py") {
			return true
		}
	}
	// TypeScript/JavaScript: *.test.ts, *.spec.ts, etc.
	if strings.Contains(base, ".test.") || strings.Contains(base, ".spec.") {
		return true
	}
	return false
}

func isIgnored(vaultPath, abs string, isDir bool, matcher *ignore.Matcher) bool {
	if matcher == nil || vaultPath == "" {
		return false
	}
	vaultPaths, err := paths.NewVaultPaths(vaultPath)
	if err != nil {
		return false
	}
	rel, err := vaultPaths.RelStrict(abs)
	if err != nil {
		return false
	}
	return matcher.IsIgnored(rel.String(), isDir)
}

func withinRoots(abs string, roots []string) bool {
	if len(roots) == 0 {
		return true
	}
	abs = normalizeAbsPath(abs)
	for _, r := range roots {
		if r == "" {
			continue
		}
		root := normalizeAbsPath(r)
		if abs == root || strings.HasPrefix(abs, root+"/") {
			return true
		}
	}
	return false
}

func normalizeAbsPath(path string) string {
	return paths.NormalizeAbsPathForCompare(path)
}

var langByExt = buildLangByExt()

func buildLangByExt() map[string]codeanchor.Lang {
	out := map[string]codeanchor.Lang{
		".py":  codeanchor.LangPy,
		".go":  codeanchor.LangGo,
		".cs":  codeanchor.LangCs,
		".php": codeanchor.LangPhp,
	}
	for _, ext := range codefile.TypeScriptJavaScriptExtensions() {
		out[ext] = codeanchor.LangTS
	}
	return out
}

func langForPath(path string) codeanchor.Lang {
	ext := strings.ToLower(filepath.Ext(path))
	return langByExt[ext]
}

// normalizePkgName canonicalizes package/module identifiers for comparison.
// It converts both dots (Python) and backslashes (Windows) to forward slashes
// so matching works across languages (callee FQNs use dots, discovered packages use slashes).
func normalizePkgName(pkg string) string {
	pkg = strings.TrimSpace(pkg)
	pkg = strings.TrimPrefix(pkg, "./")
	pkg = strings.TrimPrefix(pkg, "/")
	pkg = filepath.ToSlash(pkg)
	// Convert Python-style dot separators to slashes for consistent matching.
	pkg = strings.ReplaceAll(pkg, ".", "/")
	return pkg
}

// matchInternalPkg returns the closest ancestor in the package alias map.
func matchInternalPkg(pkg string, aliasToCanonical map[string]string) (string, bool) {
	if pkg == "" {
		return "", false
	}
	if canon, ok := aliasToCanonical[pkg]; ok {
		return canon, true
	}
	parts := strings.Split(pkg, "/")
	for len(parts) > 1 {
		parts = parts[:len(parts)-1]
		cand := strings.Join(parts, "/")
		if canon, ok := aliasToCanonical[cand]; ok {
			return canon, true
		}
	}
	return "", false
}

func stripCommonRoots(path string) string {
	path = strings.TrimPrefix(path, "/")
	parts := strings.Split(path, "/")
	for i, p := range parts {
		if p == "src" || p == "lib" {
			trimmed := strings.Join(parts[i+1:], "/")
			return trimmed
		}
	}
	return ""
}

// lookupSymbolsForFile best-effort resolves symbols for absolute or vault-relative paths.
func lookupSymbolsForFile(ctx context.Context, intel CodeRelatednessIntel, vaultPath, file string) []string {
	if intel == nil {
		return nil
	}
	var out []string
	for _, p := range candidatePathsForLookup(vaultPath, file) {
		syms, err := intel.SymbolsByFile(ctx, p)
		if err != nil {
			continue
		}
		if len(syms) == 0 {
			continue
		}
		out = append(out, syms...)
	}
	return out
}

// candidatePathsForLookup returns abs + relative variants to maximize matching against the intel store.
func candidatePathsForLookup(vaultPath, file string) []string {
	var out []string
	vaultPaths, _ := paths.NewVaultPaths(vaultPath)
	if vaultPaths.Root() != "" {
		if ref, err := paths.ResolveCodeRefWithVaultPaths(vaultPaths, file); err == nil {
			if ref.Abs != "" {
				out = append(out, ref.Abs.String())
			}
			if ref.Rel != "" {
				out = append(out, ref.Rel.String())
			}
		}
	} else if rel, err := paths.CleanRelPath(file); err == nil && rel.String() != "" {
		out = append(out, string(paths.NormalizeCode(rel.String())))
	}
	for i := 0; i < len(out); i++ {
		out[i] = filepath.ToSlash(out[i])
	}
	return dedupeStrings(out)
}

func dedupeStrings(in []string) []string {
	seen := make(map[string]struct{}, len(in))
	var out []string
	for _, s := range in {
		if s == "" {
			continue
		}
		if _, ok := seen[s]; ok {
			continue
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	return out
}

func matchingAnchorIDsForFile(ctx context.Context, intel CodeRelatednessIntel, vaultPath, file string, ancestorsCache map[string][]string) map[int64]struct{} {
	ids := make(map[int64]struct{})
	if intel == nil {
		return ids
	}

	// Symbols + ancestors.
	symbols := lookupSymbolsForFile(ctx, intel, vaultPath, file)
	seen := make(map[string]struct{}, len(symbols))
	var all []string
	for _, fqn := range symbols {
		if fqn == "" {
			continue
		}
		if _, ok := seen[fqn]; ok {
			continue
		}
		seen[fqn] = struct{}{}
		all = append(all, fqn)

		anc, ok := ancestorsCache[fqn]
		if !ok {
			anc, _ = intel.Ancestors(ctx, fqn)
			ancestorsCache[fqn] = anc
		}
		for _, a := range anc {
			if a == "" {
				continue
			}
			if _, ok := seen[a]; ok {
				continue
			}
			seen[a] = struct{}{}
			all = append(all, a)
		}
	}

	matchedBySymbol, err := intel.AnchorsBySymbols(ctx, all)
	if err == nil {
		for id := range matchedBySymbol {
			ids[id] = struct{}{}
		}
	}

	for _, lookup := range candidatePathsForLookup(vaultPath, file) {
		callAnchors, err := intel.AnchorsByCallFiles(ctx, []string{lookup})
		if err == nil {
			for _, id := range callAnchors {
				ids[id] = struct{}{}
			}
		}
		pathAnchors, err := intel.AnchorsByPathPrefix(ctx, lookup)
		if err == nil {
			for _, id := range pathAnchors {
				ids[id] = struct{}{}
			}
		}
		globAnchors, err := intel.AnchorsByGlobMatch(ctx, lookup)
		if err == nil {
			for _, id := range globAnchors {
				ids[id] = struct{}{}
			}
		}
	}
	return ids
}

func readGoModulePath(root string) string {
	data, err := os.ReadFile(filepath.Join(root, "go.mod"))
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "module ") {
			return strings.TrimSpace(strings.TrimPrefix(line, "module "))
		}
	}
	return ""
}

func intersectCount(a, b map[string]struct{}) int {
	if len(a) == 0 || len(b) == 0 {
		return 0
	}
	if len(a) > len(b) {
		a, b = b, a
	}
	n := 0
	for k := range a {
		if _, ok := b[k]; ok {
			n++
		}
	}
	return n
}

type pkgGraphSignal struct {
	anchorPR      float64
	maxAuthority  float64
	maxHub        float64
	authoritySeen bool
}

func (s pkgGraphSignal) strength() float64 {
	score := math.Log1p(s.anchorPR * 1000)
	score += s.maxAuthority * 2
	score += s.maxHub * 1.25
	return score
}

func computePkgGraphSignals(ctx context.Context, pkgs []discoveredPkg, intel CodeRelatednessIntel, vaultPath string) map[string]pkgGraphSignal {
	signals := make(map[string]pkgGraphSignal, len(pkgs))
	if intel == nil || len(pkgs) == 0 {
		return signals
	}

	vaultPaths, _ := paths.NewVaultPaths(vaultPath)

	var allPaths []string
	var allAnchorIDs []string
	for _, p := range pkgs {
		for _, abs := range p.FilesAbs {
			if rel := vaultPaths.NormalizeDocPath(abs); rel != "" {
				allPaths = append(allPaths, rel)
			}
		}
		for id := range p.AnchorIDs {
			allAnchorIDs = append(allAnchorIDs, fmt.Sprintf("%d", id))
		}
	}

	anchorScores := map[string]float64{}
	if len(allAnchorIDs) > 0 {
		if sc, err := intel.AnchorScoresByIDs(ctx, allAnchorIDs); err == nil {
			anchorScores = sc
		}
	}

	docScores := map[string]semstore.GraphDocScore{}
	if len(allPaths) > 0 {
		if sc, err := intel.GraphDocScoresByPaths(ctx, allPaths); err == nil {
			docScores = sc
		}
	}

	for _, p := range pkgs {
		sig := pkgGraphSignal{}
		for id := range p.AnchorIDs {
			if pr, ok := anchorScores[fmt.Sprintf("%d", id)]; ok {
				sig.anchorPR += pr
			}
		}
		for _, path := range p.FilesAbs {
			rel := vaultPaths.NormalizeDocPath(path)
			if rel == "" {
				continue
			}
			if sc, ok := docScores[rel]; ok {
				if sc.Authority > sig.maxAuthority {
					sig.maxAuthority = sc.Authority
				}
				if sc.Hub > sig.maxHub {
					sig.maxHub = sc.Hub
				}
				sig.authoritySeen = sig.authoritySeen || sc.DocType != ""
			}
		}
		signals[p.ImportPath] = sig
	}
	return signals
}

func buildCriticalSymbols(
	ctx context.Context,
	intel CodeRelatednessIntel,
	docStore interface {
		DocStatsForFQNs(ctx context.Context, lang string, fqns []string) (map[string]codeanchor.DocStats, error)
	},
	crossPkgSymbols map[symKey]*symAgg,
	vaultPath string,
	fileToPkg map[string]string,
	anchorByID map[int64]codeanchor.Anchor,
	noteCache map[int64]map[string]struct{},
	limit int,
) []CodeRelatednessSymbol {
	if limit <= 0 {
		limit = 25
	}
	type item struct {
		key symKey
		agg *symAgg
	}
	items := make([]item, 0, len(crossPkgSymbols))
	for k, a := range crossPkgSymbols {
		items = append(items, item{key: k, agg: a})
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].agg.calls != items[j].agg.calls {
			return items[i].agg.calls > items[j].agg.calls
		}
		if len(items[i].agg.callerPkgs) != len(items[j].agg.callerPkgs) {
			return len(items[i].agg.callerPkgs) > len(items[j].agg.callerPkgs)
		}
		if items[i].key.pkg != items[j].key.pkg {
			return items[i].key.pkg < items[j].key.pkg
		}
		return items[i].key.name < items[j].key.name
	})
	if len(items) > limit {
		items = items[:limit]
	}

	docByFQN := map[string]codeanchor.DocStats{}
	if docStore != nil && len(items) > 0 {
		byLang := make(map[codeanchor.Lang][]string)
		for _, it := range items {
			fqn := it.key.pkg + "." + it.key.name
			byLang[it.key.lang] = append(byLang[it.key.lang], fqn)
		}
		for lang, fqns := range byLang {
			if len(fqns) == 0 {
				continue
			}
			if m, err := docStore.DocStatsForFQNs(ctx, string(lang), fqns); err == nil {
				for k, v := range m {
					docByFQN[k] = v
				}
			}
		}
	}

	var out []CodeRelatednessSymbol
	for _, it := range items {
		fqn := it.key.pkg + "." + it.key.name
		s := CodeRelatednessSymbol{
			Lang:           string(it.key.lang),
			FQN:            fqn,
			Pkg:            it.key.pkg,
			Name:           it.key.name,
			Calls:          it.agg.calls,
			CallerPackages: len(it.agg.callerPkgs),
			CallerFiles:    len(it.agg.callerFiles),
		}

		s.TopCallerPackages, s.TopCallerFiles, s.TopCallerShare = buildCallerNeighborhood(vaultPath, fileToPkg, it.agg, 8, 8)

		if intel != nil {
			matched, err := intel.AnchorsBySymbols(ctx, []string{fqn})
			if err == nil && len(matched) > 0 {
				labels := make(map[string]struct{})
				notes := make(map[string]struct{})
				for id := range matched {
					if a, ok := anchorByID[id]; ok && a.Label != "" {
						labels[a.Label] = struct{}{}
					}
					paths, ok := noteCache[id]
					if !ok {
						paths = make(map[string]struct{})
						if fetched, err := intel.NotesForAnchor(ctx, id); err == nil {
							for _, n := range fetched {
								if n.Path != "" {
									paths[n.Path] = struct{}{}
								}
							}
						}
						noteCache[id] = paths
					}
					for p := range paths {
						notes[p] = struct{}{}
					}
				}
				s.MatchingAnchors = len(labels)
				s.MatchingNotes = len(notes)
				s.AnchorLabels = sortedKeys(labels)
				s.NotePaths = sortedKeys(notes)
			}
		}

		if ds, ok := docByFQN[fqn]; ok {
			s.DocResolved = ds.Resolved
			s.DocKind = ds.Kind
			s.DocPath = ds.Path
			s.DocMentions = ds.Mentions
			s.DocLinks = ds.Links
		}

		// Flag high-fanin symbols with no documentation coverage.
		// A symbol is a doc gap if it has 2+ caller packages but no anchors, notes, or doc links.
		hasDoc := s.MatchingAnchors > 0 || s.MatchingNotes > 0 || s.DocLinks > 0 || s.DocMentions > 0
		if s.CallerPackages >= 2 && !hasDoc {
			s.DocGap = true
		}

		out = append(out, s)
	}
	return out
}

func buildCallerNeighborhood(vaultPath string, fileToPkg map[string]string, agg *symAgg, topPkgs, topFiles int) ([]CodeRelatednessCallerPkg, []CodeRelatednessCallerFile, float64) {
	if agg == nil {
		return nil, nil, 0
	}
	if topPkgs <= 0 {
		topPkgs = 8
	}
	if topFiles <= 0 {
		topFiles = 8
	}

	type pkgItem struct {
		pkg   string
		calls int
		files map[string]struct{}
	}
	pkgs := make(map[string]*pkgItem)
	for file, calls := range agg.callsByFile {
		pkg := fileToPkg[file]
		if pkg == "" {
			pkg = "(unknown)"
		}
		it := pkgs[pkg]
		if it == nil {
			it = &pkgItem{pkg: pkg, files: make(map[string]struct{})}
			pkgs[pkg] = it
		}
		it.calls += calls
		it.files[file] = struct{}{}
	}

	pkgList := make([]pkgItem, 0, len(pkgs))
	for _, it := range pkgs {
		pkgList = append(pkgList, *it)
	}
	sort.Slice(pkgList, func(i, j int) bool {
		if pkgList[i].calls != pkgList[j].calls {
			return pkgList[i].calls > pkgList[j].calls
		}
		return pkgList[i].pkg < pkgList[j].pkg
	})

	var outPkgs []CodeRelatednessCallerPkg
	topCalls := 0
	if len(pkgList) > 0 {
		topCalls = pkgList[0].calls
	}
	for i := 0; i < len(pkgList) && i < topPkgs; i++ {
		outPkgs = append(outPkgs, CodeRelatednessCallerPkg{
			Pkg:   pkgList[i].pkg,
			Calls: pkgList[i].calls,
			Files: len(pkgList[i].files),
		})
	}

	type fileItem struct {
		file  string
		pkg   string
		calls int
	}
	fileList := make([]fileItem, 0, len(agg.callsByFile))
	for file, calls := range agg.callsByFile {
		fileList = append(fileList, fileItem{file: relToRootSlash(vaultPath, file), pkg: fileToPkg[file], calls: calls})
	}
	sort.Slice(fileList, func(i, j int) bool {
		if fileList[i].calls != fileList[j].calls {
			return fileList[i].calls > fileList[j].calls
		}
		return fileList[i].file < fileList[j].file
	})

	var outFiles []CodeRelatednessCallerFile
	for i := 0; i < len(fileList) && i < topFiles; i++ {
		pkg := fileList[i].pkg
		if pkg == "" {
			pkg = "(unknown)"
		}
		outFiles = append(outFiles, CodeRelatednessCallerFile{
			File:  fileList[i].file,
			Pkg:   pkg,
			Calls: fileList[i].calls,
		})
	}

	share := 0.0
	if agg.calls > 0 && topCalls > 0 {
		share = float64(topCalls) / float64(agg.calls)
	}
	return outPkgs, outFiles, share
}

func sortedKeys(m map[string]struct{}) []string {
	if len(m) == 0 {
		return nil
	}
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func hotFilesForPackage(vaultPath string, byFile map[string]*fileHotspotAgg, limit int) []CodeRelatednessFileHotspot {
	if limit <= 0 {
		limit = 6
	}
	if len(byFile) == 0 {
		return nil
	}
	type item struct {
		file string
		agg  *fileHotspotAgg
	}
	items := make([]item, 0, len(byFile))
	for f, a := range byFile {
		items = append(items, item{file: f, agg: a})
	}
	sort.Slice(items, func(i, j int) bool {
		if len(items[i].agg.calleePkgs) != len(items[j].agg.calleePkgs) {
			return len(items[i].agg.calleePkgs) > len(items[j].agg.calleePkgs)
		}
		if items[i].agg.calls != items[j].agg.calls {
			return items[i].agg.calls > items[j].agg.calls
		}
		return items[i].file < items[j].file
	})
	if len(items) > limit {
		items = items[:limit]
	}
	out := make([]CodeRelatednessFileHotspot, 0, len(items))
	for _, it := range items {
		calleePkgs := make([]string, 0, len(it.agg.calleePkgs))
		for p := range it.agg.calleePkgs {
			calleePkgs = append(calleePkgs, p)
		}
		sort.Strings(calleePkgs)
		if len(calleePkgs) > 5 {
			calleePkgs = calleePkgs[:5]
		}
		out = append(out, CodeRelatednessFileHotspot{
			File:            relToRootSlash(vaultPath, it.file),
			CallsOutOfPkg:   it.agg.calls,
			DistinctCallees: len(it.agg.calleePkgs),
			CalleePkgs:      calleePkgs,
		})
	}
	return out
}

func relToRootSlash(root, abs string) string {
	if root == "" || abs == "" {
		return filepath.ToSlash(abs)
	}
	vaultPaths, err := paths.NewVaultPaths(root)
	if err != nil {
		return filepath.ToSlash(abs)
	}
	rel, err := vaultPaths.RelStrict(abs)
	if err != nil {
		return filepath.ToSlash(abs)
	}
	if rel.String() == "" {
		return "."
	}
	return rel.String()
}

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

type unionFind struct {
	parent map[string]string
	rank   map[string]int
}

func newUnionFind() *unionFind {
	return &unionFind{parent: make(map[string]string), rank: make(map[string]int)}
}

func (u *unionFind) add(x string) {
	if x == "" {
		return
	}
	if _, ok := u.parent[x]; ok {
		return
	}
	u.parent[x] = x
	u.rank[x] = 0
}

func (u *unionFind) find(x string) string {
	if x == "" {
		return ""
	}
	p, ok := u.parent[x]
	if !ok {
		u.add(x)
		return x
	}
	if p == x {
		return x
	}
	root := u.find(p)
	u.parent[x] = root
	return root
}

func (u *unionFind) union(a, b string) {
	ra := u.find(a)
	rb := u.find(b)
	if ra == "" || rb == "" || ra == rb {
		return
	}
	if u.rank[ra] < u.rank[rb] {
		u.parent[ra] = rb
		return
	}
	if u.rank[ra] > u.rank[rb] {
		u.parent[rb] = ra
		return
	}
	u.parent[rb] = ra
	u.rank[ra]++
}
