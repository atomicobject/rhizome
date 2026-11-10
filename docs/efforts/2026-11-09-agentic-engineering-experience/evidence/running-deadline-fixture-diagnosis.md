# Running deadline fixture

Base 3f394df72fb976ce89a7a87061f308a3d3d902d9. Agent codex/gpt-6.1-sol, high reasoning. No delegates.

## Proven facts

The supplied Windows log records TestRunningDeadlinePreservesExecutionUncertainty/false panicking at server_test.go:212 because Data was map[string]string rather than map[string]any. It does not identify the underlying protocol error or handler entry.

readFrames records receivedAt before queue admission. handleFrame forwards it to serverState.run. run derives an absolute deadline from receivedAt and timeoutMs. Expiration before admission or before handler entry returns protocolError. writeFault then keeps rpcFault's map[string]string with code alone. After callback entry, deadline expiry yields executedError, and writeFault emits map[string]any with mayHaveExecuted true and an optional completed outcome. A successful callback retains its outcome; a callback error omits it.

Controlled red changed only the original test's supplied receipt timestamp to time.Now().Add(-20*time.Millisecond), keeping timeoutMs 20. The original assertions reproduced the identical map conversion panic at line 212. The patch and raw output are adjacent ae-running-deadline-controlled-red.patch and ae-running-deadline-red.log. This is a controlled source-backed counterexample, not a reconstruction of the unseen Windows scheduling sequence. Other ordinary protocol failures also use map[string]string, so the CI panic alone does not prove pre-admission expiration.

## Fix

Wrap each existing branch in testing/synctest.Test. Go 1.27.3 supports the standard package. All fixture state and channels are in memory. Fake time cannot advance during straight-line setup or validation and advances when the callback waits on ctx.Done. The same timeoutMs 20 remains. Assert callback entry, a live context at entry, exactly 20 ms elapsed, an error, nonnil fault, and a checked Data type before preserving the existing code, mayHaveExecuted, and optional outcome assertions.

No sleeps, retries, timeout increases, skips, external I/O, helpers, or production changes were added. The existing adjacent queued-deadline test remains responsible for expired queued requests. Focused neighboring queue, cancellation, sibling cancellation and admission-abort cases passed with the fixed fixture.

## Checks

Focused command: GOCACHE=$PWD/.gocache go test -tags fts5 ./pkg/app/agentcode -run 'Test(RunningDeadline|QueuedCallDeadline|ServeCancellation|ServeRejectsUnselected|QueuedCancellation|AbortCancels|ServeOverlaps)' -count=1 -v. Passed. Raw output ae-running-deadline-green.log.

The same focused command with -race passed, raw output ae-running-deadline-race.log. The full package command GOCACHE=$PWD/.gocache go test -tags fts5 ./pkg/app/agentcode -count=1 also passed, raw output ae-running-deadline-package.log. make check initially stopped before checks because web/node_modules was missing. npm ci installed those dependencies without tracked changes. make hooks-setup passed with required Git config access. Final make check passed with exit 0. It includes Go unit tests, Go analysis and vet, content and size checks, web lint and type checks, credential scan, team API checks, 270 web test files and 2169 web tests, and 10 Node environment tests. Raw output ae-running-deadline-make-check.log. The commit pre-hook also passed and reported no leaks, raw output ae-running-deadline-commit.log. The commit changes only pkg/app/agentcode/server_test.go. No independent review, Windows run, full integration gate, PR operation, push, merge or Dev installation was performed by this leaf. Root owns independent review, integration gates and external readiness. No Windows runner was executed locally.

## Independent review and root integration

A fresh codex/gpt-6.1-sol high reviewer approved 60c19fc3a with no verified reachable findings. It inspected the bubble-local state, timer and channel, catalog validation, both callback branches and cleanup. Go 1.27.3 is selected by the repository and CI. The reviewer ran no tests and did not independently execute Windows; it retained the uncertainty about the original protocol error. Root integrated the identical test-only patch as ae847e288 after inspecting it. Root focused race tests, make check and documentation plus frozen-scope validation passed. Fresh Windows CI and committed-head manual review remain pending.
