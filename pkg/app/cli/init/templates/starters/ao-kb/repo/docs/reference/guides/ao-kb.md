# AO KB knowledge workspace

The optional `ao-kb` starter presents an existing AO KB's Markdown knowledge in Rhizome. This release supports one product with the AO KB root at the vault root and schemaVersion 8. It installs Assertion, Source, Concept, Artifact, and ConflictRegistry/ConflictEntry types in Knowledge, a workspace with Attention, Concepts, Assertions, and Trace, assertion/source presentations, and model-to-evidence, claim-to-source, and reverse-impact recipes. Product-model YAML bindings arrive in a later release.

## Setup

Run from the vault root, using the repository's Rhizome launcher in place of `rzm`:

1. Run `rzm init --add-addon ao-kb` for an existing Rhizome repository, or `rzm init --addons ao-kb` on first init. Init installs static assets; it does not run a project-specific compiler. It installs `.rhizome/source-ownership.yml` before anything is indexed, so AO's files are protected from the start.
2. Run `rzm init --check`. This checks managed files only.
3. Run `rzm module ao-kb check`. This is the starter's deterministic, read-only prerequisite action. It requires Node, uses no network, and returns JSON with named checks, status, message, and evidence path. Status is `ok`, `warning`, `error`, `pending`, or `unsupported`; successful action execution alone does not mean setup is ready.
4. Resolve prerequisite errors before indexing. `.kb/config/vault.md` must exist at the vault root. SchemaVersion 8 is the inspected baseline; older versions are unsupported, so upgrade AO KB first. A multi-product KB needs product selection, arriving with model bindings; current knowledge views show all products. An unknown or newer schema requires a compatible adapter.
5. Inspect `notes.includes` and ignore rules. Include AO Markdown under `assertions/**`, `sources/**`, `concepts/**`, `artifacts/**`, and `.kb/curation/_conflicts.md`. Append missing paths while preserving unrelated rules. The check recognizes simple include lists and reports uncertainty for other YAML forms; it does not evaluate the full ignore engine. Raw `sources/_raw/**` files support evidence display but are excluded from typed Source bindings. Note discovery skips hidden directories unless an include names the hidden path, so add the exact include `.kb/curation/_conflicts.md`; a broad `**/*.md` does not reach it. The check warns when the registry exists without that include.
6. Run `rzm index`, then `rzm agent ontology-query --query '{ assertion(first: 1) { title } }'`. Confirm a known assertion projects when assertions exist; an empty response is expected only for an empty KB. Inspect indexing diagnostics for exclusions or malformed content.

Report **starter installed**, **bindings ready**, and **index ready** separately. Static Markdown knowledge bindings are ready after installation and prerequisite checks. Index readiness requires successful indexing and a typed query. Neither managed-file checks nor prerequisites alone certify a usable projection.

## Release boundaries

`.rhizome/source-ownership.yml` declares AO KB's standard layout AO-owned: `.kb/`, `assertions/`, `sources/`, `concepts/`, `artifacts/`, `decisions.md`, `_moc.md`, `MOCs/`, `_improvements/`, and the same paths under every `products/<slug>/` root, whether or not that product is selected. Rhizome refuses native edits to those files, including field edits, body edits, moves, link repairs, and edits staged before the declaration changed, and its editor shows the declared reason. This is an application boundary, not a file permission: AO skills and external editors still change the files, and Rhizome does not claim their edits passed AO validation. Add any other AO-authored location your KB uses; never add unrelated notes. Re-running init keeps your edits. Removing the add-on keeps the declaration; delete it explicitly to end the protection. Change knowledge through AO's `/ao-kb:process`, `/ao-kb:curate`, and `/ao-kb:suggest` skills. The setup skill never invokes AO ingestion, curation, generation, or publishing.

Product-model bindings, product selection, and the binding compiler/refresh action are unavailable in this release. Static Markdown bindings need no compilation. Model recipes cannot produce model records until those bindings arrive.

The optional toolkit stamp is `.kb/config/manifest.json`'s `toolkit` block (`version`, optional `commit`, `via`, `at`). It records the AO plugin that last scaffolded or synced the KB, not the version of every file or compatibility with the latest toolkit. Missing stamps are reported as unknown template age. The inspected AO toolkit baseline is commit `e9dce951`.

Saved AO analysis is read from `.kb/index/kb-weights.json` and displayed with `meta.asOf`. Missing or invalid analysis is unavailable; presence does not certify freshness, truth, or a clean KB. Rhizome does not recompute AO judgments.

## Re-init, removal, and ejection

Re-run `rzm init` to receive managed updates. Unedited managed files update; edited files are preserved and reported for reconciliation. Re-running unchanged init leaves managed bytes unchanged. Existing AO records and AO skills remain owned by AO.

`rzm init --remove-addon ao-kb` follows normal starter removal: unedited installed assets are removed and edited assets are preserved. `rzm init --eject ao-kb` keeps installed files and ends their management; `rzm init --restore ao-kb` resumes it. Neither operation migrates or deletes AO-authored knowledge. Re-run the checks and indexing after any binding update.
