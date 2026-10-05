---
summary: "B15 evidence and persistence contract for avoiding identical configuration replacement during indexing."
---

# Configuration save idempotence

Plain indexing saves selected code and embedding configuration after completing its work. Before B15, even byte-identical YAML replaced `.rhizome/config.yml`. A live server observes this as a configuration change and reconciles owned files, metadata and ontology, invalidating browser queries.

## Change and boundaries

The typed configuration writer compares the final merged, serialized YAML with the existing file. Equal bytes return without replacing the file. Different bytes and comparison read failures follow the existing atomic writer. This preserves existing write error behavior and permissions without changing the generic atomic-write helper. Main configuration and workflow persistence remain independent: an unchanged main file does not skip a changed workflow file. Empty workflow cleanup retains its existing behavior.

The first save can still normalize formatting. Unknown fields and retained comments participate in the comparison. This is a byte comparison, not a semantic YAML comparison or a new concurrency guarantee. Genuine configuration changes still reach the existing watcher/reconciliation path.

## Before evidence

The measured binary was built at `e1a31fe346da3ac7bdc0e82fc8c59006bd40de50`, SHA256 `166b23cc2232b6e6e8d0a5fb56bc8fbe3a265e553eabcbef000398b2403b65b2`. Source inspection at `61d9730b86fe9790f5163e783c52f7350bc8f103` differs only in delivery documentation.

A disposable fixture contained one Markdown note, one Go source and disabled embedding providers. Three unchanged plain index runs retained identical config bytes but changed inode and modification time. Source shows two configuration-save calls per disabled-provider run; the observation establishes at least one replacement, not a measured syscall count.

A separate settled observation began with no pending dirty paths and completed runtime epoch 2. One unchanged index produced seven SSE signals during a 15-second observation window: one `index.changed`, two `validation.invalidated`, one `schema.invalidated`, two `capabilities.invalidated` and one `index.invalidated` resync. Completed epoch advanced to 4. Startup events were recorded separately. Earlier short observation windows overlapped watcher throttling and are not independent invalidation samples.

Retained evidence: `/tmp/engine-next-write-probe/evidence.json`, `/tmp/engine-next-write-probe-isolated/evidence.json`; reproducer `/tmp/engine-next-write-probe-isolated.py`. These are local run artifacts, not required repository fixtures.

## Verification

The new persistence regression failed before the guard because an unchanged save replaced the config inode. With the guard, focused config and plain-index tests pass. Coverage checks stable bytes, file identity, timestamps and permissions after normalization, retained comments and unknown keys in both files, and a genuine workflow-only change. The CLI regression proves that the second plain index leaves configuration identity and timestamp unchanged while disabled providers remain disabled.

Independent Standards and Spec reviews found zero issues. Full `make check` passed with bounded Go parallelism (four packages/four test workers, two check jobs), including race-enabled unit/integration tests and web checks. `make build` passed; `./scripts/rzm validate` reported zero issues.

## After evidence

The full-UI B15 binary, built from the base above plus the reviewed config guard, has SHA256 `953fab4dc10afac59b05a4f03ccc5c88584a9ae8f55f5c97e26b192b6b50350c`. The same fixture recipe and 12-second startup settling/15-second observation windows were used. Unchanged indexing preserved config bytes, inode and modification time and emitted **zero SSE signals**. Pending dirty paths remained zero, and neither status snapshot contained a current or completed watcher epoch. CLI indexing still refreshed persisted validation state; B15 does not remove indexing work itself.

As a positive control, adding a real note exclusion to the config produced the expected seven signals and completed watcher reconciliation (epoch 2), ending with no pending dirty paths. The local server was stopped after observation. Evidence: `/tmp/b15-live-after/evidence.json`, reproducer `/tmp/b15-live-probe.py`, and measured tracked patch `/tmp/b15-measured.patch`. The positive control edits config bytes directly; persistence tests separately verify genuine typed saves, permissions and workflow updates.

The evidence concerns avoided file replacement, reconciliation and invalidation. It does not measure HTTP request count, rendering latency, embedding cost or complete code reparsing. Timing claims are intentionally absent because other workers were active.
