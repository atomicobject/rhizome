# Fifty-note drain measurement

The batch 3 worker measured one successful run before grouping and one after. Both used the repository's deterministic workload with 50 typed notes. All edits entered one watcher batch. A synthetic provider added 100 ms per physical shared-node call. The timer covered structural publication through zero remaining derived tickets.

| Measurement | Before grouping | After grouping |
| --- | ---: | ---: |
| Drain | 9.8016645 s | 573.448125 ms |
| Provider calls | 50 | 1 |
| Sent texts | 50 | 50 |
| Lane jobs, including the watcher batch | 255 | 9 |
| Current persisted embedding paths | 50 | 50 |
| Failures | 0 | 0 |

The baseline was worker commit `e58daaa68`, carried onto the integration branch as `b422bc785`. The after run used the batch 3 implementation before final review cleanup, subsequently committed as `91d6dc6d2` and integrated as `54d86c054`. Later cleanup preserved the 50-text dispatch shape and added large-plan, path-normalization, and acknowledgement-accounting coverage.

These are single runs on a busy 14-core host. Observed load averages were about 34 before and 23 after. The counts show fewer serial requests and lane admissions. The wall times do not establish a stable latency estimate or a controlled speedup percentage. The original findings' earlier 12.16 s and 312-job sample is a different baseline and is not this comparison.

The throwaway timing test was removed from the implementation checkout. Durable regressions assert call counts, job counts, persisted current paths, and source safety. `TestDerivedGroupFiftyChangedNotesDrainByCounts` covers typed and untyped bursts with at most two provider calls and 20 lane jobs. No timing assertion entered CI. This closure pass reused the supplied measurement and did not run a new benchmark.
