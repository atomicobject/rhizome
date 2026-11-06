---
type: curation
---
# Conflict Registry

## Active

### cf-001: Same-day cancellation policy
- **Assertions**: [[same-day-cancellation-fee]], [[same-day-cancellation-free]]
- **State**: open
- **Impact**: blocking
- **Impact reasons**: Patient price is disputed.
- **Dependents**: [[booking-prd]], [[booking-policy-review]]
- **Dependent reasons**: Both artifacts publish cancellation guidance.
- **Winner**: —
- **Rule**: equal-authority-no-winner
- **Detected**: 2026-10-22
- **Updated**: 2026-10-30
- **Escalation count**: 2
- **Last escalated**: 2026-10-30
- **Resolution**: Clinic lead must confirm the policy.
- **Impact override**: blocking
- **Clinic annotation**: Keep both authored claims visible.

## Settled

### cf-002: Reminder lead time changed
- **Assertions**: [[reminders-forty-eight-hours]], [[reminders-twenty-four-hours]]
- **State**: auto-superseded
- **Impact**: sideline
- **Impact reasons**: Reminder decision changed.
- **Dependents**: —
- **Dependent reasons**: —
- **Winner**: [[reminders-twenty-four-hours]]
- **Rule**: newer-direct-decision
- **Detected**: 2026-10-22
- **Updated**: 2026-10-24
- **Escalation count**: 0
- **Last escalated**: —
- **Resolution**: Recorded successor preserves the earlier decision.
