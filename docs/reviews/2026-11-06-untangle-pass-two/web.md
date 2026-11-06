---
summary: "Second untangle pass: web audit and concrete candidate evidence."
reference-kind: analysis
---

# Second untangle pass: browser and desktop

Baseline source: `b0d0cf824a9b7b7fc9e1832fd31aa5e7555e2f0e`; inspected current HEAD `2a9541828`, whose additions over the baseline are review documentation. Ownership covered `web`, `desktop`, `pkg/app/desktop`, and `pkg/app/web`. Read the pass-two handoff, first-pass delivery evidence, web/desktop/runtime guidance, TypeScript guidance, and local AGENTS. Compounding lookup found no matching shards.

This was read-only. No source files, tests, builds, browser sessions, runtime state, commits, or PR state were changed. The only written file is this report. Source evidence establishes the findings; no runtime-failure claim below is presented as reproduced.

## W2-1: Delete remaining unused table constant and private broker conveniences

**Confirmed surplus. Small, safe cut.**

- `web/src/components/ConfiguredViewTableModel.ts:63-64`: `FIXED_WIDTH_CELL` has no caller in the repository, including tests, kit consumers, and bundled views. It is an old CSS-class regex next to the live `columnClassName` implementation. Removing it changes no public kit surface.
- `pkg/app/web/global_events.go:126-134`: private broker methods `NotifyValidateChanged` and `NotifyIndexChanged` have no callers. Production uses `Publish` and `Server.NotifyGlobalEvent`. These are private-type conveniences with no remaining reason to change independently.

Smallest fix: delete these three declarations and their now-obsolete comments. Retain `GlobalEventReason`, `NotifyGlobalEvent`, and all broker lifecycle methods. Verification: existing web typecheck and `pkg/app/web` broker/HTTP tests. No new test for deleting uncalled declarations.

## W2-2: Existing preference event is absent from the advertised stream contract

**Confirmed hidden link, already manifested as missing capability metadata.**

- `pkg/app/web/view_preferences.go:89-95` emits the literal `view_preferences.changed` after successful preference changes.
- `pkg/app/web/public_api.go:419-430` separately lists global streamed event kinds. The emitted preference event is missing. `SupportedKinds` at `:436-449` independently repeats the global list and also omits it.
- The event is documented in the OpenAPI preference response (`pkg/app/web/openapi.yaml:1595`) and consumed by the browser, so it is not an unadvertised internal-only event.
- Browser subscription at `web/src/query/vaultEvents.ts:20-25`, preference handling at `web/src/viewPreferences/events.ts:11,16`, and unhosted kit forwarding at `web/kit/freshness.ts:163-168` each repeat the event spelling.

The emitter, advertised metadata, browser subscription, and kit forwarding should change together. Current layout lets the capability metadata fall behind production behavior.

Smallest fix: add the already-supported literal to `StreamedKinds` and `SupportedKinds`; put the browser's one exported preference-event name beside `VIEWS_CHANGED_EVENT` in `vaultEvents.ts`, then import it in the preference handler and kit. A Go constant shared by the preference emitter and capability lists avoids another local spelling copy. Keep wire spelling and payload exactly as they are. Do not alter the semantic categories or make unrelated events invalidate preferences.

There is **no existing cross-language event-code generator** to extend. OpenAPI models the capability arrays as string arrays; `npm run generate:api` generates response types, not event value lists. Go core constants live in `global_events.go`, the view-folder event lives in `custom_view_watch.go`, and bootstrap repeats six literals plus the reconciliation-failure reason (`pkg/app/bootstrap/live.go:155-162`) to avoid depending on web. A complete schema/codegen registry would be new infrastructure, not necessary for this demonstrated omission. A neutral Go event owner can be considered with the runtime lane, but should not expand this fix by default.

Meaningful verification: extend the existing public-capabilities HTTP assertion near `pkg/app/web/server_integration_test.go:1251` to require this exact streamed/supported value; retain preference-mutation SSE coverage in `view_preferences_test.go` and frontend preference/kit freshness tests. These assert separate emitted and advertised behaviors rather than comparing the list to itself.

## W2-3: Desktop privileged link creation maintains its own shell quoting

**Confirmed hidden link; reuse a current owner.**

- `pkg/app/desktop/cli_link.go:174` implements POSIX single-quote escaping locally.
- `pkg/shellquote/quote.go:6` already owns quoting one POSIX shell argument, including empty strings, metacharacters, apostrophes, and safe bare tokens.
- `cliLinkCommand` (`cli_link.go:176-189`) is the only production consumer of the local helper and applies it to the link directory, link path, expected old target, and replacement target. These rules should be shared with other shell command construction.

