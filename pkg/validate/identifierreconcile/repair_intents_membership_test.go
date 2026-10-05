package identifierreconcile

import (
	"testing"

	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/ontology/reference"
	"github.com/stretchr/testify/require"
)

func TestUnionIdenticalIntentMembershipsPreservesEveryCollisionSeed(t *testing.T) {
	shared := reference.StructuredLinkEdit{NotePath: "docs/shared.md", LinkIndex: 0, Component: reference.LinkComponentPath, Range: ontology.ByteRange{Start: 2, End: 11}, Expected: "SPEC-0001", Replacement: "SPEC-0002"}
	sharedField := reference.StructuredFieldEdit{OwnerRef: repairRef("docs/shared.md", "", "Plan"), FieldName: "spec", Kind: reference.StructuredFieldTypedIdentifierReference, Operation: reference.StructuredFieldEditReplace, Range: ontology.ByteRange{Start: 20, End: 29}, Expected: "SPEC-0001", Replacement: "SPEC-0002"}
	reviewEvidence := reference.IdentifierRewriteDiagnostic{Kind: reference.IdentifierRewriteDiagnosticReviewOnly, OwnerRef: repairRef("docs/shared.md", "", "Plan"), Range: ontology.ByteRange{Start: 40, End: 49}, Value: "SPEC-0001", Message: "review only"}
	sharedDiagnostic := RepairDiagnostic{Kind: string(reference.IdentifierRewriteDiagnosticReviewOnly), Blocking: false, Field: &reviewEvidence}
	components := []CollisionRepairIntent{
		{MembershipKeys: []string{"collision-a"}, FieldEdits: []FieldRepairIntent{{MembershipKeys: []string{"collision-a"}, Edit: sharedField}}, LinkEdits: []LinkRepairIntent{{MembershipKeys: []string{"collision-a"}, Edit: shared}}, Diagnostics: []RepairDiagnostic{sharedDiagnostic}},
		{MembershipKeys: []string{"collision-b"}, FieldEdits: []FieldRepairIntent{{MembershipKeys: []string{"collision-b"}, Edit: sharedField}}, LinkEdits: []LinkRepairIntent{{MembershipKeys: []string{"collision-b"}, Edit: shared}}, Diagnostics: []RepairDiagnostic{sharedDiagnostic}},
	}

	unionIdenticalIntentMemberships(components)
	require.Equal(t, []string{"collision-a", "collision-b"}, components[0].LinkEdits[0].MembershipKeys)
	require.Equal(t, []string{"collision-a", "collision-b"}, components[1].LinkEdits[0].MembershipKeys)
	require.Equal(t, []string{"collision-a", "collision-b"}, components[0].FieldEdits[0].MembershipKeys)
	require.Equal(t, []string{"collision-a", "collision-b"}, components[1].FieldEdits[0].MembershipKeys)
	require.Equal(t, []string{"collision-a", "collision-b"}, components[0].Diagnostics[0].MembershipKeys)
	require.Equal(t, []string{"collision-a", "collision-b"}, components[1].Diagnostics[0].MembershipKeys)
}
