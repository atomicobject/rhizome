package ontology

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCompileTypePolicies_ExtractsTraversalOverrides(t *testing.T) {
	schema := &Schema{
		Hash: "abc",
		Types: map[string]*NoteType{
			"Project": {
				Name: "Project",
				Annotations: map[string]map[string]any{
					"traversal": {
						"intents":           []any{"docs_for_code"},
						"includeAmbient":    true,
						"minStructuralHits": int64(2),
						"maxDepth":          int64(3),
					},
				},
			},
			"Decision": {Name: "Decision"},
		},
	}

	policies := CompileTypePolicies(schema)
	require.Len(t, policies, 1)
	require.Equal(t, "Project", policies[0].TypeName)
	require.Len(t, policies[0].Overrides, 1)

	override, ok := policies[0].OverrideForIntent("docs_for_code")
	require.True(t, ok)
	require.NotNil(t, override.IncludeAmbient)
	require.True(t, *override.IncludeAmbient)
	require.NotNil(t, override.MinStructuralHits)
	require.Equal(t, 2, *override.MinStructuralHits)
	require.NotNil(t, override.MaxDepth)
	require.Equal(t, 3, *override.MaxDepth)

	encoded := MarshalTypePolicy(policies[0])
	require.NotEmpty(t, encoded)

	decoded, err := UnmarshalTypePolicy(encoded)
	require.NoError(t, err)
	got, ok := decoded.OverrideForIntent("docs_for_code")
	require.True(t, ok)
	require.NotNil(t, got.IncludeAmbient)
	require.True(t, *got.IncludeAmbient)
	require.NotNil(t, got.MaxDepth)
	require.Equal(t, 3, *got.MaxDepth)
}
