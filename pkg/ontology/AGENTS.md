# Test ownership

- Source-span tests assert the unmodified authored bytes and ordered field or child identities. Helpers must not strip syntax before comparing spans.
- Edit rejection tests identify the intended conflict kind and field or path and verify unchanged source.
- Edit-to-query integration in `tests/integration/python/ontology_edit_session_test.go` refreshes metadata and calls `ontology.SyncPaths`, then asserts persisted node fields before querying; hand-built ontology snapshots do not prove convergence.
- Embedded validation tests use independently authored diagnostics, including each invalid field and node identity; do not copy the production traversal as an oracle.
- Keep exact schema, identity, and authoring-guide assertions when they guard a public contract. A compound invalid fixture must assert every claimed diagnostic separately.
