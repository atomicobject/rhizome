---
type: ReferenceDoc
summary: "Guide for configuring note-backed Person identity and current-user state."
reference-kind: guide
status: active
---

# Core Identity

`core` provides note-backed `Person` nodes for workflow templates that need accountable humans. Other starters should depend on this shape instead of inventing their own owner or assignee model.

Create people as tracked markdown notes under `people/` or `docs/people/`:

```md
---
type: Person
display-name: Drew Colthorp
aliases:
  - Drew
---

# Drew Colthorp
```

Set the local current user with:

```bash
rzm agent current-user set "Drew Colthorp"
rzm agent current-user validate
```

The current-user file lives at `.rhizome/agent/user.yml`. That file is ignored local state; the `Person` note is tracked content.

Use `rzm agent current-user show` to inspect the configured ref. Do not infer the current user from chat context, OS usernames, Git authors, or vault names; configure the explicit `Person` title once and let recipes/views bind through it.
