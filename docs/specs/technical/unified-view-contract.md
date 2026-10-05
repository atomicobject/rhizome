---
type: TechnicalSpec
id: SPEC-0110
aliases: [SPEC-0110, Unified view contract]
summary: "One registration, invocation context, configuration, lifecycle, and service contract for built-in and custom views mounted on type collections, display groups, and individual nodes."
spec-status: active
last-updated: 2026-10-04
---

# Unified view contract

## Summary

[[view-preferences|SPEC-0114]] owns durable personal settings, concrete invocation identity, reset and migration, and explicit promotion into shared YAML. Its ignored repository-local SQLite store supersedes browser-only preferences and ephemeral expansion choices in this contract.

Overview, Table, Cards, Kanban, and custom HTML/TSX views become implementations of one view contract. A shared host resolves registration, configuration, concrete subject, navigation, refresh, and the workspace edit session. Built-in React rendering and trusted custom frames remain renderer choices behind that contract.

This extends [[configured-view-engine-and-repo-config|SPEC-0058]] and [[custom-view-apps|SPEC-0105]]. Those specs retain ownership of native collection execution and custom-code serving respectively; this spec owns applicability, invocation, selection, and their shared programming boundary. It supersedes the Home/Table toggle in [[ontology-browser-workspace|SPEC-0014]] and [[notes-workspace-shell|SPEC-0090]]. [[group-views-and-view-platform|SPEC-0111]] delivers the bundled group pages this spec reserved: it adds a `bundled` catalog origin, lets a repository view replace a bundled view by id, and makes a bundled view the group fallback default.

## Goals

- Open the configured default when a user selects a type or display group, while keeping every compatible presentation selectable.
- Support views authored for one named group or type, reusable group views, and node presentations parameterized by canonical identity.
- Migrate Overview and existing native layouts onto the same contract used by custom code.
- Let a subsequent agent register a bundled generic group page without modifying routing, selection, or the host.
- Preserve existing definitions, native editing, staged reads, and custom standalone behavior.

## Non-Goals

- Authoring the bundled generic display-group page; that is explicitly subsequent work, since delivered by [[group-views-and-view-platform|SPEC-0111]].
- Replacing native collection execution, schema capabilities, or existing table/card/kanban renderers.
- A new public config version, plugin loader, package registry, or visual configuration editor.
- Changing ontology membership through display grouping or introducing a generic group query source.
- Combining trusted custom views with the isolated HTML-note viewer.

## Requirements

### Registration and applicability

- Existing `rhizome.view.v1` files MUST remain compatible and use the existing loader, validator, and catalog. No parallel custom-view manifest is introduced.
- Each selectable view MUST have a stable identity, name, applicable target, ordering, renderer, configuration, and configured-default marker. A declarative definition's variants become separately selectable choices without duplicating its native source engine.
- Overview, Table, Cards, and eligible Kanban layouts MUST register through the same normalized contract as custom views. Built-ins MUST remain available when an authored default exists.
- Definition, mount, and invocation MUST remain independent. A mount limits availability; it does not silently change query semantics.
- Custom code MUST support `type`, `interface`, `group`, `node`, and existing `standalone` mounts. A `type` names one concrete collection type; an `interface` names its implementing collection; a `node` names one concrete node type.
- `group` mounts MUST accept one effective display-group name or `"*"`. The latter means any display group and receives the selected group as context. A specific mount MUST appear only for its group.
- Multiple definitions MUST be available for the same target. Ordering MUST be deterministic; conflicting defaults at the same specificity MUST produce diagnostics and retain a usable deterministic result.
- Effective group discovery MUST share the navigation interpretation, including existing parent relationships. Display groups remain presentation metadata, never ontology membership.
- Invalid definitions MUST report actionable diagnostics without hiding compatible valid views or built-in fallbacks.
- A native view on a `type` or `interface` mount MAY set `mount.replaceGenerated: true` to take the generated view's place for that subject. The generated view then contributes no choices there and stays addressable by ID. The field on any other mount, or on a custom source, MUST produce a warning and be ignored. Several valid replacing views on one target MUST each produce a warning; the first by order, then ID, replaces and the rest remain ordinary authored choices.
- Each choice MUST carry `custom: true` when it comes from an authored view beyond the subject's standard presentations. Standard choices are the built-ins, the subject's own native layouts (generated or replacing), and a standalone target's own layouts. Choices MUST list standard ones first (built-ins, then native layouts in Table, Cards, Board order), then custom ones by order, then ID. The subject's own layouts, including a standalone target's, are named "Table", "Cards", and "Board"; custom native choices are qualified as "<View> · <Layout>".

Examples of additive custom registrations:

```yaml
apiVersion: rhizome.view.v1
id: delivery.dashboard
name: Dashboard
source:
  kind: custom
  entry: dashboard.html
mount:
  kind: group
  group: Delivery
  default: true
```

```yaml
mount:
  kind: group
  group: "*"
  default: true
```

### Invocation and programming contract

