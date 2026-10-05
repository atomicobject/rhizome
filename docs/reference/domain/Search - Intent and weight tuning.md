---
type: ReferenceDoc
summary: "Defines the main search intent modes, the evidence dimensions they tune, and the ranking biases each mode applies."
reference-kind: architecture
derived-from:
  - docs/reference-notes/Search - Intent and weight tuning.md
last-verified: 2026-04-24
status: active
---

# Search - Intent and weight tuning

## Summary

Rhizome search intent changes ranking behavior. Each intent reweights the same core evidence dimensions so the same retrieval substrate can serve discovery, docs-for-code, code-for-docs, fetch-like lookups, and subsystem overview work.

## Evidence dimensions

- vector similarity
- lexical / FTS match
- graph proximity and authority
- code/doc reference signals
- recency
- query specificity: deterministic overlap between the normalized query frame and candidate path/title/symbol/FQN/snippet metadata

Primary semantic matches are source-owned evidence, not a separate result class. Bounded factual identity/context helps retrieval find an owner; ranking and answer assembly preserve that owner as the thing to read.

## Intent model

- `search`: balanced default
- `overview` / `subsystem_overview`: broader, doc-heavier onboarding shapes
- `docs_for_code` / `code_for_docs`: strong reference-signal bias
- `related_to_seed`: graph- and ref-heavy expansion around explicit seeds
- fetch-like intents such as `go_to_def`, `find_usages`, `callers`, `callees`, `tests_for_code`, `implementers`, `overrides`, and `imports`: precision-first, code-leaning behavior

For no-seed broad search, graph/ref auto-expansion is gated by direct-match quality so weak semantic-only hits do not become noisy implicit seeds.

## Core invariant

Intent changes ranking and packing policy, not the underlying note/code identity model. If a new mode is added, it should still map onto the same evidence vocabulary rather than invent a second ranking language.
