package validate

import (
	"testing"

	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/ontology/reference"
	"github.com/atomicobject/rhizome/pkg/validate/identifierreconcile"
	"github.com/stretchr/testify/require"
)

func TestIdentifierAliasPhaseUsesExactCompletedOwnerTransition(t *testing.T) {
	owner := ontology.NodeRef{NotePath: "specs/ITEM-0001-plan.md", Fragment: "^ITEM-0001-US1", TypeName: "Story", Kind: ontology.NodeKindEmbedded, NodeID: "old-parser-node"}
	old := owner
	old.NodeID = ""
	next := old
	next.NotePath = "specs/ITEM-0002-plan.md"
	next.Fragment = "^ITEM-0002-US1"
	preferred := reference.StructuredFieldEdit{OwnerRef: owner, FieldName: "id", Kind: reference.StructuredFieldPreferredIdentifier, Operation: reference.StructuredFieldEditReplace, TargetOldRef: old, TargetNewRef: next}
	primary := []OntologyEdit{{Ref: owner, Field: "id", Kind: OntologyEditSetScalar, Value: "ITEM-0002-US1"}}
	sibling := old
	sibling.Fragment = "^ITEM-0001-US10"
	inbound := ontology.NodeRef{NotePath: "notes/inbound.md", TypeName: "Consumer", Kind: ontology.NodeKindNote}
	otherType := old
	otherType.TypeName = "Other"
	aliases := []OntologyEdit{{Ref: owner, Field: "aliases"}, {Ref: sibling, Field: "aliases"}, {Ref: inbound, Field: "aliases"}, {Ref: otherType, Field: "aliases"}}
	fields := map[string]identifierreconcile.FieldRepairIntent{
		"preferred": {Edit: preferred},
		"inbound":   {Edit: reference.StructuredFieldEdit{OwnerRef: inbound, FieldName: "related", Kind: reference.StructuredFieldTypedIdentifierReference, Operation: reference.StructuredFieldEditReplace, TargetOldRef: old, TargetNewRef: next}},
	}
	mapped, err := remapIdentifierAliasOwners(aliases, primary, fields)
	require.NoError(t, err)
	current := next
	current.NotePath = owner.NotePath
	require.Equal(t, current, mapped[0].Ref, "advance only the fragment while the governed rename is pending")
	require.Equal(t, aliases[1:], mapped[1:], "similar siblings, unrelated inbound owners and other types must remain unchanged")
	require.Equal(t, owner, aliases[0].Ref, "phase mapping must not mutate its caller's edits")
	unmapped, err := remapIdentifierAliasOwners(aliases, nil, fields)
	require.NoError(t, err)
	require.Equal(t, aliases, unmapped, "a sealed target alone cannot authorize a transition that did not run")

	for _, tc := range []struct {
		name  string
		edit  reference.StructuredFieldEdit
		extra bool
		want  string
	}{
		{"mismatched old owner", func() reference.StructuredFieldEdit { e := preferred; e.TargetOldRef = sibling; return e }(), false, "does not match its sealed owner"},
		{"changed type", func() reference.StructuredFieldEdit { e := preferred; e.TargetNewRef.TypeName = "Other"; return e }(), false, "does not match its sealed owner"},
		{"changed kind", func() reference.StructuredFieldEdit {
			e := preferred
			e.TargetNewRef.Kind = ontology.NodeKindNote
			return e
		}(), false, "does not match its sealed owner"},
		{"missing destination", func() reference.StructuredFieldEdit { e := preferred; e.TargetNewRef.NotePath = ""; return e }(), false, "does not match its sealed owner"},
		{"conflicting owner transition", func() reference.StructuredFieldEdit {
			e := preferred
			e.FieldName = "otherId"
			e.TargetNewRef.Fragment = "^DIFFERENT"
			return e
		}(), true, "conflicting identifier phase transitions"},
		{"conflicting future filename", func() reference.StructuredFieldEdit {
			e := preferred
			e.FieldName = "otherId"
			e.TargetNewRef.NotePath = "specs/elsewhere.md"
			return e
		}(), true, "conflicting identifier phase transitions"},
		{"nonunique new owner", func() reference.StructuredFieldEdit {
			e := preferred
			e.OwnerRef = sibling
			e.TargetOldRef = sibling
			return e
		}(), true, "nonunique identifier phase transition"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			candidate := map[string]identifierreconcile.FieldRepairIntent{"bad": {Edit: tc.edit}}
			applied := []OntologyEdit{{Ref: tc.edit.OwnerRef, Field: tc.edit.FieldName}}
			if tc.extra {
				candidate["original"] = identifierreconcile.FieldRepairIntent{Edit: preferred}
				applied = append(applied, primary...)
			}
			_, err := remapIdentifierAliasOwners(aliases, applied, candidate)
			require.ErrorContains(t, err, tc.want)
		})
	}
}
