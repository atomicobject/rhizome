---
type: ReferenceDoc
summary: "Guide for authoring, querying, and viewing direct ActionItem nodes."
reference-kind: guide
status: active
---

# Action Items

Action items are checkbox rows marked with `#action-item`.

```md
- [ ] Update rollout note #action-item
  assignee:: [[Drew Colthorp]]
  due:: 2026-05-12
```

The `action-items` starter depends on `core`, so assignees are `Person` notes. The local current user is configured with:

```bash
rzm agent current-user set "Drew Colthorp"
rzm agent current-user validate
```

Add action items in the note that created the commitment: a meeting note, spec, effort, project brief, source note, or any other in-scope markdown file. Do not move contextual work into a central task file unless the user asks for that workflow.

`due::` is optional and must be an ISO date (`YYYY-MM-DD`). Prose dates such as `tomorrow` are invalid typed dates.

Use direct ActionItem recipes for queries. Do not query action items by first finding conversations or meetings.

For list, table, and "my action items" summaries, select identity, title, `done`, `assignee`, `due`, `notePath`, and locator fields rather than `content`; those fields can be answered from the indexed `actionItem` source before pagination.

```bash
rzm agent query-recipe run --id all-action-items
rzm agent query-recipe run --id my-action-items --input assignee=<validated Person title or ref>
rzm agent query-recipe run --id action-items-in-note --anchor docs/specs/example.md
```

The installed `Action Items` view shows all open action items by default, sorted by due date and source note. Its `Mine` filter binds to `ontology.currentUser.resolvedRef`; if current user is missing or does not resolve to `Person`, fix identity setup rather than guessing. Views may be customized into an open/done tracker by grouping on `done`, but the starter default stays open-only.

Views and recipes push supported `done`, `assignee`, `due`, and source-note filters into the direct `actionItem` source before pagination. Source-context recipes currently scope to the source note path, which keeps action items attached to the note that produced them while staying compatible with `agentic-engineering` and plain notes.
