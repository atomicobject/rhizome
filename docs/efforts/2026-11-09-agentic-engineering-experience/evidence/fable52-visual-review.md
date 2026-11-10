⚠️ DEGRADED: single-context (the review brief forbids subagents, so the design review and the detector pass ran in one context, design review first)

# Agentic Engineering Delivery experience: visual design review

Drew's goal is one coordinated cleanup of the complete Agentic Engineering starter, fed by two independent Fable 5.2 reviews. This report is the visual design half. It is a critique and a bounded cleanup plan for the installed Delivery experience. It changes nothing in the repository.

## Run facts

- **Executed on.** Claude Code harness inside T3 Code, Anthropic provider, model `claude-fable-5-2`. No other model and no subagent ran.
- **Head.** `aa830a2b44544a3374ea33909973280df31da8ec` on `feat/agentic-engineering-experience`. `git status --porcelain` was empty before and after the review. Base `origin/main` `019a59ea128b900b57dfbcf565481f10ea4e9553` was not diffed, because this review reads the shipped surface, not the change set.
- **Writes.** This file only. No repository edit, build, gate, push, PR action, install, server, or vault note.
- **Verdict.** The experience is sound in structure, honest in copy, and stable in both themes. It is not yet at house finish. Eight bounded fixes close the gap, and most are a few CSS lines or a swap to a kit primitive that already exists. No redesign is warranted.

## Method

1. Read `poteto-mode/SKILL.md`, `.agents/skills/impeccable/SKILL.md`, `reference/critique.md`, `reference/operate.md`, `PRODUCT.md`, and `DESIGN.md` in full (all 489 lines, three chunks). `impeccable context` was not rerun, as instructed.
2. Read SPEC-0137 (`docs/specs/experience/agentic-engineering-delivery-experience.md`), `evidence/ui-proof.md`, `evidence/final-capture-review.md`, and `evidence/capture-manifest.json` (55 entries, 31 committed).
3. Opened all 31 committed captures under `evidence/screenshots/` one at a time at original resolution (1440x1000, 1280x720, 390x900). No contact sheet.
4. Opened 3 of the 24 uncommitted states from `/private/tmp/ae-experience-evidence/accepted/` to check stage layouts the committed set lacks: `stage-history-light.png`, `stage-review-light.png`, `selection-incomplete-dark.png`. The SHA-256 prefixes of the 11 light uncommitted files I hashed match the manifest.
5. Traced each visual observation to source in `pkg/app/cli/init/templates/starters/agentic-engineering/module/views/delivery/` (`delivery.css`, `presentation.tsx`, `home.tsx`, `stage.tsx`, `spec.tsx`, `effort.tsx`, `relationships.tsx`, `evidence.tsx`, `panel.tsx`, `data.tsx`) and to the kit primitives they call (`web/kit/components.css`, `layout.tsx`, `record-page.tsx`, `recordChip.tsx`, `statusMark.tsx`, `web/src/components/StatusMark.tsx`, `web/src/tokens.css`).
6. Compared against the sibling Knowledge experience (`starters/ao-kb/module/views/knowledge/panel.css`, `concept.tsx`, `assertions.tsx`) for house precedent.
7. After the design review was complete, ran the read-only detector once: `impeccable detect --json` on the delivery directory. It returned `[]` with exit 0, and the tree stayed clean. Every finding below therefore comes from the review, not the detector.

Positions such as "y≈600" are pixel rows read from the named capture. They are approximate.

## Limits

- No browser was run. Hover, keyboard focus order, focus return, clipboard, disclosure toggling, panel resize, minimize, Back and Forward, and 200 percent zoom were not exercised. Any claim about them below is marked as read from code.
- Font sizes and weights come from CSS and the cascade, checked against how the captures look. They were not measured with computed styles.
- One contrast ratio was computed by hand from `DESIGN.md` token values (VD-6). No formal contrast audit was done. Nothing in the captures looked below AA for text.
- 21 of the 24 uncommitted states were not opened, including `narrow-768`, `html-plan`, `clipboard-error`, `empty-planned`, and five stage pages. Narrow home and narrow stage layouts have no capture at all, so the prompt aside at 390px is unreviewed.
- Chrome themes other than Graphite were not captured, so panel findings are shown on Graphite only.
- Host chrome, the right pane, and authored HTML previews are outside this review. The dim red "29 issues" count in the light top bar is host chrome and is not a finding here.
- The static comps under `site2/comps` were not needed and were not opened.

