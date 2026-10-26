# Workbench redesign reference screenshots

Reference captures of the Effort notes table (the `EffortNote` type collection, Table presentation) after the SPEC-0126 workbench redesign. They are fixtures for future design work, not asserted tests: the repository has no pixel-diff test layer. The four PNGs sit beside this file, tracked in Git, at 165–225 KB each; review tools that skip binary files will not list them.

| File | Viewport | Appearance |
|---|---|---|
| `effort-notes-table-graphite-light-1100x800.png` | 1100×800 | Light |
| `effort-notes-table-graphite-dark-1100x800.png` | 1100×800 | Dark |
| `effort-notes-table-graphite-light-1440x900.png` | 1440×900 | Light |
| `effort-notes-table-graphite-dark-1440x900.png` | 1440×900 | Dark |

## How they were captured

- **Code:** commit `954ab7b4` (batches 1–5 and 2b of the redesign), built with `make build`.
- **Data:** this repository's own vault, served with `RZM_SKIP_REPO_DELEGATE=1 ./bin/darwin/rzm serve --port 7895` after "Index complete".
- **Browser:** Playwright Chromium, device scale factor 1, so one image pixel is one CSS pixel. The script opened `/notes/effort-note` with `waitUntil: "load"`, waited for the first table row, and set `localStorage` `rhizome:theme` to `graphite` and `rhizome:appearance` to `light` or `dark` before load.
- **State:** default view settings, no row selected, the right pane closed.

The Changed column shows relative times ("9m") and the rows come from the live vault, so text in a fresh capture differs. Compare layout, column widths, type, and colour, not content.
