# Session JSON stdio ownership

`Transport[M]` owns newline-delimited JSON framing, serialized and cancellable stdin writes, the bounded stderr tail, and one child process. Codex and Claude provide their frame types and vendor labels, then keep protocol routing, approvals, and session state in their drivers. One-shot command execution has a separate owner.

`Start` finishes initialization before launching readers or process reaping. Stdout and stderr use transport-owned pipe readers so `exec.Cmd.Wait` cannot discard buffered final frames. `Close` interrupts input, follows the caller's shutdown deadline, kills and reaps a child that does not exit, and releases output readers.

`Recv` returns framing failures and stdout EOF immediately with already-captured stderr. It must not wait for stderr EOF: the session needs that error to resolve its active turn and approvals and begin cleanup. Stderr draining can complete during bounded `Close`.
