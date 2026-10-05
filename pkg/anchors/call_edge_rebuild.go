package codeanchor

import (
	"context"
	"fmt"
	"os"
	pathpkg "path"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/atomicobject/rhizome/pkg/indexingperf"
	"github.com/atomicobject/rhizome/pkg/paths"
)

const (
	callEdgeWriteFlushPaths = 500
	callEdgeWriteFlushEdges = 20000
	callEdgeWriteFlushBytes = 8 << 20
)

type callEdgeRebuildItem struct {
	path string
	lang Lang
}

type callEdgeRebuildSources struct {
	canUseRefs       bool
	anchorLookup     intelMentionLookup
	cache            *callEdgeRebuildCache
	runtimeByPath    map[string]runtimeCodeIndexSource
	anchorsByPath    map[string][]IntelAnchor
	symbolRefsByPath map[string][]SymbolRefRow
	importRefsByPath map[string][]ImportRefRow
	moduleDefsByPath map[string][]ModuleDefRow
}

type callEdgeRebuildCache struct {
	moduleIDByPath      map[string]string
	ownerIDsByPath      map[string]map[string]string
	targetIDsByLangFQN  map[string][]string
	importTargetIDs     map[string]string
	goMethodIDs         map[string][]string
	pyFallbackIDsByCall map[string][]string
}

type CallEdgeSuffixSeed struct {
	Lang     Lang
	FQN      string
	AnchorID string
}

// callEdgeSuffixIndex is a per-rebuild cache for qualified suffix lookups so
// batch rebuilds can keep suffix matching without paying sqlite per probe.
type callEdgeSuffixIndex struct {
	idsByLangSuffix map[string][]string
}

type callEdgeWriteBuffer struct {
	batches []CallEdgesBatch
	edges   int
	bytes   int
}

func (b *callEdgeWriteBuffer) add(batch CallEdgesBatch) {
	b.batches = append(b.batches, batch)
	b.edges += len(batch.Edges)
	b.bytes += estimateCallEdgesBatchBytes(batch)
}

func (b *callEdgeWriteBuffer) shouldFlush() bool {
	if len(b.batches) == 0 {
		return false
	}
	return len(b.batches) >= callEdgeWriteFlushPaths || b.edges >= callEdgeWriteFlushEdges || b.bytes >= callEdgeWriteFlushBytes
}

func (b *callEdgeWriteBuffer) reset() {
	b.batches = nil
	b.edges = 0
	b.bytes = 0
}

func estimateCallEdgesBatchBytes(batch CallEdgesBatch) int {
	size := len(batch.Path) + 32
	for _, edge := range batch.Edges {
		size += len(edge.SrcID) + len(edge.DstID) + len(edge.Kind) + len(edge.MetaJSON) + 48
	}
	return size
}

func callEdgeCachePath(path string) string {
	return string(paths.NormalizeCode(path))
}

func callEdgeCacheLangFQNKey(lang Lang, fqn string) string {
	return strings.ToLower(string(lang)) + "|" + fqn
}

func callEdgeCacheImportKey(lang Lang, module string) string {
	return strings.ToLower(string(lang)) + "|" + module
}

func callEdgeCacheGoMethodKey(pkgImportPath, method string) string {
	return pkgImportPath + "|" + method
}

func callEdgeCachePyFallbackKey(callerDir, name string) string {
	return callerDir + "|" + name
}

func shouldUsePythonFallbackOnly(lang Lang, pkg, name string) bool {
	return lang == LangPy && strings.TrimSpace(name) != "" && strings.TrimSpace(pkg) == ""
}

func suffixMetricLangSuffix(lang Lang) string {
	switch lang {
	case LangGo:
		return "go"
	case LangPy:
		return "py"
	case LangCs:
		return "cs"
	case LangTS:
		return "ts"
	default:
		return strings.ToLower(string(lang))
	}
}

func addCallEdgeCacheEntries(ctx context.Context, metric string, entries int) {
	if entries > 0 {
		indexingperf.AddCount(ctx, metric, int64(entries))
	}
}

func addCallEdgeCacheLookup(ctx context.Context, metric string, hit bool) {
	if hit {
		indexingperf.AddCount(ctx, metric+".hit", 1)
		return
	}
	indexingperf.AddCount(ctx, metric+".miss", 1)
}

func addCallEdgeFallbackReason(ctx context.Context, reason string) {
	if strings.TrimSpace(reason) == "" {
		return
	}
	indexingperf.AddCount(ctx, "calledge.source_fallback."+reason+".count", 1)
}

func addCallEdgeSuffixSkip(ctx context.Context, reason string) {
	if strings.TrimSpace(reason) == "" {
		return
	}
	indexingperf.AddCount(ctx, "calledge.cache_build_suffix_skipped."+reason+".count", 1)
}

func addCallEdgeSuffixCandidateLang(ctx context.Context, lang Lang, entries int) {
	if entries <= 0 {
		return
	}
	indexingperf.AddCount(ctx, "calledge.cache_build_suffix.candidate."+suffixMetricLangSuffix(lang)+".count", int64(entries))
}

func addCallEdgeSuffixCandidateKind(ctx context.Context, kind string, entries int) {
	if entries <= 0 || strings.TrimSpace(kind) == "" {
		return
	}
	indexingperf.AddCount(ctx, "calledge.cache_build_suffix."+kind+"_candidate.count", int64(entries))
}

type callEdgeSuffixPolicy struct {
	minQualifiedPkgSegments int
	maxKeysPerSeed          int
}

var defaultCallEdgeSuffixPolicy = callEdgeSuffixPolicy{
	minQualifiedPkgSegments: 2,
	maxKeysPerSeed:          3,
}

func suffixPackageSeparator(lang Lang, pkg string) (string, bool) {
	switch lang {
	case LangGo:
		switch {
		case strings.Contains(pkg, "/"):
			return "/", true
		case strings.Contains(pkg, "."):
			return ".", true
		default:
			return "", false
		}
	case LangPy, LangCs, LangTS:
		return ".", true
	default:
		return "", false
	}
}

func suffixPackageParts(lang Lang, pkg string) []string {
	sep, ok := suffixPackageSeparator(lang, pkg)
	if !ok {
		return nil
	}
	rawParts := strings.Split(pkg, sep)
	parts := make([]string, 0, len(rawParts))
	for _, part := range rawParts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		parts = append(parts, part)
	}
	return parts
}

