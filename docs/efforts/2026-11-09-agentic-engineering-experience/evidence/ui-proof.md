# Installed Delivery proof

These are screenshots of the real installed starter in disposable synthetic repositories. The fixtures deliberately include incomplete records and unresolved links, which also account for their host Problems count.

The current manifest has 68 captures and 44 committed images. Fable 5.2 opened all 63 images in the initial cleanup manifest and all 31 selected before images individually at original resolution, then approved the visual cleanup and final eligibility and identity fixes. Root inspected the five added scenarios, both corrected singular-label captures, the complete home, and the settled first-day panel. The [review record](fable52-final-review.md) separates screenshot findings from behavior tested in the browser.

| Use case | Light | Dark |
| --- | --- | --- |
| Lead or designer chooses the next slice | [Home](screenshots/home-light.png) | [Home](screenshots/home-dark.png) |
| Select exact stories and copy one prompt | [Selected stories](screenshots/ready-selected-light.png) | [Selected stories](screenshots/ready-selected-dark.png) |
| Continue healthy work when another criterion read fails | [Partial read](screenshots/healthy-story-partial-unresolved-light.png) | [Partial read](screenshots/healthy-story-partial-unresolved-dark.png) |
| Read full criteria in their spec | [Spec](screenshots/spec-expanded-light.png) | [Spec](screenshots/spec-expanded-dark.png) |
| Inspect frozen specs separately from other authored links | [Relationships](screenshots/frozen-spec-relationships-light.png) | [Relationships](screenshots/frozen-spec-relationships-dark.png) |
| Developer resumes an inline effort | [Brief](screenshots/inline-effort-light.png) | [Brief](screenshots/inline-effort-dark.png) |
| Inspect explicit workspace scope and separate log evidence | [Workspace](screenshots/workspace-provenance-light.png) | [Workspace](screenshots/workspace-provenance-dark.png) |
| Keep unknown legacy scope visible | [Legacy workspace](screenshots/legacy-workspace-light.png) | [Legacy workspace](screenshots/legacy-workspace-dark.png) |
| Find a contract and return through actual visits | [Panel](screenshots/panel-find-recent-light.png) | [Panel](screenshots/panel-find-recent-dark.png) |
| Start with no work recorded | [First day](screenshots/empty-home-light.png) | [First day](screenshots/empty-home-dark.png) |
| Read a narrow home | [390px](screenshots/home-narrow-390-light.png) | [390px](screenshots/home-narrow-390-dark.png) |
| Select work in a narrow stage | [390px](screenshots/ready-narrow-390-light.png) | [390px](screenshots/ready-narrow-390-dark.png) |
| Read long criteria at narrow width | [390px](screenshots/narrow-390-light.png) | [390px](screenshots/narrow-390-dark.png) |
| Recover from a failed refresh without losing the brief | [Retained error](screenshots/retained-error-light.png) | [Retained error](screenshots/retained-error-dark.png) |
| Inspect a partial-read error without losing the page | [Partial error](screenshots/partial-read-error-light.png) | [Partial error](screenshots/partial-read-error-dark.png) |
| Read an image source when inline previews are unavailable | [Image fallback](screenshots/relative-image-fallback-light.png) | [Image fallback](screenshots/relative-image-fallback-dark.png) |
| Recognize loading without false empty counts | [Loading](screenshots/loading-light.png) | [Loading](screenshots/loading-dark.png) |

The additional default-theme captures show [sorted selected rows](screenshots/sorted-ready-selection-default.png), [exact pending review scope](screenshots/pending-review-scope-default.png), [active panel location](screenshots/panel-active-stage-default.png), and [changed selection](screenshots/changed-selection-default.png). Changed selection requires acknowledgement before a reduced prompt can be copied. The panel test checks that Find text survives a stage change.

All 77 installed Delivery cases pass, including source-link navigation, clipboard success and failure, incomplete scope, exact pending locators, sorting, Back/Forward, reload, tab return, a second window, keyboard/focus and 390px/768px overflow. Nominal home captures require complete reads; deliberate fault captures retain their partial notices. The 200-percent layout case uses a 720px viewport equivalent to a 1440px display rather than browser zoom hardware.

Full `make verify` passed with 2,169 web unit, 159 native browser, 90 Knowledge browser and 77 Delivery browser cases. The final singular-label and capture corrections then passed `make check`, `make build`, strict template TypeScript, the complete installed Delivery suite and both documentation validators. Fresh/update/eject and `init --check` are included in the installer checks. Earlier capture inventories remain historical evidence in the dated review records.

These screenshots do not establish a formal screen-reader or contrast audit. Source-relative and repository-root images retain their label, path and canonical source button because the kit has no public binary attachment route. Absolute and protocol-relative external image URLs still render; both cases are decoded in the browser regression. Authored HTML uses its own appearance inside the isolated viewer, including a white document in a dark host. No unmerged build was installed into Rhizome Dev.
