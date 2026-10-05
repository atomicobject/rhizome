// Package agentapi adapts the MCP tool handlers for in-process callers.
//
// The agent CLI and local chat service use this package when they want the same
// JSON contract as MCP without starting a stdio server. ToolDescriptor catalog
// metadata and handler construction live here so dispatch, CLI command mapping,
// and capability advertisement share one inventory. Keep the rest of this layer
// thin: selected code-client schemas attach to the same descriptors; agentcode
// projects them without another dispatch registry. Runtime effects and output
// payloads remain owned by the existing commands and handlers.
//
// Argument normalization and text extraction live here, while bootstrap
// owns live state and pkg/app/mcp owns session dedupe, retrieval, and packing.
package agentapi
