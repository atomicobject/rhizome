package ontology

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/atomicobject/rhizome/pkg/noteformat"
	ontoschema "github.com/atomicobject/rhizome/pkg/ontology/schema"
)

type RecordIdentity struct {
	Binding, Space, LogicalKind, LogicalKey string
	Positional                              bool
	SourceOrder                             int
}
type recordProjection struct {
	selection  recordSelection
	owner      *noteformat.OwnerFact
	binding    *ontoschema.RecordSourceBinding
	typ        *ontoschema.NoteType
	parent     *recordProjection
	projection *NodeProjection
	identity   RecordIdentity
	valid      bool
	depth      int
}

func recordDigest(parts ...string) string {
	var b strings.Builder
	for _, part := range parts {
		fmt.Fprintf(&b, "%d:%s", len(part), part)
	}
	return hashText(b.String())
}
func isRecordSelector(fragment string) bool {
	return strings.HasPrefix(fragment, "record:") || strings.HasPrefix(fragment, "record-pos:")
}

func (p *PreparedDocument) compileRecordProjections(doc *noteDoc) error {
	if p.structure == nil {
		return nil
	}
	names := make([]string, 0)
	for name, t := range p.schema.Types {
		if t.Record != nil && sourceMatchersMatchNote(t.SourceMatchers, doc) {
			names = append(names, name)
		}
	}
	depth := func(name string) int {
		n := 0
		for t := p.schema.Types[name]; t != nil && t.Record != nil && t.Record.Container != ""; t = p.schema.Types[t.Record.Container] {
			n++
		}
		return n
	}
	sort.Slice(names, func(i, j int) bool {
		a, b := depth(names[i]), depth(names[j])
		if a != b {
			return a < b
		}
		return names[i] < names[j]
	})
	rootOwner := p.owners[p.structure.Root]
	if rootOwner == nil || rootOwner.Value == 0 {
		return nil
	}
	byTypeValue := map[string]map[noteformat.ValueID][]*recordProjection{}
	for _, name := range names {
		typ := p.schema.Types[name]
		binding := typ.Record
		selections := p.selectValues(recordSelection{value: rootOwner.Value}, binding.Select, true)
		byTypeValue[name] = map[noteformat.ValueID][]*recordProjection{}
		ordinals := map[string]int{}
		for _, selection := range selections {
			owner := p.owners[p.ownerByValue[selection.value]]
			if owner == nil {
				return fmt.Errorf("selected record value %d has no source owner", selection.value)
			}
			record := &recordProjection{selection: selection, owner: owner, binding: binding, typ: typ, valid: true}
			value, _ := p.value(selection.value)
			if value.Kind != noteformat.MetadataObject && value.Kind != noteformat.MetadataString && value.Kind != noteformat.MetadataInteger {
				p.recordDiagnostic(record, "", "record_shape_unsupported", "record must be a mapping, string, or integer")
				continue
			}
			if value.Kind != noteformat.MetadataObject && binding.ScalarAs == "" {
				p.recordDiagnostic(record, "", "record_scalar_as_missing", "scalar row requires scalarAs")
				continue
			}

			if binding.Container != "" {
				for i := len(selection.ancestors) - 1; i >= 0; i-- {
					parents := byTypeValue[binding.Container][selection.ancestors[i]]
					if len(parents) > 1 {
						p.recordDiagnostic(record, "", "record_container_ambiguous", "multiple selected containers")
						break
					}
					if len(parents) == 1 {
						record.parent = parents[0]
						record.depth = record.parent.depth + 1
						break
					}
				}
				if record.parent == nil {
					continue
				}
			}
			parentIdentity := ""
			if record.parent != nil {
				parentIdentity = record.parent.projection.Ref.NodeID
			}
			physicalOrdinal := ordinals[parentIdentity]
			ordinals[parentIdentity]++
			if !p.membershipIncludes(record) {
				continue
			}
			key, keyKind, ok := p.recordKey(record)
			if !ok {
				p.recordDiagnostic(record, "", "record_key_missing", "record key is absent, empty, or unsupported")
				continue
			}
			bindingID := typ.Name + ":" + binding.Select.Text
			record.identity = RecordIdentity{Binding: bindingID, Space: binding.Space, LogicalKind: binding.LogicalKind, LogicalKey: key, Positional: binding.Key.Positional, SourceOrder: binding.SourceOrder}
			ref := NodeRef{NotePath: p.root.NotePath.String(), Kind: NodeKindEmbedded, TypeName: name}
			if owner.Range.Present {
				ref.StartByte = owner.Range.Range.StartByte
				ref.EndByte = owner.Range.Range.EndByte
			}
			parentID := ""
			if record.parent != nil {
				parentID = record.parent.projection.Ref.NodeID
				ref.ParentID = parentID
			}
			if binding.Key.Positional {
				ordinal := physicalOrdinal
				record.identity.LogicalKey = strconv.Itoa(ordinal + 1)
				if record.parent != nil {
					record.identity.LogicalKey = record.parent.identity.LogicalKey + "." + record.identity.LogicalKey
				}
				digest := recordDigest(ref.NotePath, bindingID, parentID, strconv.Itoa(ordinal))
				ref.NodeID = "record-pos-v1-" + digest
				ref.Fragment = "record-pos:v1:" + p.root.ContentHash + ":" + digest
				ref.Structural = recordDigest(string(p.sourceValueJSON(selection.value)))
			} else {
				digest := recordDigest(string(p.root.Format), ref.NotePath, bindingID, keyKind, key)
				ref.NodeID = "record-v1-" + digest
				ref.Fragment = "record:v1:" + digest
				ref.Structural = digest
			}
			projection := &NodeProjection{Ref: ref, Prepared: p, OwnerKey: owner.Key, RootSnapshot: p.root, ResolvedType: name, Type: typ, PropertyCase: typ.PropertyCase, Fields: map[string]FieldBinding{}, Collections: map[string]CollectionBinding{}}
			projection.sourceTitle = p.recordTitle(record)
			record.projection = projection
			projection.Record = &record.identity
			p.records = append(p.records, record)
			p.recordByID[ref.NodeID] = append(p.recordByID[ref.NodeID], record)
			byTypeValue[name][selection.value] = append(byTypeValue[name][selection.value], record)
		}
	}
	for _, records := range p.recordByID {
		if len(records) > 1 {
			for _, r := range records {
				r.valid = false
				p.recordDiagnostic(r, "", "record_key_duplicate", "physical record key is ambiguous")
			}
		}
	}
	// A descendant's identity and semantic parent derive from its container,
	// so an invalid container invalidates its whole subtree. Records are in
	// container-depth order, so each parent is settled before its children.
	for _, r := range p.records {
		if r.valid && r.parent != nil && !r.parent.valid {
			r.valid = false
			p.recordDiagnostic(r, "", "record_container_invalid", "selected container has no valid identity")
		}
	}
	for _, record := range p.records {
		for _, field := range record.typ.Fields {
			if field.Record != nil {
				record.projection.Fields[field.Name] = p.recordFieldBinding(record, field)
			}
			if field.RecordContains != nil {
				selected := p.selectValues(record.selection, field.RecordContains.Select, true)
				binding := FieldBinding{FieldName: field.Name, Kind: BindingKindSectionList, Presence: SourceMissing}
				for _, selection := range selected {
					for _, child := range byTypeValue[field.TypeName][selection.value] {
						if child.valid && child.parent == record {
							binding.ChildRefs = append(binding.ChildRefs, child.projection.Ref)
							binding.Values = append(binding.Values, child.projection.Ref.String())
						}
					}
				}
				binding.Present = len(p.selectValues(record.selection, field.RecordContains.Select, false)) > 0
				if binding.Present {
					binding.Presence = SourcePresent
				}
				record.projection.Fields[field.Name] = binding
				if field.List {
					record.projection.Collections[field.Name] = collectionFromFieldBinding(field.Name, binding)
				}
			}
		}
	}
	if p.rootProjection.Type != nil {
		for _, field := range p.rootProjection.Type.Fields {
			if field.RecordContains == nil {
				continue
			}
			binding := FieldBinding{FieldName: field.Name, Kind: BindingKindSectionList, Presence: SourceMissing}
			for _, selection := range p.selectValues(recordSelection{value: rootOwner.Value}, field.RecordContains.Select, true) {
				for _, child := range byTypeValue[field.TypeName][selection.value] {
					if child.valid && child.parent == nil {
						binding.ChildRefs = append(binding.ChildRefs, child.projection.Ref)
						binding.Values = append(binding.Values, child.projection.Ref.String())
					}
				}
			}
			binding.Present = len(p.selectValues(recordSelection{value: rootOwner.Value}, field.RecordContains.Select, false)) > 0
			if binding.Present {
				binding.Presence = SourcePresent
			}
			p.rootProjection.Fields[field.Name] = binding
			if field.List {
				p.rootProjection.Collections[field.Name] = collectionFromFieldBinding(field.Name, binding)
			}
		}
	}
	return nil
}

