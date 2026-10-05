# Quality Gates

Run the gates in `docs/engineering/quality-gates.md` for the changed surface, using its safe-to-run list without asking; report exact commands, results, and evidence gaps. Consult `docs/engineering/release.md` when a gate is a release condition. A bracketed placeholder there is a setup gap, never a command to invent; another repository's command never substitutes for this one's contract.

When the gate table still has bracketed placeholders, settle them before delivery that depends on them. Find the commands the repository already runs in its build files, package scripts, and CI workflows, propose one per placeholder with where you found it, and record in `docs/engineering/quality-gates.md` the ones the user confirms. Leave the rest as named setup gaps.

Reuse revision-bound results while the covered files and prerequisites remain unchanged. Repeat an affected check when later edits or missing coverage invalidate its evidence, not merely because the workflow phase or agent changed.

Use validation `fixPlan` and `nextActions`: apply safe in-scope fixes, and classify everything else as `needs decision`, `historical/frozen`, `unrelated/pre-existing`, or `out of scope`.

Failures are fixed, explicitly deferred with accountable approval, or blockers. A passing build alone is not alignment or closure.
