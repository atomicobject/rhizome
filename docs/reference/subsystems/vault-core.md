---
summary: "Operational design constraints and review checklist for vault core: path normalization through pkg/paths, vault/link/tag primitives in pkg/vault/obsidian, config discovery, blessed frontmatter, and unified ignore matching."
reference-kind: guide
last-verified: 2026-10-04
code-paths:
  - pkg/fileio
  - pkg/noteformat
  - pkg/vault/notediscovery
  - pkg/vault/obsidian
  - pkg/paths
  - pkg/vault/config
  - pkg/vault/frontmatter
  - pkg/vault/ignore
tags: [subsystem/vault-core]
code-anchors:
  go:
    - label: subsystem.vault-core.guidance.0
      ref: glob:pkg/vault/obsidian/**/*.go
    - label: subsystem.vault-core.guidance.1
      ref: glob:pkg/paths/**/*.go
    - label: subsystem.vault-core.guidance.2
      ref: glob:pkg/vault/config/**/*.go
    - label: subsystem.vault-core.guidance.3
      ref: glob:pkg/vault/frontmatter/**/*.go
    - label: subsystem.vault-core.guidance.4
      ref: glob:pkg/vault/ignore/**/*.go
    - label: subsystem.vault-core.guidance.5
      ref: glob:pkg/noteformat/**/*.go
    - label: subsystem.vault-core.guidance.6
      ref: glob:pkg/vault/notediscovery/**/*.go
    - label: subsystem.vault-core.guidance.7
      ref: glob:pkg/fileio/**/*.go
---

# Vault core guidance

## Scope

The primitives every other subsystem builds on: canonical path normalization (`pkg/paths`), closed note-format descriptors (`pkg/noteformat`), configured note/code ownership (`pkg/vault/notediscovery`), vault definition + note/tag/frontmatter/link parsing and rewriting (`pkg/vault/obsidian`), vault discovery and global config paths (`pkg/vault/config`), blessed frontmatter filtering (`pkg/vault/frontmatter`), and gitignore-style exclusion (`pkg/vault/ignore`). Entry points: `paths.NewVaultPaths` + `Normalize`/`CleanNotePath`/`NormalizeCode` (`pkg/paths/normalize.go`, `vault.go`; `NormalizeNotePath` is lexical only), `noteformat.NewRegistry`, `notediscovery.Compile`/`Plan.Classify`, input resolvers `AbsFromInputWithVaultPaths`/`ResolveNotePathInputWithVaultPaths`/`ResolveCodeInputWithVaultPaths` (`pkg/paths/input.go`; `ResolveNoteInputWithVaultPaths` is Markdown compatibility), `obsidian.LoadLocalConfig`/`FindLocalConfig`/`LocalConfigToDefinition` (`pkg/vault/obsidian/local_config.go`), `ScanWikilinks`/`ScanAllLinks`/`BuildNotePathCache` (`wikilinks.go`, `mdlinks.go`), global config helpers `config.CliPath`/`config.ObsidianFile` (`pkg/vault/config`), `frontmatter.FilterBlessed`, `ignore.LoadUnifiedMatcher`.

## Design constraints

`pkg/fileio` owns cooperative OS file access and atomic namespace replacement shared by repository configuration and runtime discovery. It does not normalize vault identities or own serialization, writer coordination, and durability policy.

- **Explicit admitted file identity survives link resolution.** `NotePathCache.ResolveNoteCandidates` first recognizes an exact cached authored path, including explicit Markdown suffixes on directory-qualified paths, with its suffix and fragment intact. It must not turn an authored HTML metadata link into a Markdown alias or admit a file absent from the cache.

