# Integrated Delivery implementation

Base remains refreshed origin/main `019a59ea128b900b57dfbcf565481f10ea4e9553`. The old design checkout and its uncommitted changes were not imported.

The installed fixture revealed two necessary shared fixes. Serialized canonical node refs containing a slash were misclassified as filenames. The public query path now sends JSON refs through the existing resolver; note, section and structural counterexamples, including stale-selector refusal, passed. Public `structural` refs also need normalization at the existing host navigation boundary. A targeted context test passed. A 768px record view exposed the shared global toolbar's 760px wrap threshold. Extending its existing wrapped layout to 980px passed the same narrow reproduction.

The record pages read their own subject first. Narrative inventory content is deferred. Selected effort prompts hydrate only selected effort contexts. Spec relationship discovery is explicit and reads exact loaded effort sources, qualifying missing authority. Original plan/log/material navigation uses the existing built-in reader, including the isolated HTML preview.

Home and stage groups retain exact authored lifecycle values. Deferred checklist reads are named deferred, rather than missing or unreadable. Missing scope selection stays unknown and does not claim a generation failure. Checklist counts come from the same GFM parser as the actual renderer; code examples are excluded and quoted tasks remain tasks.

The native view literal hashes in installer tests were captured at `f76e49f7d` before Delivery registration. Their original definitions, filters, layouts and defaults remain protected. Fresh, managed-update and ejected-install scenarios use disposable repositories and real init, registry, validation and user-state APIs.

## Comment review ledger

Codex GPT-6.1 Sol, high, deleted six comment blocks (seven lines). Root accepted the three remaining test-comment deletions. No comments were restored. Two reported clarity flags were resolved with local names: `selectorReadMatchesGeneration` and `untrustedFixtureBuildTags`. No wrapper, public signature, new configuration or new approval stop was needed. Exported contracts, the Go cache-input gotcha and the browser storage exception stayed documented. No scoped suppressions were found.

## Verification

- `make check`: passed, including 2,169 web unit tests and 10 supervisor tests.
- `./scripts/rzm validate` and `./scripts/rzm validate frozen-scope-drift`: passed with zero issues and zero errors.
- `make verify`: passed. Race-enabled unit tests, integration packages and mixed integration tests, benchmark contracts, content, credentials, frontend checks and all browser suites passed.
- Browser suites: 158 native cases, 90 Knowledge cases, and 54 installed Delivery cases passed. No retries, longer timeouts, or skips were added.
- Fresh, managed-update, and ejected-install lifecycle tests passed, including actual `init --check` and preservation of authored records and native view IDs.
- Strict transitive template TypeScript passed; 24 focused domain/adapter tests and public executable-schema query preparation passed.
- [UI proof](ui-proof.md) links 25 committed images. [The capture manifest](capture-manifest.json) records all 49 captures and hashes. Root inspected all images, including the final panel-token correction.
- [Browser diagnosis](browser-failure-diagnosis.md) records the proven HTML cache fix and the preference fixture's unbounded OS-event dependency. [Independent fixture review](native-fixture-review.md) approved the real metadata-save boundary.
- Impeccable source detector reported zero findings. This is not a formal rendered accessibility audit. No Desktop app installation or screen-reader audit was performed.
- Compounding retrieval was degraded by provider/authentication failures. No-match output is not complete retrieval evidence.

The first committed-head manual Greptile review returned 4/5. [Its two confirmed findings and fixes](greptile-review.md) have red-before-green scope proof, 25 focused tests and 16 explicit recipe cases. Follow-up strict template TypeScript, `make check`, `make build`, documentation/frozen-scope validation, and all 55 installed Delivery cases passed. This follows the earlier full `make verify` run with 54 Delivery cases. Implementation head `1ebb77b3077ca48b0af38d47b2f660276fd4616f` passed all active CI jobs and manual Greptile 5/5 with zero comments. [The readiness receipt](implementation-readiness.json) records the source result. The effort closes with its verified work; subsequent documentation heads receive their own current-head CI and manual review before handback. PR #123 remains open and unmerged.

## Integrated review ledger

Codex GPT-6.1 Sol, high, returned CHANGES for three reachable issues. S1: different selected graph generations allowed Copy. A new pure counterexample failed before the fix; prompt composition now requires one generation, and the stage also gates aggregate selected-effort coverage. S2: refresh pruned a selected story silently. The installed regression failed before the fix and passed after retaining previous selections, reporting removed eligibility, and requiring explicit acknowledgement or reselection. S3: raw Markdown lacked source-aware navigation. The installed wikilink/relative-plan regression failed before the fix and passed after reusing native conversion and the public preview resolver with source path and authored syntax. Source-link cache keys retain that syntax, so Markdown and wikilink resolution do not share incompatible entries.

