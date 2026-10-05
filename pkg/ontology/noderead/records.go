package noderead

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	semdb "github.com/atomicobject/rhizome/pkg/anchors/sqlite"
	"github.com/atomicobject/rhizome/pkg/noteformat"
	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/paths"
)

func (s *Scope) recordsForPaths(ctx context.Context, pathsList []string, profile HydrateProfile) ([]NodeRecord, error) {
	pathsList = normalizeStrings(pathsList)
	if len(pathsList) == 0 {
		return []NodeRecord{}, nil
	}
	missing := make([]string, 0, len(pathsList))
	for _, path := range pathsList {
		if s.overlayHasPath(path) {
			missing = append(missing, path)
			continue
		}
		if s.recordKnown[path] {
			s.diagnostics.RecordHits++
			continue
		}
		missing = append(missing, path)
	}
	if len(missing) > 0 {
		var records []NodeRecord
		var err error
		if s.service.Store != nil {
			records, err = s.recordsFromMetadata(ctx, missing)
		}
		if canceled := readCancellation(ctx, err); canceled != nil {
			return nil, canceled
		}
		if err != nil {
			// Property and tag reads may fail after metadata eligibility was cached.
			// Recover by projecting only requested current Markdown sources; an
			// unknown or ineligible source is never sent through the parser.
			eligible := make([]string, 0, len(missing))
			for _, path := range missing {
				if s.overlayHasPath(path) {
					continue
				}
				if !s.noteRowKnown[path] {
					return nil, err
				}
				row, ok := s.noteRowByPath[path]
				if !ok {
					continue
				}
				if row.FormatID != string(noteformat.FormatID("markdown")) || row.Projection.Status != semdb.NoteProjectionStatusCurrent {
					return nil, err
				}
				eligible = append(eligible, path)
			}
			snapshotBy, snapshotErr := s.recordsFromSnapshotsLocked(ctx, eligible)
			if snapshotErr != nil {
				return nil, snapshotErr
			}
			records = make([]NodeRecord, 0, len(eligible))
			for _, path := range missing {
				if record, ok := snapshotBy[path]; ok {
					records = append(records, record)
				}
			}
			if len(snapshotBy) != len(eligible) {
				return nil, err
			}
		}
		if overlayRecords, err := s.overlayNoteRecordsLocked(ctx, missing); err != nil {
			return nil, err
		} else if s.hasOverlay() {
			byPath := make(map[string]NodeRecord, len(records)+len(overlayRecords))
			for _, record := range records {
				byPath[record.Path] = record
			}
			for path := range s.overlayTouchedPathSetLocked(ctx) {
				delete(byPath, path)
			}
			for _, record := range overlayRecords {
				byPath[record.Path] = record
			}
			records = records[:0]
			for _, path := range missing {
				if record, ok := byPath[path]; ok {
					records = append(records, record)
				}
			}
		}
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		for _, record := range records {
			s.recordByPath[record.Path] = record
		}
		for _, path := range missing {
			s.recordKnown[path] = true
		}
		s.diagnostics.RecordLoads++
	}
	if profileNeedsContent(profile) {
		if err := s.ensureContentForPaths(ctx, pathsList); err != nil {
			return nil, err
		}
	}
	out := make([]NodeRecord, 0, len(pathsList))
	for _, path := range pathsList {
		if record, ok := s.recordByPath[path]; ok {
			out = append(out, cloneNodeRecord(record))
		}
	}
	return out, nil
}

func (s *Scope) ensureContentForPaths(ctx context.Context, pathsList []string) error {
	if s == nil || s.service == nil || (s.service.NoteReader == nil && !s.hasOverlay()) {
		return nil
	}
	missing := make([]string, 0, len(pathsList))
	for _, path := range pathsList {
		if s.contentKnown[path] {
			s.diagnostics.ContentHits++
			continue
		}
		missing = append(missing, path)
	}
	if len(missing) == 0 {
		return nil
	}
	for _, path := range missing {
		if err := ctx.Err(); err != nil {
			return err
		}
		content, ok := s.overlayContent(path)
		if !ok && s.overlayHasPath(path) {
			s.contentKnown[path] = true
			continue
		}
		if _, exists := s.recordByPath[path]; !exists {
			s.contentKnown[path] = true
			continue
		}
		if !ok {
			if s.service.NoteReader == nil {
				continue
			}
			var err error
			content, err = s.service.NoteReader.GetContents(s.service.VaultDef, path)
			if canceled := readCancellation(ctx, err); canceled != nil {
				return canceled
			}
			if err != nil {
				return err
			}
		}
		record := s.recordByPath[path]
		record.Content = content
		s.recordByPath[path] = record
		s.contentKnown[path] = true
	}
	s.diagnostics.ContentLoads++
	return nil
}

func profileNeedsContent(profile HydrateProfile) bool {
	return profile == HydrateContent || profile == HydrateWorkspace
}

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
	if optsUseCatalog(profile) {
		// IMPORTANT: catalog rows are the normal embedded-node read path. Projection
		// fallback is only for damaged/stale catalogs or profiles that need source
		// content the catalog does not carry.
		records, missing, err := s.embeddedRecordsFromCatalog(ctx, refs)
		if err != nil {
			return nil, err
		}
		if len(missing) == 0 {
			if len(records) > 0 {
				s.diagnostics.RecordLoads++
			}
			return records, nil
		}
		fallback, err := s.embeddedRecordsFromProjection(ctx, missing)
		if err != nil {
			return nil, err
		}
		if len(fallback) > 0 {
			s.diagnostics.ProjectionFallbacks++
		}
		records = append(records, fallback...)
		if len(records) > 0 {
			s.diagnostics.RecordLoads++
		}
		return records, nil
	}
	return s.embeddedRecordsFromProjection(ctx, refs)
}

