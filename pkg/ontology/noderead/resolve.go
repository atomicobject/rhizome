package noderead

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	semdb "github.com/atomicobject/rhizome/pkg/anchors/sqlite"
	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
)

var markdownLinkTargetPattern = regexp.MustCompile(`\[[^\]]+\]\(([^)]+)\)`)

// Resolve converts user-facing locator strings into canonical NodeRefs,
// hydrated records, and source locators.
//
// When requested, it can apply safe block-ID fixes for embedded nodes before
// returning a refreshed result.
func (s *Scope) Resolve(ctx context.Context, req ResolveRequest) (ResolveResult, error) {
	if s == nil || s.service == nil {
		return ResolveResult{}, nil
	}
	// Docs: [[ontology-browser-workspace#^spec-0014-us3-ac4]] requires
	// browser/search-visible refs to resolve before callers guess client-side.
	parsed, diagnostics := s.parseResolveTargets(ctx, req)
	locators := make([]string, 0, len(parsed))
	structuralPaths := make([]string, 0, len(parsed))
	for _, target := range parsed {
		if target.SourceLocator != "" {
			locators = append(locators, target.SourceLocator)
		}
		if target.Ref.Structural != "" && target.Ref.Fragment == "" && target.Ref.NodeID == "" {
			structuralPaths = append(structuralPaths, target.Ref.NotePath)
		}
	}
	rowsByLocator := map[string]codeanchor.IntelOntologyNode{}
	rowsByStructural := map[string]codeanchor.IntelOntologyNode{}
	catalogStore, hasCatalogStore := s.service.Store.(CatalogStore)
	if hasCatalogStore && len(locators) > 0 {
		// Catalog lookups are the normal fast path for embedded/section locators.
		// Projection fallback below exists for freshness/linkability gaps, not as
		// a reason to scan or parse every note during resolve.
		rows, err := catalogStore.OntologyNodesBySourceLocators(ctx, locators)
		if err != nil {
			return ResolveResult{}, err
		}
		rowsByLocator = rows
		missing := make([]string, 0)
		for _, locator := range locators {
			if _, ok := rowsByLocator[locator]; !ok {
				missing = append(missing, locator)
			}
		}
		if len(missing) > 0 {
			rows, err := catalogStore.OntologyNodesByNoteFragments(ctx, missing)
			if err != nil {
				return ResolveResult{}, err
			}
			for key, row := range rows {
				rowsByLocator[key] = row
				if row.SourceLocator != "" {
					rowsByLocator[row.SourceLocator] = row
				}
			}
		}
	}
	if hasCatalogStore && len(structuralPaths) > 0 {
		rows, err := catalogStore.OntologyNodesByPaths(ctx, normalizeStrings(structuralPaths))
		if err != nil {
			return ResolveResult{}, err
		}
		ambiguous := map[string]bool{}
		for _, row := range rows {
			ref := nodeRefFromCatalogRow(row)
			if ref.Structural == "" {
				continue
			}
			key := structuralResolveKey(ref)
			if _, exists := rowsByStructural[key]; exists {
				delete(rowsByStructural, key)
				ambiguous[key] = true
				continue
			}
			if !ambiguous[key] {
				rowsByStructural[key] = row
			}
		}
	}

	inputRefs := make([]inputRef, 0, len(parsed))
	for _, target := range parsed {
		ref := target.Ref
		resolvedFromCatalog := false
		if target.Ref.Structural != "" && target.Ref.Fragment == "" && target.Ref.NodeID == "" {
			if row, ok := rowsByStructural[structuralResolveKey(target.Ref)]; ok {
				ref = nodeRefFromCatalogRow(row)
				resolvedFromCatalog = true
			}
		}
		if !resolvedFromCatalog && target.SourceLocator != "" {
			if row, ok := rowsByLocator[target.SourceLocator]; ok {
				ref = nodeRefFromCatalogRow(row)
				resolvedFromCatalog = true
			}
		}
		projectionFragment := target.Fragment
		if projectionFragment == "" && target.Ref.Structural != "" {
			projectionFragment = "struct:" + target.Ref.Structural
		}
		projectionPath := target.NotePath
		if projectionPath == "" && target.Ref.Structural != "" && target.Ref.Fragment == "" && target.Ref.NodeID == "" {
			projectionPath = target.Ref.NotePath
		}
		if projectionPath != "" && projectionFragment != "" && !resolvedFromCatalog {
			if req.IndexOnly {
				diagnostics = append(diagnostics, NodeResolveDiagnostic{Input: target.Input, Code: "node_unresolved", Message: "node fragment is not present in the indexed catalog"})
				continue
			}
			projected, err := s.resolveFragmentByProjection(ctx, projectionPath, projectionFragment)
			if err == nil && !projected.IsZero() {
				if ref.IsZero() || ref.TypeName == "" || ref.NodeID == "" {
					ref = projected
				}
			} else if ref.IsZero() {
				if err != nil {
					diagnostics = append(diagnostics, NodeResolveDiagnostic{Input: target.Input, Code: "node_unresolved", Message: err.Error()})
				}
				continue
			}
		}
		if ref.IsZero() {
			diagnostics = append(diagnostics, NodeResolveDiagnostic{Input: target.Input, Code: "node_unresolved", Message: "node target could not be resolved"})
			continue
		}
		inputRefs = append(inputRefs, inputRef{Input: target.Input, Ref: ref})
	}

	refs := make([]ontology.NodeRef, 0, len(inputRefs))
	for _, item := range inputRefs {
		refs = append(refs, item.Ref)
	}
	records, err := s.Hydrate(ctx, refs, req.Hydrate)
	if err != nil {
		return ResolveResult{}, err
	}
	recordByRequest := make(map[string]NodeRecord, len(records))
	for _, record := range records {
		recordByRequest[nodeRefIdentityKey(record.Ref)] = record
	}
	for key, record := range indexLoadedRecordsForRequests(refs, records) {
		recordByRequest[key] = record
	}
	var locatorMap map[string]ontology.NodeLocator
	if !req.OmitLocators || req.EnsureLinkTarget == ontology.EnsureLinkTargetPlan || req.EnsureLinkTarget == ontology.EnsureLinkTargetApply {
		locatorMap, err = s.Locators(ctx, refs)
		if err != nil {
			return ResolveResult{}, err
		}
	}
	noteRecordByPath := map[string]NodeRecord{}
	for _, record := range records {
		if record.Ref.Kind == "" || record.Ref.Kind == ontology.NodeKindNote {
			noteRecordByPath[record.Path] = record
		}
	}

	result := ResolveResult{Diagnostics: diagnostics}
	seen := map[string]struct{}{}
	for _, item := range inputRefs {
		key := nodeRefIdentityKey(item.Ref)
		record := recordByRequest[key]
		if record.Path == "" && (item.Ref.Kind == "" || item.Ref.Kind == ontology.NodeKindNote) {
			record = noteRecordByPath[item.Ref.NotePath]
		}
		resolvedRef := item.Ref
		if !record.Ref.IsZero() && (item.Ref.Kind == ontology.NodeKindSection || item.Ref.Kind == ontology.NodeKindEmbedded) {
			resolvedRef = record.Ref
		}
		resolvedKey := nodeRefIdentityKey(resolvedRef)
		if _, ok := seen[item.Input+"\x00"+resolvedKey]; ok {
			continue
		}
		seen[item.Input+"\x00"+resolvedKey] = struct{}{}
		locator := locatorMap[nodereadLocatorKey(resolvedRef)]
		if locator.SourceLocator == "" {
			locator = locatorMap[ontology.NodeSourceLocator(resolvedRef)]
		}
		if locator.SourceLocator == "" && resolvedKey != key {
			locator = locatorMap[nodereadLocatorKey(item.Ref)]
			if locator.SourceLocator == "" {
				locator = locatorMap[ontology.NodeSourceLocator(item.Ref)]
			}
		}
		if locator.Status == ontology.NodeLocatorRequiresFix {
			result.Diagnostics = append(result.Diagnostics, NodeResolveDiagnostic{
				Input:   item.Input,
				Code:    "requires_fix",
				Message: "embedded ontology node needs a block ID before its link target is durable",
			})
			if len(locator.FixActions) > 0 {
				if result.FixPlan == nil {
					result.FixPlan = &ontology.NodeLinkFixPlan{}
				}
				result.FixPlan.Actions = append(result.FixPlan.Actions, locator.FixActions...)
			}
		}
		result.Resolved = append(result.Resolved, ResolvedNode{
			Input:   item.Input,
			Ref:     resolvedRef,
			Record:  record,
			Locator: locator,
		})
	}

	if req.EnsureLinkTarget == ontology.EnsureLinkTargetApply {
		applied, applyErr := s.applyEmbeddedLinkTargets(ctx, refs)
		if applyErr != nil {
			return result, applyErr
		}
		if !applied {
			return result, nil
		}
		refresh := req
		refresh.EnsureLinkTarget = ontology.EnsureLinkTargetNever
		refresh.OmitLocators = false
		return s.Resolve(ctx, refresh)
	}
	return result, nil
}

