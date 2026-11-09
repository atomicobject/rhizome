package views

import (
	"context"
	"fmt"
	"reflect"
	"sort"
	"strings"

	"github.com/atomicobject/rhizome/pkg/diag"
	"github.com/atomicobject/rhizome/pkg/ontology/noderead"
	"github.com/atomicobject/rhizome/pkg/ontology/pushdown"
	ontoschema "github.com/atomicobject/rhizome/pkg/ontology/schema"
	"github.com/atomicobject/rhizome/pkg/ontology/viewconfig"
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
	r.linkTargetResolver = pushdown.NewCatalogLinkResolver(r.opts.Schema, r.opts.Store)
	if err := r.filterFieldError(view, req.Filters); err != nil {
		return SourceResult{}, err
	}
	req, bindingWarnings := r.bindRuntimeFilterValues(ctx, req)
	scope := r.nodeReadScope()
	if len(bindingWarnings) > 0 {
		return SourceResult{ConstraintPlan: true, Rows: []TableRow{}, Warnings: bindingWarnings, Capabilities: r.ontologyEditCapabilities(ctx, scope, view), relationScope: scope}, nil
	}
	result, err := r.resolveSource(ctx, scope, view, req)
	result.relationScope = scope
	result.Warnings = append(bindingWarnings, result.Warnings...)
	if !result.ConstraintPlan {
		result.ConstraintPlan = true
		result.ResidualConstraints = SourceConstraints{Search: req.Search, Filters: append([]viewconfig.FilterSpec(nil), req.Filters...), Sort: append([]viewconfig.SortSpec(nil), req.Sort...), Page: req.Page}
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
func (r defaultSourceResolver) bindRuntimeFilterValues(ctx context.Context, req ExecuteRequest) (ExecuteRequest, []diag.Warning) {
	if len(req.Filters) == 0 {
		return req, nil
	}
	out := req
	out.Filters = append([]viewconfig.FilterSpec(nil), req.Filters...)
	var warnings []diag.Warning
	for i := range out.Filters {
		valueFrom := strings.TrimSpace(out.Filters[i].ValueFrom)
		if valueFrom == "" {
			continue
		}
		switch valueFrom {
		case "ontology.currentUser.resolvedRef":
			if r.opts.QueryDeps.OntologyRuntime == nil {
				out.Filters[i].Value = ""
				warnings = append(warnings, diag.Warning{Code: "current_user_unavailable", Message: "current-user runtime provider is unavailable"})
				continue
			}
			currentUser, err := r.opts.QueryDeps.OntologyRuntime.CurrentUser(ctx)
			if err != nil {
				out.Filters[i].Value = ""
				warnings = append(warnings, diag.Warning{Code: "current_user_unavailable", Message: err.Error()})
				continue
			}
			if !currentUser.Configured || !currentUser.Found || currentUser.TypeName != "Person" || strings.TrimSpace(currentUser.ResolvedRef) == "" {
				out.Filters[i].Value = ""
				warnings = append(warnings, diag.Warning{Code: "current_user_missing", Message: "current user is not configured or does not resolve to Person"})
				continue
			}
			out.Filters[i].Value = currentUser.ResolvedRef
		default:
			out.Filters[i].Value = ""
			warnings = append(warnings, diag.Warning{Code: "unknown_filter_value_binding", Message: fmt.Sprintf("unknown filter value binding %q", valueFrom)})
		}
	}
	return out, warnings
}
func (r defaultSourceResolver) resolveOntologySource(ctx context.Context, scope *noderead.Scope, view viewconfig.ViewDefinition, req ExecuteRequest) (SourceResult, error) {
	if r.opts.Store == nil {
		return SourceResult{Warnings: []diag.Warning{{Code: "view_source_unavailable", Message: "ontology index is unavailable"}}}, nil
	}
	typeName := view.SourceSpec.Type
	if view.SourceSpec.Kind == viewconfig.SourceKindOntologyInterface {
		typeName = view.SourceSpec.Interface
	}
	if result, ok, err := r.resolveIndexedEmbeddedOntologySource(ctx, scope, view, req, typeName); ok || err != nil {
		return result, err
	}
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
		warnings = append(warnings, diag.Warning{Code: "view_source_may_be_truncated", Message: fmt.Sprintf("source returned the configured cap of %d rows before table operations", limit)})
	}
	capabilities := r.ontologyEditCapabilities(ctx, scope, view)
	rows, err = r.projectReferencedRelationCounts(ctx, scope, view, req, rows, capabilities)
	if err != nil {
		return SourceResult{}, err
	}
	return SourceResult{Rows: rows, Capabilities: capabilities, Warnings: warnings, Plan: ConstraintPlanSummary{CandidateLimit: limit, CandidateCount: len(rows), CapPolicy: capPolicy, SourceCompleteness: sourceCompletenessForCount(len(result.Items), limit)}}, nil
}
func (r defaultSourceResolver) resolveOntologyInterfaceSource(ctx context.Context, scope *noderead.Scope, view viewconfig.ViewDefinition, req ExecuteRequest) (SourceResult, error) {
	typeNames := r.sourceTypeNames(view)
	capPolicy := sourceCapPolicy(view, req)
	limit := capPolicy.Limit
	typePlans := make(map[string]SourceResult, len(typeNames))
	plan := SourceResult{ConstraintPlan: true, ResidualConstraints: SourceConstraints{Search: req.Search, Page: req.Page}}
	plan.Warnings = append(plan.Warnings, r.interfacePartialFieldWarnings(ctx, typeNames, req)...)
	for _, typeName := range typeNames {
		noteType := r.opts.Schema.Types[typeName]
		typePlan := r.planIndexedEmbeddedViewConstraints(ctx, noteType, req)
		typePlans[typeName] = typePlan
		plan.PushedConstraints.Filters = mergeFilterSpecs(plan.PushedConstraints.Filters, typePlan.PushedConstraints.Filters)
		plan.PushedConstraints.Sort = mergeSortSpecs(plan.PushedConstraints.Sort, typePlan.PushedConstraints.Sort)
		plan.ResidualConstraints.Filters = mergeFilterSpecs(plan.ResidualConstraints.Filters, typePlan.ResidualConstraints.Filters)
		if len(typePlan.ResidualConstraints.Sort) > 0 {
			plan.ResidualConstraints.Sort = append([]viewconfig.SortSpec(nil), req.Sort...)
		}
	}
	if r.canUseCollapsedInterfaceSource(typeNames, plan) && equivalentInterfacePlans(typeNames, typePlans) {
		typePlan := typePlans[typeNames[0]]
		plan.Warnings = append(plan.Warnings, typePlan.indexedWarnings...)
		result, err := scope.TypeInstances(ctx, noderead.TypeInstancesRequest{TypeName: view.SourceSpec.Interface, Limit: limit, Predicates: typePlan.indexedPredicates, Sort: typePlan.indexedSort})
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
				plan.Warnings = append(plan.Warnings, diag.Warning{Code: "view_source_residual_constraints_after_cap", Message: fmt.Sprintf("ontology interface source returned the configured cap of %d rows while residual filters or sort remain", limit)})
			} else {
				plan.Warnings = append(plan.Warnings, diag.Warning{Code: "view_source_may_be_truncated", Message: fmt.Sprintf("source returned the configured cap of %d rows before table operations", limit)})
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
		typePlan := typePlans[typeName]
		plan.Warnings = append(plan.Warnings, typePlan.indexedWarnings...)
		result, err := scope.TypeInstances(ctx, noderead.TypeInstancesRequest{TypeName: typeName, Limit: limit, Predicates: typePlan.indexedPredicates, Sort: typePlan.indexedSort})
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
			plan.Warnings = append(plan.Warnings, diag.Warning{Code: "view_source_residual_constraints_after_cap", Message: fmt.Sprintf("ontology interface implementor %s returned the configured cap of %d rows while residual filters or sort remain", typeName, limit)})
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
			plan.Warnings = append(plan.Warnings, diag.Warning{Code: "view_source_residual_constraints_after_cap", Message: fmt.Sprintf("ontology interface source returned the configured cap of %d rows while residual filters or sort remain", limit)})
		} else {
			plan.Warnings = append(plan.Warnings, diag.Warning{Code: "view_source_may_be_truncated", Message: fmt.Sprintf("source returned the configured cap of %d rows before table operations", limit)})
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
func equivalentInterfacePlans(typeNames []string, plans map[string]SourceResult) bool {
	if len(typeNames) < 2 {
		return true
	}
	first := plans[typeNames[0]]
	for _, name := range typeNames[1:] {
		plan := plans[name]
		if !reflect.DeepEqual(first.indexedPredicates, plan.indexedPredicates) || !reflect.DeepEqual(first.indexedSort, plan.indexedSort) {
			return false
		}
	}
	return true
}
func (r defaultSourceResolver) canUseCollapsedInterfaceSource(typeNames []string, plan SourceResult) bool {
	return len(typeNames) > 0 && len(plan.ResidualConstraints.Filters) == 0 && len(plan.ResidualConstraints.Sort) == 0
}
func (r defaultSourceResolver) resolveIndexedEmbeddedOntologySource(ctx context.Context, scope *noderead.Scope, view viewconfig.ViewDefinition, req ExecuteRequest, typeName string) (SourceResult, bool, error) {
	if view.SourceSpec.Kind != viewconfig.SourceKindOntologyType || r.opts.Schema == nil || r.opts.Store == nil {
		return SourceResult{}, false, nil
	}
	noteType := r.opts.Schema.Types[typeName]
	if noteType == nil || (noteType.Role != ontoschema.TypeRoleEmbeddedNode && noteType.Role != ontoschema.TypeRoleNote) {
		return SourceResult{}, false, nil
	}
	capPolicy := sourceCapPolicy(view, req)
	limit := capPolicy.Limit
	plan := r.planIndexedEmbeddedViewConstraints(ctx, noteType, req)
	plan.Warnings = append(plan.Warnings, plan.indexedWarnings...)
	result, err := scope.TypeInstances(ctx, noderead.TypeInstancesRequest{TypeName: typeName, Limit: limit, Predicates: plan.indexedPredicates, Sort: plan.indexedSort})
	if err != nil {
		return SourceResult{}, true, fmt.Errorf("%w: indexed ontology source query failed: %s", ErrInvalidRequest, err.Error())
	}
	rows := make([]TableRow, 0, len(result.Items))
	for _, item := range result.Items {
		rows = append(rows, rowFromNodeListItem(item))
	}
	rows, hydrateWarnings := r.hydrateRowsWithScope(ctx, scope, rows)
	warnings := append([]diag.Warning(nil), hydrateWarnings...)
	if len(rows) >= limit {
		if hasResidualConstraints(plan.ResidualConstraints) {
			warnings = append(warnings, capBoundWarnings(plan.ResidualConstraints, limit)...)
		}
		warnings = append(warnings, diag.Warning{Code: "view_source_may_be_truncated", Message: fmt.Sprintf("source returned the configured cap of %d rows before residual table operations", limit)})
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
func sourceCompletenessForCountAndWarnings(count, limit int, warnings []diag.Warning) SourceCompleteness {
	for _, warning := range warnings {
		if warningIndicatesBoundedSource(warning) {
			return SourceBounded
		}
	}
	return sourceCompletenessForCount(count, limit)
}
