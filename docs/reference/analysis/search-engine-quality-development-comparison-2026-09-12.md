---
type: ReferenceDoc
reference-kind: analysis
summary: "Frozen search quality baseline and candidate comparison."
last-verified: 2026-09-12
---

# Search quality comparison

This comparison is historical evidence for the superseded corpus fingerprint below. The corrected corpus changes ambiguous-target and duplicate-title navigation cases, so its v2 report is published separately and must not be compared numerically with this table as if the judged inputs were identical.

Corpus fingerprint: `9cc7d7faa7a07ece89464b6ffcb2445dd6e4db931da11bbec3a4d66a8ceb4666`
Split: `development`

| Run | Cases | nDCG@10 | Recall@20 | Nav@1 | Nav@3 | Must-read precision | Role coverage | High-confidence precision | High-confidence coverage | Unjudged top 10 |
| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| baseline | 178 | 0.464 (164) | 0.637 (164) | 0.581 (31) | 0.774 (31) | 0.198 (164) | 0.647 (136) | 0.200 (15) | 0.091 (164) | 952 |
| candidate | 178 | 0.506 (164) | 0.637 (164) | 0.839 (31) | 0.968 (31) | 0.202 (164) | 0.647 (136) | 0.000 (0) | 0.000 (164) | 950 |
