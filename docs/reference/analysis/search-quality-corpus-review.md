---
type: ReferenceDoc
reference-kind: analysis
summary: "Development-only review identifies invalid precision judgments and insufficient semantic task diversity in the first search-quality corpus."
---

# Search quality corpus review

The first 334-case corpus is not yet an acceptance oracle for [[search-engine-quality|SPEC-0102]]. This review inspected only cases marked `development` in `pkg/search/qualityeval/testdata/corpus-v1.json`. It did not inspect held-out questions or results. Findings refer to corpus version 1.0.0, normalized fingerprint `abb109fb26ee1eaa5777f67e1893b127732035b41d9d3f0e97fa3f3ff9c9bfe3`.

## Findings

- `RHIZOME_REPOSITORY-STRUCTURAL_PRECISION-07`, `-09`, and `-10` request `go_to_def` for an authoritative source about ID allocation, ontology design principles, or quality gates. Their required sources are Markdown process specs and their required role is documentation. These expectations contradict US2: documentation cannot stand in for a code definition. Correct the task classification or replace these with actual symbol-definition cases. Do not weaken precision retrieval to satisfy them.
- Inspected conceptual cases `RHIZOME_REPOSITORY-CONCEPTUAL_DISCOVERY-06` through `-08` ask “how does” followed by an exact document title. Each has one grade-3 source and a generic title-derived rationale. These can test title retrieval but do not establish paraphrase-only semantic relevance or sufficient evidence coverage.
- Inspected multi-facet cases `RHIZOME_REPOSITORY-MULTI_FACET-01`, `-04`, and `-05` pair “explain” and “source for” the same title, with one documentation source. They do not test the complementary documentation, implementation, or test evidence required for a multi-source task.
- Inspected no-answer controls vary a `zxqv-no-answer` identifier. Retain gibberish regression coverage, but add plausible questions about absent topics and ambiguous targets; repeated gibberish does not represent the broader absence/confidence obligation.
- The runner maps both `polyglot-code-fixture` and `typed-note-fixture` to `testdata/integration/python-app/vault`. These are two labels on one physical corpus, not independent source collections. Split checks must account for shared sources across labels.
- `executeCorpus` initializes the run revision from `corpus.SourceRevision`, which can mislabel a changed implementation as its baseline. Record the actual execution revision and dirty-source fingerprint independently. Capture each corpus's index and actual embedding identity instead of only the first configured provider.

## Required follow-through

Preserve existing results as historical diagnostic evidence. Review a small, source-inspected development packet with correct intent, evidence locators, and specific relevance rationales before expanding judgments or tuning. Include actual structural relationships, paraphrases without title overlap, independent facets, multiple required source roles, and plausible absent-domain questions. Review unjudged retrieved sources independently of their rank.

Audit held-out construction through the generation method and split metadata without exposing its questions to tuning. If construction used the same invalid templates, prepare replacement blind cases rather than treating the old count as proof of coverage. Freeze the repaired corpus and rerun baseline and candidate on the same judgments. Keep acceptance open until the full protocol and thresholds are satisfied.

## Evaluator follow-up

The inspected evaluator's `roleCoverage` accepts any reported role when the source's overall relevance grade is at least 2. `Judgment` has no independently authored source-role binding. A temporary regression assigned implementation and test roles to one grade-3 explanatory document whose rationale explicitly excluded implementation and tests; the evaluator returned full coverage. `go test ./pkg/search/qualityeval -run TestParentReviewRoleEvidence -count=1` reproduced the failure. The temporary test was removed after the worker received the reproduction.

Require independently judged supported roles and relevant relationships or facets where the task requires them. A result's own role label cannot establish the correctness of that role. Otherwise required-role coverage and high-confidence correctness can be inflated by unsupported assignments even when overall source relevance is legitimate.
