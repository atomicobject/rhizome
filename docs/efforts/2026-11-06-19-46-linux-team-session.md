---
type: EffortNote
id: EFF-2026-11-06-19-46
name: Linux team session storage
created-at: 2026-11-07T00:47:44Z
status: complete
summary: Enable Atomic Object sign-in on Linux through the native Secret Service keyring.
aliases: [EFF-2026-11-06-19-46]
governing-specs:
  - "[[team-api-gateway]]"
---

# Linux team session storage

## Scope

Implement the Linux secure-store adapter, verify it with synthetic credentials on this machine, and rebuild and reinstall Rhizome Dev. Windows support, provider routing changes, publication and merging are excluded.

## Spec Set (Frozen)

- [[team-api-gateway#Local session store]] at bab4963737e6721225a5fae8007f1d0e58e4d276: one record per OS user and origin, no plaintext fallback, typed unavailable errors, existing cross-process lock and record-version protection.

## Stories In Scope (Frozen)

Requirements-only slice: [[team-api-gateway#Local session store]]. No whole-story delivery is claimed.

## Spec Coverage Checklist

- [x] Native per-user/per-origin secure storage and record replacement/deletion.
- [x] Private secret transport, trusted service selection, bounded errors without fallback.
- [x] Synthetic native acceptance and installed desktop account status.

## Plan

The user approved the preceding chat plan with “Yes”: implement Linux Secret Service storage, verify save/read/update/delete with synthetic credentials, and rebuild the installed app. Identity is not configured, so no approver identity is inferred.

One delivery batch owns `pkg/teamapi/session`, platform documentation and the local installed desktop companion. Use system libsecret's fixed `secret-tool` executable, secret-free arguments and a trusted OS-user D-Bus environment; preserve the Store interface, opaque record format and refresh lock. Distinguish absent records from locked or unavailable service. Do not expose helper output. Verify successful writes and deletes.

Exit evidence: focused session tests, opt-in native synthetic round trip and failure checks, independent review, appropriate repository checks, documentation validation, CLI rebuild and installed desktop sign-in readiness. A real browser login remains the user's action. Escalate only if native secure storage cannot meet the existing contract; never substitute a plaintext file.

## Original Intended Delivery

Make Atomic Object sign-in usable in the installed Linux desktop without weakening credential storage.

## Actual Delivered

Implemented the Linux adapter and installed the rebuilt desktop companion and standalone source-checkout CLI. The installed `rzm auth status --json` now reports signed out at the configured team origin without a secure-store error. Rhizome Dev relaunched successfully and rendered its shell. Browser authentication remains the user's action. Source delivery is on `linux-secret-service`. Earlier full verification passed; the final combined pre-commit suite timeout is recorded below.

## Execution Notes

- 2026-11-07T00:47:44Z [decision] User approved implementation and local reinstall in chat.
- 2026-11-07T00:47:44Z [learning] The desktop's GNOME keyring and Secret Service already run; the missing adapter, not machine setup, caused the error.
- 2026-11-07T00:47:44Z [review] Independent review identified environment-based D-Bus/backend redirection and locked records that secret-tool reports like missing items. Both are being addressed before native acceptance.


- 2026-11-07T00:52:58Z [validation] `TEAMAPI_SECRET_SERVICE_TEST=1 GOCACHE=$PWD/.gocache mise exec -- go test -race -tags fts5 ./pkg/teamapi/... -count=1` passed. Native unique synthetic records verified read, replacement, origin isolation, independent helper processes and idempotent deletion. A real helper on an unavailable bus returned the typed unavailable error. Synthetic items were removed.
- 2026-11-07T00:52:58Z [review] Independent read-only implementation review found no blocking issues. Adopted additional HOME/runtime override tests and documented the standard OS-user bus requirement.
- 2026-11-07T00:52:58Z [validation] Independent native experiment used `dbus-run-session` with temporary account/data/runtime/control directories and a private `gnome-keyring-daemon --foreground --unlock --components=secrets`. Locked synthetic search returned metadata without the secret; clear returned exit 1 with empty stderr and left the item present. This verifies the native behavior covered by the adapter's locked-item regression tests. The real user keyring was untouched, the private daemon stopped, and temporary files went to Trash.
- 2026-11-07T00:52:58Z [validation] `mise exec -- make check-fast` passed with zero issues. `PATH=$HOME/.cargo/bin:$PATH mise exec -- npm --prefix desktop run rzm` and `NO_WEB=1 mise exec -- make build` passed. Atomically replaced the installed companion and gateway, then relaunched the unchanged native shell. The installed CLI returned `signed_in: false` without the former error.

- 2026-11-07T00:57:15Z [validation] `mise exec -- make check-full` passed: race-enabled repository and team API tests, 244 web test files / 1,996 tests, content/credential/benchmark checks and both integration targets. `./scripts/rzm validate` and `./scripts/rzm validate frozen-scope-drift` each passed with zero issues. `git diff --check` passed. Generated web index build churn was removed; the installed binaries retain the verified build.

- 2026-11-07T03:43:20Z [PR preparation] Independent review of the combined Linux changes found no blocking issues. Rechecked `RZM_GTK_SHORTCUT_TEST=1 cargo test --locked --manifest-path desktop/src-tauri/Cargo.toml` (112 passed), `TEAMAPI_SECRET_SERVICE_TEST=1 GOCACHE=$PWD/.gocache mise exec -- go test -race -tags fts5 ./pkg/teamapi/...` (passed), and `mise exec -- npm --prefix desktop run check` (117 frontend tests plus auxiliary checks passed). Final `mise exec -- make check` failed at unchanged `ExplorerWorkspace.layout.test.tsx:77` waiting for the initial folder graph: 1,995/1,996 web tests passed. The isolated test passed, which does not diagnose or fix the full-suite timeout. No retry or timeout increase was added. Greptile is installed but not signed in; its required review and full-suite verification remain open before merge. Open a draft PR and retain active status until these gates are resolved. Native macOS execution is unavailable on this host; use the PR desktop job and subsequent visual acceptance.

- 2026-11-07T03:44:57Z [delivery] Committed implementation as `2d432b56` and opened [draft PR #110](https://github.com/atomicobject/rhizome/pull/110). `greptile review --json` on the committed branch exited with a sign-in prerequisite error. Default and frozen-scope validation passed after the PR-preparation updates; the staged credential scanner found no leaks. Keep this effort active for the recorded verification gaps.

## Deviations

None.

## Closure Checklist

- [x] Approved storage contract implemented and reviewed.
- [x] Checks and native acceptance recorded.
- [x] Installed desktop refreshed and remaining user action explained.

## Compounding Follow-ups

No shard proposed; retain the platform procedure beside the adapter documentation.

## Status

Complete. Delivered on `main` in PR #110 (`fb5583449`, merged 2026-11-07). Drew closed this effort on 2026-11-12 after an audit of open efforts. Unchecked items above are carried forward under that decision rather than verified at closure. Follow-up: native Mac visual acceptance.
