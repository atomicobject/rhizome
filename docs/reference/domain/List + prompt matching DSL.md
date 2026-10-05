---
type: ReferenceDoc
summary: "Boolean matching DSL for note selection by path, tag, property, and fuzzy path/name match."
reference-kind: architecture
derived-from:
  - docs/reference-notes/List + prompt matching DSL.md
last-verified: 2026-04-12
status: active
---

# List + prompt matching DSL

## Summary

Rhizome's list and prompt matchers share a small boolean DSL over note paths, tags, properties, and fuzzy path/name queries.

## Core forms

- exact path or folder prefix
- `tag:...`
- `find:...`
- `Property:Value`
- boolean composition with `AND`, `OR`, `NOT`, plus parentheses

## Contracts

- adjacent operands default to `OR`
- fuzzy matching remains Obsidian-path oriented rather than generic substring search
- the same matcher is reused in less obvious places such as graph include/exclude rules
