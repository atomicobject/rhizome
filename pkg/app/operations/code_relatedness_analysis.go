package operations

import (
	"context"
	"fmt"
	"math"
	"path/filepath"
	"sort"

	"github.com/atomicobject/rhizome/pkg/anchors"
	"github.com/atomicobject/rhizome/pkg/paths"
	semstore "github.com/atomicobject/rhizome/pkg/store"
)

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
