# AO model binding compiler

This directory is Lane M2's complete installable compiler. Node uses only its standard library. `uv run --no-project --with <pinned kb-site package> --with pyyaml==6.0.3 python adapter.py` supplies Python >=3.11 and AO's `pyproject.toml` environment without creating or changing the toolkit's `.venv`. The default package is the toolkit Git source at `e9dce951ab4cd6aa9ccc734df96590bde25ccde9`, version 3.137.0. `--toolkit /absolute/path/to/packages/kb-site` selects a local checkout at that revision. The adapter checks the actual imported engine, schema, standard and module bytes against `toolkit-pin.json` on every call. It never invokes AO ingestion, curation, generation, sync, or publication.

Run these from the Rhizome vault's AO account root after ordinary starter installation and source-ownership registration:

```sh
node .rhizome/modules/ao-kb/bindings/cli.mjs refresh-bindings --product alpha
node .rhizome/modules/ao-kb/bindings/cli.mjs check
node .rhizome/modules/ao-kb/bindings/cli.mjs index
```

For a single-product-layout vault, use `refresh-bindings --single`. Product roots always require an explicit product. Refresh persists it in `bindings/selection.json`; check and index never use `KB_PRODUCT`, `.kb/local.yaml`, or the caller's working directory to change context. The installed module locates its vault root. `--root` explicitly supplies that same AO/Rhizome root. An AO account nested inside a different Rhizome vault is outside this compiler's current root contract.

`RZM` optionally names the Rhizome executable. `uv` must be on PATH. Refresh may acquire the pinned Python package; check runs uv offline and reports missing cached dependencies. Python/package acquisition failure leaves the last bindings intact. Source inputs stay at their original locations. The compiler never creates an effective YAML/JSON copy or alters `.rhizome/source-ownership.yml`.

## Lane K integration

Add these families to the existing starter's `template.yaml` `installedAssetTypes` list:

```yaml
  - scripts
  - actions
```

The AO KB starter is an explicit add-on. Keep these entries in its metadata, alongside Lane K's other asset families:

```yaml
requires:
  - core
optionalAddon: true
```

Retain the starter's other existing dependencies and asset families; the compiler adds no dependency beyond core. The `module/bindings/**` files install automatically through the existing module file walker. No execution hook, task, or loader change is needed.

Add `module/actions/check.yaml`:

```yaml
description: Check AO dependencies, selected product and binding freshness
run:
  command: [node, "{module}/bindings/cli.mjs", check]
timeout: 3m
```

Add `module/actions/refresh-bindings.yaml`:

```yaml
description: Compile the selected AO effective model into read-only Rhizome bindings
inputs:
  product: {kind: string}
  single: {kind: bool}
  toolkit: {kind: string}
run:
  command: [node, "{module}/bindings/cli.mjs", refresh-bindings]
timeout: 5m
```

Actions supply their declared input object on stdin. The CLI also accepts flags for direct use. The first product action is `rzm ao-kb refresh-bindings --product alpha`; later refreshes reuse selection. Setup must establish ownership for every product's AO sources before refresh. Setup should call the explicit `index` script above for a receipt, or report normal indexing separately as unverified. No `index` module action is required.

The generated SDL is `module/ontology/ao-model.generated.graphql`. It is compiler-owned runtime output, so do not ship a static template at that path. Ensure new source include patterns cover only the inventory's paths and preserve unrelated include rules. Keep the compiler files and persisted selection in Git. Ignore `bindings/refresh.lock/`, `bindings/*.tmp`, `bindings/*.previous`, and `bindings/index-receipt.json` in the target repository; the generated SDL and manifest may be committed as disposable derived artifacts. Never ship a live selection or manifest in this template directory.

Views consume `bindings/manifest.json` and the check action result. `manifest.status: ready` is the last successful refresh, not proof that current files still match. Use the check's computed `status`/`bindingsReady` for current freshness; a read-only check does not rewrite the saved manifest. Report `starterInstalled`, `bindingsReady`, and `indexReady` independently. The index wrapper runs ordinary `rzm index`, checks every concrete model root against the expected source counts through paginated typed queries, verifies unchanged inputs, then writes `index-receipt.json`. A direct index without that receipt remains unverified by the compiler check. Provider-level projection readiness is the coordinator's Lane Y2 integration gate.

## Version 1 JSON contracts

`adapter.py --root <account> [--product <slug>]` emits the intermediate on stdout. A dependency/root/pin error emits a diagnostics envelope and exits 1. Compatibility errors retain a complete intermediate, then make the renderer refuse publication.

