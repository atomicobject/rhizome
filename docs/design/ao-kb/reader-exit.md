# SPEC-0133 structured record reader polish

Lane R merges coordinator integration through `32c66e01b` (runtime code through `6b6044f07`), including Lane P's read fixes and Lane Y3's review fixes (materialization 23). All delivery commits are local.

## Reader behavior

| Problem | Change and verification |
| --- | --- |
| Minified record value | Native property tree preserves maps, lists, authored order, null, empty strings/lists/maps, integer keys, literal text, and exact large numbers. Exact bound scalars and fully represented child collections wait behind All record properties; unbound values remain visible. Unit and YAML/JSON browser tests cover the distinctions. |
| Repeated child key/title | Native child rows omit a key equal to the display title and retain distinct keys. The collection unit test covers both cases; the browser confirms scalar job titles appear once. |
| Host filename in tabs | Locator-only embedded record tabs use their display title; host tabs retain the host title. Unit and browser Back/Forward checks cover navigation. Record URLs preserve their provider locator ahead of structural fingerprints. |
| Edit on read-only records | The shell honors native workspace capabilities, and the reader preserves the read-only reason even during global editing. Component and browser checks cover the toolbar and content. |
| Empty Info relationships | Existing relation components now receive prepared parent/child containment, authored references in both directions, and exact-record Markdown backlinks. Shared read-path tests prove record identity, direction, ambiguity refusal, and zero source reads for summary hydration; browser checks exercise the rail. |
| Generic owner banner | Native read-only reasons include the declared owner unless already named, with a generic fallback when no declaration exists. Ownership unit tests and browser checks cover both forms. |
| Silent child undercount | A 201-child collection with 199 hydrated child bodies reports 201 and explicitly says 2 more not loaded, directing readers to Source. Filtering states that it covers loaded items only. Unit and browser regressions cover the cap. |

Collections lead the additional source properties. Record titles stay ink, child titles use weight 500, body values use 14px type, the reading column stays centered and capped, and long values wrap. Nested lists and deeper maps disclose their contents. Native toolbars grow when they wrap in a narrow pane with the left sidebar open; a browser regression first proved 20px of clipping and then verifies that Copy link fits inside the toolbar. Complete source properties remain available on demand because typed fields can omit source type, emptiness, aliases, or repeated reference occurrences; Source still shows the complete original host bytes.

## Shared read changes

No database migration, GraphQL schema field, or write API was added. The existing native source JSON payload gains optional `scalarText` for numeric values and `keyText` for integer keys, so browsers preserve exact integers beyond JavaScript's safe range. The existing capability reason includes the owner's name for native records.

The shared RECORD relation read adds containment and reuses the provider-authored fragment backlink index. Fragmentless RECORD endpoints resolve source-local IDs through a batched catalog lookup because provider IDs and hashed catalog IDs differ. Markdown hydration and presentation keep their existing paths. Lane R did not edit Lane L4 starter files or Lane Y3 owned files.

## Verification and screenshots

The final gates run after merging coordinator code through `6b6044f07`. The Markdown comparison serves frontend assets preserved from `6fc94d570` and the final frontend against the same disposable current backend and fixture, then compares the PNG bytes. The Batch 3 evidence is unchanged.


| Gate | Result | Evidence |
| --- | --- | --- |
| Focused component tests | 7 files, 65 tests passed | `/private/tmp/lane-r-component-tests.log` |
| `make check-fast` | Passed | `/private/tmp/lane-r-final-check-fast.log` |
| `make check` | Passed, including Go packages and all 248 web files / 2,010 tests | `/private/tmp/lane-r-final-check.log` |
| `RHIZOME_E2E_PORT=7941 make web-e2e` | Passed: 132 workspace + 25 AO KB journeys; all three record journeys passed | `/private/tmp/lane-r-final-web-e2e.log` |
| `make build` | Passed | `/private/tmp/lane-r-final-build.log` |
| `./scripts/rzm validate` | Exit 1: five existing broken-link findings, zero errors, none in changed files | `/private/tmp/lane-r-final-validate.log` |
| Impeccable detector | Zero findings | Manual refinement check |
| Markdown screenshot comparison | Both pairs byte-identical | `/private/tmp/lane-r-final-visual-compare.log` |

The five validation findings are in `docs/reviews/2026-10-17-subsystem-review/delivery-evidence.md` and `implementation-status.md`, targeting `.golangci-gate.yml` and `.github/workflows/ci.yml`. Both documents are unchanged from baseline `6fc94d570`; they were left outside this reader task.

All 14 PNGs were opened at original size and inspected. Live checks also confirmed long-value wrapping and the narrow Info overlay. The wrapped toolbar is now 69px high and contains Copy link completely. Markdown pair SHA-256 values are identical before and after:

- Light: `8b97cc173b6a23ab46f5bd6bf03e38f8d851dc0c88d470707ff2bf65d7ff8c8f`
- Dark: `19b9a3661007168302aa54e19a862ce3704e7fd6af0308ad249d8caa44e416fe`

| Format | 1280 light | 1280 dark | 728 light | 728 dark |
| --- | --- | --- | --- | --- |
| YAML | [Screen](reader-screens/yaml-record-1280-light.png) | [Screen](reader-screens/yaml-record-1280-dark.png) | [Screen](reader-screens/yaml-record-728-light.png) | [Screen](reader-screens/yaml-record-728-dark.png) |
| JSON | [Screen](reader-screens/json-record-1280-light.png) | [Screen](reader-screens/json-record-1280-dark.png) | [Screen](reader-screens/json-record-728-light.png) | [Screen](reader-screens/json-record-728-dark.png) |

Sidebar-open narrow layout: [light](reader-screens/yaml-record-728-sidebar-light.png), [dark](reader-screens/yaml-record-728-sidebar-dark.png).

Markdown comparisons: [light before](reader-screens/markdown-before-light.png), [light after](reader-screens/markdown-after-light.png), [dark before](reader-screens/markdown-before-dark.png), [dark after](reader-screens/markdown-after-dark.png).

Two full-suite timing failures were diagnosed. The CRLF source-focus test now preloads its lazy editor before asserting focus. The dense Trace cap test counts rendered record rows directly instead of rebuilding every row title through accessibility queries; profiling reduced each count scan from 65–70 ms to about 1 ms. Both preserve the existing assertions and timeouts, with no retries or skips. Neither changes Markdown runtime behavior.

## Deliberate limits

Child bodies retain the existing 200-body workspace cap (including the parent); the reader makes unloaded children explicit rather than adding a new pagination API. No source-writing feature, custom AO view, or new dependency was added. No push, PR, merge to the coordinator, deployment, or desktop installation was performed.

Impeccable reported a stale design sidecar; DESIGN.md and the supplied design notes guided this refinement. The sidecar was left untouched as the skill requires; `impeccable document` can refresh it separately.

The fixture servers on 7941 and 7942 were stopped, and the owned preview tab was closed. All named synthetic E2E/visual fixtures, baseline frontend assets, capture scripts, API scratch data, test output, and empty runner scratch were removed with chmod followed by removal. Logs and checked-in evidence remain. The generated dist index was restored.
