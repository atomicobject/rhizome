# AO KB binding compiler design

Status: Lane M research proposal for SPEC-0133 Batch 4. Design only. No product implementation.

## Recommendation and scope

Use a starter-owned Python adapter around the pinned `kb-site` library. Export the full effective metamodel, expanded trait fields, source inventory, and compatibility diagnostics to a versioned JSON intermediate. Render that intermediate to the structured-data binding spelling chosen by Lane A. Keep AO values in their physical files. Do not port AO merge, curation, generation, or publishing logic.

Governing contract: `docs/specs/technical/format-neutral-embedded-nodes-and-ao-kb-adoption.md:82` (R2), `:94` (R3), `:118` (R5), `:128` (R6), `:254` (Batch 4). One explicitly selected product context per vault includes its visible account records. This document proposes the compiler contract; it does not settle SDL or require Lane A's unfinished work.

Scope reaches starter setup guidance, module actions/scripts, generated bindings and manifest, source ownership, stale admission, normal indexing, and compatibility fixtures. No new init execution hook, scheduled agent, editable model records, or simultaneous product browsing.

## Evidence notation and experiment

`AO/` below means `/Users/colthorp/Developer/atomic-ai-prompt-toolkit/`, pinned at `e9dce951ab4cd6aa9ccc734df96590bde25ccde9` (package version 3.137.0). `RZ/` means this Rhizome checkout. References are file:line pointers at the inspected revisions, not claims about later versions.

On 2026-11-06, copied the toolkit's Python package and its synthetic two-product test fixture into `/private/tmp/aokb-laneM-probe/`. Fixture construction followed `AO/packages/kb-site/tests/_helpers.py:81`: clinic model in alpha and beta, shared assertions, and the two-product overlay. Added only a scratch alpha override, `override: {flow: {fields: {lane_m: {type: text}}}}`. Copied the unmodified template `scripts/build-site` shim. No AO workflows besides read-only `metamodel resolve` ran.

The system Python lacked PyYAML and uv was absent from PATH. Installed dependencies exclusively in scratch. The shim's supported development override selected the copied engine:

```sh
PATH=/private/tmp/aokb-laneM-env/bin:$PATH \
UV_CACHE_DIR=/private/tmp/aokb-laneM-probe/uv-cache \
PYTHONDONTWRITEBYTECODE=1 \
KB_SITE_DEV=/private/tmp/aokb-laneM-probe/engine \
/private/tmp/aokb-laneM-probe/repo/scripts/build-site \
  metamodel resolve --product alpha --json
```

Exit 0; 67 kinds, 32 present. The overridden `flow.fields.lane_m` was `{type: text}`. An initially malformed scratch override, `override.kinds.flow`, returned exit 1 and `{"error":"metamodel: the project file overrides unknown kind 'kinds'"}`. Corrected it to AO's documented direct kind mapping before capture.

Exact output envelope and abbreviated contents (ellipsis denotes omitted entries, not JSON syntax):

```json
{
  "kinds": {
    "endpoint": {
      "file": "api.yaml#resources[].endpoints",
      "collection": "resources", "child": "endpoints", "key": "id",
      "fields": {
        "roles": {"type": "ref[]", "to": "user"},
        "screens": {"type": "text[]", "to": "screen", "edge": "serves"}
      },
      "role": null
    },
    "token": {
      "file": "design-system.yaml#tokens{}[]",
      "collection": "tokens", "child": null, "key": "id",
      "fields": {"value": {"type": "any"}}, "role": null
    },
    "assertion": {
      "file": "kb:assertions", "collection": null, "child": null,
      "key": "id", "fields": {}, "role": "claim"
    }
  },
  "ladder": {
    "tiers": [{"id":"motivation"}],
    "bands": [{"id":"why","tier":"motivation","phase":"orient",
               "kinds":["driver","goal","outcome"],"viewpoints":[]}]
  },
  "edges": [["driver","motivates","goal","served-by"]],
  "present": ["app"],
  "expected": {"phase":null,"expected":[],"absent":[]}
}
```

