---
summary: "Lane report from the 2026-10-17 subsystem quality review: Validation subsystem review. Findings are a snapshot of commit 5251923f."
---

# Validation subsystem review

Scope: `pkg/validate` (+ `catalog`, `identifierreconcile`, `namespaceadmission`, `namespacegit`), `pkg/app/validationrun`, `pkg/app/validationproduct`, `cmd/validation_product.go`, `cmd/validation_product_runner.go`, and the reuse seams into `pkg/ontology`, `pkg/notemeta`, `pkg/vault/obsidian`. Read-only review at `5251923f` on `t3/critical-subsystem-audit`. Tests were run once for timings (all passed, 4 skips).

## Summary

`pkg/validate` is two subsystems sharing one package. About 6k production lines are the checks plus their orchestration. About 10k production lines (and about 11k of the 17.5k test lines) are a general journaled filesystem write engine: repair transactions, native namespace moves with Git staging, lifecycle policy, and review authority. Browser saves, `rzm note namespace` moves, serve startup, and indexing all depend on that engine. The stated design mostly holds where it is enforced. Check ids are stable and catalog-backed. Issues are truncated centrally after complete keying. Prepared-runtime execution is the only production path.

Several stated invariants are only nominal. The "one typed registry" is really three sources: the catalog, the constants, and a hand-written snapshot switch. "Domain-specific prerequisites" are broken by one metadata probe that certifies three domains. The non-prepared `EnsureFreshRuntime` path that the docs forbid is still in the code; it is just unreachable from production. The most serious correctness risk is that the default-on `ontology` check emits a **safe** block-id migration fix whose reference rewrite is a raw regex. That regex ignores code spans and frontmatter, so it can silently edit code examples.

The broader correctness risk is that checks re-parse Markdown with legacy `obsidian` scanners and three different link resolvers. The run snapshot already carries the indexer's provider facts (links, fragment targets, frontmatter), and those are ignored, so HTML notes and some Markdown links are judged differently from how the indexer sees them. No test compares the two.

Agent JSON also embeds whole-file repair payloads that `--max-issues` does not bound. Test volume is proportional to the write engine, which is heavily and well tested. It is not proportional to the checks: several checks have only one or two behavior tests, `companion_docs` has none inside the package, and no test checks agreement with the indexer.

## Findings — production code

### VAL-1 Safe block-id migration rewrites links inside code blocks and frontmatter
- **Severity:** P1 (silent content mutation in a `safe`, default-suite fix)
- **Confidence:** High on the mechanism. A one-line regression test would confirm the end-to-end effect.
- **Location:** `pkg/validate/identifier_block_ids.go:313` (`rewriteBlockIDReferenceEdits`), `:364` (`rewriteResolvedBlockFragments`); `pkg/validate/apply_edits.go:12` (`applyBlockFragmentRewrite`); `pkg/validate/repair_planner.go:525`; `pkg/validate/check_orphan_block_ids.go:156`.
- **Problem:** Detection uses `obsidian.ScanWikilinks`/`ExtractMdLinks`. These *return* links inside code with `InsideCodeBlock=true`; they do not skip them (`pkg/vault/obsidian/wikilinks.go:578-620`). `rewriteBlockIDReferenceEdits` never checks `InsideCodeBlock`. The edit is then applied by `rewriteResolvedBlockFragments`, which runs `regexp.MustCompile(`(!)?\[\[(.+?)\]\]`)` and a Markdown-link regex over the **whole file**, with no code-span, inline-code, or frontmatter awareness. The parent `FixAction` is `FixSafetySafe` (`identifier_block_ids.go` ~line 187), and `ontology` is in `DefaultChecks`. A note whose only reference to `^old` is a code example still gets an edit. A note with one real reference and one example gets both rewritten. This contradicts the subsystem rules "Link-extracting checks skip links in fenced, indented, and inline code" and "safe fixes that rewrite content beyond the issue's span". `RunOrphanBlockIDs` has the same detection gap, so code-example references keep anchors out of the orphan list (false negatives).
- **Evidence:** `grep -n InsideCodeBlock pkg/validate/*.go` matches only `check_link_hygiene.go:72` and `check_fragile_external.go:94`. No test fixture for block-id rewriting contains a fenced block (grep for a triple backtick in the relevant `_test.go` files returns nothing).
- **Recommendation:** Delete `rewriteResolvedBlockFragments`. Make detection return byte spans (`WikilinkDetail.Start/End`, md-link spans) filtered by `!InsideCodeBlock`, and emit `FixEdit{StartByte, EndByte}` edits that the planner splices exactly. This is the same approach link-hygiene already uses (`linkHygieneEdit{start,end}`). Apply the same filter in `check_orphan_block_ids.go:156,169`. Add the regression test from VAL-T-4.
- **Effort:** S
- **Dependencies:** None. Do this first.

