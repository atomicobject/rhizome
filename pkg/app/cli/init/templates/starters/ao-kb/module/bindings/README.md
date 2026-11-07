# AO KB binding compiler

Node uses its standard library. The model adapter imports the pinned AO `kb-site` reader, version 3.137.0 at `e9dce951ab4cd6aa9ccc734df96590bde25ccde9`, through `uv`. It never invokes AO ingestion, curation, generation, sync, or publishing. Authored Markdown, YAML and JSON stay in place and remain AO-owned.

## Setup and recovery

Run from the AO account root, which must also be the Rhizome vault root:

```sh
rzm init --add-addon ao-kb
rzm init --check
rzm ao-kb check
rzm ao-kb refresh-bindings --product alpha
node .rhizome/modules/ao-kb/bindings/cli.mjs index
rzm ao-kb check
```

First init may use `rzm init --workflow none --addons ao-kb`. For a single-mode KB use `--single true` on the module action, or `--single` on the direct Node CLI. The direct fallback for an untrusted repository is `node .rhizome/modules/ao-kb/bindings/cli.mjs refresh-bindings --product alpha`; `node .rhizome/modules/ao-kb/scripts/check.mjs` runs the same checks without granting trust. Trust remains a human decision.

Install `uv` on PATH before model refresh. `RZM` may name an absolute Rhizome executable. `--toolkit /absolute/path/to/packages/kb-site` selects a read-only local checkout at the pinned revision. Otherwise uv acquires the pinned package. It never creates a toolkit `.venv`. `check` runs the adapter offline and names uncached or missing dependencies.

Refresh requires AO root `.kb/config/vault.md`, schemaVersion 8, supported manifest `siteEngine` pins, a consistent product registry and product markers, and an explicit selected product in product mode. Single-mode registries may list their product without nested roots. Selection persists in `bindings/selection.json`; ambient `KB_PRODUCT`, `.kb/local.yaml` and cwd never change it. Source ownership must already cover every knowledge type root and compiled model source. Add custom AO locations to `.rhizome/source-ownership.yml` before refreshing; the compiler never modifies it. The Node preflight certifies the standard block-source policy with scalar paths using `*`, whole-segment `**`, and `?`; aliases, brace/class patterns, and uncertain YAML fail coverage rather than claiming protection.

Keep unrelated note includes. When adding include paths after an initial refresh, refresh again because configuration participates in the fingerprint. Add `.kb/curation/_conflicts.md` explicitly and the exact model source paths listed in the manifest, including generated JSON. The index wrapper runs real `rzm index`, checks each concrete model root against the current AO source inventory through paginated typed queries, then writes an index receipt. A plain index has no compiler receipt and remains unverified by this check.

`init --check` certifies managed assets only. The check independently reports `starterInstalled`, `bindingsReady`, `knowledgeReady`, `modelReady`, `bindings.knowledge`, `bindings.model`, and `indexReady`. Installation alone is incomplete setup. Before the first refresh in a multi-product account, the static knowledge types are unscoped; check names the exact refresh command rather than claiming that product scope is ready.

Fix the named prerequisite or compatibility diagnostic, refresh, then index. A failed model stage keeps the current knowledge admission and previous model generation, advertising model setup as incomplete. A failed knowledge stage retains its previous generation. Do not delete authored sources to repair bindings. Remove `bindings/refresh.lock/` only after confirming no refresh or index wrapper is running.

## Knowledge scope

Static knowledge SDL stays in `module/ontology/`, so installed views and recipes validate before refresh. Node independently generates `ontology/ao-knowledge.generated.graphql`: a comment in single mode; four `extend type ... @admit(matches: ...)` definitions in product mode. Admission intersects the existing root selectors, preserving every field, embedded type and `ConflictRegistry` path.

AO keeps knowledge at account `<type>/<product>/`, including `artifacts/<phase>/<product>/` and `artifacts/<product>/`, rather than under `products/<product>/`. A registered product folder determines scope regardless of frontmatter. Outside every registered folder, `products` must include the selected slug or `"*"`; absent, null and empty-string values mean shared. An explicit `products: []` selects no product. Broader source search may still find unselected notes; they cannot satisfy typed model evidence references.

The matcher supports directory prefixes. Artifact phase directory names therefore participate in the admission fingerprint; adding or renaming a phase directory requires refresh. The knowledge manifest records `artifactPhases`, the ready check exposes that set, and a changed set reports `ao_artifact_phases_stale` with previous/current folders and the refresh command. The AO graph's `placed()` restricts assertions/concepts to files directly under the type root or a registered product folder. Prefix matchers cannot express that depth restriction: nested assertions/concepts within those admitted roots remain a declared compatibility limit. Keep those notes at AO's documented positions. Source `_raw` exclusion continues to use the static knowledge selector.

## Generated model contract for views

