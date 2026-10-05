---
type: EffortNote
id: EFF-2026-09-23-15-33
aliases:
  - EFF-2026-09-23-15-33
name: Trustworthy Validation On Obsidian Vaults
created-at: 2026-09-23T19:33:00Z
status: active
summary: "Make rzm validate trustworthy on a long-lived Obsidian vault: resolve links the way Obsidian does, stop link-hygiene from corrupting valid links, report each broken link once, tighten retarget suggestions, allow applying a reviewed subset of fixes, and separate links that broke from deliberate placeholders."
---

# Trustworthy Validation On Obsidian Vaults

## Scope

Drew asked on 2026-09-23 for `rzm validate` to be trustworthy on a real personal Obsidian vault. The evidence came from `rzm agent validate all --max-issues 5000` against a long-lived vault (v0.50.5): 2,631 issues, most of them false. The vault is read-only reproduction data. Every problem gets a minimal fixture in the Rhizome test suites.

In scope, in priority order:

1. link-hygiene `safe` fixes that rewrite valid links (`[[fetch.ai call ...]]`, `[[Public Ladders.xlsx]]`).
2. Wikilinks inside fenced and inline code parsed as links.
3. Links to files under ignored paths (`.rhizome/ignore`) reported as broken.
4. `ontology` and `broken-links` both reporting the same broken link.
5. Case-sensitive link resolution.
6. Low-quality `needs_confirmation` retarget suggestions.
7. No way to apply a reviewed subset of fixes.
8. `code-anchors` blocked with a remediation that cannot help; `code-frontmatter` flagging template files.
9. Placeholder-link policy (this note records the design before implementation).
10. Heading-match findings (findings only).

Excluded: changing how the ontology graph resolves edges (case-insensitive and attachment edges), alias resolution semantics, and the heading normalizer shared with persisted Markdown targets.

## Spec Set (Frozen)

- No governing spec. The contract is the request above plus the validation subsystem note [[validate]].

## Stories In Scope (Frozen)

- None. Items 1 to 10 above are the frozen scope.

## Spec Coverage Checklist

- [x] Items 1 to 8 fixed with regression tests.
- [x] Item 9 implemented per the placeholder design below.
- [x] Item 10 findings recorded.

## Decisions and boundaries

### Obsidian is the reference resolver

Obsidian 1.14.2's `MetadataCache.getLinkpathDest` (read from the installed app bundle) defines what "resolves" means:

- Lookup is by lowercased file name over every file in the vault except dot-folders. Ignore rules, `.gitignore`, and Obsidian's "excluded files" setting do not affect resolution.
- A link whose last segment contains a dot is first tried as a full file name (`Public Ladders.xlsx`, `fetch.ai call ...`), then with `.md` appended. No other extension is ever stripped.
- A link without a path resolves when one file has that name. With several candidates, Obsidian prefers files under the source note's folder, then the shortest path. Ambiguity never makes a link unresolved.
- Heading fragments compare `stripHeading(text).toLowerCase()`, where `stripHeading` replaces ``!"#$%&()*+,.:;<=>?@^`{|}~/[]\`` and line breaks with spaces, collapses whitespace, and trims. Obsidian compares block IDs case-insensitively; Rhizome keeps them case-sensitive (see Deviations).

Validation adopts these rules as a fallback after Rhizome's note cache (which keeps its alias tier and its unique-basename discipline for graph edges). A link that fails the note cache but resolves under the Obsidian rules is not broken. Files under ignored paths therefore resolve without a separate `target_ignored` code, and fragments on files that are not indexed notes are not checked.

### Code is not prose

Obsidian does not create links inside fenced code blocks or inline code. The legacy `ScanWikilinks` marked only fenced blocks, with a quadratic scan. It now marks fenced, inline, and indented code using the same protected spans as the structured scanner, and every link-extracting check skips them. The structured scanner treated every line indented four spaces or one tab as code, which hid links in nested list items (about 600 lines in the reproduction vault). Indented code now follows CommonMark: it cannot interrupt a paragraph or continue a list.

### One owner for broken links

`broken-links` owns unresolved-link reporting for wikilinks and Markdown links. The ontology projection stops emitting `broken_note_link`; the `ontology` check reports schema problems only. Markdown-link coverage moves into `broken-links` so repositories using Markdown links keep it.

### Placeholder links (item 9)

After the resolver fixes, most remaining unresolved links on a personal vault are deliberate Obsidian placeholders: people, companies, tools, and periodic notes that were never created. They make `broken-links` useless as a gate. The check must separate links that broke (the target existed, or a move or rename orphaned the link) from links that never had a target.

Options considered:

| Option | Distinguishes broke from never-existed | Cost | Failure mode |
| --- | --- | --- | --- |
| Git history of deleted and renamed paths | Yes, directly | One `git log -M --diff-filter=DR --name-status` (0.3 s on the reproduction vault, 1.6 s on this repository), only when some link is unresolved | No history (not a repository, shallow clone): cannot classify |
| Rhizome index history (remember every note name ever indexed) | Yes, from install onward | Store migration and write path | Blind to history before install and in scratch CI |
| Committed baseline file | No: freezes today's set, so a new placeholder fails the gate | User maintenance and churn | Stale baselines hide regressions |
| Severity split by heuristics (title looks like a person) | No | Low | Unreliable |

