# AO KB knowledge workspace: design notes, comp v1

The prototype is `comp-v1.html`, a self-contained page with no network calls. Its data is illustrative: a fictional clinic-booking product called Booking, with 18 assertions, 10 sources, 7 concepts, 3 artifacts, and 23 product-model rows. These notes cover the round-2 refinement against the updated SPEC-0133 R7. They are not implementation approval and not an independent council review.

## Direction

The workspace extends Rhizome's existing visual world without changing it. It reuses the Graphite chrome, the white inset panel, Inter 14px UI type, Merriweather record titles, JetBrains Mono for identifiers and paths, and teal for provenance and links. Red marks only real risk: an open registry dispute, unresolved evidence, or a dangling reference. The `.app` element represents the Rhizome shell and keeps Rhizome's own tokens; T3 theme variables style only the comp controls above it, and the page has no outer padding.

| View | Job | Where |
| --- | --- | --- |
| Attention | Find what needs attention and why | Knowledge › Attention |
| Concepts | Browse knowledge areas with origin-citation coverage | Knowledge › Concepts |
| Assertions | A standard sortable table, the foundation | Knowledge › Assertions |
| Assertion record | The claim, its evidence, conflict, history, and saved analysis | Record tab |
| Source record | Raw passages with their citing claims in the margin | Record tab |
| Trace | Bounded upstream and downstream navigation around one focus | Knowledge › Trace |
| Model, artifact, and code records | YAML rows with their own locators, artifacts with sync dates, referenced code files | Record tabs |

## Round 2 refinements

1. **Trace shows only useful edges.** Columns are hops from the focus: upstream on the left, the focus with its lateral relations in the middle, downstream on the right. Each shown record draws exactly one line, to the record it was reached from, so lines run only between adjacent columns and never arc within a column. Lateral claim relations and concept mentions sit beside the focus as cards with no line. Every other relationship traversed within the selected depths is a companion row, labelled `Second path` or `Record behind Show more`. At narrow widths (container under 900px) the same columns become stacked sections with no drawn lines.
2. **Depth is bounded in both directions.** Upstream and downstream each offer 1, 2, or 3 hops; there is no All. The count line states records and relationships in the requested scope, and per-direction counts. A comp control sets the query budget; at 12 records the Clinic operations sync trace reports `Incomplete: the query stopped at its 12-record budget while reading downstream hop 2`, separate from the per-column display cap.
3. **Links are real prototype links.** The URL fragment of this HTML file carries the navigation state. Copy link copies the page's own address. Each navigation is a browser history entry, so browser Back and Forward work, and the in-app Back and Forward buttons follow the same entries. The invented `rhizome://` link is gone.
4. **File-level sync warnings list their rows.** `features.yaml` names FEAT-BOOK-03, FEAT-BOOK-04, and FEAT-BOOK-05, each with the citation that changed and its updated date, and each row opens itself. Artifacts keep their own rows.
5. **Code paths are record navigation, not edges.** `implements` values appear as `Referenced code paths` on the component record. An exact path in the repository opens the code file; a missing one says `Not available in this repository`. Trace has no code nodes and does not count these paths; a component card says how many paths its record lists.
6. **Attention is quieter.** Each group shows its title, count, and exact basis. The explanation sits behind the basis button (`aria-expanded`, `aria-controls`). AO's saved bands are collapsed by default under the saved-analysis divider.
7. **Dates and copy.** `validFrom` is labelled `valid from` everywhere (questions, missing evidence, conflict comparisons), and a superseding claim's date reads `successor valid from`. Product copy has no em dashes, including the synthetic source titles; empty values read `Not recorded` or `None recorded`.

The council refinement removes redundant edge labels from the graph, keeps each depth label with its buttons, and uses one relationship-list expander for the entire counted scope. Superseded and expired status appears beside the claim title. Reviewed, Priority, and Horizon remain separate facts; an absent review says `Not recorded`. Conflict summaries name the registry's stored state and impact. Evidence coverage separates a missing source from an unresolved source or locator.

Synthetic saved summaries use AO's `basis_line` and `rollup` formatters: questions are separate from the weighed-claim total, origin counts are retained, reviews include their dates, open conflicts include their IDs and impact, and supersession includes its target. The stored bands and snapshot ages remain illustrative inputs; the comp does not run or validate AO's weighting algorithm.

## What the comp says, and how it stays honest