Smallest fix: call `shellquote.Quote` and remove `cliLinkQuote`. Preserve administrator argument passing, mutation ordering, exact expected-old-target checks, and refusal to replace foreign entries. Safe path tokens will lose cosmetic single quotes; that is a command-string representation change, not an intended shell behavior change.

Test cleanup matters here: `cli_link_test.go:165` and `:201` build expected command strings with the same local quoting helper used by production. They cannot catch a quoting regression. Keep permission/cancellation/foreign-replacement scenarios, but validate the generated command by executing it in the existing temporary fixture with literal paths containing an apostrophe and command substitution characters. Assert the exact resulting symlink, absence of an injection marker, and rejection when the inspected old target has changed. Keep `cliLinkAdminArgs` assertions proving the full command remains one AppleScript argument. The canonical shellquote unit test already checks literal argument preservation.

No privileged or filesystem mutation was executed during this audit. Verification should use temporary paths and unprivileged `/bin/sh`, not real `/usr/local/bin` or an administrator prompt.

## W2-4: Global and node invalidation streams duplicate the same transport lifecycle

**Confirmed hidden link; bounded extraction, not a universal SSE framework.**

- `pkg/app/web/global_events.go:161-190` and `pkg/app/web/node_events.go:360-390` repeat response headers, connected frame, flush, 20-second heartbeat, context/closed-channel termination, JSON serialization, marshal-failure skipping, and flushing.
- They differ in event wire framing: global events write `id`, named `event`, and `data`; node events write `id` and `data` only. Subscriptions, node-ref admission, view-folder watching, and runtime holds belong to their current handlers.

Smallest coherent fix: one private typed JSON-event stream loop for these two invalidation endpoints, with per-event ID/name selection. Empty event name must preserve the unnamed node message. Keep route validation, broker subscription/unsubscribe, watcher ownership and `holdRuntime` in the handlers. Do not change the 20-second heartbeat, buffer/drop policy, JSON shape, or cancellation behavior.

Do **not** extend this extraction to agent chat or index-job streams: agent events have replay/cursor ownership; lane streams use a different heartbeat, explicitly touch the runtime, and synthesize a terminal event on closure. A generic framework covering those would need new policy options and would defeat the purpose of this small extraction.

Verification: existing HTTP event tests (`TestPublicEventsStreamPublishesInvalidationEnvelope`, `TestOntologyNodeEventsStreamPublishesWatchHubUpdates`, streaming-unsupported checks) and broker-close/runtime-hold tests. Add a focused shared-loop test only if those do not cover both named and unnamed literal frames, cancellation, and channel closure. Preserve existing node identity/duplicate suppression tests.

## Deliberate retentions and rejected leads

- No reopening of first-pass graph aggregation or preview missing-value policies. Their differences remain intentional.
- Public kit exports that lack repository callers remain supported public APIs: `executeView`, kit widgets, `nodeHref`, and `workspaceViewHref` are not dead code.
- Desktop/Web section enums and native command validation are different language boundaries. Generating a full IPC schema merely to remove string duplication would add infrastructure without a demonstrated failure in this pass.
- Native themes derive actual colors from the CSS tokens and preserve prepaint behavior. Rust's finite theme enum is not grounds to replace that working mechanism in this pass.
- Desktop/web validation badges repeat a little rendering policy, but sharing their DOM would couple shell styling and browser presentation. No present discrepancy justified a shared component.
- Browser storage catches, provider-source refresh revision guards, bounded delayed reconnect, native credential-import cancellation markers, per-window generation checks, and runtime restart limits express real requirements. They were retained.
- The GraphQL sidebar exclusion is a known existing limit in the delivery/context record, not a fresh untangle finding.
- Test fixtures using OS skips, timeout bounds, or synthetic bridge output were not labeled invalid merely for using those mechanisms. No flaky behavior was claimed without execution.

## Additional comment observation

`desktop/src-tauri/src/page.rs:264-266` says only the theme command uses `on_runtime_document`'s looser gate, but `send_setting` at `:208-218` also handles the sidebar command. Correct the sentence when that file is touched; it does not justify a standalone architectural change. The runtime-origin validation itself must remain.

## Coverage and evidence limits

Inspected current browser/desktop event, transport, preference, theme, native-origin, command, runtime-bridge, setup, and credential paths; inventoried production exports and verified deletion candidates by whole-repository reference searches. No new behavior or contract was inferred from file size. This is structural review evidence, not an exhaustive dynamic security/accessibility audit. No Rhizome session retrieval was performed in this child; normative local notes and the parent-provided delivery record were read directly.
