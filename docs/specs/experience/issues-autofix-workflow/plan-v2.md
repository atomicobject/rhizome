# Issues Auto-Fix Workflow v2 — Full Validate Surface

## Goal

Expand the issues page to show ALL `rzm validate` issues (broken-links, ontology, aliases, code-frontmatter, code-anchors) — not just ontology assessment issues. Support the full fix infrastructure with safe/confirm/agent safety levels. Broken links should suggest candidate notes to relink.

## Why v2

v1 only showed ontology assessment issues stored in per-note assessments. Most vaults have far more broken-link issues than ontology issues. The user runs `rzm validate` and sees 78 issues, but the web UI shows 0 because the web server only queries per-note ontology assessments.

## Technical context

- **Validation checks live in `cmd/`**: `cmd/validate_helpers.go` (check runners) and `cmd/validate_fixes.go` (fix types + application logic). These are not importable from `pkg/app/web/`.
- **Web server has**: `cfg.VaultDef`, `cfg.VaultPath`, `runtime.Intel()` (store), `obsidian.Note{}` (note reader), `noteCache` — everything the validate checks need.
- **Fix infrastructure**: three safety levels (`safe`, `needs_confirmation`, `agent_required`), five fix edit kinds (`append_alias`, `set_frontmatter`, `rewrite_broken_link_group`, `add_section_scaffold`), plus the `pkg/ontology/fixes.go` `SuggestFixes` we added in v1.
- **Broken-link fix**: exact normalized filename match → if 1 candidate, suggests rewrite. Safety: `needs_confirmation`.
- **`ontology.EnsureFreshRuntime`**: creates a fresh store + schema + runs `EnsureIndexed`. The web server already has an indexed store — it should NOT re-index.

## Architecture decisions

### Extract validation logic to `pkg/validate/`

Move the check types, issue types, fix types, check runners, and fix builders from `cmd/` to a new `pkg/validate/` package. Keep `cmd/validate.go` and `cmd/validate_helpers.go` as thin wrappers that call into `pkg/validate/`.

**What moves to `pkg/validate/`:**
- Types: `ValidationIssue`, `CheckResult`, `FixAction`, `FixEdit`, `FixPlan`, `FixSafety`, `Result`, `Options`, `RunContext`
- Check runners: `RunBrokenLinks`, `RunOntology`, `RunAliases`, `RunCodeFrontmatter`, `RunCodeAnchors`
- Fix builders: `BuildBrokenLinkFixes`, `BuildOntologyFixes`, `BuildFixPlan`
- Fix application: `ApplyFixPlan`, `ApplyFixAction` (and per-edit-kind appliers)
- Helpers: `FindBrokenLinkCandidates`, `IdentifierCandidateFromPath`, `AddSectionScaffold`, etc.

**What stays in `cmd/`:**
- CLI flag parsing, Cobra command wiring
- `vaultDefOrDefault()` (CLI-specific vault resolution)
- `fixedVaultDefinition` (already in `cmd/ontology_inspect.go`)
- Interactive confirmation prompt (`promptValidationFix`)

**Key type renames** (exported):
- `validationIssue` → `validate.Issue`
- `validationCheckResult` → `validate.CheckResult`
- `validationFixAction` → `validate.FixAction`
- `validationFixEdit` → `validate.FixEdit`
- `validationFixPlan` → `validate.FixPlan`
- `validationFixSafety` → `validate.FixSafety`
- `validationResult` → `validate.Result`
- `validationOptions` → `validate.Options`
- `validationRunContext` → `validate.RunContext`

### New web endpoint: `GET /api/validate`

Runs the full validation suite using the web server's existing runtime objects:
- `VaultDef` from `s.cfg.VaultDef`
- `VaultPath` from `s.cfg.VaultPath`
- `NoteReader` as `&obsidian.Note{}`
- Store from `s.runtime.Intel()`

For the ontology check, the web server should use `ontology.EnsureFreshRuntimeWithStore` (passes the existing store) rather than `EnsureFreshRuntime` (which opens a new store + re-indexes).

**Response shape**: the full `validate.Result` serialized as JSON. The frontend can consume the `checks[].issues` and `fixPlan.actions` directly.

### Frontend: consume `/api/validate` instead of `__issues__` type detail

The issues home currently fetches `GET /api/ontology/type?name=__issues__`. Switch to `GET /api/validate`.

The existing `OntologyIssuesHome` already groups by issue code. The only change: map from the validate result shape (`checks[].issues[]` flattened) instead of from `typeDetail.notes[].issues[]`.

The fix actions from the validate response already carry safety levels and edit details. The frontend should:
- Show all issues from all checks, grouped by issue code
- For fixable items (`safety != "agent_required"`), show checkboxes
- Distinguish safe fixes (auto-apply) from confirmation-needed fixes (show the question)
- "Fix selected" → POST `/api/validate/fix` with selected action IDs