### VAL-2 Checks re-parse Markdown and resolve links differently from the indexer, ignoring provider facts already in the run snapshot
- **Severity:** P1 (false positives and false negatives; HTML notes are effectively unvalidated)
- **Confidence:** High that the duplication and divergence exist. Medium on each concrete false-positive example; the agreement tests in VAL-T-4 would settle them.
- **Location:** `pkg/validate/run_snapshot.go:26` (captures `notemeta.NoteSourceSnapshot`, which includes `Links`, `FragmentTargets`, `Frontmatter`, `Aliases`, `Diagnostics`; see `pkg/notemeta/source_snapshot.go:18-38`); `pkg/validate/broken_links.go:80` → `pkg/vault/obsidian/health.go:392-480`; `check_link_hygiene.go:56-150`; `check_orphan_block_ids.go:126-175`; `check_fragile_external.go:81-93`; `identifier_block_ids.go:47,329`; `check_code_frontmatter.go:47`.
- **Problem:** The snapshot is built through the indexer's provider runtime, but no check reads `source.Links`, `source.FragmentTargets`, or `source.Frontmatter`. The only provider-fact consumer is `link_history.go:205-215`, which uses historical content. Instead, each link check re-scans every note body with the legacy Markdown scanners and builds its own `NotePathCache`. Up to five full-vault link scans and four cache builds happen per `all`/`audit` run. The results diverge in three ways:
  1. **Three resolution policies inside validation.** `broken_links` uses cache, then an Obsidian `VaultFileIndex` fallback (`health.go:649-678`). Orphan, identifier, and fragile checks use the cache only. Link-hygiene uses its own `linkHygieneIndex` plus `VaultFileIndex`.
  2. **The indexer uses a fourth policy.** `notemeta.ResolveProjectedLink` (`pkg/notemeta/uri_link.go:16-33`) falls back from a failed relative Markdown path to `cache.ResolveNote` by name, which validation's `resolveMarkdown` does not. It also has no `VaultFileIndex` fallback. So `[x](Foo.md)` written in `sub/a.md`, where only a root-level `Foo.md` exists, plausibly yields an indexer edge and a `broken_note_link` at the same time.
  3. **Format blindness.** The snapshot reader exposes all formats (`run_snapshot.go:105`). `broken_links` and `link_hygiene` run Markdown parsers over HTML notes, so `<a href>` links are never checked. `buildFragmentIndex` (`health.go:312`) knows only Markdown headings plus `<a name|id>`. A `[[effort#Deviations]]` link into an HTML effort, which this repo's config includes (`docs/efforts/**/*.html`), would plausibly be reported as `broken_heading_link`. Meanwhile `markdownValidationSources` (`markdown_sources.go:14`) claims to be "the only admission path" for Markdown-syntax checks; `broken_links` and `link_hygiene` bypass it.
- **Recommendation:** Make the snapshot the single parsed input. Add a `validationRunSnapshot.links()` view that returns provider `Links` and `FragmentTargets` with spans and an in-code flag. Make one resolver the canonical one: move `obsidian.linkResolver` semantics into `notemeta` beside `ResolveProjectedLink`, or the reverse, and decide explicitly whether the `VaultFileIndex` fallback applies to indexer edges too. Every link check then consumes `(source, resolvedTarget, fragment, span)` tuples. Delete the per-check `BuildNotePathCacheWithAliases` calls (`check_orphan_block_ids.go:126`, `check_fragile_external.go:81`, `identifier_block_ids.go:47,158`, `apply_edits.go:38`) and `aliasMapFromNotes` (`alias_cache.go`, a duplicate of `obsidian.aliasesFromNotePaths`). Write the agreement tests (VAL-T-4) **before** switching, and let them record the intended differences.
- **Effort:** L
- **Dependencies:** VAL-T-4 first. This overlaps the vault-core and notemeta lanes; coordinate the resolver owner.

### VAL-3 `pkg/validate` owns the vault's general journaled write engine (repair, browser saves, namespace moves with Git)
- **Severity:** P1 (ownership and boundary; the largest structural cost in this lane)
- **Confidence:** High
- **Location:** `pkg/validate/{transaction*,repair_*,namespace_*,replace_file_*,sync_directory_*,postapply,lifecycle_policy,fix_plan}.go` are 7,674 production lines and 8,302 test lines. Add `apply*.go`, `identifier_repair_*`, `ontology_*_adapter.go`, `namespacegit/`, and `namespaceadmission/` for roughly 10k production lines. External callers: `pkg/app/web/ontology.go` (`ApplyRepairSession`, `BuildOntologyEditSessionRepairResult`), `pkg/app/web/ontology_commit_receipt.go` (`PublishRepairCompletionArtifact`), `pkg/app/cli/note_namespace_apply.go` (`ApplyNamespaceMutation`), `pkg/app/cli/serve/startup.go` (`StabilizePendingRepairJournals`), `pkg/app/web/ontology_sessions.go` (`Apply*InMemory`). A second journal implementation exists in `pkg/ontology/edit_transaction_journal.go` and `edit_transaction_recovery.go` (651 lines, `.rhizome/edit-journal`), with its own completion-artifact code parallel to `transaction_completion.go`.
- **Problem:** "Validation" is the owner of note writes for every mutating surface. That makes the package nearly impossible to reason about as a "checks" module. It couples the write path to check registry and selection types (`Options`, `RunContext`, `Result`), and it puts two crash-recovery journals with different invariants in two packages. `ApplyFixPlan` is a single 481-line function (`apply.go:14`).
- **Recommendation:** Extract a `pkg/vault/writetx` (name to taste) that owns plan finalization, transactions, journal v1/v2, recovery, namespace and Git staging, completion artifacts, path reservations, review store, and lifecycle policy. Its API takes a `RepairPlan` and two injected callbacks: `Refresh(lease, delta)` and `Postcheck(runtime, scope)`. Validation then becomes a client: it builds plans from findings and supplies the postcheck (`RunSuiteOncePrepared`). That breaks the current engine→suite call-back cycle (`repairPostApplyCheck`, `repairSessionPostcheck` in `types.go`). In the same effort, decide whether `ontology/edit_transaction_*` should become a client of that engine. The subsystem note already says browser saves route through the repair engine, so the ontology journal may now serve only direct `EditSession.Commit` callers; verify before deleting. Split `ApplyFixPlan` into recover → plan-under-lease → prepare → commit → refresh/postcheck → cleanup.
- **Effort:** L
- **Dependencies:** VAL-6 and VAL-10 shrink `Options`/`RunContext` first, which makes the seam cleaner. Coordinate with the ontology and web lanes.

