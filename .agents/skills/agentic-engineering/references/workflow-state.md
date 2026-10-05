# Workflow State

Classify before producing an artifact, using the risk classes in `docs/engineering/review-and-approval.md`: a clear local task combines scope, change, and focused verification in one pass; effort-driven work takes specification, a frozen effort, an approved plan, then implementation. `docs/engineering/README.md` names the concern documents and their precedence; read it when the route is unclear or a phase reference points at a policy you have not loaded.

Routing rules that override intuition:

- Never reopen or rewrite a `complete` or `archived` effort. Effort-driven work against a slice it delivered gets a fresh effort; a clear local fix needs no effort at all.
- A spec edited while a `planned` or `active` effort holds it frozen requires a deviation on each affected effort or an explicit refreeze; route through specification, then alignment.
- When plan approval is required but absent, stop before implementation and name the missing decision. Explicit approval in the conversation counts even when `plan-approved-by` is blank; record it when a current user is configured instead of asking again.
- Before effort-driven delivery, check `docs/engineering/quality-gates.md` for bracketed placeholder commands and settle them through `references/quality-gates.md`.
- Raise a local task to an effort when it reveals a cross-subsystem contract or data migration, would constrain future APIs or deployment, needs evidence that does not fit one focused change, or a review or incident changes the intended outcome.

For a known effort, resume from `query-recipe run --id effort-execution-context`. Reconcile its status, approval, frozen scope, plan, execution notes, actual delivery, deviations, and closure checklist with the current checkout and revision. Identify the next unfinished item and reuse still-current evidence. Refresh only context affected by later source or execution changes. A fresh session does not invalidate approval or require a new plan; missing or conflicting authority holds only the work that depends on it.

Use the existing spec or effort when one owns the work; do not manufacture artifacts to make the workflow look complete. When state is unclear, read the durable artifact and ask the smallest question that changes the route while continuing independent authorized work.
