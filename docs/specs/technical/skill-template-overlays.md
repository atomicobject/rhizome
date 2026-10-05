---
type: TechnicalSpec
summary: "Defines render-time skill template overlay slots so starter templates can inject or override bounded workflow guidance in built-in Rhizome skill templates without forking whole skills."
id: SPEC-0063
spec-status: active
last-updated: 2026-09-29
aliases:
  - SPEC-0063
  - Skill template overlays
  - skill-template-overlays
---


# Skill template overlays

## Summary

Starter templates need a way to extend built-in Rhizome skills when one starter builds on another. The immediate case is `complex-domain`: it requires `agentic-engineering`, then adds domain requirements, feature areas, processes, workflows, rules, and traceability views at the phases where those facts matter. Forking every Agentic Engineering skill would create drift. Adding conditional prose such as "if complex-domain is installed" inside a base skill is also wrong: an overlay is rendered only because the extension is active, so the final skill should speak as if the extended workflow is simply true.

Skill template overlays are a render-time init feature. Base skill templates expose explicit named slots. Extension starters contribute structured overlay fragments to those slots. `rzm init` resolves the active template set, validates all overlay targets, renders the installed `SKILL.md` files, strips every slot marker, and writes plain Markdown skills to the selected agent surfaces. Agents never need to parse overlay syntax at runtime.

The mechanism keeps base skill authors in control of where extension is allowed, lets extension starters add native workflow language, and gives maintainers debug output showing which overlays affected which final skills.

## Goals

- let starters such as `complex-domain` augment `agentic-engineering` router and adapter skills without copying entire skill files
- let base skills declare stable extension points and bounded replaceable default language
- render final installed skills as plain Markdown with no overlay markers or conditional extension prose
- make overlay composition deterministic, validated, and testable during `rzm init`
- apply the same rendered skill body to every selected skill surface, including canonical `.agents` skills and the `.claude` mirror where enabled
- provide debug output that explains overlay inputs, target slots, operations, ordering, conflicts, and final content hashes
- support append/prepend extension slots plus controlled replacement or suppression of default language where the base skill explicitly allows it
- keep saved query recipes, ontology schema, configured views, and skill overlays as separate asset families with separate validation

## Non-Goals

- runtime skill composition by agents or MCP clients
- arbitrary string patching of generated skill files after render
- allowing extension starters to modify skill frontmatter routing descriptions in v1
- turning skills into a programming language or general templating system
- composing ontology schemas, query recipes, or configured views through the skill overlay mechanism
- merging user-edited generated skill files beyond the existing init/diff-update behavior
- making extension starters responsible for base skill slot stability without base-skill opt-in

## Requirements

### Slot Markup

- Base skill templates MAY declare overlay slots with HTML comments so the template remains readable Markdown.
- Slot markup MUST be valid only in source templates under `pkg/app/cli/init/templates/...`; final installed skills MUST NOT contain slot markers.
- Slot ids MUST be unique within one skill template.
- Slot ids MUST be stable semantic names, not line numbers or implementation details.
- Slot ids SHOULD follow a dotted phase pattern such as `first-moves.after-start`, `context.before-draft`, `validation.before-handoff`, or `reconciliation.after-defaults`.
- The renderer MUST support two slot modes:
  - `extension`: default-empty slot that accepts `prepend` or `append` fragments.
  - `replaceable`: slot with default body content that accepts `prepend`, `append`, `replace`, or `suppress` fragments.
- Source markup SHOULD use this shape:

```md
<!-- rzm:skill-slot id="context.before-draft" mode="extension" -->
<!-- /rzm:skill-slot -->

<!-- rzm:skill-slot id="validation.default-checks" mode="replaceable" -->
- Run the default Agentic Engineering validation checks before handoff.
<!-- /rzm:skill-slot -->
```

