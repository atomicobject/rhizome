---
type: ReferenceDoc
summary: "Rules and mechanics for rewriting coderef links in source files when notes move or rename, including the scanner/rewriter strictness asymmetry."
reference-kind: guide
derived-from:
  - docs/reference-notes/Coderefs - rewrite on rename + move.md
last-verified: 2026-10-04
status: active
aliases:
  - Coderefs - rewrite on rename + move
---

# Coderefs - rewrite on rename + move

## Summary

When note paths change, coderef rewrites update working source links without overreaching into unrelated text. See [[coderef-contract|SPEC-0016]] for the normative contract and [[Coderefs (Hub)]] for the subsystem map. Rename/recreate edge cases interact with [[Dirty tracking + Refresh semantics]].

## Entry points

- `RewriteBatch(vaultPath, config, mappings)` — scans matching code files once and applies all `RefMapping{OldPath, NewPath}` rewrites; returns `RewriteResult{FilesUpdated, RefsUpdated}`.
- `RewriteCodeRefs(...)` — single-move convenience wrapper.
- Callers: `pkg/app/cli/rename.go` and `pkg/app/cli/move.go` (driven by `rzm note rename` / `rzm note move` when `CodeRefConfig` is set).

## Mechanics

1. **Prepare each mapping**: normalize canonical old/new paths and compile precise mention boundaries once.
2. **Discover files**: `discoverCodeFilesForRewrite` globs `config.Includes` minus `config.Excludes`. Its existing stat check rejects files larger than 2 MiB before reading their contents, avoiding an allocation proportional to an oversized file. Exactly 2 MiB remains eligible; the post-read size check also rejects files that grow after discovery.
3. **Comment-scoped rewrite**: for known languages, `rewriteRefsInPath` rewrites only inside extracted comment blocks, splicing untouched code around them. Unknown extensions fall back to whole-text rewrite for compatibility — so keep `Includes` pointed at code/comment formats, not arbitrary generated data.
4. **Skip** binary/oversized files; preserve original file permissions on write.
5. **Edit original spans**: `ScanCommentLinks` recognizes wiki and Markdown links, including extracted comment code examples. Collect link and standalone mention edits from the original block for each mapping, then apply them once from the end. Parsed links own their labels and destinations, so embedded `@` text is data. Mappings run sequentially.

## The scanner/rewriter strictness asymmetry (key invariant)

Scanning is permissive (tolerates lookup misses); **rewriting is stricter** because it mutates source:

- `MentionFull` adds a *trailing boundary* the scanner's `mentionRegex` omits, so `@MyNote` never partially replaces inside `@MyNoteExtra`.
- Mention regexes run only when their exact old spelling occurs in the current comment, including basename and alias spellings. Old names containing U+FFFD bypass this byte guard because Go regexes also match invalid UTF-8 source bytes as U+FFFD. Existing regex boundaries and structured-link exclusions remain authoritative; later mappings check the text produced by earlier mappings.
- Mention targets allow ASCII letters, digits, `_`, `/`, `.`, and `-`. Rewrites preserve that form when the destination fits; otherwise they use a full canonical wikilink when its path round-trips through the wiki parser. For example, `@Old` becomes `[[Budget$USD.md]]` for a dollar filename. A path containing wiki delimiters such as `#`, `|`, or `]]` uses a URL-encoded Markdown link instead: `@Old` becomes `[Budget#USD](Budget%23USD.md)`. This keeps rescans and later renames tied to the destination without widening mentions.
- Existing Markdown stays Markdown, retaining label, fragment bytes, title, and angle wrappers. Wiki paths that remain representable retain aliases/fragments; Markdown fallback escapes the label and fragment while preserving their meaning.
- Basename-only wikilink/mention rewrites are skipped when the match already looks pathful (contains `/`).
- Authored Markdown image destinations are excluded from rewriting. Representable wiki embeds keep `![[...]]`; an unrepresentable wiki embed becomes a normal Markdown link so subsequent renames still update it. Coderefs carry target/fragment/syntax, not separate embed identity.

Markdown path matching/resolution URL-decodes once, after separating raw `#`; emitted paths encode each folder segment and retain `/`. First-segment colons are escaped so `Budget:USD.md` stays a relative filename. `%20` means a space, and a filename containing literal `%20` uses `%2520`. `+` stays literal; malformed escapes retain their original bytes. Wikilinks never URL-decode. Resolved targets use the unique cached canonical path even when the authored destination omits `.md`.

Code indexer v1.15.0 refreshes persisted links for unchanged source files under these parsing rules. Run `rzm index --rebuild` to regenerate derived code references explicitly.

## Three target shapes per form

For each mapping, rewrites cover: full path (with `.md`), no-extension path, and basename. Wikilinks and mentions handle all three; markdown links handle full + no-ext.

## Contracts

- rewrite only recognized coderef forms; preserve surrounding source text and formatting
- preserve anchors in markdown links; preserve anchors and aliases in wikilinks
- avoid partial or ambiguous replacements that corrupt unrelated text
- image embeds and non-coderef markdown MUST NOT be rewritten as coderef moves