func (s *Scope) embeddedRecordsFromCatalog(ctx context.Context, refs []ontology.NodeRef) ([]NodeRecord, []ontology.NodeRef, error) {
	catalogStore, ok := s.service.Store.(CatalogStore)
	if !ok {
		return nil, refs, nil
	}
	nodeIDs := make([]string, 0, len(refs))
	locators := make([]string, 0, len(refs))
	positionalPaths := make([]string, 0, len(refs))
	positionalRefs := make([]ontology.NodeRef, 0, len(refs))
	refByNodeID := make(map[string]ontology.NodeRef, len(refs))
	refByLocator := make(map[string]ontology.NodeRef, len(refs))
	for _, ref := range refs {
		ref = normalizeNodeRef(ref)
		if positionalSourceRefWithStructural(ref) {
			positionalPaths = append(positionalPaths, ref.NotePath)
			positionalRefs = append(positionalRefs, ref)
			continue
		}
		locator := ontology.NodeSourceLocator(ref)
		if locator != "" {
			locators = append(locators, locator)
			refByLocator[locator] = ref
			continue
		}
		if strings.TrimSpace(ref.NodeID) != "" {
			nodeIDs = append(nodeIDs, ref.NodeID)
			refByNodeID[ref.NodeID] = ref
		}
	}
	rows := make(map[string]codeanchor.IntelOntologyNode, len(refs))
	if len(positionalPaths) > 0 {
		byPath, err := catalogStore.OntologyNodesByPaths(ctx, normalizeStrings(positionalPaths))
		if err != nil {
			return nil, nil, err
		}
		byStructural := make(map[string]codeanchor.IntelOntologyNode, len(byPath))
		ambiguous := make(map[string]bool)
		for _, row := range byPath {
			rowRef := nodeRefFromCatalogRow(row)
			if strings.TrimSpace(rowRef.Structural) == "" {
				continue
			}
			key := positionalCatalogKey(rowRef)
			if _, exists := byStructural[key]; exists {
				ambiguous[key] = true
				continue
			}
			byStructural[key] = row
		}
		for _, ref := range positionalRefs {
			key := positionalCatalogKey(ref)
			if ambiguous[key] {
				return nil, nil, fmt.Errorf("%w: structural fingerprint matches multiple unanchored items in %s", ontology.ErrAmbiguousSourceSpanIdentity, ref.NotePath)
			}
			if row, ok := byStructural[key]; ok {
				rows[nodeRefIdentityKey(ref)] = row
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
				rows[nodeRefIdentityKey(refByNodeID[nodeID])] = row
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
				rows[nodeRefIdentityKey(refByLocator[locator])] = row
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
					rows[nodeRefIdentityKey(ref)] = row
				}
				if row.SourceLocator != "" {
					if ref, wanted := refByLocator[row.SourceLocator]; wanted {
						rows[nodeRefIdentityKey(ref)] = row
					}
				}
			}
		}
	}
	out := make([]NodeRecord, 0, len(rows))
	missing := make([]ontology.NodeRef, 0)
	fieldValuesByNodeID, err := s.catalogFieldValuesByNodeID(ctx, rows)
	if err != nil {
		return nil, nil, err
	}
	for _, ref := range refs {
		key := nodeRefIdentityKey(normalizeNodeRef(ref))
		row, ok := rows[key]
		if !ok {
			s.diagnostics.CatalogMisses++
			missing = append(missing, ref)
			continue
		}
		s.diagnostics.CatalogHits++
		record := s.recordFromCatalogRow(row)
		record.Ref = mergeCatalogRefForRequest(ref, record.Ref)
		record = recordWithIndexedFieldValues(record, fieldValuesByNodeID[row.NodeID])
		out = append(out, record)
	}
	return out, missing, nil
}

func positionalCatalogKey(ref ontology.NodeRef) string {
	ref = normalizeNodeRef(ref)
	return ref.NotePath + "\x00" + string(ref.Kind) + "\x00" + ref.Structural
}

func positionalSourceRefWithStructural(ref ontology.NodeRef) bool {
	if strings.TrimSpace(ref.Structural) == "" {
		return false
	}
	fragment := strings.TrimPrefix(strings.TrimSpace(ref.Fragment), "#")
	return strings.HasPrefix(fragment, "item-") || strings.Contains(strings.TrimSpace(ref.NodeID), "#item-")
}

func (s *Scope) catalogFieldValuesByNodeID(ctx context.Context, rows map[string]codeanchor.IntelOntologyNode) (map[string][]codeanchor.IntelOntologyNodeFieldValue, error) {
	if len(rows) == 0 {
		return nil, nil
	}
	catalogStore, ok := s.service.Store.(CatalogStore)
	if !ok {
		return nil, nil
	}
	nodeIDs := make([]string, 0, len(rows))
	seen := map[string]struct{}{}
	for _, row := range rows {
		if strings.TrimSpace(row.NodeID) == "" {
			continue
		}
		if _, ok := seen[row.NodeID]; ok {
			continue
		}
		seen[row.NodeID] = struct{}{}
		nodeIDs = append(nodeIDs, row.NodeID)
	}
	if len(nodeIDs) == 0 {
		return nil, nil
	}
	fieldRows, err := catalogStore.OntologyNodeFieldValuesByNodeIDs(ctx, nodeIDs, nil)
	if err != nil {
		return nil, err
	}
	byNodeID := make(map[string][]codeanchor.IntelOntologyNodeFieldValue, len(nodeIDs))
	for _, row := range fieldRows {
		if strings.TrimSpace(row.NodeID) == "" || strings.TrimSpace(row.FieldName) == "" {
			continue
		}
		byNodeID[row.NodeID] = append(byNodeID[row.NodeID], row)
	}
	return byNodeID, nil
}

// IndexedFieldValues returns the committed indexed field rows, including link
// target refs, for each projection's catalog node in one batched read, keyed
// by RefIdentityKey of the projection ref. Notes touched by the read overlay
// are omitted because their staged values have no index rows.
func (s *Scope) IndexedFieldValues(ctx context.Context, projections []*ontology.NodeProjection) (map[string][]codeanchor.IntelOntologyNodeFieldValue, error) {
	if s == nil || s.service == nil {
		return nil, nil
	}
	keysByNodeID := make(map[string][]string, len(projections))
	for _, projection := range projections {
		if projection == nil || s.overlayHasPath(projection.Ref.NotePath) {
			continue
		}
		node, err := ontology.IntelOntologyNodeForProjection(s.service.Schema, projection, nil, 1)
		if err != nil {
			return nil, err
		}
		if node.NodeID != "" {
			keysByNodeID[node.NodeID] = append(keysByNodeID[node.NodeID], RefIdentityKey(projection.Ref))
		}
	}
	return s.indexedFieldValuesByNodeID(ctx, keysByNodeID)
}

// IndexedRecordFieldValues is IndexedFieldValues for hydrated records, such
// as note roots, whose summary records carry no indexed field rows. It finds
// their catalog nodes by source locator, so a view reads every row's field
// values in two batched reads however many rows it has.
func (s *Scope) IndexedRecordFieldValues(ctx context.Context, records []NodeRecord) (map[string][]codeanchor.IntelOntologyNodeFieldValue, error) {
	if s == nil || s.service == nil {
		return nil, nil
	}
	catalogStore, ok := s.service.Store.(CatalogStore)
	if !ok {
		return nil, nil
	}
	keysByLocator := make(map[string][]string, len(records))
	locators := make([]string, 0, len(records))
	for _, record := range records {
		ref := normalizeNodeRef(record.Ref)
		if ref.IsZero() || s.overlayHasPath(ref.NotePath) {
			continue
		}
		locator := ontology.NodeSourceLocator(ref)
		if _, ok := keysByLocator[locator]; !ok {
			locators = append(locators, locator)
		}
		keysByLocator[locator] = append(keysByLocator[locator], RefIdentityKey(record.Ref))
	}
	if len(locators) == 0 {
		return nil, nil
	}
	nodes, err := catalogStore.OntologyNodesBySourceLocators(ctx, locators)
	if err != nil {
		return nil, err
	}
	keysByNodeID := make(map[string][]string, len(nodes))
	for locator, node := range nodes {
		if node.NodeID != "" {
			keysByNodeID[node.NodeID] = append(keysByNodeID[node.NodeID], keysByLocator[locator]...)
		}
	}
	return s.indexedFieldValuesByNodeID(ctx, keysByNodeID)
}

