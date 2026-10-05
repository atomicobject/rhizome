# Indexing test ownership

- Discovery and transition contracts belong to `noteownership.Discover` and `noteownership.BuildTransitionPlan`. Indexing tests cover actual lane conversion and publication; do not resurrect test-only ownership wrappers.
- Expanded metadata refresh tests assert the exact persisted note-path set, not only a counter or a nonempty query result.
