---
summary: "B05 operation-local resolver reuse, catalog parity evidence, and controlled scaling measurements."
reference-kind: guide
---

# Catalog resolver reuse

B05, approved 2026-09-05 under SPEC-0089. Catalog construction previously rebuilt a projection resolver for every descendant and for global-source enumeration. The same operation now reuses one resolver. Structural-edge traversal uses its own operation-local resolver. No state is shared across operations.

The supplied catalog root, fallback eligibility, sorted field traversal, global-source order, node deduplication, semantic parents, field values, link dependencies, and derived identifiers remain unchanged. The broader source-edit inventory is not substituted for catalog traversal. This is a computation change; no materialization version or indexed row shape changes.

The nested/global checkbox regression passes against both original production and this implementation. Existing ontology tests cover fallback projections, invalid preferred identifiers, derived sibling allocation, and embedded structural edges. The ontology package suite passes. Independent and parent source review found no actionable issues.

Retained fixture: `pkg/ontology/catalog_scaling_test.go`, `BenchmarkCatalogResolverScaling`. Schema compilation, Markdown parsing, and initial root projection are outside timing; each iteration constructs the catalog from the same projection. It checks the requested number of ActionItem nodes before timing. Controlled baseline uses a Go overlay of the three original production files from integration commit `f5a71124`; the fixture is identical.

Controlled timings completed in the coordinator's serialized slot. Full `make check`, local `make build NO_WEB=1`, and `./scripts/rzm validate` passed. Derived-ID inventory optimization remains deferred: the earlier 500-item CPU profile was dominated by resolver construction and this fixture has no derived-ID field.


## Controlled measurements

Apple M4 Pro, `GOMAXPROCS=4`, median of three matched samples at 200 ms benchtime. Both test binaries were compiled before the quiet slot, with identical retained fixtures. No overlapping heavy work ran during timing.

| Descendants | Before | After | Speedup | Before bytes/op | After bytes/op |
| ---: | ---: | ---: | ---: | ---: | ---: |
| 10 | 0.086 ms | 0.048 ms | 1.8× | 168,388 | 105,582 |
| 100 | 3.545 ms | 0.502 ms | 7.1× | 6,150,990 | 975,383 |
| 500 | 91.305 ms | 3.460 ms | 26.4× | 172,129,253 | 4,951,392 |

Run `go test -tags fts5 ./pkg/ontology -run '^$' -bench '^BenchmarkCatalogResolverScaling$' -benchmem -benchtime=200ms -count=3` with `GOMAXPROCS=4`; use the original-production overlay for the baseline. The large fixture improves about 26-fold and allocates about 35-fold fewer bytes. Some superlinear descendant work remains, so this result is not a claim that every catalog operation is linear. No new graph or end-to-end request latency claim is inferred from the catalog fixture.


## Reconstruct the comparison

Run from a repository checkout containing the recorded commits, using the same Go/CGO toolchain for both sides. The recorded host was Apple M4 Pro, Go 1.24.2. This creates disposable detached worktrees and carries only the retained benchmark into original production; no live vault/index is used. Compile both binaries before the quiet timing window, then execute sequentially without overlapping heavy work. Setup remains outside benchmark timing. Keep the generated output directory for review.

```sh
set -eu
comparison=$(mktemp -d /tmp/rzm-b05-compare.XXXXXX)
git worktree add --detach "$comparison/before" f5a711249465b31cb0c6bfea449f1fc94b46a7bc
git worktree add --detach "$comparison/after" fcf5c4313a2ba5fc2dfacefd0e2048d9e146c311
git show fcf5c4313a2ba5fc2dfacefd0e2048d9e146c311:pkg/ontology/catalog_scaling_test.go > "$comparison/before/pkg/ontology/catalog_scaling_test.go"
for side in before after; do
  (cd "$comparison/$side" && GOMAXPROCS=4 go test -mod=vendor -tags fts5 -c -o "$comparison/$side.test" ./pkg/ontology)
done
# Enter the quiet timing window only after both builds complete.
for side in before after; do
  (cd "$comparison/$side/pkg/ontology" && GOMAXPROCS=4 "$comparison/$side.test" -test.run '^$' -test.bench '^BenchmarkCatalogResolverScaling$' -test.benchmem -test.benchtime=200ms -test.count=3) > "$comparison/$side.txt" 2>&1
done
```

The baseline uses the original catalog, structural-edge, and projection implementations from `f5a71124`. Carrying the benchmark into that checkout replaces the historical three-file production overlay without introducing any new resolver code.

This recipe was source-checked during closure alignment and was not executed during the browser timing slot. Previously recorded overlay measurements, parity tests and full gates remain the executed evidence; fresh outputs require review before becoming final integrated performance claims.
