---
name: ao-kb-setup
description: Install, check, or recover the Rhizome AO KB knowledge workspace in an existing AO KB repository.
---

# AO KB setup

Use the repository's Rhizome launcher for every command below. Run from the vault root. Keep AO knowledge authoring with `/ao-kb:process`, `/ao-kb:curate`, and `/ao-kb:suggest`. Never run AO ingestion, curation, generation, or publishing during setup.

1. Install with `rzm init --add-addon ao-kb`; on first init use `rzm init --addons ao-kb`. Init installs static assets and never runs a project-specific compiler. It also installs `.rhizome/source-ownership.yml`, which declares AO KB's standard paths AO-owned before anything is indexed. Run `rzm init --check` to check managed files.
2. Run `rzm module ao-kb check`. Inspect every named check: AO root `.kb/config/vault.md`, schemaVersion 8, single-product mode, optional toolkit stamp, note includes, ownership, bindings, and saved analysis. Older schemas are unsupported: upgrade AO KB first. Multi-product repositories need product selection in a later release; current knowledge views show all products. Stop before claiming readiness on prerequisite errors or unsupported root/schema/product mode.
3. Establish note coverage for `assertions/**`, `sources/**` (typed sources exclude `sources/_raw/**`), `concepts/**`, `artifacts/**`, and `.kb/curation/_conflicts.md`. Inspect include and ignore rules if coverage is uncertain. Append only necessary include rules, preserve unrelated rules, and rerun the check. Raw sources remain evidence files rather than typed Source records. Current discovery skips hidden directories: report the `.kb` registry projection gap when the check flags it; include rules alone cannot resolve it.
4. Confirm the check's source ownership is `ok`. If this KB keeps AO-authored files outside the standard layout, add those paths to `.rhizome/source-ownership.yml`; never add unrelated repository notes. Rhizome refuses native edits to owned files; AO skills and external editors can still change them. Deleting the declaration is the only way to end the protection. Static Markdown knowledge bindings are ready once installed. Product-model YAML bindings, product selection, and the binding compiler/refresh action are unavailable in this release. Do not invent a refresh command.
5. Run `rzm index`, then prove the typed surface with `rzm agent ontology-query --query '{ assertion(first: 1) { title } }'`. Inspect the result and indexing diagnostics. An empty result proves the query executes, but only proves expected content when the AO repository has no assertions; otherwise investigate missing projection.

Report three separate states with evidence: **starter installed** (managed-file check), **bindings ready** (static Markdown bindings and prerequisites), **index ready** (successful indexing and typed query). Include source ownership, unavailable model bindings, and saved analysis's recorded `meta.asOf` or unavailable state. Neither `init --check` nor the check action alone proves a usable projection.

For direct CLI instructions and lifecycle behavior, read `docs/reference/guides/ao-kb.md`.
