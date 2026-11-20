# Review and approval

Consulted when classifying risk, finding an approver, and deciding whether a pause is wanted. Used during specification, planning, and closure.

## Risk classes

Effort-driven work is work that changes behavior across a boundary, introduces a durable contract, carries material uncertainty, needs several phases, or has meaningful rollback or operational risk. It gets a spec, a frozen effort, an approved plan, then implementation.

A clear local task is everything else. It combines scope, change, and focused verification in one pass without an effort.

## Approval

An effort-driven plan needs explicit approval in chat before implementation, recorded on the effort when a current user is configured. An approved plan authorizes all covered steps: resolve routine implementation choices and continue, and ask only for a material decision outside that authority or an unauthorized destructive action.

## Pauses

A pause for foundation review is requested by the plan when an early phase fixes APIs, data shape, or ownership that later phases depend on. Foundation review is not a standing rule after every phase; the gates below are, when listed.

## Gates

An effort passes each gate listed here at the point named. A gate is part of the approved work: it records its outcome in the effort's execution notes and is not repeated for the same scope, plan revision, or reviewed changes. The Gate column names the skill to run, and this table is the only place the workflow learns which gates exist: add a row to turn on a gate skill of your own, and remove a row to turn one off. A row naming a skill that is not installed is a setup gap to report, not a gate to improvise. The workflow reads this table at four points: before planning, before asking for plan approval, at the end of each implementation phase, and before closure. A row for any other point is not run.

| When | Gate | Decided by |
| --- | --- | --- |
| After scope freezes or re-freezes, before planning | `effort-research` | The user chooses Run or Skip |
| After the research decision, before planning | `effort-discuss` | The user chooses Discuss or Skip; silence is not Skip |
| After a plan is drafted or structurally revised, before asking for approval | `plan-check` | Always runs |
| After each implementation phase, and once before closure for a single-phase plan | `phase-code-review` | Always runs |

## Escalation cues

Raise a local task to an effort when it reveals a cross-subsystem contract or data migration, would constrain future APIs or deployment, needs evidence that does not fit one focused change, or a review or incident changes the intended outcome.

## Team extensions

Name approvers by area, the review expectations for each risk class, branch protection, and any change that always needs a second person.
