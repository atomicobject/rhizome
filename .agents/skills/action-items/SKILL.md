---
name: action-items
description: Use whenever an agent wants to create, find, query, summarize, update, complete, assign, review, or otherwise handle action items. Not for effort closure checklists, spec coverage checklists, or quality-gate checklists.
---

# Action Items

## Required Setup

- Before answering "my action items", "what do I owe", "show my todos", or similar, run `rzm agent current-user validate`.
- If current-user state is missing, unresolved, or not a `Person`, stop and guide the user to create/select a `Person` note, then run `rzm agent current-user set "<Person title>"`.
- Never infer the current user from chat context, OS username, Git author, vault name, or prose.
- Do not use this skill for effort closure checklists, spec coverage checklists, or quality-gate checklists. Those are owned by the effort/spec workflow skills.

## Creating Or Modifying

1. Run `rzm agent ontology-authoring-guide --type ActionItem`.
2. Add action items near the source context that created them, not in a central catch-all file unless requested.
3. Author an action item as a checkbox item marked `#action-item`.
4. Put structured fields on following item-local lines when possible:

```md
- [ ] Update rollout note #action-item
  assignee:: <Person title or ref>
  due:: 2026-05-12
```

- `assignee::` must resolve to a `Person`.
- `due::` is optional and must be ISO `YYYY-MM-DD`.
- Do not create free-text assignees that do not resolve to a `Person`.
- Action items may be added to any in-scope note; the global `ActionItem` source finds marked checkbox items without requiring the note to be a conversation or meeting.

## Finding Or Querying

- Prefer bundled query recipes:
  - `rzm agent query-recipe run --id all-action-items`
  - `rzm agent query-recipe run --id my-action-items --input assignee=<validated Person ref>`
  - `rzm agent query-recipe run --id assigned-action-items --input assignee=<Person title or ref>`
  - `rzm agent query-recipe run --id action-items-in-note --anchor <note path>`
- Keep queries rooted at `actionItem`, not at a containing conversation/meeting/spec/project root.
- The installed Action Items view defaults to all open items. Use its `Mine` filter only after current-user validation succeeds; the filter binds from `ontology.currentUser.resolvedRef`.

## Updating

- Completion should toggle only the checkbox token.
- Assignee, due date, and text edits should use source-span-aware edit paths when available.
- After an edit that adds or changes `assignee::`, `due::`, or the `#action-item` marker, run `rzm agent validate ontology` so a bad Person reference or date is caught while the note is still open. A completion toggle needs no validation.
