package query

import (
	"context"
	ontoschema "github.com/atomicobject/rhizome/pkg/ontology/schema"
	"slices"
	"strconv"
	"strings"

	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/ontology/noderead"
	semdb "github.com/atomicobject/rhizome/pkg/store"
	"github.com/vektah/gqlparser/v2/ast"
)

func (e *executor) resolveNeighborField(ctx context.Context, node *noteRecord, field *ontoschema.Field, set ast.SelectionSet, path []string, first int) (any, bool) {
	typeFilter := field.TypeName
	if _, ok := e.schema.Interfaces[typeFilter]; ok {
		typeFilter = ""
	}
	rows, ok := e.resolveAmbientRows(ctx, node.Path, "", directionProvenance(field.Direction), typeFilter, 0, path)
	if !ok {
		return nil, false
	}
	records, err := e.edgeRecords(ctx, e.firstEdgeRows(rows, field.TypeName, first))
	if err != nil {
		e.addError(path, err.Error())
		return nil, false
	}
	records = firstRecords(e.filterRecordsByType(records, field.TypeName), first)
	return e.resolveNodeList(ctx, records, set, path), true
}

const relationCacheSeparator = "\x00first="

// relationCacheName names a relation selection's batched read by field and
// first, so selecting one field with different limits (`all: children` and
// `children(first: 2)`) keeps each limit's targets apart.
func relationCacheName(field string, first int) string {
	if first <= 0 {
		return field
	}
	return field + relationCacheSeparator + strconv.Itoa(first)
}

// relationFirst is a list relation field's first argument, or 0 (every
// target) when it is absent or not positive.
func (e *executor) relationFirst(field *ast.Field) int {
	return max(intValue(e.fieldArgs(field)["first"], 0), 0)
}

// firstEdgeRows keeps the first rows whose target can be of the expected type
// (an untyped row may still resolve to it), so a capped relation hydrates no
// more than it returns; 0 keeps every row.
func (e *executor) firstEdgeRows(rows []semdb.OntologyEdgeRow, expected string, first int) []semdb.OntologyEdgeRow {
	if first <= 0 {
		return rows
	}
	out := make([]semdb.OntologyEdgeRow, 0, min(first, len(rows)))
	for _, row := range rows {
		if len(out) == first {
			break
		}
		if row.DstType == "" || expected == "" || e.typeMatchesOrImplements(row.DstType, expected) {
			out = append(out, row)
		}
	}
	return out
}

// firstRecords keeps a resolved relation's first records; 0 keeps all.
func firstRecords(records []*noteRecord, first int) []*noteRecord {
	if first > 0 && len(records) > first {
		return records[:first]
	}
	return records
}