## What is already good

Do not spend cleanup effort on these.

- **The workflow map.** Shape, Build, and Settle read as activities, not a pipeline. Every card names its unit ("3 stories in 2 specs"), tone is always paired with a text label, and the overlap sentence sits directly under the map. This is the one picture the spec asked for and it holds on home, stage, record, and panel.
- **Truthful copy.** Checklist counts say "checklist items checked". An active effort with every box checked stays "active". Unknown scope says unknown. No badge, no approval language, no reassuring zero while loading. I found no label that makes a judgment.
- **Guarded selection.** The changed-selection state names the lost record and its locator, disables copy, and offers two explicit choices. Disabled rows keep their reason visible.
- **Theme parity.** Each light and dark pair is structurally identical. I found no defect that exists in only one appearance.
- **Restraint.** Ink record titles, neutral secondary buttons, no red outside the focus ring, no shadows, no decorative colour. The home two-column composition by audience ("For designers and development leads" beside "For developers and implementing agents") serves both use cases at 1440px.
- **Narrow reading.** At 390px the map stacks, criteria wrap, and the code block stays inside its own region.

## Design health score

| # | Heuristic | Score | Key issue |
|---|---|---|---|
| 1 | Visibility of system status | 3 | States are complete and honest, but unmarked (VD-3). The panel loses the current stage (VD-6). |
| 2 | Match with the real world | 3 | Raw `ExperienceSpec`, paths in prose type, lowercase field names as headings (VD-4, VD-7). |
| 3 | User control and freedom | 3 | Clear, retry, and acknowledgement exist. "Clear selection" sits apart from the list it clears (VD-2). |
| 4 | Consistency and standards | 2 | Kit status marks, the disclosure chevron, the sidebar field, and the section heading size are bypassed (VD-4, VD-5, VD-6, VD-8). |
| 5 | Error prevention | 4 | Copy disables on incomplete or changed scope with a stated reason. |
| 6 | Recognition over recall | 3 | Rules sit beside every group. Status has no fixed position to scan (VD-5). |
| 7 | Flexibility and efficiency | 2 | Find and deep links only. Lists are tall, about 90px a row (VD-1). |
| 8 | Aesthetic and minimalist design | 2 | Double rules, collapsed section gaps, repeated row text (VD-1, VD-2, O2). |
| 9 | Error recovery | 3 | Retry and retained data work. The message leaks `TypeError: Failed to fetch` (VD-3). |
| 10 | Help and documentation | 3 | Inline rules and captions carry the explanation. |
| | **Total** | **28/40** | Good band, at its lower edge. |

**Design specificity.** The surface is authored for this product. The activity map, the provenance vocabulary, and the refusal to infer approval could not be lifted into another tool unchanged. What reads as generic is the finish, not the concept. Lists and notices look like unstyled defaults sitting inside a carefully designed house system.

**Cognitive load.** Two checklist failures, which is moderate. Visual hierarchy fails where section headings collapse onto the block above and match row size (VD-2, VD-4). Grouping fails where a second rule detaches a row's reason from its row and where child stories sit at the parent's indent (VD-1, O1).

## Cleanup plan

Eight required items in priority order, then optional cleanup. "Required" means the capture shows a defect against `DESIGN.md`, `PRODUCT.md`, or SPEC-0137 US7. None blocks task completion, so nothing is P0.

Every fix stays inside the delivery template directory. None edits the kit, the host, or `DESIGN.md`.

### VD-1 (P1, required). Every row and every record header draws two rules