Decision: classify with git history.

- An unresolved note target whose file name (case-insensitive, `.md` implied when no extension) appears among paths deleted in the vault's history is a link that broke. It stays `broken_note_link` in `broken-links` and fails the default suite.
- Any other unresolved target is a placeholder. It is reported as `placeholder_note_link` by a new maintenance check, `placeholder-links`, which sits in the fixed `audit` suite like `orphan-block-ids` and never enters `default` or built-in `all`. `broken-links` adds a note with the placeholder count and the command that lists them.
- When history is unavailable (not a git repository, git missing, or a shallow clone), nothing is classified as a placeholder: every unresolved link stays `broken_note_link` and `broken-links` adds a note explaining why. A shallow CI checkout therefore keeps today's strict behavior instead of passing links a pull request just broke.
- Heading and block fragment failures are always broken: the target exists.
- `validation.brokenLinks.placeholders: strict` in `.rhizome/config.yml` restores the old gate (every unresolved link is broken). This repository sets it, because its documentation links are meant to resolve and a new typo would otherwise pass as a placeholder.

Known limits: a placeholder whose name matches a file deleted long ago counts as broke; this is the conservative direction. A link introduced with a typo in a vault on the default policy passes the default suite and shows up in `placeholder-links`.

### Retarget suggestions

A candidate is only offered as `needs_confirmation` when evidence is strong: a normalized title match (case, punctuation, whitespace), or git history showing the target file was renamed to the candidate. Title containment or token overlap stays as `agent_required` evidence. Candidates never include the source note, never cross the attachment and note boundary, never map a bare periodic date (`2025-01-08`, `2025-W47`, `2025-Q3`) to a non-periodic note or the reverse, and must contain the link's heading or block fragment. Retargets keep the authored text as the display alias.

### Heading findings (item 10)

Verified against Obsidian's `stripHeading`: emoji, the em dash, and the middle dot are not stripped, so a link that omits those characters does not resolve to a heading containing them. Rhizome reporting those differences is correct. Validation now uses Obsidian's punctuation stripping for heading comparison, which fixes false reports such as a link omitting a colon that the heading contains. A fuzzy heading candidate is left as a follow-up.

## Plan

1. Resolver and parser: Obsidian file-index fallback, code-aware `ScanWikilinks`, CommonMark indented code, Obsidian heading and block comparison. Exit: `go test ./pkg/vault/obsidian/...`.
2. link-hygiene uses the shared resolver; safe only when the link resolves to the same file before and after. Exit: regression fixtures for `fetch.ai` and `.xlsx`.
3. broken-links owns Markdown links; ontology stops emitting `broken_note_link`. Exit: `go test ./pkg/ontology/... ./pkg/validate/...`.
4. Retarget candidate tightening with display preservation. Exit: regression fixtures for each observed bad pattern.
5. Placeholder classification, `placeholder-links` check, config switch. Exit: fixtures with a git repository.
6. Fix selection by action ID or issue key; code-anchors applicability; template frontmatter (delegated, integrated here).
7. Documentation, `make check`, and before-and-after counts on the reproduction vault.

## Plan Approval

Scope and deliverables were set by Drew's request of 2026-09-23. The item-9 design above is recorded for review; `plan-approved-by` stays unset until Drew approves it.

## Original Intended Delivery

Items 1 to 9 fixed with regression tests, item 10 findings, updated docs, and a before-and-after table of validation counts on the reproduction vault by check and issue code.

## Actual Delivered

| Item | Delivered | Regression coverage |
| --- | --- | --- |
| 1 | link-hygiene resolves through `VaultFileIndex`; only `.md` is stripped; a rewrite is `safe` only when the link opens the same unambiguous file before and after | `TestRunLinkHygieneNeverRetargetsLinksThatResolveToRealFiles` |
| 2 | `ScanWikilinks` marks fenced, indented, and inline code; broken-links, link-hygiene, and fix planning skip it; indented code follows CommonMark | `TestFindBrokenLinksUsesObsidianResolution`, `TestIndentedCodeKeepsNestedListLinks` |
| 3, 5 | Obsidian resolver fallback: case-insensitive, all files outside dot-folders, ignore rules not applied, ambiguous names resolve | `TestVaultFileIndexResolveMatchesObsidian`, `TestFindBrokenLinksUsesObsidianResolution` |
| 4 | ontology no longer emits `broken_note_link`; broken-links owns wikilinks and Markdown links; materialization v9 and Markdown projection v2 invalidate persisted state | `TestBuildIndex_LeavesBrokenLinksToBrokenLinksCheck`, `TestFindBrokenLinksAcceptsMarkdownAnchorStyles` |
| 6 | strong and weak candidate tiers, `confidence` on actions and issue data, source/periodic/attachment/fragment filters, display-preserving retargets that rewrite only the reviewed fragment | `TestBrokenLinkRetargetsRequireStrongEvidence`, `TestBrokenLinksSeparatesPlaceholdersUsingGitHistory` |
| 7 | `--action` and `--from-plan` on `validate fix` and `agent validate fix` (delegated) | `apply_selection_test.go`, cmd flag tests |
| 8 | code-anchors `not_applicable` naming `code.<language>.roots`; code-frontmatter skips template frontmatter with a note (delegated) | `applicability_test.go`, `check_code_frontmatter_test.go` |
| 9 | git-history classification, audit check `placeholder-links`, `validation.brokenLinks.placeholders: strict` (set in this repository) | `TestBrokenLinksSeparatesPlaceholdersUsingGitHistory`, `TestBrokenLinksWithoutGitHistoryCountEveryUnresolvedLink` |

