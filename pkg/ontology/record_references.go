package ontology

import (
	"sort"
	"strconv"
	"strings"

	"github.com/atomicobject/rhizome/pkg/noteformat"
	ontoschema "github.com/atomicobject/rhizome/pkg/ontology/schema"
)

type RecordReferenceOccurrence struct {
	Ordinal    int
	Value      noteformat.ValueID
	Authored   string
	At         string
	Target     *NodeRef
	Diagnostic string
}
type recordReferenceValue struct {
	value  noteformat.ValueID
	scalar *noteformat.MetadataValue
}
type recordReferenceCandidate struct {
	record   *recordProjection
	document *PreparedDocument
}

// BindRecordDocuments composes only declarative membership/source order and
// logical lookup. The starter remains responsible for AO effective-file merges.
func BindRecordDocuments(documents ...*PreparedDocument) {
	var records []recordReferenceCandidate
	for _, p := range documents {
		if p == nil {
			continue
		}
		for _, r := range p.records {
			if r.valid {
				records = append(records, recordReferenceCandidate{r, p})
			}
		}
	}
	sort.SliceStable(records, func(i, j int) bool {
		a, b := records[i], records[j]
		if a.record.depth != b.record.depth {
			return a.record.depth < b.record.depth
		}
		if a.record.identity.SourceOrder != b.record.identity.SourceOrder {
			return a.record.identity.SourceOrder < b.record.identity.SourceOrder
		}
		if a.document.root.NotePath != b.document.root.NotePath {
			return a.document.root.NotePath < b.document.root.NotePath
		}
		return a.record.owner.Order < b.record.owner.Order
	})
	positions := map[string]int{}
	for _, candidate := range records {
		r := candidate.record
		if r.identity.Positional {
			parent := ""
			if r.parent != nil {
				parent = r.parent.identity.LogicalKey
			}
			group := recordDigest(r.identity.Space, r.identity.LogicalKind, parent)
			positions[group]++
			r.identity.LogicalKey = strconv.Itoa(positions[group])
			if parent != "" {
				r.identity.LogicalKey = parent + "." + r.identity.LogicalKey
			}
		}
		r.projection.sourceTitle = candidate.document.recordTitle(r)
	}
	index := buildRecordReferenceIndex(records)
	index.addMarkdownTargets(documents)
	for _, p := range documents {
		if p == nil {
			continue
		}
		p.rebindRecordReferences(index)
	}
}

func (p *PreparedDocument) referenceValues(id noteformat.ValueID, mode string) []recordReferenceValue {
	value, _ := p.value(id)
	if mode == "BY_SHAPE" {
		switch value.Kind {
		case noteformat.MetadataArray:
			mode = "ELEMENTS"
		case noteformat.MetadataObject:
			mode = "MAPPING_KEYS"
		default:
			mode = "VALUE"
		}
	}
	switch mode {
	case "ELEMENTS":
		if value.Kind != noteformat.MetadataArray {
			p.recordShapeObservation(id, mode, "sequence")
			return nil
		}
		var out []recordReferenceValue
		for _, item := range value.Items {
			v, _ := p.value(item)
			out = append(out, recordReferenceValue{item, v.Scalar})
		}
		return out
	case "MAPPING_KEYS":
		if value.Kind != noteformat.MetadataObject {
			p.recordShapeObservation(id, mode, "mapping")
			return nil
		}
		var out []recordReferenceValue
		for _, entry := range value.Entries {
			key := entry.Key
			out = append(out, recordReferenceValue{id, &key})
		}
		return out
	default:
		return []recordReferenceValue{{id, value.Scalar}}
	}
}

