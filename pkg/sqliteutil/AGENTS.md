# SQLite utility test ownership

- Corruption and contention fixtures must reach the real failure and require the intended non-nil error classification. A successful result cannot satisfy a failure-path test.
- Per-connection setup tests reserve several physical connections at once and compare against the intended literal setting. Lock-release tests assert the waiter's acquisition result, not just goroutine completion.
- `cmd` worktree snapshot tests own committed-WAL copy behavior; `pkg/anchors/sqlite` owns warm schema-probe cost. Keep generic migration behavior here, without observation callbacks added only to replay those contracts.
