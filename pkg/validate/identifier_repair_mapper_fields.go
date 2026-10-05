package validate

import (
	"context"
	"fmt"
	"sort"

	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/ontology/reference"
	"github.com/atomicobject/rhizome/pkg/validate/identifierreconcile"
)

func identifierOntologyEdits(
	ctx context.Context,
	runCtx RunContext,
	schema *ontology.Schema,
	original, overlay []byte,
	fields map[string]identifierreconcile.FieldRepairIntent,
	links []identifierreconcile.LinkRepairIntent,
) ([]OntologyEdit, error) {
	byField := make(map[string][]identifierreconcile.FieldRepairIntent)
	for _, intent := range fields {
		key := identifierOntologyFieldKey(intent.Edit.OwnerRef, intent.Edit.FieldName)
		byField[key] = append(byField[key], intent)
	}
	keys := make([]string, 0, len(byField))
	for key := range byField {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	sort.SliceStable(keys, func(i, j int) bool {
		return identifierFieldEditGroupRank(byField[keys[i]]) < identifierFieldEditGroupRank(byField[keys[j]])
	})
	overlayReader := &identifierOverlayReader{delegate: runCtx.NoteReader, content: map[string]string{byField[keys[0]][0].Edit.OwnerRef.NotePath: string(overlay)}}
	var out []OntologyEdit
	for _, key := range keys {
		intents := byField[key]
		ref, fieldName := intents[0].Edit.OwnerRef, intents[0].Edit.FieldName
		projection, err := ontology.ProjectNode(ctx, runCtx.VaultDef, overlayReader, schema, ref)
		if err != nil {
			return nil, err
		}
		if projection.Type == nil && ref.TypeName != "" {
			recovered, recoveryErr := ontology.ProjectNodeFromSnapshotWithSelectorRecovery(projection.Snapshot, schema, ref)
			if recoveryErr != nil {
				return nil, recoveryErr
			}
			if recovered.Type == nil {
				return nil, fmt.Errorf("identifier field edit %s cannot recover its selector-matched note projection", ref.String())
			}
			projection = recovered
		}
		field := identifierProjectionField(projection, fieldName)
		var binding ontology.FieldBinding
		if field != nil {
			binding = projection.Fields[fieldName]
		} else {
			binding = ontology.ProjectRawFrontmatterFieldBinding(projection.Snapshot, fieldName)
		}
		values := append([]string(nil), binding.Values...)
		sort.Slice(intents, func(i, j int) bool { return intents[i].Edit.Range.Start < intents[j].Edit.Range.Start })
		materialSemanticEdit := false
		for _, intent := range intents {
			edit := intent.Edit
			if edit.Operation != reference.StructuredFieldEditAppend {
				if !edit.Range.Valid(len(original)) || string(original[edit.Range.Start:edit.Range.End]) != edit.Expected {
					return nil, fmt.Errorf("identifier field edit %s.%s is stale", ref.String(), fieldName)
				}
			}
			switch edit.Operation {
			case reference.StructuredFieldEditAppend:
				values = append(values, edit.Replacement)
				materialSemanticEdit = true
			case reference.StructuredFieldEditReplace, reference.StructuredFieldEditRemove:
				if edit.Operation == reference.StructuredFieldEditReplace &&
					indexIdentifierFieldValue(field, values, edit.Replacement) >= 0 &&
					identifierFieldTransitionAppliedByLinks(original, edit, links) {
					continue
				}
				index := indexIdentifierFieldValue(field, values, edit.Expected)
				if index < 0 {
					return nil, fmt.Errorf("identifier field %s.%s no longer contains %q", ref.String(), fieldName, edit.Expected)
				}
				if edit.Operation == reference.StructuredFieldEditReplace {
					values[index] = edit.Replacement
				} else {
					values = append(values[:index], values[index+1:]...)
				}
				materialSemanticEdit = true
			default:
				return nil, fmt.Errorf("unsupported identifier field operation %q", edit.Operation)
			}
		}
		if !materialSemanticEdit {
			continue
		}
		switch {
		case field == nil:
			out = append(out, OntologyEdit{Kind: OntologyEditSetRawFrontmatterList, Ref: ref, Field: fieldName, Values: values})
		case field.Kind == ontology.FieldKindLink:
			out = append(out, OntologyEdit{Kind: OntologyEditSetLink, Ref: ref, Field: fieldName, Values: values})
		case field.List:
			out = append(out, OntologyEdit{Kind: OntologyEditSetScalarList, Ref: ref, Field: fieldName, Values: values})
		case len(values) == 1:
			out = append(out, OntologyEdit{Kind: OntologyEditSetScalar, Ref: ref, Field: fieldName, Value: values[0]})
		default:
			return nil, fmt.Errorf("identifier scalar field %s.%s has %d values", ref.String(), fieldName, len(values))
		}
	}
	return out, nil
}

func identifierOntologyFieldKey(ref ontology.NodeRef, field string) string {
	return identifierJSONKey(struct {
		NotePath string
		Fragment string
		Field    string
	}{ref.NotePath, ref.Fragment, field})
}

func identifierFieldEditGroupRank(intents []identifierreconcile.FieldRepairIntent) int {
	for _, intent := range intents {
		switch intent.Edit.Kind {
		case reference.StructuredFieldPreferredIdentifier:
			return 0
		case reference.StructuredFieldAliasIdentifier:
			return 2
		}
	}
	return 1
}

func identifierFieldTransitionAppliedByLinks(
	original []byte,
	field reference.StructuredFieldEdit,
	links []identifierreconcile.LinkRepairIntent,
) bool {
	if !field.Range.Valid(len(original)) || string(original[field.Range.Start:field.Range.End]) != field.Expected {
		return false
	}
	transformed := append([]byte(nil), original[field.Range.Start:field.Range.End]...)
	usedLink := false
	for _, intent := range links {
		link := intent.Edit
		if link.Range.End <= field.Range.Start || link.Range.Start >= field.Range.End {
			continue
		}
		if link.Range.Start < field.Range.Start || link.Range.End > field.Range.End ||
			!link.Range.Valid(len(original)) || string(original[link.Range.Start:link.Range.End]) != link.Expected {
			return false
		}
		start := link.Range.Start - field.Range.Start
		end := link.Range.End - field.Range.Start
		transformed = append(append(append([]byte(nil), transformed[:start]...), []byte(link.Replacement)...), transformed[end:]...)
		usedLink = true
	}
	return usedLink && string(transformed) == field.Replacement
}
