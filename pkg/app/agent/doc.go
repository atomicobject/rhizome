// Package agent contains the small LLM-facing retrieval planner used by local
// agent chat flows.
//
// The package intentionally stops at planning and dispatch abstractions: it
// knows tool names, JSON arguments, and handler results, but it does not own
// vault state, MCP runtime state, or context packing. Those heavier boundaries
// stay in pkg/app/mcp and pkg/app/agentapi so chat callers can share the same
// tool contracts as the CLI/MCP surfaces.
package agent
