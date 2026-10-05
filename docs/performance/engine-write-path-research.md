---
summary: "Write-path source audit, reproducible performance baselines, and proposed atomic persistence and alias-loading batch."
reference-kind: guide
---

# Engine write-path research

Research at `af100a9059fe1e34a88a59d70af26cf75e5e1368`, 2026-09-05, branch `codex/engine-write-path-research`. Coordinator: task `01a072bf-e520-7603-bb3d-468148d9a63b`; delivery effort EFF-2026-09-05-14-10 / SPEC-0089. No production edits or PR during this phase.

## Recommendation

First batch: make code persistence atomic across freshness and derived artifacts, then replace quadratic alias population with bulk construction. These are independently reviewable changes with a reproduced correctness defect and a measured performance target. Keep broad scan elimination, ownership batching, watcher scheduling, and configuration persistence as later decisions.

| Priority | Opportunity | Evidence | Cost / constraint |
| --- | --- | --- | --- |
| 1 | Code freshness committed before Intel/rationale failure | Injected anchor insert abort leaves fresh hash/version/ok file row and zero anchors | Medium; transaction scope and retry semantics need regression coverage |
| 2 | Quadratic alias population during one-note delta | 1k aliases: 10.97 ms incremental versus 0.251 ms bulk; 5k: 308.19 ms versus 1.669 ms | Small; preserve duplicate claimants, candidate membership, deleted/changed aliases |
| 3 | Full-index ownership checks and per-path retirement | Cold 1k-note/100-code run: ownership hold 686 ms; no-op: 46 ms | Medium; batch within existing atomic ownership/reconciliation contract |
| 4 | Permanent transaction-open failures retried | Closed store returns after 3.138 s; retry holds shared write mutex | Small; classify BeginTx errors, cancellation-aware backoff, no final sleep |
| 5 | Rationale schema/FTS work repeated per file | 512 empty rationale paths: median 17.523 ms; nine schema statements per path | Small–medium; keep migration-owned DDL and FTS replacement parity |
| 6 | Whole-vault metadata work | No-op dirty discovery at 1k notes: 14.75 ms, 40.6 MB; one-note delta 15.99 ms, 4.56 MB | Medium; source/provider/link topology correctness constrains shortcuts |
| 7 | Watcher debounce has no maximum age | 626 ms continuous hints produced zero callbacks; quiet interval released one batch | Medium; scheduler semantics and deterministic clock tests |
| 8 | No-op config rewrites / unconditional maintenance | Three no-op indexes each rewrote config; ANALYZE ~3 ms | Small benefit in this fixture; avoid prioritizing above measured hot paths |

Costs are estimates. Performance gains outside the alias construction comparison have not been measured. Atomicity is a correctness priority independent of throughput.

## Scope and source trace

Read governing indexing, notemeta, stores, and code-intel subsystem notes and engineering policy. Built the worktree binary with `NO_WEB=1 make build`. Rhizome session `PX5cagzkzyZD3y_b` started successfully with seeds `pkg/notemeta` and `pkg/app/indexing`.

Full indexing starts in `pkg/app/indexing/unified.go`. `discoverUnifiedOwnership` in `unified_ownership.go` reads persisted note/code owners, performs `noteownership.Discover`, and passes all projectable note paths into `BuildTransitionPlan`. Discovery has one eligible-file walk. Candidate slices then feed bounded file workers without rediscovery. Sealed note sources let provider projection and note ingestion reuse captured source data. This existing architecture should be retained.

`PreparePublishedMetadata` projects sealed Markdown, typed ownership controls fence preceding queue writes, note/code work is enqueued, and `BuildPublishedMetadataDeltaFromPreparation` publishes complete raw metadata before ontology. Final drains precede graph computation, config persistence, maintenance, and exact-generation acknowledgement. Full mode still reads/projects all selected notes to prove freshness; one discovery walk does not mean all work scales with changed files.

