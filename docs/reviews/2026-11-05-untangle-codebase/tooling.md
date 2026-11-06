# Untangle audit: tooling, init, and compounding

Source revision: 900eee0b0036ee2699d84d5d02d5ffe6a39a4441. Read-only audit; no production files changed. The requested Comments and Tests passes are included below. Findings marked CONFIRMED describe source structure; runtime implications are separated where untested.

## Recommended fixes, grouped above their evidence

### T1. Use the existing Markdown protection parser when reading skill markers and expanding template embeds

**CONFIRMED hidden coupling; medium priority.** Keep `pkg/markdownsyntax` as owner of Markdown code-region semantics. Make init's embed expander and skilloverlay's two marker passes consume protected regions from that package instead of each keeping another fence state machine. Preserve byte offsets, source bytes, marker validation, and template-only expansion. No new parser or dependency is needed.

Evidence:

- `pkg/app/cli/init/helper_template_embeds.go:22-70` carries `inFence`, `fenceChar`, `fenceLen`, `inCodeSpan`, and `codeSpanLen` and implements its own transitions before recognizing embeds.
- `pkg/skilloverlay/parse.go:14-36` independently tracks fences for slot parsing; `:80-104` repeats those transitions a second time for leftover-marker detection.
- `pkg/markdownsyntax/protected.go:16-41,133-170` already owns merged fenced/indented/inline code regions and matching backtick runs. Its closing fence requires whitespace after the marker, while `FenceLine` (`pkg/markdownsyntax/line.go:20-42`) reports a fence based on marker run alone and both consumers close immediately on that result.
- Consequently the three consumers must change together for a Markdown protection correction, but the interface expresses only a fence-looking line. A line such as triple backticks followed by non-whitespace inside a fenced example is treated differently by the canonical parser and these consumers. This consequence is source-derived, not runtime-tested in this audit.

Reach: `helper_template_embeds.go`, `pkg/skilloverlay/parse.go`, existing parser if a narrow exported protection iterator is truly needed (prefer current `ProtectedSpans`/`Contains`); init embed tests and skilloverlay render tests. Callers of `expandAgentTemplateEmbeds` and `skilloverlay.Render` need no API change. Keep `pkg/skilloverlay/CONTEXT.md` consistent. If observable parsing changes, capture the precise behavior in owning guidance rather than claiming this is a purely mechanical move.

Verification: existing `TestExpandAgentTemplateEmbeds_*`, `TestSkillOverlayRenderIgnoresMarkerLikeTextInFencedCode`, slot validation cases, plus literal-output cases for a false closing fence, unmatched backticks, escaped backticks, nested/list code and CRLF. Run focused Go packages after the root build finishes. Confirm embedded installed guidance bytes before/after for ordinary templates.

Comments pass: remove the transition narration at `helper_template_embeds.go:33,48,56` with the state machine. Preserve the explanation that executable embeds are restricted to prose and the template source bindings.

### T2. Resolve the compounding executable once through the existing process boundary

**CONFIRMED hidden coupling; medium priority.** Put one `resolveRzm(root)` helper beside the subprocess client in canonical `templates/starters/compounding/module/scripts/lib/calls.mjs` (or an existing appropriate boundary file) and use it from all compounding entrypoints. Preserve explicit `RZM`, then repository `bin/rzm`, then `PATH` fallback. Leave caller-injected fake clients untouched.

Evidence:

- `.rhizome/modules/compounding/scripts/lookup.mjs:36`, `mine.mjs:487`, `task-check.mjs:57`, and `shards.mjs:139` repeat the same executable-selection expression.
- `calibrate.mjs:140` uses only `process.env.RZM || "rzm"`, so it already differs from the other commands in a repository with a `bin/rzm` launcher and no global executable.
- `lib/calls.mjs:66` owns subprocess calls, but callers independently choose the binary. A change to launcher discovery currently requires five unrelated edits.