Every kind has exactly `file`, `collection`, `child`, `key`, `fields`, `role`. Actual tiers, bands, fields, edges, and present lists are longer than the excerpt. `expected.absent` entries, when present, are `{kind,file,phase}`. Without `--json`, output is only counts. CLI implementation: `AO/packages/kb-site/kb_site/cli.py:432`, `:465`; expected shape: `AO/packages/kb-site/kb_site/kits/core/_build/metamodel.py:3501`. CLI regression evidence: `AO/packages/kb-site/tests/test_metamodel_cli.py:35`.

Additional scratch assertions exercised the pinned library with a custom two-kind metamodel and account/product `lane.yaml` files. All passed: account-first positional IDs `1`, `2`; product scalar wins with warning; overlapping mapping row retains account `extra: inherited` and product `name: Product`; `groups{}[]` preserves group key and list order. Scratch directories were removed after capture. The toolkit was read only; unrelated toolkit working-tree changes were preserved.

## What resolve contains and what it omits

| Compiler requirement | Resolve evidence and gap |
| --- | --- |
| Kinds, project overrides | Effective kinds and direct field overrides are present. Full kind metadata, traits, ID families, modules and operation provenance are omitted. |
| Selectors | Raw `file` preserves the complete selector including `rows`, `rows[]`, `rows{}`, `groups{}[]`, and nesting. `collection`/`child` strip suffixes and intermediate segments; never compile from those two convenience fields. |
| Identity | Raw `key` preserves field, composite template, and null. Mapping-key precedence and null numbering are executable library rules, not exported rules. ID constraints and positional durability are absent. |
| Titles | No title rule. AO graph fallback is `name`, `t`, `term`, then logical ID for thing rows; claim model rows use `name`, `t`, ID. Kind `label` is omitted and is not a row title. |
| Containment | Explicit `container` omitted; inferred containment omitted. `child` is insufficient and not a parent identity rule. |
| Fields/references | Own field declarations retain `type`, `to`, `edge`, `ordered`, nested `items`, etc. Trait fields are absent. Strict/tolerant behavior, inverse-edge vocabulary and target matching behavior need library/schema interpretation. |
| Edges versus validation | Own fields with `to` but no edge remain visible and must validate without drawing edges. `edges` lists only declared field edges, including nested item declarations and implicit `parent` edges. It omits trait and containment edges. |
| Physical sources/product scope | No selected product, account root, source paths per record, row addresses, contributions, effective document, or duplicate locations. A relative model filename alone is not provenance. |
| Generated JSON | `rule.file` is `../engine-blocks/rows.json#rules`; jurisdiction also targets that file. Filename survives, but no generated-source metadata, digest or producer freshness is exported. |
| Freshness/setup | No toolkit revision, input fingerprints, compiler version, bindings generation, or index status. `present` is not setup readiness. |
| Knowledge | `assertion` is `kb:assertions` with empty fields. Continue the separate versioned knowledge-convention adapter from Batch 2. |

Selectors and identity: `AO/packages/kb-site/kb_site/kits/core/_build/metamodel.py:1149`, `:1232`, `:1248`, `:1286`. Trait expansion: `:1596`. Edge export: `:3549`; actual row edges: `:2129`; tolerant target matching including optional `match_name`: `:2112`. Title fallback: `AO/packages/kb-site/kb_site/kits/core/_build/trace.py:844`, `:880`. Standard conventions: `AO/packages/kb-site/kb_site/kits/core/_build/standard-metamodel.yaml:11`; generated sources: `:1304`, `:1346`.

## Narrow adapter and Python execution

Import the pinned package's engine module using the same by-path mechanism as `AO/packages/kb-site/kb_site/cli.py:262`. Load schema and `resolve_vault(selected_root)` once, retain the entire resolved dictionary, and call `_fields_of`, `_find_container_for`, and existing row/duplicate helpers through one explicitly versioned compatibility adapter. Private helper use is intentional and isolated. Probe helper availability and captured behavior; do not silently fall back to reduced CLI JSON.