- **All path normalization goes through `pkg/paths`.** No ad hoc `filepath.Abs`/`filepath.Rel` in vault-touching code; OS differences (separators, Windows POSIX-path preservation) are isolated in one package (`pkg/paths/doc.go`).
- **Indexes store only vault-root-relative paths**: forward-slash, cleaned (no `./`, no `..`), authored note extensions preserved (`NotePath`), code untouched (`CodePath`). `AbsPath` exists only for filesystem I/O and symlink resolution — never persisted, never an index key.
- **Strict variants guard the storage boundary.** Anything stored or compared as an index key uses `VaultPaths.RelStrict`/`RelNotePathStrict`/`RelCodeStrict` so escaped or absolute paths never enter SQLite (`pkg/paths/vault.go`). `RelNoteStrict` is a deprecated Markdown compatibility helper.
- **User input has two strictness tiers** (`pkg/paths/input.go`): `AbsFromInputWithVaultPaths` for filesystem I/O (vault-relative first, falls back to cwd/join); `ResolveNotePathInputWithVaultPaths` / `ResolveCodeInputWithVaultPaths` return `(rel, abs)` and reject anything outside the vault (`ErrOutsideVault`) because the rel side becomes a key. `ResolveNoteInputWithVaultPaths` is the deprecated Markdown compatibility helper. `PathRef` (`Rel` + `Abs`) is the boundary type — pass it across subsystem seams instead of re-deriving.
- **Repo-local config is forward-compatible and read-only on load.** `LoadLocalConfig` reads `.rhizome/config.yml` and `LoadLocalWorkflowConfig` reads `.rhizome/workflows.yml`; both treat blank/comment-only files as empty mappings and otherwise accept exactly one YAML document, preserve unknown keys on disk, and expose path-aware `ConfigWarning` values. Syntax errors, multiple documents, and invalid known-field types remain errors. Ordinary loads never rewrite config or workflow state; typed saves merge owned fields into the source YAML and retain unknown top-level and nested fields. The only migration writer is the bounded `rzm init` path for retired v0.49 workflow keys.
- **Repository binary ownership is singular.** `LocalRhizomeConfig.UsesExternalBinaryManager` is the shared validation and ownership boundary. An omitted `rhizome.binaryManager` preserves Rhizome-managed pinning; `external` is the only supported non-empty value and is mutually exclusive with `version`, `devBinaryDir`, `binaryDir`, and `binaryPath`. The value records the ownership mode, not the external tool's identity. Full config loading, selection-only delegation loading, saving, and invocation resolution must reject unknown or mixed ownership before binary mutation.
- **Validation suite overlays are strict config.** `validation.default.add/skip` and `validation.all.add/skip` are the only configurable validation sets. Empty config preserves built-ins; `audit` is intentionally absent/fixed. Check identity and composition validation belong to `pkg/validate`, while `LocalConfig` owns YAML shape and two-space round trips.
- **Legacy path helpers are compat-only.** `obsidian.NormalizePath` and `AddMdSuffix` (`pkg/vault/obsidian/utils.go`) are deprecated; format-neutral storage and I/O boundaries use `paths.CleanNotePath` or the strict vault/input helpers. `NormalizeNotePath` is lexical normalization only, while `NormalizeNote` remains Markdown compatibility only.
- **Repo-local full-file config writes are deterministic.** `.rhizome/config.yml` and `.rhizome/workflows.yml` Go writers use `LocalConfigYAMLIndent` (industry-standard two spaces). After merging and serializing, typed saves skip byte-identical replacements so file identity and timestamps stay stable and configuration watchers receive only real writes. Workflow persistence still runs when the main config is unchanged. Text patchers such as the standalone installer preserve existing indentation when editing an existing file, recognize settings only at the immediate-child depth of their parent mapping, and default to two spaces only when creating a new file.
- **Config publication shares OS file primitives with runtime discovery.** `pkg/fileio.OpenRead`/`ReadFile` allow deletion sharing on Windows; `Replace` uses the established POSIX rename operation, with ordinary rename fallback only when unsupported. Repo config and workflow loads, source-preserving init reads, and raw MCP/code-mode snapshots use this reader. `WriteFileAtomic` writes, syncs, sets permissions, and closes its temporary sibling before one replacement, then removes its own temporary file on failure while retaining the published target. Existing cooperative readers can finish the complete old snapshot while new openers see the complete replacement. External readers without deletion sharing or unsupported filesystems may still deny publication; errors remain visible without retries or removing the destination first. This OS path-access behavior belongs to `fileio`; vault-relative path identity remains in `pkg/paths`.
- **Relative config paths resolve against the config file's directory**, not cwd (`local_config.go` migration helpers, `LocalConfigToDefinition`). Discovery walks from cwd to the nearest git root (`FindLocalConfig`, `findNearestGitRoot`).
- **Wikilink resolution is cache-mediated.** Links resolve via `NotePathCache` (title/alias/path index, case-insensitive via `paths.CaseKey`); suppressed tags are skipped during scans (`wikilinks.go`). `ScanWikilinks` marks links in fenced, indented, and inline code with `InsideCodeBlock` using the same protected spans as the structured scanner; note-link consumers skip them, and coderefs scanning of source files ignores the flag. `RewriteLinksInContent` and `RewriteLinksPreservingDisplay` protect the same spans, so rename and retarget writes touch exactly the links the scanners report; `MarkdownCodeMask` exposes the mask to other scanners.
- **Link health uses Obsidian's resolver as the fallback.** `VaultFileIndex` (`link_file_index.go`) mirrors Obsidian's `getLinkpathDest`: case-insensitive file names over every file outside dot-folders and `node_modules`, only `.md` implied, ambiguous names resolved by source folder then shortest path, and no ignore rules. Broken-link detection and link-hygiene consult it after `NotePathCache`, so attachments and notes under ignored paths resolve; broken-link detection reads such a Markdown target once to check its fragments. Graph edges still resolve through `NotePathCache` only.
- **Rename spellings belong to one move.** `RewriteLinksInContentWithOptions` accepts old-path alternatives for authored parent aliases alongside canonical paths. All alternatives match the original content in one pass, keep the same basename-uniqueness decision, and count each link once. Existing normal-note protected spans and separate sequential move semantics remain unchanged.
- **Explicit Markdown paths outrank basename ambiguity.** When an authored Markdown target exactly names a cached vault-relative note path, resolve that path before consulting the basename candidate set. Basename collisions remain ambiguous for basename-only targets; they must not invalidate a precise path.
- **Markdown URL paths decode once and return canonical identity.** Split raw `#` before decoding the path/fragment with URL path semantics (`+` is literal; malformed escapes retain authored bytes). A decoded filename hash never becomes a fragment delimiter. Return the cache's unique canonical path, including for extensionless targets; missing explicit HTML paths cannot fall back to Markdown. Literal `%20` filenames are authored as `%2520`. Link-health checks preserve the raw path at the cache boundary and decode once for literal filesystem fallback and diagnostics, including ignored or unindexed targets. Encode first-segment colons when emitting relative paths so filenames cannot become URI schemes; later-segment colons stay literal. Wikilinks do not URL-decode.
- **Markdown fragment targets have one parser.** `EnumerateMarkdownTargets` owns ATX/Setext headings, duplicate ordinals, trailing/standalone Obsidian block IDs, byte spans, and heading normalization. `EnumerateHeadings` and fragment-health extraction delegate to it so persisted targets and broken-link checks cannot drift. Heading matching is case-insensitive; block IDs retain case. Frontmatter and fenced, indented, and inline code contexts never emit targets. Legacy metadata title selection is separately centralized in `SingleMarkdownH1Title`/`FirstMarkdownH1Title` so its strict historical ATX/Setext behavior stays stable. Both return plain text through `PlainMarkdownTitle` (wikilinks become alias or target, links their label; emphasis, strikethrough, highlight, and code markers drop; code spans and escapes stay literal; intraword underscores survive). Section titles keep authored Markdown for heading identity; only derived note titles are plain. Changing this output requires a Markdown `ProjectionVersion` and `OntologyMaterializationVersion` bump.
- **Markdown projection reuses vault syntax primitives.** Provider projection composes `ExtractFrontmatter`, `ExtractHashtags`, `ExtractInlinePropertyOccurrences`, `ScanStructuredLinkSnapshot`, and `EnumerateMarkdownTargets`; it must not add a parallel parser, resolver, or filesystem read. `ExtractInlineProperties` remains the compatibility grouping API, while occurrence extraction preserves source order and intentionally has no editable spans.
- **Structured links have sealed source spans.** `ScanStructuredLinkSnapshot` performs one bounded scan, seals the source fingerprint and exact wiki/Markdown component spans, and represents empty scans without rescanning. Protected fenced (at any indentation, so fences in list items count), inline, and indented code spans are sorted and traversed by cursor/binary search; indented code follows CommonMark in that it cannot interrupt a paragraph or continue a list, so tab-indented nested list items keep their links; arbitrary URI schemes and protocol-relative targets stay external.
- **Source comments have an explicit scan boundary.** `ScanCommentLinks` reuses the structured parser for already extracted comments/docstrings, including explicit links in their code examples. It does not change normal note protection, sealed-span mutation APIs, or legacy `ResolverInput` compatibility.
- **Provider URI references are decoded once.** `noteformat.URIReferenceFact` is the closed URI input for non-Markdown providers. It carries decoded scheme, authority, path, query, and fragment components, exact authored spans, and percent-encoding provenance. `DocumentBaseFact` carries the first usable base. Shared resolution consumes these components directly and never reparses them as Markdown.
- **Reference definitions are protected destination evidence.** `ExtractMarkdownReferenceDefinitions` recognizes internal Markdown reference-definition destinations outside fenced, inline, and indented code. It does not resolve labels or paths; callers that need graph identity resolve returned destinations through `NotePathCache` using the source note path.
- **Plain tokens are review-only.** `ScanIdentifierReviewCandidates` reuses the sealed link/code masks, excludes exact handled structured-field ranges, and performs a bounded case-folded multi-pattern scan. Prose and source-code identifier tokens may produce review evidence but never raw replacement edits.
- **Governed identifier moves are planned, not performed.** `PlanGovernedIdentifierMove` rewrites only a bounded identifier token in the source basename, preserves directory spelling, and returns stable exact plus portable case-fold vacancy requirements. Callers must carry those requirements into their transactional apply preconditions; the planner's caller-supplied sibling inventory is review evidence, not final filesystem authority.
- **All file exclusion goes through `ignore.LoadUnifiedMatcher`** — go-git gitignore semantics, last rule wins, ancestor dirs checked. No parallel glob filtering.
- **Note discovery is one pruned walk.** `DiscoverFiles` walks the root once (`fs.WalkDir`), prunes hidden directories and directories both the selection and hard matchers ignore (`IsIgnoredShallow`), and admits files per path through `NotePathMatchesSelection`. Never enumerate the tree with a whole-tree glob and filter afterwards: `node_modules`, `vendor`, and build caches must not be read at all. Include patterns are validated once up front (`doublestar.ValidatePattern`, `ErrBadPattern`); an unreadable subdirectory is skipped, only an unreadable root fails discovery; symlinked files are admitted under their in-vault name only when `RelStrict` resolves the target inside the vault.
- **Code config has one reading.** `LocalCodeConfig.IndexesCode` decides whether code indexing is on (any code setting other than `disabledLanguages`), and `LanguageBlocks` lists the per-language blocks; init and `LoadCodeConfig` both use them. When code is on and no language block names `roots`, `LoadCodeConfig` sets every language root to `.` and marks the result `AutomaticScope`, so each file's language comes from its extension (`code_config.go`).
- **Format selection is closed and configured.** `noteformat.Registry` matches provider extension claims case-insensitively without changing authored path casing, and descriptors declare default versus explicit-include ownership policy. `notediscovery.Plan` remains filesystem-free: composition supplies a precomputed `IgnoreFunc`, then the plan returns exactly ignored, note, code, or unowned. A selected note wins over a caller's code candidate.
- **Explicit providers require configured authorization.** Built-in Markdown is default-owned; built-in HTML is explicit-include-owned. A collection with no includes derives deterministic `**/*<extension>` defaults from the sole default provider (currently `**/*.md`), rather than embedding a format switch in discovery. Only exact or terminal `.html`/`.htm` include patterns (including all-HTML brace alternatives) activate HTML; broad globs, suffix wildcards, character classes, and mixed-provider extension braces fail closed. Phase 2 migrates provider-aware metadata projection and preserves authored Markdown extension casing; full ownership-plan routing of discovery, cache, watch, and coderef behavior remains T009 and later.

