package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/stretchr/testify/require"
)

func TestNodeLinkToolPlansDurableEmbeddedLink(t *testing.T) {
	root := t.TempDir()
	writeNodeLinkTestVault(t, root, nodeLinkTestSchema, map[string]string{
		"specs/001-test/spec.md": `---
type: Spec
summary: Test
---

# Spec

## User Stories

### Story A
id:: SPEC-0023.US1
status:: TODO
`,
	})
	cfg := Config{VaultPath: root, VaultDef: obsidian.VaultDefinition{Name: "vault", Path: root}}
	resp, err := NodeLinkTool(cfg)(context.Background(), mcp.CallToolRequest{
		Params: mcp.CallToolParams{
			Name: "node_link",
			Arguments: map[string]any{
				"targets": []any{"specs/001-test/spec.md#Story A"},
				"ensure":  "plan",
			},
		},
	})
	require.NoError(t, err)
	require.False(t, resp.IsError)
	payload := nodeLinkPayload(t, resp)
	require.Equal(t, "plan", payload.Ensure)
	locator := payload.Locators["specs/001-test/spec.md#Story A"]
	require.Equal(t, ontology.NodeLocatorRequiresFix, locator.Status)
	require.NotNil(t, locator.LinkTarget)
	require.Equal(t, "[[spec#^SPEC-0023-US1]]", locator.LinkTarget.Wikilink)
	require.Equal(t, "SPEC-0023-US1", locator.LinkTarget.BlockID)
	require.True(t, locator.LinkTarget.RequiresFix)
	require.Len(t, locator.FixActions, 1)
}

func TestNodeLinkToolRefusesApplyWhenReadOnly(t *testing.T) {
	root := t.TempDir()
	writeNodeLinkTestVault(t, root, nodeLinkTestSchema, nil)
	resp, err := NodeLinkTool(Config{VaultPath: root, VaultDef: obsidian.VaultDefinition{Path: root}})(context.Background(), mcp.CallToolRequest{
		Params: mcp.CallToolParams{
			Name: "node_link",
			Arguments: map[string]any{
				"targets": []any{"specs/001-test/spec.md#Story A"},
				"ensure":  "apply",
			},
		},
	})
	require.NoError(t, err)
	require.True(t, resp.IsError)
	text := resp.Content[0].(mcp.TextContent).Text
	require.Contains(t, text, "apply_requires_read_write")
}

type nodeLinkToolResponse struct {
	Ensure   string                          `json:"ensure"`
	Locators map[string]ontology.NodeLocator `json:"locators"`
}

func nodeLinkPayload(t *testing.T, resp *mcp.CallToolResult) nodeLinkToolResponse {
	t.Helper()
	require.Len(t, resp.Content, 1)
	text := resp.Content[0].(mcp.TextContent).Text
	var payload nodeLinkToolResponse
	require.NoError(t, json.Unmarshal([]byte(text), &payload))
	return payload
}

func writeNodeLinkTestVault(t *testing.T, root, schema string, files map[string]string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Join(root, ".rhizome", "ontology"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, ".rhizome", "config.yml"), []byte("notes:\n  includes: [\"**/*.md\"]\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, ".rhizome", "ontology", "schema.graphql"), []byte(schema), 0o644))
	for rel, body := range files {
		full := filepath.Join(root, rel)
		require.NoError(t, os.MkdirAll(filepath.Dir(full), 0o755))
		require.NoError(t, os.WriteFile(full, []byte(body), 0o644))
	}
}

const nodeLinkTestSchema = `
type UserStory implements Section @node(locator: EMBEDDED) {
  id: ID! @field @identifier(preferred: true, derivedSuffix: "US")
  status: String @field
}

type UserStoriesSection implements Section {
  stories: [UserStory!] @contains(level: H3)
}

type Spec @node(paths: ["specs/*/spec.md"]) {
  summary: String!
  userStoriesSection: UserStoriesSection @contains(level: H2, heading: "User Stories")
}
`

func TestNodeLinkToolApplyRequiresCapabilityAndPropagatesFailure(t *testing.T) {
	root := t.TempDir()
	source := "# Spec\n\n## User Stories\n\n### Story A\nid:: SPEC-0023.US1\nstatus:: TODO\n"
	writeNodeLinkTestVault(t, root, nodeLinkTestSchema, map[string]string{"specs/001-test/spec.md": source})
	cfg := Config{VaultPath: root, VaultDef: obsidian.VaultDefinition{Path: root}, ReadWrite: true, NoteMetadata: configuredMCPNoteMetadata(t)}
	request := mcp.CallToolRequest{Params: mcp.CallToolParams{Name: "node_link", Arguments: map[string]any{"targets": []any{"specs/001-test/spec.md#Story A"}, "ensure": "apply"}}}
	response, err := NodeLinkTool(cfg)(context.Background(), request)
	require.NoError(t, err)
	require.True(t, response.IsError)
	unchanged, err := os.ReadFile(filepath.Join(root, "specs/001-test/spec.md"))
	require.NoError(t, err)
	require.Equal(t, source, string(unchanged))
	cfg.ApplyLinkTargets = func(context.Context, string, ontology.LinkTargetRequest) (ontology.LinkTargetResult, error) {
		return ontology.LinkTargetResult{Applied: true}, fmt.Errorf("source applied; index convergence incomplete")
	}
	response, err = NodeLinkTool(cfg)(context.Background(), request)
	require.NoError(t, err)
	require.True(t, response.IsError)
	text, err := json.Marshal(response)
	require.NoError(t, err)
	require.Contains(t, string(text), "source applied; index convergence incomplete")
}
