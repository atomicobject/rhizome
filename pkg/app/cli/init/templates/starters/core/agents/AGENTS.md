## Core Identity

- Use note-backed `Person` nodes for accountable humans.
- Configure the local user with `rzm agent current-user set "<Person title>"`; validate with `rzm agent current-user validate`.
- Use `rzm agent current-user show` when you need to inspect the configured identity.
- Do not infer the current user from chat context, OS usernames, Git authors, or vault names.
