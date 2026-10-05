---
type: ReferenceDoc
reference-kind: analysis
summary: "Reproduction manifest for the frozen SPEC-0102 search corpus, development baseline, index identity, and qualified measurements."
last-verified: 2026-09-12
---

# Search engine quality manifest

## Frozen inputs

- Implementation baseline: `0049fa9d1bca65d511c04094a143c87889e45b86` (`rzm version v0.50.5`). The working checkout contained authorized search effort artifacts and unrelated concurrent agent-surface work, so the exact dirty-path list remains available through Git rather than being represented as a clean revision.
- Corpus: `pkg/search/qualityeval/testdata/corpus-v1.json`, SHA-256 `c097b2c299c046c051f637d5828938e49fbfa3e54029b24bc17e0f223ce7dd12`. Its normalized evaluation fingerprint is `abb109fb26ee1eaa5777f67e1893b127732035b41d9d3f0e97fa3f3ff9c9bfe3`.
- Corpus size: 334 cases, split into 178 development and 156 held-out cases. The four corpora contain 91 Rhizome repository, 82 polyglot fixture, 81 typed-note fixture, and 80 prose-vault cases.
- Baseline result artifact: `baseline-development-lexical.json`, SHA-256 `aff8d4ae2bbf7e46a41991ca19c1dc18310a757a525329a936f04423b63fe74d`.
- Candidate result artifact after the first integrity/navigation pass: `candidate-development-lexical.json`, SHA-256 `fcd5f0dd0a76ba7a6c3860c8d5828f062404f813fc9a0194c20881b2037f48d3`.
- Candidate result artifact after correcting ambiguous and duplicate-title navigation judgments: `candidate-development-lexical-v2.json`, SHA-256 `eb1b16c0fea2d07805bad735d1d7ca39989101123438483e14ebd2152fa68a7c`.
- Qualified live-provider Rhizome development slice: `candidate-development-rhizome-live.json`, SHA-256 `e2c624382ab2433202dff01aaffe382af015858c135a1da723457fb4fa72876b`.

The held-out results were not inspected while development changes were being selected.

## Machine and dogfood index

- macOS Darwin 27.0.0 on Apple M4 Pro, arm64, 48 GiB RAM.
- Go `go1.24.2 darwin/arm64`.
- Dogfood index: 2,331 indexed source files, 26,095 searchable chunks, 34,974 code anchors, and 6,453 document sections.
- Intel indexer version `v1.11.0`, scope hash `fc399606802477c00c0404045a55159a6b1397dac7f6336c7740f20a4b425d84`.
- Note and code vector metadata both report Voyage `voyage-4-lite`, 1,024 dimensions, committed visible generation 2. Code fingerprint version is 3. Query tests used the checkout's configured provider endpoint.
- `rzm agent current-user show` returned `missing_current_user`; the user's chat approval is recorded without an inferred Person identity.

The checked-in prose fixture configuration now supplies a stable local vault definition. Its index completed all writes but returned a duplicate validation-diagnostic key during final ownership reconciliation. The lexical candidate run could read the committed state. This is a fixture-index qualification, not a successful clean index build.

## Reproduction commands

From the repository root:

```sh
go test ./pkg/search/qualityeval ./tools/searchquality

go run ./tools/searchquality \
  -corpus pkg/search/qualityeval/testdata/corpus-v1.json \
  -execute -vault . -fast -split development \
  -run-out docs/reference/analysis/assets/search-engine-quality-2026-09-12/candidate-development-lexical.json \
  > docs/reference/analysis/assets/search-engine-quality-2026-09-12/candidate-development-lexical-report.json

go run ./tools/searchquality \
  -corpus pkg/search/qualityeval/testdata/corpus-v1.json \
  -run docs/reference/analysis/assets/search-engine-quality-2026-09-12/candidate-development-lexical.json \
  -baseline-run docs/reference/analysis/assets/search-engine-quality-2026-09-12/baseline-development-lexical.json \
  -split development -format markdown

go run ./tools/searchquality \
  -corpus pkg/search/qualityeval/testdata/corpus-v1.json \
  -execute -fast -split development \
  -run-out docs/reference/analysis/assets/search-engine-quality-2026-09-12/candidate-development-lexical-v2.json \
  > docs/reference/analysis/assets/search-engine-quality-2026-09-12/candidate-development-lexical-v2-report.json
```

