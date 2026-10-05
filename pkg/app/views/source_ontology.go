package views

import (
	"cmp"
	"context"
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/ontology/noderead"
	"github.com/atomicobject/rhizome/pkg/ontology/pushdown"
	ontologyquery "github.com/atomicobject/rhizome/pkg/ontology/query"
	"github.com/atomicobject/rhizome/pkg/ontology/viewconfig"
	"github.com/atomicobject/rhizome/pkg/paths"
)

const (
	defaultSourceCap = 5000
	maxSourceCap     = 50000
)

type defaultSourceResolver struct {
	opts               ServiceOptions
	linkTargetResolver *pushdown.CatalogLinkResolver
}

func (r defaultSourceResolver) ResolveSource(ctx context.Context, view viewconfig.ViewDefinition, req ExecuteRequest) (SourceResult, error) {
	if catalogStore, ok := r.opts.Store.(noderead.CatalogStore); ok {
		r.linkTargetResolver = pushdown.NewCatalogLinkResolver(r.opts.Schema, catalogStore)
	}
	req, bindingWarnings := r.bindRuntimeFilterValues(ctx, req)
	scope := nodereadScopeForResolver(ctx, r)
	result, err := r.resolveSource(ctx, scope, view, req)
	result.relationScope = scope
	result.Warnings = append(bindingWarnings, result.Warnings...)
	if !result.ConstraintPlan {
		result.ConstraintPlan = true
		result.ResidualConstraints = SourceConstraints{
			Search:  req.Search,
			Filters: append([]viewconfig.FilterSpec(nil), req.Filters...),
			Sort:    append([]viewconfig.SortSpec(nil), req.Sort...),
			Page:    req.Page,
		}
	}
	return result, err
}

func (r defaultSourceResolver) resolveSource(ctx context.Context, scope *noderead.Scope, view viewconfig.ViewDefinition, req ExecuteRequest) (SourceResult, error) {
	switch view.SourceSpec.Kind {
	case viewconfig.SourceKindOntologyType, viewconfig.SourceKindOntologyInterface:
		return r.resolveOntologySource(ctx, scope, view, req)
	case viewconfig.SourceKindQueryRecipe:
		return r.resolveQueryRecipeSourceWithScope(ctx, scope, view, req)
	default:
		return SourceResult{}, fmt.Errorf("%w: unsupported source kind %q", ErrInvalidRequest, view.SourceSpec.Kind)
	}
}

func (r defaultSourceResolver) bindRuntimeFilterValues(ctx context.Context, req ExecuteRequest) (ExecuteRequest, []Warning) {
	if len(req.Filters) == 0 {
		return req, nil
	}
	out := req
	out.Filters = append([]viewconfig.FilterSpec(nil), req.Filters...)
	var warnings []Warning
	for i := range out.Filters {
		valueFrom := strings.TrimSpace(out.Filters[i].ValueFrom)
		if valueFrom == "" {
			continue
		}
		switch valueFrom {
		case "ontology.currentUser.resolvedRef":
			if r.opts.QueryDeps.OntologyRuntime == nil {
				out.Filters[i].Value = "__rhizome_current_user_unavailable__"
				warnings = append(warnings, Warning{Code: "current_user_unavailable", Message: "current-user runtime provider is unavailable"})
				continue
			}
			currentUser, err := r.opts.QueryDeps.OntologyRuntime.CurrentUser(ctx)
			if err != nil {
				out.Filters[i].Value = "__rhizome_current_user_error__"
				warnings = append(warnings, Warning{Code: "current_user_unavailable", Message: err.Error()})
				continue
			}
			if !currentUser.Configured || !currentUser.Found || currentUser.TypeName != "Person" || strings.TrimSpace(currentUser.ResolvedRef) == "" {
				out.Filters[i].Value = "__rhizome_current_user_missing__"
				warnings = append(warnings, Warning{Code: "current_user_missing", Message: "current user is not configured or does not resolve to Person"})
				continue
			}
			out.Filters[i].Value = currentUser.ResolvedRef
		default:
			out.Filters[i].Value = "__rhizome_unknown_runtime_binding__"
			warnings = append(warnings, Warning{Code: "unknown_filter_value_binding", Message: fmt.Sprintf("unknown filter value binding %q", valueFrom)})
		}
	}
	return out, warnings
}

func (r defaultSourceResolver) resolveOntologySource(ctx context.Context, scope *noderead.Scope, view viewconfig.ViewDefinition, req ExecuteRequest) (SourceResult, error) {
	if r.opts.Store == nil {
		return SourceResult{Warnings: []Warning{{Code: "view_source_unavailable", Message: "ontology index is unavailable"}}}, nil
	}
	typeName := view.SourceSpec.Type
	if view.SourceSpec.Kind == viewconfig.SourceKindOntologyInterface {
		typeName = view.SourceSpec.Interface
	}
	if result, ok, err := r.resolveIndexedEmbeddedOntologySource(ctx, scope, view, req, typeName); ok || err != nil {
		return result, err
	}
	// WHY: ontology view rows must keep the same canonical NodeRef identity as
	// the browser workspace; noderead is the shared request-scoped source for
	// type lists, hydration, and pane-opening compatibility.
	if view.SourceSpec.Kind == viewconfig.SourceKindOntologyInterface {
		return r.resolveOntologyInterfaceSource(ctx, scope, view, req)
	}
	capPolicy := sourceCapPolicy(view, req)
	limit := capPolicy.Limit
	result, err := scope.TypeInstances(ctx, noderead.TypeInstancesRequest{TypeName: typeName, Limit: limit})
	if err != nil {
		return SourceResult{}, err
	}
	rows := make([]TableRow, 0, len(result.Items))
	for _, item := range result.Items {
		rows = append(rows, rowFromNodeListItem(item))
	}
	rows, warnings := r.hydrateRowsWithScope(ctx, scope, rows)
	if len(result.Items) >= limit {
		warnings = append(warnings, Warning{
			Code:    "view_source_may_be_truncated",
			Message: fmt.Sprintf("source returned the configured cap of %d rows before table operations", limit),
		})
	}
	capabilities := r.ontologyEditCapabilities(ctx, scope, view)
	rows, err = r.projectReferencedRelationCounts(ctx, scope, view, req, rows, capabilities)
	if err != nil {
		return SourceResult{}, err
	}
	return SourceResult{
		Rows:         rows,
		Capabilities: capabilities,
		Warnings:     warnings,
		Plan: ConstraintPlanSummary{
			CandidateLimit:     limit,
			CandidateCount:     len(rows),
			CapPolicy:          capPolicy,
			SourceCompleteness: sourceCompletenessForCount(len(result.Items), limit),
		},
	}, nil
}