- **Seen.** `home-light.png` shows a rule under each preview row at y≈600 and a second at y≈625, then y≈685 and y≈709. `ready-selected-light.png` shows the pair at y≈508 and 535, 599 and 626, 757 and 784. `inline-effort-light.png` shows two rules under the header at y≈282 and 307 with an empty band between. `healthy-story-partial-unresolved-light.png` shows the first rule at y≈556 cutting "Parent contract read incomplete" off from the US1 row it explains. `selection-incomplete-dark.png` shows the cost in density. Seven effort rows fill a 1000px viewport, about 90px a row.
- **Cause.** The kit `Facts` strip closes itself with a rule and a 14px margin (`web/kit/components.css:126-135`). `RowFacts` puts one inside every row (`presentation.tsx:183`), and each row adds its own rule (`delivery.css:83-86`, `delivery.css:98-103`). The record header does the same through `RecordPage` (`spec.tsx:102`, `effort.tsx:176`, header rule at `web/kit/record-page.tsx:53`).
- **Fix.** One rule in `delivery.css`:
  ```css
  .ae-row .rz-facts,
  .ae-select-row .rz-facts,
  .ae-page article > header .rz-facts {
    margin: 0;
    padding: 0;
    border: 0;
  }
  ```
- **Validate.** Recapture home, ready-selected, stage-active, healthy-story-partial, and inline-effort in both themes. Each row has one rule, the disabled reason sits inside its row above that rule, the header has one rule, and row pitch drops to roughly 60px.
- **Note.** Knowledge composes `Facts` into `RecordPage` the same way (`ao-kb/.../concept.tsx:132`), so its header likely shows the same pair. That is a kit follow-up outside this cleanup. I did not open a Knowledge capture to confirm it.

### VD-2 (P1, required). Section headings collapse onto the block above them

- **Seen.** `inline-effort-light.png` has "Actual delivered" touching the checklist note at y≈596 to 616, while its neighbours get about 40px. `empty-home-light.png` has "Start by shaping one spec" directly under the overlap sentence (y≈407 to 427) and "Workflow prompt" directly under the Outcome field (y≈552 to 566). `ready-selected-light.png` has "Workflow prompt" touching the "Clear selection" button (y≈330 to 352). `changed-selection-default.png` stacks two buttons with no gap and then the heading (y≈468 to 516). `frozen-spec-relationships-light.png` has "Effort relationships" tight under the last disclosure (y≈345 to 377). `panel-find-recent-light.png` has "Recent" touching "Record review" (y≈735 to 756).
- **Cause.** The kit spaces sections only when two are adjacent siblings (`.rz-section + .rz-section`, `web/kit/components.css:25-27`). The delivery pages put a paragraph, label, button, or disclosure between them (`effort.tsx:243-246`, `home.tsx:66-70` and `79-87`, `stage.tsx:212-253`, `spec.tsx:145-146`, `panel.tsx:100-109`).
- **Fix.** Replace the two local sibling rules (`delivery.css:299-301`, `delivery.css:312-314`) with one that does not depend on the previous sibling's type, and give the stage aside a stack gap:
  ```css
  .ae-page .rz-section:not(:first-child),
  .ae-panel .rz-section:not(:first-child) {
    margin-top: 22px;
  }
  .ae-with-aside > aside {
    display: grid;
    gap: 8px;
    align-content: start;
  }
  ```
  Then move the checklist sentence at `effort.tsx:243-246` into the Closure checklist section so it reads as that section's note, not as a stray line before the next heading. The copy stays word for word.
- **Optional within this item.** Move "Clear selection" (`stage.tsx:238-240`) into the list header controls beside "Select all visible eligible…" (`stage.tsx:161-174`). The aside then opens with its own heading, and both selection controls sit with the list they act on.
- **Validate.** Recapture the six states above. Every section heading has at least 22px above it. No two buttons touch.

### VD-3 (P1, required). Failed, partial, and changed states read as ordinary body text

