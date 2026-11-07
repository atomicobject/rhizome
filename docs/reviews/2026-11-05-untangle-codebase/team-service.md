# Team API service structural audit

Scope: `services/team-api`, the repository's only production directory under `services/`. Source-only audit against the current checkout. No production files changed; no test, build, deployment, or provider calls run. The parent owns build and verification scheduling.

The service already separates identity, traffic, storage, and accounting cleanly. Preserve the shared `storetest`/`ledgertest` suites and the two storage implementations. Four bounded changes are worth doing; the first is the strongest.

## SVC-1: Inject the existing ledger as the usage reader

**Confirmed. Hidden initialization order and surplus forwarding.**

Evidence:

- `services/team-api/cmd/team-api/main.go:132-143` constructs an empty `laterUsage`, passes it into `identity.New`, constructs traffic, then fills the embedded interface with `traf`.
- `services/team-api/cmd/team-api/main.go:196` defines `laterUsage` solely for this assignment.
- `services/team-api/internal/core/core.go:45` requires `Usage` and `ProviderUsage`. The existing `ledger.Ledger` returned by `openState` already has both (`internal/ledger/ledger.go:86-87`).
- `services/team-api/internal/traffic/traffic.go:209-212` and `internal/traffic/budget.go:15-17` forward those calls unchanged to that ledger.
- Production caller search finds no other consumers of these forwarding methods. Tests use them only in `internal/traffic/harness_test.go:187-191` and `internal/traffic/budget_test.go:20-29`.

Fix: pass `l` directly to `identity.New(cfg, st, l, rec, time.Now)`. Delete the placeholder, deferred assignment, `laterUsage`, the two traffic forwarding methods, and the now-unused `core` import in the composition root. Point the two traffic test helpers to their existing `h.tr.ledger`; they are in package `traffic`, so no new field is necessary. This leaves traffic depending on identity's runtime source, with no artificial dependency in the opposite direction and no temporarily invalid interface.

Reach: composition root, the two traffic source files, the two traffic test helpers. No schema, persistence, wire format, fixture, or config change. No new interface or wrapper is needed. Existing `core.UsageReader` remains the correct narrow identity-side contract.

Verification: focused `./cmd/team-api` stack tests and `./internal/identity`/`./internal/traffic` tests, followed by `make team-api-check`. Preserve observable sign-in/session/admin usage and paid-call accounting. This is a local refactor with no intended behavior change.

## SVC-2: Put shared receipt decision rules next to `Receipt` and `Outcome`

**Confirmed. Hidden links between storage adapters.**

Evidence:

- `internal/ledger/sqlite.go:179-192` and `internal/ledger/dynamodb.go:182-195` repeat the same route/body conflict check and state-to-error mapping for reservation reuse.
- `internal/ledger/sqlite.go:264-273` and `internal/ledger/dynamodb.go:254-263` repeat the same outcome-to-state mapping, invalid-outcome error, and known settled-credit rule.
- Shared `validReceipt`, `exceeds`, `budgeted`, `settledCurrency`, and `providerAdjustment` already establish the pattern of storage-independent decision rules in `ledger.go`/`provider.go`.
- `docs/specs/technical/team-api-gateway.md:207-221,246` requires identical idempotency/accounting semantics and shared contract suites across backends.

Fix: extract only these small pure decision rules into private functions in `ledger.go` (reservation replay code from prior route/hash/state, and outcome state/settled credits). Call them from both adapters. Keep lookup, transactions, lease checks, SQL counter arithmetic, DynamoDB conditional writes, and retry policy in their existing adapters.

Reach: the three ledger source files and the shared contract cases in `internal/ledgertest`. Existing tests already cover conflicts, in-progress/completed/unknown receipts and settlement; retain them and add a literal invalid-outcome case only if absent. Do not replace the two adapters with a generic transaction framework.

Risk: high consequence if accounting decisions change, although the extraction is small. Preserve current precedence exactly: route/hash conflicts win before state errors, `not_dispatched` keys can be reused, unknown usage retains reservations, and invalid outcomes change nothing. Verify against both SQLite and DynamoDB Local before declaring complete. No schema or recorded-receipt rewrite is warranted.

## SVC-3: Use the existing request-size limit in pricing bounds

**Confirmed. Duplicated contract constant.**

Evidence:

- `pkg/teamapi/wire/wire.go:64` owns `MaxVoyageInputs = 1000`.
- `internal/traffic/validate.go:164` correctly uses it to admit Voyage requests.
- `internal/providerbudget/prices.go:36` separately hardcodes `inputs <= 1000`; `:51` separately supplies `1000` when reporting the largest reviewed request.

