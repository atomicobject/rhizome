package validate

import (
	"fmt"
	"strings"

	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/ontology/reference"
	"github.com/atomicobject/rhizome/pkg/validate/identifierreconcile"
)

// remapIdentifierAliasOwners carries sealed owner transitions across a
// successful primary preview. Governed filename moves have not run yet.
func remapIdentifierAliasOwners(aliases, primary []OntologyEdit, fields map[string]identifierreconcile.FieldRepairIntent) ([]OntologyEdit, error) {
	applied := make(map[string]struct{}, len(primary))
	for _, edit := range primary {
		applied[identifierPhaseOwnerKey(edit.Ref)+"\x00"+edit.Field] = struct{}{}
	}
	transitions := make(map[string]ontology.NodeRef)
	futureTargets := make(map[string]string)
	ownersByNewRef := make(map[string]string)
	for _, intent := range fields {
		edit := intent.Edit
		if edit.Kind != reference.StructuredFieldPreferredIdentifier || edit.Operation != reference.StructuredFieldEditReplace {
			continue
		}
		oldKey := identifierPhaseOwnerKey(edit.OwnerRef)
		if _, ok := applied[oldKey+"\x00"+edit.FieldName]; !ok {
			continue
		}
		if oldKey != identifierPhaseOwnerKey(edit.TargetOldRef) || edit.OwnerRef.TypeName != edit.TargetNewRef.TypeName || edit.OwnerRef.Kind != edit.TargetNewRef.Kind || edit.TargetNewRef.NotePath == "" {
			return nil, fmt.Errorf("identifier phase transition does not match its sealed owner %s", edit.OwnerRef.String())
		}
		current := ontology.NodeRef{NotePath: edit.OwnerRef.NotePath, Fragment: edit.TargetNewRef.Fragment, TypeName: edit.TargetNewRef.TypeName, Kind: edit.TargetNewRef.Kind}
		futureKey := identifierPhaseOwnerKey(edit.TargetNewRef)
		if previous, ok := transitions[oldKey]; ok && (previous != current || futureTargets[oldKey] != futureKey) {
			return nil, fmt.Errorf("conflicting identifier phase transitions for %s", edit.OwnerRef.String())
		}
		newKey := identifierPhaseOwnerKey(current)
		if previous, ok := ownersByNewRef[newKey]; ok && previous != oldKey {
			return nil, fmt.Errorf("nonunique identifier phase transition to %s", current.String())
		}
		transitions[oldKey], ownersByNewRef[newKey] = current, oldKey
		futureTargets[oldKey] = futureKey
	}
	out := append([]OntologyEdit(nil), aliases...)
	for index := range out {
		if current, ok := transitions[identifierPhaseOwnerKey(out[index].Ref)]; ok {
			out[index].Ref = current
		}
	}
	return out, nil
}

func identifierPhaseOwnerKey(ref ontology.NodeRef) string {
	return strings.Join([]string{ref.NotePath, ref.Fragment, ref.TypeName, string(ref.Kind)}, "\x00")
}
