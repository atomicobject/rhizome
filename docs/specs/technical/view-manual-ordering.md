---
type: TechnicalSpec
summary: "Lets a reader drag rows, cards, and kanban cards into a new order in any configured view sorted by an editable number, enum, or boolean field. A drop stages ordinary field updates through the edit session, so the new order is the view's own sort and Save commits it like any other edit."
id: SPEC-0108
spec-status: proposed
last-updated: 2026-10-02
aliases:
  - SPEC-0108
  - View manual ordering
---

# View manual ordering

## Summary

People keep lists in an order that reflects their judgment: a ranked backlog, opportunities in the order they want to attend to them, items by priority. In a configured view ([[configured-view-engine-and-repo-config|SPEC-0058]]) that order is already expressible: author a number field such as `rank` or an enum such as `priority` and sort the view by it. Keeping that order current means editing field values by hand, and finding a number between two others.

This spec lets the reader drag an item to where it belongs instead. The view's sort stays the only source of order. A drop computes the field values that place the item at the drop point under the current sort and stages them through the ontology edit session as ordinary `setField` updates. The server re-executes the view with the staged values, and the item appears where it was dropped. Save commits the change atomically with any other staged edits.

No view configuration is needed. A view is orderable whenever its current sort leads with a field the reader can already edit inline.

## Goals

- let a reader reorder items by pointer drag and by keyboard in table, card, and kanban variants
- derive order only from the view's current sort; never store a separate order list
- write order as values of authored fields, staged and reviewable through the edit session, committed atomically
- touch as few notes as possible: one note for a typical drop, and more only when no value fits between the neighbors
- work for number fields (Float and Int), enum fields, boolean fields, and sorts that combine them, such as priority then rank

## Non-Goals

- a view-level or vault-level stored order separate from field values
- reordering by date, text, link, or list-valued fields; a drop cannot meaningfully set those
- reordering across pages; see the requirements on truncated results
- changing kanban column order or group order by drag; those come from schema enum order and view group metadata
- touch and coarse-pointer gestures beyond what native HTML drag provides
- server-side or agent-facing reorder APIs; agents set the same fields directly

## Requirements

### When a view is orderable

- A view MUST be orderable when the first key of its effective sort names a field whose execution capability has a safe inline edit (`edit` present) and whose value kind is `int`, `real`, `enum`, or `bool`. An `int` or `real` key MUST also be sorted from the index (`indexedSortable`), because number placement relies on missing values sorting last. The effective sort is the one the execution response echoes, including a sort the reader chose by clicking a column header.
- Each later sort key MUST participate in placement while it is also orderable by the same rule. Placement stops at the first key that is not; that key and later ones keep breaking ties as they do today.
- A view that is not orderable MUST look and behave as it does today. An orderable table MUST show a drag handle on each row only on hover or focus, and MUST NOT change click, double-click, or keyboard behavior on cells. Cards and board cards are dragged whole, as board cards already are.
- Reordering MUST be unavailable while the execution result is truncated (`hasMore` or a total larger than the returned rows), with the handle replaced by a short explanation on hover. Placement needs every neighbor.

### Drop targets

- A drop target is a gap between two adjacent items in one group: the table group, card group, or kanban column the items share. The drop indicator MUST show the gap.
- The gap's anchor is the item whose half the pointer is over: the lower half of item A anchors "after A", the upper half of item B anchors "before B". The first and last gaps have only one neighbor.
- In a grouped table or card view, dropping into a different group MUST also stage the group field change when that field has the safe enum, boolean, or single-node edit a kanban move requires; otherwise other groups MUST NOT accept the drop. In a kanban view, dropping into another column at a position stages the column move exactly as SPEC-0058 defines it, plus any placement updates.
- Dropping an item into its current position, or onto itself, MUST stage nothing.
- A gap whose drop would stage nothing MUST NOT be offered: no drop indicator, and the drop is not accepted. This includes the gaps inside a run of equal values when the last orderable key is an enum or boolean, because no field can place the item within that run; an item that joins such a run lands in the run's tie order.
- On a board, a drop on another column's background MUST place the card first in that column, where the column shows its drop slot, with the same placement as dropping before the column's first card.

### Placement

Placement walks the orderable sort keys in order, with the previous neighbor `P`, the next neighbor `N`, and the moving item `X`. For each key:

- If `P` and `N` both exist and hold equal values, `X` MUST take that value and placement continues with the next key.
- Enum and boolean keys: `X` MUST take the anchor's value. Placement continues with the next key, with the neighbor that holds a different value treated as absent.
- Number keys: `X` MUST take a value strictly between `P` and `N` in sort direction: their midpoint for `real`, and the integer midpoint for `int` when one exists. With one neighbor, `X` MUST take that neighbor's value one step away in sort direction. Placement then stops.
- Values compare as the server sorts them (`sortRows` in `pkg/app/views`). Indexed fields, which include enums, compare normalized text (lowercased, trimmed, link wrapper and alias removed) and keep missing values last in either direction; enums therefore sort by value text, not schema order. Booleans are not indexed: they compare as text with missing values first when ascending. A neighbor with no value counts as absent for number placement.
- When no value fits, because `int` neighbors are adjacent, two `real` neighbors are equal, a value is too large to hold exactly (an `int` beyond JavaScript's safe integer range, or a `real` that one step would not change), or `X` lands among items with no value, placement MUST renumber: assign whole-number values in steps of one, in sort direction, to the items of the group from its first item through the later of `X` and the last item that already has a value, in their new order. Items outside that range keep their values.
- `real` midpoints MUST be written as the shortest decimal that sorts correctly, so repeated drops do not produce long fractions. Placement SHOULD prefer a whole number when one fits.

### Writing

- All updates from one drop MUST be staged together through the edit session in one `stageOps` call, as `setField` ops with the existing stable op ids and `expected` witnesses, so a later drop replaces earlier staged values for the same note and field.
- The view MUST show the dropped item at its new position within its group while the server re-executes, then defer to the re-executed rows. Board column moves keep their existing staged placement. A table or card row whose staged group values match another group's, and no longer its own, MUST show in that group; a row whose values match no group, such as a link or list value, stays where it is until the view re-executes. Renderers MUST NOT write notes directly and MUST read staged values through the shared staging helpers.
- A staging failure MUST roll back and show an alert naming the item, as kanban moves do; success MUST be announced to assistive technology.

### Keyboard

- A focused orderable row or card MUST move one position up or down with Alt+Up and Alt+Down, using the same placement as a drop with the passed item as anchor. When that drop would stage nothing, as inside a run of equal enum or boolean values, the move MUST continue to the next item in that direction whose drop changes something; cards in a grid also move with Alt+Left and Alt+Right. In tables and card grids, moving past a group boundary follows the cross-group rule above. On a board, Alt+Arrow keys stay within the column; the Move menu changes columns.

### Verification

- Unit tests MUST cover placement for `real`, `int`, enum, boolean, mixed enum-then-number sorts, descending sorts, missing values, renumbering, and no-op drops.
- Component tests MUST cover pointer drop and keyboard moves in the table, cards, and kanban, and read-only behavior for non-orderable and truncated views.
- An end-to-end test MUST reorder a row in the fixture vault and see it staged.

## Open Questions

- None. The interaction is first proven on Drew's IP opportunities view, which sorts by `rank` within `stage` groups.