Reach: canonical starter versions of those five entrypoints and the selected helper owner; regenerate installed copies using authorized init flow rather than editing `.rhizome/modules` by hand. Existing `pkg/app/cli/init/testdata/compounding/` script harness and `TestCompoundingScripts` should exercise fake `RZM`, repository launcher, PATH fallback, and calibration. No provider requests or paid evaluations are needed.

Risk: launcher authority must remain explicit. Do not silently add global installation, broaden trust, or discover another checkout. Merely share the existing resolution policy and eliminate calibration's divergent fallback.

### T3. Return the complete finding projection from its one loader

**CONFIRMED hidden coupling and surplus; medium priority.** Extend `loadFindings` to produce action text and assignee from the same note bytes it already parsed. Make `workspace.mjs` project those returned fields. This removes a second parser and file read, while keeping the source note authoritative.

Evidence:

- `lib/findings.mjs:142-170` reads each note and parses its action checkbox to determine `open`.
- `workspace.mjs:32` declares another action-item regex; `:58-67` calls `loadFindings`, then rereads every selected note solely for action text/assignee.
- The workspace row can therefore combine the first read's summary/status with a later read's action text, and it owns a second knowledge of the same note format.

Reach: canonical starter `lib/findings.mjs`, `workspace.mjs`, existing findings/workspace tests. Preserve handling of missing action items, checked uppercase/lowercase boxes, CRLF, missing assignee, teammate notes, and deterministic sorting. Adjust `findingRow` to take just `f`, eliminating its filesystem dependency. Keep completed-at Git history logic and all action-item publication permissions unchanged.

Verification: existing workspace/findings tests plus one fixture asserting the same loader supplies summary/status/action/assignee. A single well-chosen read snapshot assertion is preferable to implementation-count tests.

### T4. Route every dev backend restart through the same ready-and-reconcile lifecycle

**CONFIRMED needless split; high priority relative to other tooling findings.** Initial start, rebuild, and unexpected exit should use one operation that owns process start, matching ready manifest, resulting origin, and Vite reconciliation. Keep existing bounded waits and graceful/forced shutdown. Do not remove required polling simply because it is polling.

Evidence:

- `scripts/dev_supervisor.mjs:113-123` starts the backend, waits for its PID/fresh manifest, then starts Vite.
- `:158-187` repeats the start/wait/origin sequence for a rebuild and guards concurrent restarts.
- `:213-222` handles a crash by calling `startBackend("backend exit")` directly, bypassing ready waiting, origin update and the restart serialization path.
- A crash restart landing on another port can leave Vite using the previous origin. The bypass is source-confirmed; the actual crash/port scenario was not launched in this audit.
- `web/devSupervisor.test.ts` currently covers config, fingerprints, env parsing, and manifest checks. It does not exercise the supervisor process lifecycle.

Reach: `scripts/dev_supervisor.mjs`, its tests, and possibly `web/devSupervisorRuntime.ts`. Prefer a native Node test for this Node script over growing the test-only TS adapter. The adapter currently re-declares all exported JS contracts with `as` at `web/devSupervisorRuntime.ts:1-47`; moving the existing helper tests to Node would remove this surplus boundary rather than adding new assertions to it. Coordinate with the web lane.

Verification: a disposable fake backend should publish a fresh matching manifest, exit, restart on a new origin, and prove the consumer receives that origin; verify rapid rebuild requests serialize, shutdown cancels retries, and stale manifests remain rejected. Keep ports and processes disposable and record cleanup. No live app install or production services needed.

### T5. Read the Go version from the module in workflows

**CONFIRMED hidden value coupling; low risk/priority.** Replace duplicated project Go patch-version pins in setup-go steps with its already-used `go-version-file` form and appropriate module path. Keep special signing/scanner `GOTOOLCHAIN` overrides separate because they have a different requirement.

Evidence:

- Root `go.mod:5` sets `toolchain go1.24.2`; `mise.toml:4` separately pins the local tool.
- `.github/workflows/ci.yml:54,101,163,192,231,261,336,391,411`, `release.yml:119,207,288`, `secrets.yml:17`, and `typesafe-live.yml:15` all repeat `1.24.2`.
- `team-api-deploy.yml:70` already uses `go-version-file: source/services/team-api/go.mod`, demonstrating the house pattern. The nested service should select `services/team-api/go.mod` where that is the tested module.

