# Agentic Engineering delivery experience

This is an untyped authoring draft, not an allocated ExperienceSpec. Promotion target is `docs/specs/experience/agentic-engineering-delivery-experience.md`. Live authoring-guide and identifier allocation are blocked by runtime projection publication. Do not use candidate SPEC-0137 as an allocation. On promotion, load the live ExperienceSpec guide, allocate its identifier, add required frontmatter, and derive story identifiers from that allocation. The story headings below are stable draft names, not minted locators.

## Summary

Give designers and leads a clear view of intended behavior, selectable work, and delivery evidence. Give developers and agents an exact brief they can resume without reconstructing intent. The Agentic Engineering starter installs a Delivery experience comparable in completeness to Knowledge, using the existing experience host, node workspaces, view kit, and contextual panel.

The workflow map is the organizing picture across home, lists, spec pages, effort pages, and the panel. Shape, Build, and Settle explain activities. Lists show recorded facts and may overlap. The experience copies prompts for the installed skills. It does not start agents, grant approval, change lifecycle states, or decide that delivery is accepted.

## Goals

- Help a newcomer choose a useful next action from current records or a first-day empty state.
- Let a lead select exact stories or efforts and copy one composed prompt with reviewable scope.
- Let a developer read full acceptance criteria and both effort forms' frozen scope, plans, status, and closure evidence.
- Preserve source identity, occurrence order, recorded state, missing evidence, and read coverage through every count and link.
- Ship a working installed experience for fresh, updated, and ejected starters.

## Non-Goals

Agent invocation, automatic approval or acceptance, lifecycle writes, a policy browser, a reference-document site, inferred reconciliation judgments, a new ontology lifecycle, a shared site framework, and unrelated platform refactoring are excluded. The four existing native collection IDs remain alternate views. The prototype's filesystem parser and hardcoded sample records are design evidence only.

## User Stories

### US1 - Understand current delivery work

Draft summary. A designer or lead opens Delivery and understands what is recorded and where to act.
Draft status. Ready for plan review, not an authored UserStory status field.

#### Acceptance Criteria

- Delivery opens a custom home page with the repository context, a visible Shape, Build, Settle map, five primary groups, and separate current-contract and record-review navigation.
- Each map group is an actionable link with a nearby rule and count unit. Rules are available by keyboard and touch as well as hover. The map explains that lists overlap and that records do not all move through one sequence.
- Shaping specs includes every proposed spec and every active spec with at least one draft story. Ready stories includes every ready story under proposed or active specs, grouped by spec. An active spec with draft and ready stories appears in both groups.
- Planned efforts includes effort status planned. Active efforts includes effort status active, regardless of checklist state. Delivery history includes complete and archived efforts with those states visibly distinct. Counts name specs, stories, or efforts explicitly. Ready counts show both stories and parent specs.
- Current contracts lists every active spec independently of stories and efforts. Its explanation says active means the normative contract is in force. It makes no delivered or no-work-remaining claim.
- Home gives bounded previews with exact shown and available counts and working links to complete lists. Unknown or partial totals are visibly qualified. Totals from overlapping groups are never summed into a portfolio total.
- A genuinely empty portfolio retains the map and provides an editable outcome field with one complete specify prompt. An empty Planned list explains how to select stories and create an effort. Neither state invents an Approve stage.

### US2 - Select exact work and copy one prompt

Draft summary. A lead selects work in a factual list and copies one prompt that preserves scope and authority.

#### Acceptance Criteria

- Every primary group opens a list with its rule, count unit, sort, selection controls, full-list navigation, and composed prompt preview. No rows are selected on first entry.
- Ready stories can be selected individually and by parent spec. Parent checkboxes reflect all, none, and mixed selection. Selecting a spec selects only its visible eligible ready stories, with the number stated. Unresolved or nonlinkable targets remain visible and cannot silently become prompt scope.
- Selection survives ordinary disclosure and paging within the same stage. Changing stage clears selection. A refreshed dataset retains still-valid selected identities and reports removals or changed eligibility before copying.
- One prompt includes all selected records once in displayed spec and story order, with exact canonical paths and durable story locators. It does not create one unrelated prompt per row or silently select every story in a spec.
- Ready prompts use the agentic-engineering effort and plan routes, ask the agent to evaluate the proposed slice and existing frozen selections, and preserve the user's actual authority and team policy. Selection is proposed scope until frozen by the workflow. A proposed parent spec is identified as proposed and must be settled before freezing a delivery contract.
- Shaping prompts use specify. Planned and active effort prompts use classify or implement as appropriate to inspect authority and resume context. History prompts use finish or reconcile to inspect recorded evidence. Prompts never infer missing approval or instruct unconditional semantic changes.
- The visible preview and clipboard contents match. Copy is disabled with no eligible selection or incomplete selected scope. Clipboard failure leaves selectable prompt text and an announced error. Success is announced only after the clipboard write succeeds.