- Empty extension slots with no matching overlays MUST be stripped completely.
- Replaceable slots with no matching overlays MUST render their default body with markers stripped.
- Unknown attributes, duplicate slot ids, unterminated slot blocks, nested slot blocks, or invalid modes MUST fail template validation.
- Slot comments MUST NOT appear inside fenced code blocks.

### Overlay File Format

- Extension starters MUST define overlays as structured data, not ad hoc Markdown patch files.
- Overlay files SHOULD live under `pkg/app/cli/init/templates/starters/<starter>/agents/skill-overlays/*.yaml`.
- The initial overlay schema SHOULD use this shape:

```yaml
apiVersion: rhizome.skill-overlay.v1
id: complex-domain-specify
targetSkill: agentic-engineering
fragments:
  - file: references/specification.md
    slot: context.after-discovery
    op: append
    order: 20
    content: |
      Run `domain-context-pack` and `spec-domain-context-pack` before drafting.
      Treat structurally linked requirements as the coverage contract; use semantic
      search only to explain gaps or discover candidate context.
```

- Overlay files MUST include `apiVersion`, stable `id`, `targetSkill`, and at least one fragment.
- `targetSkill` MUST resolve to a skill installed by the resolved template set.
- Each fragment MUST include `slot`, `op`, and `content` unless `op: suppress` intentionally has no content.
- A fragment MAY specify `file`, a skill-relative Markdown path that defaults to `SKILL.md`. Unknown target files MUST fail before writes; file-targeted fragments use slots declared in that file.
- Supported operations in v1 are `prepend`, `append`, `replace`, and `suppress`.
- `order` MUST be optional; when omitted it defaults to `100`.
- Fragment content MUST be Markdown body content only, not a full skill document with frontmatter.
- Fragment content SHOULD be written as native final skill prose. It SHOULD NOT say "if complex-domain is installed" or similar conditional language.
- Overlay validation MUST reject unknown target skills, unknown slots, invalid operations for the target slot mode, duplicate overlay ids, and empty content for operations that require content.

### Rendering Pipeline

- `rzm init` MUST resolve the full template set before loading skill overlays.
- The renderer MUST load base skill templates from the resolved template set according to existing skill installation rules.
- The renderer MUST parse slot declarations before applying overlays.
- The renderer MUST load overlays only from templates active in the resolved template set.
- The renderer MUST apply overlays before writing any final skill files.
- Final skill files MUST be ordinary `SKILL.md` documents without overlay comments, overlay metadata, or debug blocks.
- The renderer MUST write the same rendered body to all enabled skill surfaces so the canonical `.agents` tree and `.claude` mirror do not drift. Codex consumes the shared tree through its adapter surfaces; Cursor consumes `AGENTS.md` through `.cursor/rules/rhizome.mdc` and does not receive skill files.
- The renderer MUST preserve existing skill collision rules: two templates cannot own the same base skill unless one is extending through overlays rather than installing a second base skill file.
- The renderer MUST fail before partial writes when an overlay references a skill or slot that is not available in the resolved template set.
- The renderer SHOULD integrate with the existing diff/rejection update flow for generated skill files.

### Composition Semantics

- For each slot, the renderer MUST collect fragments from active templates that target that slot.
- Fragment ordering MUST be deterministic:
  1. resolved template order
  2. overlay file path
  3. fragment `order`
  4. fragment index within file
- `prepend` fragments render before the slot body in deterministic order.
- `append` fragments render after the slot body in deterministic order.
- A `replace` fragment replaces the default slot body for a `replaceable` slot.
- A `suppress` fragment removes the default slot body for a `replaceable` slot.
- `replace` and `suppress` MUST be rejected for `extension` slots.
- More than one `replace` or `suppress` operation for the same slot MUST be a fatal conflict.
- `replace` and `suppress` on the same slot MUST be a fatal conflict.
- `prepend` and `append` MAY compose around a replacement body.
- `prepend` and `append` MUST be rejected when a slot is suppressed, because there is no stable surrounding context.
- The renderer SHOULD preserve blank lines cleanly so final Markdown has no doubled separators, dangling list indentation, or empty headings caused by stripped slots.