### New web endpoint: `POST /api/validate/fix`

Accepts a list of fix action IDs. Applies them using the existing `validate.ApplyFixAction` logic. Returns the updated state.

For fixes that modify note files on disk (all current fix types write directly), the web server needs to be careful about file locking. The current fix appliers (`writeFixedNote`) write directly and bump mtime. This is the same as the CLI.

**Alternative considered**: convert validate fixes to edit session ops. Rejected because:
1. Fix edit kinds (`rewrite_broken_link_group`, `append_alias`, `add_section_scaffold`) don't map cleanly to existing `OntologyEditOp` kinds
2. Edit sessions are designed for ontology field edits, not arbitrary file rewrites
3. The validate fixes already have a working application pipeline

### Sidebar issues count

Currently the sidebar shows/hides the "Issues" rail based on `summary.issueNotes` from `/api/ontology/summary`. This should switch to a validate-aware count. Two options:

1. **Quick**: have the summary endpoint also run a fast issue count from the validate pipeline
2. **Better**: the frontend fetches `/api/validate` on initial load (or in the background) and uses `result.issueCount` for the badge

Option 2 is better — it avoids slowing down the summary endpoint. The validate results can be fetched lazily.

## Implementation phases

### Phase 1: Extract `pkg/validate/`

- [ ] T001 Create `pkg/validate/types.go` with exported types: `Issue`, `CheckResult`, `FixAction`, `FixEdit`, `FixPlan`, `FixExecution`, `FixSafety`, `Result`, `Options`, `RunContext`
- [ ] T002 Create `pkg/validate/checks.go` with check runners: `RunSuite`, `RunSuiteOnce`, `RunCheck`, `RunBrokenLinks`, `RunOntology`, `RunCodeFrontmatter`, `RunCodeAnchors`, `RunAliases`
- [ ] T003 Create `pkg/validate/fixes.go` with fix builders: `BuildBrokenLinkFixes`, `FindBrokenLinkCandidates`, `BuildOntologyFixes`, `BuildFixPlan`
- [ ] T004 Create `pkg/validate/apply.go` with fix appliers: `ApplyFixPlan`, `ApplyFixAction`, and per-edit-kind helpers
- [ ] T005 Update `cmd/validate_helpers.go` and `cmd/validate_fixes.go` to delegate to `pkg/validate/`
- [ ] T006 Verify `go test ./cmd/... ./pkg/validate/...` still passes

### Phase 2: Web API endpoint

- [ ] T007 Add `GET /api/validate` handler in `pkg/app/web/server.go` → `pkg/app/web/validate.go`
- [ ] T008 Implement the handler: build `validate.RunContext` from web server state, call `validate.RunSuiteOnce`, return `validate.Result` as JSON
- [ ] T009 Add `POST /api/validate/fix` handler that accepts `{ actionIds: string[] }`, calls `validate.ApplyFixAction` for each, returns updated result
- [ ] T010 Write integration test for `/api/validate` endpoint

### Phase 3: Frontend integration

- [ ] T011 Add `getValidateResult()` client function in `web/src/api/client.ts`
- [ ] T012 Add `postValidateFix()` client function
- [ ] T013 Add validate result types to `web/src/api/types.ts`
- [ ] T014 Update `OntologyIssuesHome.tsx` to consume validate result instead of type detail
- [ ] T015 Update `OntologyWorkspace.tsx`: fetch `/api/validate` for the issues home, show sidebar badge from validate issue count
- [ ] T016 Add safety-level indicators in the issue UI (safe=auto, confirm=ask, agent=manual)
- [ ] T017 Wire "Fix selected" to `POST /api/validate/fix` with selected action IDs, then refresh

### Phase 4: Polish

- [ ] T018 Show broken-link candidate notes in the issue detail (so user can see what the relink target would be)
- [ ] T019 Distinguish check types visually (broken-links vs ontology vs aliases)
- [ ] T020 Show fix confirmation questions for `needs_confirmation` fixes before applying
- [ ] T021 Handle fix application errors gracefully in the UI

## Key design decisions

1. **Extract to `pkg/validate/`** rather than duplicating logic. Both CLI and web server import from the same package.

2. **Validate runs on demand** (when the issues page loads), not cached in the summary. This avoids slowing down the workspace init.

3. **Fixes apply directly to disk** via the existing validate fix appliers, not through edit sessions. The fix types (`rewrite_broken_link_group`, `append_alias`) don't fit the edit session model.

4. **Sidebar badge**: fetched from `/api/validate` results, not from the summary endpoint.

## Validation plan

- `go test ./pkg/validate/...`
- `go test ./cmd/...`
- `go test ./pkg/app/web/...`
- `go vet ./...`
- `go test ./...`
- Web lint: `biome check src/`
- Web typecheck: `tsc --noEmit`
- Manual: open workspace, see issues from all check types, select fixes, apply, verify files changed
