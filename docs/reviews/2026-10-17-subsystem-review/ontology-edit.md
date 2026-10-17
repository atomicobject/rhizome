---
summary: "Lane report from the 2026-10-17 subsystem quality review: Ontology edit/mutation review. Findings are a snapshot of commit 5251923f."
---

# Ontology edit/mutation review

Scope: `pkg/ontology/edit_*.go`, `node_link.go` (apply path), `node_workspace.go` (read-only here), and the consumers that drive edits: `pkg/app/web/ontology.go` + `ontology_sessions.go` + `edit_session_overlay.go`, `pkg/validate/ontology_operation_adapter.go` + `apply_edits.go`, `pkg/app/cli/heading_rename.go`, `pkg/app/indexing/node_link_apply.go`, `pkg/app/cli/serve/startup.go`, `pkg/app/web/server.go`.

Method: read code and the design notes (`docs/reference/subsystems/ontology.md`, `noderead.md`, `overview.md`, `pkg/ontology/AGENTS.md`, `CONTEXT.md`, `docs/engineering/testing-policy.md`, `architecture.md`), grepped every caller, and confirmed the correctness claims below with throwaway probe tests. The probes were injected with `go test -overlay` from `/tmp/ontology-edit-scratch/`, and the repository was not modified. The probe output is quoted where it matters.

## Summary

The journal and recovery code is careful. Preflight-before-mutate, witness fingerprints, symlink and hard-link refusal, and idempotent cleanup are all present and well tested against tampering. The highest risk is the span editors that produce the bytes, not crash consistency. I confirmed end to end that two ordinary edits corrupt notes. First, a narrative edit after an inline field glues the new text onto the field line, so `status:: TODO` becomes `status:: TODOEDITED SECOND`. Second, any frontmatter edit on a UTF-8 BOM note prepends a second frontmatter block. A third path, the web `setFrontmatter`/`appendAlias` ops used by the validation `TypeChoice` fix, rewrites the entire frontmatter through a map round-trip. That drops comments, alphabetizes keys, and turns `2024-01-01` into `2024-01-01T00:00:00Z`, which contradicts the "patch targeted spans" invariant. Narrative replay also guesses between identical paragraphs by byte distance, which the subsystem note forbids.

The structural problem is that Rhizome has two journaled multi-file transaction engines. The browser commit path uses validate's repair journal. The ontology `edit-journal` engine serves only CLI heading rename and node-link apply, and its receipt/completion plumbing has no production caller. The subsystem note still describes the browser path as using the ontology journal. A single unrecoverable ontology journal aborts `rzm serve` startup and blocks every later commit, with no escape hatch, while validate's journals degrade with a warning.

The tests are fast (83 tests in 3.2 s). Recovery tests build journal states by hand, and some of those states cannot occur in a real run. No test crashes the real writer at each step, no test commits concurrently, and no test covers BOM, leading-blank, or narrative-boundary input.

## Findings — production code

### EDIT-1 Narrative replacement swallows the preceding line break and merges text into an inline field

- **Severity:** P0 (silent corruption of a typed field value)
- **Confidence:** High for server behavior, which I reproduced end to end. Medium-high for reachability: it depends on whether the submitted markdown keeps the block's leading blank lines. Agent and validate callers usually will not keep them.
- **Location:** `pkg/ontology/edit_session.go:2151` (`setNarrativeOp.apply`), `:2832` (`preserveNarrativeSuffix`), and the narrative block range from `BuildNodeBody`.
- **Problem:** A narrative block that follows an inline field has a range that starts at the end of the field's line, before its `\n`. The block therefore owns the field line's terminator. `apply` replaces the whole range with `preserveNarrativeSuffix(raw, markdown)`, which keeps trailing whitespace but drops leading whitespace.
- **Evidence:** Note body `### Story A\nTBD\n\nstatus:: TODO\n\nTBD\n^story-a\n`. The second narrative block is `Range{89,95}` with `Markdown:"\n\nTBD"`. `SetNarrative(ref, 89, 95, "\n\nTBD", "EDITED SECOND")` followed by `Commit`, with no drift, gives `committed` and writes `status:: TODOEDITED SECOND\n^story-a`. The `status` value is now wrong. The web `NarrativeEditor.tsx` seeds the draft from `block.markdown`, so any editor normalization that strips leading blank lines triggers this. `pkg/validate/ontology_operation_adapter.go:272` forwards caller-supplied markdown unchanged.
- **Recommendation:** Fix ownership at the source: narrative block ranges should start after the newline that ends the previous construct. As defense in depth, have `setNarrativeOp.apply` keep the raw block's leading whitespace prefix the same way it keeps the suffix, and add a post-apply check that every inline field span outside the replaced range still projects the same value. Fail closed otherwise.
- **Effort:** S
- **Dependencies:** None. Pair with EDIT-T-8.

### EDIT-2 BOM and leading-blank notes: the write path and read path disagree about where frontmatter is

