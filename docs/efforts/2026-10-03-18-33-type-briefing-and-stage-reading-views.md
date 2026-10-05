---
type: EffortNote
id: EFF-2026-10-03-18-33
aliases: [EFF-2026-10-03-18-33]
name: Type Briefing and stage-reading views
created-at: 2026-10-03T22:34:34Z
status: active
summary: "Replace Overview with a type Briefing built on the bundled-view platform, and make the group views and the kit read lifecycle stages and type profiles from the server instead of deriving them from tones and field names."
---

# Type Briefing and stage-reading views

## Scope

Deliver the "Briefing (delivered after SPEC-0111 lands)" section of [[type-collection-views|SPEC-0112]] and the revised shared-derivation requirements of [[group-views-and-view-platform|SPEC-0111]]:

1. A type Briefing for every type and interface, as a bundled kit view mounted on `type: "*"` and `interface: "*"`, sharing modules with the group Briefing, and replacing Overview as a type and interface choice.
2. The bundled group views (Briefing, Trace, Sections) read each member's lifecycle field, value stages, and gap fields from the type profile and enum value stages in type documentation; their tone- and name-based derivation in `web/bundled-views/group/model.ts` is removed.
3. The kit exposes the type profile through `useTypeDocs` so repository views can do the same.

Excluded: git commit times as change times, stage suggestions for Drew's vault, CLI board flags, and the remaining follow-ups of EFF-2026-10-03-09-32.

## Spec Set (Frozen)

- [[type-collection-views|SPEC-0112]], section "Briefing (delivered after SPEC-0111 lands)".
- [[group-views-and-view-platform|SPEC-0111]], sections "Shared derivations" and "Briefing" as revised on 2026-10-03 by this effort to read the profile and stages.

## Stories In Scope (Frozen)

Requirements-only delivery of the sections above.

## Spec Coverage Checklist

- [x] Type Briefing sections: needs attention, in motion, recent changes with bursts collapsed, distributions per lifecycle, ordered, and category field, primary-date months, connections per relation and reverse field, notes that link in, and the companion guide.
- [x] Type Briefing mounted for every type and interface; Overview no longer a type or interface choice; the group Briefing and type Briefing share modules.
- [x] Group Briefing, Trace, and Sections read lifecycle, stages, terminal values, in-motion values, and gap fields from type documentation; name and tone heuristics removed.
- [x] Kit `useTypeDocs` carries the profile; kit API reference updated.
- [x] Web unit tests on synthetic fixtures for both Briefings and the derivation change; browser tests for the type Briefing and a group view over a declared-stage enum.
- [ ] Gates, independent review, pull request.

## Plan

### Current gap

Overview is still the type and interface fallback choice. The bundled group views choose a member's lifecycle from tone and order metadata and field names ending in `status` or `stage`, treat `success`, `muted`, and collapsed values as terminal, and recompute KEY-field gaps, while type documentation already reports `profile` (lifecycle field, gap fields, shape) and each enum value's `stage`. The kit parses stages but not the profile.

### Batches

| Batch | Outcome | Owner and scope | Exit evidence |
| --- | --- | --- | --- |
| 1. Profile in the kit and group views | The kit parses `profile`; group Briefing, Trace, and Sections take lifecycle, stages, terminal and in-motion values, and gap fields from it; the old derivation is deleted. | Opus 5.5 high worker: `web/kit/schemaParse.ts`, kit types and reference, `web/bundled-views/group/*`. | Vitest with synthetic fixtures (declared stages, inferred stages, no lifecycle), `make check`, `make web-e2e`. |
| 2. Type Briefing | A bundled `type.briefing` view on `type: "*"` and `interface: "*"` sharing the group Briefing's modules; Overview removed from type and interface choices; catalog and default rules unchanged otherwise. | Opus 5.5 xhigh worker: `web/bundled-views/type/` (new), shared modules extracted from `web/bundled-views/group/`, `pkg/app/views` catalog wiring for the bundled type mount and Overview removal. Depends on batch 1's shared readers. | Go tests for catalog choices, Vitest for each Briefing section, browser test on the type-views fixtures, screenshots of IP opportunities and Specs Briefings. |
| 3. Review and PR | Independent Opus 5.5 review, fixes, documentation (views skill reference, views CONTEXT, kit reference, CHANGELOG), one pull request looped to Greptile 5/5. | Parent. | Gates green, review findings resolved, PR open. |

### Decisions and escalation

Settled: the Briefing's sections are those SPEC-0112 lists; the type Briefing uses the bundled-view platform rather than a React built-in; Overview stays available for display groups as SPEC-0111 requires. Escalate only for a change to those, or if removing Overview from types breaks a remembered choice in a way that needs a migration.

