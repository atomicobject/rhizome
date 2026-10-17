---
summary: "Lane report from the 2026-10-17 subsystem quality review: Foundation layers review (notemeta, stores, vault-core). Findings are a snapshot of commit 5251923f."
---

# Foundation layers review (notemeta, stores, vault-core)

Reviewer lane: `pkg/notemeta`, `pkg/sqliteutil` (+ repo-wide SQLite bypass scan), `pkg/paths`, `pkg/fileio`, `pkg/noteformat`, `pkg/vault/notediscovery`, `pkg/vault/obsidian`, `pkg/vault/config`, `pkg/vault/frontmatter`, `pkg/vault/ignore`. Worktree `t3-e36f9b1a` at `5251923f`. Read-only. Behavioral claims marked "probe" were confirmed by running a throwaway test through `go test -overlay`, so nothing was written into the repository. The probe sources are in `/tmp/rhizome-subsystem-review/probe/`.

## Summary

The SQLite layer (`pkg/sqliteutil`) is in good shape: DSN policy, typed busy/locked retry, schema-init locking and the migration runner are small, well tested and mostly honoured. The weak spots are a few bypasses and some dead mechanics. Notemeta's core contract holds up: the incremental hash and delta paths agree with a rebuild for the cases tested. But the package carries a lot of legacy and duplicate code, including 8 copies of the notes-hash aggregate, two copies of link-kind and resolution logic, and a second Markdown parse path. It also re-reads and re-projects the whole vault far more often than the contract needs: per MCP request, up to 4 times per ontology sync, and on every note add or delete.

The serious problems are in vault-core's syntax and resolution primitives. Rhizome does not have one Markdown syntax layer. It has about 12 frontmatter-boundary detectors, 6 code-region detectors and 3 or more wikilink scanners, and they disagree. Several of those disagreements are confirmed correctness bugs that persist wrong data:
- Code anchors are silently dropped when any frontmatter value contains `---`.
- Hashtags are extracted from `~~~` fences, indented code, YAML comments and `#123`, so they become persisted false tags.
- The live graph and the persisted graph disagree on links inside code.
- A repair rewrites links inside code fences.
- Wikilink resolution is case-sensitive and sensitive to Unicode NFC/NFD form, while the link-health fallback is case-insensitive. The result is "not broken" in health but no graph edge.

`pkg/vault/obsidian` (11.2k lines) is a god package with inverted layering: it imports `app/contextpack`, `anchors/sqlite`, `search/embeddings`, `search/graphalg` and `llm`. `pkg/paths` is the declared only normalizer, but its lexical `Normalize` is not a canonicalizer: it is not idempotent and it hides path escapes. There are zero Go fuzz tests in the repository, and the parsers and path code are exactly where fuzzing pays off. Test suites are fast (about 5 s for all 15 packages) and mostly behavioral. The gaps are randomized delta-versus-rebuild parity, fresh-versus-upgraded schema parity, and cross-scanner agreement. Some path and parser tests pin buggy behavior as expected output.

## Findings — production code

### FND-1 Wikilink resolution is case- and Unicode-normalization-sensitive, and the two resolvers disagree
- **Severity:** P1
- **Confidence:** High (probe)
- **Location:** `pkg/vault/obsidian/wikilinks.go:241-249` (`addPathKeys` keys `pathCandidates` by raw basename), `:420-470` (`resolveNotePathCandidates` looks up `pathCandidates[baseName]` and `Aliases[baseName]` verbatim), `pkg/paths/casekey.go:9`, `pkg/vault/obsidian/link_file_index.go:26,84-120` (always `strings.ToLower`), `health.go:649-654`.
- **Problem:** Obsidian resolves link names case-insensitively on every OS. `NotePathCache` matches basenames and aliases case-sensitively everywhere. Only explicit full paths go through `paths.CaseKey`, which is itself a platform guess: lowercase on darwin and windows, identity on Linux. Nothing applies NFC/NFD normalization. macOS filenames are frequently NFD while typed links are NFC. Link health falls back to `VaultFileIndex`, which lowercases, so the health check reports a link as fine while graph edges, backlinks and ontology relations (all resolved through `NotePathCache` only) silently lack the edge. The subsystem note claims the cache is "case-insensitive via paths.CaseKey". It is only for explicit paths.
- **Evidence (probe):** Cache over `Notes/My Note.md` and NFD `Café.md`:
  - `ResolveNote("My Note")` resolves.
  - `"my note"` does not resolve.
  - `"Notes/my note"` does not resolve.
  - `"notes/My Note.md"` resolves, on darwin only.
  - NFC `"Café"` does not resolve.
  - Alias `Alpha` resolves; `"alpha"` does not.
- **Recommendation:**
  - Add one link-name key in `pkg/paths`, for example `LinkKey(s) = strings.ToLower(norm.NFC.String(s))` (`x/text/unicode/norm` is already vendored). Use it for `pathCandidates`, `Aliases` and `explicitPathCandidates` on all platforms.
  - Keep `CaseKey` only for filesystem identity questions.
  - Collapse `VaultFileIndex` into a final resolution tier inside `NotePathCache`, so graph and health use one resolver.
  - Bump `noteMetadataIndexerVersion` and the ontology materialization version.
- **Effort:** M
- **Dependencies:** FND-11 (paths); note that ambiguity sets grow when keys fold.

### FND-2 `anchors.ParseNote` splits frontmatter on any `---` substring, silently dropping code anchors and truncating titles
- **Severity:** P1
- **Confidence:** High (probe)
- **Location:** `pkg/anchors/frontmatter.go:118-133`. Callers: `anchors/note_source.go:42` (every note-anchor ingest), `app/codeintel/ingest.go:623`, `validate/check_code_frontmatter.go:59`, `cmd/code.go:419`.
- **Problem:** The code runs `strings.TrimSpace(content)` and then `strings.SplitN(trimmed, "---", 3)`. A `---` anywhere inside a YAML value closes the frontmatter early, the remaining YAML is discarded, and invalid YAML is mapped to "no frontmatter" without a diagnostic. The validator uses the same parser, so it cannot catch this.
- **Evidence (probe):** Frontmatter `title: Build --- Release` followed by a `code-anchors` block gives `title="Build" anchors=0 err=nil`. The same note without `---` in the title gives `title="Build Release" anchors=1`.
- **Recommendation:** Replace the function with the shared frontmatter boundary (FND-4) and add the probe as a regression test. Longer term, anchors should read `code-anchors` from projected `RootMetadata` in `NoteSourceSnapshot` and stop parsing raw content.
- **Effort:** S
- **Dependencies:** FND-4 for the shared primitive; the immediate fix is independent.

