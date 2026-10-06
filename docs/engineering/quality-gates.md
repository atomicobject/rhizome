# Quality gates

Consulted when running verification and when deciding what is safe to run without asking. Used during implementation, quality gates, and closure.

## Commands

| Gate | When | Command |
| --- | --- | --- |
| Fast checks | every changed Go or TypeScript file, in the edit loop | `make check-fast` (gofmt, `go vet`, Greptile config, web lint and typechecks; seconds when warm) |
| Focused tests | changed package or bug fix | `go test ./pkg/<path>/...` or `go test ./cmd/...` |
| Pre-commit verification | before committing Go or TypeScript changes | `make check` (`check-fast` plus Go unit tests without the race detector and web unit tests; about a minute) |
| Full verification | CI runs it on every pull request and is the merge gate. Run it locally before a release, before effort closure without a pull request, or when a change touches concurrency, the vault runtime, or indexing | `make check-full` (adds race-enabled unit tests, integration tests, and benchmark contracts) |
| Documentation validation | changed Markdown, docs, or agent guidance | `./scripts/rzm validate` (`rzm validate fix` auto-fixes safe findings); `./scripts/rzm validate frozen-scope-drift` when specs or efforts changed |
| Generated surfaces | changed init templates or skills | `make build`, then `rzm init`, then `rzm init --check`, which must exit 0 |
| Web end to end | changes to the web experience | `make web-e2e` |
| Combined verification | full code checks and browser tests in one checkout | `make verify` (builds the CLI and web assets, runs `check-full`, then `web-e2e`, stopping on failure) |
| Desktop verification | changes to `desktop/` or the desktop bridge | `make desktop-deps`, then `make desktop-check` and `make desktop-build` on macOS; exercise the packaged app with a real fixture folder |
| CI test jobs | pull requests and `main` | Linux runs unit and integration tests with `-race`; Windows runs them without it, since the race detector is OS-agnostic and the Windows job exists for path and file-locking behavior. Go jobs are skipped when a pull request only touches `web/` |
| CI content check | pull requests | `rzm ci` runs the default validation suite read-only and is not a substitute for `make check` |

## Safe to run without asking

For a fresh checkout, run `mise install` for the pinned Go, Node, and credential scanner, then `cd web && npm ci && npx playwright install chromium`. Return to the repository root and run `make verify`. Enable commit checks with `make hooks-setup` before committing. The combined command builds the CLI used by runtime fixtures and orders the gates, while preserving parallel checks inside `check-full`. Do not run another build or verification command in the same checkout concurrently: E2E rebuilds the assets that Go embeds.

Every command above is local, uses disposable fixtures, and touches no production system. Run them, fix failures caused by the requested change, and rerun the affected gate without asking at each step. `make cut-release`, anything that pushes, and anything that publishes to GitHub, S3, or Homebrew are not on this list.

## Evidence

Record the exact command and result in the effort. A failed or unavailable gate is evidence, not a pass: record the failure, why it matters, and who or what resolves it. Validation findings are treated like failing tests: apply safe fixes, then classify what remains as `needs decision`, `historical/frozen`, `unrelated/pre-existing`, or `out of scope`. Never rewrite `complete` or `archived` effort history to silence validation; historical repair uses `--allow-historical` and is recorded in the owning effort, and CI never uses it.

## Team extensions

`rzm ci` exits 0 when clean, 1 for findings, and 2 for configuration or prerequisite failure; its findings carry stable issue codes and the exact `rzm validate fix` command. Pull-request workflows run for child PRs targeting an integration branch, not only PRs whose base is `main`.
