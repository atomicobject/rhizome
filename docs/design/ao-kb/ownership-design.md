# Source ownership and mutation admission

SPEC-0133 R5 protects authored files by host source path, independently of
provider, projection, schema, selected product, and index freshness.

## Declaration

Use a committed `.rhizome/source-ownership.yml` alongside existing repository
configuration. A separate document lets starters establish protection before
publishing bindings without loading or rewriting runtime/provider configuration.

```yaml
sources:
  - owner: AO KB
    reason: Managed by AO KB. Change it with the AO KB skills.
    paths:
      - knowledge/**
      - products/*/model/**
```

Paths are vault-relative exact paths or doublestar globs, with forward slashes.
Owner, reason, and at least one valid path are required. There is no AO-specific
core branch. All products' known authored sources belong here, even when only
one product is projected. Starter setup writes this before bindings or indexing.
The document is read directly for admission and capability requests; refresh
never derives, replaces, or removes it. Invalid ownership configuration fails
closed. Explicitly editing/removing this configuration is the ejection mechanism.
Native note mutation cannot edit the declaration itself.

## Existing choke points

These line references describe the inspected baseline:

- `pkg/ontology/edit_session_stage.go:171`: every field, body, collection,
  identifier/block-ID, and whole-file edit enters `EditSession.stage`.
- `pkg/ontology/edit_session_preview.go:87`: ontology commits open a shared
  source writer; `edit_source_transaction.go:18` submits all changed files.
- `pkg/sourcetxn/transaction_stage.go:12`: complete original/final file set is
  admitted before journal, backup, stage, or parent creation.
- `pkg/sourcetxn/transaction_commit.go:9`: publication revalidates prepared
  witnesses, but currently has no ownership recheck.
- `pkg/app/operations/note_namespace_plan.go:53`: graph-safe note moves collect
  endpoint changes and backlink rewrites into one validation transaction.
- `pkg/app/operations/heading_rename.go:57`: heading rename stages all source
  and inbound-link changes in an ontology edit session.
- `pkg/validate/source_engine_stage.go:5` and `namespace_prepare.go:14`:
  repair/fix and move plans use the same source transaction preparation.
- `pkg/vault/coderefs/rewrite.go:53`: code-reference rewrites write directly;
  the move workflow currently invokes these as optional postcommit effects.
- `pkg/app/operations/properties_mutate.go:43`, `tags_mutate.go:34`, and
  `delete.go:19`: direct CLI metadata/tag/delete writers need admission too.
- `pkg/notemeta` projects raw facts and does not author note bytes.

## Shared checks and atomic refusal

A small format-independent `sourceownership` package reads the declaration and
checks canonical host paths. It supplies the same owner/reason to all callers.

`EditSession.stage` checks before accepting an operation, including malformed or
untyped source targets. Shared transaction preparation checks every affected
original and destination path before writing artifacts. Shared publication checks
current ownership again before installing any prepared source. Commit therefore
refuses edits staged before a later ownership declaration. Recovery still owns
restoring interrupted transactions; ownership does not grant permission to
abandon partial writes or overwrite later external changes.

Graph operations admit their full discovered write set. Code-reference rewrites
must be planned/admitted before namespace publication, rather than discovering a
refusal after moving the note. Direct metadata/tag writers likewise preflight
all changed files before publishing, and delete checks its canonical target.
CLI, agent, and MCP adapters reuse these operations or edit sessions, avoiding
surface-specific ownership rules. Unrelated Markdown retains existing behavior.

## Capability reporting and UI

Workspace construction captures one ownership policy per read scope and overlays
its node and field capabilities. Owned nodes advertise all native edit capabilities as false and
carry `readOnlyReason`; fields remove `writeOperation` and carry the same reason.
The web workspace API and typed GraphQL workspace selection use this overlay,
including nested body workspaces. Browser edit mode and body controls honor the
node reason, and the existing property editor honors field capabilities.
Capability reporting is advisory to the UI; server admission is authoritative.
Captured policies retain load errors, so unreadable/malformed declarations fail
closed. Staging and commit capture separate policies; commit sees later changes.

## Observable verification

Cover malformed/fallback and unmatched-type sources, normal unrelated editing,
current reason reporting, staging refusal, commit after ownership changes,
whole-operation graph move refusal without source byte changes, direct CLI/MCP
mutation routes, metadata/coderef refusal, and disabled browser editing. Run
focused Go and web tests, repository gates, and the requested E2E suite.

## Bypass audit and integration notes

- `pkg/app/operations/create.go` is the content-bearing Obsidian URI route.
  It admits both the supplied path and Markdown destination, resolves physical
  parent aliases even for new files, and dispatches a canonical destination.
  Refusal tests cover append, overwrite, and creation without constructing or
  executing a URI. Open/search/daily URIs launch the external editor without
  supplying authored content, which remains permitted by R5.
- Native agent code-mode moves and heading changes delegate to the same
  application operations. MCP heading changes use those operations; node-link
  fixes use `EditSession.EnsureBlockID` and the shared writer. SDK generation
  writes generated agent artifacts, rather than authored knowledge records.
- The desktop bridge handles runtime/setup/configuration. Its note editor uses
  the same web API; the native bridge has no separate authored-note write path.
- Coderef patches now publish inside the namespace transaction, including when
  later projection refresh fails. They are no longer optional postcommit writes.
- Lane A should retain the workspace ownership overlay when changing workspace
  construction. Starter setup must write the declaration before bindings,
  covering known authored sources in unselected products as well.
