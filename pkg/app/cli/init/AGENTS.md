# Init test ownership

- Test starter adoption, refresh authority, mutation order, and rerun behavior through `Run` with a temporary repository. Keep `cmd/init_test.go` for flag parsing and forwarding.
- Test the ownership rule through `syncGeneratedFiles` (helpers in `file_plan_test.go`), and installed skill bytes and harness routing through `applyAgentSurfacesForTest`. Keep independent prompt wording and required resource checks against the canonical embedded sources.
- Recipe execution tests must require each selected recipe ID before querying. A loop over matches can pass when every recipe is missing.
- Preserve user-owned files and cleanup authority checks in the test that exercises the actual write or deletion path. A `rhizome` prefix alone gives no cleanup authority; the generated-files record does.
