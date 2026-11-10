# Browser failure diagnosis

Two native-suite failures were investigated without changing retries, timeouts, skips, or assertions.

## HTML metadata commit

The traced native run passed 157 cases and failed the HTML metadata save's updated-tab assertion. The commit returned 200 with saved bytes and the correct rendered title, but its workspace claimed Markdown format and the basename as its title. The pending public node read was then cancelled because the client correctly treated the returned commit workspace as authoritative.

The normal node-projection cache calls Markdown snapshot parsing for every path. `resolveNodeProjection` now sends registered non-Markdown sources through the existing provider-aware read scope. Markdown and legacy unregistered paths retain their cache behavior. No parser, lifecycle, or watcher was added.

The focused HTTP save counterexample compares cached and uncached servers. Before the fix, only the cached case failed, returning `markdown` instead of `html`. After the fix, both preserve HTML format, the saved metadata title in both node and content, and the authored body bytes. Existing cache, custom-view, and workspace tests also passed. Final shared-fix review approved the routing and its reachable callers.

Local logs: `/private/tmp/ae-html-cache-red.log`, `/private/tmp/ae-html-cache-green.log`. The retained trace is `/private/tmp/ae-native-html-diagnostic/trace.zip`.

## Mooring external file change

The first combined verification run failed an unchanged native preference test after it atomically replaced a Markdown note. Its diagnostic stream had no watcher admission during the assertion interval: the prior watcher job finished at 05:14:38.717 UTC, and the next admission was 05:14:50.201 UTC with no queue or lock wait. The latter job did not report a note ingestion. Because diagnostics omit affected paths, attributing that later job to the test's restoration remains an inference.

The isolated six-case preference file passed. The subsequent complete traced native run also passed that case, in 919ms. Neither pass proves the original event-delivery failure fixed. Only that test references the Mooring note; no competing writer was found. Index-backed reads cannot observe an external edit before its publication, and a page reload does not force publication.

The non-traced final combined run reproduced the same failure. Its diagnostics again showed no watcher admission during the assertion window, and no stale-triggered jobs in the retained run. A further native run with temporary event instrumentation reproduced it as well. Standard logger redaction prevented that instrumentation from distinguishing event identities, so direct fixture-only timestamps are being collected before choosing a fix. No timeout, retry, skip or assertion has changed.

Direct timestamps then localized a measured delay before application ingress. The same atomic save/restore sequence produced:

| Operation | Write completed (UTC) | OS event received (UTC) | Hub batch flushed (UTC) | Delay before ingress |
| --- | --- | --- | --- | --- |
| Save | 05:50:51.462 | 05:50:52.770717 | 05:50:52.922920 | 1.309 seconds |
| Restore | 05:50:53.490 | 05:50:59.926757 | 05:51:00.078520 | 6.437 seconds |

Both batches flushed about 150ms after ingress. Neither took the stale route. This observed OS delay exceeds the preference test's five-second assertion deadline. The existing Darwin backend test explicitly leaves FSEvents delivery timing unbounded under load. The exact OS receipt times of the earlier failures were not captured, so this is evidence of the invalid timing assumption rather than a retrospective claim about each failure.

The preference test now saves and restores the same summary through the real public edit-session API. It still asserts collapse through query changes, the content save, card/table switches and reload, and still checks the saved summary. The API's committed outcome supplies the relevant publication boundary. Other native filesystem-change tests remain enabled. No watcher behavior, retries, timeouts, skips, or assertions changed. All temporary instrumentation was removed. The focused six-case preference file passed with this fixture correction.

Local evidence: `/private/tmp/ae-integrated-verify.log`, `/private/tmp/ae-native-freshness-diagnostic.log`, `/private/tmp/ae-native-full-trace.log`, `/private/tmp/ae-native-event-direct.log`, and `/private/tmp/ae-native-preferences-api-green.log`. These logs concern disposable synthetic vaults.
