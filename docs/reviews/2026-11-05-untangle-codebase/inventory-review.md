# Independent challenge of the untangle inventory

Reviewed against `900eee0b0036ee2699d84d5d02d5ffe6a39a4441`. Read-only source review; no tests, builds, providers, credentials, or production edits.

## Verdict

The inventory supports a focused refactor. No finding requires abandoning it. Keep the concrete deletions and shared-owner fixes, with the constraints below. The audit reports are evidence for candidate changes, not proof that their behavior is interchangeable.

## Required plan corrections

### 1. Keep the narrow view read port; fix its actual gate

**CONFIRMED.** `pkg/app/views/types.go:28-39` accepts `noderead.ReadStore`, whereas `pkg/ontology/query/types.go:293-299` defines a strictly broader `query.Store` with four extra methods. Replacing both with one common dependency bundle would unnecessarily require plain type-view and catalog-only clients to supply GraphQL capabilities. `New` currently also supports schema/catalog-only construction and custom resolvers.

The smallest supported fix is the parent's revised disposition: retain the distinct optional query capabilities and change `source_ontology.go:255-258` to gate indexed type execution on the actual `ServiceOptions.Store`. The enclosing resolver already rejects missing read storage at `source_ontology.go:94-96`. Do not change `New`'s signature or add global dependency mismatch rejection based solely on duplicate constructor fields.

Prove the fix through `Service.Execute` with Store present and QueryDeps.Store absent, including filter/sort behavior across a source cap. Rows should retain the intended behavior; any plan metadata change from enabling indexed execution is part of that explicit correction. Keep query recipes, current-user bindings, and read overlays on their existing capability paths.

### 2. Treat Markdown protection consolidation as a behavior change until characterized

**CONFIRMED.** `markdownsyntax.ProtectedSpans` protects fenced, indented, and paired inline code (`protected.go:16-41`). Init's existing expander tracks fences and unpaired backtick runs but not indented blocks (`helper_template_embeds.go:22-70`). Skill-overlay parsing currently recognizes markers outside fences only (`skilloverlay/parse.go:14-36,80-104`). These are different accepted languages, not equivalent implementations.

In particular, an unmatched backtick suppresses all later init embeds today; the canonical paired-span parser does not. Indented code and a slot marker inside a multiline code span would become protected after a broad substitution. Preserve this distinction in the plan: first specify and test literal before/after cases, then adopt canonical protection only for behaviors justified as bugs. Existing tests at `helper_templates_test.go:19-57` cover ordinary inline/fenced cases, not all these differences. No blanket `CodeMask` replacement should be described as mechanical.

### 3. Retiring the old Indexer has one additional live documentation caller

**CONFIRMED missing reach.** `docs/code-anchors/Go anchor - Code Index (unified DB + intel).md:13` binds `github.com/atomicobject/rhizome/pkg/embeddings.NewIndexer`. SC-3 names the two explanatory documents but not this code-anchor note. Update that binding to the actual embedding owner, or remove the obsolete binding, and validate it with the retirement. Do not leave a broken anchor while reporting the API deletion complete.

Other exported API deletions are defensible under the approved retirement scope, but repository-wide reference absence proves repository usage only. Keep the documented `RankRelated` extension; the reports correctly excluded it.

## Verification constraints to retain

### Graph sharing must preserve boundary-specific output

Source confirms graph copies differ on module identity, language, embedded nodes, and ordering. The parent's policy-preserving extraction is appropriate. Test literal CLI and HTTP expectations separately; a test comparing both fronts to the same new helper would hide a shared regression. Keep notes-only no-collapse behavior (`graph_collapse.go:108-115`), filtering order, edge metadata, node-reference identity, and web enrichment outside any generic equivalence claim. Do not add a configurable graph framework merely to accommodate these few existing differences.

### Shared preview parsing needs caller-owned missing-value behavior

`query/workspace_bodies.go:214-235` replaces a missing property with empty text; `web/ontology.go:2878-2896` retains the token. The proposed shared parser with explicit caller fallback preserves both. Keep raw/projection property extraction outside that helper. No source evidence establishes either current missing-value behavior as the bug, so the refactor should not choose a universal output policy.

### Ledger extraction is small, but both adapters must actually run

The duplicated reservation and outcome rules are genuinely the same in the inspected source. Extracting pure decisions is reasonable; preserve adapter-owned no-row detection, lease checks, transaction/retry scope, error precedence, and counter arithmetic. Invalid outcomes must remain rejected before any transition; `not_dispatched` remains reusable; provider-unknown transitions keep holds.

`make team-api-check` alone does not prove DynamoDB behavior: `internal/dynamotest/dynamotest.go:22-24` skips unless `TEAMAPI_DYNAMODB_ENDPOINT` is set. The shared DynamoDB ledger contract at `internal/ledger/ledger_test.go:32-43` must run against disposable DynamoDB Local, or the final result must explicitly identify that coverage gap. The helper restricts itself to loopback and synthetic credentials, so this does not require production access.

## Cuts

- Do not implement the original broad Q4 constructor redesign; the narrow gate fix resolves the evidenced defect.
- Do not standardize graph or preview behavior merely to enable code sharing.
- Do not turn a whole-repository caller census into a promise about external Go consumers.
- Do not label selected passing tests as flaky. Improve synchronization only where the intended branch can be observed, or retain the test until there is a concrete replacement.

No additional architecture layer, registry framework, transaction abstraction, or generic dependency bundle is needed for these corrections.

## Review limits

Inspected the high-risk source paths and audit recommendations above. Did not independently re-read every candidate in all nine reports, run code, reproduce bugs, or validate live credentials/runtime behavior. This is a challenge review of scope and preservation arguments, not final implementation approval.
