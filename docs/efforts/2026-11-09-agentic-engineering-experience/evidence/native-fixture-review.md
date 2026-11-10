# Native fixture causal review

[Native fixture causality review](t3-thread://v2/b6cc208d-de4b-4b15-a36f-c28b251e92c7/thread:delegated-task:command%3Amcp%3A6e466306-8b4e-47d7-b21f-fded108c08d1%3Adelegate-task%3Aae-native-fixture-review-v1)

APPROVE. No reachable blockers in this narrow fixture change.

- The [timing log](/private/tmp/ae-native-event-direct.log) supports delayed OS delivery. Restore reached ingress after 6.437 seconds, then hub flush followed about 152 ms later. The captured first save passed, so this supports the causal explanation without proving every earlier failure.
- [saveMooringSummary](web/tests/e2e/view-preferences.spec.ts:24) uses the real public create and commit endpoints. Commit applies the source transaction, refreshes projections and caches, and publishes invalidation through the existing application path.
- Type System Discipline informed the boundary check. Responses stay `unknown` until runtime guards validate the session ID and committed outcome. HTTP failures and conflicts fail the helper.
- Restoration runs in `finally` through the same commit path. Its value matches the checked-in fixture. All original preference and displayed-summary assertions remain.
- Watchhub has zero diff and no diagnostic instrumentation remains there. Physical filesystem coverage remains elsewhere. The supplied focused log records six passing cases in 11.2 seconds.

I ran no tests or builds and made no edits. External-save delivery latency remains a real limitation; this fixture change does not establish a five-second guarantee or justify claiming that limitation was fixed.