### VAL-4 Every read-only validation builds a full repair plan, and agent/CI JSON serializes whole-file contents without a bound
- **Severity:** P1 (agent-facing output size and token budget; wasted I/O on every run)
- **Confidence:** High that the payload is serialized. Medium on how large it gets in practice; that depends on how many files have fixes.
- **Location:** `pkg/validate/suite.go:183` (`BuildRepairPlan` is called unconditionally inside `runSuiteOnce`); `repair_planner.go:629-641` (`wholeFileRepairOperation` sets `Content: after` and `Expected: [{Text: before, Replacement: after}]`); callers at `repair_planner.go:305` and `repair_planner_ontology.go:153` keep the whole-file `Expected`; `transaction.go:74` (`Content []byte json:"content,omitempty"`), `:23-28` (`ExpectedText` JSON-tagged); `pkg/app/cli/ci_renderer.go:217-220` (`renderCIJSON` encodes the entire `ValidationResult`, including `fixPlan.operations`); `apply_edits.go:12-41` (`applyBlockFragmentRewrite` calls `GetNotesList` and reads every note for aliases **per edit** during planning).
- **Problem:** `--max-issues` truncates `Issues` but not `FixPlan.Operations`. Each write operation carries the full post-repair file in base64. Block-id and link-upgrade operations also carry the full before and after text in `expected`, so one file can appear three times. The docs say JSON round trips cannot drive apply (`ApplyRepairSession` needs the in-memory `Result`), and `--from-plan` consumes only action IDs and issue keys. So this payload buys nothing. Planning also does `os.ReadFile` per fixable path on the live tree, and an O(edits × notes) alias rebuild for block-fragment edits, on every `rzm validate`, including the indexing snapshot run at `MaxIssues=500`.
- **Recommendation:** (a) Mark `RepairOperation.Content` and the source-text fields of `ExpectedText` `json:"-"`. Keep `sourceHash`, spans, and a short diff or summary, and add a regression test asserting no `content` key in agent JSON. (b) Build the plan lazily: compute actions and issue membership eagerly, which next actions need, but materialize operations only for `validate fix` or a repair review. (c) Build the alias cache once per plan, not per edit; this falls out of VAL-2.
- **Effort:** S for (a), M for (b)
- **Dependencies:** None for (a).

### VAL-5 Three orchestrations of "run the configured default suite", with copy-pasted facts, config, and journal composition
- **Severity:** P1 (consistency of the persisted snapshot; drift risk on every new check or fact)
- **Confidence:** High
- **Location:**
  - CLI/agent/CI: `cmd/validation_product_runner.go:46-140` → `validationproduct.RunWithProjection`.
  - Serve live: `pkg/app/validationproduct/live.go:43-89` → `RunWithProjection`.
  - Indexing snapshot: `pkg/validate/index.go:22,109-136` (`IndexValidationSnapshot` → `runIndexedValidation` → `runSuiteOnce`), called from `pkg/app/indexing/unified.go:809`.
- **Problem:**
  - **The indexing path skips applicability entirely.** No feature facts, no prerequisite probes, no `blocked`/`not_applicable` resolution. It maps legacy `Skipped` to `not_applicable` with a third mapper (`index.go` `indexedDiagnosticOutcomes`). The other two are `validationrun.promoteSoftSkippedChecks` and `BuildValidationResult`. The persisted snapshot the web UI serves can therefore come from two producers with different outcome semantics.
  - **Exact duplicates:**
    - Feature facts: `live.go:98` `liveVaultFeatureFacts` ≡ `validation_product_runner.go:215` `validationVaultFeatureFacts`, and `liveCodeConfigured` ≡ `validationCodeConfigured`.
    - Suite config conversion: `live.go:91` `liveSuiteConfig` ≡ `pkg/app/cli/validation_config.go:12` `ValidationSuiteConfigFromLocal` ≡ `pkg/validate/index.go` `validationSuiteConfigFromLocal`.
    - Local-config loading with the `ErrNoLocalConfig` fallback, in three places.
    - "Pending journal ⇒ every outcome blocked" with identical literal strings and the `pending_repair_journal` evidence code: `validate/suite.go:222`, `validationrun/run.go:139`, `validationproduct/projection.go:156`.
  - **A dummy suite run to get a string.** `projection.go:183` runs `RunSuiteOncePrepared(ctx, options, nil, nil)` only to read the recovery command back out of `NextActions`, and fails if journals vanished in between.
  - The doc acknowledges that "both fact producers" must be kept in sync (validate.md line 69).
- **Recommendation:** Create one `validationproduct.Prepare(vaultDef, selectors) (Prepared, error)` that loads config, resolves selection, computes feature facts, and builds the `RunContext`. Have CLI, live, and indexing all go through `RunValidation`. Give indexing a "prepared runtime, no projection refresh" probe adapter so `IndexValidationSnapshot` stops calling `runSuiteOnce` directly. Export `validate.RepairRecoveryOutcome(checks, journals, applyCommand)` as the single journal-blocked composer and delete the other two along with the dummy suite run.
- **Effort:** M
- **Dependencies:** VAL-6 (dead path removal) simplifies this.

