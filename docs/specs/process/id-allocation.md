---
aliases:
    - SPEC-0006
id: SPEC-0006
last-updated: 2026-08-11T00:00:00Z
spec-status: archived
summary: Defines how schema-declared sequential and filename-derived datetime identifiers, authored story ids, duplicate recovery, and explicit strategy migration are used in authoring workflows.
type: ProcessSpec
---
<!-- RZM AGENTIC-ENGINEERING RETIREMENT NOTICE -->
This is a historical record from the retired spec-driven starter, not current guidance. Current team policy lives in `docs/engineering/`. Reconcile any local policy still unique to this document there, then delete it.

# ID allocation

## Summary

This repo uses one shared `id` field for durable identifiers. Top-level entities use their schema-declared strategy: `SEQUENTIAL` produces ids such as `SPEC-0007`, while `DATETIME` copies a prospective filename's leading local minute stamp into an id such as `EFF-2026-08-05-14-32`. The Agentic Engineering effort workflow intentionally changes fresh starter filenames from their previously shipped UTC stamp to local wall time, while `created-at` remains UTC. Embedded user stories author stable ids from the parent spec instead of claiming a separate vault-wide sequence. Acceptance criteria do not have an `id` field; when a criterion needs a durable external locator, Rhizome mints a plain standalone `^block-id` on demand.

The allocation strategy and its format are ontology identifier metadata, so the allocator, authoring guide, validation, and repair workflow read one source of truth. `SEQUENTIAL` remains the default for numeric-prefix fields when `strategy` is omitted; prefixless identifiers remain author-supplied and `derivedSuffix` identifiers remain derived. Existing schemas do not need a strategy backfill. Agents allocate new ids through `rzm agent next-id --type <TypeName>` instead of re-implementing strategy logic per skill.

Parallel branches can claim the same identifier under either strategy. Minute-resolution datetime ids reduce the collision boundary but do not reserve values across branches; same local wall minutes, including repeated DST minutes, can still collide. The merged-head identifiers check is therefore mandatory, and duplicate recovery runs through `rzm validate fix identifiers` so the keeper, replacements, filename changes, and reference rewrites are planned and reviewed together. The technical strategy and reconciliation contract is [[../technical/identifier-strategy-and-reconciliation-contract|SPEC-0075]].

Embedded nodes with an identifier field use that field in block-safe form: for section-backed embedded nodes, the `- id::` metadata bullet is written with a leading caret before the semantic id. Rhizome strips the leading caret from typed values and treats the same token as the block target. Embedded nodes without an identifier field use standalone block locators only when cited; do not invent an `id::` property for them.

## Goals

- keep ids readable in markdown, diffs, and chat
- make top-level ids easy to allocate deterministically
- let filename-stamped note families avoid a shared repository-wide numeric counter
- keep user-story ids obviously attached to their parent spec

## Non-Goals

- replacing filenames, titles, or slugs; datetime allocation reads the filename stamp but does not rename it
- changing identifier strategies implicitly during `rzm init`; an adopted project changes strategy by editing its ontology and running the governed validation migration
- hiding gaps in the numeric sequence
- giving embedded user stories their own global registry

## Requirements

### Must

- Every top-level typed note family that uses durable ids authors them in a shared `id` field.
- Sequential top-level ids use an uppercase prefix plus a zero-padded monotonic number:
  - specs: `SPEC-0001`
  - add other prefixes only when the note family truly needs them
- EffortNote uses `DATETIME`: `EFF-YYYY-MM-DD-HH-MM[-N]`, derived from the prospective filename's local minute stamp.
- The id format must be encoded on the `@identifier` directive when the note family supports automated allocation:
  - `id: String! @field(source: "id") @identifier(preferred: true, strategy: SEQUENTIAL, prefix: "SPEC")`
  - `id: String! @field(source: "id") @identifier(preferred: true, strategy: DATETIME, prefix: "EFF")`
  - the directive's optional `pad` (default `4`) and `separator` (default `-`) tune the rendered shape; reach for them only when a family genuinely needs a different layout.