`-fast` disables provider, graph, and reference lanes. It is deterministic correctness and lexical-quality evidence. It is not semantic relevance evidence.

## Development result

The original baseline and first candidate use the superseded `9cc7...` corpus fingerprint. They remain historical reproduction evidence and cannot be compared directly with the corrected corpus. The correction marks the two equally exact `Search engine excellence` sources as an ambiguous-target control and gives four duplicate-basename navigation cases explicit file seeds. It changes evaluation identity rather than hiding a returned failure.

On the corrected development corpus, the deterministic v2 candidate reports nDCG@10 0.518, required-source recall@20 0.637, navigation rank 1 of 0.967 (29/30), and navigation rank 3 of 1.000 (30/30). Must-read precision is 0.204, required-role coverage is 0.647, and 949 top-ten sources remain unjudged. High confidence remains withheld for every case. These are incomplete development results below SPEC-0102 acceptance.

The first candidate pass raised navigation success at rank 1 from 0.581 to 0.839 and rank 3 from 0.774 to 0.968. nDCG@10 rose from 0.464 to 0.506. Required-source recall@20 remained 0.637. The navigation change comes from using authoritative indexed note titles and preserving exact-title eligibility after grouping. These results still do not meet SPEC-0102 acceptance targets.

The candidate still has 950 unjudged sources in top-ten pools. Must-read precision is 0.202 and required-role coverage is 0.647. This makes answer-quality and broad relevance conclusions incomplete. High confidence is withheld for all development cases because the current assessed evidence cannot support the contract. That avoids unsupported claims but is not calibration evidence and remains below the required 50-case combined minimum. The adjacent comparison and JSON reports retain every denominator and failure rather than treating missing or unjudged results as passes.

The live Voyage run covers 47 Rhizome development cases. It achieved nDCG@10 0.202, recall@20 0.512, navigation rank 1/rank 3 of 0.714/0.857, and 331 unjudged top-ten sources. Its observed end-to-end p50/p95/max were 692/746/1,307 ms with no request errors. This is a sequential, warm-process sample below the required 200 observations and excludes the other corpus families, so it is operational evidence rather than a performance-gate pass. The run artifact originally reported configured dimensions as zero because automatic dimensions were omitted from YAML; the committed index metadata supplies the actual 1,024 dimensions recorded above.

Live repeated `search engine` queries exposed non-identical Voyage query vectors for the same text while the index generation remained fixed. The Phase 6 continuation now carries the bounded exact first-page query vectors. A five-page run across separate CLI processes returned 176 unique sources in pages of 40, 40, 40, 40, and 16 with no duplicates or stale error. The resulting cursor was approximately 17.2 KB with one shared 1,024-dimensional code/note vector.

## Engineering gates

- `go test -race ./pkg/search/... ./pkg/app/unifiedsearch/... ./pkg/app/answer/... ./pkg/app/mcp/... ./pkg/app/web/... -count=1`: passed.
- `go test -tags='fts5 integration' ./tests/integration/python -run TestSearchIntents_P3 -count=1`: passed.
- Focused Playwright search suite: 4 of 4 cases passed, including a real-backend journey for API continuation, UI search, type filtering, and source opening. The broader real-backend matrix still lacks cancellation and missing-embedding cases.
- `make build`: passed.
- `./scripts/rzm validate`: zero issues.
- `./scripts/rzm validate frozen-scope-drift`: only the two recorded pre-existing SPEC-0080 findings.
- `make check`: lint, vet, web tests, type checks, integration tests, and all other packages passed. The command failed `TestSyntaxNeutralConsumersDoNotSelectConcreteProviders`, which reports two concrete-provider selections in unchanged `pkg/app/cli/init/agent_surfaces.go`. The failure reproduces in isolation and is outside this search effort.
