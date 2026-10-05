# Closure

Deliverable: a truthful closure state with delivery and closure evidence under `docs/engineering/quality-gates.md`, `docs/engineering/release.md`, and `docs/engineering/review-and-approval.md`. Return to implementation if feature work remains.

## Gather evidence

- Load `query-recipe run --id closure-drift-pack` anchored to the owning effort, or reuse a current equivalent result. Refresh evidence affected by later changes.
- Inspect `ref.typeName`. Read the Markdown effort's inline plan and lifecycle sections, or the workspace overview, complete linked plan, Markdown work log, and selected materials. Reconcile the exact approved plan revision, frozen scope, and checkout evidence. Missing or conflicting evidence remains unresolved.
- Delegate independent specialist passes only when they add useful evidence. Quality gates and alignment may run in parallel; reconciliation follows their findings; compounding follows observed friction. Include `references/closure-report-contract.md` in each prompt. Otherwise complete closure directly and reuse current evidence.

## Reconcile and decide

- Group decisions and outcomes into mechanical updates, decisions required, blockers, and follow-ups. Present only closure-blocking decisions, one at a time, with a recommendation and affected artifacts. Specialists provide evidence; the orchestrator owns grouping and decision authority.
- Update actual delivery, coverage, deviations, and closure checklist from verified outcomes. For workspaces, retain lifecycle evidence in the explicitly linked Markdown log and keep entry status and approval consistent with it. Follow the authoring and timestamp rules in `effort-setup.md`.
- A material plan change requires a recorded deviation and any required approval for the changed revision. A populated field or log section cannot supply human approval.

## Preserve release evidence

- Record non-placeholder Actual Delivered, Deviations, and Closure Checklist evidence with revisions and applicable verification. Closure names the PR or merge commit carrying the work under local release policy.
- If `docs/engineering/release.md` says release tooling collects effort evidence, commit that evidence in the form it names; uncommitted edits are not release evidence.

## Close or hand off

- Set `status: complete` only when required gates are satisfied or explicitly carried forward under repository policy. Completion does not authorize publication.
- Otherwise record current revision, verified outcomes, next unfinished item, blockers or decisions, and the next useful action.
- Complete and archived efforts remain historical records, including existing Markdown efforts. New execution belongs in a fresh effort; do not rewrite history to adopt the workspace format.
