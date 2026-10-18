package query

import (
	"context"
	"fmt"
	"github.com/atomicobject/rhizome/pkg/ontology/readmodel"
	ontoschema "github.com/atomicobject/rhizome/pkg/ontology/schema"
	"strings"

	"github.com/atomicobject/rhizome/pkg/diag"
	"github.com/atomicobject/rhizome/pkg/ontology/noderead"
	"github.com/atomicobject/rhizome/pkg/ontology/pushdown"
	"github.com/vektah/gqlparser/v2/ast"
)

func (e *executor) rootEmbeddedNodes(ctx context.Context, typeName string, args map[string]any, set ast.SelectionSet) (rootPage[*sectionRecord], error) {
	if err := e.validateLinkRootFilters(ctx, typeName, args["filters"]); err != nil {
		return rootPage[*sectionRecord]{}, err
	}
	return e.indexedRootEmbeddedNodes(ctx, typeName, args, set)
}

func (e *executor) indexedRootEmbeddedNodes(ctx context.Context, typeName string, args map[string]any, set ast.SelectionSet) (rootPage[*sectionRecord], error) {
	candidateBound := false
	noteType := e.schema.Types[typeName]
	if noteType == nil || noteType.Role != ontoschema.TypeRoleEmbeddedNode {
		return rootPage[*sectionRecord]{}, nil
	}
	rootPlan := e.indexedOntologyNodeRootPlan(ctx, typeName, args)
	plan, residualFilters, residualSort := rootPlan.indexed, rootPlan.residual, rootPlan.sort
	offset := intValue(args["offset"], 0)
	if residualFilters == nil && residualSort == nil && (e.deps.ReadOverlay == nil || e.deps.ReadOverlay.Empty()) {
		plan.Offset = offset
	} else if offset >= publicRootFirstMax {
		return rootPage[*sectionRecord]{}, fmt.Errorf("%s(...) offset must be < %d for residual filters or sort; narrow the query to continue", queryRootFieldName(typeName), publicRootFirstMax)
	}
	if e.deps.ReadOverlay != nil && !e.deps.ReadOverlay.Empty() {
		result, err := e.loaders.scope.TypeInstances(ctx, noderead.TypeInstancesRequest{
			TypeName:   typeName,
			Limit:      publicRootFirstMax,
			Predicates: plan.Predicates,
			Sort:       plan.Sort,
		})
		if err != nil {
			return rootPage[*sectionRecord]{}, err
		}
		if len(result.Items) >= publicRootFirstMax {
			candidateBound = true
		}
		records, err := e.sectionRecordsFromNodeListItems(ctx, result.Items)
		if err != nil {
			return rootPage[*sectionRecord]{}, err
		}
		if residualFilters != nil {
			filtered := records[:0]
			for _, record := range records {
				matches, err := e.sectionMatchesRootFilters(record, residualFilters)
				if err != nil {
					return rootPage[*sectionRecord]{}, err
				}
				if matches {
					filtered = append(filtered, record)
				}
			}
			records = filtered
		}
		if residualSort != nil {
			e.sortEmbeddedRootSections(ctx, records, residualSort)
		}
		records = dropRootOffset(records, offset)
		first := rootFirst(args, false)
		if first > 0 && len(records) > first {
			records = records[:first]
		}
		if e.ontologyNodeRootSelectionNeedsProjection(typeName, set) {
			for i, record := range records {
				projection, err := e.projectSectionRecord(ctx, record)
				if err != nil {
					return rootPage[*sectionRecord]{}, err
				}
				if projection != nil {
					records[i] = e.sectionRecordFromProjection(record.Note, projection)
				}
			}
		}
		return rootPage[*sectionRecord]{records: records, candidateBound: candidateBound}, nil
	}
	rows, err := e.deps.Store.OntologyNodesByTypePlan(ctx, plan)
	if err != nil {
		return rootPage[*sectionRecord]{}, err
	}
	if (residualFilters != nil || residualSort != nil) && len(rows) >= publicRootFirstMax {
		candidateBound = true
	}
	records, err := e.sectionRecordsFromCatalogRows(ctx, rows)
	if err != nil {
		return rootPage[*sectionRecord]{}, err
	}
	if residualFilters != nil {
		filtered := records[:0]
		for _, record := range records {
			matches, err := e.sectionMatchesRootFilters(record, residualFilters)
			if err != nil {
				return rootPage[*sectionRecord]{}, err
			}
			if matches {
				filtered = append(filtered, record)
			}
		}
		records = filtered
	}
	if residualSort != nil {
		e.sortEmbeddedRootSections(ctx, records, residualSort)
	}
	if plan.Offset == 0 {
		records = dropRootOffset(records, offset)
	}
	first := rootFirst(args, false)
	if first > 0 && len(records) > first {
		records = records[:first]
	}
	if e.ontologyNodeRootSelectionNeedsProjection(typeName, set) {
		for i, record := range records {
			projection, err := e.projectSectionRecord(ctx, record)
			if err != nil {
				return rootPage[*sectionRecord]{}, err
			}
			if projection != nil {
				records[i] = e.sectionRecordFromProjection(record.Note, projection)
			}
		}
	}
	return rootPage[*sectionRecord]{records: records, candidateBound: candidateBound}, nil
}