### FND-3 `ExtractHashtags` disagrees with the package's own code mask, so false tags are persisted
- **Severity:** P1
- **Confidence:** High (probe)
- **Location:** `pkg/vault/obsidian/tags.go:106-160` (fence toggle only on a trimmed "```", inline code by naive backtick split, no frontmatter skip, regex `(?:^|\s)#[\p{L}\p{N}_/\-]+` accepts all-digit tags). Consumed by the canonical Markdown provider at `pkg/noteformat/markdown/provider.go:147` into `note_tags`, and by `graph.go:1322` and `HasAnyTags`.
- **Problem:** `MarkdownCodeMask`/`markdownProtectedSpans` (`link_protected.go`) correctly protect `~~~` fences, indented code and inline code. `ExtractHashtags` does not, and it scans YAML frontmatter, where `key: value #comment` is a YAML comment. `IsValidTag` (`tagutil.go`) rejects numeric and non-ASCII tags, but nothing applies it to extraction, and its letter check is ASCII-only while the regex is Unicode.
- **Evidence (probe):** Content containing `status: draft #wip` in frontmatter, `#tildefence` inside `~~~`, `    #indentedcode`, `issue #123` and `#backtick` inside "```" gives `["#wip" "#tildefence" "#indentedcode" "#123"]`. `MarkdownCodeMask` reports all three code positions as masked.
- **Recommendation:**
  - Rewrite `ExtractHashtags` over `markdownProtectedSpans` plus the frontmatter range from FND-4.
  - Require at least one non-digit, matching Obsidian.
  - Make `IsValidTag` Unicode-aware and use it in extraction.
  - Bump the Markdown `ProjectionVersion`.
  - Fuzz property: no extracted tag offset lies inside the mask.
- **Effort:** S–M
- **Dependencies:** FND-4.

