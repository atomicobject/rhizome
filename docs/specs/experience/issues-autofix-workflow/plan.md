# Issues Auto-Fix Workflow — Implementation Plan

## Goal

Redesign the ontology workspace issues home to group by issue code, support batch selection with tri-state checkboxes, and auto-fix selected issues by creating an edit session with computed fix ops — then navigate to the Modified home for review.

## Technical context

- **Frontend**: React (TSX), no framework — `web/src/components/`
- **Backend**: Go, Cobra CLI, `pkg/ontology/` for assessment/validation, `pkg/app/web/` for HTTP handlers
- **Edit sessions**: `useOntologyEditSession` hook → `createOntologyEditSession(ops)` → backend replays ops into `ontology.EditSession` → preview/commit
- **Op kinds**: `setField`, `setLinkField`, `setNarrative`, `addEmbeddedNode`, `deleteNode`, `reorderCollection`
- **Issue data**: `ValidationIssue` carries `Code`, `NotePath`, `TypeName`, `FieldName`, `LinkKind`, `LinkTarget`, `Message`

## Gap summary

| Requirement | Status |
|---|---|
| Issues grouped by issue code | **missing** — currently groups by `resolvedType` |
| Expandable note list per issue group | **missing** — shows flat list per type group |
| Tri-state checkbox per issue group | **missing** — no selection UI exists |
| Individual note checkboxes | **missing** |
| Fix button → edit session → Modified home | **missing** — no fix capability at all |
| Backend fix suggestion computation | **missing** — `collectAssessmentIssues` only flattens, doesn't suggest ops |
| `inverse_mismatch` auto-fix | **missing** |
| `declared_type_mismatch` auto-fix | **missing** |

## Architecture decisions

### Where fix suggestions live

Fix computation goes in `pkg/ontology/fixes.go` — a new file alongside `build_assessment.go`. It takes `NoteAssessment` + `Schema` and returns `[]FixSuggestion`. Each suggestion is tagged with the issue code it resolves and carries the `OntologyEditOp`(s) to apply.

**Why here and not in `pkg/app/web/`**: the fix logic needs schema context (field types, inverse declarations, candidate types) which lives in the ontology package. The web layer maps suggestions to the API response.

### API shape

Extend the existing `OntologyNoteIssueItem` with fix data rather than a new endpoint. This keeps the issues home a single fetch and avoids a second round-trip:

```go
type OntologyNoteIssueItem struct {
    Code    string              `json:"code,omitempty"`
    Field   string              `json:"field,omitempty"`
    Message string              `json:"message"`
    Fixable bool                `json:"fixable,omitempty"`
    FixOps  []OntologyEditOp    `json:"fixOps,omitempty"`
}
```

The `fixOps` carry fully-formed edit ops. The frontend doesn't need to know how to compute the fix — it just passes the ops to `stageOps()`.

**Rejected alternative**: separate `/api/ontology/issues/fixes` endpoint. Adds a round-trip, complicates the frontend, and the fix data is cheap to compute inline.

### Fix computation for initial issue codes

**`inverse_mismatch`**: The issue message is `"field X expects inverse Y on Z"`. The `ValidationIssue` carries `NotePath` (source), `FieldName` (source field), and the message encodes the target note path and inverse field name. To make fix computation clean, we'll add two fields to `ValidationIssue`:

```go
// FixTarget is the note path that should be edited to resolve this issue.
// For inverse_mismatch, this is the target note missing the inverse link.
FixTarget     string `json:"fixTarget,omitempty"`
// FixFieldName is the field on the fix target that should receive the fix.
// For inverse_mismatch, this is the inverse relation field name.
FixFieldName  string `json:"fixFieldName,omitempty"`
```

These are populated at validation time (both `index.go:validateInverses` and `sync.go:validateInversesIncremental`) from data already in scope (`edge.DstPath`, `field.Inverse`).

The fix op is a `setLinkField` on the *target* note that adds the source note as a link value to the inverse field. The fix must read the target's current link values and append, not replace.

**`declared_type_mismatch`**: The assessment carries `CandidateTypes` — the types that selectors actually match. The fix is a `setField` on the note's `type` frontmatter key, setting it to `candidates[0]` (single candidate) or leaving it unfixable (multiple candidates). Actually: when `declared_type_mismatch` fires, the note declares a type but selectors match *different* types. The cleanest fix is to remove the declared type and let the resolver pick from candidates. This is a `setField` with `field: "type"`, `value: ""` (remove).

But removal may be surprising. A better default: if `len(CandidateTypes) == 1`, set `type` to that candidate. If `len(CandidateTypes) > 1`, mark as not fixable (needs human choice). If `len(CandidateTypes) == 0`, also not fixable.

### Frontend grouping

