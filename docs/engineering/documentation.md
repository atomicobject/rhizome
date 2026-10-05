# Documentation

Consulted when deciding which documents change with a change. Used during specification, planning, implementation, and closure.

## Defaults

Document durable decisions, boundaries, operating assumptions, and non-obvious behavior next to their owner. When a subsystem's purpose, entry points, or invariants change, its overview or `CONTEXT.md` changes in the same change set. Prefer a short rationale to a restatement of the code.

Where a document explains code, bind it to that code with the repository's supported references so retrieval and validation can find it. Rationale that would otherwise be rediscovered by archaeology belongs in the document, not the pull request.

## Review questions

- Can a contributor reach the governing explanation from the affected code or task?
- Did an API, procedure, or durable decision change without its owning document changing?
- Does the new link or binding actually resolve?

## Team extensions

Subsystem notes under `docs/reference/subsystems/` change in the same change set as the invariant they describe, with `last-verified` bumped. A package's `CONTEXT.md` changes when its entry points or invariants change. Init template sources are documented in the templates, never in generated `.agents`, `.claude`, `.codex`, or `.cursor` copies. RHIZOME.md and managed-block behavior is documented in `docs/rhizome-md-templates/*`; repo-specific preferences live in the unmanaged part of `AGENTS.md`. Hubs under `docs/hubs/` index durable topics; specs and efforts carry the delivery contract and record.
