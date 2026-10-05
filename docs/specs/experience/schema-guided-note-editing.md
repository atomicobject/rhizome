---
type: ExperienceSpec
summary: "Defines a responsive, schema-guided Notes editing experience with typed controls, durable local drafts, clear save and conflict recovery, safe source editing, and accessible visual feedback."
id: SPEC-0099
spec-status: active
last-updated: 2026-09-11
aliases:
  - SPEC-0099
---

# Schema-guided note editing

## Summary

The Notes workspace lets people stage semantic changes, but its editing controls do not currently reflect the note schema, narrative typing sends a full replay cycle for every keystroke, drafts can disappear during navigation, and conflicts can be hidden by the transition out of Edit mode. This spec defines the editing experience inside the existing tabbed Notes shell.

The experience uses the public node workspace as its capability source. Each editable field receives a control suited to its scalar, list, enum, relation, identifier, or narrative contract. Typing remains local and immediate, while compact semantic operations are staged after a short idle period or an explicit completion action. The same local draft survives navigation, reload, server restart, and conflict recovery until the user commits or discards it.

The visual model is one compact editing bar, one coherent review surface, and one set of status language. Related technical guarantees remain governed by [[ontology-edit-replay-conflict-contract|SPEC-0046]], [[node-workspace-capability-pipeline|SPEC-0019]], and [[frontend-data-lifecycle|SPEC-0074]]. The findings that motivated this contract are recorded in [[note-editing-subsystem-review-2026-09-11]].

## Goals

- make the correct editor obvious from the field's authored schema
- keep typing immediate even on long notes and slow machines
- preserve drafts across tabs, routes, reloads, and server session recovery
- make save, discard, validation, rebase, and conflict states understandable and recoverable
- provide useful editing for empty notes and a safe Source-mode escape hatch
- match the compact Notes shell with clear hierarchy, accessible feedback, and consistent tokens

## Non-Goals

- a visual ontology-schema designer
- collaborative presence, cursors, or live multi-user text merging
- rich-text Markdown authoring or a replacement for a full desktop Markdown editor
- collection-builder interactions beyond reliable add, delete, and reorder operations already supported by ontology edit sessions
- phone-sized or touch-first layout work
- raw editing of non-Markdown providers that do not advertise an authored-source write capability

## User Stories

### US1 - Edit each field with a control that matches its schema

- id:: ^SPEC-0099-US1
- summary:: Edit a field with a control that understands its type, cardinality, allowed values, relation target, and source behavior.
- status:: ready

#### Acceptance Criteria

- String, long narrative, Date, DateTime, Boolean, Int, Float, enum, list, and relation fields use one shared field-editor registry in Notes and configured tables. ^SPEC-0099-US1-AC1
- DateTime editing shows the timezone or offset explicitly and preserves authored seconds and fractional precision unless the user changes them. ^SPEC-0099-US1-AC2
- Optional Boolean and scalar fields distinguish unset from false, zero, and empty string; list controls preserve all values and support add, remove, and reorder. ^SPEC-0099-US1-AC3
- Relation controls search only compatible target types and store canonical links; identifiers expose format and uniqueness guidance; computed or inherited fields explain why they are read-only. ^SPEC-0099-US1-AC4
- Incomplete input remains local while the user types, and newly invalid changed values cannot be staged or committed. Existing invalid values elsewhere in the note do not block repair. ^SPEC-0099-US1-AC5

### US2 - Type and navigate without losing work or waiting on the server

- id:: ^SPEC-0099-US2
- summary:: Type into fields and narrative blocks immediately, move around the application, and return to the same durable draft.
- status:: ready

#### Acceptance Criteria

- Keystrokes update route-independent local draft state without issuing one stage, replay, persistence, and workspace-refresh cycle per character. ^SPEC-0099-US2-AC1
- Repeated edits to the same target coalesce into one compact semantic operation after an idle interval, blur, Enter, or explicit save; an acknowledged older response cannot replace newer local text. ^SPEC-0099-US2-AC2
- Drafts survive note-tab changes, route changes, reload, and server edit-session loss, and remain associated with canonical file and node identity. ^SPEC-0099-US2-AC3
- Escape restores the value present when editing began without staging it, and Enter completes a field exactly once. IME composition does not trigger premature staging. ^SPEC-0099-US2-AC4
- A long narrative edit has request count and payload size proportional to completed edit actions, not character count or accumulated operation history. ^SPEC-0099-US2-AC5

### US3 - Save with clear progress and recover from conflicts in place

- id:: ^SPEC-0099-US3
- summary:: Understand what Save is doing and resolve a conflict without losing the draft or leaving the editing context.
- status:: ready