// IndexedLinkRow finds the indexed row for one authored link value of a
// field. Matching the value text, not just its position, keeps a value edited
// since indexing from borrowing its predecessor's target.
func IndexedLinkRow(rows []codeanchor.IntelOntologyNodeFieldValue, field, value string) (codeanchor.IntelOntologyNodeFieldValue, bool) {
	field, value = strings.TrimSpace(field), strings.TrimSpace(value)
	for _, row := range rows {
		if strings.EqualFold(strings.TrimSpace(row.FieldName), field) && strings.TrimSpace(row.ValueText) == value {
			return row, true
		}
	}
	return codeanchor.IntelOntologyNodeFieldValue{}, false
}

// IndexedLinkTargetRef returns the target the indexer resolved for a link
// field row; false means the link was unresolved when indexed.
func IndexedLinkTargetRef(row codeanchor.IntelOntologyNodeFieldValue) (ontology.NodeRef, bool) {
	if strings.TrimSpace(row.TargetRefJSON) == "" {
		return ontology.NodeRef{}, false
	}
	var ref ontology.NodeRef
	if err := json.Unmarshal([]byte(row.TargetRefJSON), &ref); err != nil || ref.IsZero() {
		return ontology.NodeRef{}, false
	}
	return ref, true
}

func (s *Scope) indexedFieldValuesByNodeID(ctx context.Context, keysByNodeID map[string][]string) (map[string][]codeanchor.IntelOntologyNodeFieldValue, error) {
	catalogStore, ok := s.service.Store.(CatalogStore)
	if !ok || len(keysByNodeID) == 0 {
		return nil, nil
	}
	nodeIDs := make([]string, 0, len(keysByNodeID))
	for nodeID := range keysByNodeID {
		nodeIDs = append(nodeIDs, nodeID)
	}
	sort.Strings(nodeIDs)
	rows, err := catalogStore.OntologyNodeFieldValuesByNodeIDs(ctx, nodeIDs, nil)
	if err != nil {
		return nil, err
	}
	out := make(map[string][]codeanchor.IntelOntologyNodeFieldValue, len(keysByNodeID))
	for _, row := range rows {
		for _, key := range keysByNodeID[row.NodeID] {
			out[key] = append(out[key], row)
		}
	}
	return out, nil
}

func recordWithIndexedFieldValues(record NodeRecord, rows []codeanchor.IntelOntologyNodeFieldValue) NodeRecord {
	if len(rows) == 0 {
		if record.FieldValues == nil {
			record.FieldValues = map[string][]codeanchor.IntelOntologyNodeFieldValue{}
		}
		if record.InlineProps == nil {
			record.InlineProps = map[string][]string{}
		}
		return record
	}
	if record.FieldValues == nil {
		record.FieldValues = map[string][]codeanchor.IntelOntologyNodeFieldValue{}
	}
	if record.InlineProps == nil {
		record.InlineProps = map[string][]string{}
	}
	sort.SliceStable(rows, func(i, j int) bool {
		if rows[i].FieldName != rows[j].FieldName {
			return rows[i].FieldName < rows[j].FieldName
		}
		return rows[i].ListOrdinal < rows[j].ListOrdinal
	})
	for _, row := range rows {
		fieldName := strings.TrimSpace(row.FieldName)
		if fieldName == "" {
			continue
		}
		record.FieldValues[fieldName] = append(record.FieldValues[fieldName], row)
		record.InlineProps[fieldName] = append(record.InlineProps[fieldName], row.ValueText)
	}
	return record
}

func mergeCatalogRefForRequest(requested, catalog ontology.NodeRef) ontology.NodeRef {
	if catalog.IsZero() {
		return requested
	}
	if catalog.Fragment == "" && requested.Fragment != "" {
		catalog.Fragment = requested.Fragment
	}
	if catalog.NodeID == "" && requested.NodeID != "" {
		catalog.NodeID = requested.NodeID
	}
	if catalog.TypeName == "" && requested.TypeName != "" {
		catalog.TypeName = requested.TypeName
	}
	if catalog.Kind == "" && requested.Kind != "" {
		catalog.Kind = requested.Kind
	}
	return catalog
}

func (s *Scope) recordFromCatalogRow(row codeanchor.IntelOntologyNode) NodeRecord {
	ref := nodeRefFromCatalogRow(row)
	linkTarget := catalogLinkTarget(row)
	var nodeLocator *ontology.NodeLocator
	if row.SourceLocator != "" || linkTarget != nil {
		nodeLocator = &ontology.NodeLocator{
			Ref:           ref,
			Kind:          ref.Kind,
			SourceLocator: firstNonEmptyString(row.SourceLocator, ontology.NodeSourceLocator(ref)),
			Status:        ontology.NodeLocatorStatus(firstNonEmptyString(row.LocatorStatus, string(ontology.NodeLocatorLinkable))),
			LinkTarget:    linkTarget,
		}
	}
	return NodeRecord{
		catalogNodeID:    row.NodeID,
		catalogStartByte: int(row.StartByte),
		Ref:              ref,
		Path:             firstNonEmptyString(row.SourceLocator, row.NotePath),
		Title:            firstNonEmptyString(row.DisplayLabel, row.Title, row.SourceLocator, row.NotePath),
		TypeName:         row.TypeName,
		NodeLocator:      nodeLocator,
		LinkTarget:       linkTarget,
		UpdatedAt:        row.UpdatedAt,
		HasIssues:        s.issueByPath[row.NotePath],
	}
}

func catalogLinkTarget(row codeanchor.IntelOntologyNode) *ontology.NodeLinkTarget {
	if row.SourceLocator == "" {
		return nil
	}
	return &ontology.NodeLinkTarget{
		Ref:          nodeRefFromCatalogRow(row),
		Markdown:     row.SourceLocator,
		Wikilink:     "",
		DisplayLabel: firstNonEmptyString(row.DisplayLabel, row.Title),
		Exists:       strings.TrimSpace(row.LocatorStatus) == "" || row.LocatorStatus == string(ontology.NodeLocatorLinkable),
		BlockID:      strings.TrimPrefix(strings.TrimSpace(row.BlockID), "^"),
	}
}

