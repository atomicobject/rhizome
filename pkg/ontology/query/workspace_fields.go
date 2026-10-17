package query

import (
	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/vektah/gqlparser/v2/ast"
)

func (e *executor) resolveWorkspaceField(item ontology.NodeFieldState, links fieldLinkTargets, set ast.SelectionSet, path []string) map[string]any {
	out := make(map[string]any)
	e.eachSelectionField(set, func(field *ast.Field) {
		key := responseKey(field)
		switch field.Name {
		case "name":
			out[key] = item.Name
		case "kind":
			out[key] = string(item.Kind)
		case "sourceKind":
			out[key] = emptyNil(string(item.SourceKind))
		case "present":
			out[key] = item.Present
		case "status":
			out[key] = e.resolveWorkspaceStatus(item.Status, field.SelectionSet, append(path, key))
		case "range":
			out[key] = e.resolveWorkspaceRange(item.Range, field.SelectionSet, append(path, key))
		case "valueRanges":
			values := make([]any, 0, len(item.ValueRanges))
			for _, value := range item.ValueRanges {
				values = append(values, e.resolveWorkspaceRange(value, field.SelectionSet, append(path, key)))
			}
			out[key] = values
		case "values":
			// GraphQL declares this list non-null. Preserve that contract for
			// absent optional fields instead of letting a nil Go slice encode as
			// JSON null.
			out[key] = append([]string{}, item.Values...)
		case "links":
			out[key] = e.resolveWorkspaceFieldLinks(item, links, field.SelectionSet, append(path, key))
		case "sectionNodes":
			values := make([]any, 0, len(item.SectionNodes))
			for _, value := range item.SectionNodes {
				values = append(values, nodeRefGraphQLValue(value))
			}
			out[key] = values
		case "capability":
			out[key] = e.resolveWorkspaceFieldCapability(item.Capability, field.SelectionSet, append(path, key))
		case "issues":
			values := make([]any, 0, len(item.Issues))
			for _, value := range item.Issues {
				values = append(values, e.resolveAssessmentIssue(value, field.SelectionSet, append(path, key)))
			}
			out[key] = values
		default:
			e.addError(append(path, key), "field does not exist on NodeFieldState")
		}
	})
	return out
}

func (e *executor) resolveWorkspaceFieldCapability(item ontology.NodeFieldCapability, set ast.SelectionSet, path []string) map[string]any {
	out := make(map[string]any)
	e.eachSelectionField(set, func(field *ast.Field) {
		key := responseKey(field)
		values := map[string]any{
			"ownerRef":            nodeRefGraphQLValue(item.OwnerRef),
			"ownerType":           item.OwnerType,
			"typeName":            item.TypeName,
			"valueKind":           string(item.ValueKind),
			"list":                item.List,
			"required":            item.Required,
			"enumValues":          append([]string{}, item.EnumValues...),
			"enumOptions":         nodeFieldEnumOptionsGraphQLValue(item.EnumOptions),
			"targetType":          emptyNil(item.TargetType),
			"sourceKind":          emptyNil(string(item.SourceKind)),
			"valueOrigin":         string(item.ValueOrigin),
			"identifier":          item.Identifier,
			"preferredIdentifier": item.PreferredIdentifier,
			"displayImportance":   string(item.DisplayImportance),
			"writeOperation":      emptyNil(string(item.WriteOperation)),
			"readOnlyReason":      emptyNil(item.ReadOnlyReason),
		}
		if value, ok := values[field.Name]; ok {
			out[key] = value
		} else {
			e.addError(append(path, key), "field does not exist on NodeFieldCapability")
		}
	})
	return out
}

func nodeFieldEnumOptionsGraphQLValue(options []ontology.NodeFieldEnumOption) []any {
	out := make([]any, 0, len(options))
	for _, option := range options {
		out = append(out, map[string]any{"value": option.Value, "label": option.Label, "tone": emptyNil(option.Tone), "stage": emptyNil(string(option.Stage))})
	}
	return out
}

func (e *executor) resolveWorkspaceSourceRevision(item ontology.NodeSourceRevision, set ast.SelectionSet, path []string) map[string]any {
	out := make(map[string]any)
	e.eachSelectionField(set, func(field *ast.Field) {
		key := responseKey(field)
		values := map[string]any{
			"notePath":           item.NotePath,
			"contentFingerprint": item.ContentFingerprint,
			"content":            item.Content,
		}
		if value, ok := values[field.Name]; ok {
			out[key] = value
		} else {
			e.addError(append(path, key), "field does not exist on NodeSourceRevision")
		}
	})
	return out
}