- **Seen.** `retained-error-light.png` y≈164 shows "Read failed. TypeError: Failed to fetch" in plain ink with no mark. `healthy-story-partial-unresolved-light.png` y≈69 to 119 stacks three plain lines above the page title, including a raw path in prose type. `workspace-provenance-light.png` y≈328 sets "Incomplete execution context…" in muted 13px, which makes the page's most important caveat its quietest text. `changed-selection-default.png` y≈328 to 440 sets the selection change in body text. `frozen-spec-relationships-light.png` y≈433 uses ink 14px for the same kind of notice. That is four treatments for one kind of message, and none carries a shape.
- **Why it matters.** `PRODUCT.md` asks that status and risk never rely on one channel. Here they rely on reading every sentence. `DESIGN.md` keeps system diagnostics behind a disclosure (the Plain Reader Rule), and the kit already ships a failure line (`.rz-failure`, `web/kit/components.css:78-82`).
- **Cause.** `ReadState` and `CoverageNotice` emit bare paragraphs (`presentation.tsx:285-316`, `presentation.tsx:161-178`). The stage aside does the same (`stage.tsx:212-252`). `delivery.css:147-149` mutes the record partial notice.
- **Fix.** One notice treatment, applied to the existing elements. No copy changes except hiding the raw exception.
  1. Add `.ae-notice` in `delivery.css`: a 13px ink line on `var(--surface-sunk)` with 6px corners, `8px 10px` padding, and a leading mark. Use the kit `StatusMark` for the mark so the shape matches the host: the risk disc for read failures (`presentation.tsx:288`, `stage.tsx:243`, `relationships.tsx:55`) and the neutral ring for partial, retained, incomplete, and changed-selection lines (`presentation.tsx:294-308`, `stage.tsx:213` and `249`, `relationships.tsx:68`, and the `data-record-partial` line).
  2. Delete the muting rule at `delivery.css:147-149`.
  3. Keep "Read failed." and the Retry button visible. Move `query.error.message` (`presentation.tsx:289`, `stage.tsx:244`) into the button's `title` or a small `details`, so `TypeError: Failed to fetch` is available but not the headline.
  4. Merge the partial sentence and its coverage errors (`presentation.tsx:296-308`) into one notice with the paths as mono lines inside it. `DESIGN.md` asks that repeated conditions merge into one band.
- **Validate.** Recapture retained-error, both healthy-story-partial states, workspace-provenance, legacy-workspace, changed-selection, and frozen-spec-relationships in both themes. Each notice shows a mark and the same treatment. The `role="alert"` and `role="status"` attributes are unchanged, so the installed live-region assertions should pass untouched.

### VD-4 (P1, required). Evidence does not read as evidence, and headings do not outrank rows

Five small type defects with one theme. Paths, identifiers, and locators are the product's proof, and they are set like prose.

- **Seen.**
  1. `spec-expanded-light.png` y≈263 shows "ExperienceSpec · SPEC-9001 · docs/specs/experience/harbor-booking.md · Updated 2026-11-09" in Inter with visibly widened hyphens. `inline-effort-light.png` y≈263 shows the same widening. The 13px header path runs wider per character than the same path at 14px in the body at y≈512.
  2. `frozen-spec-relationships-light.png` y≈527 to 611 runs label and value together in ink Inter ("Spec Set (Frozen): docs/specs/…"), so three provenance kinds do not line up across efforts. The governing value shows raw `[[…]]` brackets, which reads as a rendering fault in prose type.
  3. `inline-effort-light.png` shows every "Source docs/…#status-777" line at body size, as loud as the content it cites.
  4. The compact map on record pages sets stage names at 12px and "This record" at 11px (`spec-expanded-light.png` y≈410 to 430).
  5. Section headings and row titles are the same size, and record titles are regular weight (`home-light.png`, `ready-selected-light.png`).
- **Cause.**
  1. `spec.tsx:103-107` and `effort.tsx:177` put the type identifier, id, and path inside `Facts`, whose list applies tabular figures to everything (`web/kit/components.css:143-151`). `DESIGN.md` names this exact failure in the Scoped Figures Rule. `spec.tsx:103` prints `ref.typeName` raw, against the Human Label Rule.
  2. `relationships.tsx:79-100`.
  3. `effort.tsx:97-103` asks for `.ae-note`, but `.ae-page button { font: inherit }` (`delivery.css:17-20`) outranks its 13px size.
  4. `delivery.css:275-279` and `delivery.css:285-288`. `DESIGN.md` reserves 12px for mono paths, ids, and ticks, and sets a 13px floor for structural labels.
  5. `.ae-page h2 { font-size: 14px }` (`delivery.css:14-16`) is unlayered, so it overrides the kit's 15/600 section heading, which lives in `@layer components` (`web/kit/components.css:30-37`). The record chip defaults to weight 400 and `delivery.css:87-91` raises only its size.