- **Severity:** P0 (document corruption)
- **Confidence:** High; reproduced end to end.
- **Location:**
  - `pkg/ontology/projection_bindings.go:292` (`frontmatterRange` uses `obsidian.FrontmatterRegex`)
  - `pkg/vault/obsidian/tags.go:12` (regex `(?s)^\s*---\r?\n(.*?)\r?\n---\s*\r?\n`)
  - `pkg/vault/obsidian/tags.go:22` (`ExtractFrontmatter` requires `HasPrefix(content, "---")`)
  - `pkg/ontology/edit_session.go:2478-2516` (`setFrontmatterField` fallback)
- **Problem:** There are three frontmatter detectors. None of them handles a UTF-8 BOM, because Go's `\s` is ASCII-only. They also disagree about leading whitespace.
  - **BOM:** No frontmatter is detected, so `setFrontmatterField` takes the "no frontmatter" fallback and prepends a new block.
  - **Leading blank line:** The regex finds frontmatter, but `ExtractFrontmatter` (projection) and Obsidian do not. The edit patches a block that no reader treats as frontmatter, so the user's edit "commits" and then appears not to take effect.
- **Evidence:**
  - Path-typed schema `type Spec @node(paths: ["notes/*.md"]) { summary: String }`, note `"\xef\xbb\xbf---\nsummary: Original\n---\n\n# Spec\n"`, then `SetScalarField(summary, "Updated")` and `Commit`. The outcome is `committed` and the file becomes `"---\nsummary: Updated\n---\n﻿---\nsummary: Original\n---\n\n# Spec\n"`. The original block becomes body text.
  - `unsetFrontmatterField` on the same note is a silent no-op, so the result is "unchanged".
  - Leading blank line: `"\n---\nstatus: draft\n---\nBody\n"` is patched in place, and unset leaves `"\n---\n---\nBody\n"`.
- **Recommendation:** Build one frontmatter locator in `pkg/vault` (for example `frontmatter.Locate(content) (r ByteRange, ok bool)`). It should skip an optional BOM, anchor `---` at line 1 exactly as Obsidian does, and include no trailing blank lines. Use it from `ExtractFrontmatter`, `frontmatterRange`, `rebuildContentWithFrontmatter`, and the ontology patchers so read and write cannot disagree. Have edits on a document whose only "frontmatter" is not at line 1 fail closed.
- **Effort:** S–M
- **Dependencies:** Unifies with EDIT-3 and EDIT-13.

### EDIT-3 Web `setFrontmatter`/`appendAlias` edits re-render the whole frontmatter through a map

- **Severity:** P1 (comments lost, key order and value types changed; violates the stated span-patch invariant)
- **Confidence:** High; reproduced.
- **Location:**
  - `pkg/app/web/ontology_sessions.go:1085-1100` (`StageFileTransform` calls `validate.ApplySetFrontmatterInMemory` / `ApplyAliasAppendInMemory`)
  - `pkg/validate/apply_edits.go:45,65`
  - `pkg/vault/obsidian/properties_edit.go:116` (`editFrontmatter`)
  - `pkg/vault/obsidian/tags_edit.go:457` (`rebuildContentWithFrontmatter`: `yaml.Marshal(map)`)
- **Problem:** `ontology.md` says "edits must patch targeted spans instead of re-rendering whole notes". The validation type picker (`web/src/components/validation/TypeChoice.tsx` sends `kind: "setFrontmatter"`) and alias append go through a map round-trip instead.
- **Evidence:** Input `---\n# owner comment\ntitle: Zeta\ndate: 2024-01-01\naliases: [a]\nstatus: draft # inline\n---` with `SetFrontmatterProperty(.., "type", "Meeting")` produces:
  ```
  aliases:
      - a
  date: 2024-01-01T00:00:00Z
  status: draft
  title: Zeta
  type: Meeting
  ```
  Both comments are gone, keys are alphabetized, the flow list became a 4-space block list, and the date became a timestamp. The ontology package already has a span-preserving patcher (`patchFrontmatterFieldSource`, `setRawFrontmatterList`) that handles these cases.
- **Recommendation:** Route `setFrontmatter`/`appendAlias` (and validate `FixKindSetFrontmatter`/`FixKindAppendAlias`) through the ontology span patcher. That means `EditSession.SetRootMetadataList` / a raw-scalar equivalent rather than `StageFileTransform`. Then either delete `obsidian.SetFrontmatterProperty`/`editFrontmatter`/`rebuildContentWithFrontmatter` or move the span patcher down into `pkg/vault` and make the obsidian helpers thin wrappers over it. That leaves one frontmatter writer in the repository instead of three.
- **Effort:** M
- **Dependencies:** EDIT-2 (shared locator).

### EDIT-4 Narrative replay guesses between identical paragraphs and overwrites changed ones

- **Severity:** P1 (the wrong paragraph is edited silently, or a concurrent change is lost)
- **Confidence:** High at function level (probe). End to end, the guess requires identical paragraphs plus drift.
- **Location:** `pkg/ontology/edit_session.go:2785-2826` (`resolveNarrativeBlock`)
- **Problem:** The stated constraints are that "identical unanchored content is ambiguous and must fail closed" and that missing or ambiguous lineage "never authorizes guessing an item from its byte offset". The code does two things that break this:
  - With more than one content match, it picks the candidate nearest the stale `Range.Start`.
  - When no block matches `PreviousMarkdown` but a block sits at the exact old range, it returns that block (`if exact != nil { return exact }`) and overwrites whatever text is there now.