`indexingpipe.ProcessCandidates` bounds workers and queue size; it retains only per-worker payloads. It already exposes queue residence/read latency. The queue has row/byte/idle flush policies and one durable writer lane. Adding writers would violate SQLite and subsystem constraints. Existing full-run timings show no meaningful enqueue contention in the small fixture.

Live cache refresh snapshots dirty/stale markers, releases its mutex before I/O, and requeues failed paths (`pkg/vault/cache/service.go:490`). Directory rename refresh rescans parents and the hub additionally marks directories stale. Full stale scans may be necessary; repeated parent/subtree work is a hypothesis, not a quantified bottleneck. Hub subscription callbacks run serially within each dispatch, but timer callbacks can overlap; no new concurrent-delivery correctness claim was established.

SQLite configuration is centralized in `pkg/sqliteutil/db.go`, with WAL, busy timeout, filesystem guard, pooling and migration ownership. No evidence justifies pragma/pool tuning in this batch. The store's shared write mutex and queued-writer fences remain required.

## Measured baselines

Host: Apple M4 Pro, Darwin arm64; `GOMAXPROCS=4`, built-in Markdown runtime, real disposable SQLite DB, embeddings disabled. Other effort workers share the host. These are bounded warm-cache engineering measurements, not isolated production latency claims. Microbenchmarks use three iterations per sample and three samples unless specified; report medians. Setup/store open is excluded from the new metadata and alias timers.

### Metadata APIs

Synthetic in-memory `NoteReader`: 100 or 1,000 notes, each with a title, tag, unique alias, and link to n0000. It counts authored-content reads and enumerations. SQLite remains real. This isolates metadata CPU/DB work and understates filesystem I/O cost.

| Operation | 100 notes ms/op | 1,000 notes ms/op | 1,000-note bytes/op | Content reads / enumerations at 1,000 |
| --- | ---: | ---: | ---: | --- |
| MetadataStateCurrent | 0.419 | 3.275 | ~2.72 MB | 2,000 / 2 |
| EnsureIndexed unchanged | 0.327 | 2.693 | ~2.35 MB | 1,000 / 1 |
| DiscoverDirtyPaths unchanged | 1.871 | 14.746 | ~40.6 MB | 1,000 / 1 |
| BuildPathDelta existing one path | 0.829 | 15.990 | ~4.56 MB | 1 / 0 |
| BuildPathDelta added one path | 2.056 | 17.186 | ~43.9 MB | 1,002 / 1 |

The delta benchmark derives without applying; the added source remains new for each iteration. It is not a stream of repeated successful additions. A one-second CPU profile of existing-path delta reports 15.906 ms/op, with 51.49% cumulative CPU attributed to `NotePathCache.removeAliasesForPath`. Profile includes benchmark setup, although the measured operation dominates samples.

Source causes:

- `Indexer.MetadataStateCurrent` calls the helper that computes the note hash, then computes it again to recover the same paths (`indexer_operations.go:97`, `provider_freshness.go:79`). A source snapshot/result returned once can remove the duplicate read without weakening validation.
- `DiscoverDirtyPaths` projects all sources before comparison (`indexer_operations.go:81`). Unchanged provider work dominates its allocations.
- `BuildPathDelta` lists all persisted paths, checks all provider rows, fingerprints all selected paths, loads all aliases for topology comparison, then loads aliases again for resolution (`indexer_operations.go:117`, `projection_incremental.go:18,131`). It reads one authored note but performs vault-sized DB/CPU work.
- New/deleted paths and changed alias sets deliberately cause complete link rederivation because unresolved inbound links are not persisted. This is a correctness constraint, not redundant work that can simply be deleted.

### Alias construction comparison

`applyAliasesToCache` invokes `AddOrUpdate` for every alias-owning path (`pkg/notemeta/index.go:625`). `AddOrUpdate` scans every alias to remove that path's old claims (`pkg/vault/obsidian/wikilinks.go:158,216`). Building A aliases therefore performs triangular alias-map scans. Existing `BuildNotePathCacheWithAliases` directly populates the map.

