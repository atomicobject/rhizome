package codeanchor

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	pathpkg "path"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/atomicobject/rhizome/pkg/codefile"
	"github.com/atomicobject/rhizome/pkg/indexingperf"
	"github.com/atomicobject/rhizome/pkg/paths"
)

const callEdgeReferrerLookupChunkSize = 128

func (s *Service) updateIntelCallEdgesOnly(ctx context.Context, lang Lang, path string, summary FileSummary) {
	intel, ok := s.store.(intelStore)
	if !ok {
		return
	}
	intelPath := string(paths.NormalizeCode(path))
	anchors, anchorEdges, ftsRows := extractIntelCodeFromSummary(intelPath, lang, summary)
	callEdges := make([]IntelEdge, 0, len(summary.Calls)+len(summary.Imports)+len(summary.TypeRefs)+len(summary.MemberRefs))
	callEdges = append(callEdges, s.buildIntelCallEdges(ctx, lang, intelPath, summary, anchors)...)
	callEdges = append(callEdges, s.buildIntelImportEdges(ctx, lang, intelPath, summary, anchors)...)
	callEdges = append(callEdges, s.buildIntelTypeRefEdges(ctx, lang, intelPath, summary, anchors)...)
	callEdges = append(callEdges, s.buildIntelMemberRefEdges(ctx, lang, intelPath, summary, anchors)...)
	if len(callEdges) == 0 {
		return
	}
	seedAnchors := false
	if lookup, ok := s.store.(intelMentionLookup); ok {
		if existing, err := lookup.IntelAnchorsByPath(ctx, intelPath); err == nil && len(existing) == 0 {
			seedAnchors = true
		}
	}
	if seedAnchors {
		allEdges := append(anchorEdges, callEdges...)
		_ = intel.ReplaceIntelCodeFile(ctx, intelPath, anchors, allEdges, ftsRows)
		return
	}
	_ = intel.UpsertIntelCallEdgesForPath(ctx, intelPath, callEdges)
}

func hasAnyCodeRefSignals(summary FileSummary) bool {
	return len(summary.Calls) > 0 || len(summary.Imports) > 0 || len(summary.TypeRefs) > 0 || len(summary.MemberRefs) > 0
}

