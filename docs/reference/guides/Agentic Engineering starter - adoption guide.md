---
type: ReferenceDoc
summary: "When to adopt the Agentic Engineering starter, what it scaffolds, and how legacy installations migrate."
reference-kind: guide
last-verified: 2026-07-19
status: active
---

# Agentic Engineering starter adoption guide

## Summary

Use the Agentic Engineering starter when a repository wants recoverable intent, bounded delivery work, durable rationale, and Rhizome-enabled engineering guidance. It is a repository harness, not a complete substitute for a team's process.

## What `rzm init` scaffolds

- specification-delivery ontology and query assets, including domain-accurate `spec-driven` filenames
- folder hubs for specs, efforts, and durable reference material
- six team-owned extension docs under `docs/engineering/`
- the `agentic-engineering` router (phase as argument), two workflow skills (`foundation-review`, `ingest-transcript`), four effort gate skills (`effort-research`, `effort-discuss`, `plan-check`, `phase-code-review`), and a compact managed agent-doc block

The extension docs are created once and owned by the repository. Teams tune their workflow, effort expectations, commands, test policy, and documentation policy there; ontology and note-shape mechanics remain managed.

## Effort gates

The four gate skills are installed for every team and do nothing until `docs/engineering/review-and-approval.md` lists them. A new installation lists all four. An upgraded repository keeps its own policy document, so its efforts run as before until the team adds the table. Paste the block below into that document, or run `rzm init --refresh-docs`, which offers Rhizome's latest version of each starter document and replaces one you have not edited without asking:

```markdown
## Gates

An effort passes each gate listed here at the point named. A gate is part of the approved work: it records its outcome in the effort's execution notes and is not repeated for the same scope, plan revision, or reviewed changes. Remove a row to turn that gate off.

| When | Gate | Decided by |
| --- | --- | --- |
| After scope freezes or re-freezes, before planning | `effort-research` | The user chooses Run or Skip |
| After the research decision, before planning | `effort-discuss` | The user chooses Discuss or Skip; silence is not Skip |
| After a plan is drafted or structurally revised, before asking for approval | `plan-check` | Always runs |
| After each implementation phase, and once before closure for a single-phase plan | `phase-code-review` | Always runs |
```

Remove a row to leave that gate off. What to expect once they are on:

- Research and discussion each ask the user one question before planning. Skipping is one answer.
- `plan-check` and `phase-code-review` each start a fresh reviewer, up to three times per run, and phase review re-runs the phase's quality gates after its fixes. Budget for that time and model cost on every effort.
- An effort that was already under way is not sent back through the gates. The skills treat an approved plan as the sign that research and discussion are past, and phase review asks once whether to start from the phase just finished.
- Phase review commits its fixes only where your agent instructions or `docs/engineering/` allow agent commits. Otherwise it leaves them uncommitted and says so.

If your team already has its own skill named like one of the four, `rzm init` asks about it. In a terminal the default answer replaces your file, so answer no, or run `rzm init --keep <path>` for each file. Init remembers that choice until Rhizome ships a new version of the skill, then asks again; answer no or pass `--keep` again. Keep the same file under both `.agents/skills` and `.claude/skills`, because a first run can otherwise leave Rhizome's version in one of them. The `Installed Skills` list in `AGENTS.md` names the skill either way.

## Legacy migration

`spec-driven` is accepted only as a migration input. Rerun `rzm init` to move starter state to `agentic-engineering`. It removes only legacy process documents that match the checked-in historical catalog. Modified or unproven documents remain at their old path with a retirement notice and a tracked reconciliation record at `.rhizome/migrations/agentic-engineering/README.md`.

No migration silently merges local policy. Reconcile retained material into `docs/engineering/`, then delete the migration record when the team is satisfied.

For starter selection across all options, see [Choosing Your Starter](choosing-your-starter.md).
