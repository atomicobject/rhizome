package noderead

import (
	"context"
	"fmt"
	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/ontology/readmodel"
	"strings"
)

func (s *Scope) embeddedRecordsWithProfile(ctx context.Context, refs []ontology.NodeRef, profile HydrateProfile) ([]NodeRecord, error) {
	if s.hasOverlay() {
		var overlayRefs []ontology.NodeRef
		var committedRefs []ontology.NodeRef
		for _, ref := range refs {
			if s.overlayHasPath(ref.NotePath) {
				overlayRefs = append(overlayRefs, ref)
			} else {
				committedRefs = append(committedRefs, ref)
			}
		}
		records := make([]NodeRecord, 0, len(refs))
		if len(committedRefs) > 0 {
			committed, err := s.embeddedRecordsWithProfileNoOverlay(ctx, committedRefs, profile)
			if err != nil {
				return nil, err
			}
			records = append(records, committed...)
		}
		if len(overlayRefs) > 0 {
			overlay, missing, err := s.overlayRecordsByRefsLocked(ctx, overlayRefs)
			if err != nil {
				return nil, err
			}
			if len(missing) > 0 {
				fallback, err := s.embeddedRecordsFromProjection(ctx, missing)
				if err != nil {
					return nil, err
				}
				overlay = append(overlay, fallback...)
			}
			records = append(records, overlay...)
		}
		return records, nil
	}
	return s.embeddedRecordsWithProfileNoOverlay(ctx, refs, profile)
}

func (s *Scope) embeddedRecordsWithProfileNoOverlay(ctx context.Context, refs []ontology.NodeRef, profile HydrateProfile) ([]NodeRecord, error) {
	if profileNeedsContent(profile) {
		return s.embeddedRecordsFromProjection(ctx, refs)
	}
	records, _, err := s.embeddedRecordsFromCatalog(ctx, refs)
	if len(records) > 0 {
		s.diagnostics.RecordLoads++
	}
	return records, err
}