| Alias paths | Current incremental population | Existing bulk constructor | Component ratio |
| --- | ---: | ---: | ---: |
| 1,000 | 10.967 ms | 0.251 ms | 43.6× |
| 5,000 | 308.192 ms | 1.669 ms | 184.7× |

This comparison uses identical complete note-path and alias inputs and checks alias count. It proves the component cost; it is not yet a production delta speedup or full semantic parity test. Production adaptation must include the union of resolved candidate paths, changed source paths and alias-owning paths, preserving the current `AddOrUpdate` effect on `NotePaths` as well as alias claims.

### End-to-end CLI

Fixture: 1,000 Markdown notes with unique aliases and links, 100 tiny Go files with one function each, Go root `src`, no embedding providers. Run the locally built binary from fixture root with `RZM_SKIP_REPO_DELEGATE=1` and `index --timings`.

- Cold run: CLI collector total 1.1 s; ownership transitions held writer 686 ms across two calls; note ingest 75 ms (57 ms writer busy); metadata delta 48 ms hold; graph 54 ms; code ingest 23 ms (16 ms writer busy). Cold includes creation/migrations, and is one sample.
- Three no-op subprocess wall times: 212.31 / 205.68 / 203.55 ms. Middle run collector total 183 ms, ownership transitions 46 ms hold, note ingest 7 ms, maintenance 3 ms. No code/note payload writes. Config mtime advanced every run.
- One-note body edit: subprocess 320.77 ms, collector 299 ms; metadata delta 59 ms hold, ownership 51 ms hold, graph 52 ms, note ingest 8 ms. This is one sample.

Ownership has two point reads per note transition (`ownershipEffectiveTransitionsTx`, `ownershipDurableNoteSourceTx`, `ownershipPathHasCodeOwnerTx`, `pkg/anchors/sqlite/ownership_transition.go:149`). Cold retirement loops over paths and touches both old-owner artifact families (`:104`). Set-oriented checks/retirement are plausible large wins, but must retain source identity comparison, coexisting-owner cleanup, affected edges, and atomic pending-generation semantics.

The timing summary's critical-path list does not include the large ownership phase, though DB-op counters report its hold time. Better phase coverage would prevent misdiagnosing 75 ms note ingest as the dominant part of a 1.1 s run.

## Correctness and failure evidence

### Fresh file metadata before failed artifact persistence

`ApplyCodePersistenceBatch` commits summaries, metadata, references and external evidence first, then writes Intel and rationale chunks in separate transactions (`pkg/anchors/sqlite/code_index_batch_store.go:130–196`). A trigger that aborts insertion into `intel_code_anchors` produces:

```text
batch error=audit induced failure
after failed batch hash="fresh-hash" version="v1.11.0" status="ok" exists=true err=<nil>
anchors=0
```

`BuildCodeIndexWork` trusts that hash/version/status (`pkg/anchors/batch.go:112`). Therefore the next retry can skip work whose artifacts never committed. An existing-file update can also leave mixed generations. Store rollback tests currently target failure in the first refs transaction (`store_batch_test.go:473`), missing later Intel/rationale failure.

### Mtime-only early skip

After the CLI fixture was indexed, replacing function `F1` with `ChangedF1` and preserving the file's mtime, then running successful full index, left symbol `F1` in SQLite. `pkg/app/codeintel/ingest.go:791–795` skips code where stored mtime is greater than or equal to candidate mtime before reading content. This also warrants a same-second test because discovery uses Unix seconds. It is a separate freshness-policy issue: do not silently remove the guard in this performance batch without assessing read-cost and timestamp contract.

### Retry loop

Closed-store `ApplyCodePersistenceBatch` returned `sql: database is closed` after 3.138286792 s. `execTxWithRetry` retries every BeginTx failure and sleeps after the fifth failure (`pkg/anchors/sqlite/store.go:9335`). Existing statement/commit branches classify busy/locked but also sleep after the final attempt; sleeps do not wake on cancellation. This differs from its stated intent to mirror embedding-store behavior.

