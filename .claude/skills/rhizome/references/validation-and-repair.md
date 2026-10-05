# Validation and repair

Validation is read-only unless an apply action is authorized. Reuse existing authorization for covered repairs; do not ask again for routine steps. Discovery or a suggested repair command does not itself authorize applying it.

For schema-backed content, the current schema is the default contract: repair notes first. Change a schema only when the user asked for schema work or concrete evidence shows the contract is wrong or obsolete; never weaken required fields, selectors, sections, or field names merely to silence validation.

## Select the relevant check

Use `rzm validate list` or `rzm agent surface` when the current selector contract is needed. Run a configured suite only when the task calls for that breadth; otherwise select the check tied to the changed content. Scope fragile-link checks to a source note, target, or node ref when possible. Read-only retrieval needs no blanket gate. Typed edits check the relevant type and identifiers; link changes check links; binding changes verify focused retrieval. Repository-required gates still apply.

The agent surface returns structured selectors, effective checks, outcomes, issues, and next actions. Preserve the selector, vault, and scope when following repair guidance.

## Repair loop

1. Run the relevant selector and scope.
2. Inspect issues and structured next actions.
3. Preview deterministic repairs with `rzm agent validate fix <selector>` or the exact advertised command.
4. Apply only safe fixes within the user's authority; ask on ambiguous, destructive, or judgment-bearing changes.
   Plain `--apply` applies safe actions only. After the user approves specific `needs_confirmation` actions, apply exactly those with `--apply --action <action-id-or-issue-key>` (repeatable) or `--apply --from-plan <file>` (JSON array or one ID per line). An exact selection skips every unlisted action, rejects IDs missing from the current plan, and cannot apply `agent_required` work.
5. Rerun the same selector and scope.

`broken-links` counts links that broke. When git history shows a target was never created, the link is a placeholder: `broken-links` notes the count and `rzm agent validate placeholder-links` lists them. Treat placeholders as the author's intent, not as repairs to make. A retarget reaches `needs_confirmation` only on strong evidence (the same title up to case and punctuation, or a git rename that orphaned an existing link), is marked `confidence: high`, and keeps the authored text as the display alias; partial title matches stay `agent_required` with `confidence: low`.

Agent validation is non-interactive. Do not assume `all` is the correct handoff gate for every task, and do not turn pre-existing unrelated issues into silent scope expansion. Report unresolved relevant issues and their classification clearly.
