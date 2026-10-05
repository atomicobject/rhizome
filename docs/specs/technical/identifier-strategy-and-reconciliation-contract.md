---
type: TechnicalSpec
summary: "Defines schema-declared sequential and filename-derived datetime identifier strategies, duplicate-aware resolution, deterministic transactional reconciliation, and explicit whole-pool strategy migration."
id: SPEC-0075
spec-status: active
last-updated: 2026-08-11
aliases:
  - SPEC-0075
  - identifier-strategy-and-reconciliation-contract
---

# Identifier strategy and reconciliation contract

## Summary

Ontology identifier fields use a first-class allocation strategy contract that applies to every note family, not only specs and efforts. `SEQUENTIAL` allocates from a shared numeric maximum. `DATETIME` derives a local wall-time base from a prospective filename's leading `YYYY-MM-DD-HH-MM` stamp and uses deterministic numeric suffixes when that base is already claimed. For the spec-driven effort workflow, local filename stamps intentionally replace the starter's previously shipped UTC filename convention; canonical `created-at` and event timestamps remain UTC.

Neither strategy reserves values across branches. When parallel branches allocate the same identifier, Rhizome must produce a deterministic reconciliation plan. The plan chooses a keeper, allocates strategy-specific replacements from a frozen inventory, updates governed filenames and structurally resolved references, then lets the repair engine group collision operations into atomic transactions by their complete affected-file and identity graph.

## Goals

- make allocation strategy explicit and extensible without implicitly changing identifiers in existing projects
- preserve sequential `max + 1` behavior while adding filename-derived datetime allocation
- make new spec-driven installs use datetime effort ids such as `EFF-2026-08-05-14-32`
- make duplicate resolution honest and deterministic
- make an explicit schema strategy change detectable and transactionally migratable through the ordinary identifiers validation workflow
- produce the same reconciliation result from the same content and shared Git history
- keep reference rewriting bounded to structurally resolved identity-bearing fields and links

## Non-Goals

- implementing random, UUID, branch-derived, or collision-free distributed allocation
- changing existing project schemas without an authored ontology edit
- encoding a timezone or converting the filename's local wall-time stamp to UTC
- continuously rekeying an identifier after its creation source filename is renamed
- backfilling `strategy:` into existing ontology schemas
- rewriting arbitrary prose or source-code mentions of identifiers
- fetching missing Git history automatically
- reusing numeric gaps or retired identifiers
- exposing an arbitrary standalone user-selected rekey command; schema-triggered whole-pool migration is limited to the US6 validation workflow
- migrating prefixes, separators, preferred identifier fields, manual/prefixless identifiers, or derived-only identifier families
- silently inventing datetime source stamps when a sequential note lacks a canonical leading filename minute

## User Stories

### US1 - Declare identifier allocation strategy in ontology metadata
- id:: ^SPEC-0075-US1
- summary:: Declare a first-class allocation strategy per shared identifier family while preserving sequential behavior by default.
- status:: satisfied

#### Acceptance Criteria

- `@identifier` accepts `strategy: SEQUENTIAL`; for an allocatable field with a numeric prefix, omitting `strategy` resolves to legacy `SEQUENTIAL` without schema backfill. Prefixless fields remain author-supplied/unallocatable, and `derivedSuffix` identifiers remain derived rather than invoking an allocator. ^SPEC-0075-US1-AC1
- Unknown strategy values fail ontology compilation or validation rather than falling back silently. ^SPEC-0075-US1-AC2
- Prefix, separator, pad, strategy, and shared-pool semantics are exposed through one public strategy metadata model used by allocation and authoring guidance. ^SPEC-0075-US1-AC3
- Only the sequential allocator ships in this increment and continues to allocate the highest used numeric suffix plus one. ^SPEC-0075-US1-AC4

### US2 - Detect and reconcile duplicate preferred identifiers deterministically
- id:: ^SPEC-0075-US2
- summary:: Detect and reconcile duplicate preferred identifiers deterministically across branches and repositories with incomplete history.
- status:: satisfied

#### Acceptance Criteria