func optsUseCatalog(profile HydrateProfile) bool {
	return profile == "" || profile == HydrateIdentity || profile == HydrateSummary
}

func (s *Scope) embeddedRecordsFromProjection(ctx context.Context, refs []ontology.NodeRef) ([]NodeRecord, error) {
	if len(refs) == 0 {
		return nil, nil
	}
	if s.hasOverlay() {
		overlay, missing, err := s.overlayRecordsByRefsLocked(ctx, refs)
		if err != nil {
			return nil, err
		}
		if len(missing) == 0 {
			if len(overlay) > 0 {
				s.diagnostics.RecordHits += len(overlay)
			}
			return overlay, nil
		}
		if len(overlay) > 0 {
			tail, err := s.embeddedRecordsFromProjectionNoOverlay(ctx, missing)
			if err != nil {
				return nil, err
			}
			return append(overlay, tail...), nil
		}
		refs = missing
	}
	return s.embeddedRecordsFromProjectionNoOverlay(ctx, refs)
}

func (s *Scope) embeddedRecordsFromProjectionNoOverlay(ctx context.Context, refs []ontology.NodeRef) ([]NodeRecord, error) {
	if len(refs) == 0 {
		return nil, nil
	}
	// Group refs by host note so each note's DocumentSnapshot is loaded and parsed
	// at most once per Hydrate call. Without this, hydrating N embedded refs from
	// the same note re-reads + re-parses the file N times.
	type pendingRef struct {
		original ontology.NodeRef
		index    int
	}
	pendingByPath := make(map[string][]pendingRef, len(refs))
	projections := make([]*ontology.NodeProjection, len(refs))
	hits := 0
	for i, ref := range refs {
		ref = normalizeNodeRef(ref)
		if ref.IsZero() {
			continue
		}
		key := projectionCacheKey(ref)
		if s.projectionKnown[key] {
			projections[i] = s.projectionByRef[key]
			hits++
			continue
		}
		pendingByPath[ref.NotePath] = append(pendingByPath[ref.NotePath], pendingRef{original: ref, index: i})
	}
	pendingPaths := make([]string, 0, len(pendingByPath))
	for path := range pendingByPath {
		pendingPaths = append(pendingPaths, path)
	}
	if len(pendingPaths) > 0 && s.service.Store != nil {
		// Batch the normal path, but retain per-host snapshot error tolerance below.
		// A failed batch is retried per host and marks only that host unsupported.
		if canceled := readCancellation(ctx, s.ensurePathStateLocked(ctx, pendingPaths)); canceled != nil {
			return nil, canceled
		}
	}
	for path, pending := range pendingByPath {
		snapshot, err := s.snapshotForPathLocked(ctx, path)
		if canceled := readCancellation(ctx, err); canceled != nil {
			return nil, canceled
		}
		if err != nil {
			// Per-ref error tolerance preserves the legacy behavior of skipping
			// individual unprojectable refs without failing the whole hydrate.
			for _, p := range pending {
				s.projectionKnown[projectionCacheKey(p.original)] = true
			}
			s.diagnostics.UnsupportedRefs += len(pending)
			continue
		}
		if snapshot == nil {
			for _, p := range pending {
				s.projectionKnown[projectionCacheKey(p.original)] = true
			}
			s.diagnostics.UnsupportedRefs += len(pending)
			continue
		}
		for _, p := range pending {
			projection, err := ontology.ProjectNodeFromSnapshot(snapshot, s.service.Schema, p.original)
			if canceled := readCancellation(ctx, err); canceled != nil {
				return nil, canceled
			}
			if errors.Is(err, ontology.ErrAmbiguousSourceSpanIdentity) {
				return nil, err
			}
			if err != nil || projection == nil {
				s.projectionKnown[projectionCacheKey(p.original)] = true
				s.diagnostics.UnsupportedRefs++
				continue
			}
			if fallback, ok := ontology.AsFallbackNoteProjection(projection); ok {
				projection = fallback
			} else if fallback, ok := ontology.AsFallbackSectionProjection(projection); ok {
				projection = fallback
			}
			projections[p.index] = projection
			s.projectionKnown[projectionCacheKey(p.original)] = true
			s.projectionByRef[projectionCacheKey(p.original)] = projection
			canonicalKey := projectionCacheKey(projection.Ref)
			s.projectionKnown[canonicalKey] = true
			s.projectionByRef[canonicalKey] = projection
		}
	}
	out := make([]NodeRecord, 0, len(refs))
	canonicalRefs := make([]ontology.NodeRef, 0, len(refs))
	for _, projection := range projections {
		if projection == nil {
			continue
		}
		canonicalRefs = append(canonicalRefs, projection.Ref)
	}
	locators, err := s.locatorsLocked(ctx, canonicalRefs)
	if err != nil {
		return nil, err
	}
	for i, projection := range projections {
		if projection == nil {
			continue
		}
		record := s.nodeRecordFromProjectionLocked(ctx, projection)
		record.resolvedFrom = append(record.resolvedFrom, normalizeNodeRef(refs[i]))
		if nodeLocator := locators[nodeRefIdentityKey(projection.Ref)]; nodeLocator != nil {
			record.NodeLocator = nodeLocator
			record.LinkTarget = nodeLocator.LinkTarget
		}
		out = append(out, record)
	}
	s.diagnostics.RecordHits += hits
	if len(out) > 0 {
		s.diagnostics.RecordLoads++
	}
	return out, nil
}

func (s *Scope) nodeRecordFromProjectionLocked(ctx context.Context, projection *ontology.NodeProjection) NodeRecord {
	_ = ctx
	title := projection.Ref.String()
	if workspace := ontology.BuildNodeWorkspaceFromProjectionWithSchema(s.service.Schema, projection); workspace != nil {
		title = workspace.Node.Title
	}
	content := ""
	if projection.Ref.StartByte >= 0 && projection.Ref.EndByte > projection.Ref.StartByte && projection.Ref.EndByte <= len(projection.Snapshot.Content) {
		content = strings.TrimSpace(projection.Snapshot.Content[projection.Ref.StartByte:projection.Ref.EndByte])
	}
	// FieldValues is intentionally left nil on projection-backed records:
	// catalog field rows are produced by ontology sync, while overlay/projection
	// records are derived from preview markdown. Frontmatter dates use the
	// catalog's text spelling so staged and committed records agree.
	record := NodeRecord{
		Ref:             projection.Ref,
		Path:            projection.Ref.NotePath,
		Title:           title,
		Content:         content,
		Frontmatter:     frontmatterWithTemporalText(projection.Snapshot.Frontmatter),
		InlineProps:     projectionInlineProps(projection),
		FieldValues:     nil,
		projectedFields: captureProjectedFieldValues(projection),
		TypeName:        projection.ResolvedType,
		UpdatedAt:       s.noteRowByPath[projection.Ref.NotePath].Mtime,
		HasIssues:       s.issueByPath[projection.Ref.NotePath],
	}
	return s.withProjectionSource(record, projection)
}

