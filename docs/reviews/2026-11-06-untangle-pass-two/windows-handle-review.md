---
summary: "Focused Sol review of the Windows released-handle correction."
reference-kind: analysis
---

# Windows released-handle review

VERDICT: APPROVE

No findings.

Scope: only the uncommitted delta from e0527544ca1f6e58b0bdc45e44b7289c7cbc0c0f in the five requested files. HEAD equals that base; git status reports only those five modified paths. Earlier refactoring was treated as approved and inspected only where needed to trace the affected caller.

Correctness evidence:
- pkg/harness/internal/jsonstdio/kill_windows.go:10-20 accepts syscall.EINVAL only after observing the supplied closed reaping channel. Open and nil channels still reject it; unrelated errors remain rejected.
- pkg/harness/internal/jsonstdio/transport.go:100-112 gives the tracker cmd.Process and closes that transport's exited channel after cmd.Wait returns. At :316-328, cleanup checks that same channel before classifying the error. On Windows, pkg/proctree/proctree_windows.go:21 calls Kill on that same process object. The exception therefore remains tied to the owned child.
- The pinned Go 1.27.3 os/exec_windows.go:48-61 releases the process with statusReleased after waiting and returns syscall.EINVAL when subsequently signaling that released process. The official source agrees: https://go.dev/src/os/exec_windows.go. This supports the reported failure without treating arbitrary permission or termination failures as success.
- pkg/harness/internal/jsonstdio/kill_windows_test.go:15-28 covers both allowed error kinds, wrapped access-denied errors, open/nil signals, and unrelated failures. At :31-40 the new test launches a real child with cmd.Run (Start plus Wait), checks its actual subsequent Kill error, and passes it through the classifier. Before this fix, its final classifier assertion would fail on Windows. The existing environment test exercises the transport caller.
- pkg/harness/internal/jsonstdio/kill_other.go remains unchanged and always returns false. No Unix cleanup error classification, shutdown budget, or process-tree policy changes are introduced.
- The three documentation changes accurately describe the bounded Windows exception and retain the requirement for updated CI results.

Verification:
- Inspected the requested diff, git status, surrounding Close/closeProcess, the Windows proctree caller, and local plus official Go source.
- git diff --check for the five requested paths passed.
- /tmp/rhizome-untangle-windows-rest-failure.log records the owned child exiting with code 0 followed by TestSessionEnvironmentDoesNotLeak failing with "kill process: invalid argument".
- /tmp/rhizome-untangle-windows-handle-check-unrestricted.log was inspected, but no final make check success was present at review time. I did not start a competing verification run.

Reviewer metadata exposed by this session: OpenAI via Codex; gpt-6.1-sol; xhigh reasoning effort. Token usage was not exposed.

Not checked: independent test execution or native Windows execution. Windows runtime verification and the completed make check result remain pending; this is source approval, not a claim that CI is green.