- **Authored, checked, and saved are visibly distinct.** Every Attention group names its basis. AO's saved bands carry builder, as-of date, and file, are never called current, and a claim updated after the as-of date shows `may be stale`. A missing weights file reads `Unavailable ... This is not a clean result.`
- **The record keeps facts apart.** Stored confidence says it is not a verdict. Missing attestation shows `Not recorded` plus AO's default. Speaker, role, validity, history, and the saved assessment are separate blocks. Nothing becomes a score.
- **Four citation states render differently:** recorded passage (with context lines and `Passage hash not verified by Rhizome`), document only (`No passage locator recorded`), timestamp locator (shown, not inline yet, with Open source), and unresolved source (named, no substitute).
- **Contradiction is not a verdict.** The claim page shows the `_conflicts.md` entry beside both claim texts and says Rhizome does not pick a winner.
- **AO handoffs are honest.** `Change this in AO KB` copies `/ao-kb:curate`, `/ao-kb:suggest`, or `/ao-kb:process` and does nothing else. No Resolve, Accept, Edit, or Publish control exists. `Refresh bindings` announces that it was simulated.
- **YAML rows stay distinct.** Rows from one file open as separate tabs with their own locators and links. Model records list the records that declare structural relations to them.
- **Product scope is shown but not decided.** Search finds a Billing note and labels it `Billing · not in AO model`.

## Navigation and links

- **State in the URL:** presentation, record, passage target, concept filter, find text, sort, Trace focus, trail, upstream and downstream depth, column expansions, mentions, and relationship-list expansion. Non-default comp controls (slice, analysis, bindings, fixture, budget) ride along so reviewers land in the same fixture. Appearance and collapsed groups are display preferences and stay out.
- **History:** opening a record, switching presentation, refocusing Trace, and following the trail push entries. Filters, sort, depth, and expansion replace the current entry. Reloading keeps Back because the stack is stored in `history.state`. An unknown record in a link shows `No record named ... here` rather than redirecting.
- Record links also preserve the originating workspace presentation. Back, reload, and Forward preserve the full navigation stack. Trace does not show list-only filter controls; their state is retained when returning to the other presentations.
- **Embedded previews:** when the page has no address (`about:srcdoc`, `blob:`), Copy link is replaced by `No direct link in this preview` with the reason in its title. If the frame refuses history changes, the in-app Back and Forward still work from their own entries.
- **What was not tested:** Rhizome's own view URL format and native deep links. The comp proves the state model and history behavior on a `file:` page and in a sandboxed srcdoc frame only.

## Trace rules

- Scope includes relationships traversed within the selected depths, including lateral relations of the focus. It excludes links between boundary records that would require another hop.
- Traversal follows the R7 classification. Upstream follows outgoing evidence references and incoming declared structure; downstream follows incoming evidence references and outgoing declared structure. Lateral relations of the focus are never expanded. Arrows keep the authored direction.
- Each column caps at 6. When a column overflows, records are picked round-robin across their parents so one busy parent does not fill it, then shown in parent order so lines do not cross. `Show N more` names the hidden types. Records reached only through hidden parents are counted as `N more reached through records not shown`.
- Hover or focus on a card highlights its line back to the focus and every companion row that touches it. Card text states the relationship to its parent, so meaning never depends on hover or color.
- A source focus adds an `Extraction history` line: claims listed in `extractedAssertionIds`, and whether they also cite the source. Extraction is never drawn as support.

## States covered

| State | How to reach it |
| --- | --- |
| Stale saved analysis | Default. The banner names the count; Same-day cancellation is free is marked `may be stale` in the saved group, table, and record. |
| Saved analysis with no known later edits, missing analysis | Comp control. |
| Stale bindings | Comp control. Combined with stale analysis, one banner with Details and Refresh bindings. |
| Empty product | Comp control. Every view says `No assertions in Booking yet` and gives the AO command. |
| Markdown-only slice (Batch 2) | Comp control. Model types are absent; Trace says `Product model not bound in this slice`. |
| Missing or unresolved evidence, dangling references, other-product collision | Default data (Eligibility check, Minors need a guardian, Booking PRD, FEAT-BOOK-05). |
| File-level sync | Default. Three model files list their affected rows; one artifact's sync date is unknown. |
| Dense neighborhood | Trace focus Clinic operations sync, downstream 3: 21 records, round-robin column cap, hidden-parent counts. |
| Incomplete query | Comp control `Trace query budget: 12 records`, focus Clinic operations sync. |
| Empty filter result | `None match the current filter` per group, with Clear filters. |
| Superseded, expired, resolved | Reminders 48h before visit, Saturday hours through October, Same-day video visits. |
| Code path not in this repository | Waitlist offer component. |
| Unknown record in a link | `#record=not-a-record`. |
| Light, dark, 1280px, 728px | Verified by screenshot; no horizontal page scroll at either width. |

## Feasibility assumptions