Reach: those workflow setup-go steps and workflow contract tests only if the tests assert the old literal. No new shared workflow, composite action, version file, or download tool is needed. Decide whether `mise.toml` remains the explicitly maintained local pin; changing the local manager is outside this small workflow fix.

Verification: parse workflows, verify every project setup-go step reads the intended module, retain the gitleaks Go 1.24.11 override documented in `release.yml`/`secrets.yml`, and run existing workflow contract tests. Actual GitHub runner execution remains CI evidence.

### T6. Keep release Makefile assertions at the command boundary

**CONFIRMED test coupling; low risk.** Convert plan/dry-run assertions to the existing stub executable pattern and remove duplicate source-spelling assertions whose command behavior is already covered. Preserve tests that protect publication authority, secret isolation, or workflow topology; those are real contracts.

Evidence:

- `scripts/release/release_makefile_test.py:18-25` requires exact source strings and splits the Makefile using neighboring target names.
- `:27-38` already executes `make release` with fake `git`/`python3` and verifies the publisher invocation.
- `:40-49` only checks that phase names and command strings occur anywhere. `:57-75` already executes all those targets and verifies their exact argument arrays, including empty first-release base and missing head.
- Audit experiment in `/tmp`: add one harmless space after the recipe tab for `release-plan`; the old literal is absent, yet `make release-plan` still calls exactly `scripts/release/release_cli.py`, `plan`, with exit 0. No production file was modified. Thus the textual assertion fails on a behavior-preserving formatting edit.

Reach: `release_makefile_test.py` only. Replace plan/dry-run checks with stub runs; remove the phase existence test after confirming its full coverage in the argument test; fold the direct release source check into its behavioral test if extra gate conditions need coverage. Keep source checks for absent private infrastructure strings.

Verification: `python3 -m unittest discover -s scripts/release -p '*_test.py'` after editing. Do not run any real publisher as a test.

## Cross-lane observation

The query lane owns the candidate dependency inversion for `validationrun/prepare.go:12,66` and `validationproduct/projection.go:229` importing `pkg/app/cli/init` only for `ValidateBundledSkillOverlays`. Existing `validate.RunContext.SkillOverlays` already provides the callback seam. Prefer wiring at application composition over relocating template sources or creating another template subsystem. Shared findings should be deduplicated in the root plan.

## Deliberate exceptions and rejected noise

- The standalone install script is long because it is a downloadable single-file installer. Splitting it into separately fetched shell modules would add integrity/version coordination. No split proposed solely for size.
- Generated installed starter copies are expected duplicates of canonical templates. The edit route is source then init/regeneration; treating both as independently owned source is the problem, not their presence.
- Guard shell parsers, fail-open handlers, bounded timeouts, publication receipts, staged ownership checks, and action-item authority are retained. Their documented external requirements justify special cases.
- The identical three-line percentile function in two historical benchmark scripts does not justify a new utility module by itself. Their frozen experiment profiles and launch budgets are evidence, not features to generalize or delete.
- Hash helpers across release, benchmark, and agent-evaluation domains have different contracts. They are not one shared domain merely because each uses SHA-256.
- No tests were flagged solely for being architecture/import checks or examining shipped guidance. Several protect genuine publication/ownership boundaries.
- No actual provider evaluation, release, install, or broad build/test run was performed. The only execution was the isolated Makefile formatting equivalence probe described in T6.

## Coverage and limits

Inspected the CLI/init ownership and rendering seams, skilloverlay, compounding scripts, Makefile, workflow setup/gating, release contracts and test shape, installer/tool inventory, and representative benchmark/evaluation helpers. This is a structural source audit, not exhaustive execution of every script. No credentials or user-global configuration were read. Source-only lifecycle consequences need the focused verification above before they are claimed as reproduced bugs.
