package ontology

import (
	"fmt"
	"strings"

	"github.com/atomicobject/rhizome/pkg/noteformat"
	ontoschema "github.com/atomicobject/rhizome/pkg/ontology/schema"
)

type recordSelection struct {
	value      noteformat.ValueID
	mappingKey *noteformat.MetadataValue
	ancestors  []noteformat.ValueID
}

func sourceScalarText(v noteformat.MetadataValue) string {
	switch value := v.Export().(type) {
	case string:
		return value
	case nil:
		return ""
	default:
		return fmt.Sprint(value)
	}
}
func (p *PreparedDocument) value(id noteformat.ValueID) (noteformat.SourceValueFact, bool) {
	if p.structure == nil || id == 0 || int(id) > len(p.structure.Values) {
		return noteformat.SourceValueFact{}, false
	}
	return p.structure.Values[id-1], true
}
func (p *PreparedDocument) property(id noteformat.ValueID, key string) (noteformat.ValueID, bool) {
	values := p.properties[id]
	if value, ok := values[key]; ok {
		return value, true
	}
	value, ok := p.foldedProperties[id][strings.ToLower(key)]
	return value, ok
}

func (p *PreparedDocument) selectValues(start recordSelection, selector ontoschema.RecordSelector, terminal bool) []recordSelection {
	current := []recordSelection{start}
	for _, step := range selector.Steps {
		var next []recordSelection
		for _, selection := range current {
			parent, _ := p.value(selection.value)
			if parent.Kind != noteformat.MetadataObject {
				p.recordShapeObservation(selection.value, step.Name, "mapping")
				continue
			}
			id, ok := p.property(selection.value, step.Name)
			if !ok {
				continue
			}
			value, ok := p.value(id)
			if !ok {
				continue
			}
			base := selection
			base.value = id
			base.ancestors = append(append([]noteformat.ValueID(nil), selection.ancestors...), selection.value)
			switch step.Expansion {
			case "":
				next = append(next, base)
			case ontoschema.RecordExpandSequence:
				if value.Kind != noteformat.MetadataArray {
					p.recordShapeObservation(id, step.Name, "sequence")
					continue
				}
				for _, item := range value.Items {
					row := base
					row.value = item
					row.ancestors = append(append([]noteformat.ValueID(nil), base.ancestors...), id)
					next = append(next, row)
				}
			case ontoschema.RecordExpandMapping, ontoschema.RecordExpandMappingSequence:
				if value.Kind != noteformat.MetadataObject {
					p.recordShapeObservation(id, step.Name, "mapping")
					continue
				}
				for _, entry := range value.Entries {
					row := base
					row.value = entry.Value
					row.ancestors = append(append([]noteformat.ValueID(nil), base.ancestors...), id)
					if row.mappingKey == nil {
						key := entry.Key
						row.mappingKey = &key
					}
					if step.Expansion == ontoschema.RecordExpandMapping {
						next = append(next, row)
					} else if sequence, ok := p.value(entry.Value); ok && sequence.Kind == noteformat.MetadataArray {
						for _, item := range sequence.Items {
							leaf := row
							leaf.value = item
							leaf.ancestors = append(append([]noteformat.ValueID(nil), row.ancestors...), row.value)
							next = append(next, leaf)
						}
					} else {
						p.recordShapeObservation(entry.Value, step.Name, "sequence")
					}
				}
			}
		}
		current = next
	}
	if !terminal {
		return current
	}
	var out []recordSelection
	for _, selection := range current {
		value, _ := p.value(selection.value)
		if value.Kind == noteformat.MetadataArray {
			for _, item := range value.Items {
				row := selection
				row.value = item
				row.ancestors = append(append([]noteformat.ValueID(nil), selection.ancestors...), selection.value)
				out = append(out, row)
			}
		} else if value.Kind != noteformat.MetadataNull {
			out = append(out, selection)
		}
	}
	return out
}