func captureProjectedFieldValues(projection *ontology.NodeProjection) map[string]projectedFieldValues {
	values := make(map[string]projectedFieldValues, len(projection.Fields))
	for name, binding := range projection.Fields {
		values[name] = projectedFieldValues{values: append([]string(nil), binding.Values...), present: binding.Present}
	}
	return values
}

// frontmatterWithTemporalText copies snapshot frontmatter with YAML dates and
// timestamps as metadata text, the spelling catalog-backed records carry.
// ponytail: classifies by value (midnight is a date), so an authored midnight
// timestamp reads as a date; carry YAML spelling in the snapshot if that bites.
func frontmatterWithTemporalText(in map[string]any) map[string]any {
	if in == nil {
		return nil
	}
	out := make(map[string]any, len(in))
	for key, value := range in {
		out[key] = temporalText(value)
	}
	return out
}

func temporalText(value any) any {
	switch typed := value.(type) {
	case time.Time:
		if metadata, err := noteformat.NewMetadataValue(typed); err == nil {
			return metadata.Export()
		}
	case []any:
		out := make([]any, len(typed))
		for index, item := range typed {
			out[index] = temporalText(item)
		}
		return out
	case map[string]any:
		return frontmatterWithTemporalText(typed)
	}
	return value
}

// snapshotForPathLocked returns a cached DocumentSnapshot for a note path, loading
// once on first miss. The caller must hold s.mu.
func (s *Scope) snapshotForPathLocked(ctx context.Context, notePath string) (*ontology.DocumentSnapshot, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if strings.TrimSpace(notePath) == "" {
		return nil, nil
	}
	if s.hasOverlay() {
		index, err := s.overlayIndexLocked(ctx)
		if err != nil {
			return nil, err
		}
		if index != nil {
			path := strings.TrimSpace(notePath)
			if _, deleted := index.DeletedPaths[path]; deleted {
				return nil, nil
			}
			if snapshot, ok := index.SnapshotsByPath[path]; ok {
				return snapshot, nil
			}
		}
	}
	if note, ok := s.overlayNote(notePath); ok {
		key := "overlay:" + strings.TrimSpace(notePath)
		if cached, ok := s.snapshotByPath[key]; ok {
			return cached, nil
		}
		if err, ok := s.snapshotKnownErr[key]; ok {
			return nil, err
		}
		if note.Deleted {
			s.snapshotByPath[key] = nil
			return nil, nil
		}
		if !s.activeOverlay().IsMarkdownSource() {
			s.snapshotByPath[key] = nil
			return nil, nil
		}
		snapshot, err := ontology.BuildDocumentSnapshot(strings.TrimSpace(notePath), note.Content, time.Unix(note.UpdatedAt, 0))
		if canceled := readCancellation(ctx, err); canceled != nil {
			return nil, canceled
		}
		if err != nil {
			s.snapshotKnownErr[key] = err
			return nil, err
		}
		s.snapshotByPath[key] = snapshot
		return snapshot, nil
	}
	if cached, ok := s.snapshotByPath[notePath]; ok {
		return cached, nil
	}
	if err, ok := s.snapshotKnownErr[notePath]; ok {
		return nil, err
	}
	if s.service.Store == nil {
		// A raw NoteReader has no provider identity. Do not make it an implicit
		// Markdown parser route.
		s.snapshotByPath[notePath] = nil
		return nil, nil
	}
	if err := s.ensurePathStateLocked(ctx, []string{notePath}); err != nil {
		return nil, err
	}
	row, ok := s.noteRowByPath[notePath]
	if !ok || row.FormatID != string(noteformat.FormatID("markdown")) || row.Projection.Status != semdb.NoteProjectionStatusCurrent {
		// Descriptor-only and future provider sources must not reach the
		// Markdown snapshot adapter.
		s.snapshotByPath[notePath] = nil
		return nil, nil
	}
	snapshot, err := ontology.LoadDocumentSnapshot(ctx, s.service.VaultDef, s.service.NoteReader, notePath)
	if canceled := readCancellation(ctx, err); canceled != nil {
		return nil, canceled
	}
	if err != nil {
		s.snapshotKnownErr[notePath] = err
		return nil, err
	}
	s.snapshotByPath[notePath] = snapshot
	return snapshot, nil
}

func (s *Scope) hasOverlay() bool {
	if s == nil {
		return false
	}
	overlay := s.activeOverlay()
	if overlay == nil {
		return false
	}
	return !overlay.Empty()
}

func (s *Scope) overlayIndexLocked(ctx context.Context) (*ReadOverlayIndex, error) {
	overlay := s.activeOverlay()
	if overlay == nil || overlay.Empty() {
		return nil, nil
	}
	if overlay.Index != nil {
		return overlay.Index, nil
	}
	if s.overlayIndex != nil {
		return s.overlayIndex, nil
	}
	index, err := BuildReadOverlayIndex(ctx, s.service.Schema, overlay)
	if canceled := readCancellation(ctx, err); canceled != nil {
		return nil, canceled
	}
	if err != nil {
		return nil, err
	}
	s.overlayIndex = index
	return index, nil
}

func (s *Scope) activeOverlay() *ReadOverlay {
	if s == nil {
		return nil
	}
	return s.opts.ReadOverlay
}

func (s *Scope) overlayNote(notePath string) (ReadOverlayNote, bool) {
	if !s.hasOverlay() {
		return ReadOverlayNote{}, false
	}
	overlay := s.activeOverlay()
	path := strings.TrimSpace(notePath)
	if note, ok := overlay.Notes[path]; ok {
		return note, true
	}
	if content, ok := overlay.UpdatedContentByPath[path]; ok {
		return ReadOverlayNote{Content: content}, true
	}
	return ReadOverlayNote{}, false
}

func (s *Scope) overlayContent(notePath string) (string, bool) {
	note, ok := s.overlayNote(notePath)
	if !ok || note.Deleted {
		return "", false
	}
	return note.Content, true
}

func (s *Scope) overlayHasPath(notePath string) bool {
	if !s.hasOverlay() {
		return false
	}
	path := strings.TrimSpace(notePath)
	overlay := s.activeOverlay()
	if overlay != nil && overlay.Index != nil {
		_, ok := overlay.Index.TouchedPaths[path]
		return ok
	}
	if _, ok := s.overlayNote(path); ok {
		return true
	}
	for _, touched := range overlay.TouchedPaths {
		if strings.TrimSpace(touched) == path {
			return true
		}
	}
	return false
}