The classic and collection Markdown discovery/reader paths compare extensions case-insensitively while preserving authored terminal spelling (`Decision.MD`) through source and persisted graph identity; they never rewrite it to `Decision.MD.md`.
- **Graph doc discovery is broader than note includes when enabled.** `IncludeDocsInGraph` discovers configured file-context doc patterns across the vault even when `notes.includes` narrows ordinary notes; unified ignore rules and explicit vault excludes still bound discovery.
- **`CONTEXT.md` is a system note, not ordinary note selection.** Exact case-sensitive basename matches are discovered anywhere inside the vault root regardless of `notes.includes` or `notes.excludes`. They still obey strict root containment, unreadable/missing handling, hidden and built-in infrastructure pruning, root/nested `.gitignore`, and `.rhizome/ignore` (legacy `.obsidianignore`). Put a sensitive path in `.rhizome/ignore`; `notes.excludes` is not a hard boundary for system context.
- **Graph doc discovery is broader than note includes when enabled.** `IncludeDocsInGraph` discovers configured file-context doc patterns across the vault even when `notes.includes` narrows ordinary notes. Ordinary graph docs retain configured excludes; exact system `CONTEXT.md` retains only the hard boundaries above.
- **Blessed frontmatter is intentionally minimal** (`pkg/vault/frontmatter/blessed.go`): lowercase keys, small allowlist. Don't widen casually — it's context-window real estate.