func (r defaultSourceResolver) resolveOntologyInterfaceSource(ctx context.Context, scope *noderead.Scope, view viewconfig.ViewDefinition, req ExecuteRequest) (SourceResult, error) {
	typeNames := r.sourceTypeNames(view)
	capPolicy := sourceCapPolicy(view, req)
	limit := capPolicy.Limit
	typePlans := make(map[string]SourceResult, len(typeNames))
	plan := SourceResult{
		ConstraintPlan: true,
		ResidualConstraints: SourceConstraints{
			Search: req.Search,
			Page:   req.Page,
		},
	}
	plan.Warnings = append(plan.Warnings, r.interfacePartialFieldWarnings(typeNames, req)...)
	for _, typeName := range typeNames {
		noteType := r.opts.Schema.Types[typeName]
		typePlan := r.planIndexedEmbeddedViewConstraints(noteType, req)
		typePlans[typeName] = typePlan
		plan.PushedConstraints.Filters = mergeFilterSpecs(plan.PushedConstraints.Filters, typePlan.PushedConstraints.Filters)
		plan.PushedConstraints.Sort = mergeSortSpecs(plan.PushedConstraints.Sort, typePlan.PushedConstraints.Sort)
		plan.ResidualConstraints.Filters = mergeFilterSpecs(plan.ResidualConstraints.Filters, typePlan.ResidualConstraints.Filters)
		if len(typePlan.ResidualConstraints.Sort) > 0 {
			plan.ResidualConstraints.Sort = append([]viewconfig.SortSpec(nil), req.Sort...)
		}
	}
	if r.canUseCollapsedInterfaceSource(typeNames, plan) {
		predicates, sortSpec, predicateWarnings := r.interfaceTypeInstancesPushdown(ctx, typeNames, typePlans)
		plan.Warnings = append(plan.Warnings, predicateWarnings...)
		result, err := scope.TypeInstances(ctx, noderead.TypeInstancesRequest{
			TypeName:   view.SourceSpec.Interface,
			Limit:      limit,
			Predicates: predicates,
			Sort:       sortSpec,
		})
		if err != nil {
			return SourceResult{}, err
		}
		rows := make([]TableRow, 0, len(result.Items))
		for _, item := range result.Items {
			rows = append(rows, rowFromNodeListItem(item))
		}
		rows, warnings := r.hydrateRowsWithScope(ctx, scope, rows)
		plan.Warnings = append(plan.Warnings, warnings...)
		if len(result.Items) >= limit {
			if hasResidualConstraints(plan.ResidualConstraints) {
				plan.Warnings = append(plan.Warnings, capBoundWarnings(plan.ResidualConstraints, limit)...)
				plan.Warnings = append(plan.Warnings, Warning{
					Code:    "view_source_residual_constraints_after_cap",
					Message: fmt.Sprintf("ontology interface source returned the configured cap of %d rows while residual filters or sort remain", limit),
				})
			} else {
				plan.Warnings = append(plan.Warnings, Warning{
					Code:    "view_source_may_be_truncated",
					Message: fmt.Sprintf("source returned the configured cap of %d rows before table operations", limit),
				})
			}
		}
		plan.Capabilities = r.ontologyEditCapabilities(ctx, scope, view)
		rows, err = r.projectReferencedRelationCounts(ctx, scope, view, req, rows, plan.Capabilities)
		if err != nil {
			return SourceResult{}, err
		}
		plan.Rows = rows
		plan.Plan.CandidateLimit = limit
		plan.Plan.CandidateCount = len(result.Items)
		plan.Plan.CapPolicy = capPolicy
		plan.Plan.SourceCompleteness = sourceCompletenessForCountAndWarnings(len(result.Items), limit, plan.Warnings)
		return plan, nil
	}
	seen := map[string]struct{}{}
	rows := make([]TableRow, 0)
	for _, typeName := range typeNames {
		noteType := r.opts.Schema.Types[typeName]
		typePlan := typePlans[typeName]
		predicates, sortSpec, predicateWarnings := r.typeInstancesPushdown(ctx, noteType, typePlan)
		plan.Warnings = append(plan.Warnings, predicateWarnings...)
		result, err := scope.TypeInstances(ctx, noderead.TypeInstancesRequest{
			TypeName:   typeName,
			Limit:      limit,
			Predicates: predicates,
			Sort:       sortSpec,
		})
		if err != nil {
			return SourceResult{}, err
		}
		for _, item := range result.Items {
			key := nodeRefKey(item.Ref)
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			rows = append(rows, rowFromNodeListItem(item))
		}
		if len(result.Items) >= limit && hasResidualConstraints(typePlan.ResidualConstraints) {
			plan.Warnings = append(plan.Warnings, capBoundWarnings(typePlan.ResidualConstraints, limit)...)
			plan.Warnings = append(plan.Warnings, Warning{
				Code:    "view_source_residual_constraints_after_cap",
				Message: fmt.Sprintf("ontology interface implementor %s returned the configured cap of %d rows while residual filters or sort remain", typeName, limit),
			})
		}
	}
	rows, warnings := r.hydrateRowsWithScope(ctx, scope, rows)
	plan.Warnings = append(plan.Warnings, warnings...)
	plan.Capabilities = r.ontologyEditCapabilities(ctx, scope, view)
	if len(plan.PushedConstraints.Sort) > 0 {
		sortRows(rows, plan.PushedConstraints.Sort, plan.Capabilities)
	}
	if len(rows) >= limit {
		if hasResidualConstraints(plan.ResidualConstraints) {
			plan.Warnings = append(plan.Warnings, capBoundWarnings(plan.ResidualConstraints, limit)...)
			plan.Warnings = append(plan.Warnings, Warning{
				Code:    "view_source_residual_constraints_after_cap",
				Message: fmt.Sprintf("ontology interface source returned the configured cap of %d rows while residual filters or sort remain", limit),
			})
		} else {
			plan.Warnings = append(plan.Warnings, Warning{
				Code:    "view_source_may_be_truncated",
				Message: fmt.Sprintf("source returned the configured cap of %d rows before table operations", limit),
			})
		}
		rows = rows[:limit]
	}
	countedRows, err := r.projectReferencedRelationCounts(ctx, scope, view, req, rows, plan.Capabilities)
	if err != nil {
		return SourceResult{}, err
	}
	rows = countedRows
	plan.Rows = countedRows
	plan.Plan.CandidateLimit = limit
	plan.Plan.CandidateCount = len(rows)
	plan.Plan.CapPolicy = capPolicy
	plan.Plan.SourceCompleteness = sourceCompletenessForCountAndWarnings(len(rows), limit, plan.Warnings)
	return plan, nil
}

func (r defaultSourceResolver) canUseCollapsedInterfaceSource(typeNames []string, plan SourceResult) bool {
	return len(typeNames) > 0 && len(plan.ResidualConstraints.Filters) == 0 && len(plan.ResidualConstraints.Sort) == 0
}