- The identifier check groups every duplicate preferred value by shared identifier pool and reports every claimant. ^SPEC-0075-US2-AC1
- A bounded rename-following history resolver traces the normalized current identifier-field claim to its earliest introducing commit; later formatting or rewrites of the same claim do not reset provenance. When every claimant has usable complete evidence, the keeper uses earliest introduction-commit author timestamp, then full commit OID, then canonical node key. ^SPEC-0075-US2-AC2
- If any claimant is untracked, missing, or has shallow, incomplete, or ambiguous introduction history, the whole collision uses canonical node-key lexical order, discloses the fallback, and never uses filesystem timestamps. ^SPEC-0075-US2-AC3
- The canonical node key is normalized vault-relative path, embedded fragment when present, ontology type, and identifier field. ^SPEC-0075-US2-AC4
- Replacement allocation freezes the full pool inventory, reserves preferred values and aliases, sorts by normalized pool key, collided value, then loser canonical key, and, for `SEQUENTIAL` pools, allocates from current maximum plus one while skipping every reserved or earlier planned value. ^SPEC-0075-US2-AC5
- Each collision seeds identifier, alias, governed filename, descendant, and inbound-reference repair operations; after reference discovery, the repair engine groups all operations connected by shared files or identities into transactions with ordinary stale/conflict/recovery/post-validation gates. ^SPEC-0075-US2-AC6

### US3 - Preserve identity semantics while rewriting governed references
- id:: ^SPEC-0075-US3
- summary:: Preserve identity semantics while rewriting governed references and refusing ambiguous first-wins behavior.
- status:: satisfied

#### Acceptance Criteria

- Duplicate preferred IDs or aliases resolve to structured ambiguity with all candidate canonical refs; no read surface silently picks the first claimant. ^SPEC-0075-US3-AC1
- A loser removes the collided old preferred value from its aliases and adds its replacement identifier as the required alias mirror. ^SPEC-0075-US3-AC2
- A basename is renamed automatically only when it contains the exact case-sensitive old identifier bounded on both sides by basename start/end or a non-letter/non-digit rune; parent directories and unrelated basename text are preserved, and an existing sibling destination that matches exactly or by filesystem-portable case-fold blocks the operation. ^SPEC-0075-US3-AC3
- Apply rewrites unambiguous preferred/alias fields, derived embedded identifiers and identifier-backed locators, typed relations, canonical node refs, wiki/Markdown targets, resolved display labels, and governed filename references. ^SPEC-0075-US3-AC4
- Bounded batched Git provenance may disambiguate an otherwise ambiguous structured bare-ID link; unresolved ambiguity blocks that collision transaction. ^SPEC-0075-US3-AC5
- Plain-text note mentions and source-code tokens are review candidates only and never enter automatic replacement. ^SPEC-0075-US3-AC6
- Rekeying a parent recursively updates every structurally derived descendant identifier, identifier-backed block target/locator, and unambiguous inbound reference to those descendants in the same connected repair transaction. ^SPEC-0075-US3-AC7

### US4 - Allocate filename-derived datetime identifiers
- id:: ^SPEC-0075-US4
- summary:: Allocate local filename-derived datetime identifiers through the shared strategy contract without migrating existing projects.
- status:: satisfied
- increment:: 2

#### Acceptance Criteria

- `@identifier` accepts `strategy: DATETIME` alongside `SEQUENTIAL`; allocation, parsing, validation, runtime metadata, authoring guidance, and reconciliation dispatch through one public strategy-owned contract. Every rendered `<prefix><separator>` namespace uses one compatible strategy/format across sibling types, and ontology compilation rejects mixed-strategy declarations in the same namespace. ^SPEC-0075-US4-AC1
- A datetime allocation derives its base from the prospective filename's leading local `YYYY-MM-DD-HH-MM` stamp and renders `<prefix><separator>YYYY-MM-DD-HH-MM`; it copies filename text without consulting the current clock, converting time zones, or changing UTC handling for `created-at`. This intentionally changes the spec-driven effort filename from the previously shipped UTC stamp to local wall time; DST-repeated and cross-zone same-wall-minute values are ordinary collision families. ^SPEC-0075-US4-AC2
- The datetime grammar accepts a valid calendar-minute base and an optional literal-hyphen disambiguator `-N`, where `N >= 2` is canonical decimal without leading zeroes; `pad` is sequential-only and an explicitly authored datetime `pad` is invalid. A missing, malformed, or calendar-invalid filename stamp, `-1`, leading-zero suffix, or nested suffix returns a structured invalid-input result rather than guessing. ^SPEC-0075-US4-AC3
- Allocation receives prospective vault-relative paths explicitly. `rzm agent next-id` accepts repeatable `--path` inputs for `DATETIME`; one identifier is returned per path in input order. Default `count: 1` remains wire-compatible, but `DATETIME` rejects `count > 1`; multiple paths, not count, determine datetime batch size. ^SPEC-0075-US4-AC4
- A datetime batch shares one frozen inventory: the first available claim uses the unsuffixed base and additional same-stamp paths use the lowest unreserved `-2`, `-3`, and later suffixes without inventing different minutes. ^SPEC-0075-US4-AC5
- Fresh spec-driven starter adoption explicitly declares `DATETIME` for `EffortNote`; omitted strategy continues to resolve to legacy `SEQUENTIAL`, and a repository that already adopted spec-driven retains its existing EffortNote strategy when other managed starter assets update. Init never performs a strategy migration; the explicit schema-first validate/fix workflow in US6 owns legacy-value conversion. This repository later adopted `DATETIME` through that explicit migration workflow. ^SPEC-0075-US4-AC6
- The spec-driven `effort-new` source template replaces its prior UTC filename command with a local filename stamp, chooses that stamp and slug before allocation, calls the strategy-aware generator with the prospective path, writes the returned id into both `id` and `aliases`, and retains UTC `created-at` and execution-event timestamps. ^SPEC-0075-US4-AC7

