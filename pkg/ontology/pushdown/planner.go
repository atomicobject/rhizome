package pushdown

import (
	"context"
	"strings"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	"github.com/atomicobject/rhizome/pkg/ontology"
)

// LinkResolver resolves a wikilink-shaped value to an authoritative note path.
// Implementations may consult an index/store; the views path passes nil, the
// GraphQL path supplies an executor-backed resolver. When nil, predicates
// fall back to UnwrapWikilink only.
type LinkResolver interface {
	ResolveLinkTarget(ctx context.Context, field *ontology.Field, raw string) (string, Warning)
}

// Planner builds predicates and partitions sort specs.
type Planner struct {
	Schema       *ontology.Schema
	Resolver     Resolver
	LinkResolver LinkResolver
	// ValueNorm is applied to ValueNorm columns. Defaults to strings.ToLower.
	ValueNorm func(string) string
}

// CanPushFilter reports whether BuildPredicate would succeed for this filter.
func (p Planner) CanPushFilter(filter FilterInput) bool {
	op := normalizeOp(filter.Op)
	key, field, ok := p.Resolver.Resolve(filter.Field)
	if !ok {
		return false
	}
	if key.IsBuiltin() {
		return builtinFilterOpAllowed(op)
	}
	capability, ok := ontology.IndexedFieldCapabilityForField(p.Schema, field)
	if !ok {
		return false
	}
	for _, allowed := range capability.FilterOps {
		if strings.EqualFold(allowed, string(op)) {
			return true
		}
	}
	return false
}

// CanPushSort reports whether BuildSort would succeed for this sort key.
func (p Planner) CanPushSort(sortItem SortInput) bool {
	key, field, ok := p.Resolver.Resolve(sortItem.Field)
	if !ok {
		return false
	}
	if key.IsBuiltin() {
		return true
	}
	capability, ok := ontology.IndexedFieldCapabilityForField(p.Schema, field)
	if !ok {
		return false
	}
	return capability.Sortable
}

// BuildPredicate constructs an OntologyFieldPredicate. Returns (_, false, _)
// when the field/op cannot be pushed. Link-typed fields will use the
// configured LinkResolver when present; otherwise the raw target path from
// UnwrapWikilink is used.
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
	capability, ok := ontology.IndexedFieldCapabilityForField(p.Schema, field)
	if !ok {
		return PredicateResult{}, false
	}
	allowed := false
	for _, candidate := range capability.FilterOps {
		if strings.EqualFold(candidate, string(op)) {
			allowed = true
			break
		}
	}
	if !allowed {
		return PredicateResult{}, false
	}
	predicate := codeanchor.OntologyFieldPredicate{FieldName: field.Name, Op: op}
	var warnings []Warning
	switch op {
	case codeanchor.OntologyFieldOpExists:
		return PredicateResult{Predicate: predicate, Key: key}, true
	case codeanchor.OntologyFieldOpIn:
		for _, value := range filter.Values {
			row, valueWarnings := p.predicateRow(ctx, field, value)
			warnings = append(warnings, valueWarnings...)
			predicate.Values = append(predicate.Values, row)
		}
		ok := len(predicate.Values) > 0
		return PredicateResult{Predicate: predicate, Key: key, Warnings: warnings}, ok
	default:
		row, valueWarnings := p.predicateRow(ctx, field, filter.Value)
		warnings = append(warnings, valueWarnings...)
		predicate.Values = []codeanchor.IntelOntologyNodeFieldValue{row}
		return PredicateResult{Predicate: predicate, Key: key, Warnings: warnings}, len(predicate.Values) > 0 && hasNonEmptyPredicateValue(predicate.Values)
	}
}

func (p Planner) predicateRow(ctx context.Context, field *ontology.Field, value string) (codeanchor.IntelOntologyNodeFieldValue, []Warning) {
	row := FieldValueRow(field, value, p.ValueNorm)
	if field == nil || field.Kind != ontology.FieldKindLink || p.LinkResolver == nil {
		return row, nil
	}
	resolved, warning := p.LinkResolver.ResolveLinkTarget(ctx, field, value)
	if resolved != "" {
		row.TargetNotePath = resolved
	}
	if warning.Code != "" {
		return row, []Warning{warning}
	}
	return row, nil
}