#### Acceptance Criteria

- The editing bar distinguishes Draft, Staging, Ready to save, Saving, Saved, Rebased, Conflict, and Error using concise text and accessible live announcements. ^SPEC-0099-US3-AC1
- Save exits Edit mode only after a successful commit. A conflict or error leaves the draft, operation list, focused field, and review actions visible. ^SPEC-0099-US3-AC2
- Unrelated disk drift rebases automatically. A changed targeted field presents Keep mine, Keep current, and Review choices with both values and the affected note and field. ^SPEC-0099-US3-AC3
- A successful no-op is reported as saved or already current, never as a conflict. Retrying a completed action does not duplicate a write. ^SPEC-0099-US3-AC4
- Undo and redo are available for local draft changes, and documented shortcuts do not collide with browser or text-input behavior. ^SPEC-0099-US3-AC5

### US4 - Review and discard changes at the scope I intend

- id:: ^SPEC-0099-US4
- summary:: Review all pending changes and discard one operation, one file, or the whole session with predictable results.
- status:: ready

#### Acceptance Criteria

- One compact action bar owns Edit/Done, Review, Save, and Discard language; the review surface uses the same actions and state labels rather than a second command vocabulary. ^SPEC-0099-US4-AC1
- Review groups semantic changes by canonical file and node while preserving readable paths and fragments. ^SPEC-0099-US4-AC2
- Discarding one operation or one file removes every matching canonical operation, including fragment-targeted operations, and immediately updates dirty markers and previews. ^SPEC-0099-US4-AC3
- Leaving or closing a note with pending local or staged work gives one clear keep, discard, or cancel decision without losing work that is still being staged. ^SPEC-0099-US4-AC4

### US5 - Edit empty notes and use Source mode safely

- id:: ^SPEC-0099-US5
- summary:: Add content to an empty note and use Source mode when structured controls cannot express the intended Markdown change.
- status:: ready

#### Acceptance Criteria

- An empty editable Markdown body offers an Add content action that creates a narrative draft inside the current edit session. ^SPEC-0099-US5-AC1
- Source mode can stage one whole-file operation when the provider advertises authored-source editing, with an explicit broad-change label and full-file diff before Save. ^SPEC-0099-US5-AC2
- Source commits require the original full-file fingerprint. Changes to headings, block IDs, or links that have external graph impact are rejected with guidance to use the corresponding graph-safe operation. ^SPEC-0099-US5-AC3
- Invalid Markdown or frontmatter is kept as a local source draft with actionable diagnostics and cannot silently replace the file. ^SPEC-0099-US5-AC4

### US6 - Work in a polished, accessible editing layout

- id:: ^SPEC-0099-US6
- summary:: Edit in a calm layout with readable properties, consistent tokens, keyboard access, and motion and contrast that respect user settings.
- status:: ready

#### Acceptance Criteria

- Properties use aligned labels and controls with adequate width for values; identity information appears once, and the Info rail does not repeat the full editable property form. ^SPEC-0099-US6-AC1
- Controls, focus rings, status, errors, and saved feedback meet WCAG AA contrast, use design tokens, and remain understandable without color alone. ^SPEC-0099-US6-AC2
- Animated editing indicators and transitions stop under `prefers-reduced-motion`, and enum styling derives from shared ontology or design tokens rather than field-local hex values. ^SPEC-0099-US6-AC3
- Every editor has a programmatic label, validation is associated with its control, focus remains predictable after stage/save/conflict actions, and all editing actions are keyboard reachable. ^SPEC-0099-US6-AC4
- The experience remains usable with both rails open at 1280px and does not introduce horizontal page overflow. ^SPEC-0099-US6-AC5

## Requirements

- MUST derive field controls from authoritative per-owner workspace capability metadata, never from validation findings or inferred current values.
- MUST keep local draft identity canonical and independent of the currently mounted route component.
- MUST send list values as ordered lists and preserve an explicit unset state for optional fields.
- MUST keep all structured and source edits inside the ontology edit-session write path.
- MUST expose server validation and typed conflicts in the same review surface used before Save.
- MUST preserve user input through retryable network, storage, replay, and commit failures.
- MUST not refresh the full node workspace for every character typed.
- MUST use the established Notes shell tokens and interaction density.
- SHOULD keep common field controls and edit-state components below about 500 lines and split orchestration from rendering.
- SHOULD measure narrative stage request count, transferred bytes, and response time against the same long-note workload before and after implementation.

## Open Questions

None. The control mapping, conflict policy, local validation boundary, source escape hatch, and persistence model are part of this contract.
