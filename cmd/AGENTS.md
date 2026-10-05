# Agent CLI tests

Test public JSON, stderr, flags, and runtime wiring through real `rzm agent` commands. Keep `agentstart.PlanRequest` classification cases in `pkg/app/agentstart`; cmd tests own only the CLI behavior and composition they can observe. Do not keep test-only runtime policy wrappers for declaration flags.

When a test runs a reused Cobra child command that inherits captured output, clear its explicit writer in `t.Cleanup`. Restoring `rootCmd` alone can leave the child holding a closed pipe on the next test. Keep this cleanup local to the test that captures it.