func (r defaultSourceResolver) resolveIndexedEmbeddedOntologySource(ctx context.Context, scope *noderead.Scope, view viewconfig.ViewDefinition, req ExecuteRequest, typeName string) (SourceResult, bool, error) {
	if view.SourceSpec.Kind != viewconfig.SourceKindOntologyType || r.opts.Schema == nil || r.opts.QueryDeps.Store == nil {
		return SourceResult{}, false, nil
	}
	noteType := r.opts.Schema.Types[typeName]
	if noteType == nil || (noteType.Role != ontology.TypeRoleEmbeddedNode && noteType.Role != ontology.TypeRoleNote) {
		return SourceResult{}, false, nil
	}
	capPolicy := sourceCapPolicy(view, req)
	limit := capPolicy.Limit
	plan := r.planIndexedEmbeddedViewConstraints(noteType, req)
	predicates, sortSpec, predicateWarnings := r.typeInstancesPushdown(ctx, noteType, plan)
	plan.Warnings = append(plan.Warnings, predicateWarnings...)
	// Diagnostic probe: surface plan warnings (residual_constraints,
	// index_unavailable, link_filter_*) at the same time the rows are
	// produced. Cheap because the queryPlan root is cached and the probe runs
	// against the same indexed plan inputs.
	if r.opts.ExecSchema != nil {
		filterVariables := r.viewFilterVariablesForType(noteType, plan.PushedConstraints.Filters)
		sortVariables := r.viewSortVariablesForType(noteType, plan.PushedConstraints.Sort)
		plan.Warnings = append(plan.Warnings, r.indexedPlanWarnings(ctx, typeName, limit, filterVariables, sortVariables)...)
	}
	result, err := scope.TypeInstances(ctx, noderead.TypeInstancesRequest{
		TypeName:   typeName,
		Limit:      limit,
		Predicates: predicates,
		Sort:       sortSpec,
	})
	if err != nil {
		return SourceResult{}, true, fmt.Errorf("%w: indexed ontology source query failed: %s", ErrInvalidRequest, err.Error())
	}
	rows := make([]TableRow, 0, len(result.Items))
	for _, item := range result.Items {
		rows = append(rows, rowFromNodeListItem(item))
	}
	rows, hydrateWarnings := r.hydrateRowsWithScope(ctx, scope, rows)
	warnings := append([]Warning(nil), hydrateWarnings...)
	if len(rows) >= limit {
		if hasResidualConstraints(plan.ResidualConstraints) {
			warnings = append(warnings, capBoundWarnings(plan.ResidualConstraints, limit)...)
		}
		warnings = append(warnings, Warning{
			Code:    "view_source_may_be_truncated",
			Message: fmt.Sprintf("source returned the configured cap of %d rows before residual table operations", limit),
		})
	}
	// WHY: a scope is required for editCandidatesForType to enumerate link
	// targets. Without it, relation field capabilities ship with no
	// candidates and the UI falls back to a read-only cell instead of a
	// picker dropdown.
	capabilities := r.ontologyEditCapabilities(ctx, scope, view)
	rows, err = r.projectReferencedRelationCounts(ctx, scope, view, req, rows, capabilities)
	if err != nil {
		return SourceResult{}, true, err
	}
	plan.Rows = rows
	plan.Capabilities = capabilities
	plan.Warnings = append(plan.Warnings, warnings...)
	plan.Plan.CandidateLimit = limit
	plan.Plan.CandidateCount = len(rows)
	plan.Plan.CapPolicy = capPolicy
	plan.Plan.SourceCompleteness = sourceCompletenessForCountAndWarnings(len(rows), limit, plan.Warnings)
	return plan, true, nil
}

// viewPlannerFor builds the canonical pushdown planner for a configured
// view's NoteType. Views resolve frontmatter./inline. prefixed and
// case-insensitive keys and use the same indexed link target resolver for
// both predicate construction and execution-time diagnostics.
func (r defaultSourceResolver) viewPlannerFor(noteType *ontology.NoteType) pushdown.Planner {
	return pushdown.Planner{
		Schema:       r.opts.Schema,
		Resolver:     pushdown.ViewResolver{NoteType: noteType},
		LinkResolver: viewLinkResolver{schema: r.opts.Schema, store: r.opts.Store, resolver: r.linkTargetResolver},
	}
}

type viewLinkResolver struct {
	schema   *ontology.Schema
	store    noderead.Store
	resolver *pushdown.CatalogLinkResolver
}

func (r viewLinkResolver) ResolveLinkTarget(ctx context.Context, field *ontology.Field, raw string) (string, pushdown.Warning) {
	resolver := r.resolver
	if resolver == nil {
		catalogStore, ok := r.store.(noderead.CatalogStore)
		if !ok {
			return "", pushdown.Warning{Code: "link_filter_resolution_failed", Message: "link filter target resolver is unavailable"}
		}
		resolver = pushdown.NewCatalogLinkResolver(r.schema, catalogStore)
	}
	return resolver.ResolveLinkTarget(ctx, field, raw)
}

func filterToPushdownInput(filter viewconfig.FilterSpec) pushdown.FilterInput {
	return pushdown.FilterInput{
		Field:  filter.Field,
		Op:     filter.Op,
		Value:  filter.Value,
		Values: filter.Values,
	}
}

func sortToPushdownInput(sortItem viewconfig.SortSpec) pushdown.SortInput {
	return pushdown.SortInput{
		Field:     sortItem.Field,
		Direction: sortItem.Direction,
	}
}

func (r defaultSourceResolver) planIndexedEmbeddedViewConstraints(noteType *ontology.NoteType, req ExecuteRequest) SourceResult {
	result := SourceResult{
		ConstraintPlan: true,
		PushedConstraints: SourceConstraints{
			Filters: make([]viewconfig.FilterSpec, 0, len(req.Filters)),
		},
		ResidualConstraints: SourceConstraints{
			Search: req.Search,
			Page:   req.Page,
		},
	}
	planner := r.viewPlannerFor(noteType)
	for _, filter := range req.Filters {
		if r.viewFilterCanPush(noteType, filter) {
			result.PushedConstraints.Filters = append(result.PushedConstraints.Filters, filter)
		} else {
			result.ResidualConstraints.Filters = append(result.ResidualConstraints.Filters, filter)
		}
	}
	sortInputs := make([]pushdown.SortInput, len(req.Sort))
	for i, sortItem := range req.Sort {
		sortInputs[i] = sortToPushdownInput(sortItem)
	}
	partition := planner.PlanSort(sortInputs)
	pushedSort := make([]viewconfig.SortSpec, 0, len(partition.Pushed))
	for _, item := range partition.Pushed {
		pushedSort = append(pushedSort, viewconfig.SortSpec{Field: item.Field, Direction: item.Direction})
	}
	result.PushedConstraints.Sort = pushedSort
	if len(partition.Residual) > 0 {
		// IMPORTANT: table execution applies residual sort globally. If the indexed
		// source pushed a prefix of the requested sort, applying only the suffix would
		// make that suffix the primary order. Reapply the full requested sort over
		// the bounded candidate set so the pushed prefix remains semantically primary.
		result.ResidualConstraints.Sort = append([]viewconfig.SortSpec(nil), req.Sort...)
	}
	return result
}

func (r defaultSourceResolver) viewFilterCanPush(noteType *ontology.NoteType, filter viewconfig.FilterSpec) bool {
	if noteType == nil {
		return false
	}
	return r.viewPlannerFor(noteType).CanPushFilter(filterToPushdownInput(filter))
}

func (r defaultSourceResolver) interfacePartialFieldWarnings(typeNames []string, req ExecuteRequest) []Warning {
	if len(typeNames) == 0 {
		return nil
	}
	var warnings []Warning
	for _, filter := range req.Filters {
		field := strings.TrimSpace(filter.Field)
		if field == "" || pushdown.IsBuiltinFieldKey(field) {
			continue
		}
		pushable := 0
		for _, typeName := range typeNames {
			if r.viewFilterCanPush(r.opts.Schema.Types[typeName], filter) {
				pushable++
			}
		}
		if pushable > 0 && pushable < len(typeNames) {
			warnings = append(warnings, Warning{
				Code:    "interface_partial_field_residual",
				Message: fmt.Sprintf("interface field %q is indexed for %d of %d implementors; residual filtering will run after source enumeration for the remaining implementors", field, pushable, len(typeNames)),
				Path:    field,
			})
		}
	}
	for _, sortItem := range req.Sort {
		field := strings.TrimSpace(sortItem.Field)
		if field == "" || pushdown.IsBuiltinFieldKey(field) {
			continue
		}
		pushable := 0
		for _, typeName := range typeNames {
			if r.viewSortKeyCanPush(r.opts.Schema.Types[typeName], sortItem) {
				pushable++
			}
		}
		if pushable > 0 && pushable < len(typeNames) {
			warnings = append(warnings, Warning{
				Code:    "interface_partial_field_residual",
				Message: fmt.Sprintf("interface sort field %q is indexed for %d of %d implementors; residual sorting will run after source enumeration for the remaining implementors", field, pushable, len(typeNames)),
				Path:    field,
			})
		}
	}
	return warnings
}