`resolve_vault` loads the selected root's project metamodel over the engine standard; module resolution and project merge already live in AO (`AO/packages/kb-site/kb_site/kits/core/_build/metamodel.py:381`, `:461`). Do not overlay the account metamodel again. AO mirrors shared configuration into product roots during its own setup (`AO/packages/kb-site/kb_site/product.py:13`, `:218`); the compiler reads the files AO would currently read and reports missing or inconsistent mirrors without repairing them.

Use `account.row_paths`, `account.account_doc`, `account.merge_doc` / `metamodel.model_doc` for filtering and effective documents. `rows()` alone is insufficient: duplicate IDs overwrite rows, and normalized rows lack physical locators. Check `duplicate_rows` and retain occurrence inventories before collapsing IDs (`AO/packages/kb-site/kb_site/kits/core/_build/metamodel.py:1341`, `:1368`). Never expose AO's synthetic `_scope`, `__container__`, or key-error markers as authored values.

Package environment: `AO/packages/kb-site/pyproject.toml:5` declares Python >=3.11, PyYAML >=6 and `kb-site = kb_site.cli:main`; `uv.lock` supplies the project environment. The normal repository shim uses manifest `siteEngine.source/version`, resolves release tags to SHA, then `uvx --from <source>@<ref>#subdirectory=packages/kb-site kb-site ...`; `KB_SITE_DEV` uses `uv run --project <path>`. See `AO/plugins/ao-kb/references/templates/scripts/build-site:25`, `:31`, `:60`.

Proposed portable invocation is an argv array, not a shell string: `uvx --from <verified pinned spec> python <module>/scripts/compile-bindings.py --root <account> --product <slug> --out <stage>`. For an explicitly configured dev checkout, use `uv run --project <verified-package-dir> python <adapter> ...`. Reuse the shim's pin resolution contract, including annotated tags, with parity fixtures; do not search arbitrary venv directories or require a global Python package. Adapter imports must resolve inside the selected package. Network/package acquisition failures are prerequisite failures and keep the old set.

Check executable availability, Python/PyYAML versions, package version and immutable source revision, manifest schema compatibility, standard/schema/module hashes, adapter helper contract, and actual loaded module location. Baseline is the inspected commit plus 3.137.0, not any package claiming the same version. Record development-source hashes and reject unexpected dirty engine content unless explicitly supported. Current AO `SCHEMA_VERSION` is 8, with its own compatibility gate (`AO/packages/kb-site/kb_site/kits/core/_build/metamodel.py:158`, `:188`); call that gate rather than inventing migrations. Never run sync, ingestion, generation, curation or publish to obtain schema information.

## Account/product merging and provenance

AO filters account rows at the effective metamodel's row positions. Only rows tagged with the selected product or `*` survive; untagged account rows reach no product. Nested rows ride with retained containers. Account files outside the model directory, unknown row-position files, and non-mapping account documents are withheld. Non-row config blocks stay intact. Sources: `AO/packages/kb-site/kb_site/kits/core/_build/account.py:202`, `:233`; parity/union fixtures: `AO/packages/kb-site/tests/test_account_row_filter_parity.py:1`, `AO/packages/kb-site/tests/test_model_union.py:65`, `:119`, `:145`, `:203`.

Effective documents use account-first sequence concatenation, recursive mapping merge, and product scalar preference with warnings on unequal values. Null takes the other value; absent product or empty top-level product mapping takes the account document. This differs from metamodel extension merging, whose lists replace. Use the existing functions for both, never conflate them (`AO/packages/kb-site/kb_site/kits/core/_build/account.py:266`; metamodel extension merge: `AO/packages/kb-site/kb_site/kits/core/_build/metamodel.py:338`).

Proposed attribution check:

