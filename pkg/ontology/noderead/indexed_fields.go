package noderead

import (
	"context"
	"encoding/json"
	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/ontology/readmodel"
	"sort"
	"strings"
)

func (s *Scope) catalogFieldValuesByNodeID(ctx context.Context, rows map[string]readmodel.NodeRow) (map[string][]readmodel.FieldValueRow, error) {
	if len(rows) == 0 {
		return nil, nil
	}
	catalogStore := s.service.Store
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
	byNodeID := make(map[string][]readmodel.FieldValueRow, len(nodeIDs))
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
func (s *Scope) IndexedFieldValues(ctx context.Context, projections []*ontology.NodeProjection) (map[string][]readmodel.FieldValueRow, error) {
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
func (s *Scope) IndexedRecordFieldValues(ctx context.Context, records []NodeRecord) (map[string][]readmodel.FieldValueRow, error) {
	if s == nil || s.service == nil {
		return nil, nil
	}
	catalogStore := s.service.Store
	keysByLocator := make(map[string][]string, len(records))
	locators := make([]string, 0, len(records))
	for _, record := range records {
		ref := NormalizeRef(record.Ref)
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
func IndexedLinkRow(rows []readmodel.FieldValueRow, field, value string) (readmodel.FieldValueRow, bool) {
	field, value = strings.TrimSpace(field), strings.TrimSpace(value)
	for _, row := range rows {
		if strings.EqualFold(strings.TrimSpace(row.FieldName), field) && strings.TrimSpace(row.ValueText) == value {
			return row, true
		}
	}
	return readmodel.FieldValueRow{}, false
}

// IndexedLinkTargetRef returns the target the indexer resolved for a link
// field row; false means the link was unresolved when indexed.
func IndexedLinkTargetRef(row readmodel.FieldValueRow) (ontology.NodeRef, bool) {
	if strings.TrimSpace(row.TargetRefJSON) == "" {
		return ontology.NodeRef{}, false
	}
	var ref ontology.NodeRef
	if err := json.Unmarshal([]byte(row.TargetRefJSON), &ref); err != nil || ref.IsZero() {
		return ontology.NodeRef{}, false
	}
	return ref, true
}

func (s *Scope) indexedFieldValuesByNodeID(ctx context.Context, keysByNodeID map[string][]string) (map[string][]readmodel.FieldValueRow, error) {
	catalogStore := s.service.Store
	if catalogStore == nil || len(keysByNodeID) == 0 {
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
	out := make(map[string][]readmodel.FieldValueRow, len(keysByNodeID))
	for _, row := range rows {
		for _, key := range keysByNodeID[row.NodeID] {
			out[key] = append(out[key], row)
		}
	}
	return out, nil
}

func recordWithIndexedFieldValues(record NodeRecord, rows []readmodel.FieldValueRow) NodeRecord {
	if len(rows) == 0 {
		if record.FieldValues == nil {
			record.FieldValues = map[string][]readmodel.FieldValueRow{}
		}
		if record.InlineProps == nil {
			record.InlineProps = map[string][]string{}
		}
		return record
	}
	if record.FieldValues == nil {
		record.FieldValues = map[string][]readmodel.FieldValueRow{}
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
