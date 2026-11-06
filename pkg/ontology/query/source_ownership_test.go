package query

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestWorkspaceCapabilitiesReadOwnershipAfterIndexing(t *testing.T) {
	env := newCustomQueryTestEnv(t, `type Document @node(paths: ["notes/*.md"]) { title: String }`, map[string]string{"notes/owned.md": "---\ntitle: Owned\n---\nBody.\n"})
	prepared, errs := PrepareWithVariables(env.execSchema, `{ node(ref: "notes/owned.md") { workspace { capabilities { canEdit canEditFields canEditCollections readOnlyReason } fields { name capability { writeOperation readOnlyReason } } bodies { fields { capability { writeOperation readOnlyReason } } } } } }`, nil)
	require.Empty(t, errs)
	reason := "Managed by AO KB. Change it with the AO KB skills."
	require.NoError(t, os.WriteFile(filepath.Join(env.root, ".rhizome/source-ownership.yml"), []byte("sources:\n  - owner: AO KB\n    reason: "+reason+"\n    paths: [notes/owned.md]\n"), 0644))
	result := Execute(t.Context(), env.deps(nil), env.schema, prepared)
	require.Empty(t, result.Errors)
	node := result.Data["node"].(map[string]any)
	workspace := node["workspace"].(map[string]any)
	capability := workspace["capabilities"].(map[string]any)
	require.Equal(t, false, capability["canEdit"])
	require.Equal(t, false, capability["canEditFields"])
	require.Equal(t, false, capability["canEditCollections"])
	require.Equal(t, reason, capability["readOnlyReason"])
	for _, field := range workspace["fields"].([]any) {
		capability := field.(map[string]any)["capability"].(map[string]any)
		require.Nil(t, capability["writeOperation"])
		require.Equal(t, reason, capability["readOnlyReason"])
	}
}
