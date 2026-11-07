package query

import (
	"context"
	"strings"

	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/ontology/noderead"
	"github.com/atomicobject/rhizome/pkg/ontology/readmodel"
	"github.com/vektah/gqlparser/v2/ast"
)

// fieldLinkTarget is the resolved target of one relation-field value.
type fieldLinkTarget struct {
	Ref   ontology.NodeRef
	Title string
}

// fieldLinkKey names one authored value occurrence: its owner node, field and
// position. Equal text in another field or owner can name a different target.
type fieldLinkKey struct {
	owner string
	field string
	index int
}

func fieldLinkKeyFor(field ontology.NodeFieldState, index int) fieldLinkKey {
	return fieldLinkKey{owner: noderead.RefIdentityKey(field.Capability.OwnerRef), field: field.Name, index: index}
}

// fieldLinkTargets maps each relation-field value occurrence to its resolved
// target, so clients can show the target's title instead of the authored link
// text.
type fieldLinkTargets map[fieldLinkKey]fieldLinkTarget

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
		if owner.Projection != nil && e.hasLinkValues(owner.Fields) {
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

	indexedRefs := map[fieldLinkKey]ontology.NodeRef{}
	pending := map[string][]fieldLinkKey{}
	for _, owner := range owners {
		if owner.Projection == nil {
			continue
		}
		rows := indexed[noderead.RefIdentityKey(owner.Projection.Ref)]
		for _, field := range owner.Fields {
			if e.recordReferenceField(field) {
				// Bound occurrences already carry the reference outcome; a
				// generic resolve could let a same-named note win.
				for i, ref := range recordReferenceValueTargets(owner.Projection, field, rows) {
					indexedRefs[fieldLinkKeyFor(field, i)] = ref
				}
				continue
			}
			if field.Capability.ValueKind != ontology.NodeFieldValueKindRelation {
				continue
			}
			for i, value := range field.Values {
				value = strings.TrimSpace(value)
				if value == "" {
					continue
				}
				key := fieldLinkKeyFor(field, i)
				row, ok := noderead.IndexedLinkRow(rows, field.Name, value)
				if !ok {
					pending[value] = append(pending[value], key)
					continue
				}
				if ref, ok := noderead.IndexedLinkTargetRef(row); ok {
					indexedRefs[key] = ref
				}
			}
		}
	}

	if len(indexedRefs) > 0 {
		refs := make([]ontology.NodeRef, 0, len(indexedRefs))
		seen := make(map[string]struct{}, len(indexedRefs))
		for _, ref := range indexedRefs {
			if _, ok := seen[noderead.RefIdentityKey(ref)]; !ok {
				seen[noderead.RefIdentityKey(ref)] = struct{}{}
				refs = append(refs, ref)
			}
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
		for key, ref := range indexedRefs {
			// A target the index names but hydration cannot find was deleted
			// since indexing; it reads as unresolved.
			if record, ok := byKey[noderead.RefIdentityKey(ref)]; ok && strings.TrimSpace(record.Path) != "" {
				out[key] = fieldLinkTarget{Ref: record.Ref, Title: record.Title}
			}
		}
	}

	if len(pending) == 0 {
		return out
	}
	targets := make([]noderead.NodeTarget, 0, len(pending))
	for value := range pending {
		targets = append(targets, noderead.NodeTarget{Input: value})
	}
	result, err := scope.Resolve(ctx, noderead.ResolveRequest{
		IndexOnly: true,
		Targets:   targets,
		FromPath:  fromPath,
		Hydrate:   noderead.HydrateOptions{Profile: noderead.HydrateSummary},
	})
	if err != nil {
		e.addError(path, err.Error())
		return out
	}
	for _, item := range result.Resolved {
		if !item.Ref.IsZero() && strings.TrimSpace(item.Record.Path) != "" {
			for _, key := range pending[item.Input] {
				out[key] = fieldLinkTarget{Ref: item.Ref, Title: item.Record.Title}
			}
		}
	}
	return out
}

func (e *executor) hasLinkValues(fields []ontology.NodeFieldState) bool {
	for _, field := range fields {
		if e.linkField(field) && len(field.Values) > 0 {
			return true
		}
	}
	return false
}

// linkField reports whether a field's values name other nodes: a relation
// field, or a RECORD field declared with @references even when typed as text.
func (e *executor) linkField(field ontology.NodeFieldState) bool {
	return field.Capability.ValueKind == ontology.NodeFieldValueKindRelation || e.recordReferenceField(field)
}

func (e *executor) recordReferenceField(field ontology.NodeFieldState) bool {
	if e == nil || e.schema == nil {
		return false
	}
	owner := e.schema.Types[field.Capability.OwnerType]
	if owner == nil {
		return false
	}
	declared := owner.ByName[field.Name]
	return declared != nil && declared.Record != nil && declared.Record.References != nil
}

// recordReferenceValueTargets maps each authored value of a RECORD reference
// field to its occurrence's target, matching occurrences in authored order.
func recordReferenceValueTargets(projection *ontology.NodeProjection, field ontology.NodeFieldState, indexed []readmodel.FieldValueRow) map[int]ontology.NodeRef {
	var rows []readmodel.FieldValueRow
	for _, row := range indexed {
		if strings.EqualFold(row.FieldName, field.Name) {
			rows = append(rows, row)
		}
	}
	out := map[int]ontology.NodeRef{}
	occurrences := projection.Fields[field.Name].ReferenceOccurrences
	next := 0
	for i, value := range field.Values {
		for j := next; j < len(occurrences); j++ {
			if occurrences[j].Authored != value {
				continue
			}
			if ref, ok := recordOccurrenceTarget(occurrences[j], rows); ok {
				if ref.Kind == ontology.NodeKindNote && ref.Fragment == "" && ref.NodeID == "" {
					// A document target names its note, not a captured revision of it.
					ref = ontology.NodeRef{NotePath: ref.NotePath, Kind: ontology.NodeKindNote, TypeName: ref.TypeName}
				}
				out[i] = ref
			}
			next = j + 1
			break
		}
	}
	return out
}

func (e *executor) resolveWorkspaceFieldLinks(item ontology.NodeFieldState, links fieldLinkTargets, set ast.SelectionSet, path []string) []any {
	out := make([]any, 0)
	if !e.linkField(item) {
		return out
	}
	for i, value := range item.Values {
		resolved, ok := links[fieldLinkKeyFor(item, i)]
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