1. Capture both physical documents with provider addresses and original bytes. Ask AO for the filtered account document and actual effective document. Retain original account addresses when filtering removes preceding rows.
2. Inventory selected row occurrences in each source using the effective selectors. Track nested container source and mapping keys; do not join rows solely by ID, since IDs can collide. List concatenation retains occurrence origins and adds account-length offsets only for effective positions.
3. For any mapping overlap at or inside a selected row, inspect surviving authored values against the two source records. If contributions cannot be proved to come entirely from one record, reject it. The minimum implementation may conservatively reject every non-null overlapping account/product mapping row, even when values coincide. State that compatibility limit explicitly rather than fabricating a locator. Ordinary config scalar conflicts outside records are warnings, not row incompatibilities.
4. Emit `unsupported-mixed-origin-row` with kind, effective key, both repository-relative source paths, and both row addresses; fail the complete binding refresh. Example: `map:overlap`, account `artifacts/_assets/_model/lane.yaml#map.overlap`, product `products/alpha/artifacts/_assets/_model/lane.yaml#map.overlap`. The scratch case retained product `name` and account `extra`, proving why `_scope: account` alone is insufficient.
5. Compare supported rows, IDs, values and references against AO's reader; reject duplicate IDs with every occurrence location. Do not consume `rows()`'s last-wins output as proof of uniqueness.

This checks attribution around AO's merge; it does not calculate replacement effective values. The first milestone does not promise per-value mixed-source provenance. Generated field overlays such as endpoint `reads_fields` also add a second source (`AO/packages/kb-site/kb_site/kits/core/_build/metamodel.py:1445`, `:1491`). Keep their generation path and freshness explicit; treat an unattributable overlay as an unsupported row case, never quietly fold it into an authored YAML record.

Identity evaluation follows `_leaf_id`: a mapping key wins except at `{}[]`; templates format the row without slugging/case folding; an explicit child ID remains unprefixed; null keys count from one per container, or globally without a container. Account-first numbering is applied to the effective collection, while the physical locator stays in the original account/product file. Positional references carry the captured source revision and effective-order revision; insertion/reorder invalidates them. If Rhizome cannot express that, durable copy-link is unavailable. Shared account records keep one physical identity and a separate selected-product logical identity context.

## Product selection

AO's registered slugs come from account `.kb/config/vault.md`; actual product mode requires at least one registered root with `products/<slug>/.kb/config/product.yaml`. `productMode` alone is not the test. AO selection precedence is explicit flag, `KB_PRODUCT`, `.kb/local.yaml`, cwd. Evidence: `AO/packages/kb-site/kb_site/product.py:68`, `:79`, `:87`, `:116`; CLI root routing: `AO/packages/kb-site/kb_site/cli.py:188`.

The starter persists one explicit root/product selection and always passes it. Do not let ambient cwd or session defaults change a compiled set. Validate registration, marker slug and account pointer. A multi-mode registry without roots requires explicit setup diagnostics instead of guessing a combined model. A direct product root can establish the explicit context only after the same validation. Single mode is a recorded null product.

Restrict typed AO admission, aliases, recipes and references to that context, including shared account evidence selected by AO's note filters (`AO/packages/kb-site/kb_site/kits/core/_build/account.py:104`, `:140`). Leave ordinary include rules intact. Other-product source search remains possible; its colliding IDs cannot satisfy selected-product references. Read-only ownership covers known AO sources in every product, independently of typed selection.

## Format-neutral JSON intermediate

Use a versioned deterministic document, with paths relative to the Rhizome vault and explicit root anchors. Example structure below is proposed, not implemented SDL:

```json
{
  "formatVersion": 1,
  "compiler": {"version":"1", "knowledgeAdapterVersion":"1"},
  "toolkit": {"version":"3.137.0", "revision":"e9dce951..."},
  "context": {"accountRoot":".", "product":"alpha", "productRoot":"products/alpha"},
  "fingerprint": "sha256:...",
  "inputs": [{"path":".kb/config/vault.md", "hash":"sha256:..."}],
  "sources": [{"id":"api-product", "path":"products/alpha/artifacts/_assets/_model/api.yaml",
               "format":"yaml", "readOnly":true, "origin":"product"}],
  "kinds": [{
    "name":"endpoint", "sources":["api-product"],
    "selector":"resources[].endpoints",
    "identity":{"mode":"field", "field":"id", "scope":["context","kind"]},
    "title":{"firstNonEmpty":["name","t","term"], "fallback":"logicalId"},
    "containment":{"kind":"resource", "identityPrefix":false},
    "fields":[{"path":["roles"], "type":"ref[]", "preserveOrder":true}],
    "references":[{"path":["roles"], "to":["user"], "mode":"strict", "edge":null}]
  }],
  "diagnostics": []
}
```

