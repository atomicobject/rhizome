---
type: ReferenceDoc
reference-kind: analysis
summary: "Source-inspected development search tasks with explicit intent, source grades, and evidence boundaries."
---

# Curated development search review

This small diagnostic packet supports the repairs in [[search-quality-corpus-review]]. It is development material, not blind evaluation or proof of the corpus-size and diversity requirements in [[search-engine-quality]]. Sources were inspected in the current working tree before running these queries. Revalidate locators when freezing the repaired corpus. Related tasks below belong to the same source cluster and must not be split across development and held-out sets.

Grades use 3 for direct task evidence, 2 for useful supporting evidence, 1 for incidental relevance, and 0 for irrelevant material. Unlisted sources remain unjudged until inspected; their absence from this packet does not imply grade 0. The implementation and tests include new work, so historical baseline runs require a matching indexed source snapshot and explicit missing-source accounting.

## D1: Canonical code definition

Query: `MergeCandidate`. Intent: `go_to_def`. Resolve in `pkg/search/merge.go`.

Grade 3: the function declaration and body at `pkg/search/merge.go:16`. It combines candidates and calls bounded evidence merging. The canonical definition is the required result. Documentation and test call sites may be supporting context, but cannot satisfy this precision task. Another function with a similar name is not a substitute.

## D2: Repeated observations

Query: “Why doesn't seeing the same retrieval fact twice make a result look more convincing?” Intent: conceptual discovery.

Grade 3 implementation evidence: `pkg/search/merge.go`, functions `dedupeEvidenceFacts` and `mergeEvidenceBounded`. The implementation groups facts by semantic identity before applying the bounds; operational lane rank is not an independent fact. Grade 3 test evidence: `pkg/search/merge_test.go:12`, `TestMergeEvidenceIdempotent`, which merges the same lexical observation and asserts one retained item with a ranking score of 0.6. Both implementation and test roles are required to answer the question and substantiate the behavior.

The separate-facet test at line 22 is grade 2: it establishes that distinct facts survive, but does not alone prove duplicate observations cannot inflate support. A generic discussion of relevance scoring is at most incidental unless it explains this behavior.

## D3: Tests for provenance stability

Query: “Find tests proving that receiving equivalent observations in a different order preserves where they came from.” Intent: tests for code. Seed: `pkg/search/merge.go`.

Grade 3: `pkg/search/merge_test.go:67`, `TestMergeEvidenceSameFactRanksAndProvenanceAreDeterministic`. It permutes three lanes, asserts the same retained observation, and checks the consolidated source list and per-lane ranks. Grade 2: `TestMergeEvidencePermutationProducesIdenticalRetainedEvidence` at line 29, which checks order invariance but does not explicitly assert the consolidated provenance fields. Production code cannot count as a primary test result.

## D4: Smaller discovery responses

Query: “Can I request only a tool's arguments without changing its client contract identity or losing execution guidance?” Intent: conceptual discovery.

Grade 3 implementation: `pkg/app/agentcode/surface.go:62`, `DescribeResponse.SelectSchemas`, which copies the operation slice and removes the unwanted schema while retaining the contract and response guidance. Grade 3 test: `pkg/app/agentcode/discovery_test.go:34`, `TestDescribeSchemaSelectionPreservesContractAndGuidance`, which checks hash equality, invocation/outcome preservation, interpretation presence, wire omission, reduced size, and nonmutation. Both roles are required. The alias-selection tests alone are incidental to this question.

## D5: Independent discovery facets

Queries: “How are JavaScript method names resolved to tool operation names?” and “What proves that changing spelling reuses the same generated client?” Intent: multi-facet discovery.

Grade 3 for the resolution facet: `pkg/app/agentcode/surface.go:141`, `normalizeSelection`, whose alias table comes from operation descriptors and maps both canonical and camel-case spellings before duplicate detection. Grade 3 for the generated-client facet: `pkg/app/agentcode/discovery_test.go:24`, `TestGenerateMethodSelectionUsesCanonicalArtifact`, which asserts equal artifact hashes and reuse. `TestDescribeAcceptsExactMethodNames` at line 10 is grade 2 supporting evidence for accepted aliases and duplicate rejection, but does not prove client reuse. Require implementation and test roles, with coverage recorded independently for each facet.

## Incorporation rules

Keep canonical-definition and tests-for-code judgments restricted to their requested result kinds. Do not convert these tasks into title-derived prose queries. Store function-level locators and the specific rationale above rather than a file-start placeholder. Review additional retrieved sources without using their rank as the relevance label. These five tasks cover only two related code areas; expand to independently authored source clusters and physical corpora before making acceptance claims. No absent-domain or ambiguous-target control has been certified by this packet.