### Watcher/configuration

A hint-only hub with 100 ms debounce receiving 30 events at 20 ms spacing delivered zero callbacks during 625.9 ms of activity; after 150 ms quiet it delivered one. `enqueue` resets a single timer on every event (`pkg/vault/watchhub/hub.go:589`). Maximum delivery latency is unbounded under sustained activity, including unrelated paths. A maximum batch age is a possible later policy change.

`runUnifiedPostIndex` always saves code and embedding config, invokes ANALYZE/optimize/checkpoint, and writes scope metadata. `SaveLocalConfig` encodes and atomically rewrites the file even when unchanged (`pkg/vault/obsidian/local_config.go:328,537`). Config rewrites were measured; downstream invalidation amplification was not. No claim that the hub's ordinary events deliver config changes is made.

## Proposed first implementation batch

1. Atomic code batch: use one `withWriteTx` for the complete existing `CodePersistenceBatch`, encompassing summaries/metas, refs/external evidence, Intel and rationale. Existing queue row/byte bounds limit normal flush size; a single oversized source remains possible, as today. Internal statement parameter chunking remains. Remove transaction splitting by artifact family. This is simpler and stronger than moving only `upsertFileMetasBatchTx` last, since summary persistence can also publish freshness. Do not add state columns or a retry journal. On failure/cancellation, return the original error and leave the prior complete batch visible; retry derives and applies normally. Readers using WAL see committed state, never this batch's mixed generations. Existing service-level doc-link writes happen before this store seam and are outside its current DTO; do not claim broader service atomicity.
2. Alias delta construction: use the existing bulk constructor with the complete candidate/alias-owner union and the already overlaid alias map, or a narrowly scoped bulk alias replacement helper if it better preserves call-site clarity. Avoid a new mutable reverse index unless required. Preserve duplicate-alias ambiguity, sorted candidates, changed aliases replacing old claims, removals, and relative-path resolution. Share the first all-alias query between topology decision and cache population where safely local; do not expand into eliminating whole-vault freshness checks.

Dependencies/ownership: write lane owns `pkg/anchors/sqlite/code_index_batch_store.go`, its focused tests, batch persistence contracts in `pkg/anchors/batch.go` only if necessary, `pkg/notemeta` delta/cache construction, and `pkg/vault/obsidian/wikilinks.go` if needed. Coordinator assigned alias work here. Ontology internals belong to read worker; graph fingerprint/browser paths belong to browser worker. Do not modify those or noderead/query behavior.

Regression evidence required:

- Seed prior complete rows, fail at first and later Intel paths and at rationale insertion, and assert prior file hashes/symbols/refs/external evidence/anchors/FTS/rationale remain intact. Include new files, >128 paths, and cancellation. Drop failure trigger, retry through the service, and prove new state arrives rather than hash-skipping. Keep existing initial-ref rollback coverage.
- Healthy persistence benchmark at 1/128/512 paths, with representative refs/anchors/rationale, before and after. Measure latency/allocations and writer hold time; verify throughput and queue behavior rather than testing transaction-count implementation details. If single-transaction hold materially regresses larger batches, split *complete per-path bundles* across bounded transactions as a coordinator-reviewed adjustment, never by artifact family.
- Delta-vs-fresh-snapshot parity for aliases: unchanged targets, duplicate claimants, removed claimant, changed alias, new/deleted path, relative links and deterministic output. Retain the 1k/5k component benchmark and add before/after one-note delta measurements.
- Focused package tests, required integration/race/full gates and Markdown validation before commit; PR targets `codex/engine-performance-integration`, then Greptile response and exact-head verification. No main merge or release.

## Research verification and reproducibility

