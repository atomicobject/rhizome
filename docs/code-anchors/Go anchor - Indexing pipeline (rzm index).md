---
summary: "Code anchors for the end-to-end indexing pipeline (batch + watcher-runtime live updating). Keep this note compact so indexing contracts surface without flooding file-context."
tags:
  [
    type/reference,
    subsystem/codeanchor,
    subsystem/indexing,
    subsystem/intel,
    subsystem/embeddings,
    subsystem/cache,
  ]
code-anchors:
  go:
    - label: index-cli
      glob: cmd/index*.go
    - label: unified-core
      symbol: github.com/atomicobject/rhizome/pkg/app/indexing.RunUnifiedCore
    - label: semantic-runtime-policy
      symbol: github.com/atomicobject/rhizome/pkg/app/semanticruntime.PolicyFor
    - label: semantic-runtime-lane
      symbol: github.com/atomicobject/rhizome/pkg/app/semanticruntime.Runtime.EnsureLane
    - label: queued-writer
      symbol: github.com/atomicobject/rhizome/pkg/app/indexwriter.New
    - label: queued-writer-loop
      symbol: github.com/atomicobject/rhizome/pkg/app/indexwriter.Writer.run
    - label: timings-summary
      symbol: github.com/atomicobject/rhizome/pkg/indexingperf.Collector.RenderSummary
    - label: auto-vacuum-policy
      symbol: github.com/atomicobject/rhizome/pkg/app/indexing.vacuumStatsNeedReclaim
    - label: vacuum-stats
      symbol: github.com/atomicobject/rhizome/pkg/anchors/sqlite.Store.VacuumStats
    - label: file-pipeline
      symbol: github.com/atomicobject/rhizome/pkg/app/indexingpipe.ProcessFiles
    - label: cli-index-lock
      symbol: github.com/atomicobject/rhizome/pkg/app/indexing.TryAcquireIndexLock
    - label: index-lock
      symbol: github.com/atomicobject/rhizome/pkg/vault/indexlock.TryAcquire
    - label: index-lock-priority
      symbol: github.com/atomicobject/rhizome/pkg/vault/indexlock.RequestPriority
    - label: index-lock-priority-check
      symbol: github.com/atomicobject/rhizome/pkg/vault/indexlock.CheckPriority
    - label: index-lock-yielding-heartbeat
      symbol: github.com/atomicobject/rhizome/pkg/vault/indexlock.StartYieldingHeartbeat
    - label: background-index
      symbol: github.com/atomicobject/rhizome/cmd.runBackgroundIndex
    - label: derived-work-scheduler
      symbol: github.com/atomicobject/rhizome/pkg/app/bootstrap.derivedScheduler.execute
    - label: structural-publication
      symbol: github.com/atomicobject/rhizome/pkg/app/indexcore.Publish
    - label: live-runtime-leader-work
      symbol: github.com/atomicobject/rhizome/pkg/app/bootstrap.LiveRuntime.startLeaderWork
    - label: watcher-runtime-indexing
      glob: cmd/serve.go
    - label: code-doclinker
      glob: cmd/code_doclinker.go
    - label: graphdb-doc-scores
      symbol: github.com/atomicobject/rhizome/pkg/search/graphdb.ComputeDocScores
    - label: graphdb-anchor-pagerank
      symbol: github.com/atomicobject/rhizome/pkg/search/graphdb.ComputeAnchorPageRank
---

# Go anchor - Indexing pipeline (rzm index)

Must preserve:

- Batch `rzm index` and live watcher refresh share staged correctness barriers: discovery, queued writes, semantic lanes, primary ontology sync, and graph recompute.
- SQLite writes stay single-lane and backpressured; concurrency belongs in discovery, parsing, embedding, and queue production.
- Semantic provider work uses `semanticruntime` compatible lanes; do not add ad hoc provider batchers for code, notes, ontology bodies, or intent exemplars.
- `rzm index` UX must keep scoped refresh/rebuild behavior observable through timings/progress without hiding skipped or locked work.
- Default unified `--timings` owns stable phase labels; code-only timing at least exposes maintenance as explicit write-locked DB ops.
- Routine maintenance is cheap (`ANALYZE`, `PRAGMA optimize`, checkpoint). Full `VACUUM` is manual or freelist-gated; never reintroduce elapsed-time/WAL-size auto-vacuum triggers.
- Primary ontology chunks remain source-owned; sync fresh projection before embedding and prune obsolete derived rows during migration.
- Primary-chunk freshness is part of semantic convergence; verify source/context/format/provider fingerprints and run a semantic rebuild before blaming ranking/answer code for missing source-owned context.
- Cross-process locks are non-blocking primitives. Callers own wait/yield policy.

## Read these first

- ![[indexing]]
- [[Indexing pipeline (Hub)]]
- [[indexing-workflow]]
- [[indexing-pipeline-architecture]]
- [[semantic-runtime-lane-policy]]
- [[indexing-observability-and-maintenance-policy]]
- [[Indexing pipeline - End-to-end walkthrough]]
- [[Indexing pipeline - rzm index orchestration]]
- [[Indexing pipeline - Phase ownership matrix]]
- [[Index lock and background coordination runbook]]
- [[Embeddings - indexing pipeline]]
- [[primary-semantic-chunks-and-noderef-search]]

## Architecture decisions

- [[Use a single queued writer lane for SQLite-backed indexing writes]]
- [[Preserve explicit correctness barriers in the indexing pipeline]]
- [[Use bounded backpressure in indexing instead of adding writer concurrency]]

## Live updating

- [[Indexing pipeline - Live updating (watcher runtime)]]

## Graph-derived signals

- [[Indexing pipeline - Graph signals (doc scores + anchor PageRank)]]
