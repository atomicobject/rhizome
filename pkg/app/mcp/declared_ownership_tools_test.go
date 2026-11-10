package mcp_test

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"github.com/atomicobject/rhizome/internal/testkit"
	mcpapi "github.com/atomicobject/rhizome/pkg/app/mcp"
	"github.com/atomicobject/rhizome/pkg/app/validationproduct"
	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/sourceownership"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/atomicobject/rhizome/pkg/vault/repoconfig"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/stretchr/testify/require"
)

// SPEC-0133 R5: MCP write tools refuse targets a repository's own declaration
// owns, in a copy of the booking fixture under its compiled bindings, with the
// owner's reason and no byte changes. The AO KB starter declares no ownership
// of its own; this declaration is the repository's.
const bookingDeclaration = "sources:\n  - owner: Model tool\n    reason: Managed by the model tool. Change it there.\n    paths:\n      - assertions/**\n      - artifacts/**\n"

func TestMCPWriteToolsRefuseDeclaredOwnedTargetsWithoutChangingBytes(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.CopyFS(root, os.DirFS("../../../testdata/ao-kb/booking")))
	static, err := filepath.Glob("../cli/init/templates/starters/ao-kb/module/ontology/*.graphql")
	require.NoError(t, err)
	require.NoError(t, os.MkdirAll(filepath.Join(root, ".rhizome/ontology"), 0o755))
	for _, file := range append(static, "../../../testdata/ao-kb/expected-model.graphql", "../../../testdata/ao-kb/expected-knowledge.graphql") {
		data, err := os.ReadFile(file)
		require.NoError(t, err)
		require.NoError(t, os.WriteFile(filepath.Join(root, ".rhizome/ontology", filepath.Base(file)), data, 0o644))
	}
	require.NoError(t, os.WriteFile(filepath.Join(root, ".rhizome/config.yml"), []byte("notes:\n  includes: ['**/*.md']\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, sourceownership.DeclarationPath), []byte(bookingDeclaration), 0o644))
	authored := map[string]string{}
	require.NoError(t, filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return err
		}
		data, err := os.ReadFile(path)
		authored[path] = string(data)
		return err
	}))

	metadata := testkit.NoteMetadataIndexer(t)
	vaultDef := obsidian.VaultDefinition{Path: root, Includes: []string{"**/*.md"}}
	config := mcpapi.Config{VaultPath: root, VaultDef: vaultDef, Vault: &repoconfig.Vault{Env: testkit.Env(t), Name: root}, ReadWrite: true, NoteMetadata: metadata}
	config.ApplyLinkTargets = func(ctx context.Context, schemaHash string, request ontology.LinkTargetRequest) (ontology.LinkTargetResult, error) {
		return validationproduct.ApplyNodeLinkTargets(ctx, validationproduct.NodeLinkApplyRequest{VaultDef: vaultDef, NoteMetadata: metadata, NoteReader: &obsidian.Note{}, SchemaHash: schemaHash, LinkTarget: request})
	}
	const assertion = "assertions/clinic-patient-books-own-slot.md"
	for _, call := range []struct {
		name string
		tool func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error)
		args map[string]any
	}{
		{"node_link ensure=apply on an owned embedded section", mcpapi.NodeLinkTool(config), map[string]any{"targets": []any{assertion + "#Connections"}, "ensure": "apply"}},
		{"note_rename_heading on an owned note", mcpapi.RenameHeadingTool(config), map[string]any{"path": assertion, "oldHeading": "Sources", "newHeading": "Evidence", "apply": true}},
	} {
		t.Run(call.name, func(t *testing.T) {
			result, err := call.tool(t.Context(), mcp.CallToolRequest{Params: mcp.CallToolParams{Arguments: call.args}})
			require.NoError(t, err)
			require.True(t, result.IsError, "%+v", result.Content)
			require.Contains(t, result.Content[0].(mcp.TextContent).Text, "Managed by the model tool. Change it there.")
			for path, content := range authored {
				data, err := os.ReadFile(path)
				require.NoError(t, err)
				require.Equal(t, content, string(data), path)
			}
		})
	}
}