`GOCACHE="$PWD/.gocache" GOMAXPROCS=4 go test -tags=fts5 ./pkg/notemeta ./pkg/app/indexingpipe ./pkg/vault/watchhub ./pkg/sqliteutil` passed (1.439s / 0.372s / 3.254s / 1.624s). Full `make check` deliberately not run during research.

Metadata and alias experiment commands, after copying the appendix into disposable `pkg/notemeta/write_path_research_test.go`:

```sh
GOCACHE="$PWD/.gocache" GOMAXPROCS=4 go test -tags=fts5 ./pkg/notemeta -run '^$' -bench '^BenchmarkWritePathResearch$' -benchtime=3x -count=3
GOCACHE="$PWD/.gocache" GOMAXPROCS=4 go test -tags=fts5 ./pkg/notemeta -run '^$' -bench '^BenchmarkAliasLoadingResearch$' -benchtime=3x -count=3
GOCACHE="$PWD/.gocache" GOMAXPROCS=4 go test -tags=fts5 ./pkg/notemeta -run '^$' -bench '^BenchmarkWritePathResearch/1000/delta_existing$' -benchtime=1s -cpuprofile=/tmp/write-path-delta.cpu -o /tmp/write-path-notemeta.test
GOCACHE="$PWD/.gocache" go tool pprof -top -nodecount=18 /tmp/write-path-notemeta.test /tmp/write-path-delta.cpu
```

Existing orphan pruning benchmark: `go test -tags=fts5 ./pkg/anchors/sqlite -run '^$' -bench '^BenchmarkPruneOrphanIntelSymbolRefTargets100K$' -benchtime=5x -count=2`. 100k targets, 0%/10%/50% orphan samples: 16.54/16.35 ms, 39.16/38.01 ms, 128.01/128.98 ms. Its store-open setup is timed before the first StopTimer; add ResetTimer before using it for production optimization comparisons. Earlier one-iteration observations are discarded as setup-contaminated. No direct code-persistence throughput benchmark existed in the audited lane.

Raw outputs in this host session: `/tmp/write-path-baseline.txt`, `/tmp/write-path-profile.txt`, `/tmp/write-alias-baseline.txt`, `/tmp/write-full-cold.txt`, `/tmp/write-full-noop-{0,1,2}.txt`, `/tmp/write-full-edit.txt`, `/tmp/write-code-preserved-mtime.txt`, `/tmp/write-focused-tests.txt`. Disposable SQLite and watcher reproducers are reproduced below so temporary paths are not the only source of evidence.

### Metadata and alias microbenchmarks

