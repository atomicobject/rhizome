---
project: "[[Project Kestrel]]"
related: ["[[Retrospective - Moving to Fernbank]]", "[[Records cutover planning, 2026-02-10]]"]
---
# Bookwright export field map

How fields in the old Bookwright export line up with Fernbank, kept by [[Odile Varga]] for the [[Project Kestrel|catalog migration]].

## Bibliographic fields

Title, author, and edition map one to one. Local subject headings go in a separate local field, which Fernbank hides unless the branch display profile turns it on.

## Barcodes

Pinecrest barcodes begin with two zeros. The export must keep them as text, not numbers, or the zeros disappear and the items will not scan.

## Patron fields

Patron categories collapse from eleven to six. Blocks carry over; loan history does not.
