# Fable 5.2 cleanup

Drew requested independent visual and implementation-quality reviews, followed by one coordinated cleanup. Both reviews ran on `claude-fable-5-2` through Claude Code. A Fable implementation leaf owned the presentation files in an isolated worktree; the root integrated its commit and handled the data and public kit changes. The PR remains open and unmerged.

## Decisions and scope

- VD-1 through VD-8: remove doubled rules, restore section spacing and house typography, use consistent marked notices, show recorded status with house marks, refine the panel, correct pluralization, and replace native disclosure triangles. Indent child stories. Preserve authored status and scope.
- IQ-1: review rows and copied prompts include each exact pending selected locator. An unavailable locator prevents copying rather than silently shrinking scope.
- IQ-2: move `Markdown`, `convertWikilinks`, and `markdownChecklist` from the unmerged core exports to `@rhizome/kit/markdown`. The shared import map owns browser and validation availability. Migrate every caller, TypeScript path, test alias, and source guide together.
- IQ-3: isolate each failed effort subject read, retain healthy siblings in request order, and bound independent work to four concurrent efforts. Reuse the already hydrated inline sections, linked log, and log sections for scope and checklist reads. Keep per-target diagnostic ownership, generation coherence, revision checks, and cancellation.
- IQ-4: index hydrated nodes once by canonical identity and follow each parent's authored references. Deduplicate repeated identity within a malformed parent list while retaining its incomplete-coverage diagnosis.
- IQ-5: delete unused attribution and source-role APIs. Keep `distinctTargets` as the real hydration deduplicator. Keep the existing read-state markers; removing them is unrelated to these findings.
- IQ-6: distinguish an unreadable selected source from a broken authored selector.
- US2 sorting: use one explicit source/title order for visible rows and composed prompts. Preserve parent grouping and selected identities.
- Panel handoff: the active workspace URL supplies only a panel's declared params. The AE panel declares non-identity `stage`; it reads that value only on the stage page. This replaces the temporary storage channel and keeps Find state across stage changes.

## Measured bundle effect

Five alternating production builds of the exact before revision and the cleanup sources used the same installed dependencies. Every repetition produced identical bytes and hashes within its source version. The measurement follows the static imports reachable from `kit.js`, counting compressed files individually. Dynamically loaded code is excluded from this metric.

| Surface | Before | After |
| --- | ---: | ---: |
| Core entry bytes | 328,998 | 101,113 |
| Core static dependency bytes | 686,381 | 457,867 |
| Core static dependency gzip bytes | 176,570 | 123,377 |
| Markdown dependency modules reachable from core | 136 | 0 |

Reproduce with `node fable52-kit-measure.mjs <before-checkout> <after-checkout>`. Supply complete source snapshots with the same `web/node_modules`; the script uses each snapshot's production kit configuration. The measured snapshots were an archive of `aa830a2b` and that archive with the cleanup files applied.

Exact-base `019a59ea` measured 456,239 static bytes and 122,534 gzip bytes with the same dependencies. The optional Markdown entry retains the existing renderer and parser; these figures establish the core load reduction, not a page-latency or heap claim. Per-run values and hashes are in `fable52-kit-measurements.json`.

The synthetic inline-effort request fixture performed eight API calls before reuse and five afterward. It asserts one hydration per requested subject or section. A separate seven-effort fixture exercises a failed subject, ordered healthy siblings, and the four-effort concurrency bound. These are operation counts, not measured network latency.

## Verification status

The first regression run failed for the newly exposed behaviors before their fixes. The initial integrated browser run passed 65 of 68 cases and exposed one real duplicate-row regression plus two incorrect new test assumptions. The duplicate rows came from repeated parent refs, and were fixed without removing the incomplete-coverage warning. The sort assertion now scopes to the selected stories rather than an unrelated story with the same ID. The error test exercises the actual partial-read state rather than expecting a whole-query error from a reader that preserves partial results.

The Fable code recheck approved the main changes but identified selectable review observations with unknown scope and a stale host comment. Both were corrected, with a pure regression for review selection. All 77 installed AE cases and full `make verify` subsequently passed: 2,169 web unit cases, 159 native browser cases, and 90 Knowledge browser cases. The final Fable screenshot review approved the 63-capture snapshot; `fable52-final-review.md` records the remaining copy and capture corrections. The final copy and capture corrections passed `make check`, `make build`, strict template TypeScript, all 77 installed AE cases, and both documentation validators. The resulting manifest has 68 captures, with 44 committed images. Current-head PR evidence remains pending until the cleanup commit is pushed.


## Current-main integration and final review follow-up

`origin/main` advanced to `a2d49bf19` while the cleanup was in verification. It merged without conflicts, and full `make verify` passed again with the same 2,169 web unit, 159 native, 90 Knowledge and 77 AE cases. The merge is preserved as a separate commit.

The old `a1b7cd0` macOS job failed before Desktop tests when Go dependency DNS resolution failed. `macos-build-network-diagnosis.md` records the exact build stage and cause. The failure is not claimed as a pass, and no production or test change is made for it.

Greptile's P2 root-image finding was reproduced in both installed themes. The source fallback now distinguishes repository-root paths from protocol-relative external images; `greptile-review.md` records G10.

The G10 correction passed the complete 77-case installed suite, `make check`, `make build`, strict template TypeScript and both validators. Both refreshed image-fallback captures were inspected.