func (r defaultSourceResolver) viewSortKeyCanPush(noteType *ontology.NoteType, sortItem viewconfig.SortSpec) bool {
	if noteType == nil {
		return pushdown.IsBuiltinFieldKey(sortItem.Field)
	}
	return r.viewPlannerFor(noteType).CanPushSort(sortToPushdownInput(sortItem))
}

func (r defaultSourceResolver) viewFilterVariablesForType(noteType *ontology.NoteType, filters []viewconfig.FilterSpec) []any {
	planner := r.viewPlannerFor(noteType)
	out := make([]any, 0, len(filters))
	for _, filter := range filters {
		item := map[string]any{
			"field": planner.CanonicalKey(filter.Field),
			"op":    filter.Op,
		}
		if len(filter.Values) > 0 {
			item["values"] = append([]string(nil), filter.Values...)
		} else {
			item["value"] = filter.Value
		}
		out = append(out, item)
	}
	return out
}

func (r defaultSourceResolver) viewSortVariablesForType(noteType *ontology.NoteType, sortSpec []viewconfig.SortSpec) []any {
	planner := r.viewPlannerFor(noteType)
	out := make([]any, 0, len(sortSpec))
	for _, sortItem := range sortSpec {
		out = append(out, map[string]any{
			"field":     planner.CanonicalKey(sortItem.Field),
			"direction": sortItem.Direction,
		})
	}
	return out
}

func (r defaultSourceResolver) indexedPlanWarnings(ctx context.Context, typeName string, first int, filters []any, sortSpec []any) []Warning {
	if r.opts.Schema == nil || r.opts.ExecSchema == nil {
		return nil
	}
	query := `query ViewOntologySourcePlan($type: String!, $first: Int!, $filters: [FieldFilterInput!], $sort: [SortInput!]) {
  ontology {
    queryPlan(type: $type, first: $first, filters: $filters, sort: $sort) {
      warnings { code message path }
    }
  }
}`
	prepared, errs := ontologyquery.PrepareWithVariables(r.opts.ExecSchema, query, map[string]any{
		"type":    typeName,
		"first":   first,
		"filters": filters,
		"sort":    sortSpec,
	})
	if len(errs) > 0 {
		return []Warning{{Code: "view_source_plan_unavailable", Message: queryErrorSummary(errs)}}
	}
	result := ontologyquery.ExecutePrepared(ctx, r.opts.QueryDeps, r.opts.Schema, r.opts.ExecSchema, prepared)
	if len(result.Errors) > 0 {
		return []Warning{{Code: "view_source_plan_unavailable", Message: queryErrorSummary(result.Errors)}}
	}
	ontologyRoot, _ := result.Data["ontology"].(map[string]any)
	queryPlan, _ := ontologyRoot["queryPlan"].(map[string]any)
	rawWarnings, _ := queryPlan["warnings"].([]any)
	out := make([]Warning, 0, len(rawWarnings))
	for _, raw := range rawWarnings {
		item, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		out = append(out, Warning{
			Code:    firstString(item["code"]),
			Message: firstString(item["message"]),
			Path:    firstString(item["path"]),
		})
	}
	return out
}

func (r defaultSourceResolver) typeInstancesPushdown(ctx context.Context, noteType *ontology.NoteType, plan SourceResult) ([]codeanchor.OntologyFieldPredicate, []codeanchor.OntologyFieldSort, []Warning) {
	planner := r.viewPlannerFor(noteType)
	predicates := make([]codeanchor.OntologyFieldPredicate, 0, len(plan.PushedConstraints.Filters))
	var warnings []Warning
	for _, filter := range plan.PushedConstraints.Filters {
		result, ok := planner.BuildPredicate(ctx, filterToPushdownInput(filter))
		for _, warning := range result.Warnings {
			warnings = append(warnings, Warning{Code: warning.Code, Message: warning.Message, Path: warning.Path})
		}
		if ok {
			predicates = append(predicates, viewPathPredicateWithMarkdownCompatibility(filter, result))
		}
	}
	sortSpec := make([]codeanchor.OntologyFieldSort, 0, len(plan.PushedConstraints.Sort))
	for _, item := range plan.PushedConstraints.Sort {
		if sortItem, ok := planner.BuildSort(sortToPushdownInput(item)); ok {
			sortSpec = append(sortSpec, sortItem)
		}
	}
	return predicates, sortSpec, warnings
}

// viewPathPredicateWithMarkdownCompatibility preserves the legacy view-input
// contract for extensionless Markdown paths. The generic pushdown planner must
// preserve authored paths, because it cannot know which provider owns a path.
// Views currently query Markdown-derived ontology roots, so this explicit
// compatibility boundary may also match the historical .md form. Explicit
// extensions always remain unchanged.
func viewPathPredicateWithMarkdownCompatibility(filter viewconfig.FilterSpec, result pushdown.PredicateResult) codeanchor.OntologyFieldPredicate {
	predicate := result.Predicate
	if !result.Key.IsBuiltin() || predicate.Op == codeanchor.OntologyFieldOpExists {
		return predicate
	}

	values := filter.Values
	if predicate.Op != codeanchor.OntologyFieldOpIn {
		values = []string{filter.Value}
	}
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || filepath.Ext(value) != "" {
			continue
		}
		markdownPath := paths.NormalizeNote(value).String()
		predicate.Values = append(predicate.Values, codeanchor.IntelOntologyNodeFieldValue{
			ValueText: markdownPath,
			ValueNorm: markdownPath,
		})
	}
	if len(predicate.Values) > 1 {
		predicate.Op = codeanchor.OntologyFieldOpIn
	}
	return predicate
}

func (r defaultSourceResolver) interfaceTypeInstancesPushdown(ctx context.Context, typeNames []string, typePlans map[string]SourceResult) ([]codeanchor.OntologyFieldPredicate, []codeanchor.OntologyFieldSort, []Warning) {
	for _, typeName := range typeNames {
		noteType := r.opts.Schema.Types[typeName]
		typePlan := typePlans[typeName]
		return r.typeInstancesPushdown(ctx, noteType, typePlan)
	}
	return nil, nil, nil
}

func rowFromNodeListItem(item ontology.NodeListItem) TableRow {
	return TableRow{
		Ref:          item.Ref,
		Path:         item.NotePath,
		Title:        item.Title,
		ResolvedType: item.ResolvedType,
		UpdatedAt:    item.UpdatedAt,
		HasIssues:    item.HasIssues,
		Fields: map[string]any{
			"title":        item.Title,
			"path":         item.NotePath,
			"resolvedType": item.ResolvedType,
			"updatedAt":    item.UpdatedAt,
			"hasIssues":    item.HasIssues,
		},
	}
}

