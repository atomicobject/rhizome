# Agentic Engineering browser fixtures

These synthetic repositories are copied into temporary Git vaults. The harness installs the actual starter with `init --workflow agentic-engineering --agents claude,codex --search off --binary-manager external`, indexes, and serves them using a branch-built `fts5 e2efake` binary. It never registers a live vault or modifies the user's repository trust store. Both signal shutdown and normal exit stop the runtime and remove scratch files.

Run from `web` with `npm run test:e2e:agentic-engineering`. The targeted first proof is `npm run test:e2e:agentic-engineering -- --project=mixed --grep 'Delivery mounts' --max-failures=1`. No retries or expected-failure annotations are configured. `make web-e2e`, and therefore `make verify`, includes this suite. `make check` does not run it.

## Records and expectations

`empty` contains only a README before installation. It has no authored spec or effort. Its home retains the workflow map and accepts an outcome for a complete specify prompt. Its Planned list explains selection and effort creation.

`mixed/docs/specs/experience/harbor-booking.md` is active SPEC-9001. Its authored stories are A/US1 ready, B/US2 draft, C/US3 ready, D/US4 satisfied. It belongs to Shaping, Ready and Current contracts simultaneously. Long criteria include nested examples, code, a verification hint and the terminal phrase `harbor confirmation ends here`. The direct criterion locator is `#^ae-a-confirmation`. Other records cover proposed ProductSpec, superseded TechnicalSpec with successor, archived OperationsSpec, and active ProcessSpec.

Inline efforts under `mixed/docs/efforts/2026-11-09-10-*.md` cover planned, active with every checkbox checked, complete with only satisfied D selected, and archived. A complete effort's unrelated ready A and C remain ready without a confirmed delivery-review finding. No fixture has plan-approved-by metadata. The textual absence of an approver is not a judgment that the plan lacks authority.

Workspace entries are under `mixed/docs/efforts/harbor-<variant>/2026-11-09-11-<minute>-effort.html`.

| Variant | Minute | Source contract |
| --- | --- | --- |
| explicit | 00 | frozen-scope explicitly selects materials/frozen.md with A twice, followed by an unresolved authored occurrence. Entry mentions B. Different work-log.md selects C. Governing metadata selects SPEC-9005 while frozen spec section selects SPEC-9001. |
| criterion-only | 01 | Selected material cites only A's criterion, never the whole story. |
| legacy | 02 | frozen-scope absent. Log C is a snapshot, authoritative membership unavailable. |
| broken | 03 | Selector retains authored materials/missing.md reference. The implementation-plan reference is also broken and must retain missing-plan.html. |
| unsupported | 04 | Selector names HTML plan.html rather than a supported Markdown frozen section source. |
| duplicate | 05 | Selected Markdown material repeats both frozen headings with a different story. Membership is incomplete. |
| same-source | 06 | frozen-scope and work-log select the same canonical log. One source retains both roles. |
| missing-sections | 07 | Selected Markdown material lacks both frozen headings. Membership is unknown. |

The planned API boundary proofs at 101/201 belong to the query lane. These UI fixtures deliberately keep record counts small.

## Routes and UI integration

Home is `/notes/group/Delivery`. Lists use `/notes?view=agentic-engineering.stage&stage=<value>` with shaping, ready, planned, active, history, contracts, review. The standalone definition ID remains agentic-engineering.stage. Unknown stage values must show an invalid route. Record tests use canonical `/notes?note=<path>` so the actual default node mount is exercised, rather than forcing a presentation. Story deep links add `story=SPEC-9001-US1`.

The required default home is agentic-engineering.home; panel is agentic-engineering.panel. The five concrete spec mounts and two effort mounts are defined in foundation-contract.md. The existing spec-driven.specs, spec-driven.efforts, spec-driven.user-stories, spec-driven.feature-areas must remain alternate views.

Selectors use human headings, labeled checkboxes, prompt textboxes, Find, Recent, and the existing groupViewHelpers frame locator. Integration may adjust wording and accessible names while retaining the assertions' meaning. Prompt preview is assumed to be a labeled readonly textbox with its value equal to the clipboard. Story controls are assumed to be named buttons. Stage navigation remains in the host workbench. No UI implementation, registration, or fallback fake page is included here.

Successful page captures write an append-only `web/test-results/agentic-engineering/screenshots.jsonl` manifest. Each entry names its scenario, fixture project, theme, viewport, URL and absolute PNG path. Clean that generated output directory before a fresh proof pass. Automatic failure screenshots and traces are attached in Playwright's results alongside error-context.md. Screenshots show appearance, not accessibility compliance. Browser tests separately exercise focus, keyboard activation and overflow, but do not constitute a screen-reader or contrast audit.

This is test-first scaffolding. The base commit has no new Delivery pages. A missing-experience failure is expected and remains a real failure until integration. Do not claim installed runtime acceptance from test discovery, static checks, an expected red, or a screenshot of the old native page.