- A filename-derived datetime family declares `strategy: DATETIME` and a prefix. Its base id is `<prefix><separator>YYYY-MM-DD-HH-MM`, copied from the prospective filename's leading local stamp. An optional literal-hyphen `-N` with canonical decimal `N >= 2` is a collision disambiguator; `pad`, `-1`, leading-zero suffixes, and nested suffixes are invalid for DATETIME.
- Types sharing one rendered `<prefix><separator>` namespace must declare one compatible strategy and format. Ontology compilation rejects sibling declarations that could allocate overlapping values through different strategies.
- Omitted strategy means `SEQUENTIAL` only for an allocatable numeric-prefix field; prefixless/manual and `derivedSuffix` identifiers do not enter the allocator. Do not backfill existing schemas only to spell out the default.
- Unknown strategies are invalid ontology, not an invitation to guess or fall back.
- Agents allocating a sequential top-level id must call `rzm agent next-id --type <TypeName>` and use the returned `next` value verbatim.
- Agents allocating a datetime top-level id must first choose the prospective vault-relative filename, then call `rzm agent next-id --type <TypeName> --path <path>` and use the corresponding returned id verbatim. The allocator validates and copies the filename stamp; it does not use the clock or perform timezone conversion.
- The bundled Agentic Engineering effort workflow must replace its prior UTC filename command with a local stamp, choose `docs/efforts/<local-YYYY-MM-DD-HH-MM>-<slug>.md` before allocation, pass that path to `next-id`, and keep `created-at` and execution-event timestamps in UTC.
- Agents creating multiple sequential same-type notes before re-indexing must allocate the batch in one call with `rzm agent next-id --type <TypeName> --count <N>` and use the returned `ids` in order. Do not call single-id allocation repeatedly against a stale index.
- `--count` is a sequential batch convenience: it returns contiguous values from one observed maximum and does not reserve them across branches. DATETIME keeps the wire-compatible default `count: 1` but rejects `count > 1`; datetime batches use repeatable `--path` values and return one id per input path, never synthesized later minutes.
- The allocated id must be mirrored into `aliases:` when the type marks `id` as `@identifier(preferred: true)`. The `rzm agent validate identifiers` check enforces this and is a release-blocking gate after any allocation.
- Gaps are allowed. Never reuse an old number, even if the note was deleted or archived.
- When a datetime base is already reserved locally, allocate the lowest available suffix starting at `-2`; reserve preferred values and aliases across the whole shared pool.
- A repository that adopted the legacy `spec-driven` starter retains its current EffortNote strategy during Agentic Engineering starter refresh. Init does not mix legacy sequential values into a datetime pool or migrate them. To change an adopted pool, edit only the ontology strategy first, then use the identifiers validation migration workflow below.
- Changing an allocatable pool between `SEQUENTIAL` and `DATETIME` is schema-first and pool-complete. An empty pool needs no content migration; for a nonempty pool, `rzm validate identifiers` must detect coherent old-strategy values and `rzm validate fix identifiers` must expose one confirmation-required migration before any mutation. If a prior apply or branch merge leaves canonical target-strategy members beside coherent old-strategy members, the plan migrates every remaining old member and treats existing target values as reservations.
- Sequential-to-datetime migration uses each note's canonical leading filename minute. Same-minute notes keep deterministic order by their prior sequential ordinal and receive the unsuffixed base, then `-2`, `-3`, and later suffixes. It does not consult Git in the normal path or invent a timestamp from filesystem metadata.
- Datetime-to-sequential migration sorts canonical datetime values by minute and suffix, then assigns a fresh dense sequence beginning at one with the target schema's prefix, separator, and padding. This mapping is deterministic but does not restore an earlier sequential numbering.
- Strategy migration must fail closed when a source identifier is noncanonical, more than one incompatible source format remains, a sequential note lacks a canonical leading filename minute, a target collides with a retained target value or another reservation, or the requested change also alters the prefix, separator, preferred field, or identifier family.
- A confirmed migration must use the existing identifier repair transaction to update preferred ids, required alias mirrors, derived descendants and locators, governed filename occurrences, and structurally resolved links/references together. Plain-prose and source-code mentions remain review items rather than guessed rewrites.
- A pool containing `complete` or `archived` efforts remains lifecycle-protected. Preview the migration normally, then provide `--allow-historical` only with explicit authority when applying the reviewed plan.
- Default/all/CI identifiers validation may detect strategy drift from the prepared typed inventory, one batched raw-property projection, and selector assessments limited to unresolved note roots. Expensive reference discovery, source hashing, Git/history work, and transaction construction must remain gated behind an actual coherent migration plan.
- Merged-head validation must reject duplicate preferred identifiers and aliases before delivery.
- Duplicate recovery must use `rzm validate fix identifiers` to preview or `rzm validate fix identifiers --apply` to execute the deterministic plan; do not hand-renumber a claimant without updating governed filenames and references.
- Embedded user stories author ids from the parent spec id plus an ordinal suffix:
  - semantic value: `SPEC-0007-US1`
  - authored linkable field: `- id::` with a leading caret before `SPEC-0007-US1`
