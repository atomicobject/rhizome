---
type: EffortNote
id: EFF-2026-11-06-09-09
aliases: [EFF-2026-11-06-09-09]
name: Bound native note body memory
created-at: 2026-11-06T14:10:09Z
status: complete
summary: "Keep note data and reading state while releasing older native note bodies without slowing ordinary tab switching."
governing-specs:
  - "[[docs/specs/experience/notes-workspace-shell]]"
  - "[[docs/specs/technical/frontend-data-lifecycle]]"
---

# Bound native note body memory

## Scope

Retain the five most recently activated native note bodies, plus protected editors, while preserving all open tabs, fetched workspaces, navigation and reading state. Keep existing data owners and live invalidation. Verify memory savings and responsiveness against the same workload.

Loaded HTML viewers remain resident because their isolated frame owns interaction state. No shared-store migration, persistent-format change, public API change, dependency, release, push or merge is included.

## Spec Set (Frozen)

Frozen before implementation at `a738a260a`:

- [[docs/specs/experience/notes-workspace-shell|SPEC-0090]]
- [[docs/specs/technical/frontend-data-lifecycle|SPEC-0074]]

The approved proposal is recorded in Plan below; its source was `/tmp/rhizome-tab-memory-consensus-plan.md` with SHA-256 `886ae1cc4529f1372a18dab01c34d90d80706da6f5790db82d37b8f2f231deb9` at approval capture.

## Stories In Scope (Frozen)

