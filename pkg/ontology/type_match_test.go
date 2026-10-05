package ontology

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestTypeMatchesOrImplementsUsesCanonicalTransitiveInterfaceSemantics(t *testing.T) {
	schema := &Schema{
		Types: map[string]*NoteType{
			"ProductSpec": {Name: "ProductSpec", Implements: []string{"SpecLike"}},
		},
		Interfaces: map[string]*InterfaceType{
			"SpecLike": {Name: "SpecLike", Implements: []string{"Document"}},
			"Document": {Name: "Document"},
		},
	}

	require.True(t, TypeMatchesOrImplements(schema, "ProductSpec", "SpecLike"))
	require.True(t, TypeMatchesOrImplements(schema, "ProductSpec", "Document"))
	require.False(t, TypeMatchesOrImplements(schema, "Document", "ProductSpec"))
}