func (p *PreparedDocument) bindRecordReferences(record *recordProjection, field *ontoschema.Field, index *recordReferenceIndex) {
	rule := field.Record.References
	binding := record.projection.Fields[field.Name]
	binding.ReferenceOccurrences = nil
	binding.ReferenceTargets = nil
	var values []recordReferenceValue
	for _, source := range binding.SourceValues {
		values = append(values, p.referenceValues(source.Value, rule.Values)...)
	}
	for i, value := range values {
		occurrence := RecordReferenceOccurrence{Ordinal: i, Value: value.value}
		if value.scalar != nil {
			occurrence.Authored = sourceScalarText(*value.scalar)
		}
		reject := func(code, message string) {
			occurrence.Diagnostic = code
			p.recordDiagnostic(record, field.Name, code, message)
		}
		if value.scalar == nil || occurrence.Authored == "" || value.scalar.Kind() == noteformat.MetadataNull {
			if rule.Mode == "STRICT" {
				reject("record_reference_invalid", "reference requires a scalar target")
			}
			binding.ReferenceOccurrences = append(binding.ReferenceOccurrences, occurrence)
			continue
		}
		if rule.AllowWildcard && occurrence.Authored == "*" {
			binding.ReferenceOccurrences = append(binding.ReferenceOccurrences, occurrence)
			continue
		}
		id := occurrence.Authored
		allowed := rule.To
		member := ontoschema.RecordSelector{}
		if rule.Encoding == "TYPED" {
			tag, rest, ok := strings.Cut(id, ":")
			id = rest
			found := false
			if ok {
				for _, target := range rule.Targets {
					if target.Tag == tag {
						allowed = []string{target.Type}
						member = target.MemberPath
						found = true
						break
					}
				}
			}
			if !found {
				reject("record_reference_type", "unknown or missing typed reference tag")
				binding.ReferenceOccurrences = append(binding.ReferenceOccurrences, occurrence)
				continue
			}
		}
		if member.Text != "" {
			var ok bool
			id, occurrence.At, ok = strings.Cut(id, ".")
			if !ok || occurrence.At == "" {
				reject("record_reference_member", "typed member reference needs a suffix")
				binding.ReferenceOccurrences = append(binding.ReferenceOccurrences, occurrence)
				continue
			}
		}
		matches := index.resolve(record.identity.Space, allowed, id, rule.MatchPath)
		if len(matches) == 0 && member.Text == "" {
			physical := index.physicalTargets(allowed, id, rule.TargetKeys)
			if len(physical) == 1 {
				ref := physical[0]
				occurrence.Target = &ref
				binding.ReferenceTargets = append(binding.ReferenceTargets, ref)
				binding.ReferenceOccurrences = append(binding.ReferenceOccurrences, occurrence)
				continue
			}
			if len(physical) > 1 {
				reject("record_reference_ambiguous", "reference matches multiple physical records")
				binding.ReferenceOccurrences = append(binding.ReferenceOccurrences, occurrence)
				continue
			}
		}
		if len(matches) > 1 {
			reject("record_reference_ambiguous", "reference matches multiple logical records")
		} else if len(matches) == 0 {
			if rule.Mode == "STRICT" {
				reject("record_reference_unresolved", "reference target is absent")
			}
		} else {
			target := matches[0]
			valid := true
			if member.Text != "" {
				members := target.document.recordProperty(target.record, member)
				declared := false
				found := false
				first, _, _ := strings.Cut(occurrence.At, ".")
				for _, m := range members {
					v, _ := target.document.value(m.value)
					if v.Scalar == nil {
						continue
					}
					declared = true
					if sourceScalarText(*v.Scalar) == first {
						found = true
					}
				}
				if declared && !found {
					valid = false
					reject("record_reference_member", "unknown declared member")
				}
			}
			if valid {
				ref := target.record.projection.Ref
				occurrence.Target = &ref
				binding.ReferenceTargets = append(binding.ReferenceTargets, ref)
			}
		}
		binding.ReferenceOccurrences = append(binding.ReferenceOccurrences, occurrence)
	}
	record.projection.Fields[field.Name] = binding
}

func (p *PreparedDocument) rebindRecordReferences(index *recordReferenceIndex) {
	retained := p.diagnostics[:0]
	for _, d := range p.diagnostics {
		if !strings.HasPrefix(d.Code, "record_reference_") {
			retained = append(retained, d)
		}
	}
	p.diagnostics = retained
	for _, r := range p.records {
		if !r.valid {
			continue
		}
		for _, field := range r.typ.Fields {
			if field.Record == nil || field.Record.References == nil {
				continue
			}
			p.bindRecordReferences(r, field, index)
		}
	}
}
