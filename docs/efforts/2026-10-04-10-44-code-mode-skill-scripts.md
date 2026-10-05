---
type: EffortNote
id: EFF-2026-10-04-10-44
name: Code-mode skill scripts
created-at: 2026-10-04T14:44:46Z
status: active
summary: Serve code-mode typed reads from the vault runtime behind a disk-checked freshness barrier, trim view output, add script input, and teach skill authors to ship code-mode scripts.
governing-specs:
  - "[[code-mode-skill-scripts|SPEC-0115]]"
aliases:
  - EFF-2026-10-04-10-44
---

# Code-mode skill scripts

## Scope

Deliver [[code-mode-skill-scripts|SPEC-0115]] so skills can brief agents quickly with saved code-mode scripts. The work came from a review of the `dai-ip-tracking` skill in Drew's vault, whose briefing route spent most of its time in per-call runtime setup and returned large view payloads.

## Spec Set (Frozen)

- [[code-mode-skill-scripts|SPEC-0115]], authored in this effort from the measured review. It refines [[persistent-agent-code-mode|SPEC-0092]], whose execution-host sentence now points here, and the code-mode routing in [[vault-runtime-coordination|SPEC-0104]], which this effort does not edit.

## Stories In Scope (Frozen)

Requirements-only delivery. All SPEC-0115 requirements are in scope.

## Spec Coverage Checklist

- [x] Typed reads in the runtime with the disk-checked freshness barrier and in-process fallback.
- [x] View `omitCapabilities` / `--omit-capabilities`.
- [x] Script `--input` / `--input-file` exposed as `input`.
- [x] Skill-authoring, code-mode, and views guidance, with regenerated managed copies.
- [x] Before/after measurements on a disposable vault copy.

## Plan

1. Measure the cost of a code-mode typed read and find where it goes before changing anything.
2. Route catalog-admitted typed reads to the vault runtime, keeping freshness, coverage, diagnostics, write authority, and the in-process fallback.
3. Add the view option and script input.
4. Write the skill guidance with one generic example script, and regenerate managed copies.
5. Verify with focused tests, repository gates, and the same workloads on a disposable copy of Drew's vault; independent review; open a PR.

### Authorization

Drew authorized this scope from the drews-vault thread reviewing `dai-ip-tracking`: "Make the improvements and open a PR regardless." The configured current user is absent, so `plan-approved-by` is omitted. The authorization covers implementation, review fixes, and opening the PR, not merging it.

### Approved extension, October 4

Drew approved the combined information-design proposal with "Do it" in T3 thread `435a82b5-5461-4d13-9ede-2a5a2bc14652` at 2026-10-04T16:15:32Z. The SPEC-0115 Readable information design requirements are added to the frozen scope. The extension preserves the existing performance work and adds:

1. Section-backed summaries in the ontology, indexed previews, native views, and bundled group views, with typed section retrieval.
2. Skill guidance and attention signals grounded in requested workflows and existing record responsibilities.
3. A fresh disposable DAI vault migration covering notes, ontology, recipes, views, and scripts together, preserving source meaning and proving replay safety.
4. Focused tests, generated-skill checks, repository gates, a rendered fixture check, independent review, and an updated PR.

The live vault keeps its current schema until the supporting build is adopted. Compatible skill corrections and source-grounded design/perspective updates can proceed now. Merging the PR and replacing the user's main build remain outside this authorization.

## Original Intended Delivery

Saved skill scripts brief an agent in about a tenth of a second from typed reads that still observe every completed write, take explicit input, and can read view rows without the capability catalog.

## Actual Delivered

The tool catalog marks which calls of a local-override operation the runtime may serve. The code-mode host forwards those under shared access; the runtime serves them through the same workflows over its open index and a schema cached by source hash. Before a typed query or recipe run, `LiveRuntime.SyncWatcher` compares every selected note on disk with the persisted metadata rows, applies differences through one watcher batch, and the runtime checks the ontology state against the current schema and notes hash. When it cannot show currency in budget, or an index rebuild is running, the host runs that call in-process as before. After a connection makes a write-capable call, its typed reads stay in-process. `view` runs accept `omitCapabilities`, and `rzm agent code execute` accepts `--input` or `--input-file`, read as `input`. The `rhizome` skill teaches shipping scripts for the activities a skill governs, with a generic orientation example.

The readable-information extension adds singular section summaries across typed GraphQL, indexed previews, native views, and bundled briefings. Optional gaps and age appear as neutral context. Authoring guidance starts with the intended reading and retrieval experience. Typed tooltips omit duplicate tags and file metadata; the DAI schema selects only useful identifying context.