### US5 - Reconcile datetime collisions deterministically
- id:: ^SPEC-0075-US5
- summary:: Reconcile same-minute datetime identifiers deterministically after branches or repositories are combined.
- status:: satisfied
- increment:: 2

#### Acceptance Criteria

- Identifier validation groups duplicate datetime preferred values and aliases through the existing shared-pool collision model and reports every claimant; datetime allocation reduces collision frequency but makes no pre-merge uniqueness guarantee. ^SPEC-0075-US5-AC1
- The existing keeper policy retains the collided preferred value it claimed, whether unsuffixed or already suffixed; losing preferred claimants receive the lowest suffix `N >= 2` not reserved by a preferred value, alias, or earlier planned replacement in that timestamp family. ^SPEC-0075-US5-AC2
- The planner treats a base and all of its suffixed forms as one reservation family, never generates nested suffixes such as `-2-2`, and produces the same assignments and fingerprint from the same content and Git evidence regardless of input order. ^SPEC-0075-US5-AC3
- Repair preserves an effort's filename timestamp and slug while updating its preferred identifier, required alias mirror, derived descendants and locators, and structurally resolved inbound references in the existing connected transaction. ^SPEC-0075-US5-AC4
- Post-validation accepts only canonical datetime identifier grammar and rejects unresolved duplicate preferred identifiers or aliases. The filename is an allocation input, not a continuously derived identity field: later note moves or filename changes do not silently rekey a stable identifier or create a filename-mismatch failure. ^SPEC-0075-US5-AC5

### US6 - Migrate an identifier pool after an explicit strategy change
- id:: ^SPEC-0075-US6
- summary:: Detect an explicit schema strategy change and migrate the complete identifier pool through the ordinary validation review and apply workflow.
- status:: satisfied
- increment:: 3

#### Acceptance Criteria

- The existing `identifiers` check detects a nonempty allocatable pool whose preferred values coherently match the other supported strategy. Detection is part of default, all, and CI validation; it uses the prepared typed inventory plus one batched raw-property projection and, only for unresolved note roots, batched selector-candidate assessments. It does not run Git history, reference discovery, source hashing, or transaction construction unless a migration is actually required. ^SPEC-0075-US6-AC1
- A `SEQUENTIAL` to `DATETIME` migration derives each target base from the containing note's canonical leading filename minute. Members sharing a minute are ordered by their prior sequential ordinal and canonical node key and receive the unsuffixed base, then `-2`, `-3`, and later suffixes. Missing or malformed filename minutes, noncanonical sequential values, mixed source strategies, or collisions between an exact proposed target and a retained alias/claim make the migration `agent_required`; validation does not guess from filesystem time, silently substitute Git history, or skip the conflicting target to alter the mapping. ^SPEC-0075-US6-AC2
- A `DATETIME` to `SEQUENTIAL` migration orders notes by datetime base, existing suffix ordinal, then canonical node key and assigns a fresh dense sequence beginning at one using the target schema's prefix, separator, and pad. The mapping is deterministic but is not required to reproduce an earlier sequential numbering. ^SPEC-0075-US6-AC3
- A coherent migration produces one `needs_confirmation` action per pool through `rzm validate fix identifiers`; `--apply` rekeys every preferred identifier, required alias mirror, structurally derived descendant and locator, unambiguous typed relation or structured link, and governed filename occurrence through the existing sealed repair transaction and held-lease replan contracts. Note roots receive governed move planning; embedded allocatable roots rekey within their containing note without requiring a file move. Plain prose and source-code mentions remain review-only follow-ups. ^SPEC-0075-US6-AC4
- A migration that would edit a `complete` or `archived` effort remains lifecycle-protected and requires the existing explicit `--allow-historical` authority. Mixed, partial, ambiguous, or non-materializable pools remain blocked for agent review rather than being partially applied. ^SPEC-0075-US6-AC5
- Collision and migration components share one reviewed sealed authority over the schema, complete Markdown source inventory, deterministic old-to-new mappings, action memberships, and required postchecks. Multiple migrating pools and unrelated collision components remain independently attributable and transactionally composable. Apply replans under the held vault lease: schema/source/mapping drift blocks apply before mutation; after a committed repair, identifiers, ontology, and broken-link findings are surfaced as remaining evidence and replan guidance rather than semantic rollback. ^SPEC-0075-US6-AC6
- The source `rhizome-ontology` skill teaches authors to edit the ontology strategy first, run `rzm validate identifiers`, review `rzm validate fix identifiers`, and apply only the confirmed whole-pool migration. It distinguishes empty-pool schema changes from nonempty migrations and documents lifecycle authority and fail-closed cases. ^SPEC-0075-US6-AC7