- **Fix.**
  1. Add one evidence class and use it for every path, id, and locator:
     ```css
     .ae-path {
       font: 500 12px/1.4 var(--font-mono);
       font-variant-numeric: normal;
       color: var(--text-muted);
       overflow-wrap: anywhere;
     }
     ```
     Wrap the values at `spec.tsx:104-105`, `effort.tsx:101-102`, `effort.tsx:177`, `effort.tsx:214`, `effort.tsx:218`, `relationships.tsx:80-98`, and `stage.tsx:222` in `<code className="ae-path">`. Raw `[[…]]` then reads as authored source, which is what it is.
  2. At `spec.tsx:103`, show the human type label through the kit (`useTypeDocs` with `typeLabel`), keeping the identifier as the `title`. The concrete type stays visible, as US3 requires.
  3. Delete `delivery.css:14-16`. Add `font-weight: 600` to `.ae-home-work > div > h2` (`delivery.css:295-298`) so the column title outranks the group headings under it. Add `font-weight: var(--title-weight)` to `delivery.css:87-91`.
  4. Raise `delivery.css:277` and `delivery.css:287` to 13px.
  5. Recommended, and the highest-value part of this item for lead review: render each relationship row at `relationships.tsx:79-100` as a two-column grid. The label ("Governing-specs metadata", "Spec Set (Frozen)", "Stories In Scope (Frozen)") sits in a fixed 13/500 muted column of about 190px. The authored value and its source sit in `.ae-path` beside it. The words do not change. This is what makes "which efforts froze US1" answerable by scanning one column.
- **Validate.** Recapture spec-expanded, frozen-spec-relationships, inline-effort, workspace-provenance, and home in both themes. Hyphens in paths are normal width, paths are mono, the type reads "Experience spec", section headings are visibly larger than row titles, and no Inter text is below 13px.

### VD-5 (P2, required). Recorded status has no mark and no fixed place

- **Seen.** `home-light.png` Shaping specs mixes "active" and "proposed" as muted lowercase words (y≈582, 666). `stage-history-light.png` mixes "complete" and "archived" the same way. `spec-expanded-light.png` y≈540 to 962 trails "ready", "draft", "ready", "satisfied" after story titles at four different x positions. On record pages the status is the quietest item in the header (`inline-effort-light.png` y≈263).
- **Why it matters.** `DESIGN.md` states that each status word keeps one mark everywhere (Active a blue half ring, Complete a green dot, Draft, Planned and Proposed a neutral ring, Archived a dashed ring) and that rows carry marks in lists that mix statuses. The delivery views draw none. A search for `StatusMark` in the delivery directory returns nothing, while Knowledge uses it (`ao-kb/.../assertions.tsx:226`).
- **Fix.** Render the recorded word through the kit's `StatusMark` with `value={status}` and `values={[]}`. The mark comes from the word itself (`web/src/components/StatusMark.tsx:57-83`), so the exact authored status stays the label and nothing is inferred. Words with no house mark, such as "ready" and "satisfied", get the neutral ring. Apply it at `presentation.tsx:183` (row note), `spec.tsx:102` and `effort.tsx:176` (record headers), and `spec.tsx:45-46` (story summaries). In the story summary, place the mark and word before the criteria count in a right-aligned group so statuses line up down the list.
- **Validate.** Recapture home, stage-history, spec-expanded, and inline-effort in both themes. Each status shows shape plus the unchanged word. Confirm the installed text assertions on status words still match, since the word is now inside a `span`.

### VD-6 (P2, required). The panel does not follow the house sidebar pattern

