---
summary: "Defines Go code anchors for the search subsystem so callers (via call-sites) automatically pull the right docs in file_context."
tags: [type/reference, subsystem/codeanchor, subsystem/search]
code-anchors:
  go:
    - label: search-planner
      symbol: github.com/atomicobject/rhizome/pkg/search/planner.Planner
    - label: search-service
      symbol: github.com/atomicobject/rhizome/pkg/search.Service
    - label: weights-for-intent
      symbol: github.com/atomicobject/rhizome/pkg/search/planner.weightsForIntent
    - label: knowledge-handle
      symbol: github.com/atomicobject/rhizome/pkg/search/knowledge.Handle
    - label: knowledge-parse
      symbol: github.com/atomicobject/rhizome/pkg/search/knowledge.ParseHandle
    - label: graphdb-doc-scores
      symbol: github.com/atomicobject/rhizome/pkg/search/graphdb.ComputeDocScores
    - label: graphdb-anchor-pagerank
      symbol: github.com/atomicobject/rhizome/pkg/search/graphdb.ComputeAnchorPageRank
    - label: graphalg-pagerank
      symbol: github.com/atomicobject/rhizome/pkg/search/graphalg.PageRank
    - label: graphalg-hits
      symbol: github.com/atomicobject/rhizome/pkg/search/graphalg.ComputeHITS
    - label: graphalg-labelprop
      symbol: github.com/atomicobject/rhizome/pkg/search/graphalg.LabelPropagation
    # Retrieval package
    - label: retriever-vector
      symbol: github.com/atomicobject/rhizome/pkg/search/retrieval.VectorRetriever
    - label: retriever-auto-expand
      symbol: github.com/atomicobject/rhizome/pkg/search/retrieval.AutoExpandRetriever
    - label: retriever-graph
      symbol: github.com/atomicobject/rhizome/pkg/search/retrieval.GraphRetriever
    - label: retriever-code-anchor-notes
      symbol: github.com/atomicobject/rhizome/pkg/search/retrieval.CodeAnchorNotesRetriever
    - label: retriever-code-anchor-refs
      symbol: github.com/atomicobject/rhizome/pkg/search/retrieval.CodeAnchorRefsRetriever
    # Semantic package
    - label: semantic-searcher
      symbol: github.com/atomicobject/rhizome/pkg/search/semantic.Searcher
    - label: semantic-chunk-builder
      symbol: github.com/atomicobject/rhizome/pkg/search/semantic.ChunkBuilder
    # Core types
    - label: search-candidate
      symbol: github.com/atomicobject/rhizome/pkg/search.Candidate
    - label: search-merge-candidate
      symbol: github.com/atomicobject/rhizome/pkg/search.MergeCandidate
---

# Go anchor - Search subsystem

![[docs/reference/subsystems/search|Search subsystem guidance]]
Code anchors for the search pipeline. Callers of these symbols automatically get docs in `file_context`.

**Read these first**: [[Search (Hub)]] → [[Search - Intent and weight tuning]] → [[Search - Seed expansion strategies]] → [[search-diagnostics-explain-architecture]]

Diagnostics contract: planner, signal-profile, retriever, merge, rollup, shaper, and pack traces are additive observability only. Do not change retriever selection, ranking, result inclusion, pack budget, or warning visibility to make an explain payload easier; see [[search-diagnostics-explain-architecture#^SPEC-0043-US2-AC1]], [[search-diagnostics-explain-architecture#^SPEC-0043-US2-AC2]], and [[search-diagnostics-explain-architecture#^SPEC-0043-US3-AC2]]. Keep CLI and MCP names aligned with [[search-diagnostics-explain-architecture#^SPEC-0043-US4-AC1]].
