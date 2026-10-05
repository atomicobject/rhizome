// Package mcpserve adapts the cataloged agent handlers to an MCP stdio server.
package mcpserve

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/atomicobject/rhizome/pkg/app/agentapi"
	"github.com/atomicobject/rhizome/pkg/vault/version"
	"github.com/mark3labs/mcp-go/mcp"
	mcpserver "github.com/mark3labs/mcp-go/server"
)

// Dispatcher runs one cataloged tool call and returns its JSON text payload.
// The command supplies the request-scoped runtime and agentapi.CallJSON
// implementation; tests can provide a small in-memory dispatcher.
type Dispatcher func(context.Context, string, map[string]any) ([]byte, error)

// NewServer builds a stdio-independent MCP server for the requested catalog
// tools. The catalog remains the source of tool names, descriptions, schemas,
// and handler eligibility.
func NewServer(allowlist []string, dispatch Dispatcher) (*mcpserver.MCPServer, error) {
	if dispatch == nil {
		return nil, fmt.Errorf("MCP server requires a dispatcher")
	}

	selected, err := selectedDescriptors(allowlist)
	if err != nil {
		return nil, err
	}

	server := mcpserver.NewMCPServer("rhizome", version.Version, mcpserver.WithHooks(&mcpserver.Hooks{}))
	for _, descriptor := range selected {
		server.AddTool(toolDefinition(descriptor), dispatchTool(dispatch))
	}
	return server, nil
}

// ValidateAllowlist checks tool names without constructing a server or
// requiring runtime state.
func ValidateAllowlist(allowlist []string) error {
	_, err := selectedDescriptors(allowlist)
	return err
}

func selectedDescriptors(allowlist []string) ([]agentapi.ToolDescriptor, error) {
	descriptors := sharedDescriptors()
	byName := make(map[string]agentapi.ToolDescriptor, len(descriptors))
	validNames := make([]string, 0, len(descriptors))
	for _, descriptor := range descriptors {
		byName[descriptor.Name] = descriptor
		validNames = append(validNames, descriptor.Name)
	}
	sort.Strings(validNames)

	selected := make([]agentapi.ToolDescriptor, 0, len(allowlist))
	seen := make(map[string]struct{}, len(allowlist))
	for _, rawName := range allowlist {
		name := strings.TrimSpace(rawName)
		if name == "" {
			continue
		}
		if _, ok := seen[name]; ok {
			continue
		}
		descriptor, ok := byName[name]
		if !ok {
			return nil, fmt.Errorf("unknown MCP tool %q; valid tools: %s", name, strings.Join(validNames, ", "))
		}
		seen[name] = struct{}{}
		selected = append(selected, descriptor)
	}
	if len(selected) == 0 {
		return nil, fmt.Errorf("MCP server requires at least one tool in --tools")
	}
	return selected, nil
}

// Serve runs the selected MCP tools over the supplied stdio streams.
func Serve(ctx context.Context, input io.Reader, output io.Writer, allowlist []string, dispatch Dispatcher) error {
	server, err := NewServer(allowlist, dispatch)
	if err != nil {
		return err
	}
	return mcpserver.NewStdioServer(server).Listen(ctx, input, output)
}

func sharedDescriptors() []agentapi.ToolDescriptor {
	descriptors := make([]agentapi.ToolDescriptor, 0)
	for _, descriptor := range agentapi.ToolCatalog() {
		if descriptor.Surfaces&agentapi.SurfaceAgentAPI == 0 || descriptor.Handler == nil {
			continue
		}
		descriptors = append(descriptors, descriptor)
	}
	return descriptors
}

func toolDefinition(descriptor agentapi.ToolDescriptor) mcp.Tool {
	if descriptor.CodeContract == nil {
		return mcp.NewToolWithRawSchema(descriptor.Name, "", json.RawMessage(`{"type":"object"}`))
	}
	return mcp.NewToolWithRawSchema(
		descriptor.Name,
		toolDescription(descriptor.CodeContract),
		descriptor.CodeContract.InputSchema,
	)
}

// toolDescription gives MCP clients the whole contract: what the tool does,
// its side effects, and how to read its result. A code-mode or agent CLI
// restriction on apply is replaced with MCP's rule, because MCP handlers apply
// changes on a read-write connection.
func toolDescription(contract *agentapi.CodeOperationContract) string {
	parts := []string{contract.Summary}
	var effects []string
	for _, effect := range contract.Effects {
		if mentionsOtherSurface(effect) {
			effect = "applies changes when requested on a read-write connection"
		}
		effects = append(effects, effect)
	}
	if len(effects) > 0 {
		parts = append(parts, "Effects: "+strings.Join(effects, "; ")+".")
	}
	for _, note := range contract.Interpretation {
		var kept []string
		for _, sentence := range strings.SplitAfter(note, ". ") {
			if !mentionsOtherSurface(sentence) {
				kept = append(kept, sentence)
			}
		}
		if text := strings.TrimSpace(strings.Join(kept, "")); text != "" {
			parts = append(parts, text)
		}
	}
	return strings.Join(parts, "\n\n")
}

// ponytail: phrase match on the contract text; give contracts per-surface
// fields if restrictions stop naming the surface they apply to.
func mentionsOtherSurface(text string) bool {
	lower := strings.ToLower(text)
	return strings.Contains(lower, "code mode") || strings.Contains(lower, "code-mode") || strings.Contains(lower, "agent cli")
}

func dispatchTool(dispatch Dispatcher) mcpserver.ToolHandlerFunc {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		payload, err := dispatch(ctx, request.Params.Name, request.GetArguments())
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		return mcp.NewToolResultText(string(payload)), nil
	}
}