### FND-4 There is no single Markdown syntax layer: about 12 frontmatter-boundary rules and 6 code-region detectors
- **Severity:** P1
- **Confidence:** High
- **Location:** Frontmatter boundary implementations:
  1. `obsidian/tags.go:22` `ExtractFrontmatter`: opens on prefix `---` at byte 0 (so `----` and `---x` count); closes on the first `"\n---"` prefix (so `\n----` and `\n---x` close it); no `...` closer.
  2. `noteformat/markdown/frontmatter_metadata.go:40`: copy of rule 1.
  3. `obsidian/markdown_title.go:84`: copy of rule 1.
  4. `obsidian/tags.go:12` `FrontmatterRegex`: allows leading whitespace and requires a newline after the closer, so a file ending in `---` does not match. Used by `ontology/projection_bindings.go:293`.
  5. `obsidian/markdown_targets.go:184`: allows an indented opener and accepts `---` or `...` as the closer.
  6. `obsidian/inline.go:45`.
  7. `ontology/markdown_links.go:89`.
  8. `anchors/frontmatter.go:118`: splits on any substring (FND-2).
  9. `app/mcp/semantic_query_preview.go:567`.
  10. `app/web/ontology_source_edit.go:103`.
  11. `app/cli/code_overview.go:677`.
  12. `validate/check_code_frontmatter.go:97`.

  Code-region detectors: `link_protected.go:16` (the correct one), `tags.go:116`, `inline.go:56`, `markdown_title.go:51`, `markdown_targets.go`, and `ontology/markdown_links.go:30-52` (untrimmed `HasPrefix(line, "```")`, so fences inside list items are missed, plus its own inline-code regex).
- **Problem:** Each subsystem decides differently whether a note has frontmatter and where code is. For example, `"\n---\ntitle: X\n---\n# Body"` has frontmatter for rules 4, 5 and 8 but not for rules 1–3. Notemeta titles (rule 1, H1 = "Body") and anchors (rule 8, title X) therefore disagree. FND-2, FND-3 and FND-5 are concrete instances.
- **Recommendation:**
  - Create one leaf package, for example `pkg/markdownsyntax` (extracted from obsidian), that exports `Frontmatter(content) (yaml string, bodyStart int, ok bool)`, `ProtectedSpans(content)`, `Hashtags`, `InlineProperties` and `Targets`.
  - Choose one boundary rule: opener exactly `---` on line 1, closer `---` or `...` on its own line, CRLF tolerant.
  - Delete the other 11 implementations and route the ontology edit-session YAML-node editor through the same boundary.
  - Add a differential fuzz test over the old functions while migrating.
- **Effort:** M
- **Dependencies:** none. This unlocks FND-2, FND-3, FND-5 and FND-7.

### FND-5 Legacy link scanners still emit links inside code: the live graph differs from the persisted graph, and a repair rewrites code
- **Severity:** P1
- **Confidence:** High (probe for the scanners; code read for the rewrite)
- **Location:**
  - `obsidian/mdlinks.go:148-170` `ScanAllLinks` maps `scanWikilinks` results and drops `InsideCodeBlock`. Markdown links are masked; wikilinks are not.
  - `obsidian/graph_snapshot.go:78` (`BuildGraphSnapshot`, the live path behind `ComputeGraphAnalysis`/`ComputeGraphStats`, used by `app/cli/graph.go:157`, `search/retrieval/graph.go:241` and `app/mcp/tool_health.go:86`).
  - `app/web/markdown_compat.go:32,68`.
  - Unguarded `ScanWikilinks` callers: `validate/check_orphan_block_ids.go:156`, `validate/identifier_block_ids.go:329`, `app/cli/note_namespace_plan.go:276`, `app/cli/heading_rename.go:173`.
  - The write path `validate/identifier_block_ids.go:364-411`, `rewriteResolvedBlockFragments`, runs `regexp` `(!)?\[\[(.+?)\]\]` and a Markdown-link regex over the full note with no mask. It is called from `validate/apply_edits.go:39` via `repair_planner.go:525`.
- **Problem:** The persisted graph (notemeta → `ScanStructuredLinkSnapshot`) excludes code. The live graph, web link resolution and several validators include it. The block-ID repair edits `[[Target#^old]]` inside a fenced code block, which contradicts the vault-core rule that rewrites touch exactly the links the scanners report. It also compiles both regexes on every call. Separately, `applyBlockFragmentRewrite` (`apply_edits.go:21-38`) rereads every note's frontmatter for every edit to rebuild aliases.
- **Evidence (probe):** For `"intro [[Live]]"`, a fenced `[[InFence]] [x](fence.md)` and an indented `[[Indented]]`, `ScanAllLinks` returns `Live, InFence, Indented` and `ScanStructuredLinkSnapshot` returns `Live`.
- **Recommendation:**
  - Make `ScanStructuredLinkSnapshot` the only scanner. Keep `ScanWikilinks` as a view over it that excludes protected spans by default, and delete `ExtractWikilinks`, `ExtractMdLinks`, `ExtractAllLinks` and `ScanAllLinks` after migrating callers.
  - Re-implement the block-fragment rewrite on `RewriteLinksPreservingDisplay`-style sealed spans and use persisted aliases (`LoadAliasMap`).
  - Delete the live `BuildGraphSnapshot` path and feed all graph analysis from `notemeta.LoadPersistedGraphSnapshot`, or from an Indexer-built snapshot when no store exists.
  - Add a fenced-code case to `TestPersistedAndFilesystemGraphSnapshotsHaveAlgorithmParity`; it would fail today.
- **Effort:** M
- **Dependencies:** FND-4.

### FND-6 Whole-vault re-reads and re-projection on hot paths
- **Severity:** P1 (performance)
- **Confidence:** High (code read)
- **Location:**
  - (a) `app/mcp/tool_helpers.go:33-43` `withProjectedNoteFacts`, called by 7 MCP handlers (`tool_query.go:38,56`, `tool_context.go:65,142`, `tool_files.go:401`, `tool_community.go:18`). It calls `LoadNoteSourceSnapshots(..., nil)`, which at `source_snapshot.go:140-197` loads every metadata row, then reads and projects every eligible note from disk, then loads all aliases, on every request.
  - (b) `ontology/sync.go:173,182,206` can call `EnsureIndexed` three times in one sync. Each call reads and hashes every note (`source_capture.go:21`). Then `ontology/index.go:1155` calls `LoadNoteSourceSnapshots(nil)`, which reads and projects every note again.
  - (c) `indexer_operations.go:147-153` with `projection_incremental.go:22-31`: any added or deleted path, or any alias change, sends `BuildPathDelta` to `buildBootstrapDelta`, a full vault read and projection. Creating one note costs O(vault).
- **Problem:** Persisted rows already hold properties, tags, aliases and resolved edges. The full rereads exist because unresolved link inputs are not persisted and because `EnsureIndexed` returns only `{NotesHash, Dirty}`, so nothing is reusable.
- **Recommendation:**
  - (1) Persist unresolved link lookup keys per source, for example a `note_link_inputs(src_path, key)` table where the key is the normalized name or alias. An add or delete then re-derives only sources whose inputs match the new or removed path's basename or aliases, plus `graph_doc_edges` sources that point at the deleted path.
  - (2) Serve MCP `NoteFacts` from `DurableNoteFactsByPaths` plus persisted edges, and drop the per-request disk projection.
  - (3) Have `EnsureIndexed` return the captured, projected entries or a generation handle, and have ontology sync call it once.
- **Effort:** L
- **Dependencies:** FND-8. Bump the indexer version when the inputs table is introduced.

### FND-7 `pkg/vault/obsidian` is a god package that inverts the layering
- **Severity:** P1 (design)
- **Confidence:** High
- **Location:**
  - `go list -f '{{.Imports}}' ./pkg/vault/obsidian` includes `pkg/app/contextpack`, `pkg/anchors`, `pkg/anchors/sqlite`, `pkg/search/embeddings`, `pkg/search/embeddings/sqlite`, `pkg/search/graphalg` and `pkg/llm`.
  - 312 production files import the package.
  - Contents span repo config schema (`local_config.go` 1370 lines, plus `code_config`, `embeddings_config`, `cli_config`, `graph_config` and preferences), graph analytics (`graph.go` 1414 lines: SCC, HITS, label propagation, recency, date parsing), link health (`health.go` 937 lines), opening the Intel store (`intel_store.go`), link parsing and resolution, tag and property editing, atomic file writes (`fsutil.go`, duplicating what `fileio` documents it owns), and dead or trivial helpers (`DeduplicateResults`, `RemoveMdSuffix`, which is case-sensitive while everything else uses `EqualFold`).
- **Problem:** The "foundation" depends on the store and search layers, so any change to Intel, search or contextpack recompiles and couples everything. Three files exceed the 500-line policy by 2–3×.
- **Recommendation:** Split the package:
  - `pkg/markdownsyntax` from FND-4 (frontmatter, mask, tags, inline, links, targets, title).
  - `pkg/vault/linkres` (`NotePathCache` plus the file-index tier from FND-1).
  - `pkg/vault/repoconfig` (local, code, embeddings, CLI and graph config, preferences, default vault name).
  - Graph algorithms move to `pkg/search/graphalg` or a new `pkg/graph`.
  - `health.go` moves to `pkg/validate`.
  - `intel_store.go` moves to `pkg/app/bootstrap`.
  - `WriteFileAtomic` moves into `pkg/fileio`.
  - `VaultDefinition` and `NoteReader` stay as the small vault core.
  - Add an import-boundary test, like `noteformat/architecture_test.go`, that forbids `pkg/vault/...` from importing `pkg/app`, `pkg/search` or `pkg/anchors`.
- **Effort:** L (mostly mechanical moves; do this after FND-4 and FND-5 to avoid moving code that is about to be deleted)
- **Dependencies:** FND-4, FND-5.

### FND-8 Notemeta has eight copies of the notes-hash and duplicated derivation code
- **Severity:** P2
- **Confidence:** High for the duplication; medium for the divergence impact
- **Location:**
  - The XOR aggregate is implemented 8 times: `index.go:325` `computeNotesHash`, `:376` `notesHashFromEntries`, `:388` `notesHashFromNoteEntries` (dead in production; golangci `unused`), `:400` `incrementalNotesHash`, `:432` `notesHashFromRows`, `indexer_operations.go:333`, `source_capture.go:71`, `source_loader.go:119`.
  - Duplicate pairs: `projection_rows.go:244` `resolutionSupported` and `source_snapshot_adapt.go:100` `linkResolutionSupported` (identical); `projection_rows.go:261` `projectionEdgeKinds` (semdb constants) and `source_snapshot_adapt.go:117` `resolvedLinkKindsFromProjection` (string literals for the same kinds); `resolveProjectionLink` and `resolveProjectedLink` (both one-line wrappers of `ResolveProjectedLink`); `buildSnapshot` and `buildDelta` (`indexer_operations.go:207-251`, identical row-emission loops).
  - `index.go:521` `titleFor` duplicates `noteformat/markdown/provider.go:90` `titleFact`. `index.go:201` `extractAliasList` duplicates `obsidian.AliasListFromFrontmatter`.
  - A second Markdown parse path: `index.go:73-181` `loadEntries` and `readNoteEntry` parse frontmatter, inline properties and tags directly (bypassing the provider) for `BuildNoteSourceFacts`.
- **Problem:** The note on this subsystem says "incremental hash must stay equal to what a full recompute would produce or freshness checks silently rot". Today the copies already differ:
  - `computeNotesHash` (used by `MetadataStateCurrent`) swallows read errors as an empty content hash (`index.go:347-349`), while `captureNoteContents` (used by `EnsureIndexed`) fails.
  - The cache branch of `computeNotesHash` hashes `TrimSpace(entry.Path)`, while `captureNoteContents` hashes `CleanNotePath(entry.Path)`. A non-canonical cache path would make `MetadataStateCurrent` permanently false while `EnsureIndexed` treats the state as current. I did not find a cache producer that emits such a path today, hence medium confidence.
- **Recommendation:**
  - Introduce one `noteSetDigest` value type with `Add/Remove(path, contentHash)` that canonicalizes internally, and delete 6 of the 8 functions.
  - Have `MetadataStateCurrent` reuse `captureNoteContents`.
  - Merge the duplicate pairs and use the semdb constants.
  - Delete `titleFor`, `extractAliasList`, `loadEntries` and `readNoteEntry`. `BuildNoteSourceFacts` can project through the provider and clear `Links`.
  - Delete `noteEntry`'s legacy fields where possible.
  - Split `index.go` (692 lines) into hashing and values files.
- **Effort:** M
- **Dependencies:** FND-T-1 should land first as the safety net.

### FND-9 The notemeta `Store` port has a single implementation and hidden capability probes
- **Severity:** P2
- **Confidence:** High
- **Location:**
  - `index.go:57-71`: the `Store` interface is built from type aliases of `semdb` types (`MetadataRow = semdb.NoteMetadataRow`, and so on), so notemeta imports `pkg/anchors/sqlite`.
  - `published_state.go:15-21`, `index.go:45-55` and `published_scoped.go:65` probe for capabilities by type assertion.
  - `indexer.go:93-100,120-123` holds "external Store" fallbacks.
  - The only implementation is `pkg/anchors/sqlite/note_metadata.go`, and tests use the real store.
- **Problem:** The port gives no decoupling, because its types come from the adapter. Optional interfaces hide required capabilities, and the fallback branches are dead in production while complicating reasoning about which gate applies. That is exactly the "inventory readiness vs requested-path availability" subtlety the subsystem note spends a paragraph explaining.
- **Recommendation:** Take `*semdb.Store` concretely, or make every probed method part of `Store`, and delete the `!ok` branches. If decoupling matters, move the DTOs into notemeta and let semdb depend on them.
- **Effort:** M
- **Dependencies:** none.

### FND-10 `notemeta.LoadAliasMap` swallows store errors, so alias-blind resolution gets published durably
- **Severity:** P2
- **Confidence:** High (code read)
- **Location:** `pkg/notemeta/index.go:189-198`. Callers: `ontology/index.go:576` (full ontology build), `ontology/sync.go:299,805`, `app/codeintel/doclinker.go:76`, `app/web/files.go:448`.
- **Problem:** A transient read error during indexing (busy, canceled context) returns `nil`, and ontology then publishes relation rows resolved without aliases, with no error or diagnostic. Those rows persist until the next full rebuild.
- **Recommendation:** Return `(map, error)` and fail or retry the indexing phase. Read-only UI callers can still degrade explicitly.
- **Effort:** S
- **Dependencies:** none.

### FND-11 `pkg/paths` lexical normalization is not canonical and hides path escapes
- **Severity:** P2
- **Confidence:** High (code read; outputs are pinned by the repo's own tests)
- **Location:** `pkg/paths/normalize.go:14-24` (`Normalize` strips exactly one leading `./` and then one `../`, with no `path.Clean`), `vault.go:64-75` (`Rel` returns `ToRel`, which is `Normalize` of `filepath.Rel`), `normalize.go:51-58` (`NormalizeNote("") == ".md"`), `docpath.go:38-52` (`NormalizeRelPathAuto`). There are 157 production calls to the lexical family and 168 to the strict family.
- **Problem:** Concrete outputs:
  - `Normalize("../a.md") = "a.md"`: an escape becomes an in-vault key.
  - `Normalize("../../a.md") = "../a.md"`, but `Normalize(Normalize("../../a.md")) = "a.md"`, so the function is not idempotent. Callers double-normalize at `app/cli/note_namespace_plan.go:269,281`, `heading_rename.go:116` and `validate/check_fragile_external.go:272` (`paths.Normalize(obsidian.NormalizeWithDefaultExt(...))`).
  - `Normalize("a//b")` and `Normalize("././a")` are not canonical.
  - `VaultPaths{root:/v}.Rel("/x/a.md") = "x/a.md"`, an alias of an in-vault key. This is reached from `search/retrieval/code_anchor_refs.go:78`.

  The deprecated Markdown helpers (`RelNote`, `RelNoteStrict`, `AbsNote`, `ResolveNote*`, `NormalizeNote`) remain alongside the extension-neutral ones, which doubles the API surface. The vault-core and notemeta notes still reference `obsidian.AddMdSuffix`, which no longer exists.
- **Recommendation:**
  - Make `Normalize` mean `CleanRelPath`: return an error on escape, and offer a separately named `SlashForDisplay` for display-only uses.
  - Delete the non-strict `Rel`, `RelNote`, `RelCode` and `RelNotePath`.
  - Delete the deprecated Markdown helpers after migrating callers.
  - Add fuzz invariants: idempotence, never absolute, never starts with `..`, and `CleanRelPath(x) == CleanRelPath(CleanRelPath(x))`.
- **Effort:** M
- **Dependencies:** FND-1 (link keys).

### FND-12 The legacy embedding-store rebuild uses connection-scoped TEMP tables across a pool, with swallowed errors and no transaction
- **Severity:** P2 (data-loss risk on derived embeddings)
- **Confidence:** Medium
- **Location:** `pkg/search/embeddings/sqlite/store.go:579-760` (`rebuildDomainPreservingEmbeddings`, triggered by `ensureSchemaWithRecovery` at `:418-450`); the same pattern in `pkg/search/embeddings/codeindex/sqlite/store.go:820-1201`.
- **Problem:** `CREATE TEMP TABLE` and the backup `INSERT ... SELECT` statements run as separate `s.db.ExecContext` calls on a pooled `*sql.DB`. TEMP tables exist only on the connection that created them. Most backup and restore statements use `_, _ =`, then `resetDomainNoLock` drops the real tables. If any statement lands on a different pooled connection (concurrent readers, idle-connection expiry), the backup is silently empty and embeddings are lost, which costs provider spend to rebuild. There is also no transaction, so a crash between reset and restore loses the data. The existing test (`store_vector_test.go:170`) passes only because sequential use reuses one idle connection.
- **Recommendation:** Delete this compatibility shim; architecture policy says "no backward-compatibility shims". `ResetDomain` plus the migration runner cover legacy schemas for a derived domain. If preservation must stay, run it inside one `sqliteutil.ExecTxWithRetry` (a single connection) and propagate every error.
- **Effort:** S
- **Dependencies:** none.

### FND-13 `QuickIntegrityCheck` scans the whole DB on every write-capable Intel open, under the cross-process schema lock, with its own retry policy
- **Severity:** P2 (performance)
- **Confidence:** High
- **Location:** `pkg/sqliteutil/corruption.go:29-75`, called at `pkg/anchors/sqlite/store.go:2469-2482` while `LockSchemaInit` is held (`:2456`).
- **Problem:** `PRAGMA quick_check(1)` reads the whole database. On the primary checkout's 1.2 GB `.rhizome/db.sqlite` it took **1.1 s** (warm cache, `sqlite3 -readonly`). Every `rzm index`, serve start and runtime open pays that cost, and other openers are serialized behind the lock. The retry loop retries *any* non-corruption error (permission, closed DB, even a `SQLITE_CORRUPT` returned by the query itself) three times. It uses context-free `time.Sleep` and a 15 s timeout per attempt, so the worst case is about 46 s with no cancellation. That contradicts the package's own `ExecTxWithRetry` rule ("only typed busy/locked retry, cancellation-aware").
- **Recommendation:** Move `quick_check` into scheduled maintenance, or run it only after an unclean-shutdown marker. Make it take `ctx`, retry only when `IsBusyOrLocked` is true, and reuse `waitForTxRetry`.
- **Effort:** S
- **Dependencies:** none.

### FND-14 The migration runner has dead mechanics and duplicate version bookkeeping
- **Severity:** P3
- **Confidence:** High
- **Location:**
  - `migration/runner.go:386-421`: `dirty=1` is written and cleared inside the same transaction, so it is never observable. Only legacy databases can have `dirty=1`, which `anchors/sqlite/store.go:2787-2818` reads.
  - `runner.go:115` `EnsureAll` and `migration/domains/intel.go:16` `IntelPlan` have no callers, including tests.
  - Intel steps also run `UPDATE schema_version` (`anchors/sqlite/store.go:3589`), a second version table next to `rzm_migration_state`.
  - `migration/domains` is three thin constructors.
- **Recommendation:** Drop the `dirty=1` write (keep the read for legacy), delete `EnsureAll` and `IntelPlan`, and retire Intel's `schema_version` table in a forward step. Consider inlining `domains` into each store.
- **Effort:** S
- **Dependencies:** none.

### FND-15 Smaller items (P3)
- **`FilterBlessed` is nondeterministic.** `pkg/vault/frontmatter/blessed.go:37-46`: with `{"Summary":"A","summary":"B"}`, the output `summary` depends on map iteration order. The package is a single function plus a mutable exported global `BlessedKeys`; fold it into the presentation layer.
- **Two tag extractors disagree on ordering.** `ontology/noderead/helpers.go:289` and `ontology/query/loaders.go:500` are identical except that one sorts its output.
- **Dead code** (golangci `unused` with `--tests=false`): `notemeta/index.go:388`, `obsidian/discovery.go:110` `noteDiscoveryPatterns`, `obsidian/health.go:369` `normalizeWikilinkFragment`.
- **Doc drift:**
  - `notemeta.md` repeats the "Entry points" bullet (lines 20–21).
  - `vault-core.md` repeats the "Graph doc discovery is broader" bullet.
  - Both reference `AddMdSuffix`, which no longer exists.
  - `vault-core.md` claims case-insensitive cache resolution (FND-1).
- **`NewContentOnlyNoteSourceSnapshot` is a legacy adapter used in production.** Calls at `app/bootstrap/live.go:1212`, `app/codeintel/ingest.go:440` and `app/cli/graph_doc.go:300` hard-code format `markdown` and derive titles via `titleFor(path, content, nil)`, which ignores frontmatter `title`. Replace them with Indexer-projected snapshots.

## Invariant violations found elsewhere in the repo

| Invariant | file:line | What it does instead |
|---|---|---|
| Open SQLite only via `sqliteutil.OpenDSN` (WAL filesystem refusal, sqlite-vec, pool, mmap) | `pkg/app/agentchat/store.go:35` | `sql.Open("sqlite3", sqliteutil.DSN(...))`: no unsafe-filesystem check, no pool limits, default driver, no `LockSchemaInit` |
| No DDL outside the migration runner | `pkg/app/agentchat/store.go:54-120` | Ad hoc `CREATE TABLE IF NOT EXISTS` plus conditional `ALTER TABLE` column patches with no version table |
| Shared retry primitive | `pkg/app/agentchat/events.go:171-174` | `isTransientSQLiteError` duplicates `sqliteutil.IsBusyOrLocked`; `store.go:221` swallows the `updated_at` update error (`_, _ =`) after a separate insert, so two writes are not in one transaction |
| One writer lock per DB file; DDL via runner; retrying transactions | `pkg/search/rerank/cache.go:28-35,100-106`, `pkg/search/embeddings/cache_provider.go:31-35,133-139` | Two independent pools and mutexes write the same `RHIZOME_EMBEDDING_CACHE` file; DDL in constructors; plain `BeginTx` without `ExecTxWithRetry` (eval-only, env-gated) |
| Writes inside one transaction with errors propagated | `pkg/search/embeddings/sqlite/store.go:607-757`, `codeindex/sqlite/store.go:820-1201` | Multi-statement DDL/DML on the pool with `_, _ =` and TEMP tables (FND-12) |
| `pkg/paths` is the only normalizer (no ad hoc `filepath.Rel`/`Abs`) | 31 production `filepath.Rel` calls outside `pkg/paths`, e.g. `obsidian/link_file_index.go:58`, `search/retrieval/explicit_seeds.go:172,183`, `search/retrieval/local_docs.go:180`, `validate/namespace_footprint.go:62,147`, `validate/check_link_hygiene.go:519`, `ontology/edit_transaction_recovery.go:160,209` | Hand-rolled relativization; `app/cli/heading_rename.go:164` uses `filepath.Join(vaultPath, source)` for note I/O |
| Normalization is canonical and single-pass | `app/cli/note_namespace_plan.go:269,281`, `app/cli/heading_rename.go:116`, `validate/check_fragile_external.go:272` | `paths.Normalize(obsidian.NormalizeWithDefaultExt(x))` double-normalizes a non-idempotent function (FND-11) |
| All exclusion through `ignore.LoadUnifiedMatcher` | `app/mcp/tool_code.go:377-400` | Walks the whole vault root with a hand-written `shouldSkipCodeContextDir` and `os.ReadFile`s every test file, including ignored ones |
| (same) | `search/retrieval/explicit_seeds.go:165`, `search/retrieval/local_docs.go:170`, `app/cli/file_context.go:1286` | Depth-limited walks with no ignore matcher |
| (documented exception) | `obsidian/link_file_index.go:41` | Walks everything except dot-folders and `node_modules`, including `vendor/` and build output, on the first unresolved link |
| One Markdown frontmatter parser | `anchors/frontmatter.go:118`, `ontology/markdown_links.go:89`, `app/mcp/semantic_query_preview.go:567`, `app/web/ontology_source_edit.go:103`, `app/cli/code_overview.go:677`, `validate/check_code_frontmatter.go:97`, `ontology/projection_bindings.go:293` (regex variant) | Independent boundary rules (FND-4) |
| One link scanner, protected spans honored | `validate/identifier_block_ids.go:369,397` (regex rewrite), `app/cli/file_context.go:911` (`!?\[\[([^\]]+)\]\]` regex), `ontology/markdown_links.go:30-53` (own fence detector plus `ScanAllLinks`), `app/web/markdown_compat.go:32,68` | Regex or line scanners that include code blocks (FND-5) |
| One "wikilink-valued property" unwrapping primitive | `notemeta/index.go:559`, `app/cli/list.go:523`, `app/views/filters.go:330`, `app/views/execute.go:610`, `app/views/source_relation_values.go:247,455`, `app/views/sort_text.go:99`, `app/presentation/ontology_context.go:180`, `ontology/index.go:1569`, `ontology/node_catalog.go:389`, `app/mcp/tool_node_link.go:241`, `validate/identifierreconcile/field_discovery_build.go:373`, `app/web/ontology_sessions.go:293` | About 13 hand-rolled `[[...]]` strip helpers with differing pipe, fragment, case and whitespace handling |
| Downstream raw-note facts via `NoteSourceSnapshot` and persisted metadata | `validate/apply_edits.go:21-38` and `validate/alias_cache.go:5` | Rereads and re-parses every note's frontmatter for each block-fragment rewrite |
| Hashtag and tag extraction belongs to the provider projection | `obsidian/graph.go:1322` (`extractTags` in the live graph), `ontology/noderead/helpers.go:289` and `ontology/query/loaders.go:500` (divergent copies) | Re-derives tags from content or frontmatter outside the projection |

## Findings — tests

Measured: one `go test -tags fts5 -count=1 -json` over all 15 packages took 5.4 s wall time, ran 1,190 tests, with 2 skips and 0 failures. The suites are cheap; the problem is what they fail to cover.

### FND-T-1 Add a randomized delta-versus-rebuild property test
- **Verdict:** add (merge existing cases into its seed corpus)
- **Tests:**
  - `pkg/notemeta/index_test.go`: `TestSyncPaths_UpdatesTouchedPathsWithoutFullRebuild`, `TestSyncPaths_NewTargetRebuildsPreviouslyUnresolvedInboundLinks`, `TestSyncPaths_RenamedTargetRebuildsUnchangedInboundLinks`, `TestSyncPaths_RemovedAliasRebuildsUnchangedInboundLinks`, `TestSyncPaths_DeletedBasenameClaimantRebuildsUnchangedInboundLinks`, `TestSyncPaths_AliasCollisionRefreshesUntouchedInboundLinks`, `TestSyncPaths_BasenameCollisionRefreshesUntouchedInboundLinks`, `TestFragmentTargets_FullAndIncrementalProjectionConverge`, `TestSyncPaths_URIExtensionlessAmbiguityMatchesFreshSnapshot`.
  - `empty_metadata_key_test.go:TestNestedEmptyMetadataKeysHaveSnapshotDeltaParity`.
  - `indexer_operations_test.go:TestBuildPathDeltaReusesAliasSnapshotWithFreshSnapshotParity`.
  - `dirty_scan_test.go:TestDiscoverDirtyPathsParityWithFullRebuild`.
  - `index_test.go:TestIncrementalNotesHash_UsesOnlyTouchedRows`, which computes its expected value through the sibling `notesHashFromRows` XOR code.
- **Why:** The central notemeta invariant ("SyncPaths must equal a fresh EnsureIndexed") is covered only by about 12 hand-picked scenarios. The bugs that rot freshness come from operation interleavings: add, then alias, then delete, then a basename collision, then a fatal projection, then a touch.
- **Replacement:** Use `testing/quick` or a seeded generator, or a `FuzzDeltaEqualsRebuild` over a byte-encoded op list. Apply N random ops (create, edit, delete, rename, alias add/remove, basename collision, invalid YAML, mtime-only touch) to an in-memory vault plus a real temp `semdb.Store`, run `SyncPaths` after each op, then dump every notemeta table and `State.RawNotesHash` and compare to a fresh store with `EnsureIndexed`. Keep the named tests only as seeds.

### FND-T-2 Add Go fuzz targets for parsers and paths (there are none in the repository)
- **Verdict:** add
- **Tests:** none exist; `grep -rn '^func Fuzz' pkg` returns nothing.
- **Replacement:**
  - `FuzzCleanRelPath`/`FuzzNormalize`: idempotence, never absolute, never `..`-prefixed, no drive letter. This catches FND-11 immediately.
  - `FuzzFrontmatterBoundary`: differential across the 12 implementations during migration, then the single implementation against yaml.v3 round-trip.
  - `FuzzScannersRespectMask`: every link from `ScanWikilinks`/`ScanAllLinks` and every tag from `ExtractHashtags` lies outside `markdownProtectedSpans`, and the link set equals `ScanStructuredLinkSnapshot`. This catches FND-3 and FND-5.
  - `FuzzResolveNoteFolding`: `ResolveNote(x) == ResolveNote(lower(NFC(x)))` (FND-1).
  - `FuzzRewriteOnlyTouchesScannedLinks`: the bytes changed by `RewriteLinksInContent` are a subset of the scanned link spans.

  These replace much of the hand-written tables in `obsidian/wikilinks_test.go` (`TestExtractWikilinks`, `TestExtractWikilinksOptions`), `obsidian/mdlinks_test.go` (`TestExtractMdLinks`, `TestScanMdLinks`, `TestExtractAllLinks`, `TestScanAllLinks`) and `obsidian/tags_test.go:TestExtractHashtags`. Keep a few named examples as seeds.

### FND-T-3 Rewrite path and parser tests that pin buggy behavior
- **Verdict:** rewrite, and delete the duplicate
- **Tests:**
  - `pkg/paths/normalize_test.go:TestNormalize` expects `"../foo/bar"→"foo/bar"`, `"././foo/bar"→"./foo/bar"`, `"C:\\Users\\test"→"C:/Users/test"` and `"/foo/bar"→"/foo/bar"`.
  - `TestNormalizeNote` expects `""→".md"`.
  - `pkg/vault/obsidian/wikilinks_test.go:TestNormalizePath` re-tests the same function through the deprecated wrapper, including `"../folder/note.md"→"folder/note.md"`.
  - `obsidian/tags_test.go:TestExtractHashtags` has no `~~~`, indented code, frontmatter-comment or numeric cases.
- **Why:** These tests would fail if the bugs were fixed. They encode accidents as contract.
- **Replacement:** After FND-11, express expected values as `CleanRelPath` semantics, including errors for escapes. Delete `TestNormalizePath` along with the wrapper. Add the FND-3 probe inputs as table rows.

### FND-T-4 Delete or merge low-signal tests
- **Verdict:** delete or merge
- **Tests:**
  - `obsidian/wikilinks_test.go:TestDeduplicateResults`: delete with the helper and use `slices.Sort` plus `slices.Compact` at the single caller `app/cli/list.go:281`.
  - `notemeta/index_test.go:TestNoteStateDigestIgnoresMtime`: pins the dead `notesHashFromNoteEntries`; the behavior is already covered by `indexer_operations_touch_test.go:TestBuildPathDeltaTouchOnlyDirtyPathsProduceSourceTouches` and `published_metadata_touch_test.go`.
  - `notemeta/index_test.go:TestTitleFor_PriorityChain`: duplicates `noteformat/markdown/provider_test.go:TestProjectTitlePrecedence` for duplicated logic; delete with `titleFor` (FND-8).
  - `sqliteutil/corruption_test.go:TestIntegrityCheckFailed_Error` (asserts string formatting) and `TestIsCorruptError_Nil`: fold into one `TestIsCorruptError` table.
  - Merge `notemeta/indexer_test.go` (`TestNewIndexerRequiresRuntime`, `TestIndexerValidateRejectsZeroValue`, `TestNewIndexerAcceptsBuiltInRuntime`) and `indexer_runtime_test.go:TestIndexerFormatRuntimeValidatesLikeIndexerOperations` into one table test. Five tests cover about six lines of validation.

### FND-T-5 Rewrite migration tests that assert unobservable state; add fresh-versus-upgrade parity
- **Verdict:** rewrite and add
- **Tests:**
  - `sqliteutil/migration/runner_test.go:TestEnsureDomain_WritesLogAndDirty` asserts `dirty == 0` after success. That is always true, because `dirty` is set and cleared in one transaction, so the test would still pass if the dirty mechanism were removed.
  - Intel upgrade coverage is a series of hand-built prior-schema tests (`anchors/sqlite/store_test.go:TestOpen_UpgradesV56WithMarkdownTargetSchema`, `TestOpen_UpgradesV60...`, `TestOpen_UpgradesV64...`, `TestOpen_UpgradesV57...`, `graph_revision_test.go:TestGraphWebFingerprintUpgradeV61RetainsRows`, `...V62...`, `ontology_flags_test.go`, `go_relationship_store_test.go`, `external_targets_store_test.go`, `derived_work_test.go`).
- **Why:** No test proves that baseline v56 plus every forward step produces the same schema as a fresh open. "Keep the consolidated fresh-schema baseline synchronized" depends on reviewer discipline.
- **Replacement:** One `TestIntelUpgradeFromBaselineMatchesFreshSchema`:
  - Create the v56 baseline and run `EnsureDomain` to the current version.
  - Dump normalized `sqlite_master` (tables, indexes, triggers, view SQL) plus `PRAGMA table_info` and `index_list` for every table.
  - Compare against a fresh `Open`.
  - Assert that a second `EnsureDomain` adds no `rzm_migration_log` rows (idempotence).

  Keep the per-version tests only where they check data preservation, not schema shape. Rewrite `TestEnsureDomain_WritesLogAndDirty` to assert log status only, or delete it, since `TestEnsureDomain_ResumesAfterFailedStep` covers failure.

### FND-T-6 Add regression tests for the confirmed bugs
- **Verdict:** add
- **Tests:** none today.
- **Replacement:** Use the exact probe inputs from `/tmp/rhizome-subsystem-review/probe/`:
  - `anchors/frontmatter_test.go`: a `---` inside a title keeps its anchors (FND-2).
  - `noteformat/markdown/provider_test.go`: no tags from `~~~`, indented code, YAML comments or `#123` (FND-3).
  - `obsidian/wikilinks_test.go`: case-folded and NFC links resolve; aliases fold (FND-1).
  - `notemeta/graph_snapshot_test.go:TestPersistedAndFilesystemGraphSnapshotsHaveAlgorithmParity`: add a fenced-code link (FND-5).
  - `validate`: a block-fragment rewrite leaves fenced code untouched (FND-5).
  - `notemeta`: `LoadAliasMap` error propagation (FND-10).

### FND-T-7 Brittle and slow timing-based sqliteutil tests
- **Verdict:** rewrite
- **Tests:**
  - `sqliteutil/corruption_test.go:TestQuickIntegrityCheck_ReturnsCorruptionForCorruptDB` (1.51 s) and `TestQuickIntegrityCheck_ExclusiveLockReturnsBusy` (1.51 s) pay production `time.Sleep` backoff.
  - `sqliteutil/schema_lock_test.go:TestLockSchemaInitContendsAcrossDatabaseAliases` (1.21 s, 6 subtests at 0.20 s each), `TestLockSchemaInitForDBContendsWithAliasPath` (0.40 s), `TestLockSchemaInitIsExclusiveUntilReleased` (0.20 s) and `TestLockSchemaInitForDBContendsWithThePathLock` (0.20 s) prove "waits" by letting a 200 ms context deadline expire.
- **Why:** The time is wasted, and the deadline-based assertions can flake on a loaded CI machine. The package's own `AGENTS.md` asks that lock-release tests "assert the waiter's acquisition result".
- **Replacement:**
  - Inject the wait function into `QuickIntegrityCheck`, as `execTxWithRetry` already does (FND-13).
  - For lock tests, start the waiter in a goroutine, assert it has not acquired via a channel, release, then assert acquisition. Use `context.WithCancel` rather than a deadline for the "still blocked" check.

### FND-T-8 Use differential tests for the ignore matcher
- **Verdict:** add (replaces a subset of hand-written cases)
- **Tests:** `pkg/vault/ignore/matcher_test.go`: `TestMatcher_DirNegationUnignoresContents`, `TestMatcher_AnchoredPatternsOnlyMatchRoot`, `TestMatcher_PatternWithoutSlashMatchesRootAndNested`, `TestMatcher_DirPatternWithoutSlashMatchesRootAndNested`, `TestMatcher_DirNegationDoesNotOverrideFilePattern`, `TestMatcher_DirNegationSuppressesRecursivePattern`, `TestMatcher_FileNegationRequiresParentUnignored`, `TestMatcher_CommentsAndEscapes`, `TestMatcher_TrailingSpaces`, `TestMatcher_EscapedWildcardLiteral`.
- **Why:** The invariant is "go-git gitignore semantics", and `git` is the oracle.
- **Replacement:** A table- or generator-driven test that writes `.gitignore` plus paths into a temp repo and compares `Matcher.IsIgnored` with `git check-ignore --no-index -v`. Skip it when `git` is absent. Keep the named tests as seeds, but derive their expected values from git.

## Suggested refactor sequence

1. **Safety nets first:** FND-T-1 (delta-versus-rebuild property test), FND-T-5 (schema parity) and FND-T-2 path and scanner fuzz targets in "report-only" form, so each later step is measured against them.
2. **Correctness fixes with regression tests** (small, independent):
   - FND-2 (anchors frontmatter split).
   - FND-10 (`LoadAliasMap` errors).
   - FND-12 (delete the embedding TEMP-table shim).
   - FND-5 rewrite masking in `identifier_block_ids.go`.
   - FND-3 (hashtags over the protected spans), with a Markdown `ProjectionVersion` bump.
3. **Single Markdown syntax layer (FND-4):** extract `pkg/markdownsyntax` from obsidian and migrate the 12 frontmatter, 6 code-region and 3+ link-scanner duplicates. Delete `ScanAllLinks`/`Extract*Links` and the live `BuildGraphSnapshot` path (FND-5).
4. **Resolution and paths:** FND-1 (folded `LinkKey`, unify with `VaultFileIndex`, indexer version bump), then FND-11 (canonical `Normalize`, delete the non-strict and deprecated helpers). These share key semantics, so do them back to back.
5. **Notemeta consolidation:** FND-8 (one digest type, delete legacy parse paths and duplicate pairs) and FND-9 (concrete store, no capability probes), guarded by FND-T-1.
6. **Performance (FND-6):** persist unresolved link inputs so add and delete become O(affected); move MCP facts onto persisted rows; have `EnsureIndexed` return reusable captures so ontology calls it once.
7. **Package split (FND-7):** move config, graph analytics, health and the Intel-store opener out of `obsidian`, and add an import-boundary test. This is mostly mechanical once steps 3–5 have deleted the duplicates.
8. **Stores cleanup:** FND-13 (integrity check off the open path, ctx-aware), FND-14 (dead runner mechanics), and moving agentchat onto `OpenDSN` plus a migration domain (invariant table).

## Slow-test table (measured)

One run: `go test -tags fts5 -count=1 -json` over the 15 packages, 5.4 s wall time, 1,190 tests.

| Package | Elapsed (s) | Slowest tests |
|---|---|---|
| `pkg/sqliteutil` | 3.75 | `TestQuickIntegrityCheck_ReturnsCorruptionForCorruptDB` 1.51, `TestQuickIntegrityCheck_ExclusiveLockReturnsBusy` 1.51, `TestLockSchemaInitContendsAcrossDatabaseAliases` 1.21, `TestLockSchemaInitForDBContendsWithAliasPath` 0.40, `TestLockSchemaInitIsExclusiveUntilReleased` 0.20, `TestLockSchemaInitForDBContendsWithThePathLock` 0.20 |
| `pkg/notemeta` | 0.98 | `TestLoadProjectedEntriesForPathsUsesBoundedParallelismAndSortedOutput` 0.10, `TestDiscoverDirtyPathsParityWithFullRebuild` 0.10 |
| `pkg/sqliteutil/migration` | 0.80 | `TestEnsureDomain_ValidationErrorsPreserveClassification` 0.52 (`busy_exhausted` subtest 0.51) |
| `pkg/vault/obsidian` | 0.70 | `TestScanStructuredLinksHandlesManyProtectedAndLiveSpans` 0.10 |
| `pkg/noteformat` | 0.51 | `TestSyntaxNeutralConsumersDoNotSelectConcreteProviders` 0.30 (AST scan of the repo) |
| other 10 packages | 0.11–0.24 each | none over 0.05 |

About 4.2 s of the 5.4 s is sleeps and deadlines in sqliteutil (FND-T-7). Everything else is effectively free.