- Every view MUST consume one typed runtime contract containing registration, concrete context, authored configuration, lifecycle state, querying, navigation, and staged editing. Merely sharing a selector does not satisfy this requirement.
- Concrete context MUST distinguish type collection, interface collection, display group, focused node, and context-free standalone invocation.
- Node context MUST carry the resolved concrete type and canonical `NodeRef`, including embedded fragment and node identity. A file path alone is insufficient.
- The host MUST own activation, context changes, refresh, and cleanup. Inactive custom frames MUST unmount and release subscriptions and polling.
- Built-ins MUST consume that contract through adapters to their existing components. `HomeTab` and `ViewTab` dispatch MUST converge on a shared host rather than remain independent presentation-selection owners.
- Queries, canonical navigation, refresh subscriptions, and edit-session operations MUST reuse the existing clients and services. Native collection execution MUST be accessible to custom views through the shared service boundary, with canonical items, capabilities, warnings, and normalized state intact.
- Custom TSX entries MUST receive context and configuration through component props or the kit's shared accessor/hook. HTML entries MUST be able to use the same accessor and services.
- Invocations and query-cache identities MUST include the subject, so two nodes using one custom definition cannot share the wrong data or tab.

### Defaults and user selection

- Selection precedence MUST be explicit requested view, valid remembered choice, configured default, then compatible built-in fallback.
- Without an authored configured default, a type or interface collection MUST open its generated view's default variant, which [[type-collection-views|SPEC-0112]] chooses from the type's shape (Table unless a workflow type's open work fits a Board), or its replacing view's default variant (else its first layout) when one replaces the generated view. Display groups fall back to a bundled view chosen by the group's shape, as [[group-views-and-view-platform|SPEC-0111]] defines, or to Overview's navigation list when no bundled view is available. Individual nodes fall back to the structured presentation. Overview remains selectable for collections.
- An exact configured group default MUST outrank a generic group default. Remembered and explicit choices retain their higher precedence.
- Preferences MUST be scoped to vault and subject. Collection preferences and individual-node preferences MUST be distinct.
- The UI MUST support clearing the remembered override to use the configured default. The view switcher marks the configured default, and selecting it clears the remembered choice so later default changes apply.
- Native layout switching MUST retain the source definition's filters, sorting, and other applicable state. Invalid or removed selections MUST fall back safely; a remembered generated layout whose view was replaced MUST select the replacing view's same layout when it has one, else the configured default.
- Existing Home/Table and native variant preferences MUST migrate once where compatible, then relinquish selection ownership to the shared mechanism.
- Shared backend resolution MUST determine applicability and configured defaults for browser and agent callers. Clients apply valid personal overrides through the shared preferences service governed by [[view-preferences|SPEC-0114]]; the server owns durable storage.

### Navigation and workspace behavior

- Clicking a type row MUST open its selected/default presentation. Overview, native layouts, and named custom views MUST be selectable in that workspace.
- Clicking a group label MUST open its group workspace; the caret remains the expansion control. Groups without an authored page MUST provide a usable built-in navigation fallback; [[group-views-and-view-platform|SPEC-0111]]'s bundled views now provide it.
- Custom node presentations MUST appear beside the existing format-appropriate structured/source presentations and preserve breadcrumbs, context panels, and the workspace edit session.
- Navigation helpers MUST support selecting a named view when opening a canonical node, including opening beside. A dashboard-to-collection-to-node flow MUST preserve subject identity.
- Direct launch URLs MUST encode the same context and configuration as embedded invocation. Existing standalone custom links MUST remain valid.
- Routing MUST distinguish a view selected inside a node workspace from a standalone view tab. Changing presentation MUST neither commit nor discard pending edits.
- Embedded custom reads and writes MUST use the host's edit session. Standalone and explicitly immediate edit modes MUST retain their existing semantics. Frame messaging MUST retain origin and source checks.

### Agent guidance and documentation

- Canonical custom-view and Rhizome skill templates MUST teach target/default discovery, schema and query discovery, mount authoring, runtime context, navigation, shared native execution, validation, and verification in the actual mounted UI.
- Guidance MUST include an end-to-end custom collection/group dashboard linking to a parameterized node view, and explain exact versus generic group defaults.
- Kit reference, owning subsystem/CONTEXT notes, OpenAPI/generated types, and release notes MUST reflect the delivered contract. Generated skill copies MUST be produced by `rzm init`, never hand-edited.

### Verification

- Tests MUST cover specific/generic applicability, default specificity and conflicts, retained built-ins, invalid-definition isolation, and backward compatibility.
- Browser checks MUST cover fresh defaults, persisted choice and reset, native/custom switching, specific group restriction, embedded-node context, two nodes using one view, direct standalone launch, selected-view navigation, and pending edits surviving switching.
- Existing custom-frame security and cleanup checks MUST remain effective. Native execution reuse MUST have a runnable check through a custom consumer.
- Repository gates MUST include `make check`, `make web-e2e`, documentation and frozen-scope validation, build plus template regeneration twice, and `make check-full` before closure without a pull request.

## Open Questions

No blocking product decisions remain. Export and route spellings may be settled during implementation within this contract; incompatible scope changes require an explicit deviation.
