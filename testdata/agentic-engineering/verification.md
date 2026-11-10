# Harness verification

The suite exercises the installed starter, not a copied demo. The integrated run passed all 54 enabled tests and captured 47 screenshots across both themes. It covers selected prompts, changed eligibility after refresh, retained data, source-aware links, both effort forms, sparse scope, navigation, keyboard controls and narrow layouts. Fresh, managed-update and ejected installs also pass their real installer tests. Final repository and PR evidence belongs to the owning effort's integration notes.

The sections below preserve the original test-first harness evidence at base e7b9c5e9386b76e9c5ba46c0c88cd6892c6146f5, before the Delivery pages existed. They are historical setup results, not current limitations.

## Historical missing-experience red

Run from `web` with local runtime/socket permissions:

```sh
RHIZOME_E2E_SKIP_WEB_BUILD=1 npm run test:e2e:agentic-engineering -- --project=mixed --grep 'Delivery mounts|scratch install' --max-failures=1
```

The web build had already passed during the initial harness run. Both mixed and empty scratch repositories installed, indexed and served. The first selected browser test failed on its enabled assertion for the “Shaping specs” link, with the normal 5000ms expectation timeout. The screenshot shows the existing native Delivery Briefing and the synthetic fixture records. This is the intended missing-experience red. The maximum-failure bound left the other two selected tests unrun; it does not mark them as passing or skipped in source.

Failure artifacts were preserved outside the output directory at `/private/tmp/ae-harness-proof/expected-red/` because a subsequent Playwright run cleans its result directory. The directory contains `test-failed-1.png`, `error-context.md`, and `trace.zip`. The screenshot was opened and inspected. It is a baseline of the existing native page, not an accepted Delivery screenshot. Successful integrated captures will populate the JSONL manifest described in README.md.

Two earlier setup failures were diagnosed before the expected red. The harness initially appended a duplicate notes config block; that write was deleted because the installed AE starter already supplies HTML note includes. The next run reached indexing but sandboxed macOS denied kern.boottime. The same corrected command then ran with runtime and loopback access. No timeout, retry, or skip was added to get past either failure.

## Initial harness limitations

All final page assertions remain enabled for the UI owner's implementation. Accessible naming and the story parameter value are explicit assumptions in README.md and may need coordinated adjustment against the integrated UI. The A/B/C fixture is authoritative on scope semantics. The base ontology lacks frozenScope, so this lane does not claim the new selector resolves before the foundation integration.

The enabled assertions cover mounted groups and records, all stage routes, prompt selection and clipboard parity, inline long criteria, source boundary cases, panel Find and Recent, direct links, reload, browser Back/Forward and a second browser tab, keyboard activation, narrow viewports and a real aborted transport. The fixture supplies rich source cases that still need deeper final integrated assertions. Additional enabled assertions cover host workbench tab switching, a held real query for loading, and a reduced viewport equivalent to a 1440px display at 200 percent zoom. They have not run against the integrated pages. The suite does not yet prove selection refresh/removal, retained-data or changed-generation UI states, actual browser zoom, screen-reader reading order or AA contrast. Screenshots cannot establish accessibility. Fresh/update/eject lifecycle proofs belong to the packaging and final integration lanes.

## Initial harness gate results

- `npm ci` in web passed with the pinned Node toolchain.
- `node --check web/scripts/run-agentic-engineering-e2e-server.mjs` passed.
- `npm run lint` in web passed, including the final server cache-directory initialization.
- The Playwright config passed an explicit TypeScript check using a temporary config extending web/tsconfig.node.json and including playwright.agentic-engineering.config.ts. The repository's node tsconfig does not yet include this new config by name. Its source was not changed outside this lane.
- `npx playwright test -c playwright.agentic-engineering.config.ts --list` discovered 43 enabled tests across three files.
- `PATH="/private/tmp/ae-gate-tools:$PATH" make check-fast` passed. An initial run found one missing blank line in the new loading test. That was fixed before the successful run.
- `PATH="/private/tmp/ae-gate-tools:$PATH" make check` passed. Web unit results were 269 files and 2156 tests passed. Go, content, team API, and credential checks also passed. The wrapper only queues golangci-lint with its supported allow-serial-runners flag, as supplied by root.
- The real install smoke command `RHIZOME_E2E_SKIP_WEB_BUILD=1 npm --prefix web run test:e2e:agentic-engineering -- --project=mixed --grep 'scratch install'` passed. It proves 12 efforts, correct active status, and actual HTML workspace projection through the public API.
- Cleanup verification initially found four scratch directories left by Playwright's default SIGKILL behavior. No fixture processes remained. The config now requests graceful SIGTERM shutdown. A fresh install smoke passed after that fix, and a filesystem assertion confirmed zero remaining AE scratch directories. Earlier inactive scratch directories were removed explicitly.

Logs are `/private/tmp/ae-harness-check-fast.log`, `/private/tmp/ae-harness-check.log`, and `/private/tmp/ae-harness-cleanup-smoke.log`. Baseline screenshot inventory is `/private/tmp/ae-harness-proof/manifest.json`. Compounding retrieval was degraded by the unavailable Voyage endpoint and provided no matching shards. Direct committed source and contract reads supplied the implementation context.

The final install smoke passed again after cache-directory initialization, with zero AE scratch directories remaining. Its log is `/private/tmp/ae-harness-final-smoke.log`. No timeout, retry, or skip changed between proofs.