func (p *PreparedDocument) recordDiagnostic(r *recordProjection, field, code, message string) {
	p.diagnostics = append(p.diagnostics, RecordDiagnostic{Code: code, Message: message, Owner: r.owner.Key, Address: r.owner.Address, TypeName: r.typ.Name, FieldName: field})
}

func (p *PreparedDocument) recordForRef(ref NodeRef) (*recordProjection, error) {
	id := ref.NodeID
	if strings.HasPrefix(ref.Fragment, "record-pos:") {
		parts := strings.Split(ref.Fragment, ":")
		if len(parts) != 4 || parts[1] != "v1" {
			return nil, fmt.Errorf("record_locator_invalid")
		}
		if parts[2] != p.root.ContentHash {
			return nil, fmt.Errorf("record_locator_stale")
		}
		id = "record-pos-v1-" + parts[3]
	}
	if strings.HasPrefix(ref.Fragment, "record:") {
		parts := strings.Split(ref.Fragment, ":")
		if len(parts) != 3 || parts[1] != "v1" {
			return nil, fmt.Errorf("record_locator_invalid")
		}
		id = "record-v1-" + parts[2]
	}
	records := p.recordByID[id]
	if len(records) == 0 {
		return nil, fmt.Errorf("record_locator_unresolved")
	}
	if len(records) > 1 {
		return nil, fmt.Errorf("record_key_ambiguous")
	}
	// An invalid record (for example under a duplicate container) has no
	// catalog identity, so a saved locator must not reopen it.
	if !records[0].valid {
		return nil, fmt.Errorf("record_locator_unresolved")
	}
	return records[0], nil
}
func (p *PreparedDocument) projectRecord(ref NodeRef, bound bool) (*NodeProjection, error) {
	record, err := p.recordForRef(ref)
	if err != nil {
		return nil, err
	}
	current := record.projection.Ref
	if ref.NodeID != "" && ref.NodeID != current.NodeID {
		return nil, fmt.Errorf("record_identity_mismatch")
	}
	if bound && ref != current {
		return nil, fmt.Errorf("record ref was not produced by this prepared document")
	}
	if record.identity.Positional && ref.Structural != "" && ref.Structural != current.Structural {
		return nil, fmt.Errorf("record_identity_changed")
	}
	if ref.TypeName != "" && ref.TypeName != current.TypeName {
		return nil, fmt.Errorf("record_type_mismatch")
	}
	return record.projection, nil
}

func IsReservedNodeSelector(fragment string) bool {
	return strings.HasPrefix(fragment, "struct:") || strings.HasPrefix(fragment, "node:") || isRecordSelector(fragment)
}
func IsRecordSelector(fragment string) bool { return isRecordSelector(fragment) }