## Must-dos when changing this subsystem

The optional `diagnostics` block uses `pkg/diagnostics.Config`. Config loads and init preserve its authored values, while the diagnostics package owns defaults and recording validation. Offline diagnostic readers deliberately bypass repository configuration and executable delegation so malformed config does not block recovery.

- New path handling: take `VaultPaths` (or a `PathRef`) as input; use `Rel*Strict` before storing, `Abs*` before I/O. Never call `filepath.Abs`/`Rel` directly. Do not infer a Markdown suffix in format-neutral code.
- New authored note formats: register stable IDs, explicit extensions, default or explicit-include ownership policy, provider and projection versions, and granular capabilities in `pkg/noteformat`; route configured ownership through `pkg/vault/notediscovery` rather than adding extension branches to consumers. Compose filesystem-backed ignore behavior outside the pure ownership plan.
- New `LocalConfig` field: update the struct + defaults/normalizers in `local_config.go`, then `pkg/app/cli/init` detection/migration so `rzm init` writes and recognizes it; keep `.rhizome/.gitignore` template in sync if the field implies a new committed file.
- Validation config changes must preserve omitted-empty output, warn for unknown nested keys, and prove init/new-config plus patch/preservation behavior. Never silently discard an invalid known-field overlay because an explicit named check could still run.
- New repo-local full-file config writer: use `LocalConfigYAMLIndent` (industry-standard two spaces) and add a regression test that a write path does not introduce invalid YAML or indentation-only churn. New installer-style text patcher: preserve existing child indentation and all nested mapping or scalar content; match editable keys only at the parent's immediate-child depth; use two spaces for new files.
- New link syntax or resolution behavior: extend `ScanWikilinks`/`scanMdLinks` + `NotePathCache`, and cover code fences, embed syntax, aliases, heading/block fragments, and emails-vs-`@mentions` edge cases with table-driven tests.
- Behavior changes to local config, workflow state, or vault discovery flow through `obsidian.LoadLocalConfig`/`LoadLocalWorkflowConfig`/`FindLocalConfig`; decoding must keep malformed YAML and known-field type errors distinct from unknown-key warnings, reject trailing documents, and must not mutate file bytes or timestamps.
- Update the touched package's `CONTEXT.md` and this note when invariants move; tests sit beside sources (`*_test.go`), fixtures in temp dirs — never personal vault data.
- Run `go test ./pkg/paths/... ./pkg/vault/...` minimum; `go test -race -tags=integration ./...` when link parsing or config discovery changes.