func (r defaultSourceResolver) enrichRowFromRecord(row TableRow, record noderead.NodeRecord) TableRow {
	row.Ref = record.Ref
	row.Path = firstNonEmpty(record.Path, row.Path)
	row.Title = firstNonEmpty(record.Title, row.Title)
	row.ResolvedType = firstNonEmpty(record.TypeName, row.ResolvedType)
	if record.UpdatedAt != 0 {
		row.UpdatedAt = record.UpdatedAt
	}
	row.HasIssues = record.HasIssues
	row.Tags = append([]string(nil), record.Tags...)
	if row.Fields == nil {
		row.Fields = map[string]any{}
	}
	row.Fields["frontmatter"] = cloneMap(record.Frontmatter)
	row.Fields["inline"] = cloneInline(record.InlineProps)
	if r.opts.Schema != nil {
		if noteType := r.opts.Schema.Types[row.ResolvedType]; noteType != nil {
			addSemanticFields(row.Fields, noteType, record)
		}
	}
	return row
}

func addSemanticFields(fields map[string]any, noteType *ontology.NoteType, record noderead.NodeRecord) {
	for _, field := range noteType.Fields {
		if field == nil || field.Name == "" {
			continue
		}
		if ontology.IsSectionSummary(field) {
			if values, ok := lookupInlineProp(record.InlineProps, field.Name); ok && len(values) > 0 {
				fields[field.Name] = strings.Join(values, " ")
			}
			continue
		}
		switch field.SourceKind {
		case ontology.FieldSourceFrontmatter:
			source := firstNonEmpty(field.Source, field.Name)
			if value, ok := record.Frontmatter[source]; ok {
				fields[field.Name] = value
			}
		case ontology.FieldSourceInline:
			source := firstNonEmpty(field.Source, field.Name)
			if values, ok := lookupInlineProp(record.InlineProps, source); ok {
				fields[field.Name] = append([]string(nil), values...)
			}
		case ontology.FieldSourceCheckbox:
			if values, ok := lookupInlineProp(record.InlineProps, field.Name); ok && len(values) > 0 {
				fields[field.Name] = values[0]
			}
		}
	}
}

// lookupInlineProp does a case-insensitive lookup against record.InlineProps.
// Catalog-hydrated records store field_name lowercased (so query-side filters
// can match without case games), while projection-hydrated records preserve
// the schema's original casing. Schema field names are commonly camelCase
// (assignedTo, displayName), so a case-sensitive lookup against the catalog
// path would silently miss.
func lookupInlineProp(props map[string][]string, key string) ([]string, bool) {
	if values, ok := props[key]; ok {
		return values, true
	}
	lower := strings.ToLower(strings.TrimSpace(key))
	if lower != key {
		if values, ok := props[lower]; ok {
			return values, true
		}
	}
	return nil, false
}

func nodereadScopeForResolver(ctx context.Context, r defaultSourceResolver) *noderead.Scope {
	return noderead.NewService(r.opts.VaultDef, r.opts.NoteReader, r.opts.Store, r.opts.Schema).NewScope(ctx, noderead.ScopeOptions{ReadOverlay: r.opts.ReadOverlay})
}

func (r defaultSourceResolver) hydrateRowsWithScope(ctx context.Context, scope *noderead.Scope, rows []TableRow) ([]TableRow, []Warning) {
	if r.opts.Store == nil || len(rows) == 0 {
		return rows, nil
	}
	refs := make([]ontology.NodeRef, 0, len(rows))
	for _, row := range rows {
		if strings.TrimSpace(row.Ref.NotePath) == "" {
			continue
		}
		refs = append(refs, row.Ref)
	}
	if len(refs) == 0 {
		return rows, nil
	}
	// HydrateSummary serves view rows directly from the indexed catalog
	// (ontology_nodes + ontology_node_field_values). HydrateContent here
	// would force a per-host projection fallback that re-parses every note,
	// turning a ~50ms catalog read into a ~600ms snapshot scan for views
	// like action-items that span many notes.
	records, err := scope.Hydrate(ctx, refs, noderead.HydrateOptions{Profile: noderead.HydrateSummary})
	if err != nil {
		return rows, []Warning{{Code: "view_source_hydration_failed", Message: err.Error()}}
	}
	recordByKey := map[string]noderead.NodeRecord{}
	relationValuesByKey := map[string]map[string][]TableRelationValue{}
	for _, record := range records {
		recordByKey[nodeRefKey(record.Ref)] = record
		if values := relationValuesForRecord(r.opts.Schema, record); len(values) > 0 {
			relationValuesByKey[noderead.RefIdentityKey(record.Ref)] = values
		}
	}
	overlayRelationValues, relationErr := resolveRawRelationValues(ctx, scope, r.opts.Schema, records)
	warnings := make([]Warning, 0, 1)
	if relationErr != nil {
		warnings = append(warnings, Warning{Code: "view_relation_resolution_failed", Message: relationErr.Error()})
	}
	for key, values := range overlayRelationValues {
		relationValuesByKey[key] = values
	}
	out := make([]TableRow, 0, len(rows))
	for _, row := range rows {
		if record, ok := recordByKey[nodeRefKey(row.Ref)]; ok {
			row = r.enrichRowFromRecord(row, record)
			row.RelationValues = relationValuesByKey[noderead.RefIdentityKey(record.Ref)]
		}
		out = append(out, row)
	}
	return out, warnings
}

// ontologyEditCapabilities assigns roles from the view source's profile, so
// an interface view reads the interface's lifecycle and summary fields.
func (r defaultSourceResolver) ontologyEditCapabilities(ctx context.Context, scope *noderead.Scope, view viewconfig.ViewDefinition) []FieldCapability {
	var profile *ontology.TypeProfile
	if derived, ok := sourceProfile(r.opts.Schema, view); ok {
		profile = &derived
	}
	return r.capabilitiesForTypes(ctx, scope, r.sourceTypeNames(view), profile)
}

// ontologyEditCapabilitiesForTypes assigns roles from each type's own profile.
func (r defaultSourceResolver) ontologyEditCapabilitiesForTypes(ctx context.Context, scope *noderead.Scope, typeNames []string) []FieldCapability {
	return r.capabilitiesForTypes(ctx, scope, typeNames, nil)
}

