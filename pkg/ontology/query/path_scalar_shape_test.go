package query

import (
	"context"
	"testing"

	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/stretchr/testify/require"
)

func TestPathScalarListRejectsStructuredMembers(t *testing.T) {
	const content = "---\nconnections:\n  supports: [plain, {target: claim}]\n---\n# Claim\n"
	env := newCustomQueryTestEnv(t, `type Assertion @node(paths: ["notes/*.md"]) { targets: [String!] @field(path: "connections.supports") }`, map[string]string{"notes/claim.md": content})
	prepared, errs := Prepare(env.execSchema, `{ assertion(first: 1) { targets } }`)
	require.Empty(t, errs)
	assessment, ok, err := ontology.NewService(env.store, env.schema).Assessment(context.Background(), "notes/claim.md")
	require.NoError(t, err)
	require.True(t, ok)
	field, ok := assessment.Field("targets")
	require.True(t, ok)
	require.Equal(t, "field_shape_mismatch", field.Issues[0].Code)
	for _, staged := range []bool{false, true} {
		deps := env.deps(nil)
		if staged {
			deps.ReadOverlay = &ReadOverlay{SourceFormat: "markdown", UpdatedContentByPath: map[string]string{"notes/claim.md": content}, Status: "dirty"}
		}
		result := Execute(context.Background(), deps, env.schema, prepared)
		require.Empty(t, result.Errors)
		row := result.Data["assertion"].([]any)[0].(map[string]any)
		require.Nil(t, row["targets"], "structured list members must not become Go map text or a partial list")

	}
}
