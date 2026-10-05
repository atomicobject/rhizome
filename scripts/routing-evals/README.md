# Routing evals

Self-reported routing evaluation of the installed agent instruction surface (managed blocks, skills, `docs/engineering/`) on Claude Fable 5.1 and GPT-6 Astra. Each scenario asks the model, read-only, which route it would take, which documents it would consult, whether it would stop before working, whether it would write tests, and whether it would escalate to an effort. Results are self-reports, not observed execution; see `docs/reference/analysis/agentic-engineering-routing-evals-2026-09-04.md` for the first run and its limitations.

Requirements: `scripts/claude-fable` (Claude Code login) and the `codex` CLI (logged in, default model `gpt-6-astra`).

```bash
# from the repository root, after `make build` and `rzm init`
RZM_EVAL_OUT=/tmp/routing-evals python3 scripts/routing-evals/runner.py
RZM_EVAL_OUT=/tmp/routing-evals python3 scripts/routing-evals/tabulate.py > /tmp/routing-evals/table.md
```

Edit `scenarios.json` to add cases; each has an `expect` block the tabulator marks with `(!)` on mismatch. Delete a result file to rerun one scenario. Run the suite before and after any change to a skill, a managed block, or a concern doc, and record prune decisions in an analysis note.
