# Testing policy

Consulted when choosing what to test, at which layer, and with what. Used during planning, implementation, and closure.

## Defaults

Confidence should be proportional to the change, with a regression test for any bug that could return. Work red-green: reproduce a defect first, keep the regression test with the fix. Start with the smallest test that demonstrates the intended behavior, prefer behavior-level assertions over implementation details, and avoid low-signal tests that only mirror the implementation.

## Layers

| Change shape | Evidence |
| --- | --- |
| Go package behavior | focused package test (`go test ./pkg/<path>/...`), race-enabled in the full gate |
| Cross-package or CLI contract | `cmd` tests, and the integration suite (`go test -tags=integration ./...`) when indexing, code intel, or fixtures are involved |
| Init templates, skills, or managed docs | `pkg/app/cli/init` tests plus a real `rzm init` followed by `rzm init --check` exiting 0 |
| Migration or persisted state | replay, upgrade, and idempotence coverage; SQLite changes go through migrations, never compatibility shims |
| Web UI | web unit tests and type checks; `make web-e2e` when the change affects the user-visible flow |
| New language support | the polyglot fixture pattern in [code-intel](../reference/subsystems/code-intel.md) |
| Bug fix | a regression test, always |

## Failure handling

A flaky or environment-dependent test is a quality signal, not a reason to delete coverage. Tests that bind loopback sockets fail inside the sandbox; run them with local socket access and record that. Review a changed observable contract before updating any snapshot or golden file.

## Team extensions

Fixture vault: `testdata/integration/python-app/vault` (register once with `rzm vault add testvault ...`). Dogfood other repositories with the built binary, never a PATH `rzm`, per the AGENTS.md dogfooding notes.
