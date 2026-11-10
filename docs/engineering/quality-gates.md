# Quality gates

Consulted when running verification and when deciding what is safe to run without asking. Used during implementation, quality gates, and closure.

## Commands

During editing, run the affected layer's checks and focused tests after a coherent batch of changes; use [testing policy](testing-policy.md) to select them. Use `check-fast` for broader feedback when needed. The final gate requirements below still apply.

| Gate | When | Command |
| --- | --- | --- |
| Fast checks | broader verification between completed edit batches | `make check-fast` (gofmt, Go unused/SA analysis, file-size ratchet, `go vet`, repository content, Greptile config, web lint and typechecks) |
| Focused tests | changed package or bug fix | `GOCACHE=$PWD/.gocache go test -tags fts5 ./pkg/<path>/...` or the same for `./cmd/...`; the Makefile keeps `GOCACHE` in the repository's `.gocache`, so sandboxed agents set it the same way to write and reuse that cache |
| Pre-commit verification | before committing Go or TypeScript changes | `make check` (`check-fast` plus Go unit tests without the race detector and web unit tests; about a minute) |
| Full verification | CI runs it on every pull request and is the merge gate. Run it locally before a release, before effort closure without a pull request, or when a change touches concurrency, the vault runtime, or indexing | `make check-full` (adds race-enabled unit tests, integration tests, and benchmark contracts) |
| Documentation validation | changed Markdown, docs, or agent guidance | `./scripts/rzm validate` (`rzm validate fix` auto-fixes safe findings); `./scripts/rzm validate frozen-scope-drift` when specs or efforts changed |
| Generated surfaces | changed init templates or skills | `make build`, then `rzm init`, then `rzm init --check`, which must exit 0 |
| Web end to end | changes to the web experience | `make web-e2e` |
| Combined verification | full code checks and browser tests in one checkout | `make verify` (builds the CLI and web assets, runs `check-full`, then `web-e2e`, stopping on failure) |
| Desktop verification | changes to `desktop/` or the desktop bridge | `make desktop-deps`, then `make desktop-check` and `make desktop-build` on macOS; exercise the packaged app with a real fixture folder |
| CI test jobs | pull requests and `main` | Linux runs unit and integration tests with `-race`; Windows runs them without it, since the race detector is OS-agnostic and the Windows job exists for path and file-locking behavior. Go jobs are skipped when a pull request only touches `web/` |
| CI content check | pull requests | `rzm ci` runs the default validation suite read-only and is not a substitute for `make check` |
| Repository content contracts | changes to shipped guidance, recipes, or repository schemas | `make test-content` runs the explicitly tagged content suite; package unit tests use synthetic fixtures |

## Safe to run without asking

For a fresh checkout, run `mise install` for the pinned Go, Node, and credential scanner, then `cd web && npm ci && npx playwright install chromium webkit`. Return to the repository root. `make verify` establishes a full baseline when one is needed; choose an individual task's required checks from the table above. Reuse an installation only in the same checkout when its lockfile, runtime version, and installed dependencies are known unchanged since the successful install; otherwise reinstall. Enable commit checks with `make hooks-setup` before committing. `make verify` builds the CLI used by runtime fixtures and orders the gates, while preserving parallel checks inside `check-full`. Do not run another build or verification command in the same checkout concurrently: E2E rebuilds the assets that Go embeds.

Every command above is local, uses disposable fixtures, and touches no production system. Run them, fix failures caused by the requested change, and rerun the affected gate without asking at each step. `make cut-release`, anything that pushes, and anything that publishes to GitHub, S3, or Homebrew are not on this list.

## Reusing verification

`check` includes `check-fast`. `check-full` supplies the unit-test coverage with the race detector and includes the fast checks, web tests, credential checks, team API checks, and integration suites. `verify` adds the build and browser suites to `check-full`. A successful containing gate satisfies its included checks; do not chain these gates on unchanged inputs. Separate documentation validation, init checks, desktop checks, and the CI merge gate still apply when required. A focused race run covers only its selected tests.

Reuse a completed result that exited zero only when its checks ran after the last edit or generation of their inputs, with no concurrent edits. Source, generated output, generator inputs, dependencies, toolchain, and relevant environment must remain unchanged afterward. Confirm that the command's build tags, flags, and test selection cover the required check; otherwise rerun it. Report Go results marked `(cached)` as cached; the [testing policy](testing-policy.md) identifies when fresh execution is required.

## Evidence

Record the exact command and result in the effort. A failed or unavailable gate is evidence, not a pass: record the failure, why it matters, and who or what resolves it. Validation findings are treated like failing tests: apply safe fixes, then classify what remains as `needs decision`, `historical/frozen`, `unrelated/pre-existing`, or `out of scope`. Never rewrite `complete` or `archived` effort history to silence validation; historical repair uses `--allow-historical` and is recorded in the owning effort, and CI never uses it.

## Team extensions

`rzm ci` exits 0 when clean, 1 for findings, and 2 for configuration or prerequisite failure; its findings carry stable issue codes and the exact `rzm validate fix` command. Pull-request workflows run for child PRs targeting an integration branch, not only PRs whose base is `main`.
