# Reports and health

Reports prioritize investigation; they are not proof by themselves. Use `rzm agent surface` for the current agent command flags/examples and `capabilities.reports.ops` for the advertised report operations. Inspect returned warnings and evidence instead of assuming a stable per-operation input or output contract. Do not preserve a static operation inventory in a skill.

Choose a report when the question is an audit, coverage, hotspot, complexity, similarity, or other advertised analysis. Use `vault-health` for graph/note hygiene and high-level health questions. Use focused validation when the question is contract correctness rather than prioritization.

Before running a report:

1. Confirm the project launcher, index freshness, and required capability (for example semantic embeddings).
2. Scope the report to the user's question when the live surface supports it.
3. Reuse the conversation session.

Afterward, inspect warnings, skipped/degraded lanes, coverage, and evidence paths. Verify important findings with `file-context`, exact code tools, typed queries, or focused validation before recommending a change. If a capability is unavailable, say which portion was omitted instead of presenting a partial report as complete.

For a refactor question, similarity, complexity, and hotspot signals narrow where to look; they do not decide the shape. Compare responsibilities and dependency direction in the source before extracting a shared helper, since similar text can hide different contracts and different text can express one ownership problem. Workflow sequencing and decision approval stay with the active workflow skill.