- **Evidence:**
  - Blocks `TODO@250` and `TODO@350` with an op for the old `TODO@200` (the second paragraph before a 150-byte external insertion) select the block at 250, which is the first paragraph.
  - A block at `100-105` whose content changed from `FOOO` to `BAZZ` (same length) is still selected, and the user's text replaces `BAZZ` with no conflict. `fieldChangedConflict` only inspects `setFieldOp`, so there is no other witness.
- **Recommendation:**
  - With a non-empty previous markdown, accept only an exact-range block with matching content, or a unique content match.
  - Return `errCollectionDrift`/`errMissingNode` when there are several matches, or when the block at the range has different content.
  - Make `PreviousMarkdown` required for `SetNarrative`.
- **Effort:** S
- **Dependencies:** EDIT-1 (normalize leading whitespace consistently when comparing).

### EDIT-5 Two journaled transaction engines; the ontology engine's receipt plumbing is dead and its docs have drifted

- **Severity:** P1 (design; duplicated data-safety-critical code)
- **Confidence:** High
- **Location:**
  - Ontology engine:
    - `pkg/ontology/edit_transaction.go` (291 lines)
    - `edit_transaction_journal.go` (406)
    - `edit_transaction_recovery.go` (245)
    - `edit_write_lock.go` (123)
  - Validate engine: `pkg/validate/transaction_*.go` plus `repair_journal_rename_*.go` (about 2,400 lines)
  - Browser commit: `pkg/app/web/ontology.go:1233-1340` (`PreviewCurrentWithRefLineage` then `validate.ApplyRepairSession`)
- **Problem:**
  - Production callers of `EditSession.CommitWithOptions` are only `pkg/app/cli/heading_rename.go:256` and `pkg/ontology/node_link.go:261`. Browser saves go through the validate engine.
  - No production code sets `CommitOptions.CompletionArtifact` or `TransactionID`. So `prepareEditCompletionArtifact`, `publishEditCompletionArtifact`, `cleanupEditCompletionArtifact`, `.rhizome/edit-receipts`, the `Completion` journal field, and the receipt branches in preflight and recovery serve tests only.
  - That dead path also leaks: on any in-loop failure (fingerprint conflict, rename error) `cleanupAfterRollback` removes note temps but never the `.completion-*` temp, and the journal is then removed, so nothing can find it again.
  - `ontology.md` lines 73, 76 and 78 still say browser edit commits "hold `.rhizome/index.lock` plus sorted canonical path leases" and that "the journal binds note replacements and the durable request receipt into one outcome". That is now untrue, and it contradicts the same note's "Edit publication" bullet (browser publication goes through the validate repair engine).
- **Evidence:** `grep -rn "ontology.CommitOptions\|CommitOptions{" pkg cmd` shows only those two callers, setting `VaultWriteLeaseHeld` and diff options. The engines also differ on durability: validate publishes journals with `MOVEFILE_WRITE_THROUGH` on Windows (`repair_journal_rename_windows.go`), and ontology does not (EDIT-12).
- **Recommendation:**
  - **Short term (S):** Delete the completion/receipt/transaction-ID plumbing from the ontology engine and its tests, and correct `ontology.md`.
  - **Target (L):** Extract one source-transaction engine into a leaf package (for example `pkg/vault/sourcetx`: stage temps → journal → witness-checked publish → commit mark → cleanup, plus preflighted recovery and pluggable completion artifacts). Both `validate` and `ontology` consume it. Heading rename and node-link apply already pass whole-file replacements, so they map directly onto it. Then delete `edit_transaction*.go` and `edit_write_lock.go`.
- **Effort:** S (delete dead plumbing) / L (consolidate)
- **Dependencies:** Land EDIT-T-1 (crash harness) before consolidating.

### EDIT-6 An unrecoverable ontology journal aborts startup and blocks every later edit

- **Severity:** P1 (availability; fail-closed with no operator path)
- **Confidence:** High
- **Location:**
  - `pkg/app/cli/serve/startup.go:24`
  - `pkg/app/web/server.go:339`
  - `pkg/ontology/edit_transaction_journal.go:311-334` (`recoverEditWriteJournals` returns the first error)
  - `edit_transaction.go:51`, `edit_session.go:577` (every commit runs recovery first)
- **Problem:** Preflight correctly refuses to roll back when a note changed outside the transaction. But that error is fatal to `RecoverInterruptedWrites` (serve startup) and to every later `CommitWithOptions` in the vault. In the same function, pending validate repair journals produce a warning and startup continues. Nothing in docs or the CLI tells a user how to clear `.rhizome/edit-journal/*.json`. An unsupported journal version or a parse failure has the same effect.
- **Evidence:** A plausible trigger sequence:
  1. Heading rename publishes note A and the process is killed before note B (the journal stays `prepared`).
  2. The user edits A in Obsidian.
  3. On restart, preflight returns `edit recovery stopped because A changed outside the interrupted transaction`, and startup fails with `recover interrupted note edits: ...`.
