---
name: code-intel-subsystem
description: Use when implementing, modifying, or reviewing code anchors, coderefs, or code pattern scanning under pkg/anchors, pkg/app/codeintel, pkg/vault/coderefs, or pkg/vault/codepatterns — including new language support. Loads code-intel constraints and review checklist.
---

# Code intel subsystem

## Goal

Change or review the code intelligence layer (anchors, coderefs, codepatterns, codeintel ingest) without breaking existing anchor matches, leaking false-positive refs, or regressing the batch indexing pipeline.

## First move

Read `docs/reference/subsystems/code-intel.md` first; its constraints are normative for this subsystem. The CONTEXT.md files in `pkg/anchors`, `pkg/vault/coderefs`, and `pkg/app/codeintel` carry the load-bearing invariants in compressed form.

## Rules

1. **Directionality is fixed**: anchors are note→code (frontmatter); coderefs are code→note (comments/docstrings). Don't blur the seam — anchor declarations enter only via `ExtractAnchorDeclarations`.
2. **New language support follows the polyglot fixture pattern**: extend `testdata/integration/python-app/vault/` with a language subtree (one vault, multiple languages), a dedicated anchor note under `vault/notes/code-anchors/`, code triggering each supported anchor kind (minimum symbol + calls; annotation/baseClass/dir where applicable), coderef files exercising wikilinks + `@mentions` + pitfalls (emails, strings), and integration tests under `tests/integration/<lang>/` asserting matches at both definition site and call sites.
3. **FQN shape is a contract**: anchors match exact FQN strings the indexer emits. Any change to FQN production requires an `IndexerVersion` bump in `pkg/anchors/indexer.go` and a `rzm index --rebuild` migration story.
4. **Stay on the batch path**: no per-file store lookups or synchronous semantic submission on the hot ingest path; when `ApplyCodePersistenceBatch` is available it owns summary/meta durability.
5. **Coderef precision over recall**: scanner must keep rejecting emails and string-literal `@`s; rewriter regexes stay stricter than scanner; web/template files remain coderef-only (no anchors).
6. **Paths via `pkg/paths`**: indexes store vault-root-relative paths only; no ad hoc `filepath.Abs/Rel`.
7. **Debug before guessing**: `rzm code anchors explain <file>` for match rationale, `rzm code symbols <file>` for actual FQNs; suspect a missed rebuild before suspecting the matcher.

## Pre-handoff checklist

- [ ] `go test ./pkg/anchors/... ./pkg/vault/coderefs/...` (plus `./pkg/app/codeintel/...` / `./pkg/vault/codepatterns/...` if touched)
- [ ] `go test -tags=integration ./...`
- [ ] Indexer behavior changed → version constant bumped; verified against a real repo with `rzm index --rebuild`
- [ ] New anchor kind or language → fixture + integration assertions cover definition site and call sites
- [ ] Docs updated: nearest CONTEXT.md and `docs/reference/subsystems/code-intel.md` if constraints shifted