func suffixEligibleSymbolRef(lang Lang, pkg, name string) bool {
	pkg = strings.TrimSpace(pkg)
	name = strings.TrimSpace(name)
	if pkg == "" || name == "" {
		return false
	}
	return len(suffixPackageParts(lang, pkg)) >= defaultCallEdgeSuffixPolicy.minQualifiedPkgSegments
}

func qualifiedPackageSuffixes(parts []string, sep string, policy callEdgeSuffixPolicy) []string {
	if len(parts) <= policy.minQualifiedPkgSegments {
		return nil
	}
	limit := policy.maxKeysPerSeed
	if limit <= 0 {
		limit = len(parts)
	}
	suffixes := make([]string, 0, min(len(parts)-1, limit))
	for i := 1; i < len(parts); i++ {
		suffixParts := parts[i:]
		if len(suffixParts) < policy.minQualifiedPkgSegments {
			continue
		}
		suffixes = append(suffixes, strings.Join(suffixParts, sep))
		if len(suffixes) >= limit {
			break
		}
	}
	return suffixes
}

func callEdgeSuffixKeysForSeed(lang Lang, fqn string) []string {
	pkg, name := splitFQN(fqn)
	pkg = strings.TrimSpace(pkg)
	name = strings.TrimSpace(name)
	if pkg == "" || name == "" {
		return nil
	}

	sep, ok := suffixPackageSeparator(lang, pkg)
	if !ok {
		return nil
	}
	pkgSuffixes := qualifiedPackageSuffixes(suffixPackageParts(lang, pkg), sep, defaultCallEdgeSuffixPolicy)

	keys := make([]string, 0, len(pkgSuffixes))
	seen := make(map[string]struct{}, len(pkgSuffixes))
	fullFQN := normalizeSymbol(SymbolRef{Lang: lang, Pkg: pkg, Name: name})
	for _, suffixPkg := range pkgSuffixes {
		key := normalizeSymbol(SymbolRef{Lang: lang, Pkg: suffixPkg, Name: name})
		if key == "" || key == fullFQN || key == name {
			continue
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		keys = append(keys, key)
	}
	return keys
}

func buildCallEdgeSuffixIndex(seeds []CallEdgeSuffixSeed) *callEdgeSuffixIndex {
	if len(seeds) == 0 {
		return nil
	}
	byKey := make(map[string]map[string]struct{})
	for _, seed := range seeds {
		if strings.TrimSpace(seed.AnchorID) == "" || strings.TrimSpace(seed.FQN) == "" {
			continue
		}
		for _, suffix := range callEdgeSuffixKeysForSeed(seed.Lang, seed.FQN) {
			key := callEdgeCacheLangFQNKey(seed.Lang, suffix)
			if byKey[key] == nil {
				byKey[key] = make(map[string]struct{})
			}
			byKey[key][seed.AnchorID] = struct{}{}
		}
	}
	if len(byKey) == 0 {
		return nil
	}
	out := &callEdgeSuffixIndex{idsByLangSuffix: make(map[string][]string, len(byKey))}
	for key, ids := range byKey {
		list := make([]string, 0, len(ids))
		for id := range ids {
			list = append(list, id)
		}
		sort.Strings(list)
		out.idsByLangSuffix[key] = list
	}
	return out
}

func (i *callEdgeSuffixIndex) lookup(lang Lang, fqn string) []string {
	if i == nil || len(i.idsByLangSuffix) == 0 || strings.TrimSpace(fqn) == "" {
		return nil
	}
	return i.idsByLangSuffix[callEdgeCacheLangFQNKey(lang, fqn)]
}

func callEdgeSummaryFromSources(item callEdgeRebuildItem, sources callEdgeRebuildSources) (FileSummary, []IntelAnchor, bool) {
	symRefs := sources.symbolRefsByPath[item.path]
	impRefs := sources.importRefsByPath[item.path]
	modDefs := sources.moduleDefsByPath[item.path]
	anchors := []IntelAnchor(nil)
	hasAuthoritativeInputs := false
	if runtimeSrc, ok := sources.runtimeByPath[item.path]; ok {
		symRefs = runtimeSrc.SymbolRefs
		impRefs = runtimeSrc.ImportRefs
		modDefs = runtimeSrc.ModuleDefs
		anchors = runtimeSrc.IntelAnchors
		hasAuthoritativeInputs = runtimeSrc.HasRebuildInputs
	}
	if len(symRefs) == 0 && len(impRefs) == 0 && len(modDefs) == 0 && !hasAuthoritativeInputs {
		return FileSummary{}, nil, false
	}
	intelPath := callEdgeCachePath(item.path)
	if len(anchors) == 0 {
		anchors = append([]IntelAnchor(nil), sources.anchorsByPath[intelPath]...)
	}
	return summaryFromRefs(item.lang, intelPath, symRefs, impRefs, modDefs), anchors, true
}

func inferGoImportPath(summary FileSummary) string {
	for _, sym := range summary.Symbols {
		pkg := strings.TrimSpace(sym.Pkg)
		if pkg == "" {
			continue
		}
		if slash := strings.LastIndex(pkg, "/"); slash >= 0 {
			if dotAfterSlash := strings.Index(pkg[slash:], "."); dotAfterSlash >= 0 {
				pkg = pkg[:slash+dotAfterSlash]
			}
		}
		if strings.TrimSpace(pkg) != "" {
			return pkg
		}
	}
	return ""
}

func (s *Service) buildCallEdgeRebuildCache(ctx context.Context, items []callEdgeRebuildItem, sources callEdgeRebuildSources) (*callEdgeRebuildCache, error) {
	cacheBuildStarted := time.Now()
	defer func() {
		indexingperf.ObserveLatency(ctx, "calledge.cache_build", time.Since(cacheBuildStarted))
	}()
	cache := &callEdgeRebuildCache{
		moduleIDByPath:      make(map[string]string, len(items)),
		ownerIDsByPath:      make(map[string]map[string]string, len(items)),
		targetIDsByLangFQN:  make(map[string][]string),
		importTargetIDs:     make(map[string]string),
		goMethodIDs:         make(map[string][]string),
		pyFallbackIDsByCall: make(map[string][]string),
	}

	exactResolver, _ := s.store.(intelExactCallResolver)
	resolver, _ := s.store.(intelCallResolver)
	methodResolver, _ := s.store.(intelGoMethodResolver)
	moduleSuffixResolver, _ := s.store.(intelModuleSuffixResolver)
	modulePathResolver, _ := s.store.(intelModulePathResolver)
	suffixSeedStore, _ := s.store.(intelCallEdgeSuffixSeedStore)
	anchorLookup := sources.anchorLookup

	exactCandidatesByLang := make(map[Lang]map[string]struct{})
	suffixCandidatesByLang := make(map[Lang]map[string]struct{})
	suffixCallCandidatesByLang := make(map[Lang]map[string]struct{})
	suffixTypeRefCandidatesByLang := make(map[Lang]map[string]struct{})
	suffixMemberCandidatesByLang := make(map[Lang]map[string]struct{})
	importsByLang := make(map[Lang]map[string]struct{})
	goMethodsByPkg := make(map[string]map[string]struct{})
	pyCallerDirsByName := make(map[string]map[string]struct{})
	pyUnqualifiedFQNSkipped := 0

	for _, item := range items {
		summary, anchors, ok := callEdgeSummaryFromSources(item, sources)
		if !ok {
			continue
		}
		intelPath := callEdgeCachePath(item.path)
		moduleID := ""
		ownerByFQN := make(map[string]string)
		for _, a := range anchors {
			if strings.EqualFold(a.Kind, "module") && moduleID == "" {
				moduleID = a.AnchorID
			}
			if strings.TrimSpace(a.FQN) != "" {
				ownerByFQN[a.FQN] = a.AnchorID
			}
		}
		if moduleID == "" {
			moduleID = intelAnchorID(item.lang, "module", intelPath, "module")
		}
		cache.moduleIDByPath[intelPath] = moduleID
		cache.ownerIDsByPath[intelPath] = ownerByFQN

		goPkgImportPath := ""
		if item.lang == LangGo && methodResolver != nil {
			goPkgImportPath = inferGoImportPath(summary)
		}
		pyCallerDir := pathpkg.Dir(filepath.ToSlash(intelPath))
		if pyCallerDir == "." {
			pyCallerDir = ""
		}

		for _, call := range summary.Calls {
			name := strings.TrimSpace(call.CalleeSymbol.Name)
			if name == "" {
				continue
			}
			callLang := call.CalleeSymbol.Lang
			if callLang == "" {
				callLang = item.lang
			}
			if callLang == LangGo && strings.TrimSpace(call.CalleeSymbol.Pkg) == "" && methodResolver != nil && goPkgImportPath != "" {
				if isCommonGoMethodName(name) {
					continue
				}
				if len(name) > 0 && name[0] >= 'A' && name[0] <= 'Z' {
					if goMethodsByPkg[goPkgImportPath] == nil {
						goMethodsByPkg[goPkgImportPath] = make(map[string]struct{})
					}
					goMethodsByPkg[goPkgImportPath][name] = struct{}{}
				}
				addCallEdgeSuffixSkip(ctx, "go_method_only")
				continue
			}
			if callLang == LangPy {
				if pyCallerDirsByName[name] == nil {
					pyCallerDirsByName[name] = make(map[string]struct{})
				}
				pyCallerDirsByName[name][pyCallerDir] = struct{}{}
				if shouldUsePythonFallbackOnly(callLang, call.CalleeSymbol.Pkg, name) {
					// Bare Python names almost always resolve via same-dir symbol lookup;
					// pushing them through the FQN resolver just creates miss storms.
					pyUnqualifiedFQNSkipped++
					addCallEdgeSuffixSkip(ctx, "py_fallback_only")
					continue
				}
			}
			fqn := normalizeSymbol(SymbolRef{Lang: callLang, Pkg: call.CalleeSymbol.Pkg, Name: name, Member: call.CalleeSymbol.Member})
			if fqn == "" {
				continue
			}
			if exactCandidatesByLang[callLang] == nil {
				exactCandidatesByLang[callLang] = make(map[string]struct{})
			}
			exactCandidatesByLang[callLang][fqn] = struct{}{}
			switch {
			case strings.TrimSpace(call.CalleeSymbol.Pkg) == "":
				addCallEdgeSuffixSkip(ctx, "bare_name")
			case !suffixEligibleSymbolRef(callLang, call.CalleeSymbol.Pkg, name):
				addCallEdgeSuffixSkip(ctx, "unqualified_pkg")
			default:
				if suffixCandidatesByLang[callLang] == nil {
					suffixCandidatesByLang[callLang] = make(map[string]struct{})
				}
				if suffixCallCandidatesByLang[callLang] == nil {
					suffixCallCandidatesByLang[callLang] = make(map[string]struct{})
				}
				suffixCandidatesByLang[callLang][fqn] = struct{}{}
				suffixCallCandidatesByLang[callLang][fqn] = struct{}{}
			}
		}

		for _, tr := range summary.TypeRefs {
			name := strings.TrimSpace(tr.TypeSym.Name)
			if name == "" {
				continue
			}
			typeLang := tr.TypeSym.Lang
			if typeLang == "" {
				typeLang = item.lang
			}
			fqn := normalizeSymbol(SymbolRef{Lang: typeLang, Pkg: tr.TypeSym.Pkg, Name: name})
			if fqn == "" {
				continue
			}
			if exactCandidatesByLang[typeLang] == nil {
				exactCandidatesByLang[typeLang] = make(map[string]struct{})
			}
			exactCandidatesByLang[typeLang][fqn] = struct{}{}
			switch {
			case strings.TrimSpace(tr.TypeSym.Pkg) == "":
				addCallEdgeSuffixSkip(ctx, "bare_name")
			case !suffixEligibleSymbolRef(typeLang, tr.TypeSym.Pkg, name):
				addCallEdgeSuffixSkip(ctx, "unqualified_pkg")
			default:
				if suffixCandidatesByLang[typeLang] == nil {
					suffixCandidatesByLang[typeLang] = make(map[string]struct{})
				}
				if suffixTypeRefCandidatesByLang[typeLang] == nil {
					suffixTypeRefCandidatesByLang[typeLang] = make(map[string]struct{})
				}
				suffixCandidatesByLang[typeLang][fqn] = struct{}{}
				suffixTypeRefCandidatesByLang[typeLang][fqn] = struct{}{}
			}
		}

		for _, mr := range summary.MemberRefs {
			name := strings.TrimSpace(mr.Sym.Name)
			if name == "" {
				continue
			}
			memberLang := mr.Sym.Lang
			if memberLang == "" {
				memberLang = item.lang
			}
			// MemberRefs reference class members by definition; Member=true makes
			// normalizeSymbol use `::` so PHP method/static FQNs match the stored form.
			fqn := normalizeSymbol(SymbolRef{Lang: memberLang, Pkg: mr.Sym.Pkg, Name: name, Member: true})
			if fqn == "" {
				continue
			}
			if exactCandidatesByLang[memberLang] == nil {
				exactCandidatesByLang[memberLang] = make(map[string]struct{})
			}
			exactCandidatesByLang[memberLang][fqn] = struct{}{}
			switch {
			case strings.TrimSpace(mr.Sym.Pkg) == "":
				addCallEdgeSuffixSkip(ctx, "bare_name")
			case !suffixEligibleSymbolRef(memberLang, mr.Sym.Pkg, name):
				addCallEdgeSuffixSkip(ctx, "unqualified_pkg")
			default:
				if suffixCandidatesByLang[memberLang] == nil {
					suffixCandidatesByLang[memberLang] = make(map[string]struct{})
				}
				if suffixMemberCandidatesByLang[memberLang] == nil {
					suffixMemberCandidatesByLang[memberLang] = make(map[string]struct{})
				}
				suffixCandidatesByLang[memberLang][fqn] = struct{}{}
				suffixMemberCandidatesByLang[memberLang][fqn] = struct{}{}
			}
		}

		for _, imp := range summary.Imports {
			module := strings.TrimSpace(imp.Module)
			if module == "" {
				continue
			}
			if importsByLang[item.lang] == nil {
				importsByLang[item.lang] = make(map[string]struct{})
			}
			importsByLang[item.lang][module] = struct{}{}
		}
	}

	fqnCandidates := 0
	for _, fqnSet := range exactCandidatesByLang {
		fqnCandidates += len(fqnSet)
	}
	importCandidates := 0
	for _, moduleSet := range importsByLang {
		importCandidates += len(moduleSet)
	}
	goMethodCandidates := 0
	for _, methodSet := range goMethodsByPkg {
		goMethodCandidates += len(methodSet)
	}
	pyFallbackCandidates := 0
	for _, callerDirs := range pyCallerDirsByName {
		pyFallbackCandidates += len(callerDirs)
	}
	addCallEdgeCacheEntries(ctx, "calledge.cache_build.fqn_candidate.count", fqnCandidates)
	addCallEdgeCacheEntries(ctx, "calledge.cache_build.import_candidate.count", importCandidates)
	addCallEdgeCacheEntries(ctx, "calledge.cache_build.go_method_candidate.count", goMethodCandidates)
	addCallEdgeCacheEntries(ctx, "calledge.cache_build.py_fallback_candidate.count", pyFallbackCandidates)
	addCallEdgeCacheEntries(ctx, "calledge.cache_build.fqn_skipped_py_unqualified.count", pyUnqualifiedFQNSkipped)
	for lang, candidates := range suffixCandidatesByLang {
		addCallEdgeSuffixCandidateLang(ctx, lang, len(candidates))
	}
	for _, candidates := range suffixCallCandidatesByLang {
		addCallEdgeSuffixCandidateKind(ctx, "call", len(candidates))
	}
	for _, candidates := range suffixTypeRefCandidatesByLang {
		addCallEdgeSuffixCandidateKind(ctx, "typeref", len(candidates))
	}
	for _, candidates := range suffixMemberCandidatesByLang {
		addCallEdgeSuffixCandidateKind(ctx, "memberref", len(candidates))
	}

	totalFQNLookupStarted := time.Now()
	if exactResolver != nil {
		for lang, fqnSet := range exactCandidatesByLang {
			if len(fqnSet) == 0 {
				continue
			}
			fqns := make([]string, 0, len(fqnSet))
			for fqn := range fqnSet {
				fqns = append(fqns, fqn)
			}
			sort.Strings(fqns)
			idsByFQN, err := exactResolver.IntelAnchorIDsByExactFQNsAndLang(ctx, string(lang), fqns)
			if err != nil {
				continue
			}
			for fqn, ids := range idsByFQN {
				cache.targetIDsByLangFQN[callEdgeCacheLangFQNKey(lang, fqn)] = append([]string(nil), ids...)
			}
		}
	} else if resolver != nil {
		for lang, fqnSet := range exactCandidatesByLang {
			if len(fqnSet) == 0 {
				continue
			}
			fqns := make([]string, 0, len(fqnSet))
			for fqn := range fqnSet {
				fqns = append(fqns, fqn)
			}
			sort.Strings(fqns)
			idsByFQN, err := resolver.IntelAnchorIDsByFQNsAndLang(ctx, string(lang), fqns)
			if err != nil {
				continue
			}
			for fqn, ids := range idsByFQN {
				cache.targetIDsByLangFQN[callEdgeCacheLangFQNKey(lang, fqn)] = append([]string(nil), ids...)
			}
		}
	}
	unresolvedSuffixByLang := make(map[Lang]map[string]struct{})
	suffixRequested := 0
	for lang, fqnSet := range suffixCandidatesByLang {
		for fqn := range fqnSet {
			if len(cache.targetIDsByLangFQN[callEdgeCacheLangFQNKey(lang, fqn)]) > 0 {
				continue
			}
			if unresolvedSuffixByLang[lang] == nil {
				unresolvedSuffixByLang[lang] = make(map[string]struct{})
			}
			unresolvedSuffixByLang[lang][fqn] = struct{}{}
			suffixRequested++
		}
	}
	addCallEdgeCacheEntries(ctx, "calledge.cache_build.fqn_lookup.suffix_requested.count", suffixRequested)

	if suffixSeedStore != nil && suffixRequested > 0 {
		suffixLangs := make([]Lang, 0, len(unresolvedSuffixByLang))
		for lang, fqnSet := range unresolvedSuffixByLang {
			if len(fqnSet) == 0 {
				continue
			}
			suffixLangs = append(suffixLangs, lang)
		}
		sort.Slice(suffixLangs, func(i, j int) bool { return suffixLangs[i] < suffixLangs[j] })
		seeds, err := suffixSeedStore.CallEdgeSuffixSeedsByLangs(ctx, suffixLangs)
		if err == nil {
			addCallEdgeCacheEntries(ctx, "calledge.cache_build_suffix.seed.count", len(seeds))
			suffixIndex := buildCallEdgeSuffixIndex(seeds)
			if suffixIndex != nil {
				addCallEdgeCacheEntries(ctx, "calledge.cache_build_suffix.index_entry.count", len(suffixIndex.idsByLangSuffix))
				suffixLookupStarted := time.Now()
				suffixResolved := 0
				for _, lang := range suffixLangs {
					fqns := make([]string, 0, len(unresolvedSuffixByLang[lang]))
					for fqn := range unresolvedSuffixByLang[lang] {
						fqns = append(fqns, fqn)
					}
					sort.Strings(fqns)
					for _, fqn := range fqns {
						ids := suffixIndex.lookup(lang, fqn)
						if len(ids) == 0 {
							continue
						}
						cache.targetIDsByLangFQN[callEdgeCacheLangFQNKey(lang, fqn)] = append([]string(nil), ids...)
						suffixResolved++
					}
				}
				indexingperf.ObserveLatency(ctx, "calledge.cache_build.fqn_lookup.suffix", time.Since(suffixLookupStarted))
				addCallEdgeCacheEntries(ctx, "calledge.cache_build.fqn_lookup.suffix_resolved.count", suffixResolved)
			}
		}
	}
	if exactResolver != nil || resolver != nil || suffixSeedStore != nil {
		indexingperf.ObserveLatency(ctx, "calledge.cache_build.fqn_lookup", time.Since(totalFQNLookupStarted))
	}

	if methodResolver != nil {
		goMethodLookupStarted := time.Now()
		pkgs := make([]string, 0, len(goMethodsByPkg))
		for pkgImportPath := range goMethodsByPkg {
			pkgs = append(pkgs, pkgImportPath)
		}
		sort.Strings(pkgs)
		for _, pkgImportPath := range pkgs {
			methods := make([]string, 0, len(goMethodsByPkg[pkgImportPath]))
			for method := range goMethodsByPkg[pkgImportPath] {
				methods = append(methods, method)
			}
			sort.Strings(methods)
			for _, method := range methods {
				ids, err := methodResolver.IntelAnchorIDsByGoMethodNameInPkg(ctx, pkgImportPath, method, 20)
				if err != nil || len(ids) == 0 {
					continue
				}
				cache.goMethodIDs[callEdgeCacheGoMethodKey(pkgImportPath, method)] = append([]string(nil), ids...)
			}
		}
		indexingperf.ObserveLatency(ctx, "calledge.cache_build.go_method_lookup", time.Since(goMethodLookupStarted))
	}

	importLookupStarted := time.Now()
	for lang, moduleSet := range importsByLang {
		if len(moduleSet) == 0 {
			continue
		}
		modules := make([]string, 0, len(moduleSet))
		for module := range moduleSet {
			modules = append(modules, module)
		}
		sort.Strings(modules)
		switch lang {
		case LangPy, LangCs:
			if moduleSuffixResolver == nil {
				continue
			}
			targetIDs, err := moduleSuffixResolver.ModuleAnchorIDsByModuleSuffix(ctx, modules)
			if err != nil {
				continue
			}
			for module, targetID := range targetIDs {
				if strings.TrimSpace(targetID) == "" {
					continue
				}
				cache.importTargetIDs[callEdgeCacheImportKey(lang, module)] = targetID
			}
		case LangTS:
			if modulePathResolver == nil {
				continue
			}
			absToRel := make(map[string]string, len(modules))
			relPaths := make([]string, 0, len(modules))
			for _, absPath := range modules {
				relPath, err := s.vault.RelCodeStrict(absPath)
				if err != nil {
					relPath = paths.NormalizeCode(absPath)
				}
				relStr := string(relPath)
				if relStr == "" {
					continue
				}
				absToRel[absPath] = relStr
				relPaths = append(relPaths, relStr)
			}
			if len(relPaths) == 0 {
				continue
			}
			pathToID, err := modulePathResolver.ModuleAnchorIDsByPaths(ctx, relPaths)
			if err != nil {
				continue
			}
			for absPath, relPath := range absToRel {
				if id := strings.TrimSpace(pathToID[relPath]); id != "" {
					cache.importTargetIDs[callEdgeCacheImportKey(lang, absPath)] = id
				}
			}
		}
	}
	indexingperf.ObserveLatency(ctx, "calledge.cache_build.import_lookup", time.Since(importLookupStarted))

	if anchorLookup != nil {
		pyFallbackLookupStarted := time.Now()
		names := make([]string, 0, len(pyCallerDirsByName))
		for name := range pyCallerDirsByName {
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			anchors, err := anchorLookup.IntelAnchorsBySymbol(ctx, name, 25)
			if err != nil {
				continue
			}
			for callerDir := range pyCallerDirsByName[name] {
				filtered := make([]string, 0, len(anchors))
				for _, a := range anchors {
					if a.Lang != LangPy || strings.EqualFold(a.Kind, "module") {
						continue
					}
					aPath := filepath.ToSlash(a.Path)
					if callerDir != "" {
						if pathpkg.Dir(aPath) != callerDir {
							continue
						}
					} else if pathpkg.Dir(aPath) != "." {
						continue
					}
					filtered = append(filtered, a.AnchorID)
				}
				cache.pyFallbackIDsByCall[callEdgeCachePyFallbackKey(callerDir, name)] = filtered
			}
		}
		indexingperf.ObserveLatency(ctx, "calledge.cache_build.py_fallback_lookup", time.Since(pyFallbackLookupStarted))
	}

	addCallEdgeCacheEntries(ctx, "calledge.cache.fqn.entry", len(cache.targetIDsByLangFQN))
	addCallEdgeCacheEntries(ctx, "calledge.cache.import.entry", len(cache.importTargetIDs))
	addCallEdgeCacheEntries(ctx, "calledge.cache.go_method.entry", len(cache.goMethodIDs))
	addCallEdgeCacheEntries(ctx, "calledge.cache.py_fallback.entry", len(cache.pyFallbackIDsByCall))

	return cache, nil
}

func (s *Service) buildCallEdgesFromStoredRefsWithCache(ctx context.Context, item callEdgeRebuildItem, summary FileSummary, cache *callEdgeRebuildCache) []IntelEdge {
	intelPath := callEdgeCachePath(item.path)
	moduleID := cache.moduleIDByPath[intelPath]
	ownerByFQN := cache.ownerIDsByPath[intelPath]
	if moduleID == "" {
		moduleID = intelAnchorID(item.lang, "module", intelPath, "module")
	}

	type edgeKey struct {
		src  string
		dst  string
		kind string
	}
	edgesByKey := make(map[edgeKey]struct{})
	includeTests := IsTestPath(intelPath)
	goPkgImportPath := ""
	if item.lang == LangGo {
		goPkgImportPath = inferGoImportPath(summary)
	}
	pyCallerDir := pathpkg.Dir(filepath.ToSlash(intelPath))
	if pyCallerDir == "." {
		pyCallerDir = ""
	}
	fqnTargetsByKey := make(map[string][]string)
	importTargetsByKey := make(map[string]string)
	goMethodTargetsByKey := make(map[string][]string)
	pyFallbackTargetsByKey := make(map[string][]string)
	normalizedFQNKeys := make(map[string]string)
	importKeys := make(map[string]string)

	normalizedFQNKey := func(lang Lang, pkg, name string, member bool) string {
		memberTag := "0"
		if member {
			memberTag = "1"
		}
		rawKey := strings.ToLower(string(lang)) + "|" + pkg + "|" + name + "|" + memberTag
		if cached, ok := normalizedFQNKeys[rawKey]; ok {
			return cached
		}
		key := ""
		if fqn := normalizeSymbol(SymbolRef{Lang: lang, Pkg: pkg, Name: name, Member: member}); fqn != "" {
			key = callEdgeCacheLangFQNKey(lang, fqn)
		}
		normalizedFQNKeys[rawKey] = key
		return key
	}
	importKey := func(lang Lang, module string) string {
		rawKey := strings.ToLower(string(lang)) + "|" + module
		if cached, ok := importKeys[rawKey]; ok {
			return cached
		}
		key := callEdgeCacheImportKey(lang, module)
		importKeys[rawKey] = key
		return key
	}

	lookupFQNTargets := func(key string) []string {
		if key == "" {
			return nil
		}
		if cached, ok := fqnTargetsByKey[key]; ok {
			return cached
		}
		dstIDs := cache.targetIDsByLangFQN[key]
		addCallEdgeCacheLookup(ctx, "calledge.cache.fqn", len(dstIDs) > 0)
		fqnTargetsByKey[key] = dstIDs
		return dstIDs
	}
	lookupImportTarget := func(key string) string {
		if key == "" {
			return ""
		}
		if cached, ok := importTargetsByKey[key]; ok {
			return cached
		}
		targetID := cache.importTargetIDs[key]
		addCallEdgeCacheLookup(ctx, "calledge.cache.import", targetID != "")
		importTargetsByKey[key] = targetID
		return targetID
	}
	lookupGoMethodTargets := func(key string) []string {
		if key == "" {
			return nil
		}
		if cached, ok := goMethodTargetsByKey[key]; ok {
			return cached
		}
		dstIDs := cache.goMethodIDs[key]
		addCallEdgeCacheLookup(ctx, "calledge.cache.go_method", len(dstIDs) > 0)
		goMethodTargetsByKey[key] = dstIDs
		return dstIDs
	}
	lookupPyFallbackTargets := func(key string) []string {
		if key == "" {
			return nil
		}
		if cached, ok := pyFallbackTargetsByKey[key]; ok {
			return cached
		}
		dstIDs := cache.pyFallbackIDsByCall[key]
		addCallEdgeCacheLookup(ctx, "calledge.cache.py_fallback", len(dstIDs) > 0)
		pyFallbackTargetsByKey[key] = dstIDs
		return dstIDs
	}

	for _, call := range summary.Calls {
		name := strings.TrimSpace(call.CalleeSymbol.Name)
		if name == "" {
			continue
		}
		callLang := call.CalleeSymbol.Lang
		if callLang == "" {
			callLang = item.lang
		}
		var dstIDs []string
		switch {
		case callLang == LangGo && strings.TrimSpace(call.CalleeSymbol.Pkg) == "" && goPkgImportPath != "":
			dstIDs = lookupGoMethodTargets(callEdgeCacheGoMethodKey(goPkgImportPath, name))
		case callLang == LangPy:
			if shouldUsePythonFallbackOnly(callLang, call.CalleeSymbol.Pkg, name) {
				dstIDs = lookupPyFallbackTargets(callEdgeCachePyFallbackKey(pyCallerDir, name))
			} else {
				dstIDs = lookupFQNTargets(normalizedFQNKey(callLang, call.CalleeSymbol.Pkg, name, call.CalleeSymbol.Member))
				if len(dstIDs) == 0 && !strings.EqualFold(strings.TrimSpace(call.CalleeSymbol.Pkg), "builtins") {
					dstIDs = lookupPyFallbackTargets(callEdgeCachePyFallbackKey(pyCallerDir, name))
				}
			}
		default:
			dstIDs = lookupFQNTargets(normalizedFQNKey(callLang, call.CalleeSymbol.Pkg, name, call.CalleeSymbol.Member))
		}
		if len(dstIDs) == 0 {
			continue
		}
		srcID := moduleID
		if owner := strings.TrimSpace(call.OwnerFQN); owner != "" {
			if id := ownerByFQN[owner]; id != "" {
				srcID = id
			}
		}
		if strings.TrimSpace(srcID) == "" {
			continue
		}
		for _, dstID := range dstIDs {
			edgesByKey[edgeKey{src: srcID, dst: dstID, kind: "calls"}] = struct{}{}
			if includeTests {
				edgesByKey[edgeKey{src: srcID, dst: dstID, kind: "tests"}] = struct{}{}
			}
		}
	}

	for _, imp := range summary.Imports {
		module := strings.TrimSpace(imp.Module)
		if module == "" {
			continue
		}
		targetID := lookupImportTarget(importKey(item.lang, module))
		if targetID == "" || targetID == moduleID {
			continue
		}
		edgesByKey[edgeKey{src: moduleID, dst: targetID, kind: "imports"}] = struct{}{}
	}

	for _, tr := range summary.TypeRefs {
		name := strings.TrimSpace(tr.TypeSym.Name)
		if name == "" {
			continue
		}
		typeLang := tr.TypeSym.Lang
		if typeLang == "" {
			typeLang = item.lang
		}
		// TypeRefs reference types (classes/interfaces/traits), not members.
		dstIDs := lookupFQNTargets(normalizedFQNKey(typeLang, tr.TypeSym.Pkg, name, false))
		if len(dstIDs) == 0 {
			continue
		}
		srcID := moduleID
		if owner := strings.TrimSpace(tr.OwnerFQN); owner != "" {
			if id := ownerByFQN[owner]; id != "" {
				srcID = id
			}
		}
		if strings.TrimSpace(srcID) == "" {
			continue
		}
		for _, dstID := range dstIDs {
			edgesByKey[edgeKey{src: srcID, dst: dstID, kind: "type_ref"}] = struct{}{}
		}
	}

	for _, mr := range summary.MemberRefs {
		name := strings.TrimSpace(mr.Sym.Name)
		if name == "" {
			continue
		}
		memberLang := mr.Sym.Lang
		if memberLang == "" {
			memberLang = item.lang
		}
		// MemberRefs reference class members by definition.
		dstIDs := lookupFQNTargets(normalizedFQNKey(memberLang, mr.Sym.Pkg, name, true))
		if len(dstIDs) == 0 {
			continue
		}
		srcID := moduleID
		if owner := strings.TrimSpace(mr.OwnerFQN); owner != "" {
			if id := ownerByFQN[owner]; id != "" {
				srcID = id
			}
		}
		if strings.TrimSpace(srcID) == "" {
			continue
		}
		for _, dstID := range dstIDs {
			edgesByKey[edgeKey{src: srcID, dst: dstID, kind: "member_ref"}] = struct{}{}
		}
	}

	if len(edgesByKey) == 0 {
		return nil
	}
	edges := make([]IntelEdge, 0, len(edgesByKey))
	for key := range edgesByKey {
		edges = append(edges, IntelEdge{SrcID: key.src, DstID: key.dst, Kind: key.kind})
	}
	sort.Slice(edges, func(i, j int) bool {
		if edges[i].Kind != edges[j].Kind {
			return edges[i].Kind < edges[j].Kind
		}
		if edges[i].SrcID != edges[j].SrcID {
			return edges[i].SrcID < edges[j].SrcID
		}
		return edges[i].DstID < edges[j].DstID
	})
	return edges
}

func (s *Service) writeCallEdgeBatches(ctx context.Context, intel intelStore, batches []CallEdgesBatch) error {
	if len(batches) == 0 {
		return nil
	}
	writeStarted := time.Now()
	defer func() {
		indexingperf.ObserveLatency(ctx, "calledge.batch_write", time.Since(writeStarted))
	}()
	if batcher, ok := s.store.(intelCallEdgesBatchStore); ok {
		return batcher.UpsertIntelCallEdgesForPathsBatch(ctx, batches)
	}
	for _, batch := range batches {
		if err := intel.UpsertIntelCallEdgesForPath(ctx, batch.Path, batch.Edges); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) loadCallEdgeRebuildSources(ctx context.Context, items []callEdgeRebuildItem) (callEdgeRebuildSources, error) {
	loadStarted := time.Now()
	refStore, hasRefStore := s.store.(intelRefStore)
	anchorLookup, hasAnchorLookup := s.store.(intelMentionLookup)
	sources := callEdgeRebuildSources{
		canUseRefs:   hasRefStore && hasAnchorLookup,
		anchorLookup: anchorLookup,
		runtimeByPath: s.runtimeCodeSourcesByPath(func() []string {
			pathsList := make([]string, 0, len(items))
			for _, item := range items {
				if path := strings.TrimSpace(item.path); path != "" {
					pathsList = append(pathsList, path)
				}
			}
			return pathsList
		}()),
	}
	if len(sources.runtimeByPath) > 0 {
		sources.canUseRefs = hasAnchorLookup
	}
	if !sources.canUseRefs || len(items) == 0 {
		indexingperf.ObserveLatency(ctx, "calledge.source_load", time.Since(loadStarted))
		return sources, nil
	}
	var err error
	pathsList := make([]string, 0, len(items))
	for _, item := range items {
		if path := strings.TrimSpace(item.path); path != "" {
			if _, ok := sources.runtimeByPath[path]; ok {
				continue
			}
			pathsList = append(pathsList, path)
		}
	}
	if len(pathsList) == 0 {
		if len(sources.runtimeByPath) == 0 {
			sources.canUseRefs = false
		}
		indexingperf.ObserveLatency(ctx, "calledge.source_load", time.Since(loadStarted))
		if sources.canUseRefs {
			sources.cache, err = s.buildCallEdgeRebuildCache(ctx, items, sources)
			if err != nil {
				return sources, fmt.Errorf("build call-edge rebuild cache: %w", err)
			}
		}
		return sources, nil
	}
	refsStarted := time.Now()
	sources.symbolRefsByPath, err = refStore.SymbolRefsByPaths(ctx, pathsList)
	if err != nil {
		return sources, fmt.Errorf("load symbol refs for call-edge rebuild: %w", err)
	}
	sources.importRefsByPath, err = refStore.ImportRefsByPaths(ctx, pathsList)
	if err != nil {
		return sources, fmt.Errorf("load import refs for call-edge rebuild: %w", err)
	}
	sources.moduleDefsByPath, err = refStore.ModuleDefsByPaths(ctx, pathsList)
	if err != nil {
		return sources, fmt.Errorf("load module defs for call-edge rebuild: %w", err)
	}
	indexingperf.ObserveLatency(ctx, "calledge.refs_load", time.Since(refsStarted))
	anchorStarted := time.Now()
	sources.anchorsByPath, err = anchorLookup.IntelAnchorsByPaths(ctx, pathsList)
	if err != nil {
		return sources, fmt.Errorf("load anchors for call-edge rebuild: %w", err)
	}
	indexingperf.ObserveLatency(ctx, "calledge.anchor_prefetch", time.Since(anchorStarted))
	indexingperf.ObserveLatency(ctx, "calledge.source_load", time.Since(loadStarted))
	sources.cache, err = s.buildCallEdgeRebuildCache(ctx, items, sources)
	if err != nil {
		return sources, fmt.Errorf("build call-edge rebuild cache: %w", err)
	}
	return sources, nil
}

func (s *Service) buildCallEdgesFromStoredRefs(ctx context.Context, item callEdgeRebuildItem, sources callEdgeRebuildSources) ([]IntelEdge, bool, error) {
	if !sources.canUseRefs || sources.anchorLookup == nil {
		return nil, false, fmt.Errorf("call-edge rebuild requires durable reverse-index rows")
	}
	symRefs := sources.symbolRefsByPath[item.path]
	impRefs := sources.importRefsByPath[item.path]
	modDefs := sources.moduleDefsByPath[item.path]
	anchors := []IntelAnchor(nil)
	hasAuthoritativeInputs := false
	runtimeSrc, hasRuntimeSource := sources.runtimeByPath[item.path]
	if hasRuntimeSource {
		symRefs = runtimeSrc.SymbolRefs
		impRefs = runtimeSrc.ImportRefs
		modDefs = runtimeSrc.ModuleDefs
		anchors = runtimeSrc.IntelAnchors
		hasAuthoritativeInputs = runtimeSrc.HasRebuildInputs
	}
	if len(symRefs) == 0 && len(impRefs) == 0 && len(modDefs) == 0 && !hasAuthoritativeInputs {
		if hasRuntimeSource {
			addCallEdgeFallbackReason(ctx, "runtime_parse_untrusted")
		} else {
			addCallEdgeFallbackReason(ctx, "no_authoritative_inputs")
		}
		return s.buildCallEdgesFromSourceFallback(ctx, item, sources)
	}
	if len(symRefs) == 0 && len(impRefs) == 0 && len(modDefs) == 0 && hasRuntimeSource {
		addCallEdgeFallbackReason(ctx, "runtime_missing_refs")
		return s.buildCallEdgesFromSourceFallback(ctx, item, sources)
	}
	if hasRuntimeSource {
		indexingperf.AddCount(ctx, "calledge.runtime_paths", 1)
	} else {
		indexingperf.AddCount(ctx, "calledge.stored_paths", 1)
	}
	intelPath := string(paths.NormalizeCode(item.path))
	if len(anchors) == 0 {
		anchors = append([]IntelAnchor(nil), sources.anchorsByPath[intelPath]...)
	}
	summary := summaryFromRefs(item.lang, intelPath, symRefs, impRefs, modDefs)
	if sources.cache != nil {
		buildStarted := time.Now()
		edges := s.buildCallEdgesFromStoredRefsWithCache(ctx, item, summary, sources.cache)
		indexingperf.ObserveLatency(ctx, "calledge.edge_build", time.Since(buildStarted))
		return edges, false, nil
	}
	buildStarted := time.Now()
	var edges []IntelEdge
	edges = append(edges, s.buildIntelCallEdges(ctx, item.lang, intelPath, summary, anchors)...)
	edges = append(edges, s.buildIntelImportEdges(ctx, item.lang, intelPath, summary, anchors)...)
	edges = append(edges, s.buildIntelTypeRefEdges(ctx, item.lang, intelPath, summary, anchors)...)
	edges = append(edges, s.buildIntelMemberRefEdges(ctx, item.lang, intelPath, summary, anchors)...)
	indexingperf.ObserveLatency(ctx, "calledge.edge_build", time.Since(buildStarted))
	return edges, false, nil
}

func (s *Service) buildCallEdgesFromSourceFallback(ctx context.Context, item callEdgeRebuildItem, sources callEdgeRebuildSources) ([]IntelEdge, bool, error) {
	indexingperf.AddCount(ctx, "calledge.source_fallback.count", 1)
	idx := s.indexers[item.lang]
	if idx == nil || sources.anchorLookup == nil {
		return nil, false, nil
	}
	absPath, err := s.absCodePath(item.path)
	if err != nil || strings.TrimSpace(absPath) == "" {
		return nil, false, nil
	}
	content, err := os.ReadFile(absPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, false, nil
		}
		return nil, false, fmt.Errorf("read %s for call-edge fallback rebuild: %w", item.path, err)
	}
	ref, err := s.codePathRef(item.path)
	if err != nil {
		return nil, false, err
	}
	summary, err := s.indexFileWithTimeout(ctx, idx, content, ref)
	if err != nil {
		return nil, false, err
	}
	if !summary.ParseStatus.TrustedForIndexing() {
		// Targeted rebuilds should mirror normal indexing semantics: parse-bad
		// fallback is best-effort, never destructive. Keep prior call edges until
		// a clean parse or durable reverse-index rows can replace them exactly.
		return nil, true, nil
	}
	normalizeSummaryPaths(&summary, item.path)
	intelPath := string(paths.NormalizeCode(item.path))
	anchors, err := sources.anchorLookup.IntelAnchorsByPath(ctx, intelPath)
	if err != nil {
		return nil, false, fmt.Errorf("load anchors for %s: %w", intelPath, err)
	}
	buildStarted := time.Now()
	var edges []IntelEdge
	edges = append(edges, s.buildIntelCallEdges(ctx, item.lang, intelPath, summary, anchors)...)
	edges = append(edges, s.buildIntelImportEdges(ctx, item.lang, intelPath, summary, anchors)...)
	edges = append(edges, s.buildIntelTypeRefEdges(ctx, item.lang, intelPath, summary, anchors)...)
	edges = append(edges, s.buildIntelMemberRefEdges(ctx, item.lang, intelPath, summary, anchors)...)
	indexingperf.ObserveLatency(ctx, "calledge.edge_build", time.Since(buildStarted))
	return edges, false, nil
}

func (s *Service) buildCallEdgesForItem(ctx context.Context, item callEdgeRebuildItem, sources callEdgeRebuildSources) (CallEdgesBatch, bool, error) {
	intelPath := string(paths.NormalizeCode(item.path))
	edges, preserveExisting, err := s.buildCallEdgesFromStoredRefs(ctx, item, sources)
	if err != nil {
		return CallEdgesBatch{}, false, err
	}
	if preserveExisting {
		return CallEdgesBatch{}, true, nil
	}
	return CallEdgesBatch{Path: intelPath, Edges: edges}, false, nil
}