| Intermediate field | Shape |
| --- | --- |
| `formatVersion`, `compilerVersion`, `providerContract`, `knowledgeAdapterVersion` | `1`, `"1"`, `"RECORD-1"`, `"1"` |
| `toolkit` | `{version, revision, schemaVersion, pyYamlVersion}` |
| `context` | `{accountRoot: ".", product: string or null, productRoot: relative path}` |
| `effectiveMetamodel` | Complete AO-resolved dictionary, including operation/module provenance and original declarations |
| `kinds` | `[{name, declaration, fields, idPatterns, selector, container, sources}]`; `fields` includes expanded traits; `sources` contains inventory IDs |
| `sources` | `[{id, kind, path, format, origin, compositionOrigin, readOnly, membership, sourceOrder, revision}]`; source revision is SHA-256 or null for absent files |
| `knowledgeSources` | AO-selected account note inventory, `[{path, kind, origin}]`; discovery and byte hashes participate in freshness |
| `inputs` | Sorted `[{path, hash}]`; repository-relative names, exact byte SHA-256, null absent-file sentinel |
| `referenceVocabulary` | AO canonical/reverse edge map and typed-reference pseudo-target configuration |
| `records` | `[{kind, key, path, address, origin, container}]`; original source JSON-pointer address, distinct from effective logical identity |
| `diagnostics` | `[{code, severity, message, ...evidence}]`; severity `note` or `error`; mixed-origin errors include kind, key, both paths and effective address |
| `oracle` | `{rows, edges, unresolvedEdges}` from AO's actual reader; generated `reads_fields` excluded by name |

The manifest has `formatVersion: 1`, `compilerVersion: "1"`, `providerContract`, `status: ready|stale|error`, `toolkit`, `context`, `fingerprint`, `inputs`, `sdlHash`, `sources`, `knowledgeSources`, `typeMap`, and `diagnostics`. `typeMap[kind]` is `{interface, types}`. `fingerprint` hashes inputs, compiler/renderer/pin bytes, selected context and root, other active SDL, and project configuration. Both schema inputs and source bytes are checked again before publication. Moving the root intentionally invalidates the manifest. A first failure may have only version/status/diagnostics; a failed later refresh preserves the previous successful generation metadata and appends failure diagnostics.

`selection.json` has `{formatVersion: 1, accountRoot: ".", product: string|null, toolkit: absolute package path|null}`. Local toolkit paths require adjustment on another machine. The default remote pin is portable. `index-receipt.json` has `{formatVersion: 1, fingerprint, sdlHash}` and is written only after the explicit normal-index wrapper succeeds, projected source counts agree, and compiler inputs are unchanged.

Publication takes one checkout-local lock, validates the complete SDL through `rzm ontology query-schema` in a disposable vault containing the other schemas, and stages both output files. Each live file is replaced with temp-and-rename. A stale manifest is published before the SDL changes, so interruption between renames cannot advertise a ready mismatched pair. Recoverable rename errors restore the old SDL and metadata, then mark stale/error. `.previous` copies are diagnostic only and never use a `.graphql` extension. Remove an abandoned lock only after confirming no compiler/index wrapper is running.

## Compatibility limits and coordinator requests

- **Lane Y2: wildcard membership.** Current core `PreparedDocument.membershipIncludes` checks only exact `includes`. AO's filter also admits `products: ["*"]`. `ao_membership_wildcard_unsupported` prevents a false ready set when those rows occur. Core needs an explicit bounded wildcard membership contract before that diagnostic can be removed. Child bindings inherit admission through their selected container; they do not independently filter the child's `products`.
- **Knowledge references.** Model fields targeting `kb:` kinds remain authored ID fields with `ao_knowledge_reference_external` notes. The current core RECORD resolver indexes RECORD candidates only. Lane K must connect the knowledge adapter's evidence relations to model records through a supported shared resolver; this compiler does not fabricate a Markdown target binding. This is separate from the structural AO `edges()` oracle, which excludes claim targets.
- AO mapping merges are product-key-first; lists concatenate account-first. The intermediate preserves AO's effective order and original attribution. RECORD `sourceOrder` supplies account-first list numbering. Positional grouped mapping compositions that require interleaved cross-file order fail with `ao_source_order_unsupported`.
- Overlapping mapping records are conservatively rejected, even if their values coincide. `ao_mixed_origin_record` includes the kind, effective key and both source paths. Configuration scalar conflicts outside selected rows remain AO compatibility notes.
- Generated `reads_fields` is omitted by name, with a note. If the declaration requires it, setup fails with `ao_required_generated_field`. Generated JSON stays bound to its original path. A missing optional generated file is fingerprinted as absent.
- Complex native fields and AO-only validation algorithms remain inspectable source/metadata with explicit notes. Dates and non-native enums remain authored text. The compiler does not execute AO truth, curation, or analysis algorithms.

## Verification

The synthetic fixture, offline intermediate, literal SDL and all tests live at `testdata/ao-kb-model/` in the source repository. See its README for commands and the deferred provider parity harness. No test or fixture is embedded in this starter directory.