### VAL-6 Dead non-prepared execution path keeps the forbidden `EnsureFreshRuntime` fallback alive
- **Severity:** P2
- **Confidence:** High. Production callers were grepped: the only external run entry is `validate.RunSuiteOncePrepared` (2 calls in `pkg/app/validationproduct/projection.go`), plus `IndexValidationSnapshot`, which always passes a non-nil runtime from `ontology.PublishedRuntimeWithStore`.
- **Location:** `suite.go:47` `RunSuiteOnce` (test-only callers), `suite.go:110-124` (`EnsureFreshRuntime` branch), `suite.go:265` `RunCheck` (test-only); `checks.go:219` `RunOntology`; `checks.go` `RunCodeAnchors` (opens its own store); `identifier_check.go:33` `RunIdentifiers`, `:53` `RunIdentifiersWithStore` (zero callers anywhere); `frozen_scope_drift.go:66` `RunFrozenScopeDrift` (turns a runtime error into `Skipped`, which later becomes `not_applicable`, so an error is silently swallowed); `broken_links.go:32` `RunBrokenLinks` (no-ctx wrapper, test-only); the nil-runtime fallbacks in `registry.go` `run*Registered`.
- **Problem:** About 150 lines of production code and the `runtimePrepared bool` plumbing exist only so tests can call checks without a prepared runtime. They contradict "Do not re-enter `EnsureFreshRuntime`" and "no validation path ... falls back". The error-to-`Skipped` conversion is a latent false-clean.
- **Recommendation:** Delete all of the above. Keep one entry point, `RunSuite(ctx, opts, runtime)`, which requires a runtime for ontology checks and a code store for code checks. Move tests to a shared `preparedRuntimeFixture(t, files)` helper; `frozen_scope_drift_test.go:58` already has the pattern.
- **Effort:** S–M (mostly test migration, see VAL-T-3)
- **Dependencies:** None.

### VAL-7 The "one typed registry" is three sources plus hand switches; descriptor metadata is mostly constant
- **Severity:** P2
- **Confidence:** High
- **Location:** `pkg/validate/catalog/catalog.go:16-31` (ID, name, aliases); `types.go:15-44` (constants, plus the legacy `CheckAliases = CheckIdentifiers` shim); `registry.go:126-249` (descriptors); `run_snapshot.go:71-79` (`checksNeedValidationRunSnapshot`, a hand-maintained list); `registry.go:330-466` (`validateCheckRegistry` and helpers, about 140 lines validating a static literal at `init`).
- **Problem:** Adding a check touches four places, and forgetting the snapshot switch silently runs the check against the live reader. Several descriptor fields are constant across the registry:
  - `MissingPrerequisiteOutcome` is `PrerequisiteBlocked` for all 14 checks.
  - `PrerequisiteMode`, `PreparationCommand`, and `ExecutionSurfaces` differ only for `code_anchors`.
  - `RuntimeRequirement` duplicates "has `ProjectionOntology`", and `preparedCodeStoreRequired` (`suite.go:257`) re-derives the code need from `ProjectionDomains`.

  `normalize` (`catalog.go:89`) and `normalizeCheckName` (`checks.go:472`) are duplicates.
- **Recommendation:** One `checkSpec` value per check, holding identity (the catalog stays the dependency-light source for REST/GraphQL), `Needs{Sources, Ontology, CodeStore}`, suites, applicability, remediation, and `Run`. Derive snapshot capture, runtime acquisition, and the code-store requirement from `Needs`. Collapse `PrerequisiteMode`/`PreparationCommand`/`ExecutionSurfaces`/`MissingPrerequisiteOutcome` into `RequiresPersistedCodeIndex bool` while keeping the JSON catalog shape stable by computing those fields. Shrink `validateCheckRegistry` to the invariants a literal can actually break: duplicate ids or aliases, and audit vs all membership. Delete `CheckAliases`; the architecture policy is "no backward-compatibility shims".
- **Effort:** M
- **Dependencies:** VAL-6.

### VAL-8 One metadata probe certifies three "domain-specific" projection domains
- **Severity:** P2 (the stated invariant is violated; prerequisites are decorative)
- **Confidence:** High
- **Location:** `pkg/app/indexing/validation_projection.go:563-566`; consumer `pkg/app/validationproduct/projection.go:291-297`.
- **Problem:** `ProjectionDomainLinks` and `ProjectionDomainMarkdownTargets` are assigned the same `metadataState` evidence as `ProjectionDomainMetadata`. The subsystem checklist names this exact failure: "one probe claims multiple unproven domains". It is also moot, because `broken_links` never reads link rows; it re-scans sources (VAL-2).
- **Recommendation:** Delete the `links` and `markdown_targets` domains and require only `metadata` for `ProjectionValidation`, or, if VAL-2 makes checks consume persisted link rows, prove them separately. Update `validate.md`.
- **Effort:** S
- **Dependencies:** None (the indexing lane owns the file).

### VAL-9 Per-check result boilerplate: hand-set counts, scattered key computation, triplicated aggregation
- **Severity:** P2
- **Confidence:** High
- **Location:** 21 production `StableIssueKey(` call sites (for example `checks.go` `RunCodeAnchorsWithStore`, `check_link_hygiene.go:79,104`); the error-reset block (`OK=false; Error=…; IssueCount=0; Issues=nil; Fixes=nil`) copied in `identifier_check.go:132-160` (three times), `checks.go` `RunQueryRecipes`, `check_code_frontmatter.go:71-77`, and `suite.go` `finalizeCheckResult`; aggregation of `IssueCount`/`ErrorCount`/`OK` in `suite.go:160-168`, `validationrun/run.go:126-136`, and `index.go`; 19 production sites set the "legacy" `Skipped`.
- **Problem:** Each check must set `IssueCount` by hand. `normalizeCheckResult` trusts it, so a check that appends issues but forgets the count reports `OK=true`; all current checks happen to be consistent. Checks compute issue keys early only so fixes can reference them, which is why every check needs its own fail-closed test (`stable_issue_key_checks_test.go`).
- **Recommendation:** Change the runner signature to `func(ctx, in Inputs) (Findings, error)`, where `Findings{Issues, Fixes(by issue index), Notes, Buckets, NotApplicable string}`. The suite then derives `IssueCount = len(Issues)`, assigns keys once, binds fixes to keys, converts `error` to `Error`, and turns `NotApplicable` into the typed outcome. That deletes `Skipped` from `CheckResult` and the three skip-promotion mappers.
- **Effort:** M
- **Dependencies:** VAL-7.

