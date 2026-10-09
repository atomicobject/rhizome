---
project: "[[Project Kestrel]]"
related: ["[[Duplicate records triage, 2026-03-24]]", "[[Decision - Freeze cataloging during cutover week]]"]
---
# Retrospective - Moving to Fernbank

Written in April 2026, four weeks after go-live, to record what went wrong and what we would do differently on the next [[Project Kestrel|catalog migration]].

## What went wrong

- Duplicate records: matching against the consortium file was too loose, and about 6,200 titles ended up with two records.
- Lost local headings: Fernbank dropped our regional history subject headings on load, so local history searches returned almost nothing for two weeks.
- Barcodes: the first export stripped leading zeros from Pinecrest item barcodes, and 11,000 items would not scan until the second run.
- Holds: queues for 1,400 popular titles reset to the order of the load file, not the order in which patrons placed them.

## What went well

The paper slip process during the offline week worked, and the [[Decision - Freeze cataloging during cutover week]] kept edits from being lost.

## Next time

Run a full test load against the consortium file, not a sample, and give branches a printed checklist before go-live.
