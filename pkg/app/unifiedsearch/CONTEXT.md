## pkg/app/unifiedsearch

Shared application search orchestration for CLI and adapters: provider/store bootstrap, effective profiles, seed handling, intent inference, source assessment, answer assembly, and continuation. The sibling `pkg/app/searchengine` package owns the cycle-free planner/ranker/service execution seam used by both this package and MCP.

- **Entry points**: `Execute` (every caller: CLI, MCP, HTTP, agent code mode, GraphQL `NoteSearcher`), `Run`, `searchengine.Execute`, `ResolveEffectivePolicy`, `ResolveSeedHandles`, `NormalizeQueryInputs`, `MergeSeedTokens`
- **Key invariant**: `cmd` owns terminal rendering; this package owns retrieval planning, ranking inputs, and conversion into `pkg/app/answer` packets. Adapters hydrate only the page `Execute` returns.
- **Provider lifetime**: factory-created providers register optional closers before store setup and close on every failed run or successful result cleanup. Matching code/note configurations reuse one provider before a second factory is called. Injected providers remain borrowed.
- **Runtime ownership**: `Run` opens providers and stores directly from `Options` or uses injected ones. This package must not import `oneshotruntime`, `bootstrap`, or `mcp` (they depend on the MCP adapter, which calls `Execute`).
- **Intent**: `Execute` uses the explicit intent input, else a mode shared by all query inputs, else `search`; `Result.Intent` is the intent that ran after target repair and is what `ApplicationResult.Request.Intent` reports.
- **Key invariant**: multiple queries should stay focused facets of one task; fan out bounded work, then merge into one answer rather than encouraging a single compound prompt.
- **Key invariant**: canonical NodeRef fields are adapted as source provenance after ranking; answer assembly must not do retrieval or live ontology projection.
- **Diagnostics**: raw/explain state must describe the same run inputs; adapters assemble stage-local trace data without changing ranking, warnings, packing, or answer selection.
- **Continuation**: membership identity excludes page and body presentation budgets, while cursor validation separately binds committed composite index generation, candidate bound, and canonical ordered-source digest. Generation reads bracket query execution so indexing cannot publish a torn result as current. The opaque v2 cursor uses lossless zlib compression before base64 encoding, preserving exact query-vector values while reducing response-budget overhead. Existing uncompressed v2 tokens remain readable. Encoded input is capped at 1 MiB and decompressed JSON at 4 MiB to support the permitted 16-query, two-vector, 4096-component shape; malformed or oversized compressed payloads fail before retrieval.
- **Assessment**: adapters consume typed target resolution, primary/supporting eligibility, relevance, roles, availability, and the pure answer response. A populated role or nearest-neighbor score alone cannot establish high confidence.

### Deep docs

- [[search-answer-workflow]]
- [[search-quality-evaluation-corpus]]
- [[unified-search-answer-architecture]]
- [[search-diagnostics-explain-architecture]]
- [[Search (Hub)]]
- [[Search - Answer engine packets]]
