package query

import (
	"context"
	"encoding/json"
	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/ontology/readmodel"
	ontoschema "github.com/atomicobject/rhizome/pkg/ontology/schema"
	semdb "github.com/atomicobject/rhizome/pkg/store"
	"strings"
)

func (e *executor) sectionRecordsFromCatalogRows(ctx context.Context, rows []readmodel.NodeRow) ([]*sectionRecord, error) {
	nodeIDs := make([]string, 0, len(rows))
	notePaths := make([]string, 0, len(rows))
	for _, row := range rows {
		if strings.TrimSpace(row.NodeID) != "" {
			nodeIDs = append(nodeIDs, row.NodeID)
		}
		if strings.TrimSpace(row.NotePath) != "" {
			notePaths = append(notePaths, row.NotePath)
		}
	}
	fieldRows, err := e.deps.Store.OntologyNodeFieldValuesByNodeIDs(ctx, nodeIDs, nil)
	if err != nil {
		return nil, err
	}
	typeRows, err := e.loaders.ontologyTypesByPaths(ctx, normalizeStrings(notePaths))
	if err != nil {
		return nil, err
	}
	fieldsByNodeID := make(map[string]map[string][]readmodel.FieldValueRow)
	for _, row := range fieldRows {
		fieldName := strings.ToLower(strings.TrimSpace(row.FieldName))
		if strings.TrimSpace(row.NodeID) == "" || fieldName == "" {
			continue
		}
		if fieldsByNodeID[row.NodeID] == nil {
			fieldsByNodeID[row.NodeID] = map[string][]readmodel.FieldValueRow{}
		}
		fieldsByNodeID[row.NodeID][fieldName] = append(fieldsByNodeID[row.NodeID][fieldName], row)
	}
	out := make([]*sectionRecord, 0, len(rows))
	for _, row := range rows {
		if row.NodeKind != string(ontology.NodeKindEmbedded) {
			continue
		}
		ref := nodeRefFromQueryCatalogRow(row)
		node := &ontology.SectionNode{
			ID:        ref.NodeID,
			NotePath:  ref.NotePath,
			Title:     firstNonEmpty(row.DisplayLabel, row.Title, row.SourceLocator, row.NotePath),
			BlockID:   row.BlockID,
			StartByte: int(row.StartByte),
			EndByte:   int(row.EndByte),
		}
		noteTypeName := typeRows[row.NotePath].TypeName
		out = append(out, &sectionRecord{
			Node:          node,
			Note:          &noteRecord{Path: row.NotePath, TypeName: noteTypeName},
			TypeName:      row.TypeName,
			catalogRef:    ref,
			PropertyCase:  e.propertyCaseForType(noteTypeName),
			indexedFields: fieldsByNodeID[row.NodeID],
		})
	}
	return out, nil
}

func (e *executor) sectionRecordsFromNodeListItems(ctx context.Context, items []ontology.NodeListItem) ([]*sectionRecord, error) {
	out := make([]*sectionRecord, 0, len(items))
	for _, item := range items {
		ref := item.Ref
		if ref.NotePath == "" {
			ref.NotePath = item.NotePath
		}
		if ref.Kind == "" {
			ref.Kind = ontology.NodeKindEmbedded
		}
		note, err := e.rootNote(ctx, ref.NotePath)
		if err != nil {
			return nil, err
		}
		if note == nil {
			continue
		}
		noteProjection, err := e.projectNoteRecord(ctx, note)
		if err != nil || noteProjection == nil {
			return nil, err
		}
		projection, err := ontology.ProjectNodeFromSnapshot(noteProjection.Snapshot, e.schema, ref)
		if err != nil || projection == nil {
			continue
		}
		out = append(out, e.sectionRecordFromProjection(note, projection))
	}
	return out, nil
}

func (e *executor) noteRecordsFromCatalogRows(ctx context.Context, rows []readmodel.NodeRow) ([]*noteRecord, error) {
	pathsList := make([]string, 0, len(rows))
	typeByPath := make(map[string]string, len(rows))
	for _, row := range rows {
		if row.NodeKind != string(ontology.NodeKindNote) || strings.TrimSpace(row.NotePath) == "" {
			continue
		}
		if _, seen := typeByPath[row.NotePath]; !seen {
			pathsList = append(pathsList, row.NotePath)
			typeByPath[row.NotePath] = row.TypeName
		}
	}
	records, err := e.loaders.recordsForPaths(ctx, pathsList)
	if err != nil {
		return nil, err
	}
	for _, record := range records {
		if record.TypeName == "" {
			record.TypeName = typeByPath[record.Path]
		}
	}
	return records, nil
}

