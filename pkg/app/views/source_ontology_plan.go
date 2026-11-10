package views

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/atomicobject/rhizome/pkg/diag"
	"github.com/atomicobject/rhizome/pkg/ontology/pushdown"
	"github.com/atomicobject/rhizome/pkg/ontology/readmodel"
	ontoschema "github.com/atomicobject/rhizome/pkg/ontology/schema"
	"github.com/atomicobject/rhizome/pkg/ontology/viewconfig"
	"github.com/atomicobject/rhizome/pkg/paths"
)

// viewPlannerFor builds the canonical pushdown planner for a configured
// view's NoteType. Views resolve frontmatter./inline. prefixed and
// case-insensitive keys and use the same indexed link target resolver for
// both predicate construction and execution-time diagnostics.
func (r defaultSourceResolver) viewPlannerFor(noteType *ontoschema.NoteType) pushdown.Planner {
	return pushdown.Planner{Schema: r.opts.Schema, Resolver: pushdown.ViewResolver{NoteType: noteType}, LinkResolver: r.linkTargetResolver}
}

// viewRowFilterKeys are the row values every ontology view row carries
// (fieldValue), plus the stale filter.
var viewRowFilterKeys = map[string]bool{"title": true, "path": true, "notePath": true, "resolvedType": true, "updatedAt": true, "hasIssues": true, "tags": true, staleFilterField: true}

// filterFieldError rejects an ontology view filter that would silently match
// nothing, as GraphQL roots do (SPEC-0136 R8): a field no source type declares,
// or a computed relation views cannot count. Row values, raw frontmatter. and
// inline. keys, and counts of list relations stay filterable.
func (r defaultSourceResolver) filterFieldError(view viewconfig.ViewDefinition, filters []viewconfig.FilterSpec) error {
	typeNames := r.sourceTypeNames(view)
	if len(typeNames) == 0 {
		return nil
	}
	resolvers := make([]pushdown.Resolver, 0, len(typeNames))
	for _, name := range typeNames {
		resolvers = append(resolvers, pushdown.ViewResolver{NoteType: r.opts.Schema.Types[name]})
	}
	for _, filter := range filters {
		field := strings.TrimSpace(filter.Field)
		if field == "" || viewRowFilterKeys[field] || strings.HasPrefix(field, "frontmatter.") || strings.HasPrefix(field, "inline.") || countsRelation(field, resolvers) {
			continue
		}
		if err := pushdown.FilterFieldError(field, resolvers...); err != nil {
			return fmt.Errorf("%w: %v", ErrInvalidRequest, err)
		}
	}
	return nil
}

func countsRelation(field string, resolvers []pushdown.Resolver) bool {
	for _, resolver := range resolvers {
		if _, schemaField, ok := resolver.Resolve(field); ok {
			if _, counted := relationCountCapability(schemaField); counted {
				return true
			}
		}
	}
	return false
}

