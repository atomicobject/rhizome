# Specification

Deliverable: one coherent spec with observable commitments, non-goals, open questions, and durable links to governing context. Given a spec path, revise it; given rough input or none, draft a strawman with `[TODO: ...]` markers for the user to react to, then rewrite it as one cohesive contract rather than appending reactions. Ask only the questions whose answers would change the draft. Keep one spec per coherent concern and split only when context shows separate owners, release paths, or validation strategies. Use `references/spec-template.md` for section shape. Route raw provenance-bearing material to `ingest-transcript` first.

Ground the contract in existing code, decisions, and user intent. When prior typed context could change the contract, run one bounded `query-recipe run --id topic-typed-survey`; deepen retrieval only for missing or conflicting evidence. Distinguish commitments from assumptions and open questions. When the spec names documentation or review obligations, consult `docs/engineering/documentation.md` and `docs/engineering/review-and-approval.md`.

<!-- rzm:skill-slot id="context.after-discovery" mode="extension" -->
<!-- /rzm:skill-slot -->

<!-- rzm:skill-slot id="context.constraint-extraction" mode="extension" -->
<!-- /rzm:skill-slot -->

Before revising a spec held frozen by a `planned` or `active` effort, load `query-recipe run --id frozen-spec-index-pack` to read it; its effort list is capped and is not the impact list. After the edit, update the spec's `last-updated` and run `rzm agent validate frozen-scope-drift --max-issues 1000`, raising the limit until `issueCount` matches the issues returned. Its findings and notes cover the planned or active efforts, Markdown or HTML, that froze the spec, except efforts whose Deviations already acknowledge an earlier edit to it. Find those by searching `docs/efforts` for `frozen-scope-drift acknowledged` together with the spec's id, and decide whether each acknowledgement covers this edit. Record a deviation on each of those efforts or explicitly refreeze. The findings are a closure gate.

Decision boundaries: the human owns material product and scope choices; use Partnership for those and settle everything else. Continue to effort setup when the contract is stable; stop only while behavior that changes the artifact remains unsettled.