func (s *Service) buildIntelCallEdges(ctx context.Context, lang Lang, path string, summary FileSummary, anchors []IntelAnchor) []IntelEdge {
	if len(summary.Calls) == 0 {
		return nil
	}
	resolver, ok := s.store.(intelCallResolver)
	if !ok {
		return nil
	}
	methodResolver, _ := s.store.(intelGoMethodResolver)
	anchorLookup, _ := s.store.(intelMentionLookup)

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
		moduleID = intelAnchorID(lang, "module", path, "module")
	}

	fqnsByLang := make(map[Lang]map[string]struct{})
	goMethodNames := make(map[string]struct{})
	pySymbolNames := make(map[string]struct{})
	hasPyCalls := false
	goPkgImportPath := ""
	if lang == LangGo && methodResolver != nil {
		// Best-effort: infer the package import path from symbol metadata.
		for _, sym := range summary.Symbols {
			pkg := strings.TrimSpace(sym.Pkg)
			if pkg == "" {
				continue
			}
			// Method symbols store Pkg as "<importPath>.<ReceiverType>" — strip receiver.
			if slash := strings.LastIndex(pkg, "/"); slash >= 0 {
				if dotAfterSlash := strings.Index(pkg[slash:], "."); dotAfterSlash >= 0 {
					pkg = pkg[:slash+dotAfterSlash]
				}
			}
			if strings.TrimSpace(pkg) != "" {
				goPkgImportPath = pkg
				break
			}
		}
	}
	for _, call := range summary.Calls {
		name := strings.TrimSpace(call.CalleeSymbol.Name)
		if name == "" {
			continue
		}
		callLang := call.CalleeSymbol.Lang
		if callLang == "" {
			callLang = lang
		}
		// Go method calls may come through with an empty package (receiver unknown). Handle separately.
		if callLang == LangGo && strings.TrimSpace(call.CalleeSymbol.Pkg) == "" && methodResolver != nil && goPkgImportPath != "" {
			// Avoid exploding the graph on extremely common method names.
			if isCommonGoMethodName(name) {
				continue
			}
			if len(name) > 0 && name[0] >= 'A' && name[0] <= 'Z' {
				goMethodNames[name] = struct{}{}
			}
			continue
		}

		if callLang == LangPy {
			hasPyCalls = true
		}
		if callLang == LangPy && strings.TrimSpace(call.CalleeSymbol.Pkg) == "" {
			pySymbolNames[name] = struct{}{}
			continue
		}

		fqn := normalizeSymbol(SymbolRef{Lang: callLang, Pkg: call.CalleeSymbol.Pkg, Name: name, Member: call.CalleeSymbol.Member})
		if fqn == "" {
			continue
		}
		if fqnsByLang[callLang] == nil {
			fqnsByLang[callLang] = make(map[string]struct{})
		}
		fqnsByLang[callLang][fqn] = struct{}{}
	}
	if len(fqnsByLang) == 0 && len(goMethodNames) == 0 && len(pySymbolNames) == 0 {
		return nil
	}

	calleeIDs := make(map[string][]string)
	for callLang, fqnSet := range fqnsByLang {
		fqns := make([]string, 0, len(fqnSet))
		for fqn := range fqnSet {
			fqns = append(fqns, fqn)
		}
		sort.Strings(fqns)
		idsByFQN, err := resolver.IntelAnchorIDsByFQNsAndLang(ctx, string(callLang), fqns)
		if err != nil {
			continue
		}
		for fqn, ids := range idsByFQN {
			key := fmt.Sprintf("%s|%s", strings.ToLower(string(callLang)), fqn)
			calleeIDs[key] = ids
		}
	}
	// Resolve Go method-name-only calls within the current package.
	goMethodIDs := make(map[string][]string)
	if methodResolver != nil && goPkgImportPath != "" && len(goMethodNames) > 0 {
		methods := make([]string, 0, len(goMethodNames))
		for m := range goMethodNames {
			methods = append(methods, m)
		}
		sort.Strings(methods)
		for _, m := range methods {
			ids, err := methodResolver.IntelAnchorIDsByGoMethodNameInPkg(ctx, goPkgImportPath, m, 20)
			if err != nil {
				continue
			}
			if len(ids) > 0 {
				goMethodIDs[m] = ids
			}
		}
	}

	hasPySymbolFallback := anchorLookup != nil && hasPyCalls
	if len(calleeIDs) == 0 && len(goMethodIDs) == 0 && !hasPySymbolFallback {
		return nil
	}

	type edgeKey struct {
		src  string
		dst  string
		kind string
	}
	edgesByKey := make(map[edgeKey]struct{})
	includeTests := IsTestPath(path)
	pySymbolIDs := make(map[string][]string)
	pyCallerDir := pathpkg.Dir(filepath.ToSlash(path))
	if pyCallerDir == "." {
		pyCallerDir = ""
	}
	resolvePySymbolFallback := func(name string) []string {
		if name == "" || anchorLookup == nil {
			return nil
		}
		if ids, ok := pySymbolIDs[name]; ok {
			return ids
		}
		anchors, err := anchorLookup.IntelAnchorsBySymbol(ctx, name, 25)
		if err != nil {
			return nil
		}
		filtered := make([]string, 0, len(anchors))
		for _, a := range anchors {
			if a.Lang != LangPy {
				continue
			}
			if strings.EqualFold(a.Kind, "module") {
				continue
			}
			aPath := filepath.ToSlash(a.Path)
			if pyCallerDir != "" {
				if pathpkg.Dir(aPath) != pyCallerDir {
					continue
				}
			} else if pathpkg.Dir(aPath) != "." {
				continue
			}
			filtered = append(filtered, a.AnchorID)
		}
		pySymbolIDs[name] = filtered
		return filtered
	}

	for _, call := range summary.Calls {
		name := strings.TrimSpace(call.CalleeSymbol.Name)
		if name == "" {
			continue
		}
		callLang := call.CalleeSymbol.Lang
		if callLang == "" {
			callLang = lang
		}
		dstIDs := []string(nil)
		if callLang == LangGo && strings.TrimSpace(call.CalleeSymbol.Pkg) == "" && methodResolver != nil && goPkgImportPath != "" {
			dstIDs = goMethodIDs[name]
		} else if callLang == LangPy && strings.TrimSpace(call.CalleeSymbol.Pkg) == "" && anchorLookup != nil {
			dstIDs = resolvePySymbolFallback(name)
		} else {
			fqn := normalizeSymbol(SymbolRef{Lang: callLang, Pkg: call.CalleeSymbol.Pkg, Name: name, Member: call.CalleeSymbol.Member})
			if fqn == "" {
				continue
			}
			key := fmt.Sprintf("%s|%s", strings.ToLower(string(callLang)), fqn)
			dstIDs = calleeIDs[key]
		}
		if len(dstIDs) == 0 && callLang == LangPy && anchorLookup != nil {
			pkg := strings.TrimSpace(call.CalleeSymbol.Pkg)
			if pkg != "" && !strings.EqualFold(pkg, "builtins") {
				dstIDs = resolvePySymbolFallback(name)
			}
		}
		if len(dstIDs) == 0 {
			continue
		}
		srcID := moduleID
		if owner := strings.TrimSpace(call.OwnerFQN); owner != "" {
			if id, ok := ownerByFQN[owner]; ok {
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

	if len(edgesByKey) == 0 {
		return nil
	}
	edges := make([]IntelEdge, 0, len(edgesByKey))
	for key := range edgesByKey {
		edges = append(edges, IntelEdge{
			SrcID: key.src,
			DstID: key.dst,
			Kind:  key.kind,
		})
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

func isCommonGoMethodName(name string) bool {
	switch strings.TrimSpace(name) {
	case "Error", "String", "Close", "Len", "Write", "Read":
		return true
	default:
		return false
	}
}

// buildIntelImportEdges creates file-level import dependency edges.
// These provide baseline connectivity even when call edges can't be resolved
// (e.g., enum attribute access, constant references).
func (s *Service) buildIntelImportEdges(ctx context.Context, lang Lang, path string, summary FileSummary, anchors []IntelAnchor) []IntelEdge {
	if len(summary.Imports) == 0 {
		return nil
	}
	// Python and C# use module suffix resolution; TypeScript uses path resolution.
	if lang != LangPy && lang != LangTS && lang != LangCs {
		return nil
	}

	// Find the module anchor ID for the current file (source of import edges).
	moduleID := ""
	for _, a := range anchors {
		if strings.EqualFold(a.Kind, "module") && moduleID == "" {
			moduleID = a.AnchorID
			break
		}
	}
	if moduleID == "" {
		moduleID = intelAnchorID(lang, "module", path, "module")
	}

	// Collect unique module names from imports.
	modules := make([]string, 0, len(summary.Imports))
	seen := make(map[string]struct{})
	for _, imp := range summary.Imports {
		mod := strings.TrimSpace(imp.Module)
		if mod == "" {
			continue
		}
		if _, ok := seen[mod]; ok {
			continue
		}
		seen[mod] = struct{}{}
		modules = append(modules, mod)
	}
	if len(modules) == 0 {
		return nil
	}

	// Resolve module names to anchor IDs using language-specific resolution.
	sort.Strings(modules)
	var targetIDs map[string]string
	var err error

	switch lang {
	case LangPy, LangCs:
		// Python: use path suffix matching (module names like "charm.misc.colors").
		resolver, ok := s.store.(intelModuleSuffixResolver)
		if !ok {
			return nil
		}
		targetIDs, err = resolver.ModuleAnchorIDsByModuleSuffix(ctx, modules)
	case LangTS:
		// TypeScript: modules are resolved file paths (since TS module anchors have empty FQNs).
		// Convert absolute paths to vault-relative paths for lookup.
		resolver, ok := s.store.(intelModulePathResolver)
		if !ok {
			return nil
		}
		// Convert absolute paths to vault-relative using VaultPaths.
		absToRel := make(map[string]string, len(modules))
		relPaths := make([]string, 0, len(modules))
		for _, absPath := range modules {
			// Use vault paths to convert to relative, falling back to path normalization.
			relPath, err := s.vault.RelCodeStrict(absPath)
			if err != nil {
				// Fallback: try just normalizing (might already be relative).
				relPath = paths.NormalizeCode(absPath)
			}
			relStr := string(relPath)
			if relStr != "" {
				absToRel[absPath] = relStr
				relPaths = append(relPaths, relStr)
			}
		}
		if len(relPaths) == 0 {
			return nil
		}
		// Look up module anchors by path.
		pathToID, lookupErr := resolver.ModuleAnchorIDsByPaths(ctx, relPaths)
		if lookupErr != nil {
			return nil
		}
		// Map back from original modules to anchor IDs.
		targetIDs = make(map[string]string, len(pathToID))
		for absPath, relPath := range absToRel {
			if id, ok := pathToID[relPath]; ok {
				targetIDs[absPath] = id
			}
		}
	}

	if err != nil || len(targetIDs) == 0 {
		return nil
	}

	// Create import edges from this file's module anchor to each imported module's anchor.
	edges := make([]IntelEdge, 0, len(targetIDs))
	for _, mod := range modules {
		targetID, ok := targetIDs[mod]
		if !ok || targetID == "" {
			continue
		}
		// Skip self-imports.
		if targetID == moduleID {
			continue
		}
		edges = append(edges, IntelEdge{
			SrcID: moduleID,
			DstID: targetID,
			Kind:  "imports",
		})
	}
	return edges
}

// buildIntelTypeRefEdges creates edges from code to referenced types.
// These connect functions/methods to the types they reference in annotations.
func (s *Service) buildIntelTypeRefEdges(ctx context.Context, lang Lang, path string, summary FileSummary, anchors []IntelAnchor) []IntelEdge {
	if len(summary.TypeRefs) == 0 {
		return nil
	}
	resolver, ok := s.store.(intelCallResolver)
	if !ok {
		return nil
	}

	// Build owner map from anchors.
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
		moduleID = intelAnchorID(lang, "module", path, "module")
	}

	// Collect unique type FQNs.
	fqnsByLang := make(map[Lang]map[string]struct{})
	for _, tr := range summary.TypeRefs {
		name := strings.TrimSpace(tr.TypeSym.Name)
		if name == "" {
			continue
		}
		typeLang := tr.TypeSym.Lang
		if typeLang == "" {
			typeLang = lang
		}
		fqn := normalizeSymbol(SymbolRef{Lang: typeLang, Pkg: tr.TypeSym.Pkg, Name: name})
		if fqn == "" {
			continue
		}
		if fqnsByLang[typeLang] == nil {
			fqnsByLang[typeLang] = make(map[string]struct{})
		}
		fqnsByLang[typeLang][fqn] = struct{}{}
	}
	if len(fqnsByLang) == 0 {
		return nil
	}

	// Resolve FQNs to anchor IDs.
	typeIDs := make(map[string][]string)
	for typeLang, fqnSet := range fqnsByLang {
		fqns := make([]string, 0, len(fqnSet))
		for fqn := range fqnSet {
			fqns = append(fqns, fqn)
		}
		sort.Strings(fqns)
		idsByFQN, err := resolver.IntelAnchorIDsByFQNsAndLang(ctx, string(typeLang), fqns)
		if err != nil {
			continue
		}
		for fqn, ids := range idsByFQN {
			key := fmt.Sprintf("%s|%s", strings.ToLower(string(typeLang)), fqn)
			typeIDs[key] = ids
		}
	}
	if len(typeIDs) == 0 {
		return nil
	}

	// Create edges.
	type edgeKey struct {
		src  string
		dst  string
		kind string
	}
	edgesByKey := make(map[edgeKey]struct{})

	for _, tr := range summary.TypeRefs {
		name := strings.TrimSpace(tr.TypeSym.Name)
		if name == "" {
			continue
		}
		typeLang := tr.TypeSym.Lang
		if typeLang == "" {
			typeLang = lang
		}
		fqn := normalizeSymbol(SymbolRef{Lang: typeLang, Pkg: tr.TypeSym.Pkg, Name: name})
		if fqn == "" {
			continue
		}
		key := fmt.Sprintf("%s|%s", strings.ToLower(string(typeLang)), fqn)
		dstIDs := typeIDs[key]
		if len(dstIDs) == 0 {
			continue
		}
		srcID := moduleID
		if owner := strings.TrimSpace(tr.OwnerFQN); owner != "" {
			if id, ok := ownerByFQN[owner]; ok {
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

	if len(edgesByKey) == 0 {
		return nil
	}
	edges := make([]IntelEdge, 0, len(edgesByKey))
	for key := range edgesByKey {
		edges = append(edges, IntelEdge{
			SrcID: key.src,
			DstID: key.dst,
			Kind:  key.kind,
		})
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

func (s *Service) buildIntelMemberRefEdges(ctx context.Context, lang Lang, path string, summary FileSummary, anchors []IntelAnchor) []IntelEdge {
	if len(summary.MemberRefs) == 0 {
		return nil
	}
	resolver, ok := s.store.(intelCallResolver)
	if !ok {
		return nil
	}

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
		moduleID = intelAnchorID(lang, "module", path, "module")
	}

	fqnsByLang := make(map[Lang]map[string]struct{})
	for _, mr := range summary.MemberRefs {
		name := strings.TrimSpace(mr.Sym.Name)
		if name == "" {
			continue
		}
		memberLang := mr.Sym.Lang
		if memberLang == "" {
			memberLang = lang
		}
		// MemberRefs reference class members by definition; Member=true makes
		// normalizeSymbol use `::` to match the stored method/property FQN.
		fqn := normalizeSymbol(SymbolRef{Lang: memberLang, Pkg: mr.Sym.Pkg, Name: name, Member: true})
		if fqn == "" {
			continue
		}
		if fqnsByLang[memberLang] == nil {
			fqnsByLang[memberLang] = make(map[string]struct{})
		}
		fqnsByLang[memberLang][fqn] = struct{}{}
	}
	if len(fqnsByLang) == 0 {
		return nil
	}

	memberIDs := make(map[string][]string)
	for memberLang, fqnSet := range fqnsByLang {
		fqns := make([]string, 0, len(fqnSet))
		for fqn := range fqnSet {
			fqns = append(fqns, fqn)
		}
		sort.Strings(fqns)
		idsByFQN, err := resolver.IntelAnchorIDsByFQNsAndLang(ctx, string(memberLang), fqns)
		if err != nil {
			continue
		}
		for fqn, ids := range idsByFQN {
			key := fmt.Sprintf("%s|%s", strings.ToLower(string(memberLang)), fqn)
			memberIDs[key] = ids
		}
	}
	if len(memberIDs) == 0 {
		return nil
	}

	type edgeKey struct {
		src  string
		dst  string
		kind string
	}
	edgesByKey := make(map[edgeKey]struct{})
	for _, mr := range summary.MemberRefs {
		name := strings.TrimSpace(mr.Sym.Name)
		if name == "" {
			continue
		}
		memberLang := mr.Sym.Lang
		if memberLang == "" {
			memberLang = lang
		}
		// MemberRefs reference class members by definition; Member=true makes
		// normalizeSymbol use `::` to match the stored method/property FQN.
		fqn := normalizeSymbol(SymbolRef{Lang: memberLang, Pkg: mr.Sym.Pkg, Name: name, Member: true})
		if fqn == "" {
			continue
		}
		key := fmt.Sprintf("%s|%s", strings.ToLower(string(memberLang)), fqn)
		dstIDs := memberIDs[key]
		if len(dstIDs) == 0 {
			continue
		}
		srcID := moduleID
		if owner := strings.TrimSpace(mr.OwnerFQN); owner != "" {
			if id, ok := ownerByFQN[owner]; ok {
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

// RebuildAllCallEdges rebuilds call and import edges for all indexed files.
// This should be called after batch indexing to resolve edges that couldn't be
// resolved during the first pass (when callee/imported anchors didn't exist yet).
func (s *Service) RebuildAllCallEdges(ctx context.Context) (returnErr error) {
	defer func() {
		if returnErr == nil {
			returnErr = s.rebuildGoPackageRelationships(ctx, nil)
		}
	}()
	intel, ok := s.store.(intelStore)
	if !ok {
		return nil
	}

	// Get all indexed files with their languages
	lister, ok := s.store.(fileWithLangLister)
	if !ok {
		return nil
	}
	list, err := lister.ListFilesWithLang(ctx, 0)
	if err != nil {
		return err
	}
	if len(list) == 0 {
		return nil
	}

	total := len(list)

	// Set up worker pool for parallel file reading and indexing.
	workerCount := runtime.GOMAXPROCS(0)
	if workerCount < 2 {
		workerCount = 2
	}
	if workerCount > 32 {
		workerCount = 32
	}
	if workerCount > total {
		workerCount = total
	}

	items := make([]callEdgeRebuildItem, 0, len(list))
	for _, f := range list {
		if f.Lang == "" {
			continue
		}
		items = append(items, callEdgeRebuildItem{path: f.Path, lang: Lang(f.Lang)})
	}
	sources, err := s.loadCallEdgeRebuildSources(ctx, items)
	if err != nil {
		return err
	}

	rebuildCtx, cancelRebuild := context.WithCancel(ctx)
	defer cancelRebuild()

	workCh := make(chan callEdgeRebuildItem, workerCount*2)
	resultCh := make(chan CallEdgesBatch, workerCount*2)
	errCh := make(chan error, 1)
	var processed atomic.Int64
	reportErr := func(err error) {
		if err == nil {
			return
		}
		cancelRebuild()
		select {
		case errCh <- err:
		default:
		}
	}

	// Writer goroutine: collects results and batches writes.
	var writeWG sync.WaitGroup
	var writeErr error
	writeWG.Add(1)
	go func() {
		defer writeWG.Done()
		var buffer callEdgeWriteBuffer
		flushBatches := func() error {
			if len(buffer.batches) == 0 {
				return nil
			}
			if err := s.writeCallEdgeBatches(rebuildCtx, intel, buffer.batches); err != nil {
				return err
			}
			buffer.reset()
			return nil
		}
		for batch := range resultCh {
			buffer.add(batch)
			if buffer.shouldFlush() {
				if err := flushBatches(); err != nil {
					writeErr = err
					cancelRebuild()
					return
				}
			}
		}
		if err := flushBatches(); err != nil {
			writeErr = err
			cancelRebuild()
		}
	}()

	// Worker goroutines: parallel file reading and edge building.
	var wg sync.WaitGroup
	for i := 0; i < workerCount; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for item := range workCh {
				select {
				case <-rebuildCtx.Done():
					return
				default:
				}

				processed.Add(1)

				batch, preserveExisting, err := s.buildCallEdgesForItem(rebuildCtx, item, sources)
				if err != nil {
					reportErr(err)
					return
				}
				if preserveExisting {
					continue
				}
				if batch.Path == "" {
					continue
				}

				select {
				case resultCh <- batch:
				case <-rebuildCtx.Done():
					return
				}
			}
		}()
	}

	// Feed work to workers.
	go func() {
		for _, item := range items {
			select {
			case workCh <- item:
			case <-rebuildCtx.Done():
				close(workCh)
				return
			}
		}
		close(workCh)
	}()

	// Wait for workers to finish, then close result channel.
	wg.Wait()
	close(resultCh)

	// Wait for writer to finish.
	writeWG.Wait()
	if writeErr != nil {
		return writeErr
	}
	select {
	case err := <-errCh:
		return err
	default:
	}

	// Mark call edges as rebuilt so RecomputeAnchorScopes doesn't rebuild again.
	s.callEdgesRebuilt = true
	return nil
}

// CallEdgeRebuildSummary reports counts for a call-edge rebuild decision.
type CallEdgeRebuildSummary struct {
	ChangedCallers  int
	ImpactedCallers int
	RebuildPaths    int
}

// CallEdgeResidual tracks unresolved reverse-index lookups that should be retried
// after more caller refs become durable.
type CallEdgeResidual struct {
	SymbolRefs []SymbolRef
	Modules    []string
	Fallbacks  []ReverseIndexFallback
}

// HasPending reports whether any unresolved reverse-index work remains.
func (r CallEdgeResidual) HasPending() bool {
	return len(r.SymbolRefs) > 0 || len(r.Modules) > 0 || len(r.Fallbacks) > 0
}

// MergeCallEdgeResidual combines residual work, de-duping refs/modules and
// widening fallback path filters when the same symbol needs multiple guards.
func MergeCallEdgeResidual(dst, src CallEdgeResidual) CallEdgeResidual {
	dst.SymbolRefs = uniqueSymbolRefs(append(dst.SymbolRefs, src.SymbolRefs...))
	dst.Modules = uniqueStrings(append(dst.Modules, src.Modules...))
	dst.Fallbacks = uniqueReverseIndexFallbacks(append(dst.Fallbacks, src.Fallbacks...))
	return dst
}

type CallEdgeRebuildPlan struct {
	Summary  CallEdgeRebuildSummary
	Paths    []string
	Residual CallEdgeResidual
}

// PlanCallEdgeRebuildForDefDeltas resolves the exact files whose call/import
// edges must be rebuilt for a set of changed callers and definition deltas.
func (s *Service) PlanCallEdgeRebuildForDefDeltas(ctx context.Context, changedPaths []string, deltas DefDeltas) (CallEdgeRebuildPlan, error) {
	return s.PlanCallEdgeRebuildIncremental(ctx, changedPaths, deltas, CallEdgeResidual{}, true)
}

// PlanCallEdgeRebuildIncremental resolves call-edge rebuild paths from the
// currently durable reverse-index rows and returns residual lookups that should
// be retried after more callers are indexed.
func (s *Service) PlanCallEdgeRebuildIncremental(ctx context.Context, changedPaths []string, deltas DefDeltas, residual CallEdgeResidual, backfillComplete bool) (CallEdgeRebuildPlan, error) {
	plan := CallEdgeRebuildPlan{}
	if ctx == nil {
		ctx = context.Background()
	}
	if len(changedPaths) == 0 && !deltas.HasChanges() && !residual.HasPending() {
		return plan, nil
	}

	rebuildSet := make(map[string]struct{})
	impactedSet := make(map[string]struct{})
	changedSet := make(map[string]struct{})
	addPath := func(path string) string {
		path = strings.TrimSpace(path)
		if path == "" {
			return ""
		}
		relPath, err := s.relCodePath(path)
		if err != nil || strings.TrimSpace(relPath) == "" {
			return ""
		}
		rebuildSet[relPath] = struct{}{}
		return relPath
	}
	for _, path := range changedPaths {
		if relPath := addPath(path); relPath != "" {
			changedSet[relPath] = struct{}{}
		}
	}

	symbolRefs := uniqueSymbolRefs(append(append(append([]SymbolRef{}, residual.SymbolRefs...), deltas.AddedSymbols...), deltas.RemovedSymbols...))
	modules := uniqueStrings(append(append(append([]string{}, residual.Modules...), deltas.AddedModules...), deltas.RemovedModules...))
	fallbacks := append([]ReverseIndexFallback{}, residual.Fallbacks...)
	fallbacks = append(fallbacks, s.reverseIndexFallbacks(deltas)...)
	fallbacks = uniqueReverseIndexFallbacks(fallbacks)
	if len(symbolRefs) > 0 && len(fallbacks) > 0 {
		fallbackSet := make(map[string]struct{}, len(fallbacks))
		for _, fb := range fallbacks {
			if key := symbolRefKey(fb.Ref); key != "" {
				fallbackSet[key] = struct{}{}
			}
		}
		filtered := symbolRefs[:0]
		for _, ref := range symbolRefs {
			if _, ok := fallbackSet[symbolRefKey(ref)]; ok {
				continue
			}
			filtered = append(filtered, ref)
		}
		symbolRefs = filtered
	}

	if !backfillComplete {
		plan.Residual = CallEdgeResidual{
			SymbolRefs: symbolRefs,
			Modules:    modules,
			Fallbacks:  fallbacks,
		}
		plan.Summary.ChangedCallers = len(changedSet)
		if len(rebuildSet) == 0 {
			return plan, nil
		}
		rebuildPaths := make([]string, 0, len(rebuildSet))
		for path := range rebuildSet {
			rebuildPaths = append(rebuildPaths, path)
		}
		sort.Strings(rebuildPaths)
		plan.Paths = rebuildPaths
		plan.Summary.RebuildPaths = len(rebuildPaths)
		return plan, nil
	}

	if refStore, ok := s.store.(intelRefStore); ok {
		if len(symbolRefs) > 0 {
			referrers, err := lookupReferrerPaths(ctx, uniqueSymbolRefs(symbolRefs), refStore.ReferrerPathsBySymbolRefs)
			if err != nil {
				return plan, err
			}
			for _, paths := range referrers {
				for _, path := range paths {
					if relPath := addPath(path); relPath != "" {
						impactedSet[relPath] = struct{}{}
					}
				}
			}
		}

		if len(fallbacks) > 0 {
			fallbackRefs := make([]SymbolRef, 0, len(fallbacks))
			fallbackFilters := make(map[string]func(string) bool, len(fallbacks))
			for _, fb := range fallbacks {
				key := symbolRefKey(fb.Ref)
				if key == "" {
					continue
				}
				fallbackRefs = append(fallbackRefs, fb.Ref)
				fallbackFilters[key] = fb.PathFilter
			}
			fallbackRefs = uniqueSymbolRefs(fallbackRefs)
			if len(fallbackRefs) > 0 {
				referrers, err := lookupReferrerPaths(ctx, fallbackRefs, refStore.ReferrerPathsBySymbolRefs)
				if err != nil {
					return plan, err
				}
				for key, paths := range referrers {
					filter := fallbackFilters[key]
					for _, path := range paths {
						if filter != nil && !filter(path) {
							continue
						}
						if relPath := addPath(path); relPath != "" {
							impactedSet[relPath] = struct{}{}
						}
					}
				}
			}
		}

		if len(modules) > 0 {
			referrers, err := lookupReferrerPaths(ctx, uniqueStrings(modules), refStore.ReferrerPathsByModules)
			if err != nil {
				return plan, err
			}
			for _, paths := range referrers {
				for _, path := range paths {
					if relPath := addPath(path); relPath != "" {
						impactedSet[relPath] = struct{}{}
					}
				}
			}
		}
	}

	plan.Summary.ChangedCallers = len(changedSet)
	plan.Summary.ImpactedCallers = len(impactedSet)
	if len(rebuildSet) == 0 {
		return plan, nil
	}
	rebuildPaths := make([]string, 0, len(rebuildSet))
	for path := range rebuildSet {
		rebuildPaths = append(rebuildPaths, path)
	}
	sort.Strings(rebuildPaths)
	plan.Paths = rebuildPaths
	plan.Summary.RebuildPaths = len(rebuildPaths)
	return plan, nil
}

func lookupReferrerPaths[T any](ctx context.Context, items []T, lookup func(context.Context, []T) (map[string][]string, error)) (map[string][]string, error) {
	merged := make(map[string][]string, len(items))
	if len(items) == 0 {
		return merged, nil
	}
	progress := callEdgePlanProgressFromContext(ctx)
	if progress != nil {
		progress.Start((len(items) + callEdgeReferrerLookupChunkSize - 1) / callEdgeReferrerLookupChunkSize)
	}
	for start := 0; start < len(items); start += callEdgeReferrerLookupChunkSize {
		end := min(start+callEdgeReferrerLookupChunkSize, len(items))
		referrers, err := lookup(ctx, items[start:end])
		if err != nil {
			return nil, err
		}
		mergeReferrerPaths(merged, referrers)
		if progress != nil {
			progress.Advance(1)
		}
	}
	return merged, nil
}

func mergeReferrerPaths(dst map[string][]string, src map[string][]string) {
	for key, paths := range src {
		if len(paths) == 0 {
			continue
		}
		dst[key] = uniqueStrings(append(dst[key], paths...))
	}
}

// RebuildCallEdgesForDefDeltas rebuilds call edges for changed callers plus
// callers impacted by definition/module deltas via the reverse index.
func (s *Service) RebuildCallEdgesForDefDeltas(ctx context.Context, changedPaths []string, deltas DefDeltas) (CallEdgeRebuildSummary, error) {
	plan, err := s.PlanCallEdgeRebuildForDefDeltas(ctx, changedPaths, deltas)
	if err != nil {
		return CallEdgeRebuildSummary{}, err
	}
	if len(plan.Paths) == 0 {
		return plan.Summary, nil
	}
	return plan.Summary, s.RebuildCallEdgesForPaths(ctx, plan.Paths)
}

func (s *Service) reverseIndexFallbacks(deltas DefDeltas) []ReverseIndexFallback {
	if !deltas.HasChanges() {
		return nil
	}
	var fallbacks []ReverseIndexFallback
	for _, idx := range s.indexers {
		if idx == nil {
			continue
		}
		if provider, ok := idx.(ReverseIndexFallbackProvider); ok {
			fallbacks = append(fallbacks, provider.ReverseIndexFallbacks(deltas)...)
		}
	}
	return fallbacks
}

// RebuildCallEdgesForPaths rebuilds call and import edges for specific files.
// This is more efficient than RebuildAllCallEdges when only a few files changed.
func (s *Service) RebuildCallEdgesForPaths(ctx context.Context, filePaths []string) (returnErr error) {
	if err := s.RebuildParserCallEdgesForPaths(ctx, filePaths); err != nil {
		return err
	}
	return s.rebuildGoPackageRelationships(ctx, filePaths)
}

// RebuildParserCallEdgesForPaths rebuilds relationships emitted by language
// indexers. Unified indexing invokes this separately so Go package analysis can
// run once after the complete persisted package snapshot is visible.
func (s *Service) RebuildParserCallEdgesForPaths(ctx context.Context, filePaths []string) (returnErr error) {
	if len(filePaths) == 0 {
		return nil
	}

	intel, ok := s.store.(intelStore)
	if !ok {
		return nil
	}

	// Build work items by detecting language from path.
	var items []callEdgeRebuildItem
	for _, p := range filePaths {
		relPath, err := s.relCodePath(p)
		if err != nil || strings.TrimSpace(relPath) == "" {
			continue
		}
		lang := detectLangFromPath(relPath)
		if lang == "" {
			continue
		}
		items = append(items, callEdgeRebuildItem{path: relPath, lang: lang})
	}
	if len(items) == 0 {
		return nil
	}
	sources, err := s.loadCallEdgeRebuildSources(ctx, items)
	if err != nil {
		return err
	}
	indexingperf.AddCount(ctx, "calledge.direct_paths", int64(len(sources.runtimeByPath)))
	indexingperf.AddCount(ctx, "calledge.planned_paths", int64(len(items)-len(sources.runtimeByPath)))

	total := len(items)
	progress := callEdgeProgressFromContext(ctx)
	if progress != nil {
		// Surface rebuild progress through the command/context progress hook instead
		// of the global logger so indexing stays quiet on interactive terminals.
		progress.Start(total)
	}

	// For small numbers of files, process sequentially to avoid goroutine overhead.
	if total <= 4 {
		var batches []CallEdgesBatch
		for _, item := range items {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			if progress != nil {
				progress.Advance(1)
			}
			batch, preserveExisting, err := s.buildCallEdgesForItem(ctx, item, sources)
			if err != nil {
				return err
			}
			if preserveExisting || batch.Path == "" {
				continue
			}
			batches = append(batches, batch)
		}
		if err := s.writeCallEdgeBatches(ctx, intel, batches); err != nil {
			return err
		}
		s.callEdgesRebuilt = true
		return nil
	}

	// For larger batches, use parallel processing (similar to RebuildAllCallEdges)
	workerCount := runtime.GOMAXPROCS(0)
	if workerCount < 2 {
		workerCount = 2
	}
	if workerCount > 32 {
		workerCount = 32
	}
	if workerCount > total {
		workerCount = total
	}

	rebuildCtx, cancelRebuild := context.WithCancel(ctx)
	defer cancelRebuild()

	workCh := make(chan callEdgeRebuildItem, workerCount*2)
	resultCh := make(chan CallEdgesBatch, workerCount*2)
	errCh := make(chan error, 1)
	var processed atomic.Int64
	reportErr := func(err error) {
		if err == nil {
			return
		}
		cancelRebuild()
		select {
		case errCh <- err:
		default:
		}
	}

	var writeWG sync.WaitGroup
	var writeErr error
	writeWG.Add(1)
	go func() {
		defer writeWG.Done()
		var buffer callEdgeWriteBuffer
		flushBatches := func() error {
			if len(buffer.batches) == 0 {
				return nil
			}
			if err := s.writeCallEdgeBatches(rebuildCtx, intel, buffer.batches); err != nil {
				return err
			}
			buffer.reset()
			return nil
		}
		for batch := range resultCh {
			buffer.add(batch)
			if buffer.shouldFlush() {
				if err := flushBatches(); err != nil {
					writeErr = err
					cancelRebuild()
					return
				}
			}
		}
		if err := flushBatches(); err != nil {
			writeErr = err
			cancelRebuild()
		}
	}()

	var wg sync.WaitGroup
	for i := 0; i < workerCount; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for item := range workCh {
				select {
				case <-rebuildCtx.Done():
					return
				default:
				}

				processed.Add(1)
				if progress != nil {
					progress.Advance(1)
				}

				batch, preserveExisting, err := s.buildCallEdgesForItem(rebuildCtx, item, sources)
				if err != nil {
					reportErr(err)
					return
				}
				if preserveExisting || batch.Path == "" {
					continue
				}

				select {
				case resultCh <- batch:
				case <-rebuildCtx.Done():
					return
				}
			}
		}()
	}

	go func() {
		for _, item := range items {
			select {
			case workCh <- item:
			case <-rebuildCtx.Done():
				close(workCh)
				return
			}
		}
		close(workCh)
	}()

	wg.Wait()
	close(resultCh)
	writeWG.Wait()

	if writeErr != nil {
		return writeErr
	}
	select {
	case err := <-errCh:
		return err
	default:
	}

	s.callEdgesRebuilt = true
	return nil
}

// RebuildGoPackageRelationshipsForPaths publishes exact relationships derived
// from complete persisted Go package snapshots containing the supplied paths.
func (s *Service) RebuildGoPackageRelationshipsForPaths(ctx context.Context, filePaths []string) error {
	return s.rebuildGoPackageRelationships(ctx, filePaths)
}

type goPackageRelationshipStore interface {
	GoPackageSnapshotsForPaths(ctx context.Context, sourcePaths []string) ([]GoPackageSnapshot, error)
	ReplaceGoPackageRelationships(ctx context.Context, replacements []GoPackageRelationshipReplacement) error
}

func (s *Service) rebuildGoPackageRelationships(ctx context.Context, sourcePaths []string) error {
	store, ok := s.store.(goPackageRelationshipStore)
	if !ok {
		return nil
	}
	snapshots, err := store.GoPackageSnapshotsForPaths(ctx, sourcePaths)
	if err != nil {
		return err
	}
	replacements := make([]GoPackageRelationshipReplacement, 0, len(snapshots))
	for _, snapshot := range snapshots {
		hasCurrentMember := false
		for _, member := range snapshot.Membership {
			if member.PackageKey == snapshot.Package.StorageKey() {
				hasCurrentMember = true
				break
			}
		}
		if !hasCurrentMember {
			replacements = append(replacements, GoPackageRelationshipReplacement{
				Package: snapshot.Package, MembershipDigest: snapshot.MembershipDigest,
				AnalyzerVersion: GoRelationshipAnalyzerVersion, Remove: true,
			})
			continue
		}
		readable := snapshot.Sources[:0]
		for _, source := range snapshot.Sources {
			absPath, err := s.vault.AbsCode(paths.CodePath(source.Path))
			if err != nil {
				continue
			}
			if _, err := s.vault.RelCodeStrict(absPath.String()); err != nil {
				continue
			}
			content, err := os.ReadFile(absPath.String())
			if err != nil {
				continue
			}
			source.Content = content
			readable = append(readable, source)
		}
		snapshot.Sources = readable
		analysis, err := AnalyzeGoPackageRelationships(ctx, snapshot)
		if err != nil {
			return err
		}
		replacements = append(replacements, GoPackageRelationshipReplacement{
			Package:          analysis.Package,
			MembershipDigest: analysis.MembershipDigest,
			AnalyzerVersion:  analysis.AnalyzerVersion,
			Complete:         analysis.Complete,
			Diagnostics:      analysis.Diagnostics,
			Relationships:    analysis.Relationships,
		})
	}
	return store.ReplaceGoPackageRelationships(ctx, replacements)
}

func summaryFromRefs(lang Lang, path string, symbolRefs []SymbolRefRow, importRefs []ImportRefRow, moduleDefs []ModuleDefRow) FileSummary {
	summary := FileSummary{
		FilePath: path,
		Lang:     lang,
	}

	for _, row := range symbolRefs {
		dstLang := row.DstLang
		if dstLang == "" {
			dstLang = lang
		}
		ref := SymbolRef{
			Lang:   dstLang,
			Pkg:    row.DstPkg,
			Name:   row.DstName,
			Member: row.DstMember,
		}
		switch row.RefKind {
		case RefKindCalls:
			summary.Calls = append(summary.Calls, CallSite{
				File:         path,
				OwnerFQN:     row.OwnerFQN,
				CalleeSymbol: ref,
			})
		case RefKindTypeRef:
			summary.TypeRefs = append(summary.TypeRefs, TypeRef{
				File:     path,
				OwnerFQN: row.OwnerFQN,
				TypeSym:  ref,
			})
		case RefKindMemberRef:
			summary.MemberRefs = append(summary.MemberRefs, MemberRef{
				File:     path,
				OwnerFQN: row.OwnerFQN,
				Sym:      ref,
			})
		}
	}

	for _, row := range importRefs {
		if strings.TrimSpace(row.Module) == "" {
			continue
		}
		summary.Imports = append(summary.Imports, ImportEdge{Module: row.Module})
	}

	if lang == LangGo {
		for _, def := range moduleDefs {
			if def.Lang != LangGo {
				continue
			}
			if strings.TrimSpace(def.Module) == "" {
				continue
			}
			summary.Symbols = append(summary.Symbols, Symbol{Lang: LangGo, Pkg: def.Module})
			break
		}
	}

	return summary
}

func uniqueSymbolRefs(refs []SymbolRef) []SymbolRef {
	if len(refs) == 0 {
		return nil
	}
	seen := make(map[string]SymbolRef, len(refs))
	for _, ref := range refs {
		if strings.TrimSpace(ref.Name) == "" || strings.TrimSpace(string(ref.Lang)) == "" {
			continue
		}
		key := symbolRefKey(ref)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = ref
	}
	if len(seen) == 0 {
		return nil
	}
	out := make([]SymbolRef, 0, len(seen))
	for _, ref := range seen {
		out = append(out, ref)
	}
	sort.Slice(out, func(i, j int) bool {
		return symbolRefKey(out[i]) < symbolRefKey(out[j])
	})
	return out
}

func uniqueReverseIndexFallbacks(fallbacks []ReverseIndexFallback) []ReverseIndexFallback {
	if len(fallbacks) == 0 {
		return nil
	}
	seenRefs := make(map[string]SymbolRef, len(fallbacks))
	filters := make(map[string]func(string) bool, len(fallbacks))
	for _, fb := range fallbacks {
		key := symbolRefKey(fb.Ref)
		if key == "" {
			continue
		}
		seenRefs[key] = fb.Ref
		if fb.PathFilter == nil {
			filters[key] = nil
			continue
		}
		if existing, ok := filters[key]; ok {
			if existing == nil {
				continue
			}
			prev := existing
			next := fb.PathFilter
			filters[key] = func(path string) bool {
				return prev(path) || next(path)
			}
			continue
		}
		filters[key] = fb.PathFilter
	}
	if len(seenRefs) == 0 {
		return nil
	}
	keys := make([]string, 0, len(seenRefs))
	for key := range seenRefs {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	out := make([]ReverseIndexFallback, 0, len(keys))
	for _, key := range keys {
		out = append(out, ReverseIndexFallback{
			Ref:        seenRefs[key],
			PathFilter: filters[key],
		})
	}
	return out
}

func uniqueStrings(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		seen[value] = struct{}{}
	}
	if len(seen) == 0 {
		return nil
	}
	out := make([]string, 0, len(seen))
	for value := range seen {
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}

// detectLangFromPath returns the language for a file path based on extension.
func detectLangFromPath(path string) Lang {
	ext := strings.ToLower(filepath.Ext(path))
	if codefile.IsTypeScriptJavaScriptExtension(ext) {
		return LangTS
	}
	switch ext {
	case ".py":
		return LangPy
	case ".go":
		return LangGo
	case ".cs":
		return LangCs
	case ".php":
		return LangPhp
	default:
		return ""
	}
}

// updateIntelForMarkdownSource persists Markdown intel doc sections + mentions
// after the public source boundary has authorized Markdown.
func (s *Service) updateIntelForMarkdownSource(ctx context.Context, path, content string, mtime int64) error {
	intel, ok := s.store.(intelStore)
	if !ok {
		return nil
	}
	// Callers pass the already-resolved note path from the source boundary.
	intelPath := string(paths.NormalizeNotePath(path))
	sections, _, ftsRows := extractMarkdownIntelDocSections(intelPath, content)
	for i := range sections {
		sections[i].UpdatedAt = mtime
	}
	mentions := s.resolveNoteMentions(ctx, intelPath, sections, content)
	return intel.ReplaceIntelDocSections(ctx, intelPath, sections, mentions, ftsRows)
}

func (s *Service) deleteIntelByPath(ctx context.Context, path string, isNote bool) error {
	intel, ok := s.store.(intelStore)
	if !ok {
		return nil
	}
	if isNote {
		return intel.DeleteIntelByPath(ctx, string(paths.NormalizeNotePath(path)))
	}
	intelPath := string(paths.NormalizeCode(path))
	return intel.DeleteIntelByPath(ctx, intelPath)
}

func (s *Service) resolveNoteMentions(ctx context.Context, intelPath string, sections []IntelDocSection, content string) []IntelEdge {
	started := time.Now()
	defer func() {
		indexingperf.ObserveLatency(ctx, "noteindex.mentions.resolve", time.Since(started))
	}()
	lookup, ok := s.store.(intelMentionLookup)
	if !ok || len(sections) == 0 {
		return nil
	}
	srcSectionID := sections[0].SectionID
	if srcSectionID == "" {
		return nil
	}

	candidates := extractMentionCandidates(intelPath, content)
	if len(candidates) == 0 {
		return nil
	}
	indexingperf.ObserveSample(ctx, "noteindex.mentions.candidates", int64(len(candidates)))

	type edgeKey struct {
		src  string
		dst  string
		kind string
	}
	seen := make(map[edgeKey]bool)
	var out []IntelEdge

	for _, c := range candidates {
		switch c.Source {
		case "mdlink":
			target := c.TargetPath
			if target == "" {
				continue
			}
			indexingperf.AddCount(ctx, "noteindex.mentions.lookup.path", 1)
			anchors, err := lookup.IntelAnchorsByPath(ctx, target)
			if err != nil {
				indexingperf.AddCount(ctx, "noteindex.mentions.lookup_error", 1)
				continue
			}
			indexingperf.ObserveSample(ctx, "noteindex.mentions.lookup.path_results", int64(len(anchors)))

			want := ""
			if c.Fragment != "" && looksLikeIdentifier(c.Fragment) {
				want = c.Fragment
			}
			for _, a := range anchors {
				if want == "" {
					if a.Kind != "module" {
						continue
					}
				} else {
					if a.Symbol != want {
						continue
					}
				}

				meta, _ := json.Marshal(map[string]any{
					"source":     "mdlink",
					"targetPath": target,
					"fragment":   c.Fragment,
				})
				k := edgeKey{src: srcSectionID, dst: a.AnchorID, kind: "mentions"}
				if seen[k] {
					continue
				}
				seen[k] = true
				out = append(out, IntelEdge{
					SrcID:    srcSectionID,
					DstID:    a.AnchorID,
					Kind:     "mentions",
					MetaJSON: string(meta),
				})
			}

		case "at":
			if c.Symbol == "" || c.Qualifier == "" {
				continue
			}
			// Conservative cap; tail-match filtering happens in-memory.
			indexingperf.AddCount(ctx, "noteindex.mentions.lookup.symbol", 1)
			anchors, err := lookup.IntelAnchorsBySymbol(ctx, c.Symbol, 250)
			if err != nil {
				indexingperf.AddCount(ctx, "noteindex.mentions.lookup_error", 1)
				continue
			}
			indexingperf.ObserveSample(ctx, "noteindex.mentions.lookup.symbol_results", int64(len(anchors)))
			for _, a := range anchors {
				if !matchesTailQualifier(a.Path, c.Qualifier) {
					continue
				}
				meta, _ := json.Marshal(map[string]any{
					"source":    "at",
					"qualifier": c.Qualifier,
					"symbol":    c.Symbol,
				})
				k := edgeKey{src: srcSectionID, dst: a.AnchorID, kind: "mentions"}
				if seen[k] {
					continue
				}
				seen[k] = true
				out = append(out, IntelEdge{
					SrcID:    srcSectionID,
					DstID:    a.AnchorID,
					Kind:     "mentions",
					MetaJSON: string(meta),
				})
			}
		}
	}

	indexingperf.ObserveSample(ctx, "noteindex.mentions.edges", int64(len(out)))
	return out
}

func matchesTailQualifier(anchorPath, qualifier string) bool {
	anchorPath = filepath.ToSlash(strings.TrimSpace(anchorPath))
	qualifier = strings.TrimSpace(qualifier)
	if anchorPath == "" || qualifier == "" {
		return false
	}

	parts := strings.Split(qualifier, ".")
	if len(parts) == 0 {
		return false
	}

	wantStem := parts[len(parts)-1]
	dirParts := parts[:len(parts)-1]
	wantDir := strings.Join(dirParts, "/")

	base := filepath.Base(anchorPath)
	stem := strings.TrimSuffix(base, filepath.Ext(base))
	if stem != wantStem {
		return false
	}
	if wantDir == "" {
		return true
	}
	dir := filepath.Dir(anchorPath)
	// Require a directory boundary match to reduce false positives.
	if strings.Contains(dir, "/"+wantDir) || strings.HasSuffix(dir, wantDir) {
		return true
	}
	return false
}
