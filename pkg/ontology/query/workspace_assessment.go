package query

import (
	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/vektah/gqlparser/v2/ast"
)

func (e *executor) resolveWorkspaceAssessment(item ontology.NoteAssessment, set ast.SelectionSet, path []string) map[string]any {
	out := make(map[string]any)
	e.eachSelectionField(set, func(field *ast.Field) {
		key := responseKey(field)
		switch field.Name {
		case "notePath":
			out[key] = item.NotePath
		case "declaredType":
			out[key] = emptyNil(item.DeclaredType)
		case "resolvedType":
			out[key] = emptyNil(item.ResolvedType)
		case "candidateTypes":
			out[key] = append([]string(nil), item.CandidateTypes...)
		case "issues":
			values := make([]any, 0, len(item.Issues))
			for _, value := range item.Issues {
				values = append(values, e.resolveAssessmentIssue(value, field.SelectionSet, append(path, key)))
			}
			out[key] = values
		case "fields":
			values := make([]any, 0, len(item.Fields))
			for _, value := range item.Fields {
				values = append(values, e.resolveFieldAssessment(value, field.SelectionSet, append(path, key)))
			}
			out[key] = values
		case "relations":
			values := make([]any, 0, len(item.Relations))
			for _, value := range item.Relations {
				values = append(values, e.resolveRelationAssessment(value, field.SelectionSet, append(path, key)))
			}
			out[key] = values
		default:
			e.addError(append(path, key), "field does not exist on NodeAssessment")
		}
	})
	return out
}

func (e *executor) resolveAssessmentIssue(item ontology.ValidationIssue, set ast.SelectionSet, path []string) map[string]any {
	out := make(map[string]any)
	e.eachSelectionField(set, func(field *ast.Field) {
		key := responseKey(field)
		values := map[string]any{"code": emptyNil(item.Code), "notePath": emptyNil(item.NotePath), "typeName": emptyNil(item.TypeName), "fieldName": emptyNil(item.FieldName), "nodeRef": emptyNil(item.NodeRef), "nodeId": emptyNil(item.NodeID), "line": item.Line, "message": item.Message}
		if value, ok := values[field.Name]; ok {
			out[key] = value
		} else {
			e.addError(append(path, key), "field does not exist on NodeAssessmentIssue")
		}
	})
	return out
}

func (e *executor) resolveFieldAssessment(item ontology.FieldAssessment, set ast.SelectionSet, path []string) map[string]any {
	out := make(map[string]any)
	e.eachSelectionField(set, func(field *ast.Field) {
		key := responseKey(field)
		switch field.Name {
		case "name":
			out[key] = item.Name
		case "description":
			out[key] = emptyNil(item.Description)
		case "kind":
			out[key] = string(item.Kind)
		case "typeName":
			out[key] = item.TypeName
		case "required":
			out[key] = item.Required
		case "list":
			out[key] = item.List
		case "source":
			out[key] = emptyNil(item.Source)
		case "sourceKind":
			out[key] = emptyNil(string(item.SourceKind))
		case "present":
			out[key] = item.Present
		case "values":
			out[key] = append([]string(nil), item.Values...)
		case "validValues":
			out[key] = append([]string(nil), item.ValidValues...)
		case "issues":
			values := make([]any, 0, len(item.Issues))
			for _, value := range item.Issues {
				values = append(values, e.resolveAssessmentIssue(value, field.SelectionSet, append(path, key)))
			}
			out[key] = values
		default:
			e.addError(append(path, key), "field does not exist on NodeFieldAssessment")
		}
	})
	return out
}

func (e *executor) resolveRelationAssessment(item ontology.RelationAssessment, set ast.SelectionSet, path []string) map[string]any {
	out := make(map[string]any)
	e.eachSelectionField(set, func(field *ast.Field) {
		key := responseKey(field)
		switch field.Name {
		case "name":
			out[key] = item.Name
		case "description":
			out[key] = emptyNil(item.Description)
		case "kind":
			out[key] = string(item.Kind)
		case "typeName":
			out[key] = item.TypeName
		case "required":
			out[key] = item.Required
		case "list":
			out[key] = item.List
		case "source":
			out[key] = emptyNil(item.Source)
		case "sourceKind":
			out[key] = emptyNil(string(item.SourceKind))
		case "direction":
			out[key] = emptyNil(string(item.Direction))
		case "present":
			out[key] = item.Present
		case "values":
			out[key] = append([]string(nil), item.Values...)
		case "targets":
			values := make([]any, 0, len(item.Targets))
			for _, value := range item.Targets {
				entry := make(map[string]any)
				e.eachSelectionField(field.SelectionSet, func(child *ast.Field) {
					childKey := responseKey(child)
					switch child.Name {
					case "path":
						entry[childKey] = value.Path
					case "typeName":
						entry[childKey] = emptyNil(value.TypeName)
					case "provenance":
						entry[childKey] = emptyNil(value.Provenance)
					case "structural":
						entry[childKey] = value.Structural
					default:
						e.addError(append(path, key, childKey), "field does not exist on NodeRelationTarget")
					}
				})
				values = append(values, entry)
			}
			out[key] = values
		case "issues":
			values := make([]any, 0, len(item.Issues))
			for _, value := range item.Issues {
				values = append(values, e.resolveAssessmentIssue(value, field.SelectionSet, append(path, key)))
			}
			out[key] = values
		default:
			e.addError(append(path, key), "field does not exist on NodeRelationAssessment")
		}
	})
	return out
}