## Requirements

### Must

- Identifier allocation MUST resolve through a public `IdentifierStrategy` abstraction owned by ontology identifier metadata.
- Strategy-owned behavior MUST parse, format, allocate, reserve, and propose collision replacements without sequential-specific numeric state leaking into shared orchestration.
- Reconciliation keeper selection MUST be separate from allocation strategy through a deterministic keeper policy.
- Shared identifier pools MUST use one strategy and one frozen inventory for each plan.
- The rendered prefix/separator namespace MUST be unique across allocation formats; sibling declarations sharing that namespace MUST compile to one compatible strategy and format.
- Datetime suffix inventory and replacement allocation MUST be scoped per timestamp base, not to a pool-wide numeric maximum.
- Existing nonempty identifier pools MUST NOT change strategy through starter refresh; strategy changes require an explicit migration/rekey contract that accounts for every legacy value.
- Explicit whole-pool strategy migration MUST be schema-first, bidirectional between `SEQUENTIAL` and `DATETIME`, deterministic, confirmation-tier, and all-or-nothing for the allocatable rendered namespace.
- Ordinary identifier validation MUST detect strategy drift from the prepared pool inventory without performing Git or reference/source planning work until a coherent nonempty migration is present.
- Sequential-to-datetime migration MUST use the canonical filename minute as its automatic source and MUST fail closed when that source is unavailable or ambiguous.
- Datetime-to-sequential migration MUST allocate a fresh dense order from the canonical datetime value order and MUST preserve target schema padding.
- A partial pool containing canonical target-strategy values and one coherent opposite-strategy source MAY migrate all remaining source members. Existing target values MUST remain unchanged and act as reservations. Malformed values, incompatible source formats, and target conflicts MUST block the migration.
- Duplicate detection MUST cover preferred-to-preferred, alias-to-preferred, and alias-to-alias collisions. For alias collisions, the same keeper policy retains the collided alias on one claimant, removes it from losers, rewrites structurally disambiguated references to each loser's preferred identifier, and blocks the connected transaction when intent remains ambiguous.
- Git history queries MUST be batched or cached in memory; implementations MUST NOT spawn one Git process per identifier occurrence.
- Reconciliation MUST scan note content at most once per plan and use indexed structured references where available.
- Independent collision plans MAY prepare concurrently, but SQLite writes MUST remain serialized through the repository writer contract.
- Filename moves MUST use a non-mutating governed move planner that emits the destination rename and every backlink/reference rewrite into the repair transaction; apply MUST NOT call the existing rename-first mutation path and MUST block on destination collision.
- A plan MUST include timings for inventory, Git provenance, reference discovery, transaction construction, apply, and post-validation.
- Apply MUST rerun identifiers, ontology, and broken-link checks for affected paths; fragile-external runs when heading-only links are affected.

### Should

- Structured links that predate one claimant or exist only on one claimant's branch should use Git ancestry evidence when it uniquely identifies the intended target.
- Plan output should disclose Git-history completeness, keeper rationale, fallback use, allocated replacements, affected paths, unresolved references, and plan fingerprint.

### May

- Future ontology versions may add allocation strategies without changing reconciliation transaction semantics.

## Documentation Plan

- Update ontology authoring guidance, identifier allocation and migration workflow, and validation subsystem guidance.
- Document keeper evidence, no-history fallback, governed filename rules, reference rewrite boundaries, and replan guidance on the owning durable surfaces.
- Add rationale comments at strategy, keeper-policy, transaction, and filename/reference rewrite boundaries where the code alone does not explain the constraint.

## Open Questions

None for this increment. Filename-derived datetime ids provide collision-tolerant eventual uniqueness after merged-head validation, not collision-free distributed allocation. Strategy migration is an explicit schema-first maintenance operation and not an `rzm init` side effect.