- User-story ids must stay stable once assigned. Reordering the stories in the document does not trigger renumbering.
- Acceptance criteria do not author `id::` lines.
- When an acceptance criterion is externally cited or selected into effort scope, run `rzm agent node-link --target <spec#criterion-fragment> --ensure plan` and use the planned durable locator. The repair should mint a plain standalone block id such as `^SPEC-0007-US1-AC1`.
- Acceptance-criterion block locators are opt-in linkability, not semantic identity. Do not pre-author them on uncited criteria, and do not treat cleanup of an unreferenced AC block locator as semantic deletion.

### Should

- Prefer the schema-encoded directive over a free-form id; encoding the prefix lets `rzm agent next-id`, the authoring guide, and validation all read the same contract.
- When two or more typed note families share a number-line (the spec variants today share `SPEC-XXXX`), declare the same `prefix`/`pad`/`separator` on each. The allocator pools all sibling types before picking `max + 1`, so off-pattern values stay safe.
- Treat post-write validation as the closing gate: confirm the new note triggers neither `duplicate_preferred_identifier` nor `identifier_not_in_aliases` before handoff.

## Allocation workflow

1. Resolve the type being authored and inspect its identifier strategy.
2. For `SEQUENTIAL`, run `rzm agent next-id --type <TypeName>` for one note, or add `--count <N>` when creating several same-type notes in one editing pass. The command returns `{ next, ids, count, currentMax, prefix, sharedWith, ... }` and lists sibling types sharing the number-line.
3. For `DATETIME`, choose each prospective filename first and pass it with a repeatable `--path`. The command returns one id per path in input order and reports the timestamp base and any local disambiguator; a malformed or missing stamp is an error.
4. Write the returned `next` value (or the corresponding value from `ids`) into the note's `id:` frontmatter and mirror it into `aliases:`.
5. Run `rzm agent validate identifiers` and confirm the new note has no `duplicate_preferred_identifier`, `identifier_not_in_aliases`, or identifier-format issue before handoff. A later note move does not rederive or rekey the stable id from the new filename.

After merging another branch, run the identifiers check again against the synthesized head. A collision uses `rzm validate fix identifiers`, followed by review/apply/replan; do not hand-renumber one without the governed filename/reference plan.

When the command returns `unsupported_for_type`, the schema does not yet declare a `prefix:` on `@identifier` — fall back to the legacy recipe (query the typed family, extract existing ids, increment the highest matching suffix), then use the `rhizome` ontology-authoring guidance to surface the schema gap so the directive can be tightened.
## Strategy migration workflow

1. Inspect the current `@identifier` declaration and the complete existing pool. Confirm the requested change is only `SEQUENTIAL` to `DATETIME` or `DATETIME` to `SEQUENTIAL`; prefix, separator, preferred-field, and namespace changes need a separate migration design.
2. Edit the ontology strategy first. Do not hand-edit identifiers, aliases, filenames, or references before validation has constructed the complete mapping.
3. Run `rzm validate identifiers`. An empty pool should validate without a migration. A coherent nonempty old-strategy pool should report one migration. A partial pool with canonical target values should report only the remaining old-strategy members and reserve the retained values. Malformed, ambiguous, conflicting, or non-materializable pools should stop as agent-required findings.
4. Run `rzm validate fix identifiers` without `--apply`. Review the complete old-to-new mapping and every governed metadata, descendant, filename, and structured-reference edit. Strategy migration is confirmation-required, never an automatic safe fix.
5. Apply the reviewed plan with `rzm validate fix identifiers --apply`. If the plan includes lifecycle-protected complete or archived efforts, apply only with explicit historical-edit authority by adding `--allow-historical`.
6. Rerun `rzm validate identifiers` and the repository's normal validation gates. If content or schema drifted after preview, replan rather than forcing the stale mapping.

When the command returns `unsupported_for_type`, the schema does not yet declare a `prefix:` on `@identifier` — fall back to the legacy recipe (query the typed family, extract existing ids, increment the highest matching suffix) and surface the schema gap to `rhizome-ontology` so the directive can be tightened.