### VAL-10 `Options`/`RunContext` are god structs carrying dead fields and shims; CLI options are copied through four structs
- **Severity:** P2
- **Confidence:** High
- **Location:** `types.go:286-320` (`Options` mixes scan, apply, private test hooks, and fragile-external scope); `types.go:326` (`RunContext.VaultMgr` is **never read**: `grep -rn '\.VaultMgr\b'` finds no reads, yet four adapter types exist to satisfy it: `validate/index.go:172 fixedVaultMgr`, `validationproduct/live.go:144 liveVaultManager`, `validationproduct/projection.go:366 projectionVaultManager`, and `cmd/ontology_inspect.go:102 fixedVaultDefinition` for this use); `cmd/validation_product.go:18-31,229-257` and `pkg/app/cli/validation_product.go:14-25,96-131` (the same seven flags copied through `validationProductOptions` → `ValidationProductRequest` → `validate.Options` → the apply-command builder); `cmd/validation_product_runner.go:117` (`ReplanCommand = strings.Replace(ApplyCommand, " --apply", "", 1)`, which corrupts a quoted selector containing ` --apply`).
- **Problem:** Readers cannot tell which options affect a read-only scan. `--scope-note/--scope-target/--scope-ref` are global flags on every validate command but only `fragile_external` (audit) reads them. Command strings are built in two places with two `shellQuote` copies (`validate/next_actions.go:244`, `pkg/app/cli/validation_product.go:134`).
- **Recommendation:** Delete `VaultMgr` and the four adapters. Split into `ScanOptions{Checks, MaxIssues, Links{SkipAnchors, SkipEmbeds, IncludeImages}, FragileScope}` and `ApplyOptions{Selection, Confirm, AllowHistorical, Refresher, CompletionArtifact}`. Move the test hooks into an unexported `applyHooks` passed only by tests. Build the replan command with the same builder (`Apply=false`) instead of string surgery. Keep one shell quoter in `pkg/app/cli`, and have `validate` return structured `{check, selector}` hints instead of command strings.
- **Effort:** M
- **Dependencies:** VAL-6; pairs with VAL-3.

### VAL-11 Oversized functions concentrate the riskiest logic
- **Severity:** P2
- **Confidence:** High (measured)
- **Location:** `apply.go:14` `ApplyFixPlan` (481 lines); `identifier_validation.go:17` `runAliasesWithRuntime` (270); `frozen_scope_drift.go:84` `RunFrozenScopeDriftWithRuntime` (269); `check_orphan_block_ids.go:70` `RunOrphanBlockIDs` (217); `identifier_inventory.go:65` (213); `check_fragile_external.go:57` (208); `identifier_block_ids.go:28` (168); `suite.go:61` `runSuiteOnce` (159).
- **Recommendation:** Split `ApplyFixPlan` by phase as part of VAL-3. Split the checks into load → index → classify → emit phases; VAL-2 removes most of the "load and index" code. `runAliasesWithRuntime` is the identifier mirror and required pass, which still carries the "Aliases" name; rename it and split it by finding family.
- **Effort:** M (spread across VAL-2 and VAL-3)
- **Dependencies:** VAL-2, VAL-3.

### VAL-12 Parallel issue, variant, and outcome DTOs plus small duplicated helpers
- **Severity:** P3
- **Confidence:** High
- **Location:**
  - Variant appears four ways: `ontology.ValidationIssue.VariantKey/VariantLabel` (`pkg/ontology/index.go:22-46`), `viewconfig.Issue.Variant string`, `validate.IssueVariant` (`types.go`), and `semdb.ValidationIssueVariant` (`pkg/anchors/sqlite/validation_snapshot_types.go:90`).
  - Outcome appears four ways: `validate.CheckApplicabilityResult`, `validationrun.ValidationCheckOutcome` (`result.go:22`, a field-for-field copy plus `RemediationCommand`, keyed by CLI name), `validate.DiagnosticCheckOutcome` (`diagnostic_snapshot.go:41`), and `semdb.ValidationCheckSnapshot`.
  - Helper duplicates: `getNotesListContext` (`validate/broken_links.go:277` ≡ `obsidian/health.go:506`), `firstNonEmpty` (`identifier_validation.go:332`) ≡ `firstNonEmptyString` (`buckets.go:126`), and `aliasMapFromNotes` (`alias_cache.go`) ≡ `obsidian.aliasesFromNotePaths`.
- **Recommendation:** Let ontology, viewconfig, and queryrecipe emit a shared tiny `diag.Finding{Code, Path, Line, Field, Message, Variant{Key, Label}}` from a leaf package, so `validate` stops writing adapters such as `ontologyValidationIssue`, `queryRecipeValidationIssue`, and the view-issue mapping. Make `ValidationCheckOutcome` embed `CheckApplicabilityResult` instead of copying it. Delete the helper duplicates.
- **Effort:** S (helpers), M (shared finding)
- **Dependencies:** None.