func (r defaultSourceResolver) capabilitiesForTypes(ctx context.Context, scope *noderead.Scope, typeNames []string, sourceProfile *ontology.TypeProfile) []FieldCapability {
	if r.opts.Schema == nil {
		return nil
	}
	if len(typeNames) == 0 {
		return nil
	}
	byKey := map[string]FieldCapability{}
	for _, typeName := range typeNames {
		noteType := r.opts.Schema.Types[typeName]
		if noteType == nil {
			continue
		}
		profile := sourceProfile
		if profile == nil {
			if derived, ok := subjectProfile(r.opts.Schema, typeName); ok {
				profile = &derived
			}
		}
		for fieldIndex, field := range noteType.Fields {
			if field == nil || field.Name == "" {
				continue
			}
			if cap, ok := relationCountCapability(field); ok {
				cap.schemaOrder = fieldIndex
				cap.schemaOrderKnown = true
				byKey[cap.Key] = mergeOntologyEditCapability(byKey[cap.Key], cap)
				continue
			}
			for _, cap := range r.capabilitiesForField(ctx, scope, field) {
				if cap.Key == "" {
					continue
				}
				cap.SemanticRole = schemaFieldRole(profile, field)
				cap.schemaOrder = fieldIndex
				cap.schemaOrderKnown = true
				byKey[cap.Key] = mergeOntologyEditCapability(byKey[cap.Key], cap)
			}
		}
		if cap := titleEditCapabilityForType(noteType); cap.Key != "" {
			byKey[cap.Key] = mergeOntologyEditCapability(byKey[cap.Key], cap)
		}
	}
	keys := make([]string, 0, len(byKey))
	for key := range byKey {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	out := make([]FieldCapability, 0, len(keys))
	for _, key := range keys {
		out = append(out, byKey[key])
	}
	return out
}

func titleEditCapabilityForType(noteType *ontology.NoteType) FieldCapability {
	if noteType == nil {
		return FieldCapability{}
	}
	return FieldCapability{
		Key:              "title",
		Label:            "Title",
		ValueKind:        "string",
		CanonicalField:   "title",
		SourceKeys:       []string{"title"},
		SourceKind:       "builtin",
		SemanticRole:     "title",
		Importance:       ontology.FieldImportanceNormal,
		Sortable:         true,
		ResidualSortable: true,
		Groupable:        false,
		FilterOps:        []string{"eq", "neq", "contains", "in", "exists"},
		ResidualFilterOps: []string{
			"eq", "neq", "contains", "in", "exists",
		},
		Source: "builtin",
		Filter: &FilterCapability{Ops: []string{
			"eq", "neq", "contains", "in", "exists",
		}},
		Edit: &EditCapability{
			Kind:      "text",
			Operation: "setField",
			Field:     "title",
			InputMode: "doubleClick",
			ValueKind: "string",
		},
		cardinalityKnown: true,
	}
}

func mergeOntologyEditCapability(existing FieldCapability, next FieldCapability) FieldCapability {
	if existing.Key == "" {
		return next
	}
	merged := existing
	merged.Values = mergeAnyValues(existing.Values, next.Values)
	merged.FilterOps = mergeStringValues(existing.FilterOps, next.FilterOps)
	merged.IndexedFilterOps = mergeStringValues(existing.IndexedFilterOps, next.IndexedFilterOps)
	merged.ResidualFilterOps = mergeStringValues(existing.ResidualFilterOps, next.ResidualFilterOps)
	merged.Sortable = existing.Sortable && next.Sortable
	merged.Groupable = existing.Groupable && next.Groupable
	merged.Required = existing.Required && next.Required
	merged.Importance = mergeFieldImportance(existing.Importance, next.Importance)
	merged.PolicyReason = cmp.Or(existing.PolicyReason, next.PolicyReason)
	merged.List = existing.List || next.List
	merged.cardinalityKnown = existing.cardinalityKnown || next.cardinalityKnown
	if next.schemaOrderKnown && (!existing.schemaOrderKnown || next.schemaOrder < existing.schemaOrder) {
		merged.schemaOrder = next.schemaOrder
	}
	merged.schemaOrderKnown = existing.schemaOrderKnown || next.schemaOrderKnown
	if !editCapabilitiesCompatible(existing.Edit, next.Edit) {
		merged.Edit = nil
		return merged
	}
	if merged.Edit == nil {
		merged.Edit = next.Edit
	}
	return merged
}

func mergeFieldImportance(left, right ontology.FieldDisplayImportance) ontology.FieldDisplayImportance {
	left = effectiveFieldImportance(left)
	right = effectiveFieldImportance(right)
	if left == right {
		return left
	}
	return ontology.FieldImportanceNormal
}

func editCapabilitiesCompatible(a *EditCapability, b *EditCapability) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	if a.Kind != b.Kind || a.Operation != b.Operation || a.Field != b.Field || a.List != b.List || a.TargetType != b.TargetType {
		return false
	}
	if !sameStringSet(a.Options, b.Options) {
		return false
	}
	if len(a.Candidates) == 0 || len(b.Candidates) == 0 {
		return len(a.Candidates) == len(b.Candidates)
	}
	return sameStringSet(editCandidateValues(a.Candidates), editCandidateValues(b.Candidates))
}

func mergeStringValues(a []string, b []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(a)+len(b))
	for _, value := range append(append([]string(nil), a...), b...) {
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}

func mergeAnyValues(a []any, b []any) []any {
	seen := map[string]struct{}{}
	out := make([]any, 0, len(a)+len(b))
	for _, value := range append(append([]any(nil), a...), b...) {
		key := fmt.Sprint(value)
		if key == "" {
			continue
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, value)
	}
	return out
}

func sameStringSet(a []string, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	seen := map[string]int{}
	for _, value := range a {
		seen[value]++
	}
	for _, value := range b {
		seen[value]--
		if seen[value] < 0 {
			return false
		}
	}
	return true
}

func editCandidateValues(candidates []EditCandidate) []string {
	out := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		out = append(out, candidate.Value)
	}
	return out
}

func (r defaultSourceResolver) sourceTypeNames(view viewconfig.ViewDefinition) []string {
	if r.opts.Schema == nil {
		return nil
	}
	switch view.SourceSpec.Kind {
	case viewconfig.SourceKindOntologyType:
		if r.opts.Schema.Types[view.SourceSpec.Type] != nil {
			return []string{view.SourceSpec.Type}
		}
	case viewconfig.SourceKindOntologyInterface:
		out := make([]string, 0)
		for name, noteType := range r.opts.Schema.Types {
			if noteType == nil {
				continue
			}
			for _, iface := range noteType.Implements {
				if iface == view.SourceSpec.Interface {
					out = append(out, name)
					break
				}
			}
		}
		sort.Strings(out)
		return out
	}
	return nil
}

func (r defaultSourceResolver) capabilitiesForField(ctx context.Context, scope *noderead.Scope, field *ontology.Field) []FieldCapability {
	if ontology.IsSectionSummary(field) {
		return []FieldCapability{{Key: field.Name, Label: labelForField(field.Name), CanonicalField: field.Name, ValueKind: "string", SemanticRole: "summary", Importance: field.Display.EffectiveImportance(), cardinalityKnown: true}}
	}
	edit := r.editCapabilityForField(ctx, scope, field)
	plan, ok := ontology.FieldQueryCapabilityForField(r.opts.Schema, field)
	if !ok {
		return nil
	}
	keys := semanticCapabilityKeys(field)
	out := make([]FieldCapability, 0, len(keys))
	enumValues := enumValuesForCapability(r.opts.Schema, field.TypeName)
	// missing (no value) is always residual; SPEC-0112 header gap facets use it.
	filterOps := append(append([]string(nil), plan.FilterOps...), "missing")
	residualOps := append(append([]string(nil), plan.ResidualFilterOps...), "missing")
	policyReason := ""
	if field.Policy != nil {
		policyReason = strings.TrimSpace(field.Policy.Reason)
	}
	for _, key := range keys {
		sourceKind := fieldCapabilitySource(key)
		cap := FieldCapability{
			Key:               key,
			Label:             labelForField(key),
			ValueKind:         plan.ValueKind,
			CanonicalField:    field.Name,
			SourceKeys:        append([]string(nil), keys...),
			SourceKind:        sourceKind,
			SemanticRole:      schemaFieldRole(nil, field),
			Required:          field.Required,
			Importance:        field.Display.EffectiveImportance(),
			Sortable:          plan.Sortable,
			IndexedSortable:   plan.Sortable,
			ResidualSortable:  plan.Sortable,
			Groupable:         plan.Groupable,
			FilterOps:         append([]string(nil), filterOps...),
			IndexedFilterOps:  append([]string(nil), plan.IndexedFilterOps...),
			ResidualFilterOps: append([]string(nil), residualOps...),
			Source:            sourceKind,
			Filter:            &FilterCapability{Ops: append([]string(nil), filterOps...)},
			Edit:              edit,
			PolicyReason:      policyReason,
			List:              field.List,
			cardinalityKnown:  true,
		}
		if len(enumValues) > 0 {
			cap.EnumName = field.TypeName
			cap.EnumValues = append([]FieldEnumValue(nil), enumValues...)
			cap.Filter.Options = enumValuesAsOptions(enumValues)
			cap.Group = &GroupCapability{Values: append([]FieldEnumValue(nil), enumValues...)}
		}
		out = append(out, cap)
	}
	return out
}

