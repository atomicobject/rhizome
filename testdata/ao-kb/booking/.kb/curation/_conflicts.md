---
type: registry
slug: conflicts
generatedBy: conflicts.py
---

# Conflict Registry

One entry per `contradicts` pair, generated and merged by
`skills/process/conflicts.py` — run it, don't hand-edit the fields it owns.

- **The script owns**: State (except the human states below), Impact, Impact
  reasons, Dependents, Dependent reasons, Winner, Rule, Updated.
- **People and escalate mode own**: Escalation count, Last escalated,
  Resolution, Impact override, and any state of `resolved-team` or
  `accepted-trade-off`. To reverse a rule decision, set State to
  `resolved-team`, set Winner, add a Resolution line, and re-run with `--apply`;
  the tool moves the `supersedes` edge to the new winner.

The stored fact is the `supersedes` edge on the winning assertion; this file
records why it is there. States: `open` (a real dispute), `auto-superseded`
(a `supersedes` edge exists — written by the rule, at intake, or by hand),
`resolved-default` (open + blocking, escalated past the threshold with no
answer — reversible), `resolved-team`, `documented-trade-off` (trade-off tied
to a decision), `accepted-trade-off`, `closed-edge-removed`. Impact:
`blocking` (cited by a live artifact, a decision/constraint, or priority now)
or `sideline`.

Dependents counts what rests on either side — artifacts (and the root decision
log) citing it, assertions whose edges depend on it, product-model items naming
it — each counted once.
`↓` is a dependent at a more concrete layer (the claim ripples down into later
work), `↑` one at a more abstract layer (earlier work now rests on this
decision). A bare number means the vault has no layer config, so direction is
unknown.

## Entries

### cf-001: same-day-cancellation-fee ↔ same-day-cancellation-free

- **Assertions**: [[same-day-cancellation-fee]], [[same-day-cancellation-free]]
- **State**: open
- **Impact**: blocking
- **Impact reasons**: override: blocking; cited by a live artifact; priority now
- **Dependents**: 2
- **Dependent reasons**: booking-prd cites same-day-cancellation-free; booking-policy-review cites same-day-cancellation-fee
- **Winner**: —
- **Rule**: tied on authority and date; needs a tiebreak
- **Detected**: 2026-10-22
- **Updated**: 2026-10-30
- **Escalation count**: 2
- **Last escalated**: 2026-10-30
- **Resolution**: Clinic lead must confirm the policy.
- **Impact override**: blocking
- **Clinic annotation**: Keep both authored claims visible.

### cf-002: reminders-forty-eight-hours ↔ reminders-twenty-four-hours

- **Assertions**: [[reminders-forty-eight-hours]], [[reminders-twenty-four-hours]]
- **State**: auto-superseded
- **Impact**: sideline
- **Impact reasons**: —
- **Dependents**: 0
- **Dependent reasons**: —
- **Winner**: reminders-twenty-four-hours
- **Rule**: supersedes edge present (written at intake or by hand)
- **Detected**: 2026-10-22
- **Updated**: 2026-10-24
- **Escalation count**: 0
- **Last escalated**: —
- **Resolution**: —
