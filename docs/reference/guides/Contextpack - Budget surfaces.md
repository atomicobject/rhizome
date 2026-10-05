---
type: ReferenceDoc
summary: "Explains the active context budget numbers: 70k contextpack default, config overrides, explicit caller budgets, and the 150k local rzm agent floor."
reference-kind: guide
last-verified: 2026-04-28
status: active
---

# Contextpack - Budget surfaces

Rhizome has several budget surfaces. Do not document them as one universal default.

- `pkg/app/contextpack.DefaultBudgetChars` is the global contextpack fallback: 70,000 chars.
- `.rhizome/config.yml` `budgetChars` overrides that fallback for MCP and CLI context tools that use vault config.
- `--budget-chars` or caller options override both when provided.
- `rzm agent` standalone CLI applies a local floor of 150,000 chars when the config/default would be smaller. This is an agent convenience, not the contextpack package default.
- `rzm search --pack` uses its own packed-output budget path; without an explicit budget it currently asks unified search for a compact 6,000-char packed search response.

Use 70k when documenting the packer package. Use 150k only when documenting local `rzm agent` behavior. If a caller passes an explicit budget, that number is the contract for that call.

## Why it matters

Docs that say "default 50k" or imply "default 150k everywhere" cause agents to over-pack or under-pack. The correct model is layered: packer default, vault config, explicit caller budget, then any command-specific floor or compacting behavior.
