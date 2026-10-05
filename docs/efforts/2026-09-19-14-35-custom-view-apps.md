---
type: EffortNote
id: EFF-2026-09-19-14-35
aliases:
  - EFF-2026-09-19-14-35
name: Custom view apps
created-at: 2026-09-19T18:35:42Z
status: complete
summary: "Deliver SPEC-0105: custom-code views served same-origin with an embedded UI kit and per-file TSX transform, mounted in the Notes rail and usable standalone, with an agent skill and two buildless proof-of-concept views."
---

# Custom view apps

## Scope

Deliver [[custom-view-apps|SPEC-0105]] from `main` at `02485b73e` on branch `t3code/6fb8cfb9`, ending with two buildless proof-of-concept views in this repository's own `.rhizome/views/` that share a component, one of which writes. No pull request until Drew asks.

Excluded: a Vite-built proof of concept, npm publication of the kit, cross-origin API access, type or interface mounts for custom views, and any migration of the first-party UI onto the kit.

## Spec Set (Frozen)

Baseline `02485b73e`:

- [[custom-view-apps|SPEC-0105]] is the delivery contract.

## Stories In Scope (Frozen)

- [[custom-view-apps#^SPEC-0105-US1|SPEC-0105.US1]]: author a custom view with two files.
- [[custom-view-apps#^SPEC-0105-US2|SPEC-0105.US2]]: serve and transform.
- [[custom-view-apps#^SPEC-0105-US3|SPEC-0105.US3]]: kit.
- [[custom-view-apps#^SPEC-0105-US4|SPEC-0105.US4]]: mount and standalone.
- [[custom-view-apps#^SPEC-0105-US5|SPEC-0105.US5]]: agent authoring loop.

## Spec Coverage Checklist

- [x] [[custom-view-apps#^SPEC-0105-US1|SPEC-0105.US1]] has loader and validator tests for the custom kind, entry containment, rejected fields, and `node_modules` skip.
- [x] [[custom-view-apps#^SPEC-0105-US2|SPEC-0105.US2]] has handler tests for shell, file serving, traversal rejection, transform, and the 422 error body.
- [x] [[custom-view-apps#^SPEC-0105-US3|SPEC-0105.US3]] has a kit build that loads in a browser with one React instance, and themed components verified by screenshot.
- [x] [[custom-view-apps#^SPEC-0105-US4|SPEC-0105.US4]] has a web unit test for the view-tab dispatch and browser evidence for embedded, standalone, note navigation, and reload.
- [x] [[custom-view-apps#^SPEC-0105-US5|SPEC-0105.US5]] has init idempotence evidence for the skill and a validation test for transform errors.

## Plan

### Decisions settled before planning

- Same-origin, trusted code; no sandbox and no capability scoping (Drew, 2026-09-19). The isolated viewer host stays note-only.
- Embed esbuild and the kit in the binary (Drew, 2026-09-19).
- Buildless proof of concept only (Drew, 2026-09-19).
- Everything exposed to embedded views must serve standalone tools too (Drew, 2026-09-19). Met by making the embedded view and the standalone tool the same URL.
- No app manifest. A custom view is a view definition with a new source kind; a folder of definitions is the multi-view unit. Rationale: one loader, one validator, one catalog, and shared components fall out of relative imports.
- GraphQL is read-only in this codebase; writes go through the edit-session routes. The kit wraps both.
- Reload polls a folder content stamp. Rationale: the vault watcher excludes `.rhizome/` on purpose to avoid self-triggered churn, and a poll of a small folder needs no watcher change.

### Phases

1. **View definitions** (`pkg/ontology/viewconfig`, `pkg/app/views`, `pkg/validate`): `SourceCustom`, `source.entry`, validation rules, `node_modules` skip, catalog passthrough, unsupported-kind error on execute. OpenAPI and generated web types.
2. **Serving and transform** (`pkg/app/web`): vendor esbuild; `/views/<id>` shell, `/views/_files/`, transform with 422 text errors and an in-page error module, folder stamp route. Handler tests.
3. **Kit** (`web/kit/`): second Vite config building shared-chunk ES modules into `pkg/app/web/assets/dist/kit/v1/`; `@rhizome/ui` components, `@rhizome/kit` hooks and helpers reusing `web/src/api`; theme file; Tailwind browser compiler; Makefile wiring.
4. **Mounting** (`web/src/components`): `ViewTab` dispatches custom views to a frame component; note-open message handling. Unit test.
5. **Agent loop**: transform check inside the `views` validation check; skill template `custom-views` under `pkg/app/cli/init/templates/skills/markdown/`; init idempotence run.
6. **Proof of concept**: `.rhizome/views/poc/` with an action-items board (writes `done`) and a subsystem map, sharing a component. Browser verification embedded and standalone, with screenshots.
7. **Close-out**: CONTEXT notes, gates (`make check-fast`, focused Go tests, `make check`, `./scripts/rzm validate`), review pass, binary size delta recorded.

Phase 1 fixes a public contract (the `custom` source kind and `entry` field). It is small and additive, so no foundation-review pause is planned.

## Plan Approval

Drew chose "Effort + plan, then build" in chat on 2026-09-19 with the stated meaning that his go-ahead counts as plan approval and work proceeds without stopping. Approval fields stay blank: he has not reviewed this plan as written, and no Rhizome current user is configured in this worktree.

## Original Intended Delivery

SPEC-0105 US1 through US5, plus the two proof-of-concept views.

## Actual Delivered

All five stories, carried by PR #383 from branch `t3code/refactor-rhizome-ui-platform`.

- `pkg/ontology/viewconfig`: `custom` source kind, `source.entry`, validation, `node_modules` skip.
- `pkg/viewscript` (new): per-file TSX/TS/JSX transform over vendored esbuild v0.28.2, and a whole-folder check.
- `pkg/app/web/custom_views.go`: `/views/<id>` shell, `/views/_files/`, `/views/_stamp/`, `/views/_check/`.
- `pkg/validate`: the `views` check reports script transform errors once per folder.
- `web/kit/` and `web/vite.kit.config.ts`: `boot.js`, `@rhizome/ui` (13 shadcn-compatible component files), `@rhizome/kit`; built into `assets/dist/kit/v1/` by `npm run build`.
- `web/src/components/CustomViewFrame.tsx` and the `ViewTab` dispatch.
- Skill template `custom-views`, installed copies under `.claude/skills` and `.agents/skills`.
- Proof of concept in `.rhizome/views/poc/`: Action Board (writes `done`) and Delivery Radar, sharing `components/Page.tsx`.

## Execution Notes

- 2026-09-19T18:35:42Z Effort opened. `NO_WEB=1 make build` succeeded in the worktree to make `./scripts/rzm` usable.
- 2026-09-19T18:50:00Z Design changed before any code, after two messages from Drew: the isolated viewer host was dropped for same-origin serving, and the app manifest was dropped for a source kind. Both simplify the plan; neither widens scope.

- 2026-09-19T19:10:00Z Browser verification (Playwright, Chromium, against `rzm serve --port 8791` on this vault) passed nine checks: rail lists both views; embedded render; note link opens in the workspace; checkbox writes `- [x]` into `docs/playground/pizza-party-2026.md`; board refreshes from the event stream; unchecking restores the file byte for byte; a syntax error in the shared component shows `poc/components/Page.tsx:20:13: Expected identifier but found "<"` on the page; fixing the file reloads the view; Delivery Radar renders. No page errors. The playground note ended unchanged.
- Gates: `make check-fast` exit 0; `make check` exit 0 (Go unit tests, 92 web test files, 732 tests); `./scripts/rzm validate` exit 0 with 0 issues; `go test ./pkg/app/cli/init/` ok; `rzm init --yes` second run reported no updates. `make check-full` and `make web-e2e` not run.
- Binary size, unstripped darwin/arm64: 69,263,090 bytes at baseline, 86,236,194 now. The baseline build embedded only the 4.5 MB of stale tracked `assets/dist` files, where this build embeds the full 14.3 MB UI, so about 9.9 MB of the difference is not this change. Attributable: kit 1.77 MB (lucide-react is 0.94 MB of it) and esbuild about 5.4 MB by subtraction.
- `rzm init --yes` also regenerated agentic-engineering references, `AGENTS.md`, `.rhizome/workflows.yml`, and migrations. That drift predates this effort; it was reverted here and left for its own change.
- `go mod vendor` deletes this repository's hand-vendored tree-sitter and sqlite files. esbuild was vendored by running it and then restoring every other vendor path from Git; `go.mod` and `go.sum` carry only the esbuild lines.

- 2026-09-19T19:25:00Z Independent review (Fable advisor, read-only) confirmed path containment, viewer-host diversion before the mux, JSON-in-script escaping, and the postMessage origin and source checks. Four findings fixed: frames render only in the active tab (each live frame holds an event stream and a poll, and HTTP/1.1 allows six connections per host); `setField` deletes its edit session in `finally` and retries only `conflicted`; reload and check use the definition's folder, not the entry's, with a nested-entry test; `/views/` responses send `Content-Security-Policy: frame-ancestors 'self'`. Also fixed: `mountView` no longer creates a root for self-mounting modules, `source.entry` is rejected on declarative kinds, and two skill claims were narrowed. Browser verification rerun: ten checks pass. `make check` rerun: exit 0.
- Observed once during the rerun and not reproduced in ten further writes: a committed edit left `pizza-party-2026.md.rzm-repair-*.backup` and a `COMMITTED` journal under `.rhizome/repair-journal/`. The next edit transaction cleaned both. Cause not identified.

- 2026-09-25T12:40:00Z Committed as five Conventional Commits and rebased onto `origin/main` (218 commits ahead of the baseline). Conflicts: `pkg/app/views/service.go` and `pkg/ontology/viewconfig/validate.go` (main added configured view variants; custom views now return before variant checks) and `web/src/components/ViewTab.tsx` (re-applied onto main's `ConfiguredView` and saved view state). `openapi.yaml` is hand-authored, so it keeps main's content plus the `entry` field; `web/src/api/generated.ts` was regenerated from it and matched. `web/package-lock.json` was rebuilt from main's lock by `npm install`, which hoists newer `@radix-ui/*` patch versions and `clsx` 2 that GraphiQL already accepts. Main's license gate required regenerated notices for esbuild and the kit's packages.
- #359 (staged reads) and #375 (UI boundary hardening) checked against the kit's assumptions. The client functions the kit calls are unchanged; staged reads are additive POST forms the kit does not use; #375 adds `applicationHostDenied` ahead of the origin check, which leaves loopback same-origin views working and still diverts HTML viewer hosts before `/views/`. Separately, saves now preempt background indexing (`edit_session_lock_test.go`): twelve writes from server start all committed, the first after about 4.8 s, where the same test before the rebase returned `conflicted`. The kit's commit retry was removed; it also contradicted the web invariant that edit POSTs are not replayed.
- Review follow-ups fixed: `/views/_files/` no longer serves view definitions (any YAML there), dot-prefixed files or folders, or `node_modules`, and a custom view's entry may not live in such a folder. `_stamp` and `_check` read through `os.Root` and never follow symlinks. One rule owns this: `viewconfig.ServablePath` and `WalkFolder`. The two new handler tests fail against the previous code (it served `board.yaml` and hashed a symlinked folder outside the views root) and pass now.
- Gates after the rebase: `make check` exit 0 (Go unit packages, 112 web test files, 925 tests); `python3 scripts/licenses/generate.py --check` exit 0; `rzm validate` exit 0 with 0 issues; `rzm init --yes` resynced only the `custom-views` skill copies (unrelated fingerprint drift in `.rhizome/migrations` and `workflows.yml` reverted). Browser checks against `bin/darwin/rzm serve --port 8791`: sixteen pass, including the rail listing, embedded and standalone rendering, a single live frame, note navigation, write and restore of the playground note, event-stream refresh, in-page transform errors, reload on fix, `frame-ancestors`, 404 for a definition, and the GraphQL explorer after the radix hoist. No page errors, no repair leftovers.

- 2026-09-25 PR #383 opened after rebasing onto `460424acd`, with a `CHANGELOG.md` entry under `## [Unreleased]` as release policy requires. First CI run: all 20 checks passed, including race-enabled unit and integration tests on Ubuntu and Windows, benchmarks, and web E2E. Review bots raised eight findings (Greptile confidence 1/5). Seven were fixed in `31702dfac`: the Vite dev proxy forwards `/views/` and `/kit/`; the kit reuses the shared reconnecting event source and refetches after every reconnect; Cmd or Ctrl click in an embedded view opens beside; entries are validated through `os.Root`, so a symlink out of the folder fails validation; remediation is registered for `invalid_custom_entry`, `custom_entry_not_found`, and `custom_view_script_error`; unreadable scripts report `custom_view_read_error`. The eighth, that CI's plain `go build` embeds no kit, was answered on the PR: release builds run `make web-build`, which builds the kit. After the fixes: `make check` exit 0 (105 web test files, 903 tests), `rzm validate` 0 issues, and browser checks for beside, write and restore, and catch-up after a server restart pass. CI on `31702dfac`: all 19 code checks passed (unit, integration, and build on Ubuntu and Windows, benchmarks, web tests, web E2E, lint, content).
- 2026-09-25 Self-review before merge, after Greptile's 5/5 on `20da1ea9f`. Fixed in `ffe0917e2`: a view defined at the views root now gets `/views/_stamp/` and `/views/_check/` (paths ending in `/.` were redirected by the mux); validation reports a script once when view folders nest, attributed to the nearest view (Greptile's follow-up on `ffe0917e2`, with a test that fails on the earlier code); a failed load shows both the transform errors and the browser's own error; the open-note message constant is shared by the kit and the frame; the skill explains loading sibling files through `import.meta.url` and that HTML entries need a manual reload. `make check` exit 0 (105 web test files, 903 tests); six browser checks pass.
- Pre-existing on `main`, not changed here: an edit-session commit (`commitTransientOntologyEditSession`, shared with the first-party editor) can leave `<note>.rzm-repair-*.backup` and a `COMMITTED` journal under `.rhizome/repair-journal/` when an early return in `validate.ApplyRepairSession` after commit skips `cleanupRepairJournal`. Server restart does not clear it; the next edit transaction's recovery does. Seen twice in about forty browser and API writes.

## Deviations

- The transform-failure design changed during verification. The first version answered a browser module request with a module that throws the diagnostics. A broken file that is imported by another fails at link time as a missing export, so the message never surfaced. Replaced by `/views/_check/<folder>`, which the kit reads on load failure and `rzm validate` shares. SPEC-0105 US2 was revised to match.
- Added to the kit during verification: refetch on `index.changed` and `node.changed`, because a refetch right after a commit reads the index before it catches up; and a bounded retry in `setField`, because a commit that lands while the indexing lane holds the index lock returned `conflicted`. The retry was removed after the rebase: saves on main preempt background indexing, and the web client never replays edit POSTs.
- The write target is the node's GraphQL `ref { notePath fragment nodeId }`, not `notePath` plus `nodeId`: embedded nodes are addressed as `notePath#fragment`.

## Compounding Follow-ups

- `go mod vendor` is unsafe in this repository; a `make vendor` target that preserves the hand-vendored files would encode that.
- `rzm init --yes` drift on `main` (agentic-engineering references, `AGENTS.md` note includes).
- Forty-four stale files under the ignored `pkg/app/web/assets/dist/` are still tracked; a local web build modifies two of them.
- lucide-react is half the kit. A curated icon subset would halve it at the cost of agents hitting missing icons.
- Review follow-up left open: `.html` entries do not reload on save.
- Validate subsystem: committed repair journals can skip cleanup on early-return paths in `ApplyRepairSession`, leaving a backup file beside the edited note until the next edit (execution notes).
- Opt-in loopback cross-origin API access for separately served tools.
- Vite-built view folder proof of concept and kit npm packaging.

## Closure Checklist

- [x] Spec coverage checklist complete with evidence.
- [x] Gates recorded with exact commands and results.
- [x] CONTEXT notes updated.
- [x] Binary size delta recorded.
- [x] Independent review findings addressed.
- [x] Drew decided to merge (chat, 2026-09-25: "Let's get this work ready to merge. Open a PR."), without a recorded hands-on trial of the proof of concept.
- [x] Closure names the carrying pull request: #383.

## Status

Complete. Delivered by PR #383; completion does not authorize publication.