Fix: use `wire.MaxVoyageInputs` in both pricing locations. The package already imports `wire`, so no new dependency or helper is needed. Keep literal boundary expectations in tests; they are independent contract checks. Update the validation error's human-readable bound only if the actual limit later changes, not as part of this behavior-preserving pass.

Reach: one source file. Verification: existing `internal/providerbudget` boundary/reservation tests and traffic request-limit tests. Risk is low with the current constant; review pricing assumptions separately before any future change to the limit.

## SVC-4: Remove unconsumed helper and redundant local forwarding

**Confirmed. Small surplus.**

- `internal/admin/admin.go:313-315`: private `negative` has no references in production or tests. Delete it. Actual admin limit validation calls `config.Within`; retain that trust-boundary validation.
- `internal/traffic/traffic.go:561-564`: private `clientVersion` only forwards to `core.ClientVersion` and has one caller at `:558`. Call `core.ClientVersion` there and delete the wrapper and its restating comment. Do not substitute the auth package's `clientVersion`: it intentionally has different semantics because it feeds version enforcement rather than sanitized diagnostics.

Reach: two source files; no tests or exports require migration. Verification: existing admin and traffic/diagnostics tests. Do not add tests for trivial deletion.

## Optional file placement, lower priority

`store/dynamodb.go:144,167,182-183` calls shared `encodeRuntime`, `decodeRuntime`, and `applyRuntime` housed in `store/sqlite.go:364,380,420`. Moving those unchanged functions to `store/runtime.go` makes their backend-independent ownership explicit. This is a same-package mechanical move, not an abstraction. It is useful if those functions are touched during this pass but not necessary to justify an independent project. Keep SQLite-only `readRuntime` in its adapter. Existing provider-config contract tests cover detached maps, partial updates, and both backends.

## Comment and test cleanup assessment

- Remove the narration accompanying the deleted placeholder and forwarding helpers through SVC-1/SVC-4.
- Keep comments documenting upstream billing assumptions, no-resend behavior, lease fencing, version enforcement, bounded response parsing, local-vs-production differences, and SQLite/DynamoDB transaction semantics. These record requirements the implementation alone does not establish.
- `internal/traffic/models_test.go:46` sleeps for 100ms to let concurrent callers arrive. It is a candidate for a deterministic concurrency proof, but no failure was reproduced and this audit does not claim it is flaky. The subsequent cache can allow delayed callers to pass without demonstrating pending-request sharing. Do not delete the test or merely increase its sleep; improve it only with an observable synchronization argument that still catches loss of coalescing.
- No useless or failing test was proven by this source-only pass. Keep shared store/ledger contract suites and real HTTP stack tests.

## Reviewed and retained boundaries

- SQLite and DynamoDB need different transaction mechanisms. DynamoDB's revision checks, retries after definite conflicts, lease deadline recheck, immutable revocation generations, and paginated reads are requirements, not surplus special cases.
- `core.RuntimeSource` and `core.UsageReader` are useful narrow contracts. SVC-1 removes only the unnecessary traffic wrapper, not either interface.
- Shared credit accounting and provider currency accounting both remain contractual (`team-api-gateway.md:217-219`). Do not delete old credit fields as presumed legacy.
- Separate auth/paid-body readers protect different payload contracts and timing bounds. Do not merge their JSON or request-body policy blindly.
- Auth's unsanitized version extraction is used to decide client eligibility. Core's sanitized version extraction protects diagnostics. Their similar names do not prove equivalent requirements.
- The usage response scanner and full JSON validator have distinct jobs: bounded extraction and rejecting ambiguous/malformed response grammar. Keep both until a replacement proves the same security and streaming constraints.
- `dynamotable.ID` is live in the DynamoDB Local table fixture; `newOwner` provides a storage-neutral random owner ID. Do not pull `dynamotable` into the SQLite ledger to deduplicate a few cryptographic-random lines.
- Process-local concurrency during canary overlap is explicitly accepted for the pilot (`team-api-gateway.md:224`). Shared distributed slots are future scope, not part of this refactor.

## Evidence limits

The compounding lookup reached the CLI but could not access the Atomic Object server (`unlock the system keychain or run rzm auth login`). It returned no current shards; this is an integration evidence gap, not proof that no prior context exists. No credentials were read. This audit used the checked-in service README, governing spec, and source/caller searches. It did not execute tests or audit deployments, provider prices, or live traffic.
