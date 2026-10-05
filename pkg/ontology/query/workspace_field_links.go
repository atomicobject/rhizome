package query

import (
	"context"
	"strings"

	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/ontology/noderead"
	"github.com/vektah/gqlparser/v2/ast"
)

// fieldLinkTarget is the resolved target of one relation-field value.
type fieldLinkTarget struct {
	Ref   ontology.NodeRef
	Title string
}

// fieldLinkTargets maps each relation-field value authored in one note to
// its resolved target, so clients can show the target's title instead of the
// authored link text.
type fieldLinkTargets map[string]fieldLinkTarget

// workspaceFieldOwner is one projected node whose relation fields need links.
type workspaceFieldOwner struct {
	Projection *ontology.NodeProjection
	Fields     []ontology.NodeFieldState
}

// workspaceFieldLinks reads each owner's relation targets from the indexed
// field values (the same rows configured views read), then titles them with
// one batched summary hydration. Only values the index cannot vouch for,
// staged edits or values changed since indexing, fall back to one index-only
// resolve relative to the note that authors them.
func (e *executor) workspaceFieldLinks(ctx context.Context, fromPath string, owners []workspaceFieldOwner, path []string) fieldLinkTargets {
	out := fieldLinkTargets{}
	if e == nil || e.loaders == nil || e.loaders.scope == nil {
		return out
	}
	scope := e.loaders.scope
	projections := make([]*ontology.NodeProjection, 0, len(owners))
	for _, owner := range owners {
		if owner.Projection != nil && hasRelationValues(owner.Fields) {
			projections = append(projections, owner.Projection)
		}
	}
	if len(projections) == 0 {
		return out
	}
	indexed, err := scope.IndexedFieldValues(ctx, projections)
	if err != nil {
		e.addError(path, err.Error())
		return out
	}

	indexedRefs := map[string]ontology.NodeRef{}
	settled := map[string]struct{}{}
	pending := map[string]struct{}{}
	for _, owner := range owners {
		if owner.Projection == nil {
			continue
		}
		rows := indexed[noderead.RefIdentityKey(owner.Projection.Ref)]
		for _, field := range owner.Fields {
			if field.Capability.ValueKind != ontology.NodeFieldValueKindRelation {
				continue
			}
			for _, value := range field.Values {
				value = strings.TrimSpace(value)
				if value == "" {
					continue
				}
				row, ok := noderead.IndexedLinkRow(rows, field.Name, value)
				if !ok {
					pending[value] = struct{}{}
					continue
				}
				settled[value] = struct{}{}
				if ref, ok := noderead.IndexedLinkTargetRef(row); ok {
					indexedRefs[value] = ref
				}
			}
		}
	}

	if len(indexedRefs) > 0 {
		refs := make([]ontology.NodeRef, 0, len(indexedRefs))
		for _, ref := range indexedRefs {
			refs = append(refs, ref)
		}
		records, err := scope.Hydrate(ctx, refs, noderead.HydrateOptions{Profile: noderead.HydrateSummary})
		if err != nil {
			e.addError(path, err.Error())
			return out
		}
		byKey := make(map[string]noderead.NodeRecord, len(records))
		for _, record := range records {
			byKey[noderead.RefIdentityKey(record.Ref)] = record
		}
		for value, ref := range indexedRefs {
			// A target the index names but hydration cannot find was deleted
			// since indexing; it reads as unresolved.
			if record, ok := byKey[noderead.RefIdentityKey(ref)]; ok && strings.TrimSpace(record.Path) != "" {
				out[value] = fieldLinkTarget{Ref: record.Ref, Title: record.Title}
			}
		}
	}

	targets := make([]noderead.NodeTarget, 0, len(pending))
	for value := range pending {
		if _, ok := settled[value]; !ok {
			targets = append(targets, noderead.NodeTarget{Input: value})
		}
	}
	if len(targets) == 0 {
		return out
	}
	result, err := scope.Resolve(ctx, noderead.ResolveRequest{
		OmitLocators: true,
		IndexOnly:    true,
		Targets:      targets,
		FromPath:     fromPath,
		Hydrate:      noderead.HydrateOptions{Profile: noderead.HydrateSummary},
	})
	if err != nil {
		e.addError(path, err.Error())
		return out
	}
	for _, item := range result.Resolved {
		if !item.Ref.IsZero() && strings.TrimSpace(item.Record.Path) != "" {
			out[item.Input] = fieldLinkTarget{Ref: item.Ref, Title: item.Record.Title}
		}
	}
	return out
}

func hasRelationValues(fields []ontology.NodeFieldState) bool {
	for _, field := range fields {
		if field.Capability.ValueKind == ontology.NodeFieldValueKindRelation && len(field.Values) > 0 {
			return true
		}
	}
	return false
}

func (e *executor) resolveWorkspaceFieldLinks(item ontology.NodeFieldState, links fieldLinkTargets, set ast.SelectionSet, path []string) []any {
	out := make([]any, 0)
	if item.Capability.ValueKind != ontology.NodeFieldValueKindRelation {
		return out
	}
	for _, value := range item.Values {
		resolved, ok := links[strings.TrimSpace(value)]
		link := make(map[string]any)
		e.eachSelectionField(set, func(field *ast.Field) {
			key := responseKey(field)
			switch field.Name {
			case "value":
				link[key] = value
			case "resolved":
				link[key] = ok
			case "ref":
				link[key] = nil
				if ok {
					link[key] = nodeRefGraphQLValue(resolved.Ref)
				}
			case "title":
				link[key] = nil
				if ok {
					link[key] = emptyNil(resolved.Title)
				}
			default:
				e.addError(append(path, key), "field does not exist on NodeFieldLink")
			}
		})
		out = append(out, link)
	}
	return out
}
