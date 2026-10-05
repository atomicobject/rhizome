---
type: TechnicalSpec
id: SPEC-0096
aliases: [SPEC-0096]
summary: "Make code-mode discovery and examples sufficient for evidence projection and recovery."
spec-status: active
last-updated: 2026-09-08
---

# Code-mode evidence guidance

## Summary

Improve live contract semantics and installed examples following the fresh workflow analysis of SPEC-0095. Agents should understand field choices, preserve partial-evidence qualifications, and recover useful evidence when projection, dedupe, or a failed call interrupts the normal path.

## Goals

- Explain the semantics needed to write correct scripts through selected live discovery.
- Teach targeted rereads and distinguish full JavaScript payloads from model-visible evidence.
- Preserve successful results and source truncation information in examples.

## Requirements

- Teach that session dedupe records content delivered to JavaScript before script projection. Show targeted `files` rereads with `includeContent: true` and `dedupe: false` when a follow-up needs omitted evidence. Preserve current runtime dedupe defaults and session semantics.
- Enrich live discovery with verified field descriptions, significant defaults, conditional requirements, and operation-specific interpretation for common retrieval, ontology, recipe/view, and mutation operations. Describe the shared outcome envelope and second-argument call options once per batch, including per-call deadline, cancellation, and session behavior. Keep startup compact and complete schemas accessible; do not change execution or validation behavior to fit metadata.
- Installed examples demonstrate unsuccessful domain outcomes and thrown call failures, preserve independent successful evidence, and guard dependent work. Explain that client calls are serialized and batching reduces tool exchanges.
- Preserve bounded context text and its embedded source/truncation metadata. Project structured file content while retaining omission/truncation fields and response diagnostics/counts/continuation. Do not imply complete coverage from previews or lexical selections.

## Verification

Execute the installed examples in fresh disposable vaults. Reproduce projection followed by session dedupe and targeted recovery; inject a failed independent call and verify preserved successes; verify the dependent guard and intact context truncation markers. Compare discovery metadata to runtime behavior, including shared options and operation-specific requirements. Regenerate guidance and verify second-init identity. Complete repository gates and fresh independent review.

## Non-Goals

Projection-aware session acknowledgments, disabling dedupe globally, parallel server execution, schema compression, a new mandatory guide, a broad model evaluation campaign, main merge, or release.