- **Seen.** In every capture with the panel, "Find a spec or effort" is a square, hard-outlined box (`panel-find-recent-light.png` y≈265 to 300). The map rows read as a run-on: "2 specs Shaping specs", "3 stories in 2 specs Ready stories" (y≈444 to 676), so labels start at a different x on every row. Link rows such as Specs, Efforts, User stories, and Feature areas sit about 19px apart (y≈922 to 980). On stage pages the panel says "Where you are: Delivery" and marks no group (`ready-selected-light.png`, `stage-history-light.png`, `stage-review-light.png`, y≈818).
- **Why it matters.** `DESIGN.md` says controls on the sidebar are chrome-tinted and never bright boxes, sidebar counts sit at the right, rows are 30px, and the focus ring on the chrome is `--chrome-text`. SPEC-0137 US5 says the panel shows the current location.
- **Cause.** `delivery.css:190-198` (input), `presentation.tsx:137-148` with `delivery.css:187-189` (count before label, line break hidden), `delivery.css:199-204` and `panel.tsx:100-145` (row height), `delivery.css:141-146` (red ring applied to panel buttons), `panel.tsx:70` and `panel.tsx:99` (location falls back to "Delivery" for every non-record page and the map receives no `current`).
- **Fix.** Copy the sibling's pattern at `ao-kb/.../panel.css:39-70` and `100-118`.
  1. Input: 28px tall, no border, `var(--r-md)` corners, `background: var(--chrome-raised)`.
  2. Rows: `min-height: 28px`, 8px inline padding, `--chrome-raised` on hover, for map rows, find results, recent, supporting views, and native collections.
  3. Compact map row: label on the left, count and unit on the right in tabular figures. The text and its numbers stay the same.
  4. Focus: `.ae-panel :focus-visible { outline: 2px solid var(--chrome-text); outline-offset: -2px; }` and drop `.ae-panel button` from `delivery.css:143`.
  5. Location: when `location.viewId` is `agentic-engineering.stage`, show that group's title and pass its stage as `current` to the panel map.
- **Computed, not captured.** The current ring is `--focus-ring` `#c8313a` on Graphite light chrome `#45433f`, which is about 1.9:1. No capture shows panel focus.
- **Read from code, not exercised.** Fix 5 assumes the panel's `useViewLocation()` reports the page's view id and params the way it already reports the page's node context. Confirm that in the installed app before relying on it.
- **Validate.** Recapture panel-find-recent and one stage page in both themes. Add one installed assertion that the panel marks the current group on a stage route. Tab through the panel once and confirm the ring is visible on the chrome.

### VD-7 (P2, required). Singular counts and lowercase field labels

- **Seen.** "Recorded proposed. 1 visible eligible ready stories." (`ready-selected-light.png` y≈671, `stage.tsx:199-201`). "Find 1 loaded records" (`panel-find-recent-light.png` y≈322, `panel.tsx:92`). Lowercase bold "implementation plan" and "work log" used as headings (`workspace-provenance-light.png` y≈943 and 985, `effort.tsx:257-259`).
- **Read from code, not captured.** The same pattern yields "1 stories" (`spec.tsx:119`), "1 criteria" (`spec.tsx:48`), and "1 recent visits are not in the loaded inventory" (`panel.tsx:120`).
- **Fix.** Use the existing `unit()` helper (`presentation.tsx:87-91`) and the criterion ternary already in `RowFacts` (`presentation.tsx:200-202`). Capitalise the three display labels to "Implementation plan", "Work log", and "Supporting material", leaving the `aria-label` text as it is. No meaning changes.
- **Validate.** The mixed fixture already contains a one-story parent and a one-match find. Recapture ready-selected and panel-find-recent. Update any installed assertion that pins the plural string.

### VD-8 (P2, required). Three small departures from house tokens and controls

1. **Read-state indent on effort pages.** `inline-effort-light.png` starts "Saved records…" at x≈313 while the title starts at x≈289. `retained-error-light.png` shows the same 24px step. `effort.tsx:160` renders `ReadState` inside `.ae-page`, so its own 24px padding (`delivery.css:240-242`) stacks on the page's. `spec.tsx:165` does the same on its no-data path. **Fix.** `.ae-page .ae-read-state { padding-inline: 0; }`.
2. **Undefined token.** `var(--sunk)` at `delivery.css:113` and `delivery.css:352` matches no token. `web/src/tokens.css:56` defines `--surface-sunk`, and no file defines `--sunk`. The prompt well and criterion code blocks therefore render with no tint, which `spec-expanded-light.png` y≈706 to 768 shows as a white code block. **Fix.** Use `var(--surface-sunk)` in both places.
3. **Native disclosure triangles.** Every story and source disclosure shows the browser's ▶ marker (`spec-expanded-light.png` y≈540 to 962). `DESIGN.md` allows one disclosure control, the 16px chevron, and the kit exports it as `DisclosureIcon` (`web/kit/layout.tsx:249`). **Fix.** Add `list-style: none` and a flex row to `.ae-page summary` (`delivery.css:138-140`), then lead each summary with `DisclosureIcon` at `spec.tsx:43` and `197`, `effort.tsx:124`, `231`, `339`, `348`, and `364`, and `evidence.tsx:159`. Summaries without React open state can rotate the icon with `details[open] > summary .rz-disclosure-icon`.