func nodeRefFromQueryCatalogRow(row readmodel.NodeRow) ontology.NodeRef {
	var ref ontology.NodeRef
	if err := json.Unmarshal([]byte(row.NodeRefJSON), &ref); err != nil {
		ref = ontology.NodeRef{NotePath: row.NotePath, Kind: ontology.NodeKind(row.NodeKind), NodeID: row.NodeID, TypeName: row.TypeName, Fragment: row.Fragment}
	}
	if ref.NotePath == "" {
		ref.NotePath = row.NotePath
	}
	if ref.NodeID == "" {
		ref.NodeID = row.NodeID
	}
	if ref.TypeName == "" {
		ref.TypeName = row.TypeName
	}
	if ref.Kind == "" {
		ref.Kind = ontology.NodeKind(row.NodeKind)
	}
	if ref.Fragment == "" {
		ref.Fragment = row.Fragment
	}
	return ref
}

func (e *executor) addError(path []string, message string) {
	e.errors = append(e.errors, Error{Message: message, Path: cloneStrings(path)})
}

func (e *executor) addErrorWithExtensions(path []string, message string, extensions map[string]any) {
	e.errors = append(e.errors, Error{Message: message, Path: cloneStrings(path), Extensions: extensions})
}

func (e *executor) resolvedType(ctx context.Context, notePath string) (semdb.OntologyNoteTypeRow, bool, error) {
	rows, err := e.loaders.ontologyTypesByPaths(ctx, []string{notePath})
	row, ok := rows[notePath]
	return row, ok, err
}

func (e *executor) noteAssessment(ctx context.Context, notePath string) *ontology.NoteAssessment {
	assessments, err := e.loaders.assessmentsByPaths(ctx, []string{notePath})
	if err != nil {
		return nil
	}
	return assessments[notePath]
}

func (e *executor) prefetchAssessments(ctx context.Context, records []*noteRecord) {
	pathsList := make([]string, 0, len(records))
	for _, record := range records {
		if record != nil && record.Path != "" {
			pathsList = append(pathsList, record.Path)
		}
	}
	_, _ = e.loaders.assessmentsByPaths(ctx, pathsList)
}

func dedupeOntologyEdgeRows(rows []semdb.OntologyEdgeRow) []semdb.OntologyEdgeRow {
	if len(rows) < 2 {
		return rows
	}
	seen := make(map[string]struct{}, len(rows))
	out := make([]semdb.OntologyEdgeRow, 0, len(rows))
	for _, row := range rows {
		key := row.DstPath
		if key == "" {
			key = row.SrcPath + "\x00" + row.RelationName + "\x00" + row.Provenance
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, row)
	}
	return out
}

func (e *executor) noteMatchesTypeCondition(node *noteRecord, typeCondition string) bool {
	typeCondition = strings.TrimSpace(typeCondition)
	switch typeCondition {
	case "", "Node", "NoteNode":
		return true
	case "Note":
		return true
	default:
		return e.typeMatchesOrImplements(node.TypeName, typeCondition)
	}
}

func (e *executor) sectionMatchesTypeCondition(node *sectionRecord, typeCondition string) bool {
	typeCondition = strings.TrimSpace(typeCondition)
	switch typeCondition {
	case "", "Node", "Section":
		return true
	case "EmbeddedNode":
		return e.sectionNodeRef(node).Kind == ontology.NodeKindEmbedded
	default:
		return e.typeMatchesOrImplements(node.TypeName, typeCondition)
	}
}

func codeMatchesTypeCondition(node *codeRecord, typeCondition string) bool {
	typeCondition = strings.TrimSpace(typeCondition)
	switch typeCondition {
	case "", "Node":
		return true
	case "CodeFile":
		return node != nil && node.Anchor == nil
	case "CodeSymbol":
		return node != nil && node.Anchor != nil
	default:
		return false
	}
}

func (e *executor) typeMatchesOrImplements(typeName, expected string) bool {
	typeName = strings.TrimSpace(typeName)
	expected = strings.TrimSpace(expected)
	if typeName == "" || expected == "" {
		return false
	}
	if expected == "Node" {
		return true
	}
	if expected == "Note" && typeName != "" {
		noteType := e.schema.Types[typeName]
		return noteType != nil && noteType.Role == ontoschema.TypeRoleNote
	}
	if typeName == expected {
		return true
	}
	noteType := e.schema.Types[typeName]
	if noteType != nil {
		for _, iface := range noteType.Implements {
			if iface == expected {
				return true
			}
			if e.interfaceImplements(iface, expected) {
				return true
			}
		}
	}
	return e.interfaceImplements(typeName, expected)
}

func (e *executor) interfaceImplements(typeName, expected string) bool {
	if typeName == expected {
		return true
	}
	if typeName == "Section" && expected == "Section" {
		return true
	}
	iface := e.schema.Interfaces[typeName]
	if iface == nil {
		return false
	}
	for _, name := range iface.Implements {
		if name == expected || e.interfaceImplements(name, expected) {
			return true
		}
	}
	return false
}