### VAL-13 `validationrun` is a thin, defensive layer; its split from `validationproduct` only partly earns its keep
- **Severity:** P3
- **Confidence:** Medium (judgment)
- **Location:** `pkg/app/validationrun` is 690 production lines importing only `pkg/validate` and `semdb`. `BuildValidationResult` (`result.go:43`, 128 lines) re-checks invariants its only callers already guarantee: duplicate selections, unknown checks, unexpected outcomes.
- **Problem:** The stated reason ("neutral below CLI and projection so neither depends upward") is real for `agentapi` and `pkg/app/cli`, which import `validationrun` without pulling in `indexing`. But duplicated logic still leaked into `validationproduct` and `validate/index.go` (VAL-5), so the neutral layer is not actually the single owner.
- **Recommendation:** Keep the package, but make it *the* owner of selection → applicability → result composition for all three producers (VAL-5). Trim the defensive re-validation to one `validateComposition` helper. If VAL-7 and VAL-9 land, much of `BuildValidationResult` becomes a straight mapping.
- **Effort:** S
- **Dependencies:** VAL-5.

### VAL-14 Subsystem note drift
- **Severity:** P3
- **Confidence:** High
- **Location:** `docs/reference/subsystems/validate.md`. It cites `pkg/validate/fixes.go`, which does not exist (lines 31, 172). It says the web server "also runs the suite" (line 48), but `pkg/app/web/validate.go` only reads persisted snapshots. It contains duplicated bullets (78≈82, 81≈83, 93≈123, 138≈140). It says legacy `Skipped` is "rejected at the product boundary" (line 67), but `validationrun.promoteSoftSkippedChecks` promotes it. It describes a single "Prerequisite evidence is domain-specific" rule that VAL-8 contradicts.
- **Recommendation:** Fix these in the first refactor PR and bump `last-verified`.
- **Effort:** S

## Findings — tests

Proportionality: `pkg/validate` has about 17.5k test lines. About 11k exercise the write engine (journal, recovery, namespace, Git), and those are mostly strong adversarial behavior tests. About 4.4k target checks. The ~23k total is proportional to the engine but **thin for the checks**. `broken_links_test.go`, `check_fragile_external_test.go`, and `check_code_frontmatter_test.go` have two tests each. `companion_docs` has no test in the package; `cmd/validate_test.go` covers clean and missing only. No test compares validation with the indexer. Message assertions are mostly targeted `Contains`, which is acceptable.

### VAL-T-1 Delete: registry tests that assert the static registry against itself
- **Verdict:** delete
- **Tests:**
  - `pkg/validate/registry_test.go:TestCheckRegistryDeclaresCompletePublicMetadata`: `init()` already panics on `validateCheckRegistry`, and the `NotEmpty` loop re-asserts the same fields.
  - `registry_test.go:TestRegistryRuntimeRequirementDrivesSharedOntologyAcquisition`: asserts constants; the behavior is pinned by `checks_test.go:TestRunSuiteOncePreparedUsesProvidedRuntimeFailure` and `TestRunSuiteOncePreparedNeverAcquiresReplacementRuntime`.
  - `registry_test.go:TestCheckDescriptorsReturnsIndependentSlices`: a defensive-copy unit test with no consumer that mutates.
  - Shrink `registry_test.go:TestCheckRegistryMatchesSharedIdentityCatalog` to its only non-tautological assertion, `len(catalog.Checks()) == len(checkRegistry)`. Its alias-equality loop is guaranteed by `checkRegistration` copying aliases from the catalog.
  - After VAL-7, cut `TestCheckRegistryRejectsIncompleteDuplicateAndAmbiguousDescriptors` (~95 lines) down to duplicate-id, ambiguous-alias, and all/audit cases.
- **Why:** These pass even if behavior breaks and fail only on intentional metadata edits.
- **Replacement:** None needed beyond the shrunk cases.

### VAL-T-2 Merge: per-check alias tests into one public-selector contract table
- **Verdict:** merge
- **Tests:** `checks_test.go:TestCanonicalCheckAcceptsQueryRecipeAliases`, `TestCanonicalCheckAcceptsViewAliases`, `TestCanonicalCheckAcceptsSkillOverlayAliases`, `TestCanonicalCheckAcceptsCompanionDocsAliases`, `registry_test.go:TestIdentifiersOwnsLegacyAliases`.
- **Why:** Aliases are a public "never remove" contract, so a lock is worth having, but five hand-written tests cover five of fourteen checks.
- **Replacement:** One `TestPublicSelectorsAreStable` table holding a frozen literal of every `(selector → canonical id)` pair for all 14 checks, plus `default`/`all`/`audit` reserved names. It fails if anyone removes or reassigns an alias.

### VAL-T-3 Rewrite: tests that exist only for the dead non-prepared path
- **Verdict:** rewrite (with VAL-6)
- **Tests:** `checks_test.go:TestRunCheckUsesOptionsMaxIssuesWhenRunContextLimitIsUnset` (exercises `RunCheck`); `buckets_test.go` (uses `RunCheck`); `fixes_test.go` (three `RunOntology` calls); `RunSuiteOnce` callers in `repair_session_test.go` (9), `repair_review_feedback_test.go` (5), `repair_postapply_red_test.go` (2), `checks_test.go` (2), `run_snapshot_test.go`, `repair_cleanup_recovery_test.go`, `link_history_test.go`; and `checks_test.go:TestRunSuiteOnceRejectsMissingNoteMetadataBeforeProjectionWork`.
- **Why:** They pin an entry point production never uses, and they keep `EnsureFreshRuntime` reachable.
- **Replacement:** A shared `preparedSuite(t, files, checks...)` helper that builds a runtime the way `frozen_scope_drift_test.go:58` does and calls `RunSuiteOncePrepared`. Keep the `MaxIssues` precedence assertion as a `finalizeCheckResult` table if that precedence is still a contract after VAL-10.