Validation after these fixes: 24 domain/adapter tests passed, strict template TypeScript passed, 35 public-kit/content unit tests passed, and both installed review regressions passed. The final expanded installed suite passed all 54 cases, and both `make check` and `make verify` passed. The final shared-fix recheck approved the provider-aware cache routing.

## Windows installer-test follow-up

[Windows web/CLI CI](https://github.com/atomicobject/rhizome/actions/runs/42767165324/job/129666291549) failed on `46e792283` because the new fresh-install assertion compared raw template and installed skill bytes. Git checked out the template with CRLF; the existing installer emitted LF for its final newline. The only differing byte was that newline's carriage return. The test now uses the existing `normalizeTemplateNewlines` helper on both values. It still compares every other character and line exactly. Production code, installation behavior, native-view byte goldens and authored-record preservation assertions are unchanged. Fresh install, managed update and eject tests passed locally, followed by `make check`. [Windows web/CLI CI passed on `4d40d2c21`](https://github.com/atomicobject/rhizome/actions/runs/42770299540/job/129676410644). All active CI jobs at that head passed, including desktop portability. Subsequent heads still require their own checks.

The subsequent G3 owner-coverage correction passed strict template TypeScript, 25 focused tests, `make check`, `make build` and all 55 installed Delivery cases. Clean sibling and neighboring-spec prompts remain available beside ambiguous sources; the affected selection stays disabled and prose stays readable. See [the manual review ledger](greptile-review.md).

The G4 manual-refresh fallback was reproduced with the event feed unavailable: no selected-detail read occurred and Copy remained enabled. The existing live-event path already worked. Explicit selection invalidation and generation-coherent merging now pass the held-read, broken-scope and recovery regression. Both hook callers pass strict TypeScript; `make check`, `make build` and all 56 expanded installed cases pass. [The review ledger](greptile-review.md) distinguishes the valid counterexample from an unusable stale-build setup attempt.

Final G5 verification passed strict TypeScript, make check, make build and all 57 installed cases. The valid HTML/ambiguity counterexample has red-before-green proof. A pending route handler caused one teardown failure; constraints tests now use Playwright's documented wait-for-handlers cleanup, preserving every assertion and surfacing handler errors. No retry, timeout or skip changed. The existing computed parent-spec total is now visible beside Ready story counts, with matching completeness qualifiers. Both themes have installed label assertions. [Final screenshot review](final-capture-review.md) approved all 47 captures; root inspected all 40 changed captures after the count correction, with seven byte-identical.

## Implementation readiness

At `1ebb77b3077ca48b0af38d47b2f660276fd4616f`, PR #123 was open, non-draft, mergeable and clean, with auto-merge absent. All active CI jobs passed; the existing conditional installed-upgrade job was not applicable. Manual Greptile returned 5/5 with no comments, and GitHub had no review or issue comments. Root reviewed the integrated diff and every later source delta. Local final gates and all 57 installed cases passed. This exact receipt is retained in [implementation-readiness.json](implementation-readiness.json). The final documentation head is checked again before handback.

## G6 image follow-up

The review of documentation head `0460bea13` found that source-relative Markdown images inherited the experience frame URL. Delivery now preserves their authored label and path with a source fallback because the kit has no public binary attachment route. Absolute image URLs retain normal rendering. The effort was reopened for this final readiness step. Both-theme installed red-before-green cases verify the fallback, decoded absolute data image and canonical source section navigation. Strict template TypeScript, make check, make build, all 59 installed cases and documentation/frozen-scope validation passed. Root inspected the paired captures; they supplement the earlier 47 approved major-state captures. External readiness is recorded below.

## Final implementation readiness

The image follow-up at `301faea690e22c0ec93337d652c776a4b5df9218` passed manual Greptile 5/5 with zero comments and all 25 active CI jobs; the existing conditional installed-upgrade job was not applicable. PR #123 was open, non-draft, mergeable and clean, with auto-merge absent and no GitHub review or issue comments. Local gates and all 59 installed cases passed. Root reviewed the source delta and both new fallback captures. [The final receipt](follow-up-readiness.json) preserves this result separately from the earlier implementation receipt. The effort is complete on the branch; the documentation head receives its own CI and manual review before handback.

## Windows init settings-test isolation

Documentation head `dab298fa6` passed manual Greptile 5/5 with zero comments but failed Windows cmd-validate in an existing settings test. The first setup discarded its structured error. A controlled unavailable-release repro exposes a real installer dependency; reusing the existing pinned-binary fixture removes that dependency without changing production behavior or settings assertions. The original Windows error remains unknown. [The diagnosis](windows-init-test-diagnosis.md) records the controlled red/green evidence, exact uncertainty and approved independent fixture review. All focused init JSON cases, make check and both documentation validators passed. Current-head review and Windows CI remain pending; no retry, timeout increase or skip was added.

## G7 Markdown destination serialization

Manual review at `cc10aeea3` scored 5/5 and identified unescaped parentheses truncating a valid Markdown destination. The installed counterexample returned a public-preview 404 before the correction and opens the canonical `plan(v2).html` viewer afterward. Delivery now percent-encodes only literal parentheses when serializing Markdown resolver input; the existing server decoder preserves path identity and existing escapes. No shared parser changes. Strict template TypeScript, make check, make build, all 60 installed cases and both validators passed. A missing blank line in the new test failed the first broad lint attempt; the corrected source passed the full chain. Manual current-head review and CI remain pending. Windows cmd-validate passed on cc10aeea3; the separate macOS updater fixture correction remains under independent review before integration.

## macOS updater measurement correction

The expected read-timeout status passed in the failed macOS job; its separate total-duration bound included feed and connection setup. [The controlled diagnosis](updater-test-clock-diagnosis.md) reproduces a correct 250 ms body timeout rejected at 450 ms total, then passes with the same thresholds measured from the first package byte. A fresh independent Codex GPT-6.1 Sol high review approved the test-only correction. Root repeated the red/green proof, verified exact source restoration and passed standard make desktop-check with actual sidecars, all 151 UI tests, six script tests, 145 Rust tests, fmt, clippy and disposable app-instance tests. Production updater code, every threshold and existing retry-behavior assertion remain unchanged. The precise original CI schedule is unobserved. Current-head CI and manual review remain required; no Rhizome Dev app was replaced.

## G8 scoped diagnostic ownership and final integration

One failed criterion no longer disables unrelated sibling work. The public batch executor supplies exact requested-ref ownership only for errors raised inside that item selection; unknown errors remain shared. The adapter preserves one batch, canonical server refs, generation guards and partial portfolio evidence. Real query regressions, 27 pure tests and all 64 installed cases pass. A new held-refresh case also proves selection acknowledgement waits for a completed read. Fresh Sol high foundation and consumer reviews approve these deltas. Root inspected four new light/dark captures; the manifest contains 53 captures and 29 committed images.

The broad query gate exposed an existing SSE fixture timer that included admission. Its independently approved correction starts the same 20 ms body collection bound after real HTTP headers, retains cursor and status assertions, and changes no production code. The original error body was discarded, so the controlled failure mechanism is established without claiming the exact original cause. [The diagnosis](sse-fixture-diagnosis.md) records both. Root final gates and committed-head CI/manual review remain pending.

Final root verification passed make check, strict template TypeScript, all 27 pure tests, the query/pushdown integration packages, the 64-case installed suite, and documentation plus frozen-scope validation with zero issues. The initial root gate failed only on five missing blank lines in the held-refresh test; lint and the complete gate passed after formatting. The integrated SSE test passed within the full repository gate. Committed-head manual review and CI remain pending.

## G9 canonical note-root relationship follow-up

Greptile at 3f394df72 confirmed G8 resolved and exposed catalog-versus-hydrated root metadata hiding frozen-spec relationships. Joining only fragment-free NOTE roots by canonical path fixes all shared identity consumers while preserving section and criterion selectors. Pure and both-theme installed counterexamples fail before and pass afterward. Fresh independent Sol high review approves the correction. make check, make build, all 64 installed cases, 28 pure tests and strict template TypeScript pass. Root inspected both new relationship captures; the manifest contains 55 captures with 31 committed images. A separate old-head Windows running-deadline fixture failure remains under controlled diagnosis.

## Windows running-deadline fixture correction

The 3f394df72 Windows rest job panicked when an existing deadline test assumed running-error data after a protocol error. A controlled expired receipt reproduces the same panic, but the original underlying protocol error remains unconfirmed. The test now uses Go's standard virtual-time bubble so setup completes and handler entry is proven before the same 20 ms deadline. Both success/error outcomes and mayHaveExecuted assertions remain; the adjacent queued-deadline case stays unchanged. Fresh independent Sol high review approved the test-only correction. [The causal record](running-deadline-fixture-diagnosis.md) preserves the proof and uncertainty. Worker focused/race/package/make check passed; final root integration gates and new-head Windows CI remain pending. No production code, retries, skips or timeout increases were added.

Final root integration passed the focused deadline/queue/cancellation race tests, make check and both documentation validators with zero issues. G9 also passed the rebuilt 64-case installed suite and all 28 pure tests. No application source changed after that installed proof. New-head CI and manual review remain required.
