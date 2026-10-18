package noderead

import (
	"context"
	"fmt"
	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/ontology/readmodel"
	"strings"
)

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
	parsed, diagnostics, err := s.parseResolveTargets(ctx, req)
	if err != nil {
		return ResolveResult{}, err
	}
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
	rowsByLocator := map[string]readmodel.NodeRow{}
	rowsByStructural := map[string]readmodel.NodeRow{}
	catalogStore := s.service.Store
	hasReadStore := catalogStore != nil
	if hasReadStore && len(locators) > 0 {
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
	if hasReadStore && len(structuralPaths) > 0 {
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
		recordByRequest[RefIdentityKey(record.Ref)] = record
	}
	for key, record := range indexLoadedRecordsForRequests(refs, records) {
		recordByRequest[key] = record
	}
	var locatorMap map[string]ontology.NodeLocator
	if req.IncludeLocators || req.EnsureLinkTarget == ontology.EnsureLinkTargetPlan || req.EnsureLinkTarget == ontology.EnsureLinkTargetApply {
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
		key := RefIdentityKey(item.Ref)
		record := recordByRequest[key]
		if record.Path == "" && (item.Ref.Kind == "" || item.Ref.Kind == ontology.NodeKindNote) {
			record = noteRecordByPath[item.Ref.NotePath]
		}
		resolvedRef := item.Ref
		if !record.Ref.IsZero() && (item.Ref.Kind == ontology.NodeKindSection || item.Ref.Kind == ontology.NodeKindEmbedded) {
			resolvedRef = record.Ref
		}
		resolvedKey := RefIdentityKey(resolvedRef)
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
		refresh.IncludeLocators = true
		return s.Resolve(ctx, refresh)
	}
	return result, nil
}

func (s *Scope) applyEmbeddedLinkTargets(ctx context.Context, refs []ontology.NodeRef) (bool, error) {
	embedded := make([]ontology.NodeRef, 0, len(refs))
	for _, ref := range refs {
		ref = NormalizeRef(ref)
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
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cacheGeneration++
	s.initializeCaches()
	s.noteStateLoaded = false
	s.noteRowsLoaded = false
	s.noteRows = nil
	s.allNotePaths = nil
	s.cachedNotePathCache = nil
	s.notePathCacheLoaded = false
	s.overlayIndex = nil
}