func (s *Scope) overlayNoteRecordsLocked(ctx context.Context, pathsList []string) ([]NodeRecord, error) {
	if !s.hasOverlay() {
		return nil, nil
	}
	index, err := s.overlayIndexLocked(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]NodeRecord, 0, len(pathsList))
	for _, path := range pathsList {
		if !s.overlayHasPath(path) {
			continue
		}
		if index != nil {
			key := nodeRefIdentityKey(ontology.NodeRef{NotePath: path, Kind: ontology.NodeKindNote})
			if record, ok := index.RecordsByRef[key]; ok {
				out = append(out, s.enrichOverlayRecordLocked(record))
				continue
			}
			if _, deleted := index.DeletedPaths[strings.TrimSpace(path)]; deleted {
				continue
			}
		}
		snapshot, err := s.snapshotForPathLocked(ctx, path)
		if err != nil {
			return nil, err
		}
		if snapshot == nil {
			continue
		}
		projection, err := ontology.ProjectNodeFromSnapshot(snapshot, s.service.Schema, ontology.NodeRef{NotePath: path, Kind: ontology.NodeKindNote})
		if err != nil || projection == nil {
			continue
		}
		if fallback, ok := ontology.AsFallbackNoteProjection(projection); ok {
			projection = fallback
		} else if fallback, ok := ontology.AsFallbackSectionProjection(projection); ok {
			projection = fallback
		}
		out = append(out, s.nodeRecordFromProjectionLocked(ctx, projection))
	}
	return out, nil
}

func (s *Scope) overlayRecordsByRefsLocked(ctx context.Context, refs []ontology.NodeRef) ([]NodeRecord, []ontology.NodeRef, error) {
	index, err := s.overlayIndexLocked(ctx)
	if err != nil || index == nil {
		return nil, refs, err
	}
	touchedMissing := make([]ontology.NodeRef, 0)
	touchedPaths := make(map[string]struct{})
	for _, ref := range refs {
		ref = normalizeNodeRef(ref)
		if _, deleted := index.DeletedPaths[ref.NotePath]; deleted {
			continue
		}
		if _, touched := index.TouchedPaths[ref.NotePath]; !touched {
			continue
		}
		if _, ok := index.RecordsByRef[nodeRefIdentityKey(ref)]; ok {
			continue
		}
		touchedMissing = append(touchedMissing, ref)
		touchedPaths[ref.NotePath] = struct{}{}
	}
	aliases := map[string]NodeRecord{}
	if len(touchedMissing) > 0 {
		candidates := make([]NodeRecord, 0)
		for _, record := range index.RecordsByRef {
			if _, ok := touchedPaths[record.Ref.NotePath]; ok {
				candidates = append(candidates, record)
			}
		}
		aliases = indexLoadedRecordsForRequests(touchedMissing, candidates)
	}
	out := make([]NodeRecord, 0, len(refs))
	missing := make([]ontology.NodeRef, 0)
	for _, ref := range refs {
		ref = normalizeNodeRef(ref)
		if _, deleted := index.DeletedPaths[ref.NotePath]; deleted {
			continue
		}
		if _, touched := index.TouchedPaths[ref.NotePath]; !touched {
			missing = append(missing, ref)
			continue
		}
		key := nodeRefIdentityKey(ref)
		if record, ok := index.RecordsByRef[key]; ok {
			out = append(out, s.enrichOverlayRecordLocked(record))
			continue
		}
		if record, ok := aliases[key]; ok {
			out = append(out, s.enrichOverlayRecordLocked(record))
			continue
		}
		missing = append(missing, ref)
	}
	return out, missing, nil
}

func (s *Scope) enrichOverlayRecordLocked(record NodeRecord) NodeRecord {
	record = cloneNodeRecord(record)
	if record.Ref.NotePath == "" {
		return record
	}
	if row, ok := s.noteRowByPath[record.Ref.NotePath]; ok {
		if record.UpdatedAt == 0 {
			record.UpdatedAt = row.Mtime
		}
		if record.Ref.Kind == ontology.NodeKindNote {
			record.HasIssues = s.issueByPath[record.Ref.NotePath]
		}
	}
	if record.HasIssues == false && s.issueByPath[record.Ref.NotePath] {
		record.HasIssues = true
	}
	if record.Ref.Kind == ontology.NodeKindNote && sourceContractEmpty(record) {
		record = s.withSourceContract(record)
	}
	return record
}