- **Recommendation:**
  - On preflight failure, move the journal and its artifacts to `.rhizome/edit-journal/quarantine/<id>/` with a diagnostic manifest, and continue startup.
  - Block only writes to the journal's paths.
  - Report the quarantine through the same channel as validate's `repair.recovery_review_required`, with a `rzm validate` or doctor action to restore or discard it.
  - If EDIT-5 consolidation lands, this falls out of validate's existing degraded-mode policy.
- **Effort:** M
- **Dependencies:** EDIT-5 (preferably share validate's policy).

### EDIT-7 Lost-update window between the fingerprint check and the rename, and a "note absent" window

- **Severity:** P2
- **Confidence:** Medium. The window is microseconds, but Obsidian and other external writers do not honor the lease.
- **Location:** `pkg/ontology/edit_transaction.go:187-222`
- **Problem:** The writer runs `ReadFile` and hash, then `Rename(abs→backup)`, then `Rename(temp→abs)`. An external `open(O_TRUNC)+write` between the read and the first rename lands in the inode that becomes the backup. Cleanup then deletes it, and the external write is lost. Separately, between the two renames the note does not exist. A crash there leaves it missing until recovery runs, and sync clients (Obsidian Sync, iCloud) can propagate a delete. Fingerprints are also checked one item at a time, after earlier items are already published, which turns a conflict into a rollback.
- **Recommendation:**
  1. Check every item's fingerprint before publishing any of them.
  2. After `Rename(abs→backup)`, hash the backup. If it differs from the expected value, rename it back and report `SOURCE_CHANGED`.
  3. Either document the remaining fd-held-open window with a `ponytail:` ceiling comment, or switch to "write backup copy from the verified bytes, fsync, then single `Rename(temp→abs)`" so the note path is never absent.
- **Effort:** S
- **Dependencies:** EDIT-5, if consolidating.

### EDIT-8 Conflict kinds are classified by matching error strings

- **Severity:** P2
- **Confidence:** High
- **Location:** `pkg/ontology/edit_session.go:2730-2783` (`conflictFromError`)
- **Problem:** Conflict kinds are a client contract (the web UI and validate map them). The code derives them with `strings.Contains(err.Error(), "missing node")`, `" not found in "`, `"collection drift"` and `"source changed"`, even though sentinels exist and most producers wrap them with `%w`. Any error message that happens to contain those phrases is misclassified, including messages quoting heading text or field names, and messages wrapped by a transform such as heading rename's "source changed since heading rename planning". The type switch over op kinds is a second copy of op dispatch (see EDIT-9).
- **Recommendation:** Use `errors.Is` against the sentinels and make projection's "not found in" producers wrap `errMissingNode`. Have each op expose `conflictTarget() (ref, field string)` instead of the switch.
- **Effort:** S
- **Dependencies:** EDIT-9.

### EDIT-9 `EditSession` is a wide, shallow API and op-kind dispatch is copied in at least six places

- **Severity:** P2
- **Confidence:** High
- **Location:**
  - `edit_session.go` (2,843 lines; the policy limit is about 500)
  - `edit_identity.go:29` (`canonicalizeEditOp` type switch)
  - `edit_session.go:1387` (`editOpMatchesBase`), `:2730` (`conflictFromError`)
  - `pkg/app/web/ontology_sessions.go:1065-1200` (`applyOntologyEditOp`)
  - `pkg/validate/ontology_operation_adapter.go:85-102` (a 16-method `ontologyPreviewSession` interface mirroring `EditSession`) plus `stageOntologyEdit`
- **Problem:**
  - About 25 public stage methods each build an op struct.
  - `setFieldOp` carries 10 flag fields (`IsLinks`, `RequireScalarList`, `Unset`, `ExpectedCaptured`, `RawRootMetadata`, and so on), several of which only make sense together.
  - Two ops do the same raw frontmatter list write: `setRawFrontmatterListOp` (`SetRawFrontmatterList`) and `setFieldOp{RawRootMetadata:true}` (`SetRootMetadataList`).
  - `setFrontmatterField` and `setRawFrontmatterList` duplicate the same patch-then-yaml-fallback block.
  - `SetInlineField` is an alias for `SetScalarField`.
  - `plan()` re-canonicalizes ops that `stage()` already canonicalized.
  - Adding an op means editing every switch.
  - `NodeLinkService.LinkTargets` mixes read and plan behavior with a source-mutating `Ensure: apply` mode on the same method.
  - Heading rename (`pkg/app/cli/heading_rename.go`) uses `EditSession` only as a fingerprint-checked whole-file writer via `StageFileTransform`, and has its own regex heading parser and fence detector parallel to `sectionparse`.
- **Recommendation:**
  - Export op value types (`ontology.SetField{...}`, `ontology.SetNarrative{...}` and so on) that implement one small interface: `target() NodeRef`, `withCanonicalPath()`, `apply(*documentState)`, `conflictTarget()`. Add a single `Stage(op)`.
  - Let the web and validate adapters build ops directly and delete their parallel interfaces and switches.
  - Fold `setRawFrontmatterListOp` into `RawRootMetadata`.
  - Split `edit_session.go` by concern: plan/replay, field ops, structural ops, title/checkbox, narrative, YAML helpers.
  - Split link apply into its own method (`ApplyLinkTargetFixes`) that requires the lease.
- **Effort:** M–L
- **Dependencies:** None, but do it after the bug fixes so the regression tests pin behavior first.

### EDIT-10 Redundant locking and recovery on every commit

- **Severity:** P2
- **Confidence:** High
- **Location:**
  - `edit_write_lock.go:65-110`
  - `edit_session.go:577` and `edit_transaction.go:41-53`
  - `edit_transaction_journal.go:336` (variadic `vaultWriteLeaseHeld ...bool`)
  - `edit_transaction.go:187` (empty `expectedCurrentFingerprint` branch)
- **Problem:**
  - Every writer holds the global `.rhizome/index.lock` lease (it is either acquired or asserted held), and in-process writes are also serialized by `editWriteMu`. The per-note `.rhizome/write-locks/<sha>.lock` files therefore exclude nothing. No other package takes them; validate's engine does not.
  - `CommitWithOptions` runs recovery and lease acquisition, releases, plans, then `writeStatesAtomically` acquires the leases and runs recovery again.
  - In production both callers pass `VaultWriteLeaseHeld: true`, so the try-acquire-global branch is reached only from tests and `server.NewServer` recovery.
  - `expectedCurrentFingerprint == ""` (skip the disk check) is reachable only from tests, because commit plans always set it.
  - `editWriteMu` is process-global across vaults.
- **Recommendation:**
  - Delete the per-path leases.
  - Run recovery once, inside the publication critical section.
  - Replace the variadic bool with an explicit `heldLease` parameter or a lease token type.
  - Make the expected fingerprint mandatory.
  - Key the mutex per vault, or drop it if the lease token proves ownership.
- **Effort:** S
- **Dependencies:** EDIT-5.

### EDIT-11 Repeated reads, parses and diffs per edit

- **Severity:** P2 (perf)
- **Confidence:** High for the redundancy; I did not measure its impact.
- **Location:**
  - `edit_session.go:664-744` (`plan`), `:889` (`populatePlanPayload`), `:839-855` (base replay)
  - `node_link.go:261`
  - `pkg/app/web/edit_session_overlay.go:156`
  - `pkg/app/cli/heading_rename.go:157-170`
- **Problem:**
  - On a first commit, `plan` loads the base snapshot and then immediately reloads and re-parses the same file as "current". That is two reads and two `BuildDocumentSnapshot` calls for identical bytes.
  - `writeStatesAtomically` reads it a third time.
  - `Preview()` always computes unified diffs, including hunks and semantic cleanup through `initdiff.GenerateUnifiedDiff`. The overlay builder (`stagedPlanForConflictedPaths`) and node-link apply (which asks for diff plus preview) discard them.
  - Rebased plans without witnesses replay every op twice (once against base, to detect no-ops).
  - Node-link apply re-runs `LinkTargets`, which re-lists the vault and re-reads every note.
  - CLI heading rename reads every file in the vault with `os.ReadFile` to find inbound links instead of using the indexed link graph.
- **Recommendation:**
  - Reuse the base snapshot as current when it was loaded in the same `plan` call.
  - Make diff generation opt-in everywhere and default it off for internal callers.
  - Compute base no-op only when `editOpMatchesBase` returned false for some op.
  - Have heading rename query backlinks from the index, with a full-scan fallback only when the index is stale.
- **Effort:** S–M
- **Dependencies:** None.

### EDIT-12 Windows durability and safety checks are no-ops

- **Severity:** P2
- **Confidence:** Medium. This comes from reading the code; I did not run anything on Windows.
- **Location:**
  - `edit_sync_directory_windows.go` (`syncEditDirectory` returns nil)
  - `edit_file_links_windows.go` (`editFileHasMultipleLinks` returns false)
  - `edit_transaction_journal.go:262` (`os.Rename` for the journal)
- **Problem:**
  - Journal publication relies on directory fsync for ordering, and on Windows that is a no-op. Validate uses `MoveFileEx(..., MOVEFILE_WRITE_THROUGH)` for the same step, but ontology uses plain `os.Rename`. After power loss, a note rename can survive while the `prepared` journal does not, leaving a half-applied multi-file edit with nothing to recover from.
  - Hard-link refusal silently does nothing on NTFS.
  - Renaming a note that another process holds open fails with a sharing violation. That path triggers rollback, but no test covers it.
- **Recommendation:** Reuse validate's write-through rename helper, or the shared engine from EDIT-5. Implement the NTFS link-count check (`GetFileInformationByHandle`, `NumberOfLinks`). Add a Windows CI job for the publish and recovery tests.
- **Effort:** S
- **Dependencies:** EDIT-5.

### EDIT-13 Inserted text uses LF in CRLF documents

- **Severity:** P3
- **Confidence:** High; verified for new frontmatter.
- **Location:**
  - `edit_session.go:2514` (new frontmatter)
  - `edit_session_section.go:109` (`sectionHeadingSnippet`)
  - Embedded-node and block-ID inserts
- **Problem:** The frontmatter list patcher detects `\r\n` (`edit_session_frontmatter_list.go:139`), but other inserts write `\n`. Example: `"Body\r\nmore\r\n"` with a set field becomes `"---\nstatus: done\n---\nBody\r\nmore\r\n"`, which has mixed line endings.
- **Recommendation:** Add one `documentEOL(content)` helper and use it for every inserted line.
- **Effort:** S
- **Dependencies:** EDIT-2.

### EDIT-14 Minor layering and contract inconsistencies

- **Severity:** P3
- **Confidence:** High
- **Location:**
  - `edit_session.go:12` (imports `pkg/app/cli/init/diff`)
  - `node_workspace.go:12` (`NodeRange` duplicates `ByteRange`, plus the `nodeRange` converter)
  - `edit_session.go:265` (`RestoreBaseFingerprints`)
- **Problem:**
  - A core package imports an app/CLI package for diffing.
  - There are two identical range types.
  - `RestoreBaseFingerprints` keys `s.docs` by the trimmed, non-canonical path, while `RestoreBases` canonicalizes. A non-canonical key would be ignored silently and a fresh base loaded, which hides drift. Because it installs current disk content as the base, it also cannot raise `FIELD_CHANGED` for drift that happened before the restore. It is a weaker sibling of `RestoreBases` with no doc saying so.
  - `maxConcurrentFilePlans = 8` imposes a concurrency-safety contract on `StageFileTransform` callers. No benchmark backs it.
- **Recommendation:**
  - Move diffing into a neutral leaf package (for example `pkg/textdiff`).
  - Delete `NodeRange`.
  - Canonicalize keys in `RestoreBaseFingerprints` and document its weaker guarantee, or migrate validate to `RestoreBases`.
  - Keep the plan worker pool only if a benchmark justifies it.
- **Effort:** S
- **Dependencies:** None.

## Findings — tests

Runtime: 83 matching tests, 3.18 s in total, all passing (see the table at the end). The suite is not slow. The problems are realism and gaps.

### EDIT-T-1 Add: crash-at-every-step harness for the real writer (highest-value missing test)

- **Verdict:** Add
- **Why:** Every recovery test builds journal and artifact states by hand. None runs `writeStatesAtomically` and stops it at each step, so the all-or-nothing claim is argued but never exercised. The completion-temp leak in EDIT-5 is exactly what such a harness finds.
- **Replacement:**
  - Add a test-only seam, `var editWriteFault func(step string, index int) error`, checked at each of these points: after temp create, after the backup reservation, after the journal is prepared, after `rename(abs→backup)` for item k, after `rename(temp→abs)` for item k, after the commit mark, and during cleanup.
  - Run one table over steps × {1, 2, 3 notes}. For each case, "crash" by returning a sentinel so the deferred cleanup is skipped, run `RecoverInterruptedEdits` twice, and assert: every note equals all-old or all-new (all-new only at or after the commit mark), no `*.rhizome-write-*`, `.*.rhizome-backup-*` or journal files remain, and the second recovery is a no-op.
  - This one test replaces most of the hand-built "reachable state" fixtures.

### EDIT-T-2 Rewrite: the recovery fixture builds unreachable states

- **Verdict:** Rewrite
- **Tests:** `edit_transaction_recovery_test.go:newEditRecoveryFixture` and all its users (`TestRecoverCommittedEditPreservesLaterSource`, `TestRecoverCommittedEditPreflightsAllEvidence`, `TestRecoverEditToleratesRemovedArtifacts`, and so on)
- **Why:** The fixture writes note = `new`, temp = `new`, backup = `old` and then marks the journal `committed`. In a real run the temp has already been renamed onto the note when commit is marked, so this state cannot occur. The tests still pass, but they prove recovery of states the writer never produces, while reachable states go unexercised (see T-1).
- **Replacement:** Generate the baseline state with the T-1 harness. Keep the adversarial mutations (symlink or hard-link backup, escaped path, non-canonical note path, overlapping case-folded names, changed artifacts), because those test tamper-resistance, not crash consistency.

### EDIT-T-3 Merge or delete duplicate journal tests

- **Verdict:** Delete or merge
- **Tests and why:**
  - `edit_transaction_journal_test.go:TestRecoverEditWriteJournalPreservesUnexpectedExternalChange` duplicates `edit_transaction_recovery_test.go:TestRecoverPreparedEditKeepsAmbiguousSourceAndMissingParent/edited` (same setup, same assertions). Delete it.
  - `edit_transaction_journal_test.go:TestRecoverEditWriteJournalTreatsCompletionReceiptAsPartOfPublication/"published receipt completes transaction"` duplicates `TestRecoverCommittedEditPreservesLaterSource/"unchanged with receipt"`. Delete that sub-case.
  - `TestRecoverEditWriteJournalRollsBackPartialPublication` and `TestRecoverInterruptedEditsContextWaitsForWriterBeforeReplayingJournal` each hand-roll a 25-line fixture instead of using `newEditRecoveryFixture`. Move them onto the shared fixture, or onto the T-1 harness.
  - If EDIT-5's dead completion plumbing is deleted, also delete:
    - `TestRecoverEditWriteJournalRejectsUnsafeCompletionTempPath`
    - the remaining receipt sub-case above
    - the `receipt=true` half of `TestRecoverCommittedEditPreservesLaterSource`'s matrix
    - the `changed receipt temp`, `changed receipt` and `missing receipt` states in `TestRecoverCommittedEditPreflightsAllEvidence`
    - the `symlink receipt` state in `edit_transaction_recovery_unix_test.go:TestRecoverEditRejectsReplacedArtifactsAndParents`

### EDIT-T-4 Rewrite: the disk-race test depends on how many times the file is read

- **Verdict:** Rewrite
- **Test:** `edit_session_regressions_test.go:TestEditSession_CommitChecksDiskBeforeReplace` (with `editSessionRaceNoteReader{mutateAfterRead: 2}`)
- **Why:** The external write is triggered on the second `GetContents` call. That pins the current base-then-current double read, which EDIT-11 removes. After that change the mutation would fire at a different stage, or never, and the test would silently stop exercising the publication check.
- **Replacement:** Use the T-1 seam (or an explicit `beforePublish` hook) to write the external content after planning and before the fingerprint check. Add a second case that writes after the check and before the rename (EDIT-7) and asserts `SOURCE_CHANGED` with the external bytes intact.

### EDIT-T-5 Delete: the parallelism assertion

- **Verdict:** Delete, or rewrite as a race check
- **Test:** `projection_edit_test.go:TestEditSession_PreviewPlansFilesConcurrently`
- **Why:** It asserts that more than one transform ran at the same time, using 25 ms sleeps and atomic counters. That is an implementation detail, not a behavior contract, and it pins a worker pool that may not be justified (EDIT-14).
- **Replacement:** If the pool stays, run the existing multi-file preview tests under `-race` (the full gate already does) and assert only the deterministic plan output, which `TestEditSession_PreviewOrdersFilesDeterministically` already covers. Otherwise delete it together with the pool.

### EDIT-T-6 Delete: subsumed inline-field test

- **Verdict:** Delete
- **Test:** `projection_edit_test.go:TestEditSession_SetInlineFieldPreservesPackedSiblings`
- **Why:** Its source line `id:: ^SPEC-001-US1 summary:: ... status:: ready` and its op are exactly `edit_session_inline_test.go:TestEditSessionPackedInlineFieldsPreserveUntouchedSource/"single field with lineage"`. The table version also runs preview, lineage and commit modes.

### EDIT-T-7 Add: frontmatter encoding edge cases as one property/table test

- **Verdict:** Add, and replace part of the existing hand-written set
- **Why:** None of the 14 `TestPatchFrontmatter*`/`TestUnsetFrontmatter*` tests in `edit_session_frontmatter_test.go` covers BOM, a leading blank line, CRLF without frontmatter, or trailing blank lines after the closing fence. EDIT-2 survived because of that. The existing tests call the private `patchFrontmatterFieldSource`, which is acceptable for a parser, but they never check that the write path and read path agree.
- **Replacement:**
  - Use one table over corpus × field shape (scalar, empty, flow list, block list, block scalar, quoted key, comment-adjacent) × value (plain, YAML-looking, multiline, empty) × encoding (LF, CRLF, BOM, leading blank).
  - Drive it through the public `EditSession` (`SetScalarField`/`SetScalarListField`/`UnsetField` + `Preview`).
  - Assert: (a) `ExtractFrontmatter` of the result yields the intended value; (b) every other key decodes identically; (c) bytes outside the edited key's lines are unchanged; (d) there is exactly one frontmatter block.
  - The BOM rows fail today.
  - Keep `TestPatchFrontmatterFieldSourceFailsClosedForUnsupportedSpan` and the flow-mapping rejection as explicit fail-closed rows.

### EDIT-T-8 Add: narrative boundary, ambiguity and witness tests

- **Verdict:** Add (all three fail today)
- **Replacement:** In `projection_edit_test.go` next to `TestEditSession_SetNarrative_CommitRebasesByMatchingNarrativeBlock`, add:
  1. A narrative block after an inline field, edited with markdown that has no leading blank lines. Assert that the field value is unchanged and that the field and narrative remain separate lines (EDIT-1).
  2. Two identical paragraphs in one node, an external insertion above them, and an edit to the second. Expect a conflict, not an edit to the first (EDIT-4).
  3. The paragraph at the staged range changed externally to text of the same length. Expect a conflict (EDIT-4).

### EDIT-T-9 Add: concurrent commit coverage

- **Verdict:** Add
- **Why:** No test runs two writers at once. The only goroutine test is the recovery lease wait.
- **Replacement:** Two `EditSession`s stage different values for the same field on one note and call `CommitWithOptions` from two goroutines while holding the lease the way production does. Assert that exactly one is `committed`, the other is `conflicted` (`SOURCE_CHANGED` or `FIELD_CHANGED`), and the file contains the winner's bytes. A second case runs an external `os.WriteFile` loop during commit and asserts the external content is never lost silently: either the edit conflicts or the external write lands afterward.

### EDIT-T-10 Add: frontmatter preservation through the web/validate paths

- **Verdict:** Add (fails today)
- **Replacement:** A `pkg/validate` test of `ApplySetFrontmatterInMemory` and `ApplyAliasAppendInMemory` (or of `applyOntologyEditOp` for `setFrontmatter`/`appendAlias` in `pkg/app/web`) with comments, non-alphabetical key order, a flow list and a date. Assert that only the target key's bytes change (EDIT-3).

### EDIT-T-11 Add: stuck-journal startup policy and a Windows lane

- **Verdict:** Add after EDIT-6 and EDIT-12
- **Replacement:**
  - A `pkg/app/cli/serve` test: a `prepared` journal whose note changed externally. Startup succeeds, the journal is quarantined, a diagnostic is emitted, and writes to that path are refused.
  - A Windows-only test that publishes over a note held open by another handle, and asserts clean rollback with the journal removed.

### EDIT-T-12 Rewrite (minor): table rows pin whitespace left behind by unset

- **Verdict:** Rewrite once the contract is decided
- **Tests:** These rows of `edit_session_inline_test.go:TestEditSessionPackedInlineFieldsPreserveUntouchedSource` expect trailing or leading spaces left behind after removal:
  - "duplicate field beside another field" → `"Keep this text. \n"`
  - "remove same-line duplicates around undeclared property" → `" unknown:: Keep this text \n"`
  - "remove field after narrative" → `"Keep this prose. \n"`
- **Why:** These are side effects of how removal is implemented, not chosen behavior. Decide whether removal should collapse adjacent whitespace. Until then, label these rows as current behavior so nobody treats them as a contract.

### EDIT-T-13 Maintenance: fixture boilerplate

- **Verdict:** Rewrite (mechanical)
- **Tests:** `projection_edit_test.go` (2,032 lines, 44 tests) and `edit_session_regressions_test.go`
- **Why:** Most tests repeat `t.TempDir`, `writeOntologyTestConfig`, `writeOntologySchema`, `writeOntologyNote`, `LoadSchema` and `NewEditSession` (about 12 lines each). Both files exceed the 500-line guidance.
- **Replacement:** Add a `newEditFixture(t, schema, map[path]content) (*EditSession, readFile func(path) string)` builder and split the file by op family (field, structural, narrative, title, commit/rebase).

## Suggested refactor sequence

1. **Stop the corruption, test first (S each).** Land EDIT-T-8 then EDIT-1 and EDIT-4. Land EDIT-T-7 then EDIT-2 (the shared BOM-aware locator). Land EDIT-T-10 then EDIT-3 (route `setFrontmatter`/`appendAlias` through the span patcher). These are local and independent.
2. **Make failure survivable.** EDIT-6 (quarantine instead of blocking startup), EDIT-7 (check all fingerprints first, verify the backup after rename), EDIT-12 (write-through rename on Windows, link-count check).
3. **Build the safety net, then delete.** EDIT-T-1 (crash harness), EDIT-T-2, EDIT-T-4. Then delete the ontology completion/receipt/transaction-ID plumbing and the per-path leases, and recover once (EDIT-5 short term, EDIT-10, EDIT-T-3). Fix `ontology.md` lines 73, 76 and 78 in the same change and bump `last-verified`.
4. **Consolidate engines (L).** Extract the shared source-transaction package, move validate and ontology onto it, and delete `edit_transaction*.go` and `edit_write_lock.go` (EDIT-5 target). The T-1 harness is the regression gate.
5. **Deepen the API.** Exported op values with a single `Stage`, sentinel-based conflict kinds, a split of `edit_session.go`, and separate link apply (EDIT-9, EDIT-8). Then the perf items (EDIT-11) and minor cleanups (EDIT-13, EDIT-14, EDIT-T-5, EDIT-T-6, EDIT-T-12, EDIT-T-13).

## Slow-test table (measured)

Command: `go test -count=1 -json -run 'Edit|Transaction|Recovery|Journal' ./pkg/ontology/` (run once). Result: 83 top-level tests, 3.18 s summed, package wall time 3.48 s, no failures.

| Test | Seconds | Note |
| --- | --- | --- |
| `edit_session_inline_test.go:TestEditSessionPackedInlineFieldsPreserveUntouchedSource` | 0.72 | 15 rows × 3 modes, including disk commit; fine |
| `edit_transaction_recovery_test.go:TestRecoverCommittedEditPreservesLaterSource` | 0.48 | 12-case matrix; halves if receipt plumbing is deleted |
| `edit_transaction_recovery_test.go:TestRecoverCommittedEditPreflightsAllEvidence` | 0.47 | 11–12 states |
| `edit_transaction_journal_test.go:TestRecoverInterruptedEditsContextWaitsForWriterBeforeReplayingJournal` | 0.18 | fixed 100 ms negative wait |
| `edit_transaction_recovery_unix_test.go:TestRecoverEditRejectsReplacedArtifactsAndParents` | 0.13 | |
| `node_link_recovery_test.go:TestNodeLinkServiceApplyRecoversCommittedEditAfterLaterSourceChange` | 0.08 | |
| `edit_transaction_recovery_test.go:TestRecoverEditToleratesRemovedArtifacts` | 0.07 | |
| `edit_transaction_journal_test.go:TestRecoverEditWriteJournalTreatsCompletionReceiptAsPartOfPublication` | 0.06 | delete per T-3 |
| `edit_transaction_recovery_unix_test.go:TestRecoverPreparedEditPersistsCommitBeforeCleanupFailure` | 0.05 | |
| `projection_edit_test.go:TestEditSession_PreviewPlansFilesConcurrently` | 0.05 | sleep-based; delete per T-5 |

No test is slow enough to matter. Test priorities are realism (T-1, T-2) and the missing edge cases (T-7 to T-11), not speed.
