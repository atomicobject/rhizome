## Compounding

- Use `.agents/skills/compounding/SKILL.md` to mine agent sessions, review proposals, look up context shards, re-verify stale shards, or report where agents lose effort.
- Before editing files or running commands in an unfamiliar area, run `node .agents/skills/compounding/scripts/lookup.mjs --files <paths>` (or `--command "<cmd>"`, or `--task "<what you are doing>"`) and follow the rules it returns. With hooks on, matching shards also arrive automatically after reads and edits; lookup is still how to check before you start.
- When an investigation turns up a non-obvious fact about this repository, state it in one sentence in your reply. Mining reads replies, not tool output.
- Approved shards live in `docs/memory/context-shards/` and agent findings in `docs/memory/agent-findings/`; create or change them only through the skill's `shards.mjs`. Complete a finding by checking its action item.
