# Test ownership

- Public HTTP and GraphQL tests own transport, status and error envelopes, admission, Host/Origin enforcement, and edit-session wire recovery. Direct node or model tests cannot replace them.
- Keep one behavioral owner per adapter contract. Carry distinct assertions into the live owner before deleting a private replay; never keep a production route or option branch only for tests.
- For staged reads, warm the session, then restage, change source, or publish a schema, and assert returned fields plus status, rebase, and conflict state. Cache-key equality or pointer identity does not prove freshness.
- Exercise SSE through real delivery, and process watch events synchronously before testing duplicate suppression. A coalesced hint is not proof of suppression.
- Negative path and security fixtures must contain the denied resource, and must use an admitted application Host when testing the later Origin guard.
- Test the REST session base graph (`buildWorkspaceGraph`) separately from canonical GraphQL/noderead graph behavior. Do not restore the retired node-workspace local graph.
- A fake authority or repair engine returning precomputed evidence proves serialization only. Assert the exact decoded receipt fields; durable application and idempotence belong to the canonical engine tests.
- Before keeping a private helper test, find its non-test caller. Delete dead helpers with their tests.
- Graph cache lifetime tests wait until each caller has joined the in-flight build at `getOrBuild`, then cancel or release it. Test the SQL fingerprint wrapper separately. Do not add production waiter callbacks or sleeps as barriers.
- Assert absent JSON fields against the raw object; a typed decoder ignores unknown keys. Overlay tests compare an observable staged value with its committed control, not merely nonempty output.
- Keep both build-tag proofs: `TestDefaultBuildHasNoAgentHarnessOverride` in the default build and the `e2efake` protocol test, which an ordinary package run excludes.
- Degrade paths use real authored inputs (for example a schema that loads but fails `BuildExecutableSchema`), not injected production function fields.
