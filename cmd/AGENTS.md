# Agent CLI tests

Test public JSON, stderr, flags, and runtime wiring through real `rzm agent` commands. Keep `agentcode.PlanStartRequest` classification cases in `pkg/app/agentcode`; cmd tests own only the CLI behavior and composition they can observe. Do not keep test-only runtime policy wrappers for declaration flags.

Construct a fresh command tree for each execution and supply the fixture's Env. Execute through the invocation wrapper so factory configuration and execution cancellation both reach the command. When configuring a child's input or output directly, execute that same tree.