func dropRootOffset[T any](records []T, offset int) []T {
	if offset <= 0 {
		return records
	}
	if offset >= len(records) {
		return records[:0]
	}
	return records[offset:]
}

func (e *executor) ontologyNodeRootSelectionNeedsProjection(typeName string, set ast.SelectionSet) bool {
	noteType := e.schema.Types[typeName]
	if noteType == nil {
		return true
	}
	for _, selection := range set {
		switch current := selection.(type) {
		case *ast.Field:
			if e.ontologyNodeRootFieldNeedsProjection(noteType, current) {
				return true
			}
		case *ast.InlineFragment:
			if e.sectionMatchesTypeName(typeName, current.TypeCondition) && e.ontologyNodeRootSelectionNeedsProjection(typeName, current.SelectionSet) {
				return true
			}
		case *ast.FragmentSpread:
			fragment := e.fragments.ForName(current.Name)
			if fragment != nil && e.sectionMatchesTypeName(typeName, fragment.TypeCondition) && e.ontologyNodeRootSelectionNeedsProjection(typeName, fragment.SelectionSet) {
				return true
			}
		default:
			return true
		}
	}
	return false
}

func (e *executor) ontologyNodeRootFieldNeedsProjection(noteType *ontoschema.NoteType, field *ast.Field) bool {
	if noteType == nil || field == nil {
		return true
	}
	switch field.Name {
	case "__typename", "ref", "nodeId", "nodeKind", "id", "path", "notePath", "title", "resolvedType", "locator", "neighborhood", "localGraph", "format", "sourceRepresentation", "evidenceRepresentation", "sourceCapabilities":
		return false
	case "workspace":
		return true
	case "content", "children", "level":
		return true
	}
	schemaField := noteType.ByName[field.Name]
	if schemaField == nil {
		return !isRecordFactField(field.Name)
	}
	switch schemaField.Kind {
	case ontoschema.FieldKindScalar, ontoschema.FieldKindEnum, ontoschema.FieldKindLink:
		_, ok := ontoschema.FieldQueryCapabilityForField(e.schema, schemaField)
		return !ok
	case ontoschema.FieldKindSection, ontoschema.FieldKindNeighbor, ontoschema.FieldKindReverse:
		return true
	default:
		return true
	}
}

func (e *executor) sectionMatchesTypeName(typeName, condition string) bool {
	condition = strings.TrimSpace(condition)
	if condition == "" || condition == typeName || condition == "Section" || condition == "Node" {
		return true
	}
	return e.typeMatchesOrImplements(typeName, condition)
}

// indexedOntologyNodeRootPlan plans the indexed ontology node query for one
// public GraphQL root (e.g. {technicalSpec(filters:..., sort:...)}). Filters
// the planner cannot push become residual filters; sort uses prefix-match
// (push the maximal supported prefix; first unsupported key + tail go
// residual). When any residual remains, the limit is widened to
// publicRootFirstMax so the in-memory operation sees the full bounded
// candidate set.
type typedRootPlan struct {
	indexed  readmodel.NodeQueryPlan
	residual []pushdown.FilterInput
	sort     []pushdown.SortInput
	warnings []diag.Warning
}

