---
type: ReferenceDoc
summary: "Specification and effort query recipes used by Agentic Engineering phases."
reference-kind: guide
---

# Spec-driven query bundle

The bundled recipes provide bounded typed context for specification and effort decisions. Use the smallest recipe that can change the decision, reuse current results across phases, and refresh data whose source note or execution evidence changed.

## GraphQL recipes

Use saved ontology query recipes when the needed context is typed-note data:

```bash
rzm agent query-recipe list
rzm agent query-recipe validate
rzm agent query-recipe run --id effort-execution-context --anchor <effort-path>
rzm agent query-recipe run --id frozen-spec-index-pack --anchor <spec-path>
rzm agent query-recipe run --id frozen-spec-detail-pack --anchor <spec-path>
rzm agent query-recipe run --id story-acceptance-pack --anchor <spec-path>
rzm agent query-recipe run --id closure-drift-pack --anchor <effort-path>
rzm agent query-recipe run --id runtime-code-evidence-pack --anchor <code-path>
rzm agent query-recipe run --id runtime-note-code-evidence-pack --anchor <note-path>
rzm agent query-recipe run --id topic-typed-survey --inputs-json '{"type":"SpecLike","topics":["<topic>"]}'
```

- `effort-execution-context`: current effort status, approval, frozen specs, frozen stories, plan, execution notes, delivery truth, deviations, and closure checklist.
- `frozen-spec-index-pack`: one frozen spec's compact ids, summaries, statuses, references, decisions, acceptance criteria content, and inbound efforts.
- `frozen-spec-detail-pack`: one frozen spec's full section content, requirements, stories, acceptance criteria, references, decisions, and inbound efforts. Run this after the index pack shows deeper reading is needed.
- `story-acceptance-pack`: story readiness, exact story ids, acceptance criteria content, compact locator wikilinks/block ids, references, decisions, and prior efforts.
- `closure-drift-pack`: effort status, approval, frozen intent, plan, execution notes, actual delivery, deviations, closure checklist, and follow-ups.
- `runtime-code-evidence-pack`: code-adjacent docs, linked notes, and tests for one code path.
- `runtime-note-code-evidence-pack`: code paths bound to one note.
- `topic-typed-survey`: ranked typed prior art for one or more topics.

## Recipe provenance

These recipes live in `.rhizome/query-recipes/` and may depend on capabilities exposed by the installed `rzm`. Treat a missing recipe or runtime field as unavailable, not as an empty result. Follow the core `rhizome` guidance to inspect the active surface and the repository's authorized init or upgrade path; do not assume prompt acceptance authorizes replacement of team-edited assets.

## Related retrieval and commands

The core `rhizome` skill owns semantic search, file context, schema discovery, identifier allocation, and validation mechanics. Use those capabilities when the current decision needs them rather than running a fixed itinerary for every phase.

When existing typed prior art could change a new spec, survey it before widening to mixed chunk search:

```bash
rzm agent query-recipe run \
  --id topic-typed-survey \
  --inputs-json '{"type":"SpecLike","topics":["<feature-topic>"]}'
```

For substantive implementation tied to a spec, story, or acceptance criterion, prefer exact known-path evidence. Semantic discovery and file context can fill a real gap when ownership, tests, or documentation bindings remain unclear:

```bash
rzm agent semantic-query \
  --mode overview \
  --query "<spec-or-story-id> implementation code tests docs"

rzm agent file-context \
  --file <affected-file-or-directory> \
  --submodule-depth 1
```

When code, tests, comments, coderefs, or code anchors cite requirement context, use the smallest durable node: acceptance criterion for one observable criterion, user story for story-level behavior, parent spec for cross-story behavior. Use `story-acceptance-pack` when you need story/criterion link targets; prefer its `locator.wikilink` and `locator.linkTarget.blockId` only when `requiresFix` is false. A planned locator is not durable. If a locator reports `requiresFix`, inspect the repair with `rzm agent node-link --target <spec-path#fragment> --ensure plan`; when source mutation is authorized, apply it through a write-capable Rhizome surface, rerun the recipe or helper, and cite only the returned link after `requiresFix` is false. Otherwise cite the nearest durable parent and record the precise target as follow-up.

Before creating a typed note id, use the live identifier strategy:

```bash
# SEQUENTIAL
rzm agent next-id --type <TypeName>

# DATETIME: choose the prospective local-stamped path first
rzm agent next-id --type EffortNote --path docs/efforts/YYYY-MM-DD-HH-MM-<slug>.md
```

When creating multiple same-type notes before re-indexing, use one frozen allocation request: `--count <N>` for sequential types, or repeat `--path` for datetime types. Use returned allocations in order and mirror every preferred id into `aliases:`. A datetime path supplies local wall-time text only; UTC `created-at` and event timestamps are authored separately.

During spec edits, audit, backport, or closure for active efforts, run `frozen-scope-drift-check`:

```bash
rzm agent validate frozen-scope-drift --max-issues 40
```

The check covers Markdown efforts, which freeze specs under `Spec Set (Frozen)`, and HTML effort workspaces, which freeze them in `governing-specs` and keep Deviations in the linked work log. It compares each spec's `last-updated` with the effort's `created-at`, so a finding means the spec may have changed since the freeze; read the spec's history to confirm what changed. A date-only `last-updated` on the effort's creation day appears as a note instead of a finding, because the date cannot show whether the edit came after the freeze. A pair acknowledged in Deviations stays skipped after later edits, because the marker has no date. The default limit is 20 issues per check; raise `--max-issues` until `issueCount` matches the issues returned.

## Phase map

- Specification: use `topic-typed-survey` when prior typed context could affect the contract; use the live identifier strategy for a new spec and `frozen-spec-index-pack` plus frozen-scope validation before changing frozen intent.
- Effort setup: use the base skill's live identifier guidance, then `story-acceptance-pack` when selecting story or criterion scope.
- Planning: `effort-execution-context`, then `frozen-spec-index-pack` for each frozen spec; use `frozen-spec-detail-pack` only where full contract text is needed.
- Implementation: use `effort-execution-context` for effort work, then one runtime evidence pack when code or note bindings could expose a relevant constraint. A clear local task without an effort does not load effort-closure recipes.
- Alignment: load `closure-drift-pack` and `frozen-spec-index-pack` when current equivalent results are unavailable, run frozen-scope validation, and use runtime code evidence when changed paths leave a contract question.
- Reconciliation: load `closure-drift-pack` when current equivalent context is unavailable; use `frozen-spec-detail-pack` only before editing full requirements or stories.
- Compounding and closure: reuse a current `closure-drift-pack`, refreshing it when its source changed. Skip compounding when no repeated friction exists.

Recipe output is evidence, not authority by itself. Reconcile approval fields with the actual authorization and lifecycle context; a blank approval can truthfully mean pending authority.