func (s *Scope) embeddedRecordsFromCatalog(ctx context.Context, refs []ontology.NodeRef) ([]NodeRecord, []ontology.NodeRef, error) {
	catalogStore := s.service.Store
	if catalogStore == nil {
		return nil, refs, nil
	}
	nodeIDs := make([]string, 0, len(refs))
	locators := make([]string, 0, len(refs))
	positionalPaths := make([]string, 0, len(refs))
	positionalRefs := make([]ontology.NodeRef, 0, len(refs))
	refByNodeID := make(map[string]ontology.NodeRef, len(refs))
	refByLocator := make(map[string]ontology.NodeRef, len(refs))
	for _, ref := range refs {
		ref = NormalizeRef(ref)
		if positionalSourceRefWithStructural(ref) {
			positionalPaths = append(positionalPaths, ref.NotePath)
			positionalRefs = append(positionalRefs, ref)
			continue
		}
		locator := ontology.NodeSourceLocator(ref)
		if locator != "" {
			locators = append(locators, locator)
			refByLocator[locator] = ref
		}
		if strings.TrimSpace(ref.NodeID) != "" {
			nodeIDs = append(nodeIDs, ref.NodeID)
			refByNodeID[ref.NodeID] = ref
		}
	}
	rows := make(map[string]readmodel.NodeRow, len(refs))
	if len(positionalPaths) > 0 {
		byPath, err := catalogStore.OntologyNodesByPaths(ctx, normalizeStrings(positionalPaths))
		if err != nil {
			return nil, nil, err
		}
		byStructural := make(map[string]readmodel.NodeRow, len(byPath))
		ambiguous := make(map[string]bool)
		for _, row := range byPath {
			rowRef := nodeRefFromCatalogRow(row)
			keys := map[string]bool{}
			for _, fingerprint := range []string{rowRef.Structural, row.RelocationFingerprint} {
				if fingerprint == "" {
					continue
				}
				candidate := rowRef
				candidate.Structural = fingerprint
				key := positionalCatalogKey(candidate)
				if keys[key] {
					continue
				}
				keys[key] = true
				if _, exists := byStructural[key]; exists {
					ambiguous[key] = true
					continue
				}
				byStructural[key] = row
			}
		}
		for _, ref := range positionalRefs {
			key := positionalCatalogKey(ref)
			if ambiguous[key] {
				return nil, nil, fmt.Errorf("%w: structural fingerprint matches multiple unanchored items in %s", ontology.ErrAmbiguousSourceSpanIdentity, ref.NotePath)
			}
			if row, ok := byStructural[key]; ok {
				rows[RefIdentityKey(ref)] = row
			}
		}
	}
	if len(nodeIDs) > 0 {
		byID, err := catalogStore.OntologyNodesByIDs(ctx, nodeIDs)
		if err != nil {
			return nil, nil, err
		}
		for nodeID, row := range byID {
			if _, wanted := refByNodeID[nodeID]; wanted {
				rows[RefIdentityKey(refByNodeID[nodeID])] = row
			}
		}
	}
	if len(locators) > 0 {
		byLocator, err := catalogStore.OntologyNodesBySourceLocators(ctx, locators)
		if err != nil {
			return nil, nil, err
		}
		for locator, row := range byLocator {
			if _, wanted := refByLocator[locator]; wanted {
				rows[RefIdentityKey(refByLocator[locator])] = row
			}
		}
		missingLocators := make([]string, 0)
		for _, locator := range locators {
			if _, ok := byLocator[locator]; !ok {
				missingLocators = append(missingLocators, locator)
			}
		}
		if len(missingLocators) > 0 {
			byFragment, err := catalogStore.OntologyNodesByNoteFragments(ctx, missingLocators)
			if err != nil {
				return nil, nil, err
			}
			for locator, row := range byFragment {
				if ref, wanted := refByLocator[locator]; wanted {
					rows[RefIdentityKey(ref)] = row
				}
				if row.SourceLocator != "" {
					if ref, wanted := refByLocator[row.SourceLocator]; wanted {
						rows[RefIdentityKey(ref)] = row
					}
				}
			}
		}
	}
	hosts := make([]string, 0, len(rows))
	for _, row := range rows {
		hosts = append(hosts, row.NotePath)
	}
	if err := s.ensureIssueFlagsLocked(ctx, hosts); err != nil {
		return nil, nil, err
	}
	out := make([]NodeRecord, 0, len(rows))
	missing := make([]ontology.NodeRef, 0)
	fieldValuesByNodeID, err := s.catalogFieldValuesByNodeID(ctx, rows)
	if err != nil {
		return nil, nil, err
	}
	for _, ref := range refs {
		key := RefIdentityKey(NormalizeRef(ref))
		row, ok := rows[key]
		if !ok {
			s.diagnostics.CatalogMisses++
			missing = append(missing, ref)
			continue
		}
		s.diagnostics.CatalogHits++
		record := s.recordFromCatalogRow(row)
		record.Ref = mergeCatalogRefForRequest(ref, record.Ref)
		record.resolvedFrom = append(record.resolvedFrom, NormalizeRef(ref))
		record = recordWithIndexedFieldValues(record, fieldValuesByNodeID[row.NodeID])
		out = append(out, record)
	}
	return out, missing, nil
}

func positionalCatalogKey(ref ontology.NodeRef) string {
	ref = NormalizeRef(ref)
	return ref.NotePath + "\x00" + string(ref.Kind) + "\x00" + ref.Structural
}

func positionalSourceRefWithStructural(ref ontology.NodeRef) bool {
	if strings.TrimSpace(ref.Structural) == "" {
		return false
	}
	fragment := strings.TrimPrefix(strings.TrimSpace(ref.Fragment), "#")
	return strings.HasPrefix(fragment, "item-") || strings.Contains(strings.TrimSpace(ref.NodeID), "#item-")
}
