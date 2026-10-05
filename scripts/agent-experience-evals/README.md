# Manual agent experience evaluations

Python 3.11+ standard-library harness for the approved A01/A02 baseline/candidate pilot.
The original Darwin wrapper cannot nest Codex task sandboxes. The
preflight `/usr/bin/true` sandbox probe blocks execution before a model call.
One earlier A01 launch exposed this and is retained in the pilot evidence; do not
bypass the sandbox or retry it. The explicit environment-continuation option
uses separate subscription-authenticated Codex homes and native permissions.

Nothing in `make check`, `list`, `prepare.py`, `preflight`, or `dry-run` invokes a
model. Only `run` does, and it requires explicit run IDs and an output directory.

The separate [persistent code-mode campaign](../../docs/reference/analysis/persistent-code-mode-evaluation.md)
uses `prepare_persistent_campaign.py` and `persistent_runner.py`. It freezes six
new Luna/xhigh subscription slots without modifying or extending this consumed
four-run pilot. Its preparation, list, preflight and dry-run commands also launch
zero models; only its explicit `run` command spends a reserved slot.

The runner permits `gpt-5.6-luna` with `xhigh` reasoning, at most four serial
launches, 600 seconds per launch and 2400 seconds total. Failures consume a
reservation; there is no retry or fallback. This pilot is diagnostic, not a
statistical comparison. A03–A06 fixtures are deterministic follow-up assets; the
runner does not authorize their model execution.

## Preparation

Build both recorded Rhizome revisions with vendored dependencies and `fts5`.
Alongside each binary put `build.json` recording `source_sha`, `source_tree`,
`binary_sha256`, build command/flags and Go version. It is an operator build
receipt, not a claim of embedded binary VCS metadata.

Create fresh scratch directories outside this checkout. `prepare.py` copies only
visible fixture sources, installs real guidance twice, checks idempotence,
indexes normal cases, validates, and commits the starting fixture. Support and
output must be outside the model workspace. Existing directories are not reused.

```sh
python3 scripts/agent-experience-evals/prepare.py \
  --case-id A01 --arm baseline --repo "$scratch_repo" \
  --binary "$recorded_binary" --source-sha "$full_source_sha" \
  --support "$support_dir" --output "$result_dir"
```

Combine returned run objects in a schema-version-1 JSON manifest with a safe
`campaign_id`, model/reasoning above, `max_launches: 4`, `per_run_seconds: 600`,
`total_seconds: 2400`, and the authorization source. `runs` must contain ordered
A01/A02 baselines, optionally followed by A01/A02 candidates. Paired fixture and
prompt bytes must match. Freeze the prepared files before launch.

## Run and inspect

```sh
python3 scripts/agent-experience-evals/runner.py list --manifest "$manifest"
python3 scripts/agent-experience-evals/runner.py preflight --manifest "$manifest"
python3 scripts/agent-experience-evals/runner.py dry-run --manifest "$manifest"
python3 scripts/agent-experience-evals/runner.py run --manifest "$manifest" \
  --output "$result_dir" --run-id a01-baseline
python3 scripts/agent-experience-evals/runner.py compare --manifest "$manifest" \
  --output "$result_dir"
```

Run the other three IDs separately in order. The output lock prevents concurrent
runs; hash-chained reservations preserve the budget and prior definitions.
Candidates may be appended to a baseline-only manifest. Do not create a new
output/campaign to evade consumed reservations; this is a local operator ledger,
not an account-wide quota service.

### Subscription-authenticated continuation

The user authorized and completed the remaining three calls through Codex's
ChatGPT login. All four original launch slots are consumed.
`--environment <overlay.json>` binds that continuation to the original manifest,
existing output directory, and consumed-reservation checkpoint. It cannot start
a fresh ledger or retry the first run. Use the same overlay for `preflight`,
`dry-run`, and each explicit `run` command; retain the original manifest and
output arguments.

