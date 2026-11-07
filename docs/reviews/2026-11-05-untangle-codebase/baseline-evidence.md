# Baseline verification

Revision: `900eee0b0036ee2699d84d5d02d5ffe6a39a4441`. No application code, tests, schemas, or configuration changed before these commands.

## Build

`make build` built the web assets and `bin/darwin/rzm` (v0.50.5), then exited 2 when sandbox DNS prevented downloading dependencies for the nested team API module. Running `make team-api-build` with dependency-download access exited 0. Generated changes to tracked web HTML were restored to their starting bytes.

## Baseline gate

`make check` exited 2. The root Go unit target failed in `pkg/harness/internal/jsonstdio`:

```text
--- FAIL: TestImmediateDecodeFailureHasInitializedProcessIdentity
    transport_test.go:120:
        Received unexpected error:
        context deadline exceeded
```

The failure occurs in `connection.Close(closeCtx)` with a 300 ms context after receiving the expected decode failure. Production close records descendants, closes input, waits a bounded grace period, kills if needed and waits for process reaping. Source inspection alone does not establish whether the failure is production cleanup behavior, the fixture's synchronization, or load sensitivity.

The same command passed web lint/typechecks, repository content checks, Go vet, secret checks, and the team API unit/stack/deployment-script suites before it stopped. Other root Go packages reported success. The command did not reach its separate web-unit-test step. DynamoDB Local execution was not established by this default gate.

Diagnosis belongs to the approved test/refactor work. Preserve the process-identity assertion and cleanup guarantees; a successful rerun is counterevidence, not a fix.

A focused diagnostic rerun, `GOCACHE=$PWD/.gocache go test -tags fts5 ./pkg/harness/internal/jsonstdio -run '^TestImmediateDecodeFailureHasInitializedProcessIdentity$' -count=20`, passed. Each test itself repeats the scenario ten times. This narrows the evidence toward a timing/load-sensitive failure; it neither establishes the cause nor resolves the original gate failure.

## Separate web baseline

`make web-test` ran 203 files: 202 passed, with 1,828 passing tests and four failures in `devProxyConfig.test.ts`. All four failures accompanied `listen EPERM: operation not permitted 127.0.0.1` from the sandbox. Running `./node_modules/.bin/vitest run devProxyConfig.test.ts` from `web/` with local socket access passed all seven tests in that file. This resolves the observed web failures as a permissions issue; it is split baseline evidence, not a single successful `make check` run.

## Planning integration

`./scripts/rzm agent start` succeeded. Direct current-user inspection reported `missing_current_user`; no identity was inferred or configured. Typed runtime-authoring queries through code mode timed out once with the normal deadline and again with local runtime permission and a longer diagnostic deadline. Compounding lookup also reported unavailable Atomic Object gateway authentication. No typed spec, identifier allocation, frozen effort, or plan approval was fabricated.

Follow-up outside the sandbox verified a ready headless runtime with active background indexing. The direct ontology authoring guide for TechnicalSpec and EffortNote succeeded with socket access. A subsequent code-mode `nextId` request still exceeded its 120-second call deadline, so no returned identifier was available and no typed spec or effort was authored. Documentation validation remains incomplete. The earlier sandbox status report that no runtime was reachable must not be interpreted as proof that the runtime process was absent.

Ordinary review artifacts were written from checked-in policy and source. No implementation approval has been inferred from automatic goal continuations.