### US3 - Read the complete spec contract

Draft summary. A designer or developer opens a spec and reads the contract and its exact delivery relationships.

#### Acceptance Criteria

- The spec page shows its title, identifier, concrete type, source path, recorded status, summary, last-updated value when present, and workflow memberships without assigning one exclusive stage.
- Stories retain authored order and their draft, ready, or satisfied statuses. Each disclosure opens the full criterion text and detail inline, including nested lists, code, and verification hints. A deep link to a story opens and focuses the correct disclosure.
- Criteria counts reflect typed direct criterion nodes rather than lines or nested bullets. Missing stories, absent criteria, empty criteria, and failed reads have distinct text. Requirements and other normative narrative remain readable through the existing node workspace rendering.
- Effort relationships state their provenance separately as governing-specs metadata, Spec Set (Frozen), and exact Stories In Scope (Frozen) selection. A general inbound story link is labeled as a mention and never presented as frozen selection.
- A complete effort and a ready story can coexist without a warning that the story is stale. Historical delivery context never removes the story from Ready. Superseded and archived specs remain reachable through find and native views, with their recorded status and successor when present.

### US4 - Resume either effort form from its real brief

Draft summary. A developer or agent opens an effort and can determine scope, current evidence, and the next investigation.

#### Acceptance Criteria

- Shared identity and status come from Effort. EffortNote reads its declared inline sections. EffortWorkspace reads its explicit implementationPlan, workLog, and materials references. Nearby files, folder names, and backlinks do not establish component membership.
- The resume brief shows recorded Status prose and Execution Notes with source links and authored order. A bounded preview offers the full section. It labels entries by their recorded date only when structured evidence supports that label. It does not invent a latest event by searching arbitrary prose for dates.
- The brief preserves separate governingSpecs metadata and frozen spec-section occurrences. Frozen story and criterion selections come only from the focused frozen section's sourceLinks. Repeated occurrences, source ordering, unresolved links, and their authored text stay available.
- The selected contract expands full story criteria and selected criterion details inline. A criterion-only selection remains criterion-only. A whole-spec selection shows that scope explicitly and links the full contract. Missing selection is unknown scope, not a claim of no obligations.
- The page exposes the inline or linked plan, Original Intended Delivery, Actual Delivered, Deviations, Closure Checklist, and supporting materials. HTML plans use the supported safe rendering route. Broken or unsupported components show the exact reference and a readable failure state.
- Checkbox progress is labeled as recorded checklist items checked, with the denominator and missing or empty states. All boxes checked on an active effort leaves it active. There is no acceptance-ready badge, automatic acceptance action, or inferred approval judgment.
- Copy resume prompt contains the exact effort entry and explicit plan and log references when applicable. It asks the skill to inspect the recorded status, frozen scope, execution notes, deviations, and authority before continuing. The first unchecked closure item is optional recorded context, never asserted to be the actual next implementation task.

### US5 - Navigate with a contextual panel and find

Draft summary. A user keeps their place while moving among the workflow, contracts, and efforts.

#### Acceptance Criteria

- The Delivery panel mounts through the existing experience panel host. It shows the same map groups and counts as the page, the current location, and links to home, current contracts, evidence review, and native alternate views.
- Spec and effort pages retain a compact version of the map with text explaining all applicable memberships. The location trail follows navigation context and never fabricates a single parent stage for an overlapping spec.
- Find searches loaded spec and effort titles, identifiers, and paths with deterministic ranking. It distinguishes no match from incomplete inventory. Results navigate through canonical node refs to the correct record page.
- Back, forward, direct links, refresh, and workbench tabs retain route parameters and focused story targets. Recent entries record actual visits per repository and window and contain no hardcoded demo names.
- Panel minimize, expand, resize, and narrow-screen disclosure reuse host behavior. Keyboard controls remain reachable, focus returns on dismissal, and the panel cannot cover the only path back to the page.

### US6 - Distinguish pending work from record review

Draft summary. A lead sees actionable recorded work separately from evidence that needs inspection.

#### Acceptance Criteria

- Proposed specs, draft or ready stories, and planned or active efforts are recorded work. Record review is a separately labeled list of observations with rules, exact sources, scope, and completeness. Neither list claims the other is an error queue.
- An active effort with a nonempty fully checked closure checklist appears as a record-review observation with its actual status. The suggested action is inspect closure evidence. An absent or empty checklist does not satisfy that rule.
- A complete effort with remaining ready or draft stories appears only as inspect delivery evidence when exact frozen story or criterion selection is proven. General governing-spec links or inbound UserStory.efforts cannot qualify it. The observation states the coexisting facts and does not call them stale, unreconciled, or erroneous.
- Unknown selections, failed component reads, governing-versus-frozen disagreement, and unresolved links show source-specific evidence gaps. Unknown cases never count as confirmed pending implementation or confirmed maintenance defects.
- Evidence-review prompts ask the finish or reconcile skill to compare actual delivery, deviations, and the exact selected contract. They propose investigation rather than acceptance, state edits, or a forced reconciliation conclusion.

