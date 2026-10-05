package noteformat

import (
	"testing"

	"github.com/atomicobject/rhizome/pkg/paths"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValidateMetadataPatchPlanChecksExactSourceAndOverlap(t *testing.T) {
	descriptor := Descriptor{ID: "test", Extensions: []string{".test"}, ProviderVersion: "v1", ProjectionVersion: "v1", OwnershipPolicy: OwnershipDefault, Capabilities: MustCapabilities(CapabilitySourceReading)}
	source, err := NewAuthoredSource(paths.NotePath("note.test"), descriptor, []byte("abcdef"), 0)
	require.NoError(t, err)

	valid := MetadataPatchPlan{Patches: []MetadataSourcePatch{
		{Range: SourceRange{StartByte: 1, EndByte: 2}, Expected: []byte("b"), Replacement: []byte("B")},
		{Range: SourceRange{StartByte: 4, EndByte: 4}, Replacement: []byte("!")},
	}}
	assert.NoError(t, ValidateMetadataPatchPlan(source, valid))

	overlap := valid
	overlap.Patches = append(overlap.Patches, MetadataSourcePatch{Range: SourceRange{StartByte: 1, EndByte: 3}, Expected: []byte("bc")})
	assert.ErrorContains(t, ValidateMetadataPatchPlan(source, overlap), "overlap")

	stale := MetadataPatchPlan{Patches: []MetadataSourcePatch{{Range: SourceRange{StartByte: 0, EndByte: 1}, Expected: []byte("x")}}}
	assert.ErrorContains(t, ValidateMetadataPatchPlan(source, stale), "expected bytes")
}
