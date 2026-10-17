package query

import (
	"context"
	"fmt"
	ontoschema "github.com/atomicobject/rhizome/pkg/ontology/schema"
	"sort"
	"strings"

	"github.com/atomicobject/rhizome/pkg/ontology/noderead"
	"github.com/atomicobject/rhizome/pkg/ontology/pushdown"
)

func (e *executor) rootNotesWithMax(ctx context.Context, args map[string]any, maxFirst int) ([]*noteRecord, error) {
	typeFilter := strings.TrimSpace(stringValue(args["type"]))
	findArg := strings.TrimSpace(stringValue(args["find"]))
	semanticArgs := semanticArgList(args["semantic"])
	first := rootFirstMax(args, len(semanticArgs) > 0, maxFirst)

	switch {
	case findArg != "":
		return e.findTypedNotes(ctx, findArg, typeFilter, first)
	case args["property"] != nil:
		propertyArgs, _ := args["property"].(map[string]any)
		return e.propertyTypedNotes(ctx, propertyArgs, typeFilter, first)
	case len(semanticArgs) > 0:
		if typeFilter != "" {
			return e.semanticTypedNotes(ctx, semanticArgs, typeFilter, first)
		}
		return e.semanticNotes(ctx, semanticArgs, first)
	case typeFilter != "":
		if _, ok := e.schema.Interfaces[typeFilter]; ok {
			rows, err := e.loaders.selectorMetadataRows(ctx)
			if err != nil {
				return nil, err
			}
			pathsList := make([]string, 0, len(rows))
			for _, row := range rows {
				pathsList = append(pathsList, row.Path)
			}
			sort.Strings(pathsList)
			return e.filterTypedRecords(ctx, pathsList, typeFilter, first)
		}
		return e.loaders.noteRowsByType(ctx, typeFilter, first)
	default:
		return nil, fmt.Errorf("notes(...) requires at least one of type, find, property, or semantic")
	}
}

func (e *executor) rootTypedNotes(ctx context.Context, typeName string, args map[string]any) (rootPage[*noteRecord], error) {
	if err := e.validateLinkRootFilters(ctx, typeName, args["filters"]); err != nil {
		return rootPage[*noteRecord]{}, err
	}
	pathArg := strings.TrimSpace(stringValue(args["path"]))
	findArg := strings.TrimSpace(stringValue(args["find"]))
	semanticArgs := semanticArgList(args["semantic"])
	first := rootFirst(args, len(semanticArgs) > 0)
	var records []*noteRecord
	var err error

	switch {
	case pathArg != "":
		if intValue(args["offset"], 0) > 0 {
			return rootPage[*noteRecord]{}, nil
		}
		record, err := e.rootNote(ctx, pathArg)
		if err != nil {
			return rootPage[*noteRecord]{}, err
		}
		if record == nil || record.TypeName != typeName {
			return rootPage[*noteRecord]{}, nil
		}
		records = []*noteRecord{record}
	case findArg != "":
		records, err = e.findTypedNotes(ctx, findArg, typeName, first)
	case args["property"] != nil:
		propertyArgs, _ := args["property"].(map[string]any)
		records, err = e.propertyTypedNotes(ctx, propertyArgs, typeName, first)
	case len(semanticArgs) > 0:
		records, err = e.semanticTypedNotes(ctx, semanticArgs, typeName, first)
	default:
		return e.indexedRootTypedNotes(ctx, typeName, args)
	}
	if err != nil {
		return rootPage[*noteRecord]{}, err
	}
	limited, _ := args["__selectorCandidateLimit"].(bool)
	candidateBound := limited && len(records) >= publicRootFirstMax
	records, err = e.applyNoteRootConstraints(ctx, records, args)
	return rootPage[*noteRecord]{records: records, candidateBound: candidateBound}, err
}

