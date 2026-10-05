---
summary: "Code anchors for answer-shaped unified search orchestration; keeps answer packet and search workflow docs attached to callers."
tags: [type/reference, subsystem/codeanchor, subsystem/search, subsystem/answer]
code-anchors:
  go:
    - label: unified-search-run
      symbol: github.com/atomicobject/rhizome/pkg/app/unifiedsearch.Run
    - label: unified-search-build-answer
      symbol: github.com/atomicobject/rhizome/pkg/app/unifiedsearch.BuildAnswer
    - label: answer-build
      symbol: github.com/atomicobject/rhizome/pkg/app/answer.Build
    - label: answer-render-text
      symbol: github.com/atomicobject/rhizome/pkg/app/answer.RenderText
    - label: presentation-default-packer
      symbol: github.com/atomicobject/rhizome/pkg/app/presentation.DefaultPacker
---

# Go anchor - Search answer orchestration

![[docs/reference/subsystems/search|Search subsystem guidance]]
Automatic context for answer-shaped search entrypoints.

Read first: [[search-answer-workflow]], [[search-quality-evaluation-corpus]], [[unified-search-answer-architecture]], [[search-diagnostics-explain-architecture]], [[answer-packet-diagnostics-contract]], [[Search - Answer engine packets]], [[Contextpack - Budget surfaces]].

Contracts:

- `pkg/app/unifiedsearch` owns orchestration and adapts ranked results into answer inputs.
- `pkg/app/answer` is pure packet assembly; no retrieval, embeddings, filesystem walks, or live ontology projection.
- `pkg/app/presentation` may do bounded reads for already-ranked packed text.
- Card hits are provenance/evidence for owning sources, not standalone answer items.
- Search-quality changes should compare answer output and raw ranked output via [[search-quality-evaluation-corpus#^SPEC-0041-US2-AC1]].
- Raw/explain diagnostics are observation-only; they must not change ranking, pack budget, warnings, or answer role selection per [[search-diagnostics-explain-architecture#^SPEC-0043-US1-AC2]] and [[search-diagnostics-explain-architecture#^SPEC-0043-US1-AC3]].
- Answer diagnostics should explain role normalization, must-read selection, specificity/decision skips, coverage, confidence, and next queries per [[search-diagnostics-explain-architecture#^SPEC-0043-US3-AC3]], [[answer-packet-diagnostics-contract#^SPEC-0050-US1-AC2]], [[answer-packet-diagnostics-contract#^SPEC-0050-US2-AC2]], [[answer-packet-diagnostics-contract#^SPEC-0050-US3-AC2]], and [[answer-packet-diagnostics-contract#^SPEC-0050-US4-AC1]].
- `pkg/app/answer` owns the detailed answer-stage trace vocabulary in [[answer-packet-diagnostics-contract]]; adapters may serialize it but must not recompute role decisions.
- CLI and MCP diagnostics should share field names or documented aliases; stage-owned traces stay where decisions are made per [[search-diagnostics-explain-architecture#^SPEC-0043-US4-AC1]] and [[search-diagnostics-explain-architecture#^SPEC-0043-US4-AC2]].
- Low-specificity semantic hits must not satisfy explanatory coverage without direct source evidence per [[search-quality-evaluation-corpus#^SPEC-0041-US3-AC1]] and [[search-quality-evaluation-corpus#^SPEC-0041-US3-AC2]].
