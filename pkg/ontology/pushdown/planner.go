package pushdown

import (
	"context"
	"fmt"
	"github.com/atomicobject/rhizome/pkg/diag"
	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/ontology/readmodel"
	ontoschema "github.com/atomicobject/rhizome/pkg/ontology/schema"
	"strings"
)

// LinkResolver resolves a wikilink-shaped value to an authoritative note path.
// Implementations may consult an index/store; the views path passes nil, the
// GraphQL path supplies an executor-backed resolver. When nil, predicates
// fall back to ontology.UnwrapLinkValue only.
type LinkResolver interface {
	ResolveLinkTarget(ctx context.Context, field *ontoschema.Field, raw string) (string, diag.Warning)
}

// Planner builds predicates and partitions sort specs.
type Planner struct {
	Schema       *ontoschema.Schema
	Resolver     Resolver
	LinkResolver LinkResolver
}

func (p Planner) Plan(ctx context.Context, filters []FilterInput, sorts []SortInput) Plan {
	var plan Plan
	for _, filter := range filters {
		result, supported := p.BuildPredicate(ctx, filter)
		plan.Warnings = append(plan.Warnings, result.Warnings...)
		if supported {
			plan.Pushed.Predicates = append(plan.Pushed.Predicates, result.Predicate)
			plan.PushedFilters = append(plan.PushedFilters, filter)
		} else {
			plan.Residual = append(plan.Residual, filter)
		}
	}
	for _, input := range sorts {
		if spec, ok := p.BuildSort(input); ok && len(plan.Sort) == 0 {
			plan.Pushed.Sort = append(plan.Pushed.Sort, spec)
			plan.PushedSort = append(plan.PushedSort, input)
		} else if len(plan.Sort) == 0 {
			plan.Sort = append([]SortInput(nil), sorts...)
		}
	}
	return plan
}

// BuildPredicate constructs an OntologyFieldPredicate. Returns (_, false, _)
// when the field/op cannot be pushed. Link-typed fields will use the
// configured LinkResolver when present; otherwise the raw target path from
// ontology.UnwrapLinkValue is used.
func (p Planner) BuildPredicate(ctx context.Context, filter FilterInput) (PredicateResult, bool) {
	op := normalizeOp(filter.Op)
	key, field, ok := p.Resolver.Resolve(filter.Field)
	if !ok {
		return PredicateResult{}, false
	}
	if key.IsBuiltin() {
		predicate, ok := buildBuiltinPredicate(key.Canonical, op, filter)
		return PredicateResult{Predicate: predicate, Key: key}, ok
	}
	capability, ok := ontoschema.FieldQueryCapabilityForField(p.Schema, field)
	if !ok {
		return PredicateResult{}, false
	}
	allowed := false
	for _, candidate := range capability.IndexedFilterOps {
		if strings.EqualFold(candidate, string(op)) {
			allowed = true
			break
		}
	}
	if !allowed {
		if field.Kind == ontoschema.FieldKindLink {
			return PredicateResult{Warnings: []diag.Warning{{Code: "link_filter_operator_unsupported", Message: fmt.Sprintf("link field %q supports eq, in, and exists; %s does not test resolved target membership", field.Name, op)}}}, false
		}
		return PredicateResult{}, false
	}
	predicate := readmodel.FieldPredicate{FieldName: field.Name, Op: op}
	var warnings []diag.Warning
	switch op {
	case readmodel.FieldOpExists:
		return PredicateResult{Predicate: predicate, Key: key}, true
	case readmodel.FieldOpIn:
		for _, value := range filter.Values {
			row, valueWarnings := p.predicateRow(ctx, field, value)
			warnings = append(warnings, valueWarnings...)
			if hasNonEmptyPredicateValue([]readmodel.FieldValueRow{row}) {
				predicate.Values = append(predicate.Values, row)
			}
		}
		ok := len(predicate.Values) > 0
		return PredicateResult{Predicate: predicate, Key: key, Warnings: warnings}, ok
	default:
		row, valueWarnings := p.predicateRow(ctx, field, filter.Value)
		warnings = append(warnings, valueWarnings...)
		predicate.Values = []readmodel.FieldValueRow{row}
		return PredicateResult{Predicate: predicate, Key: key, Warnings: warnings}, len(predicate.Values) > 0 && hasNonEmptyPredicateValue(predicate.Values)
	}
}

