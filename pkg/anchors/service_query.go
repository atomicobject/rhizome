package codeanchor

import (
	"context"
	"path/filepath"
	"sort"

	"github.com/bmatcuk/doublestar/v4"
)

// NotesForFile resolves anchors and notes for a file.
//
// This is the retrieval-facing read path for note -> code bindings. It prefers
// materialized scopes for speed, but falls back to exact matching when scopes are
// missing, dirty, or suspiciously empty for a file with obvious symbols/calls.
// That fallback protects correctness for live runs and fresh indexes without
// forcing every file_context request to recompute global scopes.
func (s *Service) NotesForFile(ctx context.Context, path string) (FileContext, error) {
	relPath, err := s.relCodePath(path)
	if err != nil {
		return FileContext{}, err
	}
	if ctx == nil {
		ctx = context.Background()
	}
	fc, generation, ok := s.cachedFileCtx(relPath)
	if ok {
		s.cacheMu.Lock()
		s.cacheHits++
		s.cacheMu.Unlock()
		return fc, nil
	}
	s.cacheMu.Lock()
	s.cacheMisses++
	s.cacheMu.Unlock()
	fileSymbols, err := s.store.SymbolsByFile(ctx, relPath)
	if err != nil {
		return FileContext{}, err
	}

	// Always include calls/annotations in the returned context; these are useful for debugging and
	// for downstream presentation even when scope-based matching is used.
	annotations, err := s.store.AnnotationsOnSymbols(ctx, fileSymbols)
	if err != nil {
		return FileContext{}, err
	}

	calls, err := s.store.CallsFromFile(ctx, relPath)
	if err != nil {
		return FileContext{}, err
	}

	fileLang := langFromPath(relPath)
	hasScopes := s.anchorScopesAvailable(ctx)
	overflow, dirtyIDs := s.dirtyIDsSnapshot()

	matchedBySymbol := map[int64][]string{}
	callAnchorSet := map[int64]bool{}

	if hasScopes {
		// Fast path: use precomputed scopes to avoid per-file graph traversals.
		matchedBySymbol, err = s.store.AnchorsBySymbols(ctx, fileSymbols)
		if err != nil {
			return FileContext{}, err
		}
		callPath := normalizePathCase(relPath)
		callAnchors, err := s.store.AnchorsByCallFiles(ctx, []string{callPath})
		if err != nil {
			return FileContext{}, err
		}
		for _, id := range callAnchors {
			callAnchorSet[id] = true
		}
	}

	var allSymbols []string
	var allSymbolsBuilt bool
	buildAllSymbols := func() error {
		if allSymbolsBuilt {
			return nil
		}
		allSymbolsBuilt = true
		seenSym := map[string]bool{}
		for _, fqn := range fileSymbols {
			if fqn == "" || seenSym[fqn] {
				continue
			}
			seenSym[fqn] = true
			allSymbols = append(allSymbols, fqn)
			anc, err := s.store.Ancestors(ctx, fqn)
			if err != nil {
				return err
			}
			for _, a := range anc {
				if a == "" || seenSym[a] {
					continue
				}
				seenSym[a] = true
				allSymbols = append(allSymbols, a)
			}
		}
		return nil
	}

	exactMatch := func() (map[int64][]string, map[int64]bool, []int64, error) {
		// Exact matching first narrows candidate anchors by indexed symbols/calls,
		// then verifies selectors against this file. It is still bounded by the
		// file's facts, not a scan of every anchor against every indexed file.
		if err := buildAllSymbols(); err != nil {
			return nil, nil, nil, err
		}
		candidateIDs := make(map[int64]bool)

		// Symbols (definitions + ancestors) can match baseClass and function anchors.
		if fileLang != "" && len(allSymbols) > 0 {
			pkgs := make([]string, 0, len(allSymbols))
			names := make([]string, 0, len(allSymbols))
			for _, fqn := range allSymbols {
				pkg, name := splitFQN(fqn)
				if pkg == "" || name == "" {
					continue
				}
				pkgs = append(pkgs, pkg)
				names = append(names, name)
			}
			ids, err := s.store.AnchorsMatchingSymbols(ctx, names, pkgs, fileLang)
			if err != nil {
				return nil, nil, nil, err
			}
			for _, id := range ids {
				candidateIDs[id] = true
			}
		}

		// Call-site candidates for function anchors.
		if len(calls) > 0 {
			pkgsByLang := map[Lang][]string{}
			namesByLang := map[Lang][]string{}
			for _, c := range calls {
				ref := c.CalleeSymbol
				if ref.Lang == "" || ref.Pkg == "" || ref.Name == "" {
					continue
				}
				pkgsByLang[ref.Lang] = append(pkgsByLang[ref.Lang], ref.Pkg)
				namesByLang[ref.Lang] = append(namesByLang[ref.Lang], ref.Name)
			}
			for lang, pkgs := range pkgsByLang {
				ids, err := s.store.AnchorsMatchingSymbols(ctx, namesByLang[lang], pkgs, lang)
				if err != nil {
					return nil, nil, nil, err
				}
				for _, id := range ids {
					candidateIDs[id] = true
				}
			}
		}

		// Annotation candidates.
		if len(annotations) > 0 {
			annTypes := make([]SymbolRef, 0, len(annotations))
			seenAnn := make(map[string]bool)
			for _, a := range annotations {
				ref := a.AnnSymbol
				if ref.Lang == "" || ref.Name == "" {
					continue
				}
				key := string(ref.Lang) + "\x00" + ref.Pkg + "\x00" + ref.Name
				if seenAnn[key] {
					continue
				}
				seenAnn[key] = true
				annTypes = append(annTypes, ref)
			}
			ids, err := s.store.AnchorsMatchingAnnotations(ctx, annTypes)
			if err != nil {
				return nil, nil, nil, err
			}
			for _, id := range ids {
				candidateIDs[id] = true
			}
		}

		if len(candidateIDs) == 0 {
			return nil, nil, nil, nil
		}

		ids := make([]int64, 0, len(candidateIDs))
		for id := range candidateIDs {
			ids = append(ids, id)
		}
		anchors, err := s.store.AnchorsByIDs(ctx, ids)
		if err != nil {
			return nil, nil, nil, err
		}

		exactBySymbol := map[int64][]string{}
		exactCallSet := map[int64]bool{}
		for _, anchor := range anchors {
			switch anchor.Kind {
			case AnchorKind("functionUse"):
				anchor.Kind = AnchorFunc
			}
			if anchorMatchesFile(anchor, fileLang, fileSymbols, allSymbols, calls, annotations) {
				switch anchor.Kind {
				case AnchorFunc:
					if anchor.BaseSym != nil {
						base := normalizeSymbol(*anchor.BaseSym)
						if sliceContainsString(fileSymbols, base) {
							exactBySymbol[anchor.ID] = []string{base}
						} else if fileCallsContain(calls, *anchor.BaseSym) {
							exactCallSet[anchor.ID] = true
						}
					}
				case AnchorAnnotation:
					if anchor.Ann != nil {
						exactBySymbol[anchor.ID] = []string{normalizeSymbol(anchor.Ann.Symbol)}
					}
				default:
					if anchor.BaseSym != nil {
						exactBySymbol[anchor.ID] = []string{normalizeSymbol(*anchor.BaseSym)}
					}
				}
			}
		}

		return exactBySymbol, exactCallSet, ids, nil
	}

	// Correctness path: when scopes are missing or dirty overflow is set, fall back to exact matching
	// for just the anchors relevant to this file and warm scopes incrementally in the background.
	if !hasScopes || overflow {
		exactBySymbol, exactCallSet, ids, err := exactMatch()
		if err != nil {
			return FileContext{}, err
		}
		for id, syms := range exactBySymbol {
			matchedBySymbol[id] = syms
		}
		for id := range exactCallSet {
			callAnchorSet[id] = true
		}
		if len(ids) > 0 {
			s.warmAnchorScopesAsync(ids)
		} else if overflow {
			// Overflow means "global scopes are stale"; schedule a background recompute.
			s.warmAnchorScopesAsync(nil)
		}
	} else if len(dirtyIDs) > 0 {
		// Scopes exist, but some anchors may be stale. Evaluate dirty anchors exactly and
		// schedule an incremental recompute to refresh scopes.
		anchors, err := s.store.AnchorsByIDs(ctx, dirtyIDs)
		if err != nil {
			return FileContext{}, err
		}
		needsAncestors := false
		for _, a := range anchors {
			if a.Kind == AnchorBaseClass {
				needsAncestors = true
				break
			}
		}
		if needsAncestors {
			if err := buildAllSymbols(); err != nil {
				return FileContext{}, err
			}
		} else {
			allSymbols = fileSymbols
		}
		for _, anchor := range anchors {
			switch anchor.Kind {
			case AnchorKind("functionUse"):
				anchor.Kind = AnchorFunc
			}
			if anchorMatchesFile(anchor, fileLang, fileSymbols, allSymbols, calls, annotations) {
				switch anchor.Kind {
				case AnchorFunc:
					if anchor.BaseSym != nil {
						base := normalizeSymbol(*anchor.BaseSym)
						if sliceContainsString(fileSymbols, base) {
							matchedBySymbol[anchor.ID] = []string{base}
						} else if fileCallsContain(calls, *anchor.BaseSym) {
							callAnchorSet[anchor.ID] = true
						}
					}
				case AnchorAnnotation:
					if anchor.Ann != nil {
						matchedBySymbol[anchor.ID] = []string{normalizeSymbol(anchor.Ann.Symbol)}
					}
				default:
					if anchor.BaseSym != nil {
						matchedBySymbol[anchor.ID] = []string{normalizeSymbol(*anchor.BaseSym)}
					}
				}
			}
		}
		s.warmAnchorScopesAsync(nil)
	}
	if hasScopes && !overflow && len(dirtyIDs) == 0 {
		needsFallback := (len(fileSymbols) > 0 && len(matchedBySymbol) == 0) ||
			(len(calls) > 0 && len(callAnchorSet) == 0) ||
			(len(annotations) > 0 && len(matchedBySymbol) == 0)
		if needsFallback {
			// A fully empty scope result for a fact-rich file often means scopes are
			// stale or not yet materialized for the newest index rows. Try exact
			// matching before returning a false negative to callers.
			exactBySymbol, exactCallSet, _, err := exactMatch()
			if err != nil {
				return FileContext{}, err
			}
			if len(exactBySymbol) > 0 {
				if matchedBySymbol == nil {
					matchedBySymbol = make(map[int64][]string, len(exactBySymbol))
				}
				for id, syms := range exactBySymbol {
					if len(matchedBySymbol[id]) == 0 {
						matchedBySymbol[id] = syms
					}
				}
			}
			if len(exactCallSet) > 0 {
				for id := range exactCallSet {
					callAnchorSet[id] = true
				}
			}
		}
	}

	// Fetch anchors matching directory path prefix (dir: anchors).
	pathAnchors, err := s.store.AnchorsByPathPrefix(ctx, relPath)
	if err != nil {
		return FileContext{}, err
	}
	pathAnchorSet := make(map[int64]bool, len(pathAnchors))
	for _, id := range pathAnchors {
		pathAnchorSet[id] = true
	}

	// Fetch anchors matching glob patterns (glob: / globs: anchors).
	globAnchors, err := s.store.AnchorsByGlobMatch(ctx, relPath)
	if err != nil {
		return FileContext{}, err
	}
	globAnchorSet := make(map[int64]bool, len(globAnchors))
	for _, id := range globAnchors {
		globAnchorSet[id] = true
	}

	// Union of matched anchors. Deduping before NotesForAnchor avoids repeated
	// note lookups when one anchor matches by both definition and call-site facts.
	matchedSet := make(map[int64]bool, len(matchedBySymbol)+len(callAnchorSet)+len(pathAnchors)+len(globAnchors))
	for id := range matchedBySymbol {
		matchedSet[id] = true
	}
	for id := range callAnchorSet {
		matchedSet[id] = true
	}
	for id := range pathAnchorSet {
		matchedSet[id] = true
	}
	for id := range globAnchorSet {
		matchedSet[id] = true
	}

	ids := make([]int64, 0, len(matchedSet))
	for id := range matchedSet {
		ids = append(ids, id)
	}

	anchors, err := s.store.AnchorsByIDs(ctx, ids)
	if err != nil {
		return FileContext{}, err
	}

	var matched []string
	var traces []AnchorMatchTrace
	noteSet := map[int64]Note{}
	notesByAnchor := make(map[string][]Note)
	anchorKinds := make(map[string]AnchorKind)
	for _, anchor := range anchors {
		matchedAnchor := false

		switch anchor.Kind {
		case AnchorKind("functionUse"):
			anchor.Kind = AnchorFunc
		case AnchorBaseClass, AnchorFunc:
			if syms := matchedBySymbol[anchor.ID]; len(syms) > 0 {
				matchedAnchor = true
				traces = append(traces, AnchorMatchTrace{
					AnchorLabel: anchor.Label,
					Reason:      "symbol matched scope",
					Target:      syms[0],
				})
			} else if anchor.Kind == AnchorFunc && callAnchorSet[anchor.ID] {
				matchedAnchor = true
				target := relPath
				if anchor.BaseSym != nil {
					target = normalizeSymbol(*anchor.BaseSym)
				}
				traces = append(traces, AnchorMatchTrace{
					AnchorLabel: anchor.Label,
					Reason:      "call site matched scope",
					Target:      target,
				})
			}
		case AnchorAnnotation:
			if syms := matchedBySymbol[anchor.ID]; len(syms) > 0 {
				matchedAnchor = true
				traces = append(traces, AnchorMatchTrace{
					AnchorLabel: anchor.Label,
					Reason:      "annotation matched scope",
					Target:      syms[0],
				})
			}
		case AnchorPath:
			if pathAnchorSet[anchor.ID] {
				matchedAnchor = true
				traces = append(traces, AnchorMatchTrace{
					AnchorLabel: anchor.Label,
					Reason:      "path matched directory",
					Target:      anchor.PathPrefix,
				})
			}
		case AnchorGlob:
			if globAnchorSet[anchor.ID] {
				matchedAnchor = true
				traces = append(traces, AnchorMatchTrace{
					AnchorLabel: anchor.Label,
					Reason:      "path matched glob",
					Target:      firstMatchingGlob(anchor.Globs, relPath),
				})
			}
		}

		if !matchedAnchor {
			continue
		}
		matched = append(matched, anchor.Label)
		if anchor.Label != "" {
			anchorKinds[anchor.Label] = anchor.Kind
		}
		notes, err := s.store.NotesForAnchor(ctx, anchor.ID)
		if err != nil {
			return FileContext{}, err
		}
		if anchor.Label != "" && len(notes) > 0 {
			notesByAnchor[anchor.Label] = append(notesByAnchor[anchor.Label], notes...)
		}
		for _, n := range notes {
			noteSet[n.ID] = n
		}
	}

	var notes []Note
	for _, n := range noteSet {
		notes = append(notes, n)
	}

	var anchorNotes []AnchorNotes
	if len(notesByAnchor) > 0 {
		labels := make([]string, 0, len(notesByAnchor))
		for label := range notesByAnchor {
			labels = append(labels, label)
		}
		sort.Strings(labels)
		for _, label := range labels {
			perAnchor := notesByAnchor[label]
			// Deduplicate within an anchor by note ID to avoid repeats.
			seen := make(map[int64]bool, len(perAnchor))
			uniq := make([]Note, 0, len(perAnchor))
			for _, n := range perAnchor {
				if seen[n.ID] {
					continue
				}
				seen[n.ID] = true
				uniq = append(uniq, n)
			}
			sort.Slice(uniq, func(i, j int) bool { return uniq[i].Path < uniq[j].Path })
			anchorNotes = append(anchorNotes, AnchorNotes{Label: label, Notes: uniq})
		}
	}

	ctxVal := FileContext{
		File:        relPath,
		Anchors:     matched,
		Notes:       notes,
		AnchorNotes: anchorNotes,
		AnchorKinds: anchorKinds,
		Trace:       traces,
		Annotations: annotations,
		Calls:       calls,
		Symbols:     fileSymbols,
	}
	s.cacheFileContext(relPath, ctxVal, generation)
	return ctxVal, nil
}