**Validate.** Recapture inline-effort, retained-error, and spec-expanded in both themes. The read-state line shares the title's left edge, code blocks sit on the tint, and disclosures show the house chevron. Run the keyboard disclosure test once, since the summary markup changes.

## Optional cleanup

Take these only if they fit the same pass. None is a defect against a written rule strong enough to require it.

- **O1. Indent child stories under their parent spec.** In `ready-selected-light.png` the parent checkbox and its stories share x≈294, so "Harbor proposed contract 2" reads as a sibling of the story above it. One rule fixes it: `.ae-story-group > .ae-select-row + .ae-select-row { padding-left: 24px; }` (`stage.tsx:185-205`). This is the optional item I would take first.
- **O2. Say the repeated row text once.** "Open brief for checklist evidence" repeats on all nine Active rows (`selection-incomplete-dark.png`, `presentation.tsx:212`), and "ready" repeats on every Ready row. `DESIGN.md` asks for status per row only in mixed lists. Moving the checklist sentence to the group caption changes where the copy sits, so I leave it as Drew's call.
- **O3. Shorten the preamble.** Each page opens with "Saved records. Unsaved workspace edits are not included." and a Refresh button on two lines above its title (`presentation.tsx:309-314`). `DESIGN.md` puts a refresh control inline after the text it renews. Home also repeats "Delivery" as a section heading under the view title (`home.tsx:62`), and stage pages repeat their title as the list heading (`stage.tsx:151` and `156`).
- **O4. Hold the layout while loading.** `loading-light.png` is two lines on an empty panel. Five neutral `.rz-placeholder` blocks in the map's grid would keep the page from jumping. The current state is honest and acceptable.
- **O5. Even out compact map cards.** A card with "This record" is taller than cards in other activity groups (`inline-effort-light.png` y≈374 to 425). Stretching all five to one height removes the ragged row.
- **O6. Separate "current" from "focused".** The current stage card uses a 2px offset ink outline (`delivery.css:60-63`), the same geometry as the focus ring. A `--selection` fill would say "you are here" without looking like keyboard focus.
- **O7. Remove the dead rule.** `.ae-panel .ae-link` is declared twice (`delivery.css:164-166`, `delivery.css:266-268`). The first never applies.

## Persona checks

- **Lead or designer shaping the next slice.** Home answers "what is recorded and where do I act" in one viewport, and the Ready list composes one prompt. The weak points are scanning status across a mixed list (VD-5), telling parents from children (O1), and reading relationship provenance on a spec (VD-4 step 5).
- **Developer or agent resuming or finishing.** "Where to resume" sits top left, the checklist count is exact, and the brief never claims acceptance. The weak points are the caveat that whispers (VD-3), source lines as loud as content (VD-4), and headings that collide in the closure column (VD-2).
- **Keyboard and low-vision user.** Marks are missing where meaning should not depend on reading every sentence (VD-3, VD-5), and the panel's focus ring is likely too faint on light chrome (VD-6, computed).

## For the quality reviewer, outside my ownership

One thing I noticed and did not verify. SPEC-0137 US2 lists "sort" among the things each stage list shows. No stage capture I opened states an order. I did not check whether the rule text or the tests cover it.

## Run notes

- Target: `pkg/app/cli/init/templates/starters/agentic-engineering/module/views/delivery/`. No slug, snapshot, or trend was written, because the brief allows one file.
- Ignore list: `.impeccable/critique/ignore.md` was not consulted. It lives in the report-only `.impeccable` tree.
- Assessment independence: degraded, one context, design review completed before the detector ran.
- Detector: ran once, 0 findings, exit 0.
- Browser, overlay, live server: not used, by instruction.
- Temp files: detector output sits in this session's scratchpad only.

Questions skipped: the brief asks for a report to the root agent, which decides scope with the second review in hand.
