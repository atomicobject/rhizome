# AO KB adoption: council and design brief

## Requested outcome

Drew wants a detailed, reviewed plan and compelling design comps before implementation. The first milestone keeps the existing AO KB skills and Rhizome side by side over the same authored files. Rhizome supplies native nodes, queries, source/relationship navigation, and custom views. Existing AO KB workflows continue to handle curation, authoring, generation, and publishing.

The user explicitly requested council members Fable, Opus, and Sol xhigh, with Opus producing design comps and the other models collaborating through critique. The coordinator will relay all findings and request fresh reviews after revisions. No platform or starter implementation is authorized yet. Prototype HTML and plan documentation are authorized.

## Sources and current state

- Proposed plan: `docs/specs/technical/format-neutral-embedded-nodes-and-ao-kb-adoption.md` (SPEC-0133). Read it in full. It includes the new R7 experience requirements.
- Current Rhizome baseline: `e8d910cb5` in this shared checkout. No implementation changes have been made.
- AO toolkit: `/Users/colthorp/Developer/atomic-ai-prompt-toolkit`, inspected at `e9dce951`, AO KB 7.138.8 and kb-site 3.137.0. Treat it as read-only.
- AO evidence rules: `plugins/ao-kb/references/templates/.kb/system/kb-conventions.md`; metamodel and account behavior: `packages/kb-site/kb_site/kits/core/_build/{metamodel.py,standard-metamodel.yaml,account.py}`.
- Existing Rhizome visual language: `PRODUCT.md`, `DESIGN.md`, `web/src`, and relevant committed screenshots. Follow `.agents/skills/impeccable/SKILL.md`; read full design guidance rather than the stale `.impeccable/design.json` sidecar.
- Existing platform contracts: provider facts, generic embedded-node semantics, native query/view/navigation APIs, and managed starter lifecycle. Do not invent separate copies of source records or a new global graph engine.

## Experience goals

1. A person can find which assertions or knowledge areas deserve attention, understand why, and enter the relevant evidence without hunting across files.
2. A person can read a complete assertion, inspect exact source evidence and attribution, distinguish recorded confidence/origin/status/validity facts, and follow contradiction or supersession relationships.
3. A person can navigate a focused, legible relationship/impact view from source to assertion to concept/model record to affected artifact or authored code reference. Relationships have explicit meanings; source containment is not evidence.
4. Moving between a list, record, evidence, and links preserves selection/filter context and provides a clear way back.
5. Dense, calm professional UI, using Rhizome's existing typography, color and interaction language. No dashboard theater or invented aggregate scores. Give the claim text and source evidence useful space.

These are jobs, not a demand for three disconnected screens. Prefer a coherent workspace with purposeful views. Standard components are a foundation; the user explicitly wants meaningful custom experiences, so a set of generic tables alone is insufficient.

## Truth and scope boundaries

- This is a design and planning exercise, not feature implementation. Comps use synthetic data and identify it as illustrative.
- Do not imply that a resolve/accept/edit/publish button executes a native workflow that the milestone does not support. Viewing evidence and navigating records may be fully interactive in the prototype; AO handoffs must say what they actually do.
- Distinguish authored AO state, structural facts Rhizome computes, and unavailable/stale analysis. No invented certainty, automatic truth arbitration, or false successful empty states.
- All AO-owned authored sources are proposed to be read-only through native Rhizome mutation surfaces. External AO workflows remain the writer. Read-only enforcement must survive failed projection/bindings.
- One selected product context per vault is a proposed first-release limit, NOT approved by Drew yet. Broader ordinary vault search still exists. Do not treat product selection as access control.
- Setup currently requires ordinary starter install, a dependency/product/binding action, then indexing. Init does not have an install-time compiler hook. Avoid promising that file copying alone produces the model.
- Preserve full embedded-node locators; same-file records remain distinct. Graph expansion is bounded and counted, never silently truncated.
- Existing repository files and other agents' changes are not yours to revert. Reviewers are read-only. Opus owns only `docs/design/ao-kb/` design artifacts, excluding this coordinator-owned brief.

## Opus deliverables

- A coherent design direction with enough complementary views to demonstrate all three jobs. Produce a working self-contained HTML prototype at `docs/design/ao-kb/comp-v1.html` and concise `design-notes.md` stating rationale, interaction coverage, empty/error/stale/dense states, and feasibility assumptions.
- Use internal synthetic fixture data for navigation/selection/filtering. No network calls, live data, secrets, implementation changes, or server startup.
- The coordinator shows HTML through T3 `html_preview` then `html_render` after council agreement. Build a complete HTML document with inline CSS/JS, transparent outer surface, fluid layout, and T3 theme variables (`--foreground`, `--muted-foreground`, `--card`, `--border`, `--primary`, `--accent`, `--chart-1` etc.). Preserve the actual Rhizome visual language. Support light/dark appearance.
- Main desktop composition should work around 1280px. At 728px inline width, reflow details rather than shrinking text or clipping; phone-first product redesign is not requested. No `vh` or fixed page-height layout.
- Use semantic buttons/links and keyboard focus. No dead interactive-looking controls. Show source excerpts, relationship meaning, and real record text, not placeholders.
- Self-review the rendered output if the available tools permit. Do not show the prototype to the user yourself; return paths and the most important tradeoffs to the coordinator.

## Review contract

Each member returns concrete findings and a recommendation. Separate material blockers from worthwhile refinements and optional later work. Do not expand the initial release to the full AO KB port.

For plan reviews, assess user outcome, architectural correctness, implementation feasibility, testability, and scope. For comp reviews, assess usefulness, evidence fidelity, navigation clarity, consistency with Rhizome, accessibility/legibility, and honest behavior.

Give a score out of 10 for the plan and, when available, design separately. Scores are feedback, not a substitute for stated blockers. Say `satisfied` only when no material issue remains. The coordinator records actual scores by round and does not infer agreement from silence.

Round 1 reviews the expanded plan and proposes design direction while Opus builds. Round 2 reviews the integrated plan and actual rendered comp. Additional focused revisions/reviews address concrete unresolved findings; avoid scope creep.