## Review checklist — problems to catch

- Ad hoc `filepath.Abs`/`filepath.Rel` (or manual `strings` path surgery) outside `pkg/paths`.
- New code calling deprecated `obsidian.NormalizePath`/`AddMdSuffix` instead of `paths.*`, silently dropping unknown config keys, probing retired config filenames, or mutating config during a read.
- Absolute or vault-escaping paths reaching SQLite: non-strict `Rel`/`RelNote`/`RelCode` (or raw strings) used for stored/compared keys.
- OS-specific path handling (separator checks, drive letters, case assumptions) outside `pkg/paths`; use `paths.CaseKey`/`CaseEqual` for case-insensitive comparison.
- Config field added to `LocalConfig` without defaults/normalizer or without `pkg/app/cli/init` detection/migration — silently dropped on `rzm init` refresh.
- Relative config values resolved against cwd instead of the config file's directory.
- Link-parsing changes without tests for edge cases: code fences, embeds, aliases, fragments, markdown vs wiki links, external URLs.
- Structured-link code that rescans an empty note, linearly searches all protected spans per byte, accepts unsealed spans, or rewrites a parent-directory identifier while only the basename moved.
- File filtering bypassing `ignore.LoadUnifiedMatcher` (re-implemented glob/exclude logic).
- `AbsPath` leaking into return values destined for storage, IDs, or FQNs.
- Extension switches or broad-glob HTML enablement outside `notediscovery`, simultaneous note/code ownership, or treating an unsupported glob expression as explicit HTML authorization.