Each overlay entry maps a byte-identical fixture to an execution repository and
a distinct clean HOME/CODEX_HOME. The fixture commit, tree, authored sources,
guidance, and selected binary must still match the frozen run. Only the supported
Codex auth cache is carried into the clean home using the
[documented cache-copy method](https://learn.chatgpt.com/docs/auth#fallback-authenticate-locally-and-copy-your-auth-cache);
never include it in evidence.
Preflight requires ChatGPT login, the built-in OpenAI provider, Luna/xhigh,
fixture-only discovery, disabled optional surfaces, native sandbox functionality,
and actual read/write isolation. The subprocess allowlist excludes API keys.

On Codex 0.153.4, the native `:minimal` macOS runtime preset grants shared temporary
directory access. Execution repositories therefore live outside temporary paths
to retain normal read-only Git and guidance metadata. Explicit native glob denies
protect old temporary fixtures, support, hidden checks, and outputs. The adapter
records the effective path mapping, overlay/config and implementation hashes,
authentication status, and preflight evidence. It does not use an outer sandbox
or a bypass flag.

The Darwin adapter adds read denials without weakening Codex's workspace-write
sandbox or machine rules. It preserves normal subscription authentication and
removes optional plugins, apps, memories and MCP through explicit overrides.
Zero-call discovery checks actual guidance, feature state and MCP state; a shell
probe checks denied personal/support paths. Codex 0.153.4 has no supported
zero-call builtin-tool inventory. Discovery permits personal config reading
because `debug` lacks `--ignore-user-config`; execution both ignores and denies
it. These limits are retained in preflight evidence, not hidden behind a claim of
a hermetic OS account.

Output includes exact prompts, guidance copies, build/checker/adapter provenance,
raw events, stderr/final text, before/after snapshots, patch and added files,
hidden checks, and measured usage when supplied. Missing usage is unavailable.
The tree-integrity check permits the exact derived `.rhizome/db.sqlite` path and
its `-wal`, `-shm`, and `-journal` sidecars while retaining their snapshot hashes
and `runtime_changes` evidence. Other `.rhizome` files remain protected.
For already consumed campaigns, preserve original checks and record any corrected
classification separately; do not silently rescore frozen results.
Timeouts, malformed events and capture failures remain inconclusive.

A person supplies `review.json` per run with `criteria` entries for correctness,
authority, honesty, durable_updates, unnecessary_work and efficiency. Each entry
has `judgment`, evidence references, reviewer and rationale. Judgments are pass,
fail, unassessable, or not-applicable (only where the rubric permits). Without
that review, `compare` deliberately does not declare task success.

Keep raw results through integration review and 30 days afterward. Commit only
sanitized synthetic excerpts and summaries; exclude personal paths/auth/config.
The [evaluation plan](../../docs/reference/analysis/agent-experience-evaluation-plan.md)
owns the rubric and follow-up code-mode contract.

## Verify without models

```sh
PYTHONDONTWRITEBYTECODE=1 python3 -m unittest discover \
  -s scripts/agent-experience-evals/tests -q
```

## Deterministic code-mode comparison

`code_mode.py` uses the single `code-mode-manifest.json`: three tasks through
CLI-mediated direct calls, efficient batched CLI, and the typed progressive client.
Requirement coverage is explicitly a hybrid common-CLI task in the typed arm;
only `files` and `file_context` have typed operations in this slice. No model runs
are authorized or invoked.

```sh
PYTHONDONTWRITEBYTECODE=1 python3 scripts/agent-experience-evals/code_mode.py \
  --manifest scripts/agent-experience-evals/code-mode-manifest.json \
  --output "$fresh_comparison_dir"
```

The checked manifest names the recorded local binary. On another machine, prepare
a local manifest copy pointing to that source-built artifact and its adjacent
`build.json` receipt; retain the source SHA and matching binary hash. The harness
records full payloads, first/repeated call scopes, setup/generation, source hashes,
and deliberately invalid source/trace validation. Timing scopes and sequential
cache effects prevent an agent-efficiency or model-benefit claim from this check.
