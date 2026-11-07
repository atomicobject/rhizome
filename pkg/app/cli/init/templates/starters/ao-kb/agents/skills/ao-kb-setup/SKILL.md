---
name: ao-kb-setup
description: Install, check, refresh, or recover the Rhizome AO KB knowledge workspace and model bindings in an existing AO KB repository.
---

# AO KB setup

Use the repository's Rhizome launcher for every command below. Run from the vault root, which must be the AO KB root (the account root for a multi-product KB). Keep AO knowledge and model authoring with `/ao-kb:process`, `/ao-kb:curate`, and `/ao-kb:suggest`. Never run AO ingestion, curation, generation, or publishing during setup.

1. **Install.** `rzm init --add-addon ao-kb`, or `rzm init --addons ao-kb` on first init. Init installs static assets, the binding compiler, and `.rhizome/source-ownership.yml`, which declares AO KB's standard layout AO-owned (the root and every `products/<slug>/` root) before anything is compiled or indexed. It runs no project-specific compiler. `rzm init --check` checks managed files only.
2. **Trust.** Module actions run only in a trusted repository. If `rzm ao-kb check` is refused as untrusted, ask the user to run `rzm trust` in the repository; it is interactive, so never run it for them. Until then, `node .rhizome/modules/ao-kb/scripts/check.mjs` prints the same check.
3. **Check.** `rzm ao-kb check` reports named rows. Knowledge: AO root `.kb/config/vault.md`, schemaVersion 8 (older is unsupported: upgrade AO KB first), product mode, toolkit stamp, note includes, source ownership, knowledge bindings, sequence-shaped frontmatter connections, and saved analysis. Model: compiler dependencies (`uv` on PATH supplies Python and AO's pinned kb-site package), model bindings, model index, and model source ownership. Stop on errors.
4. **Includes.** Note includes must cover `assertions/**`, `sources/**`, `concepts/**`, and `artifacts/**`, plus the exact `.kb/curation/_conflicts.md`: discovery skips hidden directories unless an include names the path. Append only what is missing and keep unrelated rules.
5. **Ownership before refresh.** Source ownership must be `ok`. If the KB keeps AO-authored files outside the standard layout, add those paths to `.rhizome/source-ownership.yml` now; never add unrelated notes. Rhizome refuses native edits to owned files; AO skills and external editors still change them. Deleting the declaration is the only way to end the protection.
6. **Refresh bindings.** `rzm ao-kb refresh-bindings --product <slug>` for a multi-product KB (the product is required and persisted), or `rzm ao-kb refresh-bindings --single true` for a single-product KB. Later refreshes reuse the selection. `--toolkit <absolute path to packages/kb-site>` selects a local AO checkout at the pinned revision. A failed refresh keeps the last bindings and reports them stale. Then rerun the check: model bindings must be `ok` and model source ownership must be `ok` before indexing. If ownership lists unowned compiled sources, add them to the declaration first.
7. **Index.** `rzm index`, then prove the knowledge projection with `rzm agent ontology-query --query '{ assertion(first: 1) { title } }'`. Model records are not indexed or shown in this release, so `node .rhizome/modules/ao-kb/bindings/cli.mjs index` cannot certify them yet; report the model index as not verified.

Report three states separately, with evidence: **starter installed** (`init --check` and the check's `starterInstalled`), **bindings ready** (`bindingsReady` from the check, never the saved manifest alone), and **index ready** (successful indexing and the typed knowledge query; model index unverified). Include source ownership, the selected product, and saved analysis's `meta.asOf` or unavailable state. Neither `init --check` nor the check action alone proves a usable projection.

For direct CLI instructions and lifecycle behavior, read `docs/reference/guides/ao-kb.md`.