func (e *executor) resolveWorkspaceCollection(item ontology.NodeCollectionState, set ast.SelectionSet, path []string) map[string]any {
	out := make(map[string]any)
	e.eachSelectionField(set, func(field *ast.Field) {
		key := responseKey(field)
		switch field.Name {
		case "name":
			out[key] = item.Name
		case "kind":
			out[key] = string(item.Kind)
		case "status":
			out[key] = e.resolveWorkspaceStatus(item.Status, field.SelectionSet, append(path, key))
		case "range":
			out[key] = e.resolveWorkspaceRange(item.Range, field.SelectionSet, append(path, key))
		case "items":
			values := make([]any, 0, len(item.Items))
			for _, value := range item.Items {
				entry := make(map[string]any)
				e.eachSelectionField(field.SelectionSet, func(child *ast.Field) {
					childKey := responseKey(child)
					switch child.Name {
					case "ref":
						entry[childKey] = nodeRefGraphQLValue(value.Ref)
					case "range":
						entry[childKey] = e.resolveWorkspaceRange(value.Range, child.SelectionSet, append(path, key, childKey))
					default:
						e.addError(append(path, key, childKey), "field does not exist on NodeCollectionItem")
					}
				})
				values = append(values, entry)
			}
			out[key] = values
		case "orderFingerprint":
			out[key] = emptyNil(item.OrderFingerprint)
		default:
			e.addError(append(path, key), "field does not exist on NodeCollectionState")
		}
	})
	return out
}

func (e *executor) resolveWorkspaceRange(item ontology.ByteRange, set ast.SelectionSet, path []string) map[string]any {
	out := make(map[string]any)
	e.eachSelectionField(set, func(field *ast.Field) {
		key := responseKey(field)
		switch field.Name {
		case "start":
			out[key] = item.Start
		case "end":
			out[key] = item.End
		default:
			e.addError(append(path, key), "field does not exist on NodeRange")
		}
	})
	return out
}

func (e *executor) resolveWorkspaceCapabilities(item ontology.NodeCapabilities, set ast.SelectionSet, path []string) map[string]any {
	values := map[string]bool{"canEdit": item.CanEdit, "canEditFields": item.CanEditFields, "canEditCollections": item.CanEditCollections, "canNavigateChildren": item.CanNavigateChildren, "canSubscribe": item.CanSubscribe}
	out := make(map[string]any)
	e.eachSelectionField(set, func(field *ast.Field) {
		key := responseKey(field)
		if value, ok := values[field.Name]; ok {
			out[key] = value
		} else {
			e.addError(append(path, key), "field does not exist on NodeCapabilities")
		}
	})
	return out
}

func (e *executor) resolveWorkspaceStatus(item ontology.NodeStatus, set ast.SelectionSet, path []string) map[string]any {
	out := make(map[string]any)
	e.eachSelectionField(set, func(field *ast.Field) {
		key := responseKey(field)
		switch field.Name {
		case "dirty":
			out[key] = item.Dirty
		case "hasWarnings":
			out[key] = item.HasWarnings
		case "validation":
			out[key] = e.resolveSingleInt("issueCount", item.Validation.IssueCount, field.SelectionSet, append(path, key))
		case "freshness":
			out[key] = e.resolveSingleString("state", item.Freshness.State, field.SelectionSet, append(path, key))
		case "session":
			out[key] = e.resolveSingleString("state", item.Session.State, field.SelectionSet, append(path, key))
		default:
			e.addError(append(path, key), "field does not exist on NodeStatus")
		}
	})
	return out
}

func (e *executor) resolveSingleInt(name string, value int, set ast.SelectionSet, path []string) map[string]any {
	out := make(map[string]any)
	e.eachSelectionField(set, func(field *ast.Field) {
		key := responseKey(field)
		if field.Name == name {
			out[key] = value
		} else {
			e.addError(append(path, key), "unknown field")
		}
	})
	return out
}
func (e *executor) resolveSingleString(name, value string, set ast.SelectionSet, path []string) map[string]any {
	out := make(map[string]any)
	e.eachSelectionField(set, func(field *ast.Field) {
		key := responseKey(field)
		if field.Name == name {
			out[key] = emptyNil(value)
		} else {
			e.addError(append(path, key), "unknown field")
		}
	})
	return out
}
