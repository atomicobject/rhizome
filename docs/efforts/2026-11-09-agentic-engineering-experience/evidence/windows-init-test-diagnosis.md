# Windows init settings-test diagnosis

[Windows cmd-validate](https://github.com/atomicobject/rhizome/actions/runs/42784627232/job/129722711020) failed `TestInitCommandJSONReportsAndChangesAConfiguredFoldersSettings` at its first setup call on documentation head `dab298fa609c5a183262c5e3ec114255c27ca5a2`. That head changed only effort/evidence records. Application head `301faea690e22c0ec93337d652c776a4b5df9218` had already passed Windows CI and manual review.

## Verified mechanism and evidence gap

The test supplied `--workflow none --search off`, but seeded no pinned executable. `runInitJSON` calls first-run `Apply`, which installs the pinned release after setup. Search off controls embeddings. `RZM_SKIP_REPO_DELEGATE=1` does not disable installation. A fresh ordinary repository pins the current release, so this test could contact GitHub, download an archive and execute its binary.

Both setup errors and a nonempty `result.Pin.Error` return `silentExitError`. The first invocation discarded stdout, where the structured error is encoded, and asserted only against empty stderr. The original job therefore cannot identify which setup or install operation failed. No claim is made that its error was specifically HTTP 404, an artifact issue or a Windows filesystem failure.

A Codex GPT-6.1 Sol high read-only diagnosis independently traced this call path and recommended capturing JSON before changing production behavior. It also found that the command does not pass its invocation Env into init and the installer uses a background context. Neither is established as the original cause; neither production path changes here.

## Controlled red and green

Root changed the test to preserve JSON diagnostics and point `RZM_UPDATE_MANIFEST_URL` at a local `httptest` server returning 404. The override uses the supported `/releases/latest` suffix, so version lookup stays on that server. An earlier bare-host override fell back to the public release endpoint and passed; that invalid setup is excluded from the regression evidence.

The unchanged first setup then failed locally in 0.07 seconds. Its captured JSON included:

```text
"pin":{"version":"v0.51.0","error":"install pinned Rhizome binary: manifest stage failed: fetch GitHub release: 404 Not Found"}
```

The minimal fixture correction calls the existing `seedCurrentPinnedBinary` helper before setup, as neighboring init-command tests do. The failed release service remains in the test. Every existing configured-state, plan, write-summary and generated-skill assertion remains. First-run, plan and apply failures retain stdout and stderr in their assertion messages. Production installer behavior is unchanged. Removing the seed reproduces the failed manifest dependency deterministically.

The red evidence is retained locally in `/private/tmp/ae-windows-init-isolation-red.log`; the invalid-route attempt is kept separately and is not counted as proof. All `^TestInitCommandJSON` cases passed locally in 0.801 seconds with the unavailable release service retained. A fresh Codex GPT-6.1 Sol high read-only review approved the narrow fixture correction with no verified issues; it inspected the helper, marker fast path, callers and retained red/green logs. `make check` and documentation/frozen-scope validation also passed. Current-head manual review and Windows CI remain required before readiness. No retry, timeout increase or skip is introduced.
