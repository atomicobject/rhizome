## Session Memory

- Use `.agents/skills/session-memory/SKILL.md` to mine agent sessions, review proposals, look up context shards, or re-verify stale shards.
- Before editing files or running commands in an unfamiliar area, run `node .agents/skills/session-memory/scripts/lookup.mjs --files <paths>` (or `--command "<cmd>"`, or `--task "<what you are doing>"`) and follow the rules it returns.
- When an investigation turns up a non-obvious fact about this repository, state it in one sentence in your reply. Mining reads replies, not tool output.
- Approved shards live in `docs/context-shards/`; change them only through the skill's `shards.mjs`.
