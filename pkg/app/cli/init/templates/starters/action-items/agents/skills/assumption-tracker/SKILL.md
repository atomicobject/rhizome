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
- When the code or a cited source does not directly establish a claim the agent is about to document

## Non-goals

- Do not use this to track bugs or implementation issues — those belong in the project's issue tracker
- Do not use for uncertain implementation decisions on AO's delivery work — use the spec `Open Questions` section for that

## Operating mode

- **Flag mode**: `Agent Delegation`. Write assumption items directly once their owner is resolved; ask only when the required Person cannot be resolved.
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
4. **Place the item in context** — write it in the note that documents the file, table, or module where the uncertainty lives; a catch-all file such as `misc-assumptions.md` defeats querying by subsystem
5. **Assign the owner.** Use the person the user named. Otherwise, run `rzm agent current-user validate` and use the resolved current user. Follow `action-items` for Person resolution and validation. If the named owner or current user cannot resolve to a Person, ask the user to select or create that Person before writing the item.
6. **Set the due date.** Use the next known client meeting date. If no meeting date is known, omit the optional due date.

Flag mode is done for a note when every documented claim that the code or a cited source does not directly establish has a "Confirm: X" item with a resolved Person assignee, and ActionItem validation passes.

### Good vs. bad assumption items

| Good ✓ | Bad ✗ |
|---|---|
| `Confirm: status=7 in orders means "shipped" based on the handling in OrderController.php:142` | `Ask about order statuses` |
| `Confirm: the customer_code field is always the same as erp_id for legacy customers` | `Unclear customer ID fields` |
| `Confirm: the EDI 850 inbound flow always creates a new order even if a duplicate PO number exists` | `EDI handling needs review` |
| `Confirm: commission_rate on line items overrides the customer-level rate when non-null` | `Commission calculation questions` |

## Synthesize Mode Procedure

Before a client kickoff or clarification session:

1. **Run the query recipe** to retrieve the first page of open action items:
   ```bash
   rzm agent query-recipe run --id assessment-assumptions
   ```
2. **Complete retrieval, then filter.** The recipe caps all open action items at 100, before assumption filtering. Inspect `extensions.typedRoots.actionItem`. When `hasMore` is true, use the `rhizome` ontology-usage route to inspect the `ActionItem` query schema and continue the recipe's query through `ontology-query`, setting `offset` to `nextOffset`. Preserve its fields, filter, and sort until coverage is complete. If coverage metadata is absent and 100 rows are returned, treat the set as partial and verify continuation. Collect every page, then keep rows whose `title` contains `#assumption`. If retrieval cannot finish, label the questions as a partial draft and state the coverage gap.
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

Synthesize mode is done when retrieval covers all open action items in the requested scope, every open `#assumption` item appears as a question or is merged into one, and the user has reviewed the grouped list. A partial draft is not complete.

## Guardrails

- **Don't assume the answer** — the purpose of flagging is to schedule human validation; do not fill in the answer and mark it done without a real conversation

## Reference notes

- See `action-items` skill for full ActionItem authoring, query, and update workflow
- Engagement workflow chain: `legacy-codebase-assessor` (assess; its findings note seeds candidate assumption types per priority file) → `assumption-tracker` (this skill) → `client-harness-builder` (package validated documentation into client-facing deliverables)