### US7 - Use the experience under incomplete and constrained conditions

Draft summary. Users can read and act accurately across supported themes, screen sizes, and read failures.

#### Acceptance Criteria

- Every page and the panel distinguish initial loading, complete empty, transport error, retained prior data, partial data, missing node, unresolved target, and invalid route. Failed or partial reads never display a reassuring zero or everything-complete claim.
- Long titles, paths, narratives, criteria, and lists remain accessible with wrapping and bounded disclosures. Any preview states omissions. Full content is reachable without silent character truncation.
- Light and dark themes use house typography and tokens. Body text stays near the comps' 14px density, record titles remain ink colored, and blue Shape, orange Build, and magenta Settle encode activity orientation rather than fabricated lifecycle completion. Text and icons carry the same meaning without color.
- At 1440px the home retains the comps' calm two-column composition. At 768px and 390px groups stack, prompt controls stay reachable, and criteria reflow. At 200 percent zoom there is no page-level horizontal overflow or covered control. Necessary wide tables may scroll within their labeled region.
- Every disclosure, checkbox, map link, find result, copy control, and panel control works by keyboard with a visible focus indicator and accessible name. Selection groups expose mixed state, current links expose current state, status announcements use a live region, and no essential rule requires hover.
- Contrast meets WCAG AA for text and controls in both themes. Reduced motion is honored. Screen-reader reading order matches visible order, and focus remains stable after paging, disclosure, copy, and route navigation.

### US8 - Install and maintain the actual starter experience

Draft summary. A repository receives a complete working experience through the supported starter lifecycle.

#### Acceptance Criteria

- A fresh agentic-engineering install includes the experience manifest, page and record mounts, panel, CSS, and shared starter logic from template sources. Delivery home is the group default. The four native IDs spec-driven.specs, spec-driven.efforts, spec-driven.user-stories, and spec-driven.feature-areas remain reachable as alternate views.
- Updating an existing install makes the same experience available without changing authored records, team policy, disabled guidance, or unrelated view overrides. Starter-owned updates follow the supported ownership mechanism.
- A deliberate local eject retains a self-contained usable experience. A later update preserves supported ejected overrides and reports ownership conflicts rather than overwriting them.
- Fresh, update, check, and eject verification runs in disposable repositories using the branch-built absolute executable with repository delegation disabled. The installed pages and copied prompts are exercised in the real interface. No generated skill copies in this checkout or Rhizome Dev installation are used as substitutes.

## Requirements

MUST use existing experience hosting, view navigation, source-preserving node workspace rendering, and kit read primitives. MUST keep domain rules in one starter-owned model used by home, stages, records, panel, counts, and prompts.

MUST paginate typed-root inventories until extensions.typedRoots reports complete coverage. MUST retain errors, truncation, generation, and unknown outcomes. Selected node hydration MUST use readNodes batches of at most 200 and preserve unresolved, errors, truncated, and complete.

MUST read frozen scope from focused section workspace.sourceLinks and resolved refs. The existing first-100 response cannot prove completeness at 100. Add only the minimal shared offset and traversal-coverage support necessary for complete source-link reads. MUST NOT implement a client Markdown or YAML parser, infer scope from UserStory.efforts, union governing and frozen links into authority, or infer readiness from link absence.

MUST retain authored occurrence order before producing any deduplicated display summary. MUST label the provenance of every relation and the unit and scope of every count. Missing approval metadata means no approver recorded. It does not mean unapproved. Missing dates never become filesystem modification dates.

MUST keep the workflow map visible across all five page families. It is an activity map with independently selectable factual groups, not a state machine for the portfolio. Approval and acceptance remain decisions owned by the user's workflow and policy.

## Open Questions

No discretionary product decision is deferred. The data foundation needs independent review before dependent implementation. Live authoring and ID allocation are blocked and must succeed before this draft becomes a typed ExperienceSpec. The offset extension's exact coverage schema is an implementation foundation decision governed by the plan, not an excuse to hide partial reads.

## Documentation plan

Document grouping rules, counts, provenance, prompt behavior, and lifecycle ownership in starter-owned explanatory material next to the implementation. Update the GraphQL subsystem note and contract when sourceLinks pagination changes. Add a user-facing changelog entry. Preserve this cohesive contract during implementation and record actual deviations on the effort.
