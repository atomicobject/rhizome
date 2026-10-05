---
type: TechnicalSpec
summary: "Defines the shared core starter template and explicit template dependency resolution for composing spec-driven, project-kb, action-items, and future starter templates."
id: SPEC-0061
spec-status: proposed
last-updated: 2026-05-06
aliases:
  - SPEC-0061
  - Core template and template dependencies
---

# Core template and template dependencies

## Summary

Rhizome starter templates need a shared foundation for identity and template composition. `spec-driven`, `project-kb`, `action-items`, and future workflow starters should be able to share a single `Person` model, a reusable current-user setting, and deterministic dependency resolution instead of duplicating schema and skill guidance in each starter.

This spec introduces a non-workflow `core` template plus explicit starter metadata. The `core` template owns shared identity primitives such as `Person`, optional team identity, and local current-user configuration. Starter metadata lets init distinguish templates the user explicitly selected from templates required or activated by other templates. Selecting `spec-driven` or `project-kb` can therefore resolve to `core` plus the selected workflow starter plus default addons such as `action-items`, while keeping the dependency graph observable and testable.

## Goals

- factor shared identity primitives out of workflow-specific starters
- make `core` a required dependency for `spec-driven`, `project-kb`, and `action-items`
- allow starter templates to declare required dependencies and default activated addons
- make selecting `spec-driven` or `project-kb` activate `action-items` by default while preserving an explicit opt-out for optional addons
- keep required dependencies non-optional once a dependent template is selected
- record enough template selection state for future init reruns to explain what was selected explicitly and what was resolved transitively
- validate dependency graphs, installed ontology, query recipes, views, skills, and managed docs before writing partial starter output

## Non-Goals

- building a public plugin marketplace or third-party template registry
- version-negotiating independently released template packages
- putting project-kb persona semantics into `core.Person`
- making action items inseparable from every starter forever; action-items is a default addon, not a required dependency of spec-driven or project-kb
- using OS usernames, Git authors, chat context, or vault names as implicit identity
- moving transient agent runtime state into tracked repo config

## Requirements

### Template Metadata And Resolution

#### Must

- Each starter template MUST have metadata with stable template id, human name, required dependencies, default activated addons, and installed asset families.
- The resolver MUST normalize the explicitly requested template set before expanding dependencies.
- The resolver MUST expand `requires` transitively.
- A required dependency MUST NOT be disabled while any selected or activated template depends on it.
- The resolver MUST expand default activated addons after required dependencies are known.
- Optional activated addons MUST be disableable through an explicit init setting or flag.
- Resolver output MUST distinguish templates explicitly selected by the user from templates added because they are required dependencies or default activated addons.
- Init MUST surface the resolved template set before writing starter assets in interactive mode.
- Non-interactive init MUST write enough config to preserve explicit selections, disabled optional addons, and the effective resolved template set on later reruns.
- Unknown template ids, dependency cycles, disabled required dependencies, and starter asset collisions MUST fail before any template assets are written.
- Collision checks MUST run against the resolved template set, including required dependencies and default addons.

#### Should

- Metadata should live beside starter assets, for example under `pkg/app/cli/init/templates/starters/<id>/template.yaml`.
- The resolver should produce a deterministic explanation payload suitable for CLI output, tests, and future MCP/init surfaces.
- Existing comma-separated template input should remain a supported authoring shorthand while mapping into the richer resolver model.
- Rerunning init should preserve previous explicit choices unless the user changes the template selection.

### Core Template

#### Must

- `rzm init` MUST recognize a `core` template.
- `core` MUST be installable as a dependency even when it is not directly selected by the user.
- `core` MUST install source assets from `pkg/app/cli/init/templates/starters/core/**`.
- `core` MUST install starter-owned ontology under a path such as `.rhizome/ontology/core.graphql`.
- `core` MUST define a note-backed `Person` type.
- `Person` MUST use tracked markdown notes, not embedded nodes inside team or project notes.
- `Person` MUST use the note title as its primary author-facing identity; it MUST NOT require a synthetic id field.
- `Person` MUST support display name and aliases.
- `core` MUST provide a private per-vault current-user setting that points to a `Person` node by author-facing title, alias, wikilink target, or path that Rhizome canonicalizes.
- The current-user setting MUST live in ignored local state under `.rhizome/agent/` or an equivalent ignored per-vault agent-state path.
- The `Person` note itself MUST be tracked repo content; only the local selection of which `Person` is "me" is private ignored state.
- Validation or setup diagnostics MUST report missing, unresolved, or non-`Person` current-user settings when an installed workflow requires a current user.
- Rhizome MUST provide a user-facing agent or CLI surface for showing, setting, and validating the current-user setting so agents do not hand-edit ignored identity files.
- The ontology GraphQL/runtime surface MUST expose the configured current user through a queryable value such as `currentUser { person { nodeRef displayName path } }`.
- Query recipes and configured views MUST be able to bind filter values from the current-user query/runtime value without inferring identity from chat context, OS usernames, Git authors, vault names, or prose.

#### Should