### Base Skill Authoring Rules

- Base skills SHOULD expose slots only at meaningful workflow boundaries, not between arbitrary sentences.
- Base skills SHOULD prefer extension slots when an addon may add extra context-loading, validation, or handoff steps.
- Base skills SHOULD use replaceable slots only when the default language is a coherent block that an addon may legitimately replace or suppress.
- Base skill authors MUST keep the default skill useful when no overlays are active.
- Slot names MUST be treated as public template API once released.
- Removing or renaming a slot MUST be treated as a breaking starter-template change unless all bundled overlays are migrated in the same release.
- Base skills SHOULD document intended slot use in source comments or nearby template tests, not in the final installed skill.
- Base skill slots SHOULD be narrow enough that extension starters add a few paragraphs or bullets, not a second skill body.

### Extension Skill Authoring Rules

- Extension overlays MUST add operational instructions that agents can execute, not vague descriptions of extra context.
- Overlay prose SHOULD include exact recipe ids, view ids, validation checks, or handoff artifacts when those are required for the outcome.
- Overlay prose SHOULD say what to do with empty, partial, conflicting, stale, or high-volume context results.
- Overlay prose SHOULD preserve the phase boundary of the target skill. For example, a specification-phase overlay should not start implementation planning.
- Overlay prose MUST NOT duplicate ontology field semantics that agents should rediscover from the authoring guide or query schema.
- Overlay prose SHOULD be concise enough that the final skill remains usable by agents in small contexts.
- Extension starters SHOULD include their own router and phase skills when the workflow is larger than a small augmentation to base skills.

### Complex-Domain Overlay Contract

- `complex-domain` SHOULD overlay the `agentic-engineering` specification reference to require domain/requirement context packs before creating or changing specs.
- `complex-domain` SHOULD overlay the `agentic-engineering` effort-setup reference to preserve material upstream requirement/domain context alongside frozen spec and story scope.
- `complex-domain` SHOULD overlay the `agentic-engineering` planning reference to require requirement trace packs before architecture, phase, test, or foundation decisions.
- `complex-domain` SHOULD overlay the `agentic-engineering` implementation reference to check linked requirements before coding, investigation, or tests and to record domain mismatches for `domain-backport`.
- `complex-domain` SHOULD overlay the `agentic-engineering` compounding reference's `routing.additional-compounding` slot to capture repeated domain/traceability friction as durable future-capacity work.
- `complex-domain` MAY overlay `ingest-transcript` to route domain-heavy source material into requirement-source extraction.
- `agentic-engineering` exposes the extension slot `alignment.additional-context` in `references/alignment.md` after classification of findings, and `reconciliation.additional-targets` in `references/reconciliation.md` after selection of the smallest durable home. `complex-domain` may consume these hooks for affected domain evidence and reconciliation targets, reusing current context and preserving authority boundaries. Empty hooks add no behavior to Agentic Engineering alone.
- `domain-backport` owns delivery-to-domain reconciliation for requirements, domain types, processes, workflows, sources, and related action items. It is a complex-domain skill, not an Agentic Engineering overlay target.
- These overlays MUST read as native workflow instructions in the final skill. They MUST NOT mention installation conditionals.

### Debug Output And Manifest

- `rzm init` MUST report overlay application in the normal init run log or structured init log when overlays are active.
- Overlay debug output MUST include target skill, slot id, operation, overlay id, source template, source file, fragment order, and outcome.
- Overlay debug output SHOULD include content hashes rather than full fragment bodies so logs stay compact and do not duplicate skill prose.
- Init SHOULD support a debug flag that writes a structured manifest for deeper inspection.
- The manifest SHOULD be written under `.rhizome/logs/` or another ignored runtime/debug path unless implementation planning identifies a tracked provenance use case.
- A manifest entry SHOULD include:
  - resolved template set and template order
  - base skill source path and rendered destination paths
  - slot ids discovered in each base skill
  - fragments applied to each slot
  - skipped overlays and why they were skipped, if any
  - conflicts or validation errors when render fails
  - final rendered skill content hash