func (e *executor) indexedOntologyNodeRootPlan(ctx context.Context, typeName string, args map[string]any) typedRootPlan {
	first := rootFirst(args, false)
	plan := readmodel.NodeQueryPlan{TypeNames: []string{typeName}, Limit: first}
	if plan.Limit <= 0 {
		plan.Limit = publicRootFirstMax
	}
	planner := pushdown.Planner{
		Schema:       e.schema,
		Resolver:     pushdown.SchemaResolver{NoteType: e.schema.Types[typeName]},
		LinkResolver: e.linkTargetResolver,
	}
	var filterInputs []pushdown.FilterInput
	for _, raw := range anyList(args["filters"]) {
		if filter, ok := raw.(map[string]any); ok {
			filterInputs = append(filterInputs, graphqlFilterToInput(filter))
		}
	}
	compiled := planner.Plan(ctx, filterInputs, nil)
	plan.Predicates = compiled.Pushed.Predicates
	warnings := compiled.Warnings
	residualFilters := compiled.Residual
	if residualFilters != nil {
		plan.Limit = publicRootFirstMax
	}
	rawSort, _ := args["sort"].([]any)
	if len(rawSort) == 0 {
		return typedRootPlan{indexed: plan, residual: residualFilters, warnings: warnings}
	}
	inputs := make([]pushdown.SortInput, 0, len(rawSort))
	for _, item := range rawSort {
		sortMap, ok := item.(map[string]any)
		if !ok {
			continue
		}
		inputs = append(inputs, pushdown.SortInput{
			Field:     stringValue(sortMap["field"]),
			Direction: stringValue(sortMap["direction"]),
		})
	}
	// Sorts alone may name the updatedAt record fact, which the store orders by
	// note modification time; filters keep the schema resolver so it never
	// becomes a path predicate. An edit overlay re-sorts in memory, where
	// updatedAt is unknown, so there it stays a residual sort.
	sortPlanner := planner
	if recordFactFree(e.schema, e.schema.Types[typeName], recordFieldUpdatedAt) && (e.deps.ReadOverlay == nil || e.deps.ReadOverlay.Empty()) {
		sortPlanner.Resolver = updatedAtSortResolver{planner.Resolver}
	}
	sortPlan := sortPlanner.Plan(ctx, nil, inputs)
	plan.Sort = sortPlan.Pushed.Sort
	if len(sortPlan.Sort) == 0 {
		return typedRootPlan{indexed: plan, residual: residualFilters, warnings: warnings}
	}

	// WHY: residual sort runs over the indexed candidate set after the
	// store query returns. Truncating the candidate set to the requested
	// `first` before sorting would let the catalog's note-path/start-byte
	// order win and silently drop rows that should rank higher under the
	// requested sort. Load up to the residual cap so the client-side sort
	// sees the full bounded candidate set.
	plan.Limit = publicRootFirstMax
	// Reapply the full requested sort over the candidate set so the pushed
	// prefix remains semantically primary; the in-memory sort sees a stable,
	// coherent comparator. This matches the configured-views residual-sort
	// contract.
	return typedRootPlan{indexed: plan, residual: residualFilters, sort: sortPlan.Sort, warnings: warnings}
}

func anyList(raw any) []any {
	items, _ := raw.([]any)
	return items
}

func graphqlFiltersFromInputs(inputs []pushdown.FilterInput) any {
	if len(inputs) == 0 {
		return nil
	}
	filters := make([]any, len(inputs))
	for i, input := range inputs {
		filters[i] = map[string]any{"field": input.Field, "op": input.Op, "value": input.Value, "values": input.Values}
	}
	return filters
}

// graphqlFilterToInput adapts an untyped GraphQL filter map to the planner's
// FilterInput shape.
func graphqlFilterToInput(filter map[string]any) pushdown.FilterInput {
	return pushdown.FilterInput{
		Field:  stringValue(filter["field"]),
		Op:     stringValue(filter["op"]),
		Value:  stringValue(filter["value"]),
		Values: stringListValue(filter["values"]),
	}
}

func rootFilterInputs(raw any) []pushdown.FilterInput {
	if inputs, ok := raw.([]pushdown.FilterInput); ok {
		return inputs
	}
	var inputs []pushdown.FilterInput
	for _, item := range anyList(raw) {
		if filter, ok := item.(map[string]any); ok {
			inputs = append(inputs, graphqlFilterToInput(filter))
		}
	}
	return inputs
}

func rootSortInputs(raw any) []pushdown.SortInput {
	if inputs, ok := raw.([]pushdown.SortInput); ok {
		return inputs
	}
	var inputs []pushdown.SortInput
	for _, item := range anyList(raw) {
		if spec, ok := item.(map[string]any); ok {
			inputs = append(inputs, pushdown.SortInput{Field: stringValue(spec["field"]), Direction: stringValue(spec["direction"])})
		}
	}
	return inputs
}

func graphqlSortsFromInputs(inputs []pushdown.SortInput) any {
	if len(inputs) == 0 {
		return nil
	}
	sorts := make([]any, len(inputs))
	for i, input := range inputs {
		sorts[i] = map[string]any{"field": input.Field, "direction": input.Direction}
	}
	return sorts
}
