---
summary: "Review of platform-specific CI corrections for the untangle PR."
reference-kind: analysis
---

# CI supervisor fixture review

**Approved. No findings.**

Reviewed only the current change to `scripts/dev_supervisor.test.mjs`, its three `restartingSupervisor` consumers, and the unchanged production readiness/restart implementation. Also read the supplied coarse-clock red log. No tests or production commands were run by this reviewer.

- The ready fixture now writes an explicit synthetic mtime in whole seconds, rounded upward. That keeps its intended fresh manifest at or after the restart's `startedAt` threshold even when automatic filesystem timestamps are coarse. It still goes through the real file, PID, origin and freshness admission path. The separate stale-manifest and foreign-PID test retains its explicit old timestamps and rejection assertions.
- The shutdown test now observes an early restart rejection instead of leaving it unhandled while waiting forever for `stopVite`. If restart completes before reaching that stop, the new assertion fails the test. When the stop is reached, the existing sequence still sets shutdown while that stop is blocked, releases it, awaits restart completion and requires zero new Vite starts. The losing promise remains observed by `Promise.race`.
- No production logic, timeout, retry or existing result assertion was weakened or changed.

The supplied red log records the two expected pre-fix failure modes. Coarse-clock green and the full ten-test run remain root's verification work.
