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

Name the documentation surfaces that must change with particular kinds of code: architecture records, API docs, runbooks, changelog, ownership. Add the checks that prove they remain retrievable.
