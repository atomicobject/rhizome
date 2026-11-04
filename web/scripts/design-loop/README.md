# Design loop

Tools for hill-climbing the web workbench's visual design: capture a fixed set of screenshots, have a council of models score them with [RUBRIC.md](RUBRIC.md), fix what they find, and capture again. Implementers make the design changes from a specific brief (Opus 5.5 in rounds 1–4, GPT-6.1 Sol from round 5); the reviewers only review.

## Set up the frozen vault once

The screenshots use this repository at a pinned commit, so changing content never moves a score. Build `rzm` first, then run:

```sh
make build
web/scripts/design-loop/snapshot.sh setup <vault-dir> <commit>
```

`setup` turns embeddings off in the copy, so no note text goes to a provider. It also restores each file's last-commit time, so recency views look real.

## Capture a round

Build the version you want to score with `make build`, then run from `web/` with the sandbox off:

```sh
node scripts/design-loop/capture.mjs --vault <vault-dir> \
  --out scripts/design-loop/rounds/round-N/shots --now 2026-11-04T18:00:00Z
```

One command does the whole capture. It starts `rzm serve` from `bin/<os>/rzm` on 127.0.0.1:7841, with saved view state wiped and only on that address. It waits for the startup re-index to finish, takes every screenshot, and stops the server. A full run takes about 35 seconds. Use `--rzm` and `--port` to choose another binary or port, `--only id,id` and `--themes light` for a subset, and `--jobs` to change how many screens are captured at once (default 4). Keep `--now` the same for every round so relative times match. `rounds/` is ignored by Git.

To iterate on one screen against a server you already run with `snapshot.sh serve`, pass `--base http://127.0.0.1:<port>` instead of `--vault`. `serve` refuses any directory `setup` did not create and any vault with embeddings enabled, because it trashes the vault's saved view state.

Two captures of the same build match byte for byte. The script gets there in these ways:

- **Readiness.** It takes each screenshot once the page is quiet: no requests in flight, no graph layout running, no `aria-busy` element, fonts and images loaded, and no DOM change in any frame for 500 ms. A screen that has not settled after 20 seconds prints `SLOW` and is captured anyway. There are no fixed waits.
- **Randomness and time.** `Math.random` is seeded, because the Explorer graph jitters its starting positions. `--now` pins the clock, animations are disabled, and Chromium runs without partial raster, which otherwise varies antialiased edges between runs.
- **Shared state.** Each screen gets a fresh browser context. Finder recents, the one server-side record the current screens write, are answered in the browser and never reach the server. The script prints `WARN` if a screen saves view preferences, since other screens could then read them.
- **Server churn.** If the server re-indexes while a screen is captured (for example, because something touched the vault), the script prints `RETRY` and captures that screen again.

`capture.mjs` defines the screen list (`SURFACES`) and the cross-section scored for the overall app (`GLOBAL`). Add a screen there, not in a one-off script. An `act` runs after the page settles. It should wait for whatever it opens; the page settles again before the screenshot.

## Compare two rounds

```sh
node scripts/design-loop/compare.mjs scripts/design-loop/rounds/round-1/shots scripts/design-loop/rounds/round-2/shots
```

It prints each screenshot that changed, with its count of changed pixels, and writes `compare.html` in the later round's folder. The page puts the before and after screenshots of each changed screen side by side for human review. `--all` includes unchanged screens, and `--sheet <file.html>` writes the page somewhere else. Comparing two captures of the same build is also the repeatability check: it should report 0 different.

## Run a review round

Give each reviewer `RUBRIC.md` and the round's `shots` folder. Start a fresh reviewer every round, and never tell reviewers the target score or earlier scores. Record each reviewer's JSON block in the round folder.
