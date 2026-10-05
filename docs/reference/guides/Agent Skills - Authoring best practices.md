---
type: ReferenceDoc
summary: "Practical authoring heuristics for skills: concise instructions, strong descriptions, scoped freedom, and multi-model testing."
reference-kind: guide
derived-from:
  - docs/reference-notes/Agent Skills - Authoring best practices.md
last-verified: 2026-04-12
status: active
---

# Agent Skills - Authoring best practices

## Summary

Good skills are concise, discoverable, and scoped to the level of freedom the task can safely tolerate.

## Durable heuristics

- keep `SKILL.md` short and push depth into supporting files
- write descriptions around real trigger phrases, not abstract labels
- give exact instructions only when the workflow is fragile
- test on the models or runtimes the skill will actually target
