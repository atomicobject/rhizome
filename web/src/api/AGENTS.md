# API test ownership

- `client.test.ts` owns HTTP transport, retry, cancellation, and serialized request contracts. Assert edit overlays at the outgoing request body.
- `graphql/operations.test.ts` owns checked-in operation validity against the Go-generated public schema and field selections consumed by workspace adapters and the explorer.
- `nodeWorkspaceAdapter.test.ts` owns public GraphQL projection into workspace nodes. Assert relationships through refs and returned IDs, not the private ID format.
