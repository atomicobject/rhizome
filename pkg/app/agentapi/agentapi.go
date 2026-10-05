package agentapi

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	mcpapi "github.com/atomicobject/rhizome/pkg/app/mcp"
	mcpgo "github.com/mark3labs/mcp-go/mcp"
)

type Config = mcpapi.Config

type FilesResponse = mcpapi.FilesResponse
type ContextTextResponse = mcpapi.ContextTextResponse
type CapabilitiesResponse = mcpapi.CapabilitiesResponse
type CapabilitiesServer = mcpapi.CapabilitiesServer
type CapabilitiesVault = mcpapi.CapabilitiesVault
type CapabilitiesTools = mcpapi.CapabilitiesTools
type CapabilitiesFeatures = mcpapi.CapabilitiesFeatures
type CapabilitiesLimits = mcpapi.CapabilitiesLimits
type CapabilitiesReports = mcpapi.CapabilitiesReports

type SemanticQueryResponse = mcpapi.SemanticQueryResponse
type SemanticQueryOptions = mcpapi.SemanticQueryOptions
type ExternalReferencesResponse = mcpapi.ExternalReferencesResponse

// SemanticQueryUnifiedWithOptions runs the shared query implementation with
// typed public search controls. It exists for callers that need typed access to
// the answer packet while preserving the same config/runtime boundary as the
// MCP tool.
func SemanticQueryUnifiedWithOptions(ctx context.Context, cfg Config, options SemanticQueryOptions) (SemanticQueryResponse, error) {
	return mcpapi.SemanticQueryUnifiedWithOptions(ctx, cfg, options)
}

// CallJSON dispatches one MCP-style tool handler in process and returns its JSON
// text payload.
//
// The marshal/unmarshal normalization is intentional: CLI callers often build
// maps with Go ints, bools, or []string values, while MCP handlers receive JSON
// decoded values. Normalizing here keeps the in-process path behavior-aligned
// with real MCP requests.
func CallJSON(ctx context.Context, cfg Config, name string, args map[string]any) ([]byte, error) {
	handler, err := toolHandler(name, cfg)
	if err != nil {
		return nil, err
	}
	normalizedArgs, err := normalizeArguments(args)
	if err != nil {
		return nil, err
	}
	req := mcpgo.CallToolRequest{
		Params: mcpgo.CallToolParams{
			Name:      name,
			Arguments: normalizedArgs,
		},
	}
	res, err := handler(ctx, req)
	if err != nil {
		return nil, err
	}
	if res == nil {
		return []byte("null"), nil
	}
	payload := extractToolText(res)
	if res.IsError {
		if payload == "" {
			payload = "tool returned an error"
		}
		return nil, fmt.Errorf("%s", payload)
	}
	if payload == "" {
		return []byte("null"), nil
	}
	return []byte(payload), nil
}

func normalizeArguments(args map[string]any) (map[string]any, error) {
	if args == nil {
		return map[string]any{}, nil
	}
	data, err := json.Marshal(args)
	if err != nil {
		return nil, err
	}
	var normalized map[string]any
	if err := json.Unmarshal(data, &normalized); err != nil {
		return nil, err
	}
	return normalized, nil
}

func CallContextText(ctx context.Context, cfg Config, name string, args map[string]any) (string, error) {
	payload, err := CallJSON(ctx, cfg, name, args)
	if err != nil {
		return "", err
	}
	var resp struct {
		Text string `json:"text"`
	}
	if err := json.Unmarshal(payload, &resp); err != nil {
		return "", err
	}
	return resp.Text, nil
}

func toolHandler(name string, cfg Config) (func(context.Context, mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error), error) {
	descriptor, ok := descriptorForName(name, SurfaceAgentAPI)
	if !ok {
		return nil, fmt.Errorf("unknown tool: %s", name)
	}
	return descriptor.Handler(cfg), nil
}

func extractToolText(res *mcpgo.CallToolResult) string {
	var b strings.Builder
	for _, content := range res.Content {
		switch c := content.(type) {
		case mcpgo.TextContent:
			b.WriteString(c.Text)
		case *mcpgo.TextContent:
			b.WriteString(c.Text)
		}
	}
	return b.String()
}
