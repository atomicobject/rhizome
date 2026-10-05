---
name: assumption-tracker
description: >-
  Use when documenting a legacy codebase and you need to flag uncertain
  findings as structured assumptions for human validation, or when
  synthesizing open assumptions into kickoff meeting questions. Two modes:
  flag (during documentation) and synthesize (before meetings).
---

# Assumption Tracker

## Goal

Surface every point of uncertainty during legacy codebase analysis and route it to the right human review. Undocumented assumptions become silent defects in the documentation. Explicit, queryable assumption items become meeting agendas.

## When to use

- **Flag mode**: while documenting code — whenever the code alone is ambiguous, naming is unclear, or business logic appears to be driven by convention rather than code
- **Synthesize mode**: before a client kickoff meeting or clarification session — to turn all open assumptions into a prioritized question list organized by subsystem and type
- When the agent has less than `source_backed` confidence in a claim it is about to document

## Non-goals

- Do not use this to track bugs or implementation issues — those belong in the project's issue tracker
- Do not use for uncertain implementation decisions on AO's delivery work — use the spec `Open Questions` section for that
- Do not synthesize meeting questions without first querying via the recipe — never rely on memory of what was flagged

## Operating mode

- **Flag mode**: `Agent Delegation` — write assumption items immediately when confidence is low; no check-in needed
- **Synthesize mode**: `Agent Partnership` — review the grouped list with the user before finalizing meeting questions

## Assumption Markup Convention

Write assumption items as ActionItem checkbox items with an `#assumption/<type>` tag in the body:

```markdown
- [ ] Confirm: what does status=7 mean in the orders table? #action-item #assumption/business-logic
  assignee:: <Person title>
  due:: <next client meeting date, YYYY-MM-DD>
```

**The four assumption types:**

| Tag | Use for |
|---|---|
| `#assumption/business-logic` | Unknown business rule, domain meaning, or process decision encoded in the system |
| `#assumption/schema` | Uncertain field meaning, implicit relationship, constraint, or data type intent |
| `#assumption/integration` | Unclear integration point between modules, systems, or the legacy/2.0 boundary |
| `#assumption/process` | Uncertain workflow, operational process, or how the system is actually used in practice |

## Flag Mode Procedure

When encountering an uncertain finding while documenting:

1. **Identify the uncertainty** — is the code itself ambiguous, or is the business meaning unclear even if the code is clear? Both warrant a flag.
2. **Choose the right type** — pick from the four types above; if it spans two types, pick the one closest to the meeting conversation you'd need to have
3. **Write a specific "Confirm: X" statement** — name the exact claim being confirmed, not a vague topic
4. **Place the item in context** — write it in the note that documents the file, table, or module where the uncertainty lives; do NOT accumulate assumptions in a catch-all file
5. **Set the due date** — use the next scheduled client meeting date as the default

### Good vs. bad assumption items

| Good ✓ | Bad ✗ |
|---|---|
| `Confirm: status=7 in orders means "shipped" based on the handling in OrderController.php:142` | `Ask about order statuses` |
| `Confirm: the customer_code field is always the same as erp_id for legacy customers` | `Unclear customer ID fields` |
| `Confirm: the EDI 850 inbound flow always creates a new order even if a duplicate PO number exists` | `EDI handling needs review` |
| `Confirm: commission_rate on line items overrides the customer-level rate when non-null` | `Commission calculation questions` |

## Synthesize Mode Procedure

Before a client kickoff or clarification session:

1. **Run the query recipe** to surface all open assumptions:
   ```bash
   rzm agent query-recipe run --id assessment-assumptions
   ```
2. **Filter the results** — keep only rows whose `title` contains `#assumption`
3. **Group by type and subsystem**:
   - Group first by subsystem (source `notePath` → the module being documented)
   - Within each subsystem, group by assumption type tag: `business-logic` → `integration` → `schema` → `process`
4. **Draft the questions**: turn each "Confirm: X" item into a direct question for the technical lead or domain expert, and merge items that one answer would settle.
5. **Order groups by meeting priority**:
   - First: `business-logic` and `integration` (highest business risk, block documentation accuracy)
   - Second: `schema` and `process` (important but usually answerable from the team quickly)
6. **Output to `docs/assessment/kickoff-questions.md`** with this structure:

```markdown
# Kickoff Questions — [Date]

## [Subsystem Name]

### Business Logic
1. [Question from assumption item — cite the relevant code location if helpful]

### Schema
1. [Question from assumption item]

## [Next Subsystem]
...
```

## Guardrails

- **"Confirm: X" not "Ask about Y"** — every assumption must name the specific claim, not just the topic area
- **Near context, not catch-all** — place items in the note that documents the relevant code; a `misc-assumptions.md` file defeats querying by subsystem
- **Query before synthesizing** — never write meeting questions from memory; always run `assessment-assumptions` first
- **Don't assume the answer** — the purpose of flagging is to schedule human validation; do not fill in the answer and mark it done without a real conversation

## Reference notes

- See `action-items` skill for full ActionItem authoring, query, and update workflow
- The `assessment-assumptions` query recipe surfaces all open items across the vault; filter client-side for `#assumption` in title
- See `legacy-codebase-assessor` for the upstream assessment that produces the priority-file list this skill flags against; its findings note seeds candidate assumption types per file
- See the `client-harness-builder` skill for packaging validated documentation into client-facing deliverables
- Engagement workflow chain: `legacy-codebase-assessor` (assess) → `assumption-tracker` (this skill) → `client-harness-builder` (package)
