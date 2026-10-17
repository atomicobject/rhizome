// Package pushdown plans which view/graphql constraints can be served by the
// indexed ontology node read model and which must run residually after the
// source returns rows. It owns the canonical:
//
//   - field-key resolution (with view-side frontmatter./inline. prefixes and
//     graphql-side schema-canonical names)
//   - predicate construction (typed value coercion, path variant expansion)
//   - sort partitioning (prefix-match: pushes the maximal supported prefix,
//     residualizes the remainder so it sees a coherent suffix)
//
// Two call sites consume it: pkg/app/views/source_ontology.go (configured
// views) and pkg/ontology/query/execute.go (public GraphQL roots). Keeping
// one planner ensures both surfaces resolve aliases and coerce values the
// same way and obey the same residual semantics.
package pushdown

import (
	"github.com/atomicobject/rhizome/pkg/diag"
	"github.com/atomicobject/rhizome/pkg/ontology/readmodel"
)

// FilterInput is the planner's input shape. Both viewconfig.FilterSpec and
// untyped GraphQL filter maps adapt to this.
type FilterInput struct {
	Field  string
	Op     string
	Value  string
	Values []string
}

// SortInput is the planner's input shape for one sort key.
type SortInput struct {
	Field     string
	Direction string
}

// FieldKeySource records how a requested key was resolved against the schema.
type FieldKeySource string

const (
	FieldKeyUnknown     FieldKeySource = ""
	FieldKeyBuiltin     FieldKeySource = "builtin"     // notePath, path
	FieldKeySchema      FieldKeySource = "schema"      // direct (canonical) field name
	FieldKeyFrontmatter FieldKeySource = "frontmatter" // frontmatter.X prefix
	FieldKeyInline      FieldKeySource = "inline"      // inline.X prefix
)

// FieldKey describes how an authored key was resolved.
type FieldKey struct {
	Requested string
	Canonical string
	Source    FieldKeySource
}

// IsBuiltin reports whether the key resolved to a built-in notePath/path field.
func (k FieldKey) IsBuiltin() bool { return k.Source == FieldKeyBuiltin }

// PredicateResult bundles a predicate with the field key that produced it.
type PredicateResult struct {
	Predicate readmodel.FieldPredicate
	Key       FieldKey
	Warnings  []diag.Warning
}

// Plan keeps executable constraints together with every constraint that still
// needs evaluation. A failed build can never silently remove a filter.
type Plan struct {
	Pushed        readmodel.NodeQueryPlan
	PushedFilters []FilterInput
	PushedSort    []SortInput
	Residual      []FilterInput
	Sort          []SortInput
	Warnings      []diag.Warning
}