Also represent mapping-key, bounded template and positional identities, ID patterns, scalar rows, group-key evidence, nested item paths, ordered/repeated authored references, tolerant matching and `match_name`, typed-ref allowed kinds/pseudo-targets, normalized forward edge direction, and validation-only references. Retain unsupported declaration details in diagnostics, not silently dropped fields. Preserve missing/null/empty distinctions in source facts, not invented defaults. The adapter supplies full declarations; the renderer translates only supported semantics and rejects others.

A source entry distinguishes product/account/generated origin, exact path, content revision, optional generator metadata and selected row membership. Physical occurrence/containment evidence is separate from logical identity. Dynamic values and row membership must refresh through ordinary indexing against AO's effective-document rules, not remain frozen in the schema artifact. If the Rhizome runtime cannot consume the source-composition/attribution contract, that is a named Batch 4 integration gap, not permission to index files independently. Renderers must not generate a copied effective YAML/JSON document as the new source of truth.

## Freshness, staged publication, and actions

Fingerprint canonical bytes plus absent-file sentinels, not mtimes alone: compiler/IR/renderer and knowledge-adapter versions; Rhizome binding/provider contract version; immutable toolkit source and package version; engine schema/standard and enabled module files; full selected root/product and registry/marker configuration; manifest pin; account and selected-product metamodel files including overrides; relevant mirrored config and its ownership declarations; binding source/ownership configuration. Hash exact input paths as well as content so moves and product changes invalidate the artifact. AO's `content_hash` excludes operation logs and is only 16 hex characters: useful evidence, insufficient as the whole binding fingerprint (`AO/packages/kb-site/kb_site/kits/core/_build/metamodel.py:469`).

Track a separate projection-input revision for authored YAML/JSON/Markdown and any generated overlays, including missing generated files. Ordinary edits trigger ordinary refresh; they need not recompile identical declarations. Re-evaluate mixed-origin support and effective positional order on source changes before publishing projected rows. AO saved analyses require their own recorded freshness, not a binding hash. Include phase/expected-kind configuration only in the diagnostic input revision unless it changes compiled behavior. Hash all files actually consumed by the adapter, including newly discovered inputs, and recheck the snapshot before commit.

Publish procedure: acquire one checkout-local refresh lock; capture inputs; compile into a sibling temporary generation; validate JSON contract, complete rendered binding set, references and single-source attribution; verify input hashes still match; write generation manifest and content hashes. Atomically replace a single active-generation descriptor selecting the immutable set. Never rename individual live bindings in succession. Generic discovery must load exactly the active set and ignore staging and previous generations. If the existing binding loader cannot select one generation atomically, add that bounded integration seam before claiming atomicity.

Use existing Rhizome schema admission and index single-writer publication. The pointer swap is not proof of index readiness: index activation must bind to the same generation and input revision. On compile/validation failure keep the previous set, write stale/error status atomically, and retain ownership. Check freshness from actual inputs when admitting reads/index work so failure to write the status file cannot leave a false ready state. Previous data can be shown only with explicit stale status; initial failure has no ready model. Successful refresh clears the error; only a successful normal index reports index ready. Crash before swap leaves old active generation; crash after swap leaves bindings ready/index pending. Concurrent refresh cannot mix generations.

