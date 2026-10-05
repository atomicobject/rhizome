package noderead

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/ontology/pushdown"
)

const overlayTypeInstancesPlanLimit = 10000

// TypeInstances lists note-root and embedded instances for one ontology type.
//
// Catalog rows are preferred for embedded nodes; projection fallback is kept
// request-scoped so broad type views do not repeatedly parse the same notes.
//
// When req.Predicates/Sort/Offset are supplied for concrete types, this routes
// through the indexed plan path so filters and sort apply before the limit cap.
// Interface predicates/sort are collapsed into a single multi-type indexed
// query only when every implementor supports the requested fields; otherwise
// callers get an explicit unsupported error for sort because NodeListItem does
// not carry enough field-value data to apply a correct global sort afterward.
// Plan requests are not cached because the result depends on the predicate set.
func (s *Scope) TypeInstances(ctx context.Context, req TypeInstancesRequest) (ontology.TypeListResult, error) {
	if s == nil || s.service == nil || s.service.Store == nil {
		return ontology.TypeListResult{}, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := s.ensureNoteState(ctx); err != nil {
		return ontology.TypeListResult{}, err
	}
	typeName := strings.TrimSpace(req.TypeName)
	useOverlay := s.hasOverlay()
	usePlan := len(req.Predicates) > 0 || len(req.Sort) > 0 || req.Offset > 0
	cacheKey := fmt.Sprintf("%s:%d", typeName, req.Limit)
	if !usePlan && !useOverlay {
		if result, ok := s.typeInstancesByID[cacheKey]; ok {
			return cloneTypeListResult(result), nil
		}
	}
	switch typeName {
	case "":
		return ontology.TypeListResult{}, nil
	case ontology.TypeScopeAll:
		result := s.listAll()
		if useOverlay {
			items, err := s.overlayListItemsLocked(ctx, result.Items, true)
			if err != nil {
				return ontology.TypeListResult{}, err
			}
			result.Items = items
			result.Count = len(items)
			return result, nil
		}
		if !usePlan {
			s.typeInstancesByID[cacheKey] = cloneTypeListResult(result)
		}
		return result, nil
	case ontology.TypeScopeIssues:
		result := s.listIssues()
		if useOverlay {
			// Issue state comes from committed assessments, so staged notes
			// update or drop out here but new staged notes do not appear.
			items, err := s.overlayListItemsLocked(ctx, result.Items, false)
			if err != nil {
				return ontology.TypeListResult{}, err
			}
			result.Items = items
			result.Count = len(items)
			return result, nil
		}
		if !usePlan {
			s.typeInstancesByID[cacheKey] = cloneTypeListResult(result)
		}
		return result, nil
	}
	if ontology.OntologyTypeVisibility(typeName) == ontology.TypeVisibilityInternal {
		return ontology.TypeListResult{}, nil
	}
	doc, err := ontology.NewService(s.service.VaultDef, s.service.NoteReader, nil, s.service.Schema).TypeDoc(typeName)
	if err != nil {
		return ontology.TypeListResult{}, err
	}
	if doc == nil || s.service.Schema == nil {
		return ontology.TypeListResult{}, nil
	}
	if s.service.Schema.Interfaces[typeName] != nil {
		baseReq := req
		if useOverlay && !usePlan {
			baseReq.Limit = 0
		}
		items, err := s.interfaceItems(ctx, typeName, baseReq)
		if err != nil {
			return ontology.TypeListResult{}, err
		}
		if useOverlay {
			items, err = s.mergeOverlayTypeItemsLocked(ctx, typeName, items, req)
			if err != nil {
				return ontology.TypeListResult{}, err
			}
		}
		result := ontology.TypeListResult{TypeDoc: doc, Count: len(items), IssueCount: s.issueCountForItems(items), Items: items}
		if !usePlan && !useOverlay {
			s.typeInstancesByID[cacheKey] = cloneTypeListResult(result)
		}
		return result, nil
	}
	noteType := s.service.Schema.Types[typeName]
	if noteType == nil {
		return ontology.TypeListResult{}, nil
	}
	if usePlan {
		planReq := req
		if useOverlay {
			planReq.Limit = overlayTypeInstancesPlanLimit
			planReq.Offset = 0
		}
		items, ok, err := s.planTypeItems(ctx, typeName, noteType, planReq)
		if err != nil {
			return ontology.TypeListResult{}, err
		}
		if ok {
			if useOverlay {
				items, err = s.mergeOverlayTypeItemsLocked(ctx, typeName, items, req)
				if err != nil {
					return ontology.TypeListResult{}, err
				}
			}
			return ontology.TypeListResult{TypeDoc: doc, Count: len(items), IssueCount: s.issueCountForItems(items), Items: items}, nil
		}
	}
	limit := req.Limit
	if useOverlay && !usePlan {
		limit = 0
	}
	items, err := s.typeItems(ctx, typeName, noteType, limit)
	if err != nil {
		return ontology.TypeListResult{}, err
	}
	if useOverlay {
		items, err = s.mergeOverlayTypeItemsLocked(ctx, typeName, items, req)
		if err != nil {
			return ontology.TypeListResult{}, err
		}
	}
	result := ontology.TypeListResult{TypeDoc: doc, Count: len(items), IssueCount: s.issueCountForItems(items), Items: items}
	if !usePlan && !useOverlay {
		s.typeInstancesByID[cacheKey] = cloneTypeListResult(result)
	}
	return result, nil
}

// planTypeItems pushes filter/sort/page through the indexed plan when the
// catalog has rows for the type. Returns (nil, false, nil) when the store is
// not catalog-backed; callers fall back to the non-plan path.
func (s *Scope) planTypeItems(ctx context.Context, typeName string, noteType *ontology.NoteType, req TypeInstancesRequest) ([]ontology.NodeListItem, bool, error) {
	catalogStore, ok := s.service.Store.(CatalogStore)
	if !ok {
		return nil, false, nil
	}
	plan := codeanchor.OntologyNodeQueryPlan{
		TypeNames:  []string{typeName},
		Predicates: append([]codeanchor.OntologyFieldPredicate(nil), req.Predicates...),
		Sort:       append([]codeanchor.OntologyFieldSort(nil), req.Sort...),
		Limit:      req.Limit,
		Offset:     req.Offset,
	}
	rows, err := catalogStore.OntologyNodesByTypePlan(ctx, plan)
	if err != nil {
		return nil, true, err
	}
	items := make([]ontology.NodeListItem, 0, len(rows))
	for _, row := range rows {
		switch row.NodeKind {
		case string(ontology.NodeKindEmbedded):
			ref := nodeRefFromCatalogRow(row)
			items = append(items, ontology.NodeListItem{
				Ref:          ref,
				Title:        firstNonEmptyString(row.DisplayLabel, row.Title, row.SourceLocator, row.NotePath),
				ResolvedType: row.TypeName,
				NotePath:     row.NotePath,
				UpdatedAt:    row.UpdatedAt,
				HasIssues:    s.issueByPath[row.NotePath],
			})
		default:
			items = append(items, s.noteRootItem(row.NotePath, row.TypeName))
		}
	}
	if noteType != nil && noteType.Role == ontology.TypeRoleEmbeddedNode {
		s.diagnostics.CatalogHits += len(items)
	}
	return items, true, nil
}

func (s *Scope) typeItems(ctx context.Context, typeName string, noteType *ontology.NoteType, limit int) ([]ontology.NodeListItem, error) {
	if noteType != nil && noteType.Role == ontology.TypeRoleEmbeddedNode {
		if items, ok, err := s.catalogTypeItems(ctx, typeName, limit); err != nil {
			return items, err
		} else if ok {
			if len(items) > 0 || noteType.SourceShape == "" {
				return items, nil
			}
		}
		if err := s.ensureEmbeddedIndex(ctx); err != nil {
			return nil, err
		}
		return append([]ontology.NodeListItem(nil), s.embeddedByType[typeName]...), nil
	}
	pathsList, err := s.service.Store.OntologyPathsByType(ctx, typeName, limit)
	if err != nil {
		return nil, err
	}
	items := make([]ontology.NodeListItem, 0, len(pathsList))
	for _, path := range pathsList {
		items = append(items, s.noteRootItem(path, typeName))
	}
	sortNodeListItems(items)
	return items, nil
}

func (s *Scope) mergeOverlayTypeItemsLocked(ctx context.Context, typeName string, committed []ontology.NodeListItem, req TypeInstancesRequest) ([]ontology.NodeListItem, error) {
	overlayItems, err := s.overlayTypeItemsLocked(ctx, typeName, req)
	if err != nil {
		return nil, err
	}
	if len(overlayItems) == 0 && !s.hasOverlay() {
		return committed, nil
	}
	touched := s.overlayTouchedPathSetLocked(ctx)
	byKey := map[string]ontology.NodeListItem{}
	for _, item := range committed {
		if touched[item.NotePath] {
			continue
		}
		byKey[nodeListItemKey(item)] = item
	}
	for _, item := range overlayItems {
		byKey[nodeListItemKey(item)] = item
	}
	out := make([]ontology.NodeListItem, 0, len(byKey))
	for _, item := range byKey {
		out = append(out, item)
	}
	if len(req.Sort) > 0 {
		if err := s.sortOverlayMergedItemsLocked(ctx, out, req.Sort); err != nil {
			return nil, err
		}
	} else {
		sortNodeListItems(out)
	}
	if req.Offset > 0 {
		if req.Offset >= len(out) {
			return nil, nil
		}
		out = out[req.Offset:]
	}
	if req.Limit > 0 && len(out) > req.Limit {
		out = out[:req.Limit]
	}
	return out, nil
}

func (s *Scope) overlayTypeItemsLocked(ctx context.Context, typeName string, req TypeInstancesRequest) ([]ontology.NodeListItem, error) {
	if !s.hasOverlay() || s.service.Schema == nil {
		return nil, nil
	}
	acceptedTypes := s.overlayAcceptedTypes(typeName)
	if len(acceptedTypes) == 0 {
		return nil, nil
	}
	index, err := s.overlayIndexLocked(ctx)
	if err != nil {
		return nil, err
	}
	if index != nil {
		return s.overlayTypeItemsFromIndexLocked(index, typeName, acceptedTypes, req), nil
	}
	var out []ontology.NodeListItem
	for path := range s.overlayTouchedPathSetLocked(ctx) {
		snapshot, err := s.snapshotForPathLocked(ctx, path)
		if err != nil {
			return nil, err
		}
		if snapshot == nil {
			continue
		}
		root, err := ontology.ProjectNodeFromSnapshot(snapshot, s.service.Schema, ontology.NodeRef{NotePath: path, Kind: ontology.NodeKindNote})
		if err != nil || root == nil {
			continue
		}
		if fallback, ok := ontology.AsFallbackNoteProjection(root); ok {
			root = fallback
		}
		items, err := s.overlayProjectionTypeItemsLocked(ctx, root, acceptedTypes, req)
		if err != nil {
			return nil, err
		}
		out = append(out, items...)
		globalRefs, err := ontology.GlobalSourceNodeRefsFromSnapshot(snapshot, s.service.Schema)
		if err == nil {
			for _, ref := range globalRefs {
				projection, err := ontology.ProjectBoundNodeFromSnapshot(snapshot, s.service.Schema, ref)
				if err != nil || projection == nil {
					continue
				}
				items, err := s.overlayProjectionTypeItemsLocked(ctx, projection, acceptedTypes, req)
				if err != nil {
					return nil, err
				}
				out = append(out, items...)
			}
		}
	}
	return out, nil
}

func (s *Scope) overlayTypeItemsFromIndexLocked(index *ReadOverlayIndex, typeName string, acceptedTypes map[string]bool, req TypeInstancesRequest) []ontology.NodeListItem {
	if index == nil {
		return nil
	}
	raw := index.ItemsByType[typeName]
	out := make([]ontology.NodeListItem, 0, len(raw))
	for _, item := range raw {
		if !acceptedTypes[item.ResolvedType] && !acceptedTypes[typeName] {
			continue
		}
		record, ok := index.RecordsByRef[nodeListItemKey(item)]
		if !ok {
			continue
		}
		record = s.enrichOverlayRecordLocked(record)
		if s.recordMatchesPredicates(record, req.Predicates) {
			item.Title = record.Title
			item.UpdatedAt = record.UpdatedAt
			item.HasIssues = record.HasIssues
			out = append(out, item)
		}
	}
	return out
}

func (s *Scope) overlayProjectionTypeItemsLocked(ctx context.Context, projection *ontology.NodeProjection, acceptedTypes map[string]bool, req TypeInstancesRequest) ([]ontology.NodeListItem, error) {
	if projection == nil {
		return nil, nil
	}
	var out []ontology.NodeListItem
	if acceptedTypes[projection.ResolvedType] {
		record := s.nodeRecordFromProjectionLocked(ctx, projection)
		if s.recordMatchesPredicates(record, req.Predicates) {
			out = append(out, ontology.NodeListItem{
				Ref:          record.Ref,
				Title:        record.Title,
				ResolvedType: record.TypeName,
				NotePath:     record.Ref.NotePath,
				UpdatedAt:    record.UpdatedAt,
				HasIssues:    record.HasIssues,
			})
		}
	}
	names := make([]string, 0, len(projection.Fields))
	for name := range projection.Fields {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		for _, ref := range projection.Fields[name].SectionNodes {
			child, err := ontology.ProjectBoundNodeFromSnapshot(projection.Snapshot, s.service.Schema, ref)
			if err != nil || child == nil {
				continue
			}
			items, err := s.overlayProjectionTypeItemsLocked(ctx, child, acceptedTypes, req)
			if err != nil {
				return nil, err
			}
			out = append(out, items...)
		}
	}
	return out, nil
}

func (s *Scope) overlayAcceptedTypes(typeName string) map[string]bool {
	out := map[string]bool{}
	if s.service.Schema.Types[typeName] != nil {
		out[typeName] = true
		return out
	}
	if s.service.Schema.Interfaces[typeName] != nil {
		for _, implementor := range s.interfaceImplementors(typeName) {
			out[implementor] = true
		}
	}
	return out
}

func (s *Scope) overlayTouchedPathSetLocked(ctx context.Context) map[string]bool {
	out := map[string]bool{}
	if !s.hasOverlay() {
		return out
	}
	if index, err := s.overlayIndexLocked(ctx); err == nil && index != nil {
		for path := range index.TouchedPaths {
			out[path] = true
		}
		return out
	}
	overlay := s.activeOverlay()
	for path := range overlay.Notes {
		if strings.TrimSpace(path) != "" {
			out[strings.TrimSpace(path)] = true
		}
	}
	for path := range overlay.UpdatedContentByPath {
		if strings.TrimSpace(path) != "" {
			out[strings.TrimSpace(path)] = true
		}
	}
	for _, path := range overlay.TouchedPaths {
		if strings.TrimSpace(path) != "" {
			out[strings.TrimSpace(path)] = true
		}
	}
	return out
}

func nodeListItemKey(item ontology.NodeListItem) string {
	if key := nodeRefIdentityKey(item.Ref); key != "" {
		return key
	}
	return item.NotePath
}

func (s *Scope) recordMatchesPredicates(record NodeRecord, predicates []codeanchor.OntologyFieldPredicate) bool {
	for _, predicate := range predicates {
		op := strings.ToLower(strings.TrimSpace(string(predicate.Op)))
		if op == "" {
			op = "eq"
		}
		field := s.recordSchemaField(record, predicate.FieldName)
		if field != nil && field.Kind == ontology.FieldKindLink {
			values := s.FieldValues(record, predicate.FieldName)
			switch op {
			case "exists":
				if len(values) == 0 {
					return false
				}
			case "in", "eq":
				if len(predicate.Values) > 0 && !anyStringIntersects(values, predicateTextValues(predicate)) {
					return false
				}
			default:
				return false
			}
			continue
		}
		rows := s.scalarFieldRows(record, predicate.FieldName, field)
		switch op {
		case "exists":
			exists := false
			for _, row := range rows {
				if row.ValueNorm != "" {
					exists = true
					break
				}
			}
			if !exists {
				return false
			}
		case "eq", "in", "gt", "gte", "lt", "lte":
			if len(predicate.Values) == 0 {
				continue
			}
			operands := predicate.Values
			if op != "in" {
				operands = operands[:1]
			}
			matches := false
			for _, operand := range operands {
				kind := pushdown.PredicateValueKind(operand)
				right := pushdown.ScalarFromRow(operand, kind)
				for _, row := range rows {
					if pushdown.ScalarFromRow(row, kind).Matches(right, op) {
						matches = true
						break
					}
				}
				if matches {
					break
				}
			}
			if !matches {
				return false
			}
		default:
			return false
		}
	}
	return true
}

// overlayListItemsLocked applies staged note records to a note-level pseudo
// listing: touched notes take their staged title and type, deleted notes drop
// out, and, when includeNew is set, notes created in the session appear.
func (s *Scope) overlayListItemsLocked(ctx context.Context, committed []ontology.NodeListItem, includeNew bool) ([]ontology.NodeListItem, error) {
	touched := s.overlayTouchedPathSetLocked(ctx)
	if len(touched) == 0 {
		return committed, nil
	}
	paths := make([]string, 0, len(touched))
	for path := range touched {
		paths = append(paths, path)
	}
	records, err := s.overlayNoteRecordsLocked(ctx, paths)
	if err != nil {
		return nil, err
	}
	staged := make(map[string]NodeRecord, len(records))
	for _, record := range records {
		staged[record.Ref.NotePath] = record
	}
	out := make([]ontology.NodeListItem, 0, len(committed)+len(staged))
	for _, item := range committed {
		if !touched[item.NotePath] {
			out = append(out, item)
			continue
		}
		record, ok := staged[item.NotePath]
		if !ok {
			// A touched note without a staged record is deleted in the session.
			continue
		}
		delete(staged, item.NotePath)
		out = append(out, allItemFromRecord(record, item))
	}
	if includeNew {
		for _, record := range staged {
			out = append(out, allItemFromRecord(record, ontology.NodeListItem{}))
		}
	}
	sortNodeListItems(out)
	return out, nil
}

func allItemFromRecord(record NodeRecord, committed ontology.NodeListItem) ontology.NodeListItem {
	resolved := record.TypeName
	if ontology.OntologyTypeVisibility(resolved) == ontology.TypeVisibilityInternal {
		resolved = ""
	}
	return ontology.NodeListItem{
		Ref:          ontology.NodeRef{NotePath: record.Ref.NotePath, Kind: ontology.NodeKindNote},
		Title:        firstNonEmptyString(record.Title, committed.Title, record.Ref.NotePath),
		ResolvedType: resolved,
		NotePath:     record.Ref.NotePath,
		UpdatedAt:    firstNonZero(record.UpdatedAt, committed.UpdatedAt),
		HasIssues:    record.HasIssues || committed.HasIssues,
	}
}

func firstNonZero(values ...int64) int64 {
	for _, value := range values {
		if value != 0 {
			return value
		}
	}
	return 0
}

func (s *Scope) sortOverlayMergedItemsLocked(ctx context.Context, items []ontology.NodeListItem, sorts []codeanchor.OntologyFieldSort) error {
	if len(items) == 0 || len(sorts) == 0 {
		return nil
	}
	refs := make([]ontology.NodeRef, 0, len(items))
	for _, item := range items {
		refs = append(refs, item.Ref)
	}
	records, err := s.hydrateLocked(ctx, refs, HydrateOptions{Profile: HydrateSummary})
	if err != nil {
		return err
	}
	byKey := map[string]NodeRecord{}
	for _, record := range records {
		byKey[nodeRefIdentityKey(record.Ref)] = record
	}
	type sortEntry struct {
		item   ontology.NodeListItem
		values []pushdown.Scalar
		stable pushdown.StableKey
	}
	entries := make([]sortEntry, 0, len(items))
	for _, item := range items {
		record := byKey[nodeRefIdentityKey(item.Ref)]
		values := make([]pushdown.Scalar, len(sorts))
		for i, spec := range sorts {
			if pushdown.IsBuiltinFieldKey(spec.FieldName) {
				values[i] = pushdown.ScalarFromRow(codeanchor.IntelOntologyNodeFieldValue{ValueNorm: record.Ref.NotePath}, "string")
				continue
			}
			field := s.recordSchemaField(record, spec.FieldName)
			if rows := record.FieldValues[strings.ToLower(strings.TrimSpace(spec.FieldName))]; len(rows) > 0 {
				values[i] = pushdown.AggregateSortRows(rows, spec.ValueKind, spec.Desc)
			} else {
				values[i] = pushdown.AggregateSortValues(field, s.scalarFieldValues(record, spec.FieldName, field), spec.ValueKind, spec.Desc)
			}
		}
		ref := item.Ref
		ref.TypeName = firstNonEmptyString(ref.TypeName, item.ResolvedType)
		stable := pushdown.StableKey{NotePath: ref.NotePath, StartByte: record.catalogStartByte, NodeID: record.catalogNodeID}
		if stable.NodeID == "" {
			stable = pushdown.StableKey{NotePath: ref.NotePath, StartByte: ref.StartByte, NodeID: ontology.OntologyNodeID(ref)}
		}
		entries = append(entries, sortEntry{item, values, stable})
	}
	sort.SliceStable(entries, func(i, j int) bool {
		for k, spec := range sorts {
			if order := entries[i].values[k].CompareSort(entries[j].values[k], spec.Desc, spec.NullsLast); order != 0 {
				return order < 0
			}
		}
		return entries[i].stable.Compare(entries[j].stable) < 0
	})
	for i := range entries {
		items[i] = entries[i].item
	}
	return nil
}

func (s *Scope) recordSchemaField(record NodeRecord, name string) *ontology.Field {
	if s.service.Schema == nil {
		return nil
	}
	_, field, _ := (pushdown.SchemaResolver{NoteType: s.service.Schema.Types[record.TypeName]}).Resolve(name)
	return field
}

func (s *Scope) scalarFieldRows(record NodeRecord, name string, field *ontology.Field) []codeanchor.IntelOntologyNodeFieldValue {
	if pushdown.IsBuiltinFieldKey(name) {
		return []codeanchor.IntelOntologyNodeFieldValue{{ValueText: record.Ref.NotePath, ValueNorm: record.Ref.NotePath}}
	}
	if rows := record.FieldValues[strings.ToLower(strings.TrimSpace(name))]; len(rows) > 0 {
		return rows
	}
	values := s.scalarFieldValues(record, name, field)
	rows := make([]codeanchor.IntelOntologyNodeFieldValue, len(values))
	for i, value := range values {
		rows[i] = pushdown.StoredFieldValue(field, value)
	}
	return rows
}

func (s *Scope) scalarFieldValues(record NodeRecord, name string, field *ontology.Field) []string {
	var values []string
	if field != nil {
		if binding, ok := record.projectedFields[field.Name]; ok {
			values = pushdown.PresentSortValues(field, binding.values, binding.present)
		} else if extracted, present, applicable := s.schemaFieldValues(record, field); applicable {
			values = pushdown.PresentSortValues(field, extracted, present)
		} else {
			values = s.FieldValues(record, name)
		}
	} else {
		values = s.FieldValues(record, name)
	}
	return values
}

// FieldValues returns a record's values for a schema field: indexed or
// overlay-projected values first, otherwise the field's declared source read
// the way the indexer reads it. Callers outside the merged sort use it to show
// the same value the sort and index see.
func (s *Scope) FieldValues(record NodeRecord, field string) []string {
	field = strings.TrimSpace(field)
	if field == "" || len(record.FieldValues[field]) == 0 {
		// Committed note records carry raw metadata rather than indexed field
		// values. Read schema fields the way the indexer does, so a renamed
		// property key or an inline mention in prose cannot reorder the merge.
		if values, _, ok := s.schemaFieldValues(record, s.recordSchemaField(record, field)); ok {
			return values
		}
		return recordFieldValues(record, field)
	}
	rows := record.FieldValues[field]
	values := make([]string, 0, len(rows))
	for _, row := range rows {
		values = append(values, fieldValueRowComparableText(row))
	}
	return values
}

// schemaFieldValues reads a note-level schema field from its declared source
// using its authored property names, matching ontology.extractFieldValues. It
// reports values, authored presence, and whether the rule applies. Embedded
// nodes author fields inside the body; some types use other sources.
func (s *Scope) schemaFieldValues(record NodeRecord, field *ontology.Field) ([]string, bool, bool) {
	if s.service.Schema == nil || (record.Ref.Kind != "" && record.Ref.Kind != ontology.NodeKindNote) {
		return nil, false, false
	}
	if field == nil {
		return nil, false, false
	}
	switch field.SourceKind {
	case ontology.FieldSourceInline:
		for _, name := range ontology.FieldSourceNames(field) {
			if values, present := caseInsensitiveValue(record.InlineProps, name); present {
				return append([]string(nil), values...), true, true
			}
		}
		return nil, false, true
	case ontology.FieldSourceFrontmatter, "":
		for _, name := range ontology.FieldSourceNames(field) {
			if value, present := caseInsensitiveValue(record.Frontmatter, name); present {
				return anyFieldValues(value), true, true
			}
		}
		return nil, false, true
	default:
		return nil, false, false
	}
}

func caseInsensitiveValue[V any](values map[string]V, key string) (V, bool) {
	if value, ok := values[key]; ok {
		return value, true
	}
	for current, value := range values {
		if strings.EqualFold(strings.TrimSpace(current), strings.TrimSpace(key)) {
			return value, true
		}
	}
	var zero V
	return zero, false
}

func recordFieldValues(record NodeRecord, field string) []string {
	field = strings.TrimSpace(field)
	switch field {
	case "", "path", "notePath":
		return []string{record.Ref.NotePath}
	case "title":
		return []string{record.Title}
	case "resolvedType", "type":
		return []string{record.TypeName}
	}
	if rows := record.FieldValues[field]; len(rows) > 0 {
		out := make([]string, 0, len(rows))
		for _, row := range rows {
			out = append(out, firstNonEmptyString(row.ValueText, row.TargetSourceLocator, row.TargetNotePath, row.TargetRefJSON))
		}
		return out
	}
	if values := record.InlineProps[field]; len(values) > 0 {
		return append([]string(nil), values...)
	}
	if strings.HasPrefix(field, "frontmatter.") {
		return anyFieldValues(record.Frontmatter[strings.TrimPrefix(field, "frontmatter.")])
	}
	if strings.HasPrefix(field, "inline.") {
		return append([]string(nil), record.InlineProps[strings.TrimPrefix(field, "inline.")]...)
	}
	if values := anyFieldValues(record.Frontmatter[field]); len(values) > 0 {
		return values
	}
	return nil
}

func anyFieldValues(value any) []string {
	switch typed := value.(type) {
	case nil:
		return nil
	case string:
		return []string{typed}
	case bool:
		if typed {
			return []string{"true"}
		}
		return []string{"false"}
	case []string:
		return append([]string(nil), typed...)
	case []any:
		out := make([]string, 0, len(typed))
		for _, item := range typed {
			out = append(out, anyFieldValues(item)...)
		}
		return out
	default:
		return []string{fmt.Sprint(typed)}
	}
}

func predicateTextValues(predicate codeanchor.OntologyFieldPredicate) []string {
	out := make([]string, 0, len(predicate.Values))
	for _, value := range predicate.Values {
		out = append(out, fieldValueRowComparableText(value))
	}
	return out
}

func fieldValueRowComparableText(value codeanchor.IntelOntologyNodeFieldValue) string {
	if value.ValueBool != nil {
		return strconv.FormatBool(*value.ValueBool)
	}
	if value.ValueInt != nil {
		return strconv.FormatInt(*value.ValueInt, 10)
	}
	if value.ValueReal != nil {
		return strconv.FormatFloat(*value.ValueReal, 'g', -1, 64)
	}
	if value.ValueDate != nil {
		return *value.ValueDate
	}
	if value.ValueDateTime != nil {
		return *value.ValueDateTime
	}
	return firstNonEmptyString(value.ValueText, value.TargetSourceLocator, value.TargetNotePath, value.TargetRefJSON)
}

func anyStringIntersects(actual []string, want []string) bool {
	if len(want) > 4 {
		set := make(map[string]struct{}, len(want))
		for _, value := range want {
			set[strings.ToLower(strings.TrimSpace(value))] = struct{}{}
		}
		for _, value := range actual {
			if _, ok := set[strings.ToLower(strings.TrimSpace(value))]; ok {
				return true
			}
		}
		return false
	}
	for _, left := range actual {
		for _, right := range want {
			if strings.EqualFold(strings.TrimSpace(left), strings.TrimSpace(right)) {
				return true
			}
		}
	}
	return false
}

func (s *Scope) catalogTypeItems(ctx context.Context, typeName string, limit int) ([]ontology.NodeListItem, bool, error) {
	catalogStore, ok := s.service.Store.(CatalogStore)
	if !ok {
		return nil, false, nil
	}
	rows, err := catalogStore.OntologyNodesByType(ctx, typeName)
	if err != nil {
		return nil, true, err
	}
	if len(rows) == 0 {
		return nil, true, nil
	}
	items := make([]ontology.NodeListItem, 0, len(rows))
	for _, row := range rows {
		if row.NodeKind != string(ontology.NodeKindEmbedded) {
			continue
		}
		ref := nodeRefFromCatalogRow(row)
		items = append(items, ontology.NodeListItem{
			Ref:          ref,
			Title:        firstNonEmptyString(row.DisplayLabel, row.Title, row.SourceLocator, row.NotePath),
			ResolvedType: row.TypeName,
			NotePath:     row.NotePath,
			UpdatedAt:    row.UpdatedAt,
			HasIssues:    s.issueByPath[row.NotePath],
		})
	}
	sortNodeListItems(items)
	if limit > 0 && len(items) > limit {
		items = items[:limit]
	}
	s.diagnostics.CatalogHits += len(items)
	return items, true, nil
}

func (s *Scope) interfaceItems(ctx context.Context, typeName string, req TypeInstancesRequest) ([]ontology.NodeListItem, error) {
	implementors := s.interfaceImplementors(typeName)
	if len(implementors) == 0 {
		return nil, nil
	}
	// Collapsed multi-type plan: when every implementor has the predicates'
	// fields with compatible capability, a single n.type_name IN (...) query
	// yields a globally-paged result. Falls back to the per-implementor union
	// when any implementor lacks a predicate field.
	if len(req.Sort) > 0 {
		if !s.allImplementorsSupportSorts(implementors, req.Sort) {
			return nil, fmt.Errorf("interface type %s does not support sorted TypeInstances requests for non-common or non-indexed sort fields", typeName)
		}
		if req.Limit <= 0 {
			return nil, fmt.Errorf("interface type %s sorted TypeInstances requests require a positive limit", typeName)
		}
		if len(req.Predicates) > 0 && !s.allImplementorsSupportPredicates(implementors, req.Predicates) {
			return nil, fmt.Errorf("interface type %s does not support indexed TypeInstances predicates for all implementors", typeName)
		}
		return s.interfaceItemsCollapsed(ctx, implementors, req)
	}
	if len(req.Predicates) > 0 && s.allImplementorsSupportPredicates(implementors, req.Predicates) {
		return s.interfaceItemsCollapsed(ctx, implementors, req)
	}
	return s.interfaceItemsPerImplementor(ctx, implementors, req)
}

// interfaceImplementors returns implementor types in deterministic order.
func (s *Scope) interfaceImplementors(typeName string) []string {
	implementors := make([]string, 0)
	for name, noteType := range s.service.Schema.Types {
		if noteType == nil {
			continue
		}
		for _, iface := range noteType.Implements {
			if iface == typeName {
				implementors = append(implementors, name)
				break
			}
		}
	}
	sort.Strings(implementors)
	return implementors
}

// allImplementorsSupportPredicates reports whether every implementor type has
// the predicate fields with capability-compatible operators. This is the gate
// for a single multi-type plan.
func (s *Scope) allImplementorsSupportPredicates(typeNames []string, predicates []codeanchor.OntologyFieldPredicate) bool {
	if len(predicates) == 0 || s.service.Schema == nil {
		return false
	}
	for _, predicate := range predicates {
		fieldName := strings.TrimSpace(predicate.FieldName)
		if fieldName == "" {
			continue
		}
		// Builtin path/notePath predicates always work across implementors.
		if fieldName == "notePath" || fieldName == "path" {
			continue
		}
		op := strings.ToLower(strings.TrimSpace(string(predicate.Op)))
		if op == "" {
			op = "eq"
		}
		for _, typeName := range typeNames {
			noteType := s.service.Schema.Types[typeName]
			if noteType == nil {
				return false
			}
			field := noteType.ByName[fieldName]
			if field == nil {
				return false
			}
			capability, ok := ontology.IndexedFieldCapabilityForField(s.service.Schema, field)
			if !ok {
				return false
			}
			supported := false
			for _, allowed := range capability.FilterOps {
				if strings.EqualFold(allowed, op) {
					supported = true
					break
				}
			}
			if !supported {
				return false
			}
		}
	}
	return true
}

// allImplementorsSupportSorts reports whether every implementor can satisfy
// the requested sort keys through the indexed catalog with compatible value
// kinds. That proof is required before using a collapsed interface plan,
// because per-implementor merging cannot preserve field-order semantics.
func (s *Scope) allImplementorsSupportSorts(typeNames []string, sorts []codeanchor.OntologyFieldSort) bool {
	if len(sorts) == 0 || s.service.Schema == nil {
		return false
	}
	for _, sortSpec := range sorts {
		fieldName := strings.TrimSpace(sortSpec.FieldName)
		if fieldName == "" {
			continue
		}
		if strings.EqualFold(fieldName, "notePath") || strings.EqualFold(fieldName, "path") {
			continue
		}
		expectedKind := strings.ToLower(strings.TrimSpace(sortSpec.ValueKind))
		for _, typeName := range typeNames {
			noteType := s.service.Schema.Types[typeName]
			if noteType == nil {
				return false
			}
			field := noteType.ByName[fieldName]
			if field == nil {
				return false
			}
			capability, ok := ontology.IndexedFieldCapabilityForField(s.service.Schema, field)
			if !ok || !capability.Sortable {
				return false
			}
			if expectedKind != "" && expectedKind != "builtin" && !strings.EqualFold(expectedKind, capability.ValueKind) {
				return false
			}
		}
	}
	return true
}

func (s *Scope) interfaceItemsCollapsed(ctx context.Context, implementors []string, req TypeInstancesRequest) ([]ontology.NodeListItem, error) {
	catalogStore, ok := s.service.Store.(CatalogStore)
	if !ok {
		return s.interfaceItemsPerImplementor(ctx, implementors, req)
	}
	limit := req.Limit
	offset := req.Offset
	if limit <= 0 {
		limit = 0 // Plan requires positive limit; fall back below.
	}
	if limit == 0 {
		// No limit means we cannot use the SQL plan (it requires positive Limit);
		// fall back to per-implementor.
		return s.interfaceItemsPerImplementor(ctx, implementors, req)
	}
	plan := codeanchor.OntologyNodeQueryPlan{
		TypeNames:  append([]string(nil), implementors...),
		Predicates: append([]codeanchor.OntologyFieldPredicate(nil), req.Predicates...),
		Sort:       append([]codeanchor.OntologyFieldSort(nil), req.Sort...),
		Limit:      limit,
		Offset:     offset,
	}
	rows, err := catalogStore.OntologyNodesByTypePlan(ctx, plan)
	if err != nil {
		return nil, err
	}
	out := make([]ontology.NodeListItem, 0, len(rows))
	for _, row := range rows {
		switch row.NodeKind {
		case string(ontology.NodeKindEmbedded):
			ref := nodeRefFromCatalogRow(row)
			out = append(out, ontology.NodeListItem{
				Ref:          ref,
				Title:        firstNonEmptyString(row.DisplayLabel, row.Title, row.SourceLocator, row.NotePath),
				ResolvedType: row.TypeName,
				NotePath:     row.NotePath,
				UpdatedAt:    row.UpdatedAt,
				HasIssues:    s.issueByPath[row.NotePath],
			})
		default:
			out = append(out, s.noteRootItem(row.NotePath, row.TypeName))
		}
	}
	s.diagnostics.CatalogHits += len(out)
	return out, nil
}

func (s *Scope) interfaceItemsPerImplementor(ctx context.Context, implementors []string, req TypeInstancesRequest) ([]ontology.NodeListItem, error) {
	out := make([]ontology.NodeListItem, 0)
	seen := map[string]struct{}{}
	for _, name := range implementors {
		noteType := s.service.Schema.Types[name]
		if noteType == nil {
			continue
		}
		var items []ontology.NodeListItem
		var err error
		if len(req.Predicates) > 0 || len(req.Sort) > 0 || req.Offset > 0 {
			var ok bool
			limit := req.Limit
			if limit > 0 && req.Offset > 0 {
				limit += req.Offset
			}
			items, ok, err = s.planTypeItems(ctx, name, noteType, TypeInstancesRequest{
				TypeName:   name,
				Limit:      limit,
				Predicates: req.Predicates,
				Sort:       req.Sort,
			})
			if err != nil {
				return nil, err
			}
			if !ok {
				items, err = s.typeItems(ctx, name, noteType, req.Limit)
			}
		} else {
			items, err = s.typeItems(ctx, name, noteType, req.Limit)
		}
		if err != nil {
			return nil, err
		}
		for _, item := range items {
			key := item.Ref.String()
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			out = append(out, item)
		}
	}
	sortNodeListItems(out)
	if req.Offset > 0 {
		if req.Offset >= len(out) {
			return nil, nil
		}
		out = out[req.Offset:]
	}
	if req.Limit > 0 && len(out) > req.Limit {
		out = out[:req.Limit]
	}
	return out, nil
}

func (s *Scope) listAll() ontology.TypeListResult {
	items := make([]ontology.NodeListItem, 0, len(s.noteRows))
	for _, row := range s.noteRows {
		resolved := ""
		if typeRow, ok := s.typeRowsByPath[row.Path]; ok {
			resolved = typeRow.TypeName
			if ontology.OntologyTypeVisibility(resolved) == ontology.TypeVisibilityInternal {
				resolved = ""
			}
		}
		items = append(items, ontology.NodeListItem{
			Ref:          ontology.NodeRef{NotePath: row.Path, Kind: ontology.NodeKindNote},
			Title:        firstNonEmptyString(row.Title, row.Path),
			ResolvedType: resolved,
			NotePath:     row.Path,
			UpdatedAt:    row.Mtime,
			HasIssues:    s.issueByPath[row.Path],
		})
	}
	sortNodeListItems(items)
	return ontology.TypeListResult{
		TypeDoc:    &ontology.TypeDoc{Name: ontology.TypeScopeAll, Label: "All notes", Description: "Every indexed note, regardless of resolved ontology type."},
		Count:      len(items),
		IssueCount: s.issueCountForItems(items),
		Items:      items,
	}
}

func (s *Scope) listIssues() ontology.TypeListResult {
	items := make([]ontology.NodeListItem, 0)
	for _, row := range s.noteRows {
		if !s.issueByPath[row.Path] {
			continue
		}
		resolved := ""
		if typeRow, ok := s.typeRowsByPath[row.Path]; ok {
			resolved = typeRow.TypeName
			if ontology.OntologyTypeVisibility(resolved) == ontology.TypeVisibilityInternal {
				resolved = ""
			}
		}
		items = append(items, ontology.NodeListItem{
			Ref:          ontology.NodeRef{NotePath: row.Path, Kind: ontology.NodeKindNote},
			Title:        firstNonEmptyString(row.Title, row.Path),
			ResolvedType: resolved,
			NotePath:     row.Path,
			UpdatedAt:    row.Mtime,
			HasIssues:    true,
		})
	}
	sortNodeListItems(items)
	return ontology.TypeListResult{
		TypeDoc:    &ontology.TypeDoc{Name: ontology.TypeScopeIssues, Label: "Notes with issues", Description: "Every note that has at least one validation issue."},
		Count:      len(items),
		IssueCount: s.issueCountForItems(items),
		Items:      items,
	}
}

func (s *Scope) noteRootItem(path string, resolvedType string) ontology.NodeListItem {
	row := s.noteRowByPath[path]
	return ontology.NodeListItem{
		Ref:          ontology.NodeRef{NotePath: path, Kind: ontology.NodeKindNote},
		Title:        firstNonEmptyString(row.Title, path),
		ResolvedType: resolvedType,
		NotePath:     path,
		UpdatedAt:    row.Mtime,
		HasIssues:    s.issueByPath[path],
	}
}

func (s *Scope) ensureEmbeddedIndex(ctx context.Context) error {
	if s.embeddedReady {
		return nil
	}
	for _, path := range s.allNotePaths {
		projection, err := s.projectionLocked(ctx, ontology.NodeRef{NotePath: path, Kind: ontology.NodeKindNote})
		if err != nil {
			return err
		}
		if err := s.collectProjectionItems(projection); err != nil {
			return err
		}
		globalRefs, err := ontology.GlobalSourceNodeRefsFromSnapshot(projection.Snapshot, s.service.Schema)
		if err != nil {
			return err
		}
		for _, ref := range globalRefs {
			globalProjection, err := ontology.ProjectBoundNodeFromSnapshot(projection.Snapshot, s.service.Schema, ref)
			if err != nil {
				return err
			}
			if err := s.collectProjectionItems(globalProjection); err != nil {
				return err
			}
		}
	}
	for typeName := range s.embeddedByType {
		sortNodeListItems(s.embeddedByType[typeName])
	}
	s.embeddedReady = true
	return nil
}

func (s *Scope) collectProjectionItems(projection *ontology.NodeProjection) error {
	if projection == nil {
		return nil
	}
	if projection.Ref.Kind == ontology.NodeKindEmbedded && projection.ResolvedType != "" {
		workspace := ontology.BuildNodeWorkspaceFromProjectionWithSchema(s.service.Schema, projection)
		title := projection.Ref.String()
		if workspace != nil && strings.TrimSpace(workspace.Node.Title) != "" {
			title = workspace.Node.Title
		} else if projection.Snapshot != nil {
			if span := projection.Snapshot.SourceSpansByID[projection.Ref.NodeID]; span != nil && strings.TrimSpace(span.Title) != "" {
				title = span.Title
			}
		}
		if s.embeddedSeen[projection.ResolvedType] == nil {
			s.embeddedSeen[projection.ResolvedType] = map[string]struct{}{}
		}
		key := projection.Ref.String()
		if _, ok := s.embeddedSeen[projection.ResolvedType][key]; ok {
			goto children
		}
		s.embeddedSeen[projection.ResolvedType][key] = struct{}{}
		s.embeddedByType[projection.ResolvedType] = append(s.embeddedByType[projection.ResolvedType], ontology.NodeListItem{
			Ref:          projection.Ref,
			Title:        title,
			ResolvedType: projection.ResolvedType,
			NotePath:     projection.Ref.NotePath,
			UpdatedAt:    s.noteRowByPath[projection.Ref.NotePath].Mtime,
			HasIssues:    s.issueByPath[projection.Ref.NotePath],
		})
	}
children:
	for _, field := range projection.Fields {
		for _, ref := range field.SectionNodes {
			child, err := ontology.ProjectBoundNodeFromSnapshot(projection.Snapshot, s.service.Schema, ref)
			if err != nil {
				return err
			}
			if err := s.collectProjectionItems(child); err != nil {
				return err
			}
		}
	}
	return nil
}

func (s *Scope) issueCountForItems(items []ontology.NodeListItem) int {
	seen := map[string]struct{}{}
	total := 0
	for _, item := range items {
		if _, ok := seen[item.NotePath]; ok {
			continue
		}
		seen[item.NotePath] = struct{}{}
		if s.issueByPath[item.NotePath] {
			total++
		}
	}
	return total
}

func sortNodeListItems(items []ontology.NodeListItem) {
	sort.SliceStable(items, func(i, j int) bool {
		if items[i].Title != items[j].Title {
			return items[i].Title < items[j].Title
		}
		return items[i].Ref.String() < items[j].Ref.String()
	})
}