func (s *Scope) applyEmbeddedLinkTargets(ctx context.Context, refs []ontology.NodeRef) (bool, error) {
	embedded := make([]ontology.NodeRef, 0, len(refs))
	for _, ref := range refs {
		ref = normalizeNodeRef(ref)
		if ref.Kind == ontology.NodeKindEmbedded {
			embedded = append(embedded, ref)
		}
	}
	if len(embedded) == 0 {
		return false, nil
	}
	if s.service.ApplyLinkTargets == nil || s.opts.ReadOverlay != nil {
		return false, fmt.Errorf("ensure-link apply requires an explicit live writer")
	}
	_, err := s.service.ApplyLinkTargets(ctx, ontology.LinkTargetRequest{
		Refs: embedded, Ensure: ontology.EnsureLinkTargetApply, Purpose: "file-context",
	})
	// A failed publication can follow a successful source commit. Every attempt
	// drops dependent state, including retries with no remaining source fixes.
	s.invalidateAfterApply()
	return true, err
}

func structuralResolveKey(ref ontology.NodeRef) string {
	return strings.TrimSpace(ref.NotePath) + "\x00" + strings.TrimSpace(ref.Structural)
}

func (s *Scope) invalidateAfterApply() {
	if s.service.SharedCache != nil {
		s.service.SharedCache.InvalidateAll("ensure-link apply")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cacheGeneration++
	s.initializeCaches()
	s.noteStateLoaded = false
	s.noteRows = nil
	s.allNotePaths = nil
	s.cachedNotePathCache = nil
	s.notePathCacheLoaded = false
	s.embeddedReady = false
	s.overlayIndex = nil
}

type parsedResolveTarget struct {
	Input         string
	SourceLocator string
	NotePath      string
	Fragment      string
	Ref           ontology.NodeRef
}

type inputRef struct {
	Input string
	Ref   ontology.NodeRef
}

func (s *Scope) parseResolveTargets(ctx context.Context, req ResolveRequest) ([]parsedResolveTarget, []NodeResolveDiagnostic) {
	targets := make([]parsedResolveTarget, 0, len(req.Targets))
	diagnostics := make([]NodeResolveDiagnostic, 0)
	var cache *obsidian.NotePathCache
	notePathCache := func() *obsidian.NotePathCache {
		if cache == nil {
			cache = s.notePathCache(ctx)
		}
		return cache
	}
	var preferredIdentifierIndex map[string][]ontology.NodeRef
	var preferredIdentifierErr error
	preferredIdentifierLoaded := false
	preferredIdentifierCandidates := func(target string) ([]ontology.NodeRef, error) {
		if !preferredIdentifierLoaded {
			preferredIdentifierLoaded = true
			preferredIdentifierIndex, preferredIdentifierErr = s.loadPreferredIdentifierIndex(ctx)
		}
		if preferredIdentifierErr != nil {
			return nil, preferredIdentifierErr
		}
		return append([]ontology.NodeRef(nil), preferredIdentifierIndex[normalizePreferredIdentifierValue(target)]...), nil
	}
	for _, rawTarget := range req.Targets {
		input := strings.TrimSpace(rawTarget.Input)
		if input == "" {
			continue
		}
		if ref, ok := parseNodeRefJSON(input); ok {
			targets = append(targets, parsedResolveTarget{Input: input, Ref: ref})
			continue
		}
		target := unwrapResolveTarget(input)
		// Authored wikilinks still need inventory resolution for omitted
		// extensions and relative spellings before any canonical-path reads.
		isWikilink := strings.HasPrefix(input, "[[") && strings.HasSuffix(input, "]]")
		if !isLikelyMarkdownLink(input) && !isWikilink {
			if parsed, ok := explicitResolveTarget(input, target); ok {
				targets = append(targets, parsed)
				continue
			}
		}
		relativeWiki := isWikilink && (strings.HasPrefix(target, "./") || strings.HasPrefix(target, "../"))
		if relativeWiki {
			notePath, fragment := splitLocatorTarget(target)
			fromPath, _ := splitLocatorTarget(req.FromPath)
			notePath = filepath.ToSlash(filepath.Clean(filepath.Join(filepath.Dir(fromPath), notePath)))
			target = notePath
			if fragment != "" {
				target += "#" + fragment
			}
		}
		var resolved obsidian.ResolvedNoteTarget
		var ok bool
		var candidates []obsidian.ResolvedNoteTarget
		if cache := notePathCache(); cache != nil && isLikelyMarkdownLink(input) {
			resolved, ok = cache.ResolveMdLinkTarget(target, req.FromPath)
			if !ok {
				candidates = cache.ResolveNoteCandidates(target)
			}
		} else if cache != nil {
			candidates = cache.ResolveNoteCandidates(target)
		}
		if relativeWiki {
			notePath, _ := splitLocatorTarget(target)
			exact := candidates[:0]
			for _, candidate := range candidates {
				if candidate.Path == notePath || strings.TrimSuffix(candidate.Path, filepath.Ext(candidate.Path)) == notePath {
					exact = append(exact, candidate)
				}
			}
			candidates = exact
			if len(candidates) == 0 || req.FromPath == "" {
				diagnostics = append(diagnostics, NodeResolveDiagnostic{Input: input, Code: "target_unresolved", Message: "relative link target did not resolve from its source note"})
				continue
			}
		}
		if !ok && len(candidates) == 1 {
			resolved, ok = candidates[0], true
		}
		if len(candidates) > 1 {
			diagnostics = append(diagnostics, NodeResolveDiagnostic{
				Input:      input,
				Code:       "target_ambiguous",
				Message:    "link target resolves to multiple notes",
				Candidates: s.canonicalCandidateRefs(ctx, candidates),
			})
			continue
		}
		if !ok {
			identifierTarget, fragment := splitLocatorTarget(target)
			identifierCandidates, err := preferredIdentifierCandidates(identifierTarget)
			if err != nil {
				diagnostics = append(diagnostics, NodeResolveDiagnostic{
					Input:   input,
					Code:    "target_unresolved",
					Message: fmt.Sprintf("preferred identifier lookup failed: %v", err),
				})
				continue
			}
			if len(identifierCandidates) > 1 {
				diagnostics = append(diagnostics, NodeResolveDiagnostic{
					Input:      input,
					Code:       "target_ambiguous",
					Message:    "preferred identifier resolves to multiple notes",
					Candidates: identifierCandidates,
				})
				continue
			}
			if len(identifierCandidates) == 1 {
				candidate := identifierCandidates[0]
				if fragment == "" {
					targets = append(targets, parsedResolveTarget{
						Input:         input,
						SourceLocator: candidate.NotePath,
						NotePath:      candidate.NotePath,
						Ref:           candidate,
					})
					continue
				}
				resolved = obsidian.ResolvedNoteTarget{Path: candidate.NotePath, Fragment: fragment}
				ok = true
			}
		}
		if !ok {
			notePath, fragment := splitLocatorTarget(target)
			if notePath == "" {
				diagnostics = append(diagnostics, NodeResolveDiagnostic{Input: input, Code: "target_unresolved", Message: "link target did not resolve to a note"})
				continue
			}
			resolved = obsidian.ResolvedNoteTarget{Path: notePath, Fragment: fragment}
		}
		ref := ontology.NodeRef{NotePath: resolved.Path, Kind: ontology.NodeKindNote}
		sourceLocator := resolved.Path
		if strings.TrimSpace(resolved.Fragment) != "" {
			fragment := strings.TrimSpace(resolved.Fragment)
			sourceLocator = resolved.Path + "#" + fragment
			ref = ontology.NodeRef{NotePath: resolved.Path, Fragment: fragment, Kind: ontology.NodeKindEmbedded}
			if structural := strings.TrimPrefix(fragment, "struct:"); structural != fragment {
				ref.Fragment = ""
				ref.Structural = strings.TrimSpace(structural)
				ref.Kind = ontology.NodeKindSection
			}
		}
		targets = append(targets, parsedResolveTarget{
			Input:         input,
			SourceLocator: sourceLocator,
			NotePath:      resolved.Path,
			Fragment:      strings.TrimSpace(resolved.Fragment),
			Ref:           ref,
		})
	}
	return targets, diagnostics
}

type preferredIdentifierPath struct {
	typeName string
	sources  map[string]struct{}
}

func (s *Scope) loadPreferredIdentifierIndex(ctx context.Context) (map[string][]ontology.NodeRef, error) {
	typeNames := make([]string, 0, len(s.service.Schema.Types))
	for typeName := range s.service.Schema.Types {
		typeNames = append(typeNames, typeName)
	}
	sort.Strings(typeNames)

	pathBindings := make(map[string]preferredIdentifierPath)
	sourceSet := make(map[string]struct{})
	for _, typeName := range typeNames {
		noteType := s.service.Schema.Types[typeName]
		if noteType == nil || noteType.Role != ontology.TypeRoleNote {
			continue
		}
		var preferred *ontology.Field
		for _, field := range noteType.Fields {
			if field != nil && field.IsPreferredIdentifier && field.SourceKind == ontology.FieldSourceFrontmatter {
				preferred = field
				break
			}
		}
		if preferred == nil {
			continue
		}
		sourceNames := ontology.FieldSourceNames(preferred)
		if len(sourceNames) == 0 {
			sourceNames = []string{ontology.DefaultPropertyName(preferred.Name, noteType.PropertyCase)}
		}
		sources := make(map[string]struct{}, len(sourceNames))
		for _, source := range sourceNames {
			source = strings.ToLower(strings.TrimSpace(source))
			if source == "" {
				continue
			}
			sources[source] = struct{}{}
			sourceSet[source] = struct{}{}
		}
		if len(sources) == 0 {
			continue
		}
		paths, err := s.service.Store.OntologyPathsByType(ctx, typeName, 0)
		if err != nil {
			return nil, fmt.Errorf("list preferred identifier paths for %s: %w", typeName, err)
		}
		sort.Strings(paths)
		for _, path := range paths {
			path = strings.TrimSpace(path)
			if path == "" {
				continue
			}
			if _, exists := pathBindings[path]; exists {
				continue
			}
			pathBindings[path] = preferredIdentifierPath{typeName: typeName, sources: sources}
		}
	}
	if len(pathBindings) == 0 {
		return map[string][]ontology.NodeRef{}, nil
	}

	paths := make([]string, 0, len(pathBindings))
	for path := range pathBindings {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	sources := make([]string, 0, len(sourceSet))
	for source := range sourceSet {
		sources = append(sources, source)
	}
	sort.Strings(sources)
	rows, err := s.service.Store.CurrentNotePropertyValues(ctx, paths, sources, semdb.NotePropertySourceFrontmatter)
	if err != nil {
		return nil, fmt.Errorf("read preferred identifier values: %w", err)
	}

	byValue := make(map[string][]ontology.NodeRef)
	seen := make(map[string]map[string]struct{})
	for _, row := range rows {
		binding, ok := pathBindings[row.NotePath]
		if !ok {
			continue
		}
		if _, ok := binding.sources[strings.ToLower(strings.TrimSpace(row.PropertyName))]; !ok {
			continue
		}
		value := normalizePreferredIdentifierValue(row.ValueText)
		if value == "" {
			continue
		}
		ref := ontology.NodeRef{NotePath: row.NotePath, Kind: ontology.NodeKindNote, TypeName: binding.typeName}
		key := nodeRefIdentityKey(ref)
		if seen[value] == nil {
			seen[value] = make(map[string]struct{})
		}
		if _, exists := seen[value][key]; exists {
			continue
		}
		seen[value][key] = struct{}{}
		byValue[value] = append(byValue[value], ref)
	}
	for value := range byValue {
		sort.Slice(byValue[value], func(i, j int) bool {
			left, right := byValue[value][i], byValue[value][j]
			if left.NotePath != right.NotePath {
				return left.NotePath < right.NotePath
			}
			return left.TypeName < right.TypeName
		})
	}
	return byValue, nil
}

func normalizePreferredIdentifierValue(value string) string {
	value = strings.TrimSpace(value)
	value = strings.TrimPrefix(value, "^")
	value = strings.TrimSpace(value)
	if strings.HasPrefix(value, "[[") && strings.HasSuffix(value, "]]") {
		value = strings.TrimSuffix(strings.TrimPrefix(value, "[["), "]]")
	}
	if before, _, ok := strings.Cut(value, "|"); ok {
		value = strings.TrimSpace(before)
	}
	return ontology.NormalizeIdentifierSemanticValue(value)
}

func (s *Scope) canonicalCandidateRefs(ctx context.Context, candidates []obsidian.ResolvedNoteTarget) []ontology.NodeRef {
	paths := make([]string, 0, len(candidates))
	locators := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		paths = append(paths, candidate.Path)
		if candidate.Fragment != "" {
			locators = append(locators, candidate.Path+"#"+candidate.Fragment)
		}
	}

	typeRows := map[string]semdb.OntologyNoteTypeRow{}
	if s != nil && s.service != nil && s.service.Store != nil {
		if rows, err := s.service.Store.OntologyTypesByPaths(ctx, paths); err == nil {
			typeRows = rows
		}
	}
	catalogRows := map[string]codeanchor.IntelOntologyNode{}
	if s != nil && s.service != nil && len(locators) > 0 {
		if store, ok := s.service.Store.(CatalogStore); ok {
			if rows, err := store.OntologyNodesBySourceLocators(ctx, locators); err == nil {
				catalogRows = rows
			}
		}
	}

	refs := make([]ontology.NodeRef, 0, len(candidates))
	for _, candidate := range candidates {
		locator := candidate.Path
		if candidate.Fragment != "" {
			locator += "#" + candidate.Fragment
		}
		if row, ok := catalogRows[locator]; ok {
			refs = append(refs, nodeRefFromCatalogRow(row))
			continue
		}
		ref := ontology.NodeRef{
			NotePath: candidate.Path,
			Kind:     ontology.NodeKindNote,
			TypeName: strings.TrimSpace(typeRows[candidate.Path].TypeName),
		}
		if candidate.Fragment != "" {
			ref.Kind = ontology.NodeKindEmbedded
			ref.Fragment = candidate.Fragment
		}
		refs = append(refs, ref)
	}
	return refs
}

func explicitResolveTarget(input string, target string) (parsedResolveTarget, bool) {
	isCode := strings.HasPrefix(strings.TrimSpace(input), "code:")
	if isCode {
		target = strings.TrimPrefix(strings.TrimSpace(target), "code:")
	}
	notePath, fragment := splitLocatorTarget(target)
	nodeID := ""
	if index := strings.Index(target, "#node:"); !isCode && index > 0 {
		notePath = filepath.ToSlash(filepath.Clean(strings.TrimSpace(target[:index])))
		nodeID = strings.TrimSpace(target[index+len("#node:"):])
		fragment = "node:" + nodeID
	}
	if notePath == "" || strings.Contains(notePath, "://") {
		return parsedResolveTarget{}, false
	}
	if !(isCode || nodeID != "" || strings.Contains(notePath, "/") || filepath.IsAbs(notePath)) {
		return parsedResolveTarget{}, false
	}
	ref := ontology.NodeRef{NotePath: notePath, Kind: ontology.NodeKindNote}
	if isCode {
		ref.Kind = "CODE_FILE"
	}
	sourceLocator := notePath
	if fragment != "" {
		sourceLocator = notePath + "#" + fragment
		ref = ontology.NodeRef{NotePath: notePath, Fragment: fragment, Kind: ontology.NodeKindEmbedded}
		if nodeID != "" {
			ref.Fragment = ""
			ref.NodeID = nodeID
			ref.Kind = ontology.NodeKindSection
			sourceLocator = ontology.NodeSourceLocator(ref)
		} else if structural := strings.TrimPrefix(fragment, "struct:"); structural != fragment {
			ref.Fragment = ""
			ref.Structural = strings.TrimSpace(structural)
			ref.Kind = ontology.NodeKindSection
		}
	}
	return parsedResolveTarget{
		Input:         input,
		SourceLocator: sourceLocator,
		NotePath:      notePath,
		Fragment:      fragment,
		Ref:           ref,
	}, true
}

func (s *Scope) notePathCache(ctx context.Context) *obsidian.NotePathCache {
	if s == nil || s.service == nil || s.service.NoteReader == nil {
		return nil
	}
	s.mu.Lock()
	if s.notePathCacheLoaded {
		cache := s.cachedNotePathCache
		s.mu.Unlock()
		return cache
	}
	generation := s.cacheGeneration
	s.mu.Unlock()

	notes, err := s.service.NoteReader.GetNotesList(s.service.VaultDef)
	if err != nil {
		return nil
	}
	var aliases map[string][]string
	if provider, ok := s.service.NoteReader.(obsidian.NoteEntriesProvider); ok {
		if entries, err := provider.NoteEntriesSnapshot(ctx); err == nil {
			aliases = obsidian.AliasesFromNoteEntries(entries)
		}
	}
	if aliases == nil {
		if provider, ok := s.service.Store.(NoteAliasesProvider); ok {
			if rows, err := provider.CurrentNoteAliases(ctx); err == nil {
				if len(rows) > 0 {
					aliases = rows
				}
			}
		}
	}
	cache := obsidian.BuildNotePathCacheWithAliases(notes, aliases)
	s.mu.Lock()
	if s.cacheGeneration == generation {
		s.cachedNotePathCache = cache
		s.notePathCacheLoaded = true
	}
	s.mu.Unlock()
	return cache
}

func (s *Scope) resolveFragmentByProjection(ctx context.Context, notePath string, fragment string) (ontology.NodeRef, error) {
	ref := ontology.NodeRef{NotePath: notePath, Kind: ontology.NodeKindEmbedded, Fragment: fragment}
	fragment = strings.TrimSpace(fragment)
	reservedSelector := false
	if nodeID := strings.TrimSpace(strings.TrimPrefix(fragment, "node:")); nodeID != fragment {
		if nodeID == "" {
			return ontology.NodeRef{}, fmt.Errorf("node id is required in %s", notePath)
		}
		ref.Kind = ontology.NodeKindSection
		ref.Fragment = ""
		ref.NodeID = nodeID
		reservedSelector = true
	} else if structural := strings.TrimPrefix(fragment, "struct:"); structural != fragment {
		ref.Kind = ontology.NodeKindSection
		ref.Fragment = ""
		ref.Structural = strings.TrimSpace(structural)
		reservedSelector = true
	}
	projection, err := s.Projection(ctx, ref)
	if err != nil {
		return ontology.NodeRef{}, err
	}
	if projection == nil || projection.Ref.IsZero() {
		return ontology.NodeRef{}, fmt.Errorf("fragment %q not found in %s", fragment, notePath)
	}
	ref = projection.Ref
	if ref.TypeName == "" {
		ref.TypeName = projection.ResolvedType
	}
	if ref.Fragment == "" && !reservedSelector {
		ref.Fragment = fragment
	}
	return ref, nil
}

func parseNodeRefJSON(input string) (ontology.NodeRef, bool) {
	if !strings.HasPrefix(strings.TrimSpace(input), "{") {
		return ontology.NodeRef{}, false
	}
	var ref ontology.NodeRef
	if err := json.Unmarshal([]byte(input), &ref); err != nil || ref.IsZero() {
		return ontology.NodeRef{}, false
	}
	return ref, true
}

func unwrapResolveTarget(input string) string {
	input = strings.TrimSpace(input)
	if strings.HasPrefix(input, "[[") && strings.HasSuffix(input, "]]") {
		input = strings.TrimSuffix(strings.TrimPrefix(input, "[["), "]]")
		if idx := strings.Index(input, "|"); idx >= 0 {
			input = input[:idx]
		}
		return strings.TrimSpace(input)
	}
	if match := markdownLinkTargetPattern.FindStringSubmatch(input); len(match) == 2 {
		return strings.TrimSpace(match[1])
	}
	if target := unwrapResolveTargetURL(input); target != "" {
		return target
	}
	return input
}

func unwrapResolveTargetURL(input string) string {
	u, err := url.Parse(strings.TrimSpace(input))
	if err != nil || u.Scheme == "" {
		return ""
	}
	values := u.Query()
	for _, key := range []string{"ref", "note", "path", "file"} {
		value := strings.TrimSpace(values.Get(key))
		if value == "" {
			continue
		}
		if fragment := strings.TrimSpace(u.Fragment); fragment != "" && !strings.Contains(value, "#") {
			value += "#" + fragment
		}
		return value
	}
	return ""
}

func isLikelyMarkdownLink(input string) bool {
	return markdownLinkTargetPattern.MatchString(strings.TrimSpace(input))
}

func splitLocatorTarget(target string) (string, string) {
	target = strings.TrimSpace(target)
	if idx := strings.LastIndex(target, "#"); idx > 0 {
		return filepath.ToSlash(filepath.Clean(target[:idx])), strings.TrimSpace(target[idx+1:])
	}
	return filepath.ToSlash(filepath.Clean(target)), ""
}

func nodeRefFromCatalogRow(row codeanchor.IntelOntologyNode) ontology.NodeRef {
	var ref ontology.NodeRef
	if err := json.Unmarshal([]byte(row.NodeRefJSON), &ref); err != nil {
		ref = ontology.NodeRef{
			NotePath: row.NotePath,
			Kind:     ontology.NodeKind(row.NodeKind),
			NodeID:   row.NodeID,
			TypeName: row.TypeName,
			Fragment: row.Fragment,
		}
	}
	if ref.Fragment == "" && row.Fragment != "" {
		ref.Fragment = row.Fragment
	}
	if ref.TypeName == "" {
		ref.TypeName = row.TypeName
	}
	if ref.Kind == "" {
		ref.Kind = ontology.NodeKind(row.NodeKind)
	}
	if ref.Structural == "" {
		ref.Structural = strings.TrimSpace(row.StructuralFingerprint)
	}
	return ref
}