Before and after on a snapshot of the reproduction vault (`rzm agent validate all --max-issues 5000`; before is this repository's pre-change HEAD, which matched the reported v0.50.5 baseline within two issues of vault drift):

| Check | Before | After |
| --- | --- | --- |
| ontology | 1,076 `broken_note_link` | 0 |
| broken-links | 1,547 `broken_note_link`, 4 `broken_heading_link` | 16 `broken_note_link`, 4 `broken_heading_link` |
| link-hygiene | 4 `wikilink_target_is_alias` (all wrong) | 0 |
| code-frontmatter | 2 `code_frontmatter_yaml_error` | 0 (2 templates noted) |
| code-anchors | blocked, "Prepare: rzm index" | not_applicable, names `code.<language>.roots` |
| Total | 2,633 | 20 |
| Fix plan (safe / confirm / agent) | 2 / 102 / 1,696 | 0 / 1 / 15 |

The audit suite now reports 924 `placeholder_note_link`. All 16 remaining `broken_note_link` targets were deleted or renamed in the vault's history; one of them (`Welcome Back Briefing – 2025-08-11`, with an en dash) was misread as a placeholder until the quotepath fix.

## Deviations

- Block IDs stay case-sensitive, unlike Obsidian: they back Rhizome node identifiers and an existing test encodes that choice (#93).
- Markdown-link fragments also accept GitHub heading slugs and HTML anchors. Wikilinks keep Obsidian's heading rules. Without this, moving Markdown links into broken-links surfaced 61 false heading findings on imported documents.
- broken-links skips `/managed-docs/` templates, carrying over the ontology path's exclusion, because their links target the receiving repository.
- Rename evidence counts as strong only when the linking note already contained the link just before the rename commit. A link written after a rename (for example `[[agentic engineering]]` three days after a clipping named `Agentic Engineering` was renamed) is weak evidence.
- The final review found three gaps, all fixed with regression tests: git history escaped non-ASCII paths (a deleted `Café — ✅.md` read as a placeholder), link-hygiene's Markdown and pseudo-link scans still masked only fenced code, and the rewrite writer protected neither indented code nor unbalanced fences. The writer now uses the scanners' protected spans, which also applies to note renames.
- Follow-up hardening of the git evidence (requested after delivery): one shared scan per suite run, partial-clone detection with `GIT_NO_LAZY_FETCH`, timeouts with specific unavailability notes, and `-z` parsing so names with quotes, tabs, or newlines are read exactly.
- Fixed a pre-existing bug found by the new tests: a retarget of `[[Target]]` also rewrote `[[Target#fragment]]` links that belonged to a different issue group.

## Compounding Follow-ups

- Ontology refresh reruns a full rebuild on every call when a note carries `missing_required_field`: `ontologyStateMaterialized` never matches. Reproduced on the pre-change commit; out of scope here.
- Graph edges still resolve through `NotePathCache` only, so case-only and attachment links create no ontology edges.
- Next actions do not yet suggest `--action` commands for reviewed confirmation actions.
- A fuzzy heading candidate (item 10) remains open.

## Closure Checklist

- [x] Regression tests for items 1 to 8.
- [x] Item 9 implemented per this design.
- [x] Docs updated for changed checks and config.
- [x] Before-and-after counts recorded.
- [ ] `make check` passes: Go gates pass (`make lint vet greptile-check test-fast integration`); web gates not run because this worktree has no `web/node_modules`, and no web code changed.
- [x] Independent final review (Fable advisor): verdict "fix first"; all three findings fixed.

## Status

Active.

## Execution Notes

- 2026-09-23: Obsidian 1.14.2 behavior was read from the installed app bundle (`getLinkpathDest`, `resolveSubpath`, `stripHeading`) rather than inferred.
- 2026-09-23: Items 7 and 8 were delegated to a subagent in an isolated worktree and cherry-picked after review.
- 2026-09-23: Baseline on a snapshot of the reproduction vault with the pre-change build: 2,633 issues (ontology 1,076 `broken_note_link`; broken-links 1,547 `broken_note_link` and 4 `broken_heading_link`; link-hygiene 4 `wikilink_target_is_alias`; code-frontmatter 2 `code_frontmatter_yaml_error`; code-anchors blocked).