### VAL-T-4 Add: validation-indexer agreement and format coverage (the main gap)
- **Verdict:** add
- **Tests to add** (fixture-driven, one table):
  1. `TestBrokenLinksAgreeWithIndexedEdges`: for a fixture vault, every link the indexer resolves (`NoteSourceSnapshot.Links` / `ResolveProjectedLink`) must not appear in `broken_links`, and every `broken_note_link` must have no indexed edge. Seed cases: relative Markdown basename (`sub/a.md` → `[x](Foo.md)` where `Foo.md` is at the root), alias target, case-only mismatch, attachment, a `.md` file excluded by `.rhizome/ignore`, and a dot in the name. Record intentional differences in the table rather than leaving them implicit.
  2. `TestBrokenLinksHTMLNotes`: an HTML note with a broken `<a href>`, and a Markdown `[[effort#Deviations]]` pointing into an HTML note with `<h2>Deviations</h2>`. Both are expected to pass today for the wrong reason or fail (VAL-2).
  3. `TestBlockIDMigrationNeverRewritesCode` (regression for VAL-1): a referencing note containing the same `[[target#^old]]` once in prose, once in a fenced block, once in inline code, and once in frontmatter. Only the prose link may change, and a code-only referencing note gets no edit.
  4. `TestOrphanBlockIDsIgnoresCodeReferences`.
  5. `TestAgentJSONOmitsRepairFileContents` (regression for VAL-4a): render `RenderCI(JSON)` for a result with a write operation and assert no `content` or full-text `expected` payload.
- **Why:** The subsystem's core promise is that "links resolve the way Obsidian resolves them" consistently. Nothing currently checks that against the indexer, and three resolvers already diverge.

### VAL-T-5 Add: table-driven per-check matrix for thinly covered checks
- **Verdict:** add (and merge existing narrow tests into it)
- **Tests:** Extend `broken_links_test.go:TestRunBrokenLinks*` (2 tests), `check_fragile_external_test.go` (2), `check_code_frontmatter_test.go` (2), and add `check_companion_docs_test.go`.
- **Why:** The subsystem's own must-do ("zero findings, findings, soft-skip Notes, full issue production before truncation") is met only by `frozen_scope_drift_test.go`. `companion_docs` has untested security-relevant paths: `companion_doc_outside_vault` with `../` and absolute paths, `companion_doc_directory`, and `companion_doc_stat_failed`.
- **Replacement:** One `checkMatrix` table of `{check, files, wantCodes, wantNotes, maxIssues}` run through `preparedSuite`, with a shared assertion that `IssueCount == len(fullIssues)` when it exceeds `MaxIssues`. This replaces several bespoke fixtures.

### VAL-T-6 Rewrite: parallelize or share fixtures in the expensive Git and interruption sweeps
- **Verdict:** rewrite (no coverage loss)
- **Tests:**
  - `namespacegit/prepare_test.go:TestPreparationMatchesActualGitMoves` (12.25 s, 16 sequential subtests, each with its own fixture). Add `t.Parallel()` to every variant except `global-config`, which uses `t.Setenv`.
  - `namespace_interruptions_test.go:TestNamespaceEveryRequiredPublicationInterruptionRestoresBatch` (6.96 s, 12 subtests) and `TestNamespaceEveryReverseRollbackInterruptionResumesSafely` (3.14 s): parallel subtests over independent temp vaults.
  - `repair_rename_ordering_test.go:TestApplyFixPlanRenamePublicationOrderRecoversEveryMutation` (2.17 s, 14 subtests).
  - `identifierreconcile/provenance_git_test.go` (15 tests, 5.25 s) and `link_provenance_git_test.go` (3.24 s): build a template repository once per package (`TestMain` or `sync.Once`) and `cp -r` it per test instead of replaying `git init`/commits.
- **Why:** Of 61 `t.Parallel()` calls, all sit in cheap pure tests. The 47 s `pkg/validate` wall time and 18 s `namespacegit` time are dominated by serial Git and filesystem sweeps.
- **Expected:** Roughly halve package wall time. This estimate is unverified.

### VAL-T-7 Merge: near-identical TOCTOU tests
- **Verdict:** merge
- **Tests:** `repair_recovery_toctou_red_test.go:TestApplyFixPlanRejectsConcurrentSourceChangeBeforeFirstMutation`, `…ModeChangeBeforeFirstMutation`, `…DeletionBeforeFirstMutation`, and `…ParentSymlinkSwapBeforeFirstMutation`.
- **Why:** Each has the same setup and plan, differing only in the `BeforeInstall` mutation and the preserved-state assertion.
- **Replacement:** One table `{name, mutate func(path), assertPreserved func(t)}`. Keep the Windows mode special case as a row guard.

### VAL-T-8 Rewrite (organization): test files named after process history
- **Verdict:** rewrite (rename and regroup when VAL-3 moves the engine)
- **Tests:** The `*_red_test.go` files (`repair_adversarial_red`, `repair_engine_red`, `repair_final_batch_red`, `repair_journal_red`, `repair_mutation_progress_red`, `repair_planner_report_red`, `repair_postapply_red`, `repair_recovery_sequence_red`, `repair_recovery_toctou_red`), `repair_greptile_followup_test.go`, and `repair_foundation_test.go`, which mixes stable keys, fingerprints, transaction grouping, and eight `TestClassifyLifecycleEdit*` tests.
- **Why:** File names record when a test was written, not the contract it guards, so contributors cannot find the owning test or spot duplicates. The tests themselves are sound.
- **Replacement:** Group by contract: `journal_recovery_test.go`, `transaction_isolation_test.go`, `lifecycle_policy_test.go` (fold the eight `TestClassifyLifecycleEdit*` into one table), `plan_identity_test.go`, `postapply_lease_test.go`.