func (p Planner) predicateRow(ctx context.Context, field *ontoschema.Field, value string) (readmodel.FieldValueRow, []diag.Warning) {
	row := ontology.FieldValueRow(field, value)
	if field == nil || field.Kind != ontoschema.FieldKindLink || p.LinkResolver == nil {
		return row, nil
	}
	resolved, warning := p.LinkResolver.ResolveLinkTarget(ctx, field, value)
	if resolved != "" {
		row.TargetNotePath = resolved
	}
	if warning.Code != "" {
		return row, []diag.Warning{warning}
	}
	return row, nil
}

// BuildSort builds a single OntologyFieldSort. Returns false if the sort key
// is not pushable.
func (p Planner) BuildSort(sortItem SortInput) (readmodel.FieldSort, bool) {
	desc := strings.EqualFold(strings.TrimSpace(sortItem.Direction), "desc")
	key, field, ok := p.Resolver.Resolve(sortItem.Field)
	if !ok {
		return readmodel.FieldSort{}, false
	}
	if key.IsBuiltin() {
		return readmodel.FieldSort{FieldName: key.Canonical, ValueKind: "builtin", Desc: desc}, true
	}
	capability, ok := ontoschema.FieldQueryCapabilityForField(p.Schema, field)
	if !ok || !capability.Sortable {
		return readmodel.FieldSort{}, false
	}
	return readmodel.FieldSort{
		FieldName: field.Name,
		ValueKind: capability.ValueKind,
		Desc:      desc,
		NullsLast: true,
	}, true
}

func normalizeOp(op string) readmodel.FieldOperator {
	value := readmodel.FieldOperator(strings.ToLower(strings.TrimSpace(op)))
	if value == "" {
		return readmodel.FieldOpEq
	}
	return value
}

func builtinFilterOpAllowed(op readmodel.FieldOperator) bool {
	switch op {
	case readmodel.FieldOpEq, readmodel.FieldOpIn, readmodel.FieldOpExists:
		return true
	}
	return false
}

func buildBuiltinPredicate(fieldName string, op readmodel.FieldOperator, filter FilterInput) (readmodel.FieldPredicate, bool) {
	if !builtinFilterOpAllowed(op) {
		return readmodel.FieldPredicate{}, false
	}
	predicate := readmodel.FieldPredicate{FieldName: fieldName, Op: op}
	switch op {
	case readmodel.FieldOpExists:
		return predicate, true
	case readmodel.FieldOpIn:
		for _, value := range filter.Values {
			value = strings.TrimSpace(value)
			if value == "" {
				continue
			}
			for _, variant := range PathPredicateVariants(value) {
				predicate.Values = append(predicate.Values, readmodel.FieldValueRow{ValueText: variant, ValueNorm: variant})
			}
		}
		return predicate, len(predicate.Values) > 0
	default:
		value := strings.TrimSpace(filter.Value)
		if value == "" {
			return predicate, false
		}
		variants := PathPredicateVariants(value)
		if len(variants) > 1 {
			predicate.Op = readmodel.FieldOpIn
		}
		for _, variant := range variants {
			predicate.Values = append(predicate.Values, readmodel.FieldValueRow{ValueText: variant, ValueNorm: variant})
		}
		return predicate, len(predicate.Values) > 0
	}
}

func hasNonEmptyPredicateValue(values []readmodel.FieldValueRow) bool {
	for _, value := range values {
		if value.ValueText != "" || value.ValueBool != nil || value.ValueInt != nil || value.ValueReal != nil || value.ValueDate != nil || value.ValueDateTime != nil || value.TargetNotePath != "" {
			return true
		}
	}
	return false
}