func (e *executor) validateLinkRootFilters(ctx context.Context, typeName string, raw any) error {
	noteType := e.schema.Types[typeName]
	if noteType == nil {
		return nil
	}
	filters, _ := raw.([]any)
	for _, item := range filters {
		filter, _ := item.(map[string]any)
		field := noteType.ByName[strings.TrimSpace(stringValue(filter["field"]))]
		if field == nil || field.Kind != ontoschema.FieldKindLink {
			continue
		}
		op := strings.ToLower(strings.TrimSpace(stringValue(filter["op"])))
		if op == "" {
			op = "eq"
		}
		if op != "eq" && op != "in" && op != "exists" {
			return fmt.Errorf("%s.%s is a link: operator %q is unsupported; use eq or in for resolved target membership, or exists", typeName, field.Name, op)
		}
		if op == "exists" {
			continue
		}
		values := []string{stringValue(filter["value"])}
		if op == "in" {
			values = stringListValue(filter["values"])
		}
		if len(values) == 0 {
			return fmt.Errorf("%s.%s link filter %s requires a target value", typeName, field.Name, op)
		}
		for _, value := range values {
			resolved, warning := e.linkTargetResolver.ResolveLinkTarget(ctx, field, value)
			if resolved == "" {
				return fmt.Errorf("%s.%s: %s", typeName, field.Name, warning.Message)
			}
		}
	}
	return nil
}

func (e *executor) indexedRootTypedNotes(ctx context.Context, typeName string, args map[string]any) (rootPage[*noteRecord], error) {
	candidateBound := false
	noteType := e.schema.Types[typeName]
	if noteType == nil || noteType.Role != ontoschema.TypeRoleNote {
		return rootPage[*noteRecord]{}, nil
	}
	rootPlan := e.indexedOntologyNodeRootPlan(ctx, typeName, args)
	plan, residualFilters, residualSort := rootPlan.indexed, rootPlan.residual, rootPlan.sort
	offset := intValue(args["offset"], 0)
	if residualFilters == nil && residualSort == nil && (e.deps.ReadOverlay == nil || e.deps.ReadOverlay.Empty()) {
		plan.Offset = offset
	} else if offset >= publicRootFirstMax {
		return rootPage[*noteRecord]{}, fmt.Errorf("%s(...) offset must be < %d for residual filters or sort; narrow the query to continue", queryRootFieldName(typeName), publicRootFirstMax)
	}
	if e.deps.ReadOverlay != nil && !e.deps.ReadOverlay.Empty() {
		result, err := e.loaders.scope.TypeInstances(ctx, noderead.TypeInstancesRequest{
			TypeName:   typeName,
			Limit:      publicRootFirstMax,
			Predicates: plan.Predicates,
			Sort:       plan.Sort,
		})
		if err != nil {
			return rootPage[*noteRecord]{}, err
		}
		if len(result.Items) >= publicRootFirstMax {
			candidateBound = true
		}
		pathsList := make([]string, 0, len(result.Items))
		for _, item := range result.Items {
			if strings.TrimSpace(item.NotePath) != "" {
				pathsList = append(pathsList, item.NotePath)
			}
		}
		records, err := e.loaders.recordsForPaths(ctx, pathsList)
		if err != nil {
			return rootPage[*noteRecord]{}, err
		}
		records, err = e.applyNoteRootConstraints(ctx, records, map[string]any{
			"filters": residualFilters,
			"sort":    residualSort,
			"first":   args["first"],
			"offset":  offset,
		})
		return rootPage[*noteRecord]{records: records, candidateBound: candidateBound}, err
	}
	rows, err := e.deps.Store.OntologyNodesByTypePlan(ctx, plan)
	if err != nil {
		return rootPage[*noteRecord]{}, err
	}
	if (residualFilters != nil || residualSort != nil) && len(rows) >= publicRootFirstMax {
		candidateBound = true
	}
	records, err := e.noteRecordsFromCatalogRows(ctx, rows)
	if err != nil {
		return rootPage[*noteRecord]{}, err
	}
	residualArgs := map[string]any{
		"filters": residualFilters,
		"sort":    residualSort,
		"first":   args["first"],
	}
	if plan.Offset == 0 {
		residualArgs["offset"] = offset
	}
	records, err = e.applyNoteRootConstraints(ctx, records, residualArgs)
	return rootPage[*noteRecord]{records: records, candidateBound: candidateBound}, err
}

