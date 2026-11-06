---
summary: "Review of platform-specific CI corrections for the untangle PR."
reference-kind: analysis
---

# Windows CI correction review

**Verdict: APPROVE. No concrete findings.**

Read only the implemented delta in `jsonstdio/transport.go`, `kill_windows.go`, `kill_other.go`, `kill_windows_test.go`, and `repositorytrust/check_test.go`, plus the worker's implementation report. No tests, builds, subagents, or repository edits were performed.

- The exception is called only after a kill attempt and after the transport receives from that exact child's `exited` channel. The Windows helper independently requires the channel to be ready and the error to match the actual `syscall.ERROR_ACCESS_DENIED`, including wrapped syscall errors.
- A nil or still-open exit channel cannot authorize the exception. Unreaped, timeout, and cancellation paths retain the kill failure. Other errors remain errors, and the non-Windows helper always returns false, preserving Unix group cleanup error handling.
- The Windows test includes the needed negative cases: the same error while unreaped, absent process identity, unrelated errors, and the general permission sentinel. It then checks the raw and wrapped Windows error after channel closure.
- The trust fixture now uses a real store directory with a directory at the exact canonical record path. It exercises an actual read failure without relying on how a platform treats a file used as a parent directory. Cancellation fallback, injected lookup failure, and the requirement to return a trust-read error remain intact; production trust behavior is unchanged.

The original environment test and shutdown timing policy are unchanged. Root still needs the planned focused local tests, Windows cross-compilation, and actual Windows CI execution; this source approval does not establish those results.
