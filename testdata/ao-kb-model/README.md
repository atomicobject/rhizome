# AO binding compiler fixtures

All authored fixture values are synthetic. `account/` is an AO account with alpha and beta product roots, a project metamodel override and a selected account assertion. Beta deliberately reuses alpha's model/claim IDs. Alpha includes only its account rows and its original generated JSON source. The fixtures do not run AO ingestion, curation, generation, sync, or publication.

`intermediate.json` is the pinned adapter's captured version 1 export. Its `oracle.rows`, `oracle.edges`, and `oracle.unresolvedEdges` come from AO's reader. `expected.graphql` is the literal deterministic renderer output and compiles with Rhizome's actual RECORD schema compiler. Offline tests need no AO installation, Python package download, service, or network:

```sh
node --test testdata/ao-kb-model/compiler.test.mjs
GOCACHE=$PWD/.gocache go test -tags fts5 ./testdata/ao-kb-model
make test-content
make check-fast
```

The Node suite tests deterministic output, source/renderer fingerprint drift, separate setup/index states, failed validation, both live-rename failures, initial failure, mid-compile source changes, concurrent refresh, missing dependencies, missing native projections and typed-root pagination. The Python suite additionally exercises real AO resolution/filtering/merging and source attribution, including mapping overlap, scalar override, original account addresses after filtering, duplicate IDs, required versus optional generated overlays, unknown pins and unsupported wildcard/interleaved positional composition.

Run the live adapter tests with Python >=3.11 and PyYAML 6.0.3 installed only in scratch. Point `PYTHONPATH` at the pinned toolkit package; these tests import it read-only:

```sh
PYTHONPATH=/Users/colthorp/Developer/atomic-ai-prompt-toolkit/packages/kb-site \
PYTHONDONTWRITEBYTECODE=1 \
/private/tmp/aokb-laneM2-env/bin/python -W ignore::ResourceWarning \
  -m unittest discover -s testdata/ao-kb-model -p test_adapter.py
```

The warning filter covers the pinned AO reader's unclosed read handle in `vault_schema_version`; no AO source is changed. The adapter itself explicitly loads the pinned co-located modules, so an account directory on `sys.path` cannot shadow AO's `account.py`.

To recapture an intentional fixture/contract update, run the adapter against this fixture, save its JSON, then render it with `render(intermediate)`. Review both diffs. Do not regenerate either artifact to hide a parity failure. The intermediate's full metamodel includes the toolkit's standard trait and vocabulary metadata; all row content is illustrative.

## Deferred provider parity

`parity_test.go` is an executable harness, gated until Lane Y2 registers YAML and JSON providers:

```sh
AOKB_RUN_PROVIDER_PARITY=1 GOCACHE=$PWD/.gocache \
  go test -tags fts5 ./testdata/ao-kb-model -run TestProviderParity -v
```

Before integration, that command intentionally fails with the missing provider path. The ordinary Go command reports the parity case as skipped. After integration it projects the original YAML/JSON bytes through `noteformat.Runtime`, prepares the real RECORD schema, binds all hosts together and builds Rhizome's in-memory index. If YAML registration requires an explicit configured runtime, call `assertProviderParity(t, runtime)` from the package's integration test with Lane Y2's constructor.

The harness asserts:

| Contract | Expected evidence |
| --- | --- |
| Selection and uniqueness | Only selected product/account records; beta and untagged account rows cannot satisfy IDs |
| Physical identity | Every AO logical kind/key maps to the original host path and JSON-pointer address |
| Values | Complete normalized rows agree with AO; scalar record wrapping and grouped mapping fallback are explicit |
| Position | `position:1` belongs to the account source and `position:2` to alpha |
| Repeated authored refs | `service:S1.uses` retains `SHARED`, `SHARED`, `MISSING` in that order |
| Typed refs | `field:C1.email` resolves to collection C1 with qualifier `email` |
| Containment and direction | The actual indexed edges equal AO's canonical structural edge set, including container edges and inverse aliases |
| Unresolved refs | AO edges to absent records are removed from the edge oracle; Rhizome must emit `record_reference_unresolved` |

Generated `reads_fields` is excluded by name from the AO oracle. Knowledge evidence relations are a separate Lane K adapter integration gate because AO's structural `edges()` omits claim-target fields. Wildcard account membership is a named core gap in this checkout; its live test proves the adapter fails explicitly rather than claiming ready.

The fixture package lives under `testdata`, so Go's `./...` discovery intentionally excludes it. The coordinator should invoke the two offline commands explicitly from its integration test job. No CI job should run the live AO test or acquire a Python environment.