```go
package notemeta

import (
 "context"
 "fmt"
 "path/filepath"
 "sync/atomic"
 "testing"
 semdb "github.com/atomicobject/rhizome/pkg/anchors/sqlite"
 "github.com/atomicobject/rhizome/pkg/vault/obsidian"
)

type researchReader struct { fakeNoteReader; reads atomic.Int64; scans atomic.Int64 }
func (r *researchReader) GetContents(v obsidian.VaultDefinition, p string) (string,error) { r.reads.Add(1); return r.fakeNoteReader.GetContents(v,p) }
func (r *researchReader) GetNotesList(v obsidian.VaultDefinition) ([]string,error) { r.scans.Add(1); return r.fakeNoteReader.GetNotesList(v) }

func BenchmarkWritePathResearch(b *testing.B) {
 for _, size := range []int{100,1000} { for _, mode := range []string{"current","ensure_noop","dirty_noop","delta_existing","delta_added"} { b.Run(fmt.Sprintf("%d/%s",size,mode),func(b *testing.B) {
  ctx:=context.Background(); root:=b.TempDir(); vault:=obsidian.VaultDefinition{Path:root}
  store,err:=semdb.Open(filepath.Join(root,"intel.sqlite")); if err!=nil {b.Fatal(err)}; defer store.Close()
  r:= &researchReader{fakeNoteReader:fakeNoteReader{notes:map[string]string{}}}
  for n:=0;n<size;n++ { r.notes[fmt.Sprintf("n%04d.md",n)]="---\ntags: [bench]\naliases: [alias"+fmt.Sprint(n)+"]\n---\n# Heading\nSee \x5b\x5bn0000\x5d\x5d.\n" }
  idx:=testIndexer(b); if _,err=idx.EnsureIndexed(ctx,vault,r,store);err!=nil {b.Fatal(err)}
  if mode=="delta_added" { r.notes["added.md"]="# Added\n" }
  r.reads.Store(0);r.scans.Store(0);b.ReportAllocs();b.ResetTimer()
  for n:=0;n<b.N;n++ { switch mode {
  case "current": var ok bool;ok,err=idx.MetadataStateCurrent(ctx,vault,r,store);if !ok && err==nil {b.Fatal("not current")}
  case "ensure_noop": _,err=idx.EnsureIndexed(ctx,vault,r,store)
  case "dirty_noop": _,err=idx.DiscoverDirtyPaths(ctx,vault,r,store)
  case "delta_existing": _,err=idx.BuildPathDelta(ctx,vault,r,store,[]string{"n0001.md"},nil)
  case "delta_added": _,err=idx.BuildPathDelta(ctx,vault,r,store,[]string{"added.md"},nil)
  };if err!=nil {b.Fatal(err)} }
  b.StopTimer();b.ReportMetric(float64(r.reads.Load())/float64(b.N),"reads/op");b.ReportMetric(float64(r.scans.Load())/float64(b.N),"scans/op")
 }) } }
}

func BenchmarkAliasLoadingResearch(b *testing.B) {
 for _,size:=range []int{1000,5000} {for _,mode:=range []string{"incremental","bulk"}{b.Run(fmt.Sprintf("%d/%s",size,mode),func(b *testing.B){
 aliases:=make(map[string][]string,size);paths:=make([]string,size)
 for n:=range size{paths[n]=fmt.Sprintf("n%04d.md",n);aliases[paths[n]]=[]string{fmt.Sprintf("alias%d",n)}}
 b.ReportAllocs();b.ResetTimer();for n:=0;n<b.N;n++{var cache *obsidian.NotePathCache
 if mode=="incremental" {cache=obsidian.BuildNotePathCacheWithAliases(paths,nil);applyAliasesToCache(cache,aliases)}else{cache=obsidian.BuildNotePathCacheWithAliases(paths,aliases)}
 if len(cache.Aliases)!=size {b.Fatal("lost aliases")}
 }
 })}}
}
```

### SQLite failure and retry reproducer

```go
package main
import (
 "context"
 "fmt"
 "os"
 "path/filepath"
 "time"
 a "github.com/atomicobject/rhizome/pkg/anchors"
 s "github.com/atomicobject/rhizome/pkg/anchors/sqlite"
 "github.com/atomicobject/rhizome/pkg/sqliteutil"
)
func main() {
 ctx:=context.Background(); dir,e:=os.MkdirTemp("", "rzm-store-audit-"); must(e); p:=filepath.Join(dir,"db.sqlite")
 st,e:=s.Open(p); must(e)
 db,e:=sqliteutil.OpenDSN(sqliteutil.DSN(p),sqliteutil.Options{}); must(e)
 _,e=db.Exec(`CREATE TRIGGER audit_fail BEFORE INSERT ON intel_code_anchors BEGIN SELECT RAISE(ABORT, 'audit induced failure'); END`);must(e)
 e=st.ApplyCodePersistenceBatch(ctx,a.CodePersistenceBatch{Metas:[]a.FileMeta{{Path:"a.go",Lang:a.LangGo,Hash:"fresh-hash",ParseStatus:a.ParseOK}},IntelReps:[]a.IntelCodeFileReplace{{Path:"a.go",Anchors:[]a.IntelAnchor{{AnchorID:"a",Path:"a.go",Lang:"go",Kind:"function"}}}}})
 fmt.Printf("batch error=%v\n",e)
 h,v,status,ok,e:=st.FileHash(ctx,"a.go"); fmt.Printf("after failed batch hash=%q version=%q status=%q exists=%v err=%v\n",h,v,status,ok,e)
 var count int;must(db.QueryRow(`SELECT count(*) FROM intel_code_anchors`).Scan(&count));fmt.Printf("anchors=%d\n",count)
 must(db.Close());must(st.Close())
 start:=time.Now();e=st.ApplyCodePersistenceBatch(ctx,a.CodePersistenceBatch{Metas:[]a.FileMeta{{Path:"b.go",Lang:a.LangGo,Hash:"hash"}}});fmt.Printf("closed store error=%v latency=%v\n",e,time.Since(start))
 fmt.Printf("fixture=%s\n",dir)
}
func must(e error){if e!=nil{panic(e)}}
```