### Authorization

Drew approved this plan in chat on 2026-10-03 ("y", in reply to the request to approve the three-batch plan). This covers implementation, review fixes, and opening the pull request, not merging. No current user is configured in this checkout, so `plan-approved-by` is omitted rather than inferred.

## Original Intended Delivery

Opening a type's Briefing shows what needs attention, what is moving, what changed, how its records spread across stages and categories, and how it connects, and every group view agrees with the type views about which values are in motion or finished because both read the server's stages.

## Actual Delivered

Not yet delivered.

## Execution Notes

- 2026-10-03T22:34:34Z: Effort opened; SPEC-0111's shared derivations and Briefing requirements revised to read the profile and stages, with frozen-scope drift acknowledged on EFF-2026-10-03-08-11.
- 2026-10-03T22:59:31Z: Batch 1 complete. The kit parses `profile` and exposes it through `useTypeDocs` (946705d); the group Briefing, Trace, and Sections read each member's lifecycle field, value stages, and gap fields from type documentation, and the tone- and name-based derivation is deleted (92c293b); the group-views browser test asserts in-motion records over a declared-stage enum without assuming their order (25ca810).
- 2026-10-03T23:39:58Z: Batch 2 complete. The group Briefing's loader and blocks are shared with collections (a61a2f6); `type.briefing` and `interface.briefing` show every SPEC-0112 Briefing section over a one-member model (61842f4); the catalog lists them first for every type and interface and no longer offers Overview there, with `type: "*"` and `interface: "*"` mounts (cbff581); browser tests open the Briefing on the type-views fixtures (c34c642); interface link targets take navigation labels (7417cad); the views skill, kit reference, CONTEXT notes, and CHANGELOG describe it (aa9bf5a).
- 2026-10-04T00:30:00Z: Batch 3 review fixes. An outside note's sections count as the note; typed roots sort by `updatedAt`, so each type's 500 most recently changed records are read; an interface Briefing prefers the interface's own companion doc; a lifecycle without an active stage gets no In motion block on the type Briefing; the header hides notes and average links for every collection view but Overview; list `@reverse` and `@neighbors` fields take `first`, and the collection query reads `<field>Count`, 20-target samples, and at most 50 inbound notes per record; native views on a wildcard warn; reverse declarations merge across implementors; values outside an enum count as Other; the guide shows once; interface bursts count by implementing type; the Briefing is offered only where navigation lists the collection; eject explains how to proceed after an earlier release's eject; an unknown profile shape no longer fails a type's documentation. SPEC-0112's non-goal that called switching SPEC-0111's views to stages follow-up work is removed, since batch 1 delivered it; frozen-scope-drift acknowledged for SPEC-0112 on that removal, which records delivered behavior without changing the Briefing requirements.
- 2026-10-04T00:45:00Z: Rough measurements against this repository (`rzm serve`, best of three). The Specs Briefing's records query is 125 KB and about 75 ms before and after the bounds, because no record here exceeds them; per type the read is now at most 500 × (50 + 20 per reverse field) linked notes besides authored forward links. Sorting by `updatedAt` adds about 8 ms over 1,470 acceptance criteria. Counting `efforts`, a subtree-scoped inbound neighbor list, costs about 380 ms over those records, more than reading the full lists (about 215 ms); neither type is offered a Briefing now, since navigation does not list them.

## Deviations

- 2026-10-04: (frozen-scope-drift acknowledged via EFF-2026-10-04-10-01) SPEC-0112 now delegates personal view persistence to [[view-preferences|SPEC-0114]]. [[2026-10-04-10-01-view-preferences]] owns instance-scoped storage, remembered Briefing expansion, and explicit shared configuration saves. This supersedes browser-only persistence guidance without changing this effort's Briefing content or stage derivations.

- 2026-10-03T23:24:50Z: The plan put the type Briefing in a new `web/bundled-views/type/` folder. It ships in `web/bundled-views/group/` instead, beside the group views, because eject copies one bundled folder and the type Briefing imports the group Briefing's loader and blocks: from a separate folder, an ejected type Briefing would still import the bundled modules it does not own, or need its own copy of them. One folder keeps one module set, and ejecting any of the five views copies all of them.

## Closure Checklist

- [ ] Required quality gates pass.
- [ ] Alignment and actual outcomes are verified.
- [ ] Specs and documentation are reconciled.
- [ ] Follow-ups are triaged.

## Compounding Follow-ups

- Make `<field>Count` on subtree-scoped `@neighbors` lists cheaper than reading the list; today it costs more, so a view reading counts on such a field pays for it.

## Status

Active. Batches 1 and 2 are complete; batch 3 review fixes are in, and the pull request is next.
