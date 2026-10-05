package mcp

import (
	"context"
	"encoding/json"
	"fmt"

	actions "github.com/atomicobject/rhizome/pkg/app/cli"
	"github.com/mark3labs/mcp-go/mcp"
)

// RenameHeadingTool implements the note_rename_heading MCP tool.
func RenameHeadingTool(config Config) func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		args := request.GetArguments()
		path, _ := args["path"].(string)
		oldHeading, _ := args["oldHeading"].(string)
		newHeading, _ := args["newHeading"].(string)
		apply, _ := args["apply"].(bool)
		if apply && !config.ReadWrite {
			encoded, _ := json.Marshal(map[string]string{
				"code":    "apply_requires_read_write",
				"message": "note_rename_heading apply requires MCP read-write mode",
			})
			return mcp.NewToolResultError(string(encoded)), nil
		}
		upgrade, _ := args["upgradeToBlockId"].(string)
		fallback, _ := args["fallback"].(string)
		result, err := actions.RenameHeading(config.Vault, actions.RenameHeadingParams{
			Context:          ctx,
			NoteMetadata:     config.NoteMetadata,
			Path:             path,
			OldHeading:       oldHeading,
			NewHeading:       newHeading,
			Apply:            apply,
			UpgradeToBlockID: actions.HeadingRenameUpgradeMode(upgrade),
			Fallback:         actions.HeadingRenameFallbackMode(fallback),
		})
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("rename heading failed: %s", err)), nil
		}
		encoded, err := json.Marshal(result)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("error marshaling response: %s", err)), nil
		}
		return mcp.NewToolResultText(string(encoded)), nil
	}
}
