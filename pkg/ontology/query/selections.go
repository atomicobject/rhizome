package query

import (
	"fmt"

	"github.com/vektah/gqlparser/v2/ast"
)

// resolveSelections shares alias and fragment traversal while each node kind
// owns its fields and type conditions. Fragments write into the same result in
// selection order, preserving the existing overwrite behavior for repeated keys.
func (e *executor) resolveSelections(set ast.SelectionSet, path []string, unsupported string, matchesType func(string) bool, resolveField func(*ast.Field, []string) (any, bool)) map[string]any {
	out := make(map[string]any)
	var visit func(ast.SelectionSet)
	visit = func(set ast.SelectionSet) {
		for _, selection := range set {
			var condition string
			var nested ast.SelectionSet
			switch current := selection.(type) {
			case *ast.Field:
				key := responseKey(current)
				if value, ok := resolveField(current, append(path, key)); ok {
					out[key] = value
				}
				continue
			case *ast.InlineFragment:
				condition, nested = current.TypeCondition, current.SelectionSet
			case *ast.FragmentSpread:
				fragment := e.fragments.ForName(current.Name)
				if fragment == nil {
					e.addError(path, fmt.Sprintf("fragment %q not found", current.Name))
					continue
				}
				condition, nested = fragment.TypeCondition, fragment.SelectionSet
			default:
				e.addError(path, unsupported)
				continue
			}
			if matchesType(condition) {
				visit(nested)
			}
		}
	}
	visit(set)
	return out
}
