package query

import "github.com/vektah/gqlparser/v2/ast"

func orderResponseValue(value any, set ast.SelectionSet, fragments ast.FragmentDefinitionList) any {
	switch current := value.(type) {
	case map[string]any:
		return orderResponseObject(current, set, fragments)
	case []any:
		out := make([]any, 0, len(current))
		for _, item := range current {
			out = append(out, orderResponseValue(item, set, fragments))
		}
		return out
	default:
		return value
	}
}

func orderResponseObject(data map[string]any, set ast.SelectionSet, fragments ast.FragmentDefinitionList) orderedObject {
	out := make(orderedObject, 0, len(data))
	seen := make(map[string]struct{}, len(data))
	var appendSelections func(ast.SelectionSet)
	appendSelections = func(currentSet ast.SelectionSet) {
		for _, selection := range currentSet {
			switch current := selection.(type) {
			case *ast.Field:
				key := responseKey(current)
				if _, ok := seen[key]; ok {
					continue
				}
				value, ok := data[key]
				if !ok {
					continue
				}
				seen[key] = struct{}{}
				if len(current.SelectionSet) > 0 {
					value = orderResponseValue(value, current.SelectionSet, fragments)
				}
				out = append(out, orderedField{key: key, value: value})
			case *ast.InlineFragment:
				appendSelections(current.SelectionSet)
			case *ast.FragmentSpread:
				fragment := fragments.ForName(current.Name)
				if fragment != nil {
					appendSelections(fragment.SelectionSet)
				}
			}
		}
	}
	appendSelections(set)
	return out
}