// BuildSort builds a single OntologyFieldSort. Returns false if the sort key
// is not pushable.
func (p Planner) BuildSort(sortItem SortInput) (codeanchor.OntologyFieldSort, bool) {
	desc := strings.EqualFold(strings.TrimSpace(sortItem.Direction), "desc")
	key, field, ok := p.Resolver.Resolve(sortItem.Field)
	if !ok {
		return codeanchor.OntologyFieldSort{}, false
	}
	if key.IsBuiltin() {
		return codeanchor.OntologyFieldSort{FieldName: key.Canonical, ValueKind: "builtin", Desc: desc}, true
	}
	capability, ok := ontology.IndexedFieldCapabilityForField(p.Schema, field)
	if !ok || !capability.Sortable {
		return codeanchor.OntologyFieldSort{}, false
	}
	return codeanchor.OntologyFieldSort{
		FieldName: field.Name,
		ValueKind: capability.ValueKind,
		Desc:      desc,
		NullsLast: true,
	}, true
}

// PlanSort partitions sort specs using prefix-match: pushes the maximal
// supported prefix; the first unsupported key (and everything after it)
// becomes residual. This matches the documented residual-sort contract: an
// in-memory sort over the full bounded candidate set sees a coherent suffix.
func (p Planner) PlanSort(sorts []SortInput) SortPartition {
	if len(sorts) == 0 {
		return SortPartition{}
	}
	pushed := make([]SortInput, 0, len(sorts))
	for i, item := range sorts {
		if p.CanPushSort(item) {
			pushed = append(pushed, item)
			continue
		}
		residual := append([]SortInput(nil), sorts[i:]...)
		return SortPartition{Pushed: pushed, Residual: residual}
	}
	return SortPartition{Pushed: pushed}
}

// CanonicalKey resolves a key to its canonical form for pushdown variables
// (e.g., turning "frontmatter.status" into "status" for the indexed root
// args). Falls back to the trimmed input when the key cannot be resolved.
func (p Planner) CanonicalKey(key string) string {
	resolved, _, ok := p.Resolver.Resolve(key)
	if !ok {
		return strings.TrimSpace(key)
	}
	return resolved.Canonical
}

func normalizeOp(op string) codeanchor.OntologyFieldOperator {
	value := codeanchor.OntologyFieldOperator(strings.ToLower(strings.TrimSpace(op)))
	if value == "" {
		return codeanchor.OntologyFieldOpEq
	}
	return value
}

func builtinFilterOpAllowed(op codeanchor.OntologyFieldOperator) bool {
	switch op {
	case codeanchor.OntologyFieldOpEq, codeanchor.OntologyFieldOpIn, codeanchor.OntologyFieldOpExists:
		return true
	}
	return false
}

func buildBuiltinPredicate(fieldName string, op codeanchor.OntologyFieldOperator, filter FilterInput) (codeanchor.OntologyFieldPredicate, bool) {
	if !builtinFilterOpAllowed(op) {
		return codeanchor.OntologyFieldPredicate{}, false
	}
	predicate := codeanchor.OntologyFieldPredicate{FieldName: fieldName, Op: op}
	switch op {
	case codeanchor.OntologyFieldOpExists:
		return predicate, true
	case codeanchor.OntologyFieldOpIn:
		for _, value := range filter.Values {
			value = strings.TrimSpace(value)
			if value == "" {
				continue
			}
			for _, variant := range PathPredicateVariants(value) {
				predicate.Values = append(predicate.Values, codeanchor.IntelOntologyNodeFieldValue{ValueText: variant, ValueNorm: variant})
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
			predicate.Op = codeanchor.OntologyFieldOpIn
		}
		for _, variant := range variants {
			predicate.Values = append(predicate.Values, codeanchor.IntelOntologyNodeFieldValue{ValueText: variant, ValueNorm: variant})
		}
		return predicate, len(predicate.Values) > 0
	}
}

func hasNonEmptyPredicateValue(values []codeanchor.IntelOntologyNodeFieldValue) bool {
	for _, value := range values {
		if value.ValueText != "" || value.ValueBool != nil || value.ValueInt != nil || value.ValueReal != nil || value.ValueDate != nil || value.ValueDateTime != nil || value.TargetNotePath != "" {
			return true
		}
	}
	return false
}