Reuse starter module packaging and argv actions (`RZ/pkg/app/cli/init/templates/starters/README.md:3`; `RZ/pkg/app/cli/init/templates/starters/compounding/module/actions/health.yaml:1`). Put `check` and `refresh-bindings` under module actions and compiler code under module scripts; no task is needed. Compounding's task check is an example of deterministic prerequisite reporting, not a reason to schedule compilation (`RZ/pkg/app/cli/init/templates/starters/compounding/module/tasks/mine.yml:7`; `RZ/pkg/app/cli/init/templates/starters/compounding/module/scripts/task-check.mjs:64`). Its temp-and-rename pattern is file-atomic only (`RZ/pkg/app/cli/init/templates/starters/compounding/module/scripts/lib/store.mjs:19`). Do not import its repository-shared proposal state: binding state belongs to this checkout and vault.

`check` is read only: report dependencies, explicit selection, managed installation, active generation, current fingerprint, last failure, and index generation/revision. Proposed exit 0 means current bindings and prerequisites; exit 1 means incomplete/stale/error, with machine-readable reasons and the exact next command. Index readiness is a separate field, never inferred. `refresh-bindings` performs preflight, ownership registration and staged refresh; exit 0 certifies bindings only, exit 1 leaves old bindings with the diagnostic. Neither action invokes indexing implicitly.

Setup guidance runs normal `rzm init` with the starter, preflight/root/product selection, independent source-ownership registration, refresh-bindings, then normal index. Report three independent statuses: `starter installed` means managed assets exist; `bindings ready` means complete current active set; `index ready` means the current generation and source revision were indexed successfully. `init --check` certifies managed assets only. Direct CLI documentation provides this same sequence. Initial compile failure must already have established ownership; failed dependency preflight reports ownership setup incomplete and must not claim protected/ready sources. Ejection is explicit and separate from stale/error handling.

## Required implementation fixtures and verification limits

| Fixture | Required result |
| --- | --- |
| Project override | Direct `override.flow.fields` reaches IR and rendered bindings; trait overrides, module additions/removals and unknown versions are covered. |
| Product with shared account evidence | Alpha sees alpha and `*` rows/notes, excludes untagged account model rows, and uses original shared source locations. |
| Unselected product with colliding IDs | Beta duplicates claim/model IDs; alpha reference resolution never selects beta, while ordinary source search can still find it. |
| Mapping overlap | Account `map.overlap.extra` plus product `map.overlap.name` fails the whole set with kind, key, both paths and addresses; no fabricated locator. |
| Scalar override | Product scalar beats account scalar with AO warning; outside a selected row it is not automatically a mixed-row error. |
| Account-first positional numbering | Shared account record gets `1`, product record `2`; insertion/reorder invalidates old positional locator and updates containment consistently. |
| Selector and identity matrix | Named/dotted, list, mapping, grouped lists, nested/scalar rows, explicit child ID, mapping precedence, composite template and missing template field. |
| Reference semantics | Strict/tolerant, optional name matching, inverse/parent/containment edges, validation-only fields, nested items, typed refs, repeated ordered values and duplicate IDs with all locations. |
| Generated sources | Existing JSON target resolves at original path; absent/stale output is reported without invoking its generator; multi-source overlays fail explicitly if unsupported. |
| Publication/recovery | Initial failure, stale installed starter, failed refresh retaining old set/ownership, crash around swap, concurrent refresh, mid-compile input change, source-only change introducing overlap, successful refresh followed by index. |
| Lifecycle/dependencies | Init/re-init/upgrade/edited-file preservation/ejection; missing uv/Python/package, wrong toolkit/schema, product switch, root move and configuration deletion. |
| Parser parity | Quoted/numeric IDs, dates, bool-like words, null/empty values and unsupported YAML features compared to pinned AO reader. |

Research verification completed: real shim resolve against copied synthetic fixtures, project override observation, focused pinned-library merge/selector/identity assertions, and source inspection. Rhizome-integrated retrieval and documentation validation were unavailable because this checkout lacks `bin/darwin/rzm`; `./scripts/rzm validate` failed at that missing launcher target. No Rhizome projection, renderer, atomic activation, or action implementation was tested in this design phase.