- Each concrete `AOKBModel_<mangled-kind>_<origin>` implements both `AOKBModel_<mangled-kind>` and `AOKBModelRecord { aoValue: String }`, has `@node(locator: EMBEDDED, label: "<AO kind label>")`, and `@display(group: "Product model")`. Fields retain the M2 `v_<mangled-field>` spelling; underscores and punctuation are encoded by hexadecimal codepoint.
- `AOKBModelFile` selects each model document root with existing RECORD syntax. Its `synced: String` exposes the file's top-level authored timestamp. Built-in `notePath` gives the physical source path; no YAML parsing is needed in a view. These positional file observations are separate from model records and the umbrella interface.
- Each concrete model type gets a YAML under `views/knowledge/`, with `source: {kind: custom, entry: model.tsx}` and `mount: {kind: node, type: <ConcreteType>, default: true}`. Node mounts are exact-type matches. Views are published with model SDL, removed when kinds disappear, and excluded from binding fingerprints. `views/knowledge/model.tsx` is the static presentation entry and is never overwritten by refresh.
- Model fields targeting AO `kb:assertions` use `@references(to: ["Assertion"], targetKey: "aoId", mode: STRICT, values: ELEMENTS, allowWildcard: false, edge: {name: "evidenced-by", direction: OUTBOUND})`. The existing edge helper preserves reverse vocabulary direction. Other `kb:` targets fail explicitly. Markdown logical-ID fields such as `extractedAssertionIds` and `provenance.source` remain authored strings.
- Account model membership includes the selected slug or wildcard `"*"`, excludes untagged rows, and is inherited by children through their container. This differs intentionally from the knowledge missing-products rule.

## Intermediate and publication

`adapter.py --root <account> [--product <slug>]` emits deterministic JSON: `formatVersion: 1`, `compilerVersion: "1"`, `providerContract: "RECORD-1"`, `knowledgeAdapterVersion: "1"`; pinned `toolkit`; explicit `context`; complete `effectiveMetamodel`; expanded `kinds`; physical `sources`; selected `knowledgeSources`; configuration `inputs`; `referenceVocabulary`; physically attributed `records`; `diagnostics`; and AO `oracle: {rows, edges, unresolvedEdges}`. Source revisions are evidence, not binding-fingerprint inputs. Compatibility errors retain the intermediate and prevent model publication.

Fingerprints cover configuration/metamodel bytes, registry/markers, manifest pin, selection, compiler, other SDL, ownership, and the set of model source paths. `.rhizome/config.yml` is excluded: `rzm index` re-serializes it, and the check's `note includes` row separately names compiled sources that no include covers. They exclude note and row bytes, generated SDL, manifests, receipts and generated views. Ordinary authored edits flow through normal indexing. Every check still reruns AO's adapter offline: mixed-origin rows, duplicate IDs and unsupported membership remain source compatibility errors, distinct from stale configuration. The first refresh and later source changes use the same compatibility path.

`manifest.json` describes the model generation; `knowledge-manifest.json` describes admission. Each stores context, fingerprint, input evidence, SDL hash, status and generated file inventory. Model metadata additionally stores toolkit, sources, knowledgeSources, typeMap and diagnostics. `selection.json` records product and optional local toolkit path. `index-receipt.json` records the verified model fingerprint/SDL hash, knowledge admission fingerprint, and an authored-source signature. A changed phase inventory requires a new index receipt even when model SDL is unchanged. An ordinary source edit expires index readiness while bindings remain ready.

One checkout-local lock protects refresh/index. Each stage validates complete SDL through `rzm ontology query-schema` in a disposable vault containing static/other SDL plus generated admission. Publication stages files and manifest, marks pending metadata stale, then renames each live file. Recoverable failure restores the prior stage's files. Interruption never advertises a ready mismatched generation. Knowledge publishes before Python begins; the stages deliberately have independent failure states.

Generated SDL, views and manifests are compiler output outside init's managed-file inventory. Re-init preserves them; ejection keeps them and source ownership, along with installed assets. Add-on removal leaves generated/unrecorded files and ownership in place: review these explicitly if removing the projection. Deleting the ownership declaration ends native mutation protection independently of binding state. Local toolkit paths need adjustment on another machine; the default remote pin is portable.

## Compatibility and tests

AO mapping merges are product-key-first; lists concatenate account-first. RECORD `sourceOrder` supplies account-first positional numbering. Overlapping mapping rows are conservatively rejected with kind/key/both source paths; interleaved cross-file grouped positional composition is also rejected. Configuration scalar conflicts outside rows remain notes. Missing optional generated JSON stays declared at its original path. Generated `reads_fields` is omitted by name; required overlays fail until contributing-source provenance exists. Complex fields stay available as native content; AO validation declarations are retained without reimplementing AO judgments.

Synthetic fixtures, checked AO intermediates, literal SDL, Node publication/lifecycle tests, and real-provider Go parity live in `testdata/ao-kb-model`, `testdata/ao-kb`, and `testdata/ao-kb-product`. CI runs no Python. AO structural `edges()` omits claim targets, so the evidence oracle separately resolves each AO row's ordered evidence IDs against AO-selected assertions, including missing and ambiguous cases.