func enumValuesAsOptions(values []FieldEnumValue) []any {
	out := make([]any, 0, len(values))
	for _, value := range values {
		out = append(out, value.Value)
	}
	return out
}

func enumValuesForCapability(schema *ontology.Schema, typeName string) []FieldEnumValue {
	if schema == nil || schema.EnumTypes == nil {
		return nil
	}
	enumType := schema.EnumTypes[typeName]
	if enumType == nil {
		return nil
	}
	out := make([]FieldEnumValue, 0, len(enumType.Values))
	tones := enumType.Tones()
	collapsed := enumType.CollapsedDefaults()
	stages := enumType.Stages()
	for index, value := range enumType.Values {
		if value == nil || value.Name == "" {
			continue
		}
		item := FieldEnumValue{
			Value:              value.Name,
			Label:              firstNonEmpty(value.View.Label, value.Name),
			Description:        value.Description,
			Rank:               value.View.Order,
			Tone:               tones[index],
			CollapsedByDefault: index < len(collapsed) && collapsed[index],
		}
		if index < len(stages) {
			item.Stage = stages[index].Stage
			item.StageDeclared = stages[index].Declared
		}
		out = append(out, item)
	}
	return out
}

func (r defaultSourceResolver) editCapabilityForField(ctx context.Context, scope *noderead.Scope, field *ontology.Field) *EditCapability {
	switch {
	case field.Kind == ontology.FieldKindEnum:
		return &EditCapability{
			Kind:      "enum",
			Operation: "setField",
			Field:     field.Name,
			List:      field.List,
			InputMode: "direct",
			ValueKind: "enum",
			Options:   enumOptions(r.opts.Schema, field.TypeName),
		}
	case field.SourceKind == ontology.FieldSourceCheckbox:
		return &EditCapability{
			Kind:      "boolean",
			Operation: "setField",
			Field:     field.Name,
			InputMode: "direct",
			ValueKind: "bool",
			Options:   []string{"false", "true"},
		}
	case safeAuthoredScalarEditField(field):
		return &EditCapability{
			Kind:      scalarEditKind(field.TypeName),
			Operation: "setField",
			Field:     field.Name,
			List:      field.List,
			InputMode: "doubleClick",
			ValueKind: scalarEditValueKind(field.TypeName),
		}
	case field.Kind == ontology.FieldKindLink && field.TypeName != "" && field.TypeName != "Note":
		return &EditCapability{
			Kind:       "node",
			Operation:  "setLinkField",
			Field:      field.Name,
			List:       field.List,
			InputMode:  "direct",
			ValueKind:  "relation",
			TargetType: field.TypeName,
			Candidates: r.editCandidatesForType(ctx, scope, field.TypeName),
		}
	default:
		return nil
	}
}

func scalarEditKind(typeName string) string {
	switch scalarEditValueKind(typeName) {
	case "date":
		return "date"
	case "datetime":
		return "datetime"
	case "int", "real":
		return "number"
	default:
		return "text"
	}
}

func scalarEditValueKind(typeName string) string {
	switch strings.ToLower(strings.TrimSpace(typeName)) {
	case "date":
		return "date"
	case "datetime":
		return "datetime"
	case "int", "integer":
		return "int"
	case "float", "real", "number":
		return "real"
	case "url":
		return "url"
	case "id":
		return "id"
	default:
		return "string"
	}
}

func safeAuthoredScalarEditField(field *ontology.Field) bool {
	if field == nil || field.Kind != ontology.FieldKindScalar || field.List {
		return false
	}
	if field.SourceKind != ontology.FieldSourceFrontmatter && field.SourceKind != ontology.FieldSourceInline {
		return false
	}
	if field.IsIdentifier || field.IsPreferredIdentifier || field.IsDerivableIdentifier {
		return false
	}
	if field.Policy != nil && field.Policy.RequiresUserConfirmation {
		return false
	}
	switch strings.ToLower(strings.TrimSpace(field.TypeName)) {
	case "", "string", "date", "datetime", "int", "integer", "float", "real", "number", "url":
		return true
	default:
		return false
	}
}

func semanticCapabilityKeys(field *ontology.Field) []string {
	keys := []string{field.Name}
	source := firstNonEmpty(field.Source, field.Name)
	switch field.SourceKind {
	case ontology.FieldSourceFrontmatter:
		keys = append(keys, "frontmatter."+source)
	case ontology.FieldSourceInline:
		keys = append(keys, "inline."+source)
	}
	return keys
}

func mergeFilterSpecs(existing []viewconfig.FilterSpec, next []viewconfig.FilterSpec) []viewconfig.FilterSpec {
	seen := make(map[string]struct{}, len(existing)+len(next))
	out := make([]viewconfig.FilterSpec, 0, len(existing)+len(next))
	for _, filter := range append(append([]viewconfig.FilterSpec(nil), existing...), next...) {
		key := fmt.Sprintf("%s\x00%s\x00%s\x00%v", filter.Field, filter.Op, filter.Value, filter.Values)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, filter)
	}
	return out
}

func mergeSortSpecs(existing []viewconfig.SortSpec, next []viewconfig.SortSpec) []viewconfig.SortSpec {
	seen := make(map[string]struct{}, len(existing)+len(next))
	out := make([]viewconfig.SortSpec, 0, len(existing)+len(next))
	for _, sortItem := range append(append([]viewconfig.SortSpec(nil), existing...), next...) {
		key := sortItem.Field + "\x00" + sortItem.Direction
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, sortItem)
	}
	return out
}

func hasResidualConstraints(constraints SourceConstraints) bool {
	return strings.TrimSpace(constraints.Search) != "" || len(constraints.Filters) > 0 || len(constraints.Sort) > 0
}

func capBoundWarnings(constraints SourceConstraints, limit int) []Warning {
	if !hasResidualConstraints(constraints) {
		return nil
	}
	warnings := []Warning{{
		Code:    "view_residual_constraints_cap_bound",
		Message: fmt.Sprintf("residual table operations ran over a capped candidate set of %d rows", limit),
	}}
	if strings.TrimSpace(constraints.Search) != "" {
		warnings = append(warnings, Warning{
			Code:    "view_search_cap_bound",
			Message: fmt.Sprintf("search ran over a capped candidate set of %d rows", limit),
			Path:    "search",
		})
	}
	if len(constraints.Filters) > 0 {
		warnings = append(warnings, Warning{
			Code:    "view_filter_cap_bound",
			Message: fmt.Sprintf("residual filters ran over a capped candidate set of %d rows", limit),
			Path:    "filters",
		})
	}
	if len(constraints.Sort) > 0 {
		warnings = append(warnings, Warning{
			Code:    "view_sort_cap_bound",
			Message: fmt.Sprintf("residual sort ran over a capped candidate set of %d rows", limit),
			Path:    "sort",
		})
	}
	return warnings
}