## Key files

- `pkg/fileio/read*.go`, `replace*.go` — cooperative OS reads and atomic namespace replacement shared by repo config and runtime publishers.
- `pkg/paths/doc.go` — canonical invariants; `vault.go` (`VaultPaths`, `Rel*Strict`), `input.go` (input resolvers, `PathRef` constructors), `normalize.go`, `casekey.go`, `convert.go` (`ToAbs`/`ToRel`/`ResolveSymlinks`).
- `pkg/noteformat/` — stable provider descriptors and immutable registry; `pkg/vault/notediscovery/` — configured ownership plan and explicit HTML trust grammar.
- `pkg/vault/obsidian/local_config.go` — warning-aware `.rhizome/config.yml` load/save, selection-only delegation discovery, v0.49 workflow migration reads, and `LocalConfigToDefinition`.
- `pkg/vault/obsidian/wikilinks.go` / `mdlinks.go` / `link_*.go` — resolution, sealed structured-link scanning, backlinks, and review masks; `markdown_targets.go` — canonical heading/block target parsing; `rewrite.go` — link rewriting on moves.
- `pkg/vault/obsidian/properties*.go`, `tags*.go`, `note.go` — frontmatter/tag/note primitives.
- `pkg/vault/config/` — `CliPath`, `ObsidianFile`, env `ResolveValue`.
- `pkg/vault/frontmatter/blessed.go`; `pkg/vault/ignore/load.go` + `matcher.go`.

## Related docs

- [[PathRef contract]] and [[Code Intel - IDs + path normalization]] — storage-side path identity rules.
- [[Go anchor - Paths]] — code anchors binding this note's contracts to `pkg/paths` symbols.
- [[Ignore + exclude rules]] and [[Ignore behavior]] — exclusion semantics.
- [[Graph (Hub)]] and [[List + prompt matching DSL]] — deep docs for `pkg/vault/obsidian` graph/matching layers.