### Rationale batch baseline

```go
package main
import("context";"fmt";"os";"path/filepath";"time";a "github.com/atomicobject/rhizome/pkg/anchors";s "github.com/atomicobject/rhizome/pkg/anchors/sqlite")
func main(){dir,e:=os.MkdirTemp("","rzm-rationale-audit-");if e!=nil{panic(e)}; st,e:=s.Open(filepath.Join(dir,"db.sqlite"));if e!=nil{panic(e)};defer st.Close();ctx:=context.Background();for _,n:=range []int{1,128,512}{b:=a.CodePersistenceBatch{};for i:=0;i<n;i++{b.RationaleBatches=append(b.RationaleBatches,a.RationaleBatch{Path:fmt.Sprintf("src/file_%d.go",i)})};if e:=st.ApplyCodePersistenceBatch(ctx,b);e!=nil{panic(e)};for r:=0;r<3;r++{start:=time.Now();for j:=0;j<10;j++{if e:=st.ApplyCodePersistenceBatch(ctx,b);e!=nil{panic(e)}};fmt.Printf("empty rationale paths=%d repeat=%d ms/op=%.3f\n",n,r,float64(time.Since(start).Microseconds())/10000)}};fmt.Println("fixture",dir)}
```

### Watcher continuous-event reproducer

```go
package main
import("context";"fmt";"os";"sync/atomic";"time";"github.com/atomicobject/rhizome/pkg/vault/watchhub")
func main(){root,err:=os.MkdirTemp("","watch-audit-");if err!=nil{panic(err)};h,err:=watchhub.NewHub(root,watchhub.Options{DisableFSNotify:true,Debounce:100*time.Millisecond});if err!=nil{panic(err)};h.Start(context.Background());defer h.Close();var calls atomic.Int64;h.Subscribe("audit",watchhub.Filter{IncludeFiles:true},func(context.Context,[]watchhub.WatchEvent){calls.Add(1)},nil);started:=time.Now();for n:=0;n<30;n++{h.EmitHintPaths([]string{"note.md"});time.Sleep(20*time.Millisecond)};fmt.Println("continuous_hint_duration",time.Since(started),"callbacks",calls.Load());time.Sleep(150*time.Millisecond);fmt.Println("after_quiet_callbacks",calls.Load())}
```

Run each standalone Go reproducer from the repository root using `GOCACHE="$PWD/.gocache" GOMAXPROCS=4 go run -tags=fts5 /tmp/<reproducer>.go`. Fixtures are disposable; use `trash` for cleanup.

### CLI fixture generator

Run this Python from any directory, using a new empty temporary root if rerunning the cold sample:

```python
from pathlib import Path
p = Path('/tmp/rzm-write-perf-fixture')
for directory in ['.rhizome', 'notes', 'src']:
    (p / directory).mkdir(parents=True, exist_ok=True)
(p / '.rhizome/config.yml').write_text('code:\n  enabled: true\n  go:\n    roots: [src]\nnoteEmbeddings:\n  enabled: false\ncodeEmbeddings:\n  enabled: false\n')
link = chr(91) * 2 + 'n0000' + chr(93) * 2
for i in range(1000):
    (p / 'notes' / f'n{i:04d}.md').write_text(f'---\naliases: [alias{i}]\ntags: [bench]\n---\n# Note {i}\nSee {link}.\n')
for i in range(100):
    (p / 'src' / f'f{i:04d}.go').write_text(f'package bench\n// F{i} refers to {link}.\nfunc F{i}() int {{ return {i} }}\n')
(p / 'go.mod').write_text('module example.com/bench\n\ngo 1.24\n')
```

From that root, use the absolute local worktree binary:

```sh
GOMAXPROCS=4 RZM_SKIP_REPO_DELEGATE=1 <worktree>/bin/darwin/rzm index --timings
```

Run once cold and three times unchanged. Append a paragraph to `notes/n0001.md` and run again for the edit sample. For the code guard reproduction, capture `src/f0001.go` stat times, change `func F1()` to `func ChangedF1()`, restore times with `os.utime(..., ns=(stat.st_atime_ns, stat.st_mtime_ns))`, run full indexing, and query `SELECT name FROM symbols WHERE file='src/f0001.go'` in `.rhizome/db.sqlite`. Observed result was the old `F1` despite successful indexing.

## B02 delivery evidence

Approved by the coordinator on 2026-09-05. One `withWriteTx` now covers all existing code persistence domains; internal Intel statement chunks retain their size bounds. First/later Intel failures, later rationale failure, mid-transaction cancellation, new/prior files across 129 paths, and actual service retry all reproduced failure before the change and passed afterward. Existing large-batch coverage now asserts persisted artifacts and operation visibility rather than three implementation-specific commits. Independent source review found no actionable issue.

Controlled `BenchmarkCodePersistenceBatch`, M4 Pro / GOMAXPROCS=4, warm real SQLite, 10 operations per sample ×3, setup excluded. Each source has a symbol, references, external evidence, anchor, FTS, and rationale. Baseline compiled using a Go overlay restoring only `code_index_batch_store.go` from 76d41fd3; identical new benchmark/test sources were used for both binaries. B06/B07 edits were isolated out of this checkout before compilation. Commands:

```sh
GOMAXPROCS=4 /tmp/b02-before.test -test.run '^$' -test.bench '^BenchmarkCodePersistenceBatch$' -test.benchtime=10x -test.count=3
GOMAXPROCS=4 /tmp/b02-after.test -test.run '^$' -test.bench '^BenchmarkCodePersistenceBatch$' -test.benchtime=10x -test.count=3
```

| Paths | Before median ms/op | After median ms/op | Before / after cumulative writer hold per 10 operations |
| --- | ---: | ---: | --- |
| 1 | 1.094 | 1.073 | 10 / 10 ms |
| 128 | 17.819 | 17.742 | 177 / 177 ms |
| 512 | 73.278 | 72.360 | 731 / 723 ms |

Healthy throughput did not regress in this fixture. The 512-path atomic batch holds the writer continuously for about 72 ms, instead of yielding between five transactions. The collector reports cumulative hold, not the old maximum individual segment; mixed-workload fairness is not established by this microbenchmark. The measured total work does not justify weakening failure atomicity. Raw logs: `/tmp/b02-before-bench.txt`, `/tmp/b02-after-bench.txt`.

Focused SQLite, anchors and codeintel tests passed. Markdown validation passed with zero findings. Required full gate runs on the isolated B02 source; final gate/PR evidence will be appended before handoff.

B02 full verification passed: `GOMAXPROCS=4 make check GO_TEST_PACKAGES=4 GO_TEST_PARALLEL=4 CHECK_JOBS=2` on the isolated B02 checkout (race unit suite, both integration targets, Go lint/vet, benchmark harness tests, web lint/tests/type checks, Greptile configuration). Log: `/tmp/b02-make-check.txt`. No B06/B07 source was present in this gate.
