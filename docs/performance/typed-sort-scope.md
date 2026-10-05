---
summary: "B08 selected-type SQL sort aggregation, ordering parity, query-plan evidence, and controlled measurements."
reference-kind: guide
---

# Typed sort aggregation scope

B08, approved 2026-09-05 under SPEC-0089. Sorting by a field previously aggregated that field across every indexed node before joining the selected type. Each sort aggregate now restricts node IDs to nodes belonging to the requested types. It uses existing indexes and adds no migration.

The store method and its SQL construction moved mechanically from `pkg/anchors/sqlite/store.go` into `pkg/anchors/sqlite/typed_node_query.go`. The aggregate still considers every row with the field name: list MIN/MAX, NULL behavior, ties, and legacy denormalized field type/kind remain unchanged. Candidate ownership comes from `ontology_nodes`, and all values remain bound parameters.

Exact ordering regressions pass against both original production and the new query. They cover single/multiple selected types, duplicate/blank type names, lists, NULL and absent rows, ascending/descending sorts, predicates, offsets, multiple sort keys, embedded identity, and unrelated extreme values. Actual generated SQL EXPLAIN uses `idx_ontology_nodes_type` for the selected nodes and `idx_ontology_node_field_values_node` for node/field lookup; it does not scan the field table. Independent and coordinator source review found no actionable issues.

Retained fixture: `pkg/anchors/sqlite/typed_node_query_benchmark_test.go`, `BenchmarkTypedSortUnrelatedRows`. It holds ten Wanted nodes fixed and increases total rows from 100 to 10,000. Each query returns exactly IDs n0 through n4; database setup is outside timing. Baseline uses the original production method from integration `f5a71124` through a Go overlay with the same fixture.

Controlled timings completed in the coordinator's serialized slot. Full `make check` passed outside the sandbox after the restricted run could not bind HTTP test ports. Local `make build NO_WEB=1` and `./scripts/rzm validate` also passed.

## Controlled measurements

Apple M4 Pro, `GOMAXPROCS=4`, median of three matched samples at 200 ms benchtime. Both binaries were compiled before the quiet slot; no overlapping heavy work ran during timing.

| Total nodes | Before | After | Before bytes/op | After bytes/op |
| ---: | ---: | ---: | ---: | ---: |
| 100 | 62.962 µs | 53.883 µs | 11,470 | 11,868 |
| 10,000 | 2,722.385 µs | 55.178 µs | 11,491 | 11,869 |

The large fixture improves 49.3-fold, with four additional Go allocations per query (169 to 173). Runtime stays near 54–55 µs as unrelated rows grow. This establishes the selected-type field-sort behavior in the retained fixture, not end-to-end workspace latency.

Run `GOMAXPROCS=4 go test -tags fts5 ./pkg/anchors/sqlite -run '^$' -bench '^BenchmarkTypedSortUnrelatedRows$' -benchmem -benchtime=200ms -count=3`, using the original-production overlay for the baseline.


## Reconstruct the comparison

Run from a repository checkout containing the recorded commits, using the same Go/CGO toolchain for both sides. The recorded host was Apple M4 Pro, Go 1.24.2. This creates disposable detached worktrees and carries only the retained benchmark into original production; no live vault/index is used. Compile both binaries before the quiet timing window, then execute sequentially without overlapping heavy work. Setup remains outside benchmark timing. Keep the generated output directory for review.

```sh
set -eu
comparison=$(mktemp -d /tmp/rzm-b08-compare.XXXXXX)
git worktree add --detach "$comparison/before" f5a711249465b31cb0c6bfea449f1fc94b46a7bc
git worktree add --detach "$comparison/after" 956d317b0ed6531f5a83c8052560115c4f58e782
git show 956d317b0ed6531f5a83c8052560115c4f58e782:pkg/anchors/sqlite/typed_node_query_benchmark_test.go > "$comparison/before/pkg/anchors/sqlite/typed_node_query_benchmark_test.go"
for side in before after; do
  (cd "$comparison/$side" && GOMAXPROCS=4 go test -mod=vendor -tags fts5 -c -o "$comparison/$side.test" ./pkg/anchors/sqlite)
done
# Enter the quiet timing window only after both builds complete.
for side in before after; do
  (cd "$comparison/$side/pkg/anchors/sqlite" && GOMAXPROCS=4 "$comparison/$side.test" -test.run '^$' -test.bench '^BenchmarkTypedSortUnrelatedRows$' -test.benchmem -test.benchtime=200ms -test.count=3) > "$comparison/$side.txt" 2>&1
done
```

The baseline invokes the original `OntologyNodesByTypePlan` in `store.go`; no new SQL-builder file is needed because only the public-method benchmark is copied. The historical overlay included a baseline-compatible builder solely to compile newer query-plan tests. This clean-checkout recipe avoids that test-compilation dependency without changing the measured production method.

This recipe was source-checked during closure alignment and was not executed during the browser timing slot. Previously recorded overlay measurements, parity tests and full gates remain the executed evidence; fresh outputs require review before becoming final integrated performance claims.