func fileCallsContain(calls []CallSite, target SymbolRef) bool {
	if len(calls) == 0 {
		return false
	}
	for _, c := range calls {
		if c.CalleeSymbol.Lang == target.Lang && c.CalleeSymbol.Pkg == target.Pkg && c.CalleeSymbol.Name == target.Name {
			return true
		}
	}
	return false
}

func sliceContainsString(vals []string, target string) bool {
	if target == "" || len(vals) == 0 {
		return false
	}
	for _, v := range vals {
		if v == target {
			return true
		}
	}
	return false
}

func fileAnnotationsContain(annotations []AnnotationUse, target SymbolRef, filters map[string]string) bool {
	for _, a := range annotations {
		if a.AnnSymbol.Lang != target.Lang || a.AnnSymbol.Pkg != target.Pkg || a.AnnSymbol.Name != target.Name {
			continue
		}
		if matchArgs(filters, a.Args) {
			return true
		}
	}
	return false
}

func anchorMatchesFile(anchor Anchor, fileLang Lang, fileSymbols []string, allSymbols []string, calls []CallSite, annotations []AnnotationUse) bool {
	switch anchor.Kind {
	case AnchorBaseClass:
		if anchor.BaseSym == nil || anchor.BaseSym.Lang == "" {
			return false
		}
		if fileLang != "" && anchor.BaseSym.Lang != fileLang {
			return false
		}
		target := normalizeSymbol(*anchor.BaseSym)
		for _, sym := range allSymbols {
			if sym == target {
				return true
			}
		}
		return false
	case AnchorFunc:
		if anchor.BaseSym == nil || anchor.BaseSym.Lang == "" {
			return false
		}
		if fileLang != "" && anchor.BaseSym.Lang != fileLang {
			return false
		}
		target := normalizeSymbol(*anchor.BaseSym)
		for _, sym := range fileSymbols {
			if sym == target {
				return true
			}
		}
		return fileCallsContain(calls, *anchor.BaseSym)
	case AnchorAnnotation:
		if anchor.Ann == nil || anchor.Ann.Symbol.Lang == "" {
			return false
		}
		return fileAnnotationsContain(annotations, anchor.Ann.Symbol, anchor.Ann.ArgFilters)
	default:
		return false
	}
}

func firstMatchingGlob(patterns []string, target string) string {
	if len(patterns) == 0 {
		return ""
	}
	t := filepath.ToSlash(target)
	for _, pat := range patterns {
		if pat == "" {
			continue
		}
		ok, _ := doublestar.Match(pat, t)
		if ok {
			return pat
		}
	}
	// If none match (unexpected), fall back to the first for debugging.
	return patterns[0]
}

// SymbolsForFile returns the FQNs of symbols defined in the file.
// Useful for discovering what symbols exist for anchor definitions.
func (s *Service) SymbolsForFile(ctx context.Context, path string) ([]string, error) {
	relPath, err := s.relCodePath(path)
	if err != nil {
		return nil, err
	}
	return s.store.SymbolsByFile(ctx, relPath)
}