func filterToPushdownInput(filter viewconfig.FilterSpec) pushdown.FilterInput {
	return pushdown.FilterInput{Field: filter.Field, Op: filter.Op, Value: filter.Value, Values: filter.Values}
}
func sortToPushdownInput(sortItem viewconfig.SortSpec) pushdown.SortInput {
	return pushdown.SortInput{Field: sortItem.Field, Direction: sortItem.Direction}
}
func (r defaultSourceResolver) planIndexedEmbeddedViewConstraints(ctx context.Context, noteType *ontoschema.NoteType, req ExecuteRequest) SourceResult {
	planner := r.viewPlannerFor(noteType)
	filters := make([]pushdown.FilterInput, len(req.Filters))
	for i, filter := range req.Filters {
		filters[i] = filterToPushdownInput(filter)
	}
	sorts := make([]pushdown.SortInput, len(req.Sort))
	for i, spec := range req.Sort {
		sorts[i] = sortToPushdownInput(spec)
	}
	compiled := planner.Plan(ctx, filters, sorts)
	result := SourceResult{ConstraintPlan: true, ResidualConstraints: SourceConstraints{Search: req.Search, Page: req.Page}, indexedSort: compiled.Pushed.Sort}
	for i, input := range compiled.PushedFilters {
		filter := viewconfig.FilterSpec{Field: input.Field, Op: input.Op, Value: input.Value, Values: input.Values}
		result.PushedConstraints.Filters = append(result.PushedConstraints.Filters, filter)
		key, _, _ := planner.Resolver.Resolve(input.Field)
		result.indexedPredicates = append(result.indexedPredicates, viewPathPredicateWithMarkdownCompatibility(filter, pushdown.PredicateResult{Predicate: compiled.Pushed.Predicates[i], Key: key}))
	}
	for _, input := range compiled.Residual {
		result.ResidualConstraints.Filters = append(result.ResidualConstraints.Filters, viewconfig.FilterSpec{Field: input.Field, Op: input.Op, Value: input.Value, Values: input.Values})
	}
	for _, input := range compiled.PushedSort {
		result.PushedConstraints.Sort = append(result.PushedConstraints.Sort, viewconfig.SortSpec{Field: input.Field, Direction: input.Direction})
	}
	for _, input := range compiled.Sort {
		result.ResidualConstraints.Sort = append(result.ResidualConstraints.Sort, viewconfig.SortSpec{Field: input.Field, Direction: input.Direction})
	}
	result.indexedWarnings = append(result.indexedWarnings, compiled.Warnings...)
	return result
}
func (r defaultSourceResolver) viewFilterCanPush(ctx context.Context, noteType *ontoschema.NoteType, filter viewconfig.FilterSpec) bool {
	if noteType == nil {
		return false
	}
	_, ok := r.viewPlannerFor(noteType).BuildPredicate(ctx, filterToPushdownInput(filter))
	return ok
}
func (r defaultSourceResolver) interfacePartialFieldWarnings(ctx context.Context, typeNames []string, req ExecuteRequest) []diag.Warning {
	if len(typeNames) == 0 {
		return nil
	}
	var warnings []diag.Warning
	for _, filter := range req.Filters {
		field := strings.TrimSpace(filter.Field)
		if field == "" || pushdown.IsBuiltinFieldKey(field) {
			continue
		}
		pushable := 0
		for _, typeName := range typeNames {
			if r.viewFilterCanPush(ctx, r.opts.Schema.Types[typeName], filter) {
				pushable++
			}
		}
		if pushable > 0 && pushable < len(typeNames) {
			warnings = append(warnings, diag.Warning{Code: "interface_partial_field_residual", Message: fmt.Sprintf("interface field %q is indexed for %d of %d implementors; residual filtering will run after source enumeration for the remaining implementors", field, pushable, len(typeNames)), Path: field})
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
			warnings = append(warnings, diag.Warning{Code: "interface_partial_field_residual", Message: fmt.Sprintf("interface sort field %q is indexed for %d of %d implementors; residual sorting will run after source enumeration for the remaining implementors", field, pushable, len(typeNames)), Path: field})
		}
	}
	return warnings
}
func (r defaultSourceResolver) viewSortKeyCanPush(noteType *ontoschema.NoteType, sortItem viewconfig.SortSpec) bool {
	if noteType == nil {
		return pushdown.IsBuiltinFieldKey(sortItem.Field)
	}
	_, ok := r.viewPlannerFor(noteType).BuildSort(sortToPushdownInput(sortItem))
	return ok
}

// viewPathPredicateWithMarkdownCompatibility preserves the legacy view-input
// contract for extensionless Markdown paths. The generic pushdown planner must
// preserve authored paths, because it cannot know which provider owns a path.
// Views currently query Markdown-derived ontology roots, so this explicit
// compatibility boundary may also match the historical .md form. Explicit
// extensions always remain unchanged.
func viewPathPredicateWithMarkdownCompatibility(filter viewconfig.FilterSpec, result pushdown.PredicateResult) readmodel.FieldPredicate {
	predicate := result.Predicate
	if !result.Key.IsBuiltin() || predicate.Op == readmodel.FieldOpExists {
		return predicate
	}
	values := filter.Values
	if predicate.Op != readmodel.FieldOpIn {
		values = []string{filter.Value}
	}
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || filepath.Ext(value) != "" {
			continue
		}
		markdownPath := paths.EnsureNoteExtension(value, ".md").String()
		predicate.Values = append(predicate.Values, readmodel.FieldValueRow{ValueText: markdownPath, ValueNorm: markdownPath})
	}
	if len(predicate.Values) > 1 {
		predicate.Op = readmodel.FieldOpIn
	}
	return predicate
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
func capBoundWarnings(constraints SourceConstraints, limit int) []diag.Warning {
	if !hasResidualConstraints(constraints) {
		return nil
	}
	warnings := []diag.Warning{{Code: "view_residual_constraints_cap_bound", Message: fmt.Sprintf("residual table operations ran over a capped candidate set of %d rows", limit)}}
	if strings.TrimSpace(constraints.Search) != "" {
		warnings = append(warnings, diag.Warning{Code: "view_search_cap_bound", Message: fmt.Sprintf("search ran over a capped candidate set of %d rows", limit), Path: "search"})
	}
	if len(constraints.Filters) > 0 {
		warnings = append(warnings, diag.Warning{Code: "view_filter_cap_bound", Message: fmt.Sprintf("residual filters ran over a capped candidate set of %d rows", limit), Path: "filters"})
	}
	if len(constraints.Sort) > 0 {
		warnings = append(warnings, diag.Warning{Code: "view_sort_cap_bound", Message: fmt.Sprintf("residual sort ran over a capped candidate set of %d rows", limit), Path: "sort"})
	}
	return warnings
}
