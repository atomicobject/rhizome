## Persistent MCP serving

One application package owns the stdio transport and request-scoped invoker. `cmd/mcp.go` supplies runtime factories and connection write authority; the tool catalog supplies schemas, descriptions, and eligible tools.

- `NewServer` and `Serve` adapt JSON-RPC tools/list and tools/call to the shared agentapi handlers.
- `NewInvoker` snapshots the selected vault definition and configuration bytes before accepting calls. A changed snapshot requires reconnection.
- Each request resolves its concrete runtime operation, uses the bound planner for recipe execution, and closes its acquired resources after success or error. Capabilities uses the explicit runtime-free constructor.
- Read-write authority belongs to the connection and is passed to the shared handler. Request arguments never grant additional authority.

The invoker and transport tests exercise actual tool requests, configuration changes, argument forwarding, write-mode errors, cleanup, and recipe routing.
