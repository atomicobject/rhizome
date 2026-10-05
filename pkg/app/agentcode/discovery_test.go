package agentcode

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDescribeAcceptsExactMethodNames(t *testing.T) {
	want, err := Describe([]string{"query_recipe", "ontology_authoring_guide"})
	require.NoError(t, err)
	got, err := Describe([]string{"queryRecipe", "ontologyAuthoringGuide"})
	require.NoError(t, err)
	require.Equal(t, want, got)

	_, err = Describe([]string{"query_recipe", "queryRecipe"})
	require.ErrorContains(t, err, "duplicate")
	_, err = Describe([]string{"queryRecip"})
	require.ErrorContains(t, err, "unsupported")
	require.ErrorContains(t, err, "code surface")
}

func TestGenerateMethodSelectionUsesCanonicalArtifact(t *testing.T) {
	output := t.TempDir()
	canonical, err := Generate(output, []string{"query_recipe"})
	require.NoError(t, err)
	method, err := Generate(output, []string{"queryRecipe"})
	require.NoError(t, err)
	require.Equal(t, canonical.ArtifactHash, method.ArtifactHash)
	require.True(t, method.Reused)
}

func TestDescribeSchemaSelectionPreservesContractAndGuidance(t *testing.T) {
	full, err := Describe([]string{"query_recipe", "ontology_authoring_guide"})
	require.NoError(t, err)
	for _, selection := range []string{"input", "output", "both"} {
		t.Run(selection, func(t *testing.T) {
			selected, err := full.SelectSchemas(selection)
			require.NoError(t, err)
			require.Equal(t, full.ContractHash, selected.ContractHash)
			require.Equal(t, full.Selected, selected.Selected)
			require.Equal(t, full.Invocation, selected.Invocation)
			require.Equal(t, full.Outcome, selected.Outcome)
			var wire map[string]any
			encoded, err := json.Marshal(selected)
			require.NoError(t, err)
			require.NoError(t, json.Unmarshal(encoded, &wire))
			for i, raw := range wire["operations"].([]any) {
				op := raw.(map[string]any)
				require.NotEmpty(t, op["interpretation"])
				if selection == "input" {
					require.NotContains(t, op, "outputSchema")
					require.Equal(t, full.Operations[i].InputSchema, selected.Operations[i].InputSchema)
				} else if selection == "output" {
					require.NotContains(t, op, "inputSchema")
					require.Equal(t, full.Operations[i].OutputSchema, selected.Operations[i].OutputSchema)
				} else {
					require.Equal(t, full, selected)
				}
			}
			if selection != "both" {
				require.Equal(t, selection, wire["schemaSelection"])
				require.Less(t, len(encoded), jsonSize(t, full))
			} else {
				require.NotContains(t, wire, "schemaSelection")
			}
		})
	}
	// Projection must not mutate the full contract used for client generation.
	again, err := Describe([]string{"query_recipe", "ontology_authoring_guide"})
	require.NoError(t, err)
	require.Equal(t, again, full)
	_, err = full.SelectSchemas("inputs")
	require.ErrorContains(t, err, "input, output, or both")
}