func projectionInlineProps(projection *ontology.NodeProjection) map[string][]string {
	if projection == nil || len(projection.Fields) == 0 {
		return nil
	}
	out := map[string][]string{}
	for name, binding := range projection.Fields {
		if strings.TrimSpace(name) == "" || len(binding.Values) == 0 {
			continue
		}
		if field := projection.Type.ByName[name]; ontology.IsSectionSummary(field) {
			out[name] = ontology.SectionSummaryValues(projection, field)
		} else {
			out[name] = append([]string(nil), binding.Values...)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// Locators returns cached source locators for refs without mutating note files.
func (s *Scope) Locators(ctx context.Context, refs []ontology.NodeRef) (map[string]ontology.NodeLocator, error) {
	return s.LocatorsWithEnsure(ctx, refs, ontology.EnsureLinkTargetNever)
}

// LocatorsWithEnsure returns source locators and may plan or apply safe link
// target fixes depending on ensure.
func (s *Scope) LocatorsWithEnsure(ctx context.Context, refs []ontology.NodeRef, ensure ontology.EnsureLinkTargetMode) (map[string]ontology.NodeLocator, error) {
	if s == nil || s.service == nil {
		return map[string]ontology.NodeLocator{}, nil
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if ensure == ontology.EnsureLinkTargetApply {
		if _, err := s.applyEmbeddedLinkTargets(ctx, refs); err != nil {
			return nil, err
		}
		ensure = ontology.EnsureLinkTargetNever
	}
	if ensure != ontology.EnsureLinkTargetNever {
		return s.locatorsWithEnsure(ctx, refs, ensure)
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	locators, err := s.locatorsLocked(ctx, refs)
	if err != nil {
		return nil, err
	}
	out := make(map[string]ontology.NodeLocator, len(locators))
	for _, ref := range refs {
		ref = normalizeNodeRef(ref)
		locator, ok := locators[nodeRefIdentityKey(ref)]
		if !ok || locator == nil {
			continue
		}
		out[locator.SourceLocator] = *locator
		out[nodereadLocatorKey(ref)] = *locator
	}
	return out, nil
}

func (s *Scope) locatorsWithEnsure(ctx context.Context, refs []ontology.NodeRef, ensure ontology.EnsureLinkTargetMode) (map[string]ontology.NodeLocator, error) {
	if s == nil || s.service == nil || s.service.Schema == nil || s.service.NoteReader == nil {
		return map[string]ontology.NodeLocator{}, nil
	}
	result, err := (&ontology.NodeLinkService{
		VaultDef:   s.service.VaultDef,
		NoteReader: s.service.NoteReader,
		Schema:     s.service.Schema,
	}).Locators(ctx, ontology.LinkTargetRequest{
		Refs:   refs,
		Ensure: ensure,
	})
	if canceled := readCancellation(ctx, err); canceled != nil {
		return nil, canceled
	}
	return result, err
}

func (s *Scope) locatorsLocked(ctx context.Context, refs []ontology.NodeRef) (map[string]*ontology.NodeLocator, error) {
	out := make(map[string]*ontology.NodeLocator, len(refs))
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if s == nil || s.service == nil || s.service.Schema == nil {
		return out, nil
	}
	missing := make([]ontology.NodeRef, 0, len(refs))
	identityKeysByLocatorKey := make(map[string][]string, len(refs))
	for _, ref := range refs {
		ref = normalizeNodeRef(ref)
		if ref.IsZero() {
			continue
		}
		identityKey := nodeRefIdentityKey(ref)
		if locator, ok := s.locatorByRef[identityKey]; ok {
			locatorCopy := locator
			out[identityKey] = &locatorCopy
			continue
		}
		if s.locatorKnown[identityKey] {
			continue
		}
		locatorKey := nodereadLocatorKey(ref)
		identityKeysByLocatorKey[locatorKey] = append(identityKeysByLocatorKey[locatorKey], identityKey)
		missing = append(missing, ref)
	}
	if len(missing) == 0 {
		return out, nil
	}
	if s.service.NoteReader == nil {
		return out, nil
	}
	result, err := (&ontology.NodeLinkService{
		VaultDef:   s.service.VaultDef,
		NoteReader: s.service.NoteReader,
		Schema:     s.service.Schema,
	}).Locators(ctx, ontology.LinkTargetRequest{
		Refs:   missing,
		Ensure: ontology.EnsureLinkTargetNever,
	})
	// NodeLinkService can translate a canceled source read into diagnostics.
	// Check the context before publishing those locators as completed results.
	if canceled := readCancellation(ctx, err); canceled != nil {
		return nil, canceled
	}
	if err != nil {
		return out, nil
	}
	for _, ref := range missing {
		s.locatorKnown[nodeRefIdentityKey(ref)] = true
	}
	for locatorKey, locator := range result {
		for _, identityKey := range identityKeysByLocatorKey[locatorKey] {
			s.locatorByRef[identityKey] = locator
			locatorCopy := locator
			out[identityKey] = &locatorCopy
		}
	}
	return out, nil
}

func nodereadLocatorKey(ref ontology.NodeRef) string {
	ref.NotePath = strings.TrimSpace(ref.NotePath)
	ref.Fragment = strings.TrimSpace(ref.Fragment)
	ref.NodeID = strings.TrimSpace(ref.NodeID)
	ref.Structural = strings.TrimSpace(ref.Structural)
	if ref.Kind == ontology.NodeKindNote {
		return ref.NotePath
	}
	if ref.Fragment != "" {
		return ref.String()
	}
	if ref.NodeID != "" {
		if strings.HasPrefix(ref.NodeID, ref.NotePath+"#") {
			return ref.NodeID
		}
		return ref.NotePath + "#node:" + ref.NodeID
	}
	if ref.Structural != "" {
		return ref.NotePath + "#struct:" + ref.Structural
	}
	return ref.String()
}

// Projection returns a request-scoped cached ontology projection for a NodeRef.
// It gives non-GraphQL consumers such as semantic indexing the same
// projection cache used by hydration and embedded-node enumeration.
func (s *Scope) Projection(ctx context.Context, ref ontology.NodeRef) (*ontology.NodeProjection, error) {
	if s == nil || s.service == nil {
		return nil, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.projectionLocked(ctx, ref)
}

func (s *Scope) projectionLocked(ctx context.Context, ref ontology.NodeRef) (*ontology.NodeProjection, error) {
	ref = normalizeNodeRef(ref)
	if ref.IsZero() {
		return nil, nil
	}
	key := projectionCacheKey(ref)
	if s.projectionKnown[key] {
		return s.projectionByRef[key], nil
	}
	if ref.Kind == ontology.NodeKindNote || (ref.Fragment == "" && ref.NodeID == "") {
		projection, handled, err := s.providerRootProjectionLocked(ctx, ref)
		if err != nil {
			return nil, err
		}
		if handled {
			s.projectionKnown[key] = true
			s.projectionByRef[key] = projection
			return projection, nil
		}
	}
	// IMPORTANT: projectionCacheKey is richer than NodeRef.String so note-root,
	// embedded, section, and structural fallback refs never share a projection.
	// Route through the per-note snapshot cache so callers projecting many refs
	// from the same host note pay one parse, not N.
	snapshot, err := s.snapshotForPathLocked(ctx, ref.NotePath)
	if err != nil {
		return nil, err
	}
	if snapshot == nil {
		s.projectionKnown[key] = true
		return nil, nil
	}
	projection, err := ontology.ProjectNodeFromSnapshot(snapshot, s.service.Schema, ref)
	if err != nil {
		return nil, err
	}
	if fallback, ok := ontology.AsFallbackNoteProjection(projection); ok {
		projection = fallback
	} else if fallback, ok := ontology.AsFallbackSectionProjection(projection); ok {
		projection = fallback
	}
	s.projectionKnown[key] = true
	if projection != nil {
		s.projectionByRef[key] = projection
		canonicalKey := projectionCacheKey(projection.Ref)
		s.projectionKnown[canonicalKey] = true
		s.projectionByRef[canonicalKey] = projection
	}
	return projection, nil
}

func (s *Scope) providerRootProjectionLocked(ctx context.Context, ref ontology.NodeRef) (*ontology.NodeProjection, bool, error) {
	if err := s.ensurePathStateLocked(ctx, []string{ref.NotePath}); err != nil {
		return nil, false, err
	}
	row, ok := s.noteRowByPath[ref.NotePath]
	if !ok || row.Projection.Status != semdb.NoteProjectionStatusCurrent || row.FormatID == string(noteformat.FormatID("markdown")) {
		return nil, false, nil
	}
	format := noteformat.FormatID(row.FormatID)
	provider, ok := s.service.NoteFormats.Provider(format)
	if !ok || !s.service.NoteFormats.CanProject(format) || s.service.NoteReader == nil {
		return nil, true, nil
	}
	content, err := s.service.NoteReader.GetContents(s.service.VaultDef, ref.NotePath)
	if err != nil {
		return nil, true, err
	}
	notePath, err := paths.CleanNotePath(ref.NotePath)
	if err != nil {
		return nil, true, err
	}
	source, err := noteformat.NewAuthoredSource(notePath, provider.Descriptor(), []byte(content), row.Mtime)
	if err != nil {
		return nil, true, err
	}
	projected, err := s.service.NoteFormats.Project(source)
	if err != nil {
		return nil, true, err
	}
	metadata := make(map[string]any, len(projected.Facts.RootMetadata))
	for _, fact := range projected.Facts.RootMetadata {
		metadata[fact.Key] = fact.Value.Export()
	}
	title := row.Title
	if projected.Facts.Title != nil && strings.TrimSpace(projected.Facts.Title.Value) != "" {
		title = strings.TrimSpace(projected.Facts.Title.Value)
	}
	root := &ontology.RootDocumentSnapshot{
		NotePath: notePath, Format: format, RawSource: []byte(content),
		ContentHash: row.ContentHash, Mtime: row.Mtime,
		SourceRepresentation:   ontology.SourceRepresentationUTF8,
		EvidenceRepresentation: ontology.EvidenceRepresentationProviderProjection,
		Projection:             projected.Copy(), Title: title, Metadata: metadata,
		Root:          ontology.NodeRef{NotePath: ref.NotePath, Kind: ontology.NodeKindNote},
		RootMetadata:  append([]noteformat.RootMetadataFact(nil), projected.Facts.RootMetadata...),
		SearchRegions: append([]noteformat.SearchRegionFact(nil), projected.Facts.SearchRegions...),
		Diagnostics:   append([]noteformat.Diagnostic(nil), projected.Diagnostics...),
	}
	projection, err := ontology.ProjectRootDocumentSnapshot(root, s.service.Schema)
	if fallback, ok := ontology.AsFallbackNoteProjection(projection); ok {
		projection = fallback
	}
	return projection, true, err
}

func (s *Scope) recordsFromMetadata(ctx context.Context, pathsList []string) ([]NodeRecord, error) {
	if err := s.loadPathMetadataLocked(ctx, pathsList); err != nil {
		return nil, err
	}
	rowsByPath := make(map[string]semdb.NoteMetadataRow, len(pathsList))
	for _, path := range pathsList {
		if row, ok := s.noteRowByPath[path]; ok {
			rowsByPath[path] = row
		}
	}
	if len(rowsByPath) == 0 {
		return nil, nil
	}
	knownPaths := make([]string, 0, len(rowsByPath))
	seenPaths := make(map[string]struct{}, len(rowsByPath))
	for _, path := range pathsList {
		if _, ok := rowsByPath[path]; !ok {
			continue
		}
		if _, duplicate := seenPaths[path]; duplicate {
			continue
		}
		seenPaths[path] = struct{}{}
		knownPaths = append(knownPaths, path)
	}
	loadProperties := s.service.Store.CurrentNotePropertyValues
	if s.service.ExactNotePropertyValues != nil {
		loadProperties = s.service.ExactNotePropertyValues
	}
	propRows, err := loadProperties(ctx, knownPaths, nil, 0)
	if err != nil {
		return nil, err
	}
	loadTags := s.service.Store.CurrentNoteTags
	if s.service.ExactNoteTags != nil {
		loadTags = s.service.ExactNoteTags
	}
	tagRows, err := loadTags(ctx, knownPaths)
	if err != nil {
		return nil, err
	}
	typeRows, err := s.typesByPathsLocked(ctx, knownPaths)
	if err != nil {
		return nil, err
	}
	assessments, err := s.assessmentsByPathsLocked(ctx, knownPaths)
	if err != nil {
		return nil, err
	}
	recordsByPath := make(map[string]NodeRecord, len(rowsByPath))
	for _, path := range pathsList {
		row, ok := rowsByPath[path]
		if !ok {
			continue
		}
		recordsByPath[path] = NodeRecord{
			Ref:                    ontology.NodeRef{NotePath: path, Kind: ontology.NodeKindNote},
			Path:                   path,
			Title:                  firstNonEmptyString(row.Title, noteTitle(path, nil)),
			Frontmatter:            map[string]any{},
			InlineProps:            map[string][]string{},
			Tags:                   []string{},
			UpdatedAt:              row.Mtime,
			Format:                 noteformat.FormatID(row.FormatID),
			SourceRepresentation:   ontology.SourceRepresentationUTF8,
			EvidenceRepresentation: ontology.EvidenceRepresentationProviderProjection,
			Capabilities:           s.sourceCapabilities(row),
		}
	}
	frontmatterLists := make(map[string]map[string][]semdb.NotePropertyValueRow)
	for _, row := range propRows {
		record, ok := recordsByPath[row.NotePath]
		if !ok {
			continue
		}
		switch row.Source {
		case semdb.NotePropertySourceInline:
			record.InlineProps[row.PropertyName] = append(record.InlineProps[row.PropertyName], row.ValueText)
		default:
			if row.IsList {
				if frontmatterLists[row.NotePath] == nil {
					frontmatterLists[row.NotePath] = make(map[string][]semdb.NotePropertyValueRow)
				}
				frontmatterLists[row.NotePath][row.PropertyName] = append(frontmatterLists[row.NotePath][row.PropertyName], row)
				recordsByPath[row.NotePath] = record
				continue
			}
			record.Frontmatter[row.PropertyName] = propertyValue(row)
		}
		recordsByPath[row.NotePath] = record
	}
	for path, byName := range frontmatterLists {
		record, ok := recordsByPath[path]
		if !ok {
			continue
		}
		for name, rows := range byName {
			sort.Slice(rows, func(i, j int) bool { return rows[i].ListOrdinal < rows[j].ListOrdinal })
			values := make([]any, 0, len(rows))
			for _, row := range rows {
				if semdb.IsEmptyListSentinel(row) {
					continue
				}
				values = append(values, propertyValue(row))
			}
			record.Frontmatter[name] = values
		}
		recordsByPath[path] = record
	}
	for _, row := range tagRows {
		record, ok := recordsByPath[row.NotePath]
		if !ok {
			continue
		}
		record.Tags = append(record.Tags, row.TagNorm)
		recordsByPath[row.NotePath] = record
	}
	out := make([]NodeRecord, 0, len(pathsList))
	for _, path := range pathsList {
		record, ok := recordsByPath[path]
		if !ok {
			continue
		}
		record.TypeName = strings.TrimSpace(typeRows[path].TypeName)
		if record.TypeName == "" {
			record.TypeName = stringValue(record.Frontmatter[ontology.TypeFieldName()])
		}
		record.Ref.TypeName = record.TypeName
		record.HasIssues = s.issueByPath[path] || ontology.AssessmentHasIssues(assessments[path])
		out = append(out, record)
	}
	if err := s.hydrateSectionSummaries(ctx, out); err != nil {
		return nil, err
	}
	return out, nil
}
