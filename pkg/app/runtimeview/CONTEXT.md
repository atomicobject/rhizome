## Non-blocking live capability snapshots for agent and web consumers

`View` separates the async bootstrap producer from MCP/web readers. Each call takes a fresh `Snapshot`; consumers never synchronize a second mutable runtime.

- **Entry points**: `View`, `Snapshot`, `CapabilityState`.
- **Key invariants**: search, semantic, code, and leader states are independent; use only the matching `WaitFor*` method. A ready semantic phase never implies code or leader readiness.
- **Producer**: `bootstrap.LiveRuntime`. Consumers must not initialize stores or providers here.

### Deep docs

- [[mcp-server]]
- `pkg/app/bootstrap/CONTEXT.md`