The issues home switches from grouping by `resolvedType` to grouping by `code`. Each group shows:
- Issue label (human-readable, derived from code)
- Count badge
- Fixable indicator
- Tri-state group checkbox (only if group has fixable items)
- Expandable note list with individual checkboxes

### Tri-state checkbox logic

State per group:
- `"none"` — no items selected
- `"some"` — some items selected (renders as indeterminate)
- `"all"` — all fixable items selected

Clicking the group checkbox cycles: none → all → none. When items are individually toggled, the group recomputes. State is local to the component (not persisted).

### Navigation after fix

Hitting "Fix selected":
1. Collect all selected `fixOps` across groups
2. Call `editSession.stageOps(allOps)` — this creates a session if needed
3. Call `editSession.startEditing()` if not already editing
4. Call `selectType(PSEUDO_TYPE_MODIFIED)` to navigate to the Modified home

The Modified home already renders the session diff. The user reviews, then commits or discards.

## Implementation phases

### Phase 1: Backend — fix suggestion infrastructure

Add the fix data fields and computation without changing the frontend yet.

- [x] T001 Add `FixTarget` and `FixFieldName` fields to `ValidationIssue` in `pkg/ontology/index.go`
- [x] T002 Populate `FixTarget`/`FixFieldName` for `inverse_mismatch` in both `index.go:validateInverses` and `sync.go:validateInversesIncremental`
- [x] T003 Create `pkg/ontology/fixes.go` with `SuggestFixes(assessment *NoteAssessment, schema *Schema) []FixSuggestion` and a `FixSuggestion` type
- [x] T004 Implement `inverse_mismatch` fix in `fixes.go`: generate `setLinkField` op on target note, appending source to inverse field's current values
- [x] T005 Implement `declared_type_mismatch` fix in `fixes.go`: if single candidate, generate `setField` op for `type` frontmatter
- [x] T006 Add `Fixable` and `FixOps` fields to `OntologyNoteIssueItem` in `pkg/app/web/types.go`
- [x] T007 Update `collectAssessmentIssues` in `pkg/app/web/ontology.go` to call `SuggestFixes` and populate the new fields
- [x] T008 Write tests for `SuggestFixes` covering both issue types, edge cases (multi-candidate, missing schema), in `pkg/ontology/fixes_test.go`

### Phase 2: Frontend — redesigned issues home

- [x] T009 Add `fixable` and `fixOps` to `OntologyNoteIssueItem` in `web/src/api/types.ts` (regenerated from OpenAPI)
- [x] T010 Rewrite `OntologyIssuesHome.tsx` to group by issue `code` instead of `resolvedType`
- [x] T011 Add issue code → human label mapping (e.g., `inverse_mismatch` → "Missing inverse link")
- [x] T012 Implement tri-state checkbox component or inline logic for group selection
- [x] T013 Add individual note-level checkboxes for fixable items
- [x] T014 Add "Fix selected" button with count badge
- [x] T015 Wire "Fix selected" to `stageOps` + `startEditing` + `selectType(PSEUDO_TYPE_MODIFIED)` via `handleApplyFixes` in OntologyWorkspace
- [x] T016 Style the new layout (expand/collapse, checkbox alignment, fix button placement)

### Phase 3: Integration and edge cases

- [x] T017 Handle the case where an edit session already exists (`stageOps` already creates or stages onto existing)
- [x] T018 Handle `inverse_mismatch` fix when the target note's current link values need to be fetched (computed server-side in `SuggestFixes` via `currentLinkTargetPaths` from neighbor assessments)
- [x] T019 Refresh the issues list after commit (existing `refreshSidebarData` handles this)
- [x] T020 Pass edit session props through via `onApplyFixes` callback from workspace to issues home

## Key design decisions to confirm

1. **Grouping**: group by issue code, not by note type. Each issue code gets one expandable group. Notes that have multiple issues appear in multiple groups.

2. **Fix ops inline in the issue response**: no separate endpoint. The fix ops ride along with the issue data.

3. **`inverse_mismatch` fix target**: the fix edits the *target* note (the one missing the inverse link), not the source. This is the note identified by `edge.DstPath` / `FixTarget`.

4. **`declared_type_mismatch` fix**: only fixable when exactly one candidate type exists. Sets frontmatter `type` to that candidate.

5. **T018 — fetching current link values for `setLinkField`**: `setLinkField` replaces the full value list for a field. The fix must include existing values + the appended one. This data is available from the assessment's `RelationAssessment.Targets` on the *target* note, or from a workspace fetch. The cleanest path is to compute this in the backend `SuggestFixes` where we have the full assessment graph.

## Validation plan

- `go test ./pkg/ontology/... -run TestSuggestFixes`
- `go vet ./...`
- `go test ./...`
- Manual: open the workspace, confirm issues page groups by code, select some, hit Fix, land on Modified home with correct ops
