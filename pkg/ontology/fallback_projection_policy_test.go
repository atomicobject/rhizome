package ontology

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestFallbackNoteTypeIsInternalByDefault(t *testing.T) {
	require.True(t, IsFallbackNoteTypeName(FallbackNoteTypeName))
	require.Equal(t, TypeVisibilityInternal, OntologyTypeVisibility(FallbackNoteTypeName))
	require.Equal(t, TypeVisibilityPublic, OntologyTypeVisibility("Project"))
}
