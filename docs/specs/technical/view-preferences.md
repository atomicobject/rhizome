---
type: TechnicalSpec
id: SPEC-0114
aliases: [SPEC-0114, View preferences]
summary: "Durable personal settings for each view instance, with shared view structure in repository YAML and one preferences API for native and custom views."
spec-status: active
last-updated: 2026-10-04
---

# View preferences

## Summary

Views remember personal choices automatically in an ignored, repository-local SQLite database. Shared view definitions remain in `.rhizome/views/`. A preference belongs to a concrete invocation of a view, so reusing a definition for different groups, collections, or nodes does not combine their settings.

This extends [[unified-view-contract|SPEC-0110]], [[configured-view-engine-and-repo-config|SPEC-0058]], [[type-collection-views|SPEC-0112]], and [[custom-view-apps|SPEC-0105]]. It replaces their browser-only preference storage and ephemeral collapse behavior. Native collection execution, staged content edits, and repository view definitions retain their existing owners.

## Goals

- Restore personal view choices across reloads, server restarts, and changes of browser origin.
- Keep ordinary view interactions out of Git while allowing deliberate publication of shared view structure.
- Isolate settings by view instance and provide a reset that returns to current repository defaults.
- Give native views, bundled views, and repository-authored HTML/TSX views the same persistence service.

## Non-Goals

- Cloud synchronization, user accounts, or shared preferences across separate checkouts.
- Replacing view YAML, the index database, or the ontology edit session.
- Persisting drag state, menus, checked rows, focus, scroll windows, or camera motion.
- A general visual editor for custom-view definitions.

## Requirements

### Ownership and precedence

- `rzm new-worktree` MUST seed personal settings from the source checkout when its user-state database exists and the destination has no user-state database or WAL/SHM sidecars. The copy MUST be a consistent SQLite snapshot, using copy-on-write where supported, and MUST retain revisions, migration claims, and reset tombstones. Later preference changes MUST be independent between checkouts. Existing destination state MUST be preserved.
- Personal settings MUST be stored in `.rhizome/user-state.sqlite`, independently of the disposable index and its rebuild lifecycle. The database and sidecars MUST remain ignored by Git.
- Shared view definitions MUST remain authored YAML. Personal interaction MUST NOT modify them implicitly.
- Effective settings MUST use built-in defaults, then repository configuration, then valid personal overrides. Explicit navigation requests retain their precedence over remembered view selection.
- Absent overrides MUST follow later repository-default changes. Invalid values or removed fields MUST fall back safely without inventing valid replacements.
- Reset MUST remove personal overrides for the selected instance without changing shared configuration. A custom-view host reset MUST also clear its persisted widget preferences, including unmounted widgets, without crossing host instances. A reset MUST survive reload and prevent stale browser migration from resurrecting cleared values.

### Instance identity

- A scope MUST include the view ID and concrete `ViewContext`: type, interface, group, node, or standalone. The owning checkout is implicit in the server and database; browser caches MUST still distinguish vaults.
- Node identity MUST retain canonical root or embedded-node identity without mutable byte offsets. Node paths MUST remain vault-relative. Wildcard mounts MUST resolve to concrete subjects before persistence.
- An optional authored host slot MUST distinguish separate mounts of the same view. A child widget slot MUST inherit that host identity. React mount IDs, random values, display labels, and current tab IDs MUST NOT become persistent widget identity.
- View selection MUST be target-scoped, since selection precedes choosing a view. Native variants of the same instance MUST share applicable filtering and sorting while keeping renderer-specific layout preferences separate.

### Store and API

- The server MUST own a small preferences store, its lifecycle, and a versioned SQLite migration domain using the repository's SQLite initialization, writer, transaction, and filesystem policies.
- An unavailable preference store MUST leave other web features usable, preserve its files, and report preference operations as unavailable. Index rebuild MUST NOT be offered as preference recovery.
- Reads MUST return a consistent snapshot. Atomic patches MUST change only named keys; unrelated concurrent edits MUST survive. Conflicting revisions MUST be explicit and safely rebased by the client.
- Reset and legacy import MUST be transactional. A legacy source lacking subject identity MUST be claimed by at most one concrete instance, and import MUST never overwrite previously changed or reset state.
- The public service MUST expose read, patch, reset, and import operations under `/api/v1/view-preferences`. Request scopes and values MUST be validated and bounded. JSON `null` MUST remain distinct from removal.
- Native and custom clients MUST share identity, validation, hydration, optimistic updates, failure reporting, and synchronization behavior. Writes that fail MUST remain visible and retryable rather than appearing saved.
- Clients MUST wait for initial hydration before executing a native query that depends on preferences. Defaults may render after a failed read with a visible failure and retry path.
- Matching instances MUST observe acknowledged changes from other mounted clients. Reconnection or focus MUST refresh state when live notification is unavailable.

### Remembered interactions

- Native views MUST remember columns, re-shown empty columns, density, widths, sorting, filters and presets, grouping, and Board column/lane fields.
- Table and Cards groups, Board columns and lanes, Trace bands and row trees, and stable Briefing expansion controls MUST remember explicit expansion choices. Grouping-field identity MUST prevent collapse settings from leaking to a different grouping.
- Refreshing data, saving content, filtering, and loading more rows MUST NOT reset expansion choices for unchanged groups.
- Trace MUST remember its selected row type per view instance. Overview MUST remember durable display toggles; query text and camera motion remain temporary.
- Free-text search and paging MAY remain session-local and MUST NOT be imported as durable preferences.
- Existing vault-scoped browser layout and selection preferences MUST migrate where compatible. Ambiguous legacy settings MUST NOT be copied into every invocation, and removal of old values MUST wait for acknowledged migration.

### Shared configuration and authoring

- Saving shared view configuration MUST clearly identify that it writes repository YAML. It MUST preserve unrelated authored values and existing validation/conflict behavior.
- Temporary search MUST NOT silently become shared configuration. Its inclusion, if offered, MUST be explicit. The save interface MUST make the settings being published reviewable.
- After saving, only overrides promoted into repository configuration MUST be cleared. Personal widths and expansion choices MUST remain, including when a generated view receives a new authored ID.
- The public kit MUST provide a React hook and non-React accessor that infer the current instance. Authored configuration supplies defaults; writing defaults merely because the view mounted is forbidden.
- Canonical custom-view guidance and examples MUST teach this behavior for repository-authored views, including validation, reset, stable widget slots, and failure reporting. Generated skill copies MUST come from their template sources.

### Verification

- Store tests MUST cover reopening, concurrent handles, atomic patches, revision conflicts, scope isolation, migration replay, reset tombstones, and independence from index replacement.
- Client tests MUST cover delayed hydration, malformed stored values, same-instance synchronization, different-instance isolation, failed writes and retry, and legacy migration without resurrection.
- Browser checks MUST prove collapse survives content changes and reloads, independent type/group/node contexts, reset follows shared defaults, explicit YAML save, and custom-view use through the public kit.
- Repository gates, generated-surface checks, and an independent post-build review MUST pass before PR delivery. The delivery effort owns the requested Greptile 5/5 review loop.