- [[docs/specs/experience/notes-workspace-shell#^SPEC-0090-US2|SPEC-0090 US2]]: retained tabs and cached opening.
- [[docs/specs/experience/notes-workspace-shell#^SPEC-0090-US4|SPEC-0090 US4]]: current right-rail context.
- [[docs/specs/experience/notes-workspace-shell#^SPEC-0090-US5|SPEC-0090 US5]]: structural reading, drill history and refresh preservation.
- [[docs/specs/technical/frontend-data-lifecycle#^SPEC-0074-US3|SPEC-0074 US3]]: coherent staged edits.
- [[docs/specs/technical/frontend-data-lifecycle#^SPEC-0074-US4|SPEC-0074 US4]]: existing invalidation contract.

## Spec Coverage Checklist

- [x] Retain tab identity, data and navigation while bounding native rendered bodies, covering [[docs/specs/experience/notes-workspace-shell#^SPEC-0090-US2|US2]].
- [x] Preserve right-rail targets, disclosures, scroll and drill state, covering [[docs/specs/experience/notes-workspace-shell#^SPEC-0090-US4|US4]] and [[docs/specs/experience/notes-workspace-shell#^SPEC-0090-US5|US5]].
- [x] Protect editors and existing refresh behavior, covering [[docs/specs/technical/frontend-data-lifecycle#^SPEC-0074-US3|US3]] and [[docs/specs/technical/frontend-data-lifecycle#^SPEC-0074-US4|US4]].

## Plan

User approval in chat: "Okay, do it, use subagents to divide and conquer." This approves the council's revised memory plan. Current user is not configured, so `plan-approved-by` is absent; the actual approval is retained here without inferring a configured identity.

1. Preserve the old binary and run baseline probes for temporary CSS containment, staged-edit render behavior and WebKit. The existing Chromium workload contains 24 notes, each 26,688 bytes and 90 sections. Its five-run median is 59.931 MB retained JS heap, versus 6.701 MB with one note. Closing 19 notes gives 20.467 MB and is only a diagnostic control.
2. Keep NoteTab/usePaneStack ownership. Add bounded body residency in NotesShell: five most recent native bodies, including active, plus bodies already resident or visited during an editing session. Do not awaken old bodies when editing starts. Protection ends after the session and save/discard settle, with trimming on the next activation.
3. Keep reading state above the released body, keyed by canonical pane and presentation. Track disclosures explicitly. Capture a stable visible content anchor and offset with a pixel fallback. Restore disclosures before scrolling; allow one late-layout correction, cancelled by user interaction. New explicit navigation wins. Clear references into released DOM.
4. Keep existing live invalidation and request generations. Restoration alone causes no new note query. Exempt loaded HTML viewers from disposal to preserve isolated interaction state.
5. Prove behavior through focused and browser tests. Compare interleaved before/after runs in the same browser/build mode, separating recent and restored switches. Inspect repeated cycles, protected editor peaks, detached DOM and source-editor cleanup. Use forced GC where available and report WebKit measurement limitations.
6. Run `make check`, `make web-e2e`, targeted WebKit tests and documentation validation. Have an independent reviewer inspect the integrated change. Update web lifecycle docs, SPEC-0090 and CHANGELOG.

Proposed measurement budgets: halve the extra JS heap added by 24 notes compared with one; reduce DOM count by at least 60%; recent-switch median/p95 no worse than the same-engine baseline by more than one 60 Hz frame; restored-switch p95 below 100 ms on this fixture. A failed budget must be reported and explained, not hidden by retries or relabeled as passing.

## Original Intended Delivery

A reviewable implementation in this worktree with preserved tab data and reading/editing state, measured memory and responsiveness, focused regression coverage and the required local gates. No publication or installed-app replacement.

## Actual Delivered

Implemented bounded native note-body retention with existing tab data and live refresh ownership. Five recent bodies remain mounted; editing sessions and isolated HTML viewers are protected. Reading positions, disclosures and drill history survive body recreation without a new note query. Chromium retained heap fell 57.7% on the matched 24-note workload; recent-tab switching was effectively unchanged. The rebased Chromium browser suite passes all 127 cases, and all six focused Chromium/WebKit retention cases pass. The current rebased `make check` passes 1,985 web tests plus the Go, lint, type, content and team API gates. Default documentation validation and frozen-scope drift validation each report zero issues. The implementation is committed as `0705be63f` with the stable-identity correction in `3b236e912`. Full Send now carries it through PR review and merge; no installed app has been replaced yet.

## Execution Notes

- Sol Loop completed two rounds with GPT-6.1 Sol (xhigh). Round 1 S1 was fixed; round 2 approved with no open findings. Usage was not exposed. The added test fixture then received its required explicit note path without changing the tested behavior. Current `make check` passes 1,985 web tests; the rebased browser suite passed 127 tests.
- Sol Loop round 1 requested changes for S1: mutable structural fingerprints remounted protected editors and separated saved reading states during ordinary refreshes. Removed the fingerprint from the reading identity while retaining stable node identity and presentation. Both editor-undo and suspended-scroll regressions failed before the fix; all 60 focused tests passed afterward.
- 2026-11-06T14:49:21Z: Full Send authorized shipment. Committed the implementation and rebased onto `d4973a47004818cba30c5fa48d24ee2d6b494aa5`; the only conflict was adjacent changelog entries, and both were preserved. Current rebased implementation commit: `0705be63f`. Refreshed build/check evidence is being gathered before Sol review.
- 2026-11-06T14:10:09Z: Recorded explicit user implementation approval and split work among native agents: tab residency/wiring, reading-state/body disposal, and browser/performance verification. Parent owns integration, records and final review.
- Preserved baseline executable at `/tmp/rhizome-tab-memory-before-rzm` before source implementation. Baseline fixture resides under the path recorded in `/tmp/rhizome-tab-memory-root`.
- Regression was red against the old binary: after visiting eight notes the oldest body was still rendered. Existing editor/undo browser preservation test passed against the old binary.
- Balanced CSS probes showed no retained-memory reduction (Chromium 60.990 MB versus 60.996 MB; no DOM reduction). Layout/switch timing differences were exploratory under concurrent machine work, so no CSS change was included.
- Baseline staged-edit instrumentation captured a small one-note case, but the 24-heavy-note baseline stalled entering global edit mode before a comparable staged-edit count could be captured. This is an observed limitation, not an attributed root cause or a new optimization in this effort.
- Native retention and reading-state workers completed their owned changes. Focused residency, pane and shell suite passes 41 tests after integration. Independent review found one source-issue restoration bug; a new regression failed with old issue selection 6 instead of 0, then passed after suppressing stale source reveal targets during restoration.
- Integrated build passed. Final `make check` passed, including 1,977 web tests across 241 files, Go unit checks, content contracts, lint/type checks and team API checks. Initial gate reported spacing-only test lint errors; those were corrected before the passing run.
- All three new browser regressions passed in Chromium and WebKit (six cases): late-layout reading restoration without requery, explicit fragment navigation after eviction, and editor/undo preservation. WebKit required a unique late-image fixture URL so its decoded-image cache did not bypass the intended delayed load.
- Clean comparison completed: five runs per build per browser, alternating builds, with identical note content in isolated fixture vaults. Chromium retained heap fell 59.936 MB to 25.327 MB (57.7%); retained DOM nodes fell 66,268 to 14,854. Warm-switch median changed 53.75 to 54.05 ms; restored-tab p95 was 82.5 ms. WebKit document elements fell 26,982 to 6,367; warm median changed 90 to 73.5 ms; restored p95 was 83 ms. All planned budgets passed. Every run made exactly 24 initial note reads and no activation/revisit reads, with no observed empty frames. Node counts remained stable across two full revisit cycles.
- Editing-protection probe: five source views used 23.690 MB, visiting all 24 during editing peaked at 37.590 MB, and finishing editing plus the next activation returned to five views and 24.051 MB. Starting editing did not awaken suspended bodies.
- Retained measurements: [[docs/efforts/2026-11-06-09-09-note-tab-memory/measurements.json|raw comparison and editing evidence]]. Chromium supports forced-GC heap metrics; Playwright WebKit does not expose equivalent heap control, so its result reports element counts. These are browser-engine results, not an installed WKWebView or GPU-memory measurement.
- Added a focused suspended-pane invalidation check: the note updates while its body is released, and restoring it shows the updated content and existing drill history without another query. Both event-isolation tests pass.
- The first full browser gate passed 117 cases and exposed an HTML Preview/Source state regression. The reading-state provider key included the native presentation mode, remounting the isolated viewer. HTML now keeps a stable provider key across those modes; native-note residency and the measured native path are unchanged. The complete browser gate then passed all 124 tests, including every HTML viewer case.
- Final verification after the HTML identity fix: `make check` passes (1,978 web tests), `make web-e2e` passes (124 tests), default validation passes (zero issues) and frozen-scope drift passes (zero issues).
- Rhizome `next-id` waited for initial worktree indexing. Authoring guidance and the allocated ID came from the local CLI; current-user inspection reported unconfigured.

## Deviations

- 2026-11-06T14:49:21Z: User requested `$sol-loop` and `$full-send`, extending delivery authorization through local commits, PR review, CI, merge and the repository-required Rhizome Dev update from main. Earlier no-publication scope describes the initial implementation phase; public releases and unrelated production changes remain outside this effort.

- Loaded isolated HTML viewers are exempt from body disposal. Their internal reading state cannot be restored by the parent without a new cross-origin protocol; preserving it takes priority over forcing a five-body cap for HTML. Native Markdown remains the measured target.

## Closure Checklist

- [x] Focused and browser behavior checks pass.
- [x] Before/after measurements and limitations recorded.
- [x] Required local gates and documentation validation recorded.
- [x] Independent integrated review resolved.
- [x] Actual delivery and durable docs reconciled.
- [x] Committed delivery references: `0705be63f`, `3b236e912`; PR delivery reference: [#105](https://github.com/atomicobject/rhizome/pull/105).

## Status

Complete for delivery in [PR #105](https://github.com/atomicobject/rhizome/pull/105) on `t3/reduce-web-app-memory`. All implementation scope is delivered and local verification passes. PR review, CI, merge and the main-checkout Rhizome Dev update remain the authorized Full Send delivery steps.

## Compounding Follow-ups

None established. Any additional performance work requires measured evidence outside this bounded change.