### VAL-T-9 Rewrite: full-sentence assertions on non-contract prose
- **Verdict:** rewrite
- **Tests:** `checks_test.go:224` (`require.Equal` on the full next-action message); `applicability_test.go:458` (full summary sentence); `index_test.go:122` (full error string); `diagnostic_recovery_test.go:39` (summary literal).
- **Why:** Message prose is guidance, not contract. Evidence codes, commands, and issue codes are the contract.
- **Replacement:** Assert `Evidence[].Code`, `Command`, and `Category`, plus at most a `Contains` on the actionable token (for example `rzm init --check`).

### VAL-T-10 Merge: tests on duplicated producers
- **Verdict:** merge (with VAL-5)
- **Tests:** `pkg/app/validationproduct/projection_test.go:TestLiveVaultFeatureFactsRequireCodeAnchorRoots` covers only the live copy. `cmd/validation_product_runner.go:validationVaultFeatureFacts` has no equivalent test.
- **Replacement:** One test on the single `Prepare` facts producer.

## Suggested refactor sequence

1. **Correctness quick wins (S, independent).**
   - VAL-1: span-based block-id rewrites with code filtering, plus VAL-T-4 items 3 and 4.
   - VAL-4a: `json:"-"` on repair payloads, plus VAL-T-4 item 5.
   - VAL-8: drop the aliased projection domains.
   - VAL-14: doc fixes.
2. **Delete dead weight (S–M).**
   - VAL-6: dead runners and the `EnsureFreshRuntime` branch, migrating tests per VAL-T-3.
   - VAL-10: delete `RunContext.VaultMgr` and its four adapters, delete `CheckAliases`, and replace replan string surgery.
   - VAL-12: helper duplicates.
   - VAL-T-1 and VAL-T-2 test cleanup.
3. **One orchestration (M).** VAL-5: a single `Prepare` plus `RunValidation` for CLI, live, and indexing, with one journal-blocked composer (VAL-13 trims `validationrun` around it, VAL-T-10).
4. **Check contract (M).**
   - VAL-7: `checkSpec` with `Needs`, deriving snapshot and runtime acquisition.
   - VAL-9: `(Findings, error)` runners, with the suite owning counts, keys, and not-applicable.
   - VAL-T-5 matrix on the new contract.
5. **Single parsed input and resolver (L).** VAL-2: write the VAL-T-4 agreement tests first, then switch checks to snapshot provider facts and one resolver shared with `notemeta`. VAL-4b (lazy plan materialization) lands here because the per-edit alias rebuild disappears.
6. **Extract the write engine (L).** VAL-3, with VAL-11's `ApplyFixPlan` split. Reorganize tests per VAL-T-8, merge per VAL-T-7, and parallelize per VAL-T-6. The parallelization can also go earlier, since it is independent.

## Slow-test table (measured)

One run: `go test -count=1 -json ./pkg/validate/... ./pkg/app/validationrun/... ./pkg/app/validationproduct/...` on a shared machine. All tests passed; 627 top-level tests and 424 subtests, 4 skipped.

| Package | Wall (s) |
| --- | --- |
| pkg/validate | 47.0 |
| pkg/validate/namespacegit | 18.3 |
| pkg/validate/identifierreconcile | 10.7 |
| pkg/app/validationproduct | 3.8 |
| pkg/app/validationrun | 0.3 |
| pkg/validate/namespaceadmission | 0.1 |

| Elapsed (s) | Test | Note |
| --- | --- | --- |
| 12.25 | namespacegit `TestPreparationMatchesActualGitMoves` | 16 serial real-Git variants → VAL-T-6 |
| 6.96 | validate `TestNamespaceEveryRequiredPublicationInterruptionRestoresBatch` | 12 serial interruption points |
| 3.14 | validate `TestNamespaceEveryReverseRollbackInterruptionResumesSafely` | 6 serial subtests |
| 2.74 | namespacegit `TestMovedSourceStateMatchesActualGit` | real Git |
| 2.17 | validate `TestApplyFixPlanRenamePublicationOrderRecoversEveryMutation` | 14 serial subtests |
| 1.34 | validate `TestNamespaceTombstoneRetainsRecoveredOutcome` | |
| 1.26 | validationproduct `TestRunLiveRecipeDiscoveryMatchesSupportedSources` | |
| 0.98 | namespacegit `TestMalformedAdmissionAndScratchAreHardFailures` | |
| 0.98 | validate `TestOntologyEditSessionRepairPlanRecoversCommittedJournal` | |
| 0.91 | validate `TestNamespaceGitFailureAndCrashAfterIndexRestoreOriginalStaging` | |
| 0.80 | identifierreconcile `TestAmbiguousLinkGitProvenanceUsesSourceBranchAncestryNotAuthorDates` | per-test Git history replay |
| 0.70 | identifierreconcile `TestGitProvenanceFallsBackForUntrackedShallowAndNoRepository` | |

By file: `namespacegit/prepare_test.go` 12.6 s; `namespace_interruptions_test.go` 10.1 s; `identifierreconcile/provenance_git_test.go` 5.3 s across 15 tests; `repair_rename_ordering_test.go` 3.6 s; `namespace_git_recovery_test.go` 3.3 s; `identifierreconcile/link_provenance_git_test.go` 3.2 s. All check-focused test files run in under 1.1 s each.