## Execution Notes

- Cost before the change, on a disposable copy of Drew's vault (2,492 notes): `rzm ontology query` spent about 0.69 s building a one-shot runtime with its index opened read-write and 0.42 s refreshing: notemeta read every note twice (0.34 s and 0.14 s) and the ontology currency check scanned the stored projection (0.12 s). Code mode did the same per call under an exclusive per-connection lock, so four parallel queries took 4.2 s. Views read the persisted projection without a refresh; their 0.6 s per call was runtime setup. No history explains why typed reads were local: the catalog marked them local in the initial commit, and SPEC-0104's local-only list names validation, identifiers, current user, note move, and ontology authoring, not typed reads.
- A first barrier that trusted the runtime's watcher returned stale data for a read issued 1 ms after an external write, because filesystem events take roughly 150 ms (coalescing plus hub debounce) to reach the watcher. The barrier now compares disk with the persisted metadata rows and no longer depends on event delivery. The same test returns the written value on every run.
- The first disk check ran the ownership selector on every path (about 90 ms). Stored notes were admitted when indexed, so only unknown paths need it now; the check costs about 40 ms (27 ms discovery, 4.5 ms row query, stats).
- Applying one changed note takes the watcher 0.7 to 1.6 s, and the batch can queue behind another. A 2 s budget fell back to the in-process refresh, which reprojects every note after a change (about 10 s). The budget is 10 s, and a boot catch-up or explicit index returns immediately.
- After an in-process refresh rewrites the metadata rows, the next runtime read sees every note as changed and applies them once (about 1 s); later reads are fast again.
- Measurements are on the same disposable copy of Drew's vault, with the runtime idle, three runs each. Script times are whole `rzm agent code execute` wall time; call times come from inside the script. Outputs of the vault skill scripts and prototypes are identical before and after. Embeddings are off in the copy, so `topic.js` measures its no-semantic path: after the review fix, a semantic query the runtime cannot serve runs in-process, matching the baseline. The vault scripts changed later in the day; rerun on the current versions, both builds produce identical output.

| Workload | Before | After |
| --- | --- | --- |
| One small typed query | 1.06 s script, 0.97 s call | 0.12–0.15 s script, 61–68 ms call |
| Four queries under `Promise.all` | 4.22 s | 0.13–0.17 s (72–101 ms calls) |
| Four queries in sequence | 4.38 s (1.02–1.13 s each) | 0.25–0.31 s (191–234 ms total) |
| Nested single-subject query (prototype) | 1.09 s | 0.12–0.13 s |
| One-call briefing (prototype) | 1.12 s | 0.14–0.17 s |
| `view` list, then run | 605 ms, 597 ms | 23–29 ms, 26–33 ms |
| `view` run payload | 106,101 chars | 19,678 chars with `omitCapabilities` |
| Vault `brief.js` | 1.49–2.58 s | 0.14–0.16 s |
| Vault `subject.js` | 1.45–1.59 s | 0.13–0.15 s |
| Vault `topic.js` (embeddings off) | 0.97–2.05 s | 1.04–1.08 s, served in-process |
| Read 1 ms after an external write | 10.8 s, fresh | 1.7–2.7 s, fresh |
| The read after that | 1.3 s | 42–82 ms |

