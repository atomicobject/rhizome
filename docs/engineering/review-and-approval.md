# Review and approval

Consulted when classifying risk, finding an approver, and deciding whether a pause is wanted. Used during specification, planning, and closure.

## Risk classes

Effort-driven work is work that changes behavior across a boundary, introduces a durable contract, carries material uncertainty, needs several phases, or has meaningful rollback or operational risk. It gets a spec, a frozen effort, an approved plan, then implementation.

A clear local task is everything else. It combines scope, change, and focused verification in one pass without an effort.

## Approval

An effort-driven plan needs explicit approval in chat before implementation, recorded on the effort when a current user is configured. An approved plan authorizes all covered steps: resolve routine implementation choices and continue, and ask only for a material decision outside that authority or an unauthorized destructive action.

## Pauses

A pause for foundation review is requested by the plan when an early phase fixes APIs, data shape, or ownership that later phases depend on. It is not a standing rule after every phase.

## Escalation cues

Raise a local task to an effort when it reveals a cross-subsystem contract or data migration, would constrain future APIs or deployment, needs evidence that does not fit one focused change, or a review or incident changes the intended outcome.

## Team extensions

Drew Colthorp approves effort-driven plans; record approval in `plan-approved-by` (the configured current user is a note-backed `Person`). Use an effort for changes to a subsystem invariant, ontology schema semantics, persisted data or SQLite schema, generated init behavior, or what a release publishes and where. Mechanical edits that preserve those contracts, such as a typo in a template or hardening a release script without changing what it publishes, stay local tasks. Use `foundation-review` when a plan's Phase 1 fixes a public API, schema, or ownership boundary that later phases depend on. Conventional Commits. Stage only what the conversation changed, and never revert, clean, or stash unrelated diffs or untracked files; assume they are in-progress work.