func (p *PreparedDocument) recordProperty(record *recordProjection, path ontoschema.RecordSelector) []recordSelection {
	if len(path.Steps) == 1 && path.Steps[0].Name == record.binding.ScalarAs {
		value, _ := p.value(record.selection.value)
		if value.Kind == noteformat.MetadataString || value.Kind == noteformat.MetadataInteger {
			return []recordSelection{record.selection}
		}
	}
	return p.selectValues(record.selection, path, false)
}
func (p *PreparedDocument) scalarAt(record *recordProjection, selector ontoschema.RecordSelector) (string, bool) {
	selections := p.recordProperty(record, selector)
	if len(selections) != 1 {
		return "", false
	}
	value, ok := p.value(selections[0].value)
	if !ok || value.Scalar == nil || value.Kind == noteformat.MetadataNull {
		return "", false
	}
	return sourceScalarText(*value.Scalar), true
}
func (p *PreparedDocument) recordKey(record *recordProjection) (string, string, bool) {
	key := record.binding.Key
	if key.Positional {
		return "", "positional", true
	}
	if key.MappingKey {
		if record.selection.mappingKey == nil {
			return "", "", false
		}
		value := *record.selection.mappingKey
		if value.Kind() != noteformat.MetadataString && value.Kind() != noteformat.MetadataInteger {
			return "", "", false
		}
		text := sourceScalarText(value)
		return text, string(value.Kind()), text != ""
	}
	if len(key.Template) > 0 {
		var out strings.Builder
		for _, part := range key.Template {
			out.WriteString(part.Literal)
			if part.Field != "" {
				value, ok := p.scalarAt(record, ontoschema.RecordSelector{Steps: []ontoschema.RecordSelectorStep{{Name: part.Field}}})
				if !ok || value == "" {
					return "", "", false
				}
				out.WriteString(value)
			}
		}
		text := out.String()
		return text, "template", text != ""
	}
	path := ontoschema.RecordSelector{Steps: []ontoschema.RecordSelectorStep{{Name: key.Field}}}
	selections := p.recordProperty(record, path)
	if len(selections) != 1 {
		return "", "", false
	}
	value, _ := p.value(selections[0].value)
	if value.Scalar == nil || (value.Kind != noteformat.MetadataString && value.Kind != noteformat.MetadataInteger) {
		return "", "", false
	}
	text := sourceScalarText(*value.Scalar)
	return text, string(value.Kind), text != ""
}
func (p *PreparedDocument) membershipIncludes(record *recordProjection) bool {
	rule := record.binding.Membership
	if rule == nil {
		return true
	}
	selections := p.recordProperty(record, rule.Path)
	if len(selections) == 0 {
		return rule.IncludeMissing
	}
	for _, s := range selections {
		value, _ := p.value(s.value)
		if value.Scalar != nil && sourceScalarText(*value.Scalar) == rule.Includes {
			return true
		}
		for _, id := range value.Items {
			item, _ := p.value(id)
			if item.Scalar != nil && sourceScalarText(*item.Scalar) == rule.Includes {
				return true
			}
		}
	}
	return false
}

func (p *PreparedDocument) recordFieldBinding(record *recordProjection, field *ontoschema.Field) FieldBinding {
	binding := FieldBinding{FieldName: field.Name, Kind: BindingKindSourceField, SourceKind: ontoschema.FieldSourceProperty, Presence: SourceMissing}
	selections := p.recordProperty(record, field.Record.Path)
	if len(selections) == 0 && field.Record.MappingKeyFallback && record.selection.mappingKey != nil {
		binding.Present = true
		binding.Presence = SourcePresent
		binding.ValueKind = record.selection.mappingKey.Kind()
		binding.Values = []string{sourceScalarText(*record.selection.mappingKey)}
		return binding
	}
	for _, selection := range selections {
		value, _ := p.value(selection.value)
		binding.Present = true
		binding.Presence = SourcePresent
		binding.ValueKind = value.Kind
		if value.Kind == noteformat.MetadataNull && len(selections) == 1 {
			binding.Presence = SourceNull
		}
		owner := p.owners[p.ownerByValue[selection.value]]
		address := ""
		if owner != nil {
			address = owner.Address
		}
		binding.SourceValues = append(binding.SourceValues, SourceValueOccurrence{Value: selection.value, Address: address})
		if value.Scalar != nil && value.Kind != noteformat.MetadataNull {
			binding.Values = append(binding.Values, sourceScalarText(*value.Scalar))
		}
		if value.Kind == noteformat.MetadataArray {
			for _, id := range value.Items {
				item, _ := p.value(id)
				if item.Scalar != nil && item.Kind != noteformat.MetadataNull {
					binding.Values = append(binding.Values, sourceScalarText(*item.Scalar))
				}
			}
		}
	}
	return binding
}