- Independent review (Claude Fable advisor) found no architectural problem and four barrier gaps, all fixed with tests: a stored note that selection dropped while its file remained (an ignore edit) passed the check; received but unapplied watcher input was ignored when the disk comparison was clean; the boot catch-up check was skipped when disk matched, so an indexer-version rebuild could serve old rows; and the runtime's semantic readiness could differ from the host's providers. Pending note events are now hash-compared against their rows, so a late duplicate event does not force a batch; configuration-class events, resyncs, and dropped stored notes force one.
- Greptile's first review (2/5) found three issues, all fixed. A same-size rewrite that kept the stored modification second could be missed once it was older than the two-second hash window if its event was also missed: the runtime now hash-checks each note once per exact nanosecond stat and remembers the result, so any later rewrite is hashed whatever its age, and unchanged notes cost a stat after the first check. Runtime-read orchestration moved from `cmd/` to `pkg/app/cli.CodeModeRuntimeReads` with a fake-runtime test; `cmd` now only adapts the live runtime. `README.md` documents `--input`, `--input-file`, and `--omit-capabilities`.
- Greptile's second review (3/5) found two more: a pending watcher event for a note now voids its remembered stat, so a rewrite that restores the exact timestamp is hashed; and runtime-served queries pass the executable schema to their dependencies, which current-user lookups need. Both have tests. Its third review (5/5) noted that clearing the remembered stat on every read of a still-deferred event re-read the note each time; the watcher now numbers events per path, and a remembered stat stays trusted until the next event, so each event costs one rehash. The fourth review (5/5) asked that the per-path event counts and remembered stats not grow without bound; each check now prunes both to the notes it selected.
- Guidance: `skill-authoring.md` has a 40-line token budget, so the scripts guidance and example live in a new `references/skill-scripts.md` that it routes to; the reference inventory test lists the new file.
- Verification: `make check-fast` passed. Focused tests passed for `pkg/app/agentapi`, `pkg/app/agentcode`, `pkg/app/bootstrap`, `pkg/app/cli`, `pkg/vault/cache`, and `cmd`. Mutation checks confirmed the write-tracking and same-size-edit tests fail without their code. `make build`, then `rzm init` updated six managed `rhizome` reference copies and created the two `skill-scripts.md` copies; `rzm init --check` exited 0. `TestAmbiguousLinkGitProvenanceUsesSourceBranchAncestryNotAuthorDates` in `pkg/validate/identifierreconcile` fails the same way on a clean `origin/main` (0839691) checkout in this environment, so it is pre-existing and unrelated. The DAI skill's `check-queries.py` passed with both builds.

## Deviations

The brief suggested forwarding typed reads to the runtime with its cached ontology. Forwarding alone could not keep SPEC-0092's freshness guarantee, so the effort added the disk-checked barrier, per-call in-process fallback, and connection write tracking.

## Closure Checklist

