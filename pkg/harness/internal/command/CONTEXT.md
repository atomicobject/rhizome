# Short-lived harness commands

`OSRunner` owns command lookup, spawning, output capture, cancellation, waiting, and process logs for Claude and Codex version probes and generation. `Spec` carries the executable, arguments, directory, and input; `Result` carries stdout, the last 16 KiB of stderr, and the completed exit code.

- Inherit the caller's environment and login without reading credentials or overriding HOME.
- A completed nonzero exit returns its code and diagnostics with no Go error. An actual context-triggered kill returns the context error with captured output. Cancellation after completion preserves the completed result.
- Keep the successful cancellation marker separate from the context's final state. The caller still owns phase mapping and version-cache behavior.
- Vendor drivers choose the process-log label and retain their small private runner interfaces and fakes. Session protocols and their process lifetime belong to `internal/jsonstdio` and the vendor session owners.

See [[coding-agent-harness]] and the [harness subsystem guidance](../../../../docs/reference/subsystems/harness.md).