func (e *executor) resolveReverseField(ctx context.Context, ref ontology.NodeRef, field *ontoschema.Field, set ast.SelectionSet, path []string, first int) (any, bool) {
	refs, prefetched := e.reverseRelations[noderead.RefIdentityKey(ref)][relationCacheName(field.Name, first)]
	if !prefetched {
		result, err := e.loaders.scope.Edges(ctx, noderead.NeighborhoodRequest{
			Sources: []ontology.NodeRef{ref}, Direction: noderead.TraversalDirectionInbound,
			RelationNames: []string{field.ReverseField}, IncludeStructural: true,
		})
		if err != nil {
			e.addError(path, err.Error())
			return nil, false
		}
		refs = make([]ontology.NodeRef, 0, len(result.Edges))
		for _, edge := range result.Edges {
			refs = append(refs, edge.Target)
		}
	}
	// Keep the first targets of the field's type before hydrating any.
	refs = slices.DeleteFunc(slices.Clone(refs), func(target ontology.NodeRef) bool {
		return !e.typeMatchesOrImplements(target.TypeName, field.TypeName)
	})
	if first > 0 && len(refs) > first {
		refs = refs[:first]
	}
	paths := make([]string, 0, len(refs))
	for _, target := range refs {
		if target.Kind == ontology.NodeKindNote && e.typeMatchesOrImplements(target.TypeName, field.TypeName) {
			paths = append(paths, target.NotePath)
		}
	}
	records, err := e.loaders.recordsForPaths(ctx, paths)
	if err != nil {
		e.addError(path, err.Error())
		return nil, false
	}
	notesByPath := make(map[string]*noteRecord, len(records))
	for _, record := range records {
		notesByPath[record.Path] = record
	}
	// Keep full refs through this step. Several embedded nodes can share one
	// note path, but each is a separate reverse relation target.
	type targetIndex struct {
		note  bool
		index int
	}
	order := make([]targetIndex, 0, len(refs))
	notes := make([]*noteRecord, 0, len(records))
	sections := make([]*sectionRecord, 0)
	for _, target := range refs {
		if !e.typeMatchesOrImplements(target.TypeName, field.TypeName) {
			continue
		}
		if target.Kind == ontology.NodeKindEmbedded || target.Kind == ontology.NodeKindSection {
			section, err := e.rootSection(ctx, target)
			if err != nil {
				e.addError(path, err.Error())
				return nil, false
			}
			if section != nil && e.typeMatchesOrImplements(section.TypeName, field.TypeName) {
				order = append(order, targetIndex{index: len(sections)})
				sections = append(sections, section)
			}
			continue
		}
		if note := notesByPath[target.NotePath]; note != nil && e.typeMatchesOrImplements(note.TypeName, field.TypeName) {
			order = append(order, targetIndex{note: true, index: len(notes)})
			notes = append(notes, note)
		}
	}
	noteValues := e.resolveNodeList(ctx, notes, set, path)
	sectionValues := e.resolveSectionList(ctx, sections, set, path)
	out := make([]any, 0, len(order))
	for _, target := range order {
		if target.note {
			out = append(out, noteValues[target.index])
		} else {
			out = append(out, sectionValues[target.index])
		}
	}
	return out, true
}

func (e *executor) resolveAmbientBuiltin(ctx context.Context, node *noteRecord, field *ast.Field, path []string) (any, bool) {
	args := e.fieldArgs(field)
	typeFilter := strings.TrimSpace(stringValue(args["type"]))
	first := intValue(args["first"], 20)
	if first <= 0 {
		first = 20
	}
	loadType := typeFilter
	loadFirst := first
	if _, ok := e.schema.Interfaces[typeFilter]; ok {
		loadType = ""
		loadFirst = 0
	}
	rows, ok := e.resolveAmbientRows(ctx, node.Path, "", builtinProvenance(field.Name), loadType, loadFirst, path)
	if !ok {
		return nil, false
	}
	records, err := e.edgeRecords(ctx, rows)
	if err != nil {
		e.addError(path, err.Error())
		return nil, false
	}
	records = e.filterRecordsByType(records, typeFilter)
	if first > 0 && len(records) > first {
		records = records[:first]
	}
	return e.resolveNodeList(ctx, records, field.SelectionSet, path), true
}

func (e *executor) resolveAmbientRows(ctx context.Context, notePath, relation string, provenance map[string]struct{}, typeFilter string, first int, path []string) ([]semdb.OntologyEdgeRow, bool) {
	grouped, err := e.loaders.ambientEdges(ctx, []string{notePath}, relation, provenance, typeFilter, first)
	if err != nil {
		e.addError(path, err.Error())
		return nil, false
	}
	return dedupeOntologyEdgeRows(grouped[notePath]), true
}

func (e *executor) edgeRecords(ctx context.Context, rows []semdb.OntologyEdgeRow) ([]*noteRecord, error) {
	pathsList := make([]string, 0, len(rows))
	for _, row := range rows {
		pathsList = append(pathsList, row.DstPath)
	}
	records, err := e.loaders.recordsForPaths(ctx, pathsList)
	if err != nil {
		return nil, err
	}
	byPath := make(map[string]string, len(rows))
	for _, row := range rows {
		byPath[row.DstPath] = row.DstType
	}
	for _, record := range records {
		if typeName := byPath[record.Path]; typeName != "" {
			record.TypeName = typeName
		}
	}
	return records, nil
}

func (e *executor) filterRecordsByType(records []*noteRecord, expected string) []*noteRecord {
	expected = strings.TrimSpace(expected)
	if expected == "" {
		return records
	}
	out := make([]*noteRecord, 0, len(records))
	for _, record := range records {
		if record == nil || !e.typeMatchesOrImplements(record.TypeName, expected) {
			continue
		}
		out = append(out, record)
	}
	return out
}
