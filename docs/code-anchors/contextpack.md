---
summary: "Code anchors for contextpack; embeds budget and answer-shaped search docs for callers of the packing system."
code-anchors:
  go:
    - label: contextpack-pack
      ref: github.com/atomicobject/rhizome/pkg/app/contextpack.Pack
    - label: contextpack-pack-intent
      ref: github.com/atomicobject/rhizome/pkg/app/contextpack.PackWithIntent
    - label: contextpack-piece
      ref: github.com/atomicobject/rhizome/pkg/app/contextpack.Piece
    - label: contextpack-compressor
      ref: github.com/atomicobject/rhizome/pkg/app/contextpack.Compressor
---

![[pkg/app/contextpack/CONTEXT]]
![[agent-surface]]
![[Contextpack - Budget surfaces]]
![[unified-search-answer-architecture]]
![[Search - Answer engine packets]]

Contextpack is a caller-budgeted packing boundary for agent/file/search outputs. Search packed output must honor the query spec budget and must not inherit broad agent defaults accidentally.