func enumOptions(schema *ontology.Schema, typeName string) []string {
	if schema == nil || schema.EnumTypes == nil {
		return nil
	}
	enumType := schema.EnumTypes[typeName]
	if enumType == nil {
		return nil
	}
	out := make([]string, 0, len(enumType.Values))
	for _, value := range enumType.Values {
		if value != nil && value.Name != "" {
			out = append(out, value.Name)
		}
	}
	return out
}

func (r defaultSourceResolver) editCandidatesForType(ctx context.Context, scope *noderead.Scope, typeName string) []EditCandidate {
	if scope == nil || strings.TrimSpace(typeName) == "" {
		return nil
	}
	result, err := scope.TypeInstances(ctx, noderead.TypeInstancesRequest{TypeName: typeName, Limit: 200})
	if err != nil {
		return nil
	}
	out := make([]EditCandidate, 0, len(result.Items))
	refs := make([]ontology.NodeRef, 0, len(result.Items))
	for _, item := range result.Items {
		if item.Ref.NotePath == "" {
			continue
		}
		refs = append(refs, item.Ref)
	}
	records, _ := scope.Hydrate(ctx, refs, noderead.HydrateOptions{Profile: noderead.HydrateSummary})
	recordsByPath := make(map[string]noderead.NodeRecord, len(records))
	for _, record := range records {
		recordsByPath[record.Path] = record
	}
	noteType := r.opts.Schema.Types[typeName]
	for _, item := range result.Items {
		if item.Ref.NotePath == "" {
			continue
		}
		label := editCandidateLabel(recordsByPath[item.Ref.NotePath], noteType, item.Title, item.NotePath)
		out = append(out, EditCandidate{
			Ref:   item.Ref,
			Value: editCandidateAuthoringValue(label, item.NotePath),
			Label: label,
			Path:  item.NotePath,
		})
	}
	return out
}

func (r defaultSourceResolver) FieldCandidates(ctx context.Context, view viewconfig.ViewDefinition, req FieldCandidatesRequest) (FieldCandidatesResponse, error) {
	fieldName := strings.TrimPrefix(strings.TrimPrefix(strings.TrimSpace(req.Field), "frontmatter."), "inline.")
	if fieldName == "" {
		return FieldCandidatesResponse{}, fmt.Errorf("%w: missing field", ErrInvalidRequest)
	}
	var targetType string
	for _, typeName := range r.sourceTypeNames(view) {
		noteType := r.opts.Schema.Types[typeName]
		if noteType == nil {
			continue
		}
		field := noteType.ByName[fieldName]
		if field == nil || field.Kind != ontology.FieldKindLink || field.List || field.TypeName == "" || field.TypeName == "Note" {
			continue
		}
		if targetType == "" {
			targetType = field.TypeName
			continue
		}
		if targetType != field.TypeName {
			return FieldCandidatesResponse{Field: req.Field}, nil
		}
	}
	if targetType == "" {
		return FieldCandidatesResponse{Field: req.Field}, nil
	}
	scope := nodereadScopeForResolver(ctx, r)
	candidates := r.editCandidatesForType(ctx, scope, targetType)
	query := strings.ToLower(strings.TrimSpace(req.Query))
	if query != "" {
		filtered := candidates[:0]
		for _, candidate := range candidates {
			if strings.Contains(strings.ToLower(candidate.Label), query) ||
				strings.Contains(strings.ToLower(candidate.Value), query) ||
				strings.Contains(strings.ToLower(candidate.Path), query) {
				filtered = append(filtered, candidate)
			}
		}
		candidates = filtered
	}
	limit := req.Limit
	if limit <= 0 {
		limit = 50
	}
	hasMore := len(candidates) > limit
	if hasMore {
		candidates = candidates[:limit]
	}
	return FieldCandidatesResponse{
		Field:      req.Field,
		TargetType: targetType,
		Candidates: candidates,
		HasMore:    hasMore,
	}, nil
}

func editCandidateLabel(record noderead.NodeRecord, noteType *ontology.NoteType, fallbackTitle, notePath string) string {
	for _, preferred := range []string{"displayName", "display-name", "name", "title"} {
		if value := frontmatterStringForField(record.Frontmatter, noteType, preferred); value != "" {
			return value
		}
	}
	return firstNonEmpty(record.Title, fallbackTitle, notePath)
}

func frontmatterStringForField(frontmatter map[string]any, noteType *ontology.NoteType, fieldName string) string {
	if len(frontmatter) == 0 || noteType == nil {
		return ""
	}
	field := noteType.ByName[fieldName]
	if field == nil || field.SourceKind != ontology.FieldSourceFrontmatter {
		return ""
	}
	for _, source := range ontology.FieldSourceNames(field) {
		if value := strings.TrimSpace(fmt.Sprint(frontmatter[source])); value != "" && value != "<nil>" {
			return value
		}
	}
	return ""
}

func editCandidateAuthoringValue(label, notePath string) string {
	target := strings.TrimSuffix(strings.TrimSpace(notePath), ".md")
	label = strings.TrimSpace(label)
	if target == "" {
		return label
	}
	if label == "" || strings.EqualFold(label, filepath.Base(target)) {
		return "[[" + target + "]]"
	}
	return "[[" + target + "|" + label + "]]"
}

func fieldCapabilitySource(key string) string {
	if idx := strings.Index(key, "."); idx > 0 {
		return key[:idx]
	}
	return "schema"
}

func sourceCapPolicy(view viewconfig.ViewDefinition, req ExecuteRequest) SourceCapPolicy {
	limit := defaultSourceCap
	source := "default"
	if view.Defaults.SourceCap > 0 {
		limit = view.Defaults.SourceCap
		source = "view.defaults.sourceCap"
	}
	if req.Source.MaxRows > 0 {
		limit = req.Source.MaxRows
		source = "request.source.maxRows"
	}
	if limit > maxSourceCap {
		limit = maxSourceCap
	}
	if limit <= 0 {
		limit = defaultSourceCap
		source = "default"
	}
	return SourceCapPolicy{Limit: limit, Source: source}
}

func sourceCompletenessForCount(count, limit int) SourceCompleteness {
	if limit > 0 && count >= limit {
		return SourceBounded
	}
	return SourceComplete
}

func sourceCompletenessForCountAndWarnings(count, limit int, warnings []Warning) SourceCompleteness {
	for _, warning := range warnings {
		if warningIndicatesBoundedSource(warning) {
			return SourceBounded
		}
	}
	return sourceCompletenessForCount(count, limit)
}

func cloneMap(in map[string]any) map[string]any {
	if len(in) == 0 {
		return map[string]any{}
	}
	out := make(map[string]any, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}

func cloneInline(in map[string][]string) map[string]any {
	out := make(map[string]any, len(in))
	for key, values := range in {
		out[key] = append([]string(nil), values...)
	}
	return out
}