- `core` should also define a note-backed `Team` type when doing so keeps action-items, spec-driven, and project-kb from inventing parallel team identity shapes.
- Team-specific context such as role, membership status, or dates should be modeled as embedded membership nodes on a team note, not by embedding the canonical person.
- Interactive init should offer to create or select a `Person` note for the human running init when a current-user setting is needed.
- Non-interactive init should accept an explicit current-user ref or leave an actionable setup issue rather than fabricating identity.
- `core` should install concise identity authoring guidance, and may install a small identity skill, so creating/selecting `Person` nodes is not accidentally owned by action-items.
- Future personal workflow features should consume the same current-user resolver rather than defining feature-local settings.

### Starter Dependency Contracts

#### Must

- `spec-driven` MUST declare `requires: [core]`.
- `project-kb` MUST declare `requires: [core]`.
- `action-items` MUST declare `requires: [core]`.
- `spec-driven` MUST activate `action-items` by default.
- `project-kb` MUST activate `action-items` by default.
- Selecting `spec-driven` in interactive or non-interactive init MUST resolve to at least `core` and `spec-driven`, and SHOULD include `action-items` unless optional addons are explicitly disabled.
- Selecting `project-kb` in interactive or non-interactive init MUST resolve to at least `core` and `project-kb`, and SHOULD include `action-items` unless optional addons are explicitly disabled.
- Selecting `action-items` directly MUST resolve to `core` plus `action-items`.
- `spec-driven` and `project-kb` MUST NOT duplicate `Person` or current-user schema once they depend on `core`.

#### Should

- The default new-repo workflow should present `spec-driven` plus its resolved `core` and `action-items` template set as the recommended path unless the user selects a different workflow.
- The resolver should allow future starters to activate additional optional addons without adding starter-name branches to init write logic.

### Person Modeling

#### Must

- A `Person` is a note-backed ontology node.
- A team note MAY contain embedded membership nodes that link to `Person`, but those embedded nodes MUST NOT replace the canonical `Person`.
- Action-item assignees MUST link to `Person` nodes supplied by `core`.
- Cross-template fields that mean "human or accountable actor" SHOULD use `Person` unless a spec explicitly requires a narrower type.

#### Should

- `Person` notes should live under a conventional path such as `people/**/*.md` or `docs/people/**/*.md`, with final path policy owned by the core authoring guide.
- `Team` can be used for grouping people, but assignment semantics should remain explicit: either assign to a `Person`, or introduce a separately specified accountable-group field later.

### Tests

#### Must

- Resolver unit tests MUST cover direct selection, transitive dependencies, default activated addons, optional addon opt-out, unknown template ids, dependency cycles, disabled required dependencies, and deterministic ordering.
- Init integration tests MUST prove `spec-driven` installs `core` and default `action-items` assets.
- Init integration tests MUST prove `project-kb` installs `core` and default `action-items` assets.
- Init integration tests MUST prove direct `action-items` selection installs `core`.
- Init integration tests MUST prove `spec-driven` and `project-kb` can compose with `core` and `action-items` without ontology, query recipe, view, skill, or managed-doc collisions.
- Validation tests MUST prove duplicate `Person` definitions are not introduced by dependent starters.
- Tests MUST prove current-user private settings are ignored local state while `Person` notes are tracked content.
- Tests MUST prove the current-user setup surface can show, set, and validate a `Person` identity.
- Tests MUST prove the GraphQL/runtime `currentUser` value resolves configured, missing, unresolved, and non-`Person` identity states.
- Tests MUST prove configured views can bind canned filter values from the current-user query/runtime value.

#### Should

- Tests should validate the resolver explanation payload, not only final files on disk.
- Tests should include a rerun case where a repo previously selected `spec-driven` and then opts out of optional `action-items` without losing required `core`.

## Related Contracts

- [[action-items-starter-template]] owns `ActionItem` schema, action-item recipes, action-item views, and the action-items skill that consumes `core.Person`.
- [[init-template-architecture]] owns existing starter asset families, managed docs, install/refresh behavior, and collision boundaries.
- [[init-starter-workflow]] owns user-facing init behavior for selecting workflow starters and installing starter docs and skills.
- [[configured-view-engine-and-repo-config]] owns tracked view config that default activated addons may install.
- [[saved-query-recipes]] owns tracked query recipe metadata and validation.

## Open Questions

- [TODO: Confirm metadata shape] Should starter metadata live in a `template.yaml` file beside assets, in Go registration structs, or in both with tests enforcing parity?
- [TODO: Confirm config shape] What exact `.rhizome/config.yml` keys should preserve explicit selections, disabled optional addons, and resolved templates?
- [TODO: Confirm default opt-out UX] Should disabling optional addons be exposed as `--no-template action-items`, `--disable-addon action-items`, a structured config value, or interactive checklist state?
- [TODO: Confirm current-user path] Should the first current-user file be `.rhizome/agent/user.yml`, `.rhizome/agent/identity.yml`, or another ignored per-vault path?
- [TODO: Confirm Team scope] Should `Team` ship in the first core slice, or should `core` initially define only `Person` and leave `Team` to a follow-on effort?
