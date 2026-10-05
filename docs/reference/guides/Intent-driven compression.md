---
type: ReferenceDoc
summary: "How retrieval compression uses caller intent to preserve the most relevant content when context budget is tight."
reference-kind: guide
derived-from:
  - docs/reference-notes/Intent-driven compression.md
last-verified: 2026-09-22
status: active
---

# Intent-driven compression

## Summary

When retrieval output would exceed budget badly enough, Rhizome can ask an LLM to compress content according to the caller's actual task intent instead of bluntly truncating.

## Contracts

- compression requires `compression.enabled: true` and a user-supplied provider key; key availability alone never enables it
- setup does not offer compression or bundle Cerebras credentials
- packing runs first; compression only activates once omission would be substantial
- the freeform `intent` is the prioritization hint for compression
- compression failures fall back to normal truncation
- compressed output is computed per request; production runtimes do not memoize it