func (e *executor) applyNoteRootConstraints(ctx context.Context, records []*noteRecord, args map[string]any) ([]*noteRecord, error) {
	if args == nil {
		return records, nil
	}
	if rawFilters := args["filters"]; rawFilters != nil {
		filters, linkTargets, err := e.resolvedNoteLinkFilters(ctx, records, rawFilters)
		if err != nil {
			return nil, err
		}
		filtered := records[:0]
		for _, record := range records {
			matches, err := e.noteMatchesRootFilters(record, filters, linkTargets)
			if err != nil {
				return nil, err
			}
			if matches {
				filtered = append(filtered, record)
			}
		}
		records = filtered
	}
	if rawSort := args["sort"]; rawSort != nil {
		e.sortRootNotes(ctx, records, rawSort)
	}
	if offset := intValue(args["offset"], 0); offset > 0 {
		if offset >= len(records) {
			return records[:0], nil
		}
		records = records[offset:]
	}
	first := rootFirst(args, false)
	if first > 0 && len(records) > first {
		records = records[:first]
	}
	return records, nil
}

func (e *executor) resolvedNoteLinkFilters(ctx context.Context, records []*noteRecord, raw any) ([]pushdown.FilterInput, map[string]map[string][]string, error) {
	filters := rootFilterInputs(raw)
	if len(filters) == 0 || len(records) == 0 {
		return filters, nil, nil
	}
	paths := make([]string, 0, len(records))
	for _, record := range records {
		paths = append(paths, record.Path)
	}
	resolved := append([]pushdown.FilterInput(nil), filters...)
	targets := map[string]map[string][]string{}
	loaded := map[string]bool{}
	for i, filter := range filters {
		fieldName := strings.TrimSpace(filter.Field)
		var field *ontoschema.Field
		for _, record := range records {
			if noteType := e.schema.Types[record.TypeName]; noteType != nil && noteType.ByName[fieldName] != nil {
				field = noteType.ByName[fieldName]
				break
			}
		}
		if field == nil || field.Kind != ontoschema.FieldKindLink {
			resolved[i] = filter
			continue
		}
		if strings.EqualFold(filter.Op, "in") {
			canonical := make([]string, 0, len(filter.Values))
			for _, value := range filter.Values {
				path, warning := e.linkTargetResolver.ResolveLinkTarget(ctx, field, value)
				if path == "" {
					return nil, nil, fmt.Errorf("%s.%s: %s", records[0].TypeName, field.Name, warning.Message)
				}
				canonical = append(canonical, path)
			}
			resolved[i].Values = canonical
		} else if !strings.EqualFold(filter.Op, "exists") {
			path, warning := e.linkTargetResolver.ResolveLinkTarget(ctx, field, filter.Value)
			if path == "" {
				return nil, nil, fmt.Errorf("%s.%s: %s", records[0].TypeName, field.Name, warning.Message)
			}
			resolved[i].Value = path
		}
		if loaded[fieldName] {
			continue
		}
		loaded[fieldName] = true
		grouped, err := e.loaders.structuralEdges(ctx, paths, fieldName)
		if err != nil {
			return nil, nil, err
		}
		for path, edges := range grouped {
			if targets[path] == nil {
				targets[path] = map[string][]string{}
			}
			for _, edge := range edges {
				targets[path][fieldName] = append(targets[path][fieldName], edge.DstPath)
			}
		}
	}
	return resolved, targets, nil
}