- The manifest MUST NOT be required for normal agent operation. It is a troubleshooting and test artifact.

### Validation And Errors

- Overlay validation MUST run as part of starter validation before writes.
- Validation MUST produce stable issue codes for unknown target skill, unknown slot, invalid slot mode, invalid operation, duplicate slot id, duplicate overlay id, conflicting replace/suppress, unterminated slot, nested slot, and markers left in rendered output.
- Init MUST fail on fatal overlay validation issues before writing skill files.
- Non-fatal diagnostics MAY warn when a slot is declared but unused or when an overlay file is present for a template that is inactive.
- `rzm agent validate` SHOULD include a skill-overlay check once the asset family exists.
- Validation MUST ensure the final rendered skill body contains no `rzm:skill-slot` markers.
- Validation MUST keep skill overlay files out of saved query recipe discovery. Skill agent metadata files such as `agents/openai.yaml` remain metadata, not recipes.

### Implementation Boundaries

- Overlay parsing and rendering SHOULD live in the init/template package near existing skill installation code.
- The renderer SHOULD use a small Markdown/comment scanner rather than regular expressions that can cross fenced-code boundaries accidentally.
- Overlay YAML parsing SHOULD use strict field decoding so misspelled fields fail fast.
- The rendered output should flow through the existing managed file writer/diff updater rather than introducing a parallel file write path.
- Template metadata SHOULD advertise `skill-overlays` as an installed asset family when a starter ships overlays.
- Existing starter collision checks SHOULD account for overlays as extensions, not as duplicate skill ownership.
- Backward compatibility with old generated skills is not required beyond the ordinary init refresh/update path.

### Testing

- Unit tests MUST cover slot parsing, marker stripping, empty extension slots, replaceable default preservation, prepend/append ordering, replace, suppress, and conflict detection.
- Unit tests MUST cover fenced-code blocks containing marker-like text that must not be parsed as slots.
- Unit tests MUST cover overlay YAML strict decoding, unknown target skill, unknown slot, invalid operation, invalid mode, duplicate ids, and deterministic ordering.
- Init integration tests MUST cover:
  - `agentic-engineering` alone renders its router and references with markers stripped and no complex-domain content.
  - `complex-domain` alone resolves `agentic-engineering` and renders complex-domain overlays into the affected router and adapter skills.
  - `agentic-engineering,complex-domain` renders the same effective skill output as `complex-domain` plus explicit Agentic Engineering selection, modulo resolved-template explanation.
  - overlays render consistently into the canonical `.agents` tree and the enabled `.claude` mirror.
  - a broken overlay fails before partial skill writes.
- Golden tests SHOULD cover the `agentic-engineering` router and its specification, planning, and implementation references with and without complex-domain overlays.
- Debug tests SHOULD prove the init log or manifest names applied overlays without embedding full skill bodies.

## User Stories

### US1 - Declare stable extension slots where downstream starters can add or replace bounded workflow guidance
- id:: ^SPEC-0063-US1
- summary:: Declare stable extension slots where downstream starters can add or replace bounded workflow guidance.
- status:: ready

#### Acceptance Criteria

- Slots are explicit and validated. ^SPEC-0063-US1-AC1
  Base skill templates can declare named extension and replaceable slots, and invalid slot markup fails template validation before init writes generated skills.
- Default skills remain useful alone. ^SPEC-0063-US1-AC2
  When no overlays target a base skill, the rendered skill keeps its default behavior with all slot markers stripped.
- Slots are stable template API. ^SPEC-0063-US1-AC3
  Removing or renaming a slot requires migrating bundled overlays and tests in the same change. Additive alignment and reconciliation hooks preserve every existing slot and its skill-relative file target.

