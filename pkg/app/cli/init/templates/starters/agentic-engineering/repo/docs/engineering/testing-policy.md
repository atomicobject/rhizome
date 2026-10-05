# Testing policy

Consulted when choosing what to test, at which layer, and with what. Used during planning, implementation, and closure.

## Defaults

Confidence should be proportional to the change, with a regression test for any bug that could return. Start with the smallest test that demonstrates the intended behavior. For a defect, reproduce the failure first and keep the regression test with the fix.

Prefer behavior-level assertions over implementation details. Keep fixtures deterministic and close to the contract they exercise. Do not add tests for reversible, low-impact changes that only mirror the implementation.

## Layers

| Change shape | Evidence |
| --- | --- |
| Local behavior | focused unit or package test |
| Cross-module contract | boundary or integration test |
| Migration or persisted state | replay, upgrade, and idempotence coverage |
| User-visible workflow | one representative end-to-end path when practical |
| Bug fix | regression test, or a recorded reason none is possible |

## Failure handling

A flaky or environment-dependent test is a quality signal, not a reason to delete coverage. Isolate the cause when possible; otherwise record the degraded evidence and who owns it. Review a changed observable contract before updating any snapshot.

## Team extensions

Add test locations, fixture ownership, integration environments, coverage expectations, and the change shapes that always require the broader layers. Put executable commands in [quality-gates.md](quality-gates.md).
