# Retrieval and Evidence

Use the installed `rhizome` skill for session reuse, exact reads, live authoring/schema discovery, mutation, readiness interpretation, and proportionate validation. This reference owns the domain meaning of retrieved results.

## Choose the Smallest Pack

- Known domain note: `domain-context-pack`.
- Known requirement: `requirement-trace-pack`.
- Known spec: `spec-domain-context-pack`.
- Known changed source, requirement, process, workflow, feature area, context, or type: `changed-domain-impact-pack`.
- Topic-only work: run `domain-inventory-pack` for each relevant live type before optionally using `domain-topic-survey` to rank semantic candidates.
- Maintenance triage: use `feature-area-backlog-pack`, `coverage-gap-pack`, or `sources-needing-review-pack`, then inspect the underlying notes before making a lifecycle or satisfaction claim.

For bounded topic inventory:

```bash
rzm agent query-recipe run \
  --session-id "$SESSION_ID" \
  --id domain-inventory-pack \
  --inputs-json '{"type":"Requirement","find":"<optional exact text>","first":50}'
```

Discover current ontology type names before guessing them. Omit `find` when a bounded typed inventory is useful. Use `domain-topic-survey` only after this structural pass; its scores rank candidates and do not establish authority, coverage, or satisfaction.

## Qualify Results

Distinguish these outcomes in the result and handoff:

- An unresolved anchor proves only that the requested note did not resolve.
- A resolved note with no authored links supports a missing-authored-coverage finding for that note.
- A degraded or failed capability leaves an evidence gap; continue independent source reading or bounded work that does not depend on it.
- `pageInfo.truncated`, warnings, or a reached inventory cap make completeness unknown. Narrow with a supported typed/property selector or an explicit source, process, workflow, feature area, requirement, or spec. Filtering only returned rows does not establish a complete inventory.
- A bounded `linked` or `backlinked` list reaching its explicit limit may be truncated. Confirm named targets with focused packs.
- Stale evidence must be identified from source/version evidence, not inferred from retrieval rank or from code disagreement.

Keep authored links, readable target resolution, delivery evidence, obligation satisfaction, and lifecycle authorization separate. Resolve story and acceptance-criterion locators and inspect relevant evidence; a string locator, backlink, or semantic match proves none of those by itself.

Reuse still-applicable context across phases. Refresh only when the anchor, selected scope, source revision, delivery evidence, or capability state changed.

For several related anchors, run the packs in one script through the `rhizome` skill's code-mode route, for example `rzm.queryRecipe({ op: "run", id: "domain-context-pack", anchor: ["<note path>"] })` per anchor, and return each anchor's rows with its own failures and pagination warnings. Filtering returned rows does not repair incomplete coverage. The domain workflow continues to own lifecycle and satisfaction judgments.