### US2 - Add native workflow guidance to existing skills without forking the whole skill file
- id:: ^SPEC-0063-US2
- summary:: Add native workflow guidance to existing skills without forking the whole skill file.
- status:: ready

#### Acceptance Criteria

- Overlay files target slots, not lines. ^SPEC-0063-US2-AC1
  An overlay names a target skill, optional skill-relative Markdown file, and slot id, supplies an operation and Markdown fragment, and does not depend on line numbers or brittle string replacement.
- Overlay prose is final-skill prose. ^SPEC-0063-US2-AC2
  Rendered fragments do not contain installation conditionals such as "if complex-domain is installed"; they read as native instructions for the active workflow.
- Invalid overlays fail early. ^SPEC-0063-US2-AC3
  Unknown slots, invalid operations, duplicate replacement, and conflicting suppression fail before any partial skill files are written.

### US3 - Initialize a repo with composed starters and receive one coherent set of rendered skills
- id:: ^SPEC-0063-US3
- summary:: Initialize a repo with composed starters and receive one coherent set of rendered skills.
- status:: ready

#### Acceptance Criteria

- Complex-domain renders through Agentic Engineering. ^SPEC-0063-US3-AC1
  Selecting `complex-domain` resolves the required `agentic-engineering` router and adapter templates and renders complex-domain overlay fragments into the affected skills and phase references. Alignment and reconciliation use their declared extension hooks without repeating base retrieval mechanics or silently changing accepted scope.
- Explicit double selection is deterministic. ^SPEC-0063-US3-AC2
  Selecting both `agentic-engineering` and `complex-domain` produces the same rendered skill bodies as selecting `complex-domain` alone, except for the resolver explanation that records explicit selections.
- Installed skills contain no overlay mechanics. ^SPEC-0063-US3-AC3
  Final `SKILL.md` files contain plain Markdown instructions and no `rzm:skill-slot` comments, overlay ids, or debug manifests.

### US4 - Inspect which overlays changed which skills without reading every generated file by hand
- id:: ^SPEC-0063-US4
- summary:: Inspect which overlays changed which skills without reading every generated file by hand.
- status:: ready

#### Acceptance Criteria

- Init reports applied overlays. ^SPEC-0063-US4-AC1
  The init log or structured run output reports target skill, slot id, operation, overlay id, source template, and outcome for every applied overlay.
- A debug manifest can be written. ^SPEC-0063-US4-AC2
  A debug flag can emit a structured manifest with resolved templates, parsed slots, applied fragments, conflicts, skipped overlays, destination paths, and rendered content hashes.
- Debug output is not operational state. ^SPEC-0063-US4-AC3
  Agents do not need the manifest to use skills, and the manifest is ignored or debug-scoped unless a later spec makes tracked provenance explicit.

### US5 - Catch overlay drift, slot drift, and invalid final skill output in automated checks
- id:: ^SPEC-0063-US5
- summary:: Catch overlay drift, slot drift, and invalid final skill output in automated checks.
- status:: ready

#### Acceptance Criteria

- Overlay validation has stable issue codes. ^SPEC-0063-US5-AC1
  Validation reports stable codes for unknown target skill, unknown slot, invalid operation, duplicate slot id, duplicate overlay id, conflicting replace/suppress, invalid markup, and leftover markers.
- Render tests cover composed starter cases. ^SPEC-0063-US5-AC2
  Tests prove `agentic-engineering` alone, `complex-domain` alone, and `agentic-engineering,complex-domain` produce the expected rendered skill outputs.
- Recipe validation remains separate. ^SPEC-0063-US5-AC3
  Skill overlays do not cause skill metadata or agent interface files to be loaded as saved query recipes.

## Open Questions

- Should the debug manifest path be controlled by a general `rzm init --debug-output` flag, a skill-overlay-specific flag, or the existing Rhizome log configuration once that is standardized?
- Should v1 allow overlays to adjust skill frontmatter descriptions, or should routing metadata remain base-skill-owned until a concrete starter needs frontmatter composition?