- The workspace is a starter-shipped kit view on the Knowledge group using `useViewRows` and `useGraphQL`, plus node presentations for assertion, source, and model records. Trace is a custom SVG view because `Diagram` cannot do counted hop columns, re-centering, or edge-row pairing.
- The Trace query is server-side, bounded, and counted per direction and hop, with an explicit incomplete flag when the budget stops it. The client must not fetch everything and cap.
- Inline passages need a narrow, path-checked read of a line or section span from an authorized raw file.
- `Referenced code paths` uses existing file navigation and an exact-path existence check within the configured code roots.
- Conflict registry entries are read through the knowledge adapter and shown on the claim page.

## Verification

A Playwright script against the `file:` page checked: explanations hidden until requested; saved bands collapsed; `features.yaml` lists its three rows and opens the chosen one; browser Back and Forward and the in-app Back agree; find text survives a record round trip; Copy link copies the page address with state; a fresh page restores Trace focus, depth, and expansion from a link; Trace draws one line per shown non-lateral record (13 for 13) with no All control; refocus is a history entry; the 12-record budget reports an incomplete query; code paths open or say `Not available in this repository`; no em dashes in eight views; no horizontal page scroll at 1280 and 728 in light and dark; in a sandboxed srcdoc frame the link is labelled unavailable and in-app Back still works; no page errors.

The engineering review identified four prototype defects and reversed extraction wording. Six focused checks reproduced the failures before correction and passed afterward: document-only citations open the available source text without passage highlighting; record links and reload preserve the originating Trace presentation; Forward survives Back and reload; Trace hides inapplicable list filters while retaining them for list views; the AO total excludes ordinary code files; and extraction names the source as the declaring record. The changed source and Trace screens were captured at 1280 light and 728 dark.

Final checks passed for the single relationship expander, all counted relationship rows, grouped depth controls, and removal of edge labels at 1280px and 728px in both themes. Five coverage categories account for all 18 claims, with separate missing and unresolved examples. Superseded status, review/priority/horizon facts, and registry/timestamp copy were checked on records. All 18 saved assertion basis strings and six stored concept rollups match the imported AO formatters for their fixture inputs. The six navigation/source regression checks still pass. Graph, relationship-list, concept, assertion, and source captures were inspected; no page errors were reported.

The final evidence review also reproduced and fixed three inconsistencies: concept membership now uses all authored concept links across tables, records, filters, and Attention counts; the Markdown-only Trace explicitly excludes unbound model records from its counts; and superseded questions are no longer open. These three checks pass, as do the six earlier navigation/source regressions. Overall coverage totals count a claim once even when it links to several concepts. Registry details include the recorded impact reasons, dependents, and last escalation; sources without fixture text say so. Evidence coverage follows the marked origin before the first-source fallback, and default attestation follows the four direct-by-default AO kinds.

## Remaining issues

1. **Single-product scope** remains Drew's decision; the comp assumes it.
2. **Product link format** is undefined. The comp proves the state model, not Rhizome's view URL encoding.
3. **Query budget value** is not pinned in the plan. The comp uses 200 records and a 12-record demonstration.
4. **Graph keyboard model:** cards are Tab-focusable buttons with full labels and the companion rows are the accessible list, but there is no arrow-key movement between cards.
5. **At depth 3 on both sides** the graph can exceed the panel and scrolls horizontally inside its own box. Empty columns are narrowed to delay this.
6. **Unclassified custom relationships** are not in the fixture, so their list-only treatment is described, not demonstrated.

## Plan review (SPEC-0133, updated R7)

The updated R7 resolves both round-1 blockers: the traversal table defines upstream, downstream, lateral, and extraction behavior, and `implements` paths are now record navigation excluded from Trace counts. It also adopts the coverage unit, file-level sync grouping, the validity-date rule, the named presentations, and the navigation state list. The remaining open item is the product-context decision. A pinned budget value and a stated product link format would make the Trace and direct-link acceptance cases fully testable.

**Plan self-score: 8.5/10** (round 1: 7.5).

## Design self-score: 8/10 (round 1: 7)

- **Stronger:** Trace legibility at both widths, bounded and counted scope, honest incomplete state, working links and history, quieter Attention, row-level sync navigation.
- **Still weak:** the Concepts chart is modest; companion rows are tall when the focus repeats; no arrow-key graph navigation.

## Questions for the next review

1. Is one line per record, with second paths only in the list, enough relationship context, or should hovering a card also draw its second paths?
2. Are upstream and downstream defaults of 2 hops right, or should upstream default to 1?
3. Should the round-robin column cap stay, or should the cap favor records with recorded attention conditions?
