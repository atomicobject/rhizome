---
summary: "Code anchors for catalog-dispatched MCP handlers and per-call runtime snapshots so tool/runtime changes pull subsystem guidance in file_context."
tags: [type/reference, subsystem/codeanchor, subsystem/mcp-server]
code-anchors:
  go:
    - label: agentapi-call-json
      symbol: github.com/atomicobject/rhizome/pkg/app/agentapi.CallJSON
    - label: agentapi-call-context-text
      symbol: github.com/atomicobject/rhizome/pkg/app/agentapi.CallContextText
    - label: mcp-config
      symbol: github.com/atomicobject/rhizome/pkg/app/mcp.Config
    - label: agentapi-tool-catalog
      ref: glob:pkg/app/agentapi/catalog.go
    - label: runtime-view
      ref: glob:pkg/app/runtimeview/**/*.go
    - label: mcp-files-tool
      symbol: github.com/atomicobject/rhizome/pkg/app/mcp.FilesTool
---

# Go anchor - Agent tool handlers

Code anchors for `pkg/app/mcp` handlers, catalog-backed `pkg/app/agentapi` dispatch, and the live per-call capability view used by agent and serve runtimes.

## Read these first

![[mcp-server]]
