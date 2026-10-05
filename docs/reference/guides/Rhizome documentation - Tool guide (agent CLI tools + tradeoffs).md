---
type: ReferenceDoc
summary: "What each `rzm agent` retrieval surface is best at, where it is weak, and how to combine the tools into a predictable reading workflow."
reference-kind: guide
derived-from:
  - docs/reference-notes/Rhizome documentation - Tool guide (agent CLI tools + tradeoffs).md
last-verified: 2026-04-12
status: active
---

# Rhizome documentation - Tool guide (agent CLI tools + tradeoffs)

## Summary

Rhizome's agent CLI surfaces are complementary. `vault-context` orients, `semantic-query` discovers, `files` reads deterministically, and `file-context` supplies contract-heavy connective tissue. The right workflow is usually multi-step, not one command.

## Recommended pattern

1. orient with `vault-context`
2. discover likely entrypoints with `semantic-query` or metadata-only `files`
3. read selected files with `files --include-content`
4. contextualize the files or directories you will change with `file-context`

## Core tradeoff

`file-context` is optimized for constraints and linked docs, not for full code-body reading. Pair it with `files` whenever you need deterministic source review.
