# Session JSON stdio ownership

`Transport[M]` owns newline-delimited JSON framing, serialized and cancellable stdin writes, the bounded stderr tail, and one child process. Codex and Claude provide their frame types and vendor labels, then keep protocol routing, approvals, and session state in their drivers. One-shot command execution has a separate owner.

`Start` finishes initialization before launching readers or process reaping. Stdout and stderr use transport-owned pipe readers so `exec.Cmd.Wait` cannot discard buffered final frames. `Close` interrupts input, follows the caller's shutdown deadline, kills and reaps a child that does not exit, kills the child's group, session, and every descendant its `pkg/proctree` tracker recorded, with the sessions they lead (Claude Code gives shells sessions of their own, reparented once it exits), on Unix so descendants cannot survive it, and releases output readers.

`Exited` closes as soon as `Wait` reaps the child, before logging its exit. A blocked log destination must not delay the process completion signal or consume the shutdown budget. With a shutdown deadline, graceful waiting reserves the existing one-second kill/reap window for process inventory and cleanup; short deadlines force termination without spending that remaining budget on grace. The no-deadline grace period is unchanged.

`Recv` returns framing failures and stdout EOF immediately with already-captured stderr. It must not wait for stderr EOF: the session needs that error to resolve its active turn and approvals and begin cleanup. Stderr draining can complete during bounded `Close`.

On Windows, TerminateProcess can report access denied after the process has already exited, and Go returns EINVAL after Wait releases its handle. The transport accepts those specific errors only after observing the exact child’s reaped signal; errors for an unreaped child and Unix process-group failures remain failures.
