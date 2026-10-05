## Action Items

- Use `.agents/skills/action-items/SKILL.md` whenever creating, finding, updating, completing, assigning, or summarizing action items.
- Query action items through direct `ActionItem` recipes/roots, not through a conversation or meeting type.
- Use `rzm agent current-user validate` before answering "my action items"; configure with `rzm agent current-user set "<Person title>"` when missing.
- Add action items to the note where the commitment belongs, using `#action-item`, `assignee::`, and optional ISO `due:: YYYY-MM-DD`.