- [x] Implementation authorized in chat.
- [x] Changes integrated and verified.
- [x] Independent review completed and findings resolved.
- [x] PR opened and linked: [PR #78](https://github.com/atomicobject/rhizome/pull/78), targeting `main`.

## Compounding Follow-ups

- `rzm ontology query` and `rzm agent ontology-query` on the CLI still build a one-shot runtime and refresh the projection (about 1.1 s). They could attach to the runtime through the same agent-operation route and barrier; the change is small but outside this scope.
- The first read after an edit is dominated by the watcher's per-change work (anchor-scope recomputation and graph refresh), 0.7 to 1.6 s for one note. Reducing it would speed every runtime consumer, not only code mode.

## Status

Active. PR #78 merged the original runtime-read and script-input implementation. The approved readable-information and tooltip extension is in [PR #81](https://github.com/atomicobject/rhizome/pull/81); merging that extension and adopting its vault migration remain separate steps.

### Tooltip refinement, October 4

Drew requested less noisy tooltips after reviewing the live IP opportunity preview. The approved refinement hides low-value IP properties through existing `@display(hover: false)` schema configuration. Typed Rhizome cards also omit duplicate tag chips and the automatic file/date footer; the path remains on the title as secondary hover text. This small presentation change is included in the readable-information extension.

### Readable-information verification

- Section-summary tests cover built-in and custom Section types, absent optional summaries, indexed hydration without source reads, staged section edits, native summary capabilities, and source-preserving editing. Independent review found no blocking issues.
- The expanded test run passed all changed Go packages, lint, vet, TypeScript checks, credential checks, 23 benchmark checks, 1,343 web tests, and integration packages/mixed suites. The only full-suite failure was the existing Git provenance test on Homebrew Git 2.56.0, reproduced on unchanged HEAD b99d341e. Git 2.56 follows independently added identical files as renames; the full affected package passes with Apple Git 2.54.0. Tests use an external GOTMPDIR because in-repository temporary fixtures discover the enclosing repository. Node 26 DOM tests use NODE_OPTIONS=--no-experimental-webstorage.
- Final tooltip-focused tests passed 26 tests; all 85 browser journeys passed after updating two assertions to find optional gaps under Context. Repository validation and frozen-scope validation passed with zero issues. Managed guidance regeneration and init --check passed.
- The final branch build passed the DAI disposable fixture in 5.35 s. All 43 migrated notes preserve original bodies, identities, links, ordering and unrelated metadata. A fresh replay reproduces every note and a second pass makes no edits. The 58-file patch applies and reverses on an isolated baseline with every candidate hash verified.
- Browser inspection of the copied vault confirms readable section summaries in cards, tables, note Structure, and the exact opportunity tooltip supplied by Drew. The tooltip contains only type, stage, title, and summary. Live schema display settings hide noisy IP properties on the installed build; the structural migration stays isolated.
- PR #78 merged while the extension was in progress. The extension is published separately against current main; its review package remains in the vault. Original performance measurements above belong to PR #78 and were not remeasured for the presentation extension.

- Follow-up PR #81 rebased cleanly onto main e496f8d1. On the rebased branch, `make check-fast` and race tests for all ontology packages, views, web, and CLI init passed.

### PR #81 review fixes

Greptile's first review scored 4/5 and requested real indexed native-view coverage and preservation of case-variant untyped tags. The view test now executes generated cards through `Service.Execute` with a real indexed source, independently asserting multiline, empty, and absent summaries plus their read-only capabilities. The preview keeps an untyped `Tags` field when no tag chips represent it. CI also exposed different composite-return formatting in Go 1.24 and local Go 1.27; two fixture returns now use named locals and pass both formatters.

Verification: `PATH=/usr/bin:/bin:/opt/homebrew/bin NODE_OPTIONS=--no-experimental-webstorage GOTMPDIR=/tmp/dai-rhizome-gotmp make check` passed, including all Go unit tests and 1,347 web tests. The indexed view regression passed, 27 preview tests passed, and `npm run test:e2e -- tests/e2e/ui-consistency.spec.ts` passed all six browser checks. All tracked non-vendor Go files pass the CI Go 1.24 formatter.

### Windows CI fixture startup repair

The Windows anchors/app shard failed `TestLiveWorkloadBlockedProviderMeasurements` before any fixture nodes or vectors existed. The unchanged fixture allowed background cache warmup while disabling later scheduler ticks, so its single ownership batch could only start an asynchronous recrawl and never reconcile the completed scan. A temporary overlay forcing the warm-cache ordering reproduced the CI diagnostics three times. The fixture now uses `SkipCacheWarmup` and lets its first ownership batch own the cold crawl, preserving the real indexing and provider workload. No production runtime behavior changed.

All three workload tests passed 20 repetitions each (18.791 s), and the complete bootstrap package passed (9.658 s).

The repository `make check` rerun also passed all Go unit tests, 1,347 web tests, lint, vet, and type checks using the same environment settings recorded above.

### Integration with view preferences

Main advanced to `9264ce09` while PR #81 was under review. The branch incorporates its instance-scoped view preferences, including remembered Briefing expansion. The incoming preference test now finds the optional Not linked signal under Context, preserving the same expansion key. Independent integration review found no other product issue.

Main also repaired the same workload-fixture race independently. Its explicit cache readiness and requested structural publication supersede this branch's cold-cache workaround. The merged fixture exactly matches main; all three workload tests passed 20 repetitions each (58.965 s while the repository check ran).

### Integration after persistent diagnostics merged

Drew asked to hold PR #81 until persistent diagnostics PR #79 merged. PR #79 merged as `7faee61b`; this branch now incorporates main `630349a1`, including PR #82's row-reordering fix. Both incoming changes merged without conflicts. The earlier workload-fixture conflict preserves main's explicit warm-cache publication, and remembered Briefing expansion continues to find optional link gaps under Context.

On the integrated tree, `PATH=/usr/bin:/bin:/opt/homebrew/bin NODE_OPTIONS=--no-experimental-webstorage GOTMPDIR=/tmp/dai-rhizome-gotmp make check-full` passed all Go race tests, 1,424 web tests, integration packages and mixed suites, lint, vet, type checks, credential checks, and 23 benchmark checks. With the same environment and `RHIZOME_E2E_PORT=4178`, `make web-e2e` passed all 93 browser journeys. `make build` passed, and the rebuilt binary's `init --check` reported everything up to date. Repository and frozen-scope validation returned zero issues.

### Final integrated review findings

Greptile found two remaining regressions on integrated head `9ed2ec3e`. Interface members selected the first implementor's summary field for every record, dropping prose from other field names. The group model now reads each record's concrete type declaration. Four rendered regressions cover Sections and Trace with different section names and mixed scalar/section summaries, including an interface without its own summary declaration.

Typed previews also suppressed an explicitly selected normal-importance tags property even though their automatic tag chips were hidden. The card now suppresses duplicate tag fields only when tag chips actually display them. A regression verifies that schema-selected tags remain visible on a typed card.

`make check` passed all Go unit tests, 1,429 web tests, lint, vet, type checks, and credential checks. The focused interface regressions and existing model/Sections/Trace tests passed, as did all nine preview-card tests.

The focused browser run over group views, type views, and UI consistency passed all 19 journeys. Repository and frozen-scope validation returned zero issues.
