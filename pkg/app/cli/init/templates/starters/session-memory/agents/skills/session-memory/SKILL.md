---
name: session-memory
description: Use when mining this repository's Claude Code or Codex sessions for reusable project knowledge, reviewing or approving the resulting proposals, looking up the context shards that apply to files, a command, or a task, or re-verifying shards whose files changed.
---

# Session memory

This skill turns the agent sessions recorded for this repository into context shards: short, verified rules and facts that later agents should know. Mining produces local proposals; a person approves each one before it becomes a tracked note in `docs/memory/context-shards/`.

Run every script from the repository root with Node 20 or later. `<skill>` below is this skill's directory. Scripts reach models only through `rzm agent generate` and Jev through `rzm agent evaluate-batch`, and print JSON. A script that refuses an action exits 2 and says why; relay the reason instead of working around it.

## Lookup

Run before editing files or running commands in an area you have not worked in this session:

```sh
node <skill>/scripts/lookup.mjs --files path/a.go path/b.md
node <skill>/scripts/lookup.mjs --command "make web-e2e"
node <skill>/scripts/lookup.mjs --task "add a validation check for views"
```

Follow the rules it returns; facts are background, and a where-to-find shard names the document to open. Lookup returns at most five shards, rules first, and withholds shards whose files changed since they were verified. When more match, it says so; `--skip N` shows the matches after the first N. When it reports withheld shards, mention it; the person can run maintain.

## Hooks

With hooks on, Rhizome's hook dispatcher runs this skill's `scripts/hook.mjs` and the shards arrive without asking: shards with no patterns at session and subagent start, and after a tool call, the shards for the files the agent read, edited, or patched, or for a command it ran. A context sees each shard once until a compaction or clear. Hook delivery selects shards the same way lookup does, so lookup is still how to check a task or files before starting, and the only way in hosts that do not run project hooks, such as Codex threads in T3 Code.

- Hooks are off until the person turns them on: `rzm init --hooks on`, and `rzm init --hooks off` to stop. Init writes the entries into `.claude/settings.json` and `.codex/hooks.json`; never edit those entries by hand.
- `rzm init --check` reports, per harness, whether the entries are installed and current and when this skill last returned context. Codex runs a new or changed entry only after the person trusts it with `/hooks` in Codex; tell them so after turning hooks on.
- When shards do not arrive, read the last lines of `.rhizome/hooks/diagnostics.log` and report what it says.

## Mine

1. Pass the harness you are running in: `--harness claude` in Claude Code, `--harness codex` in Codex. Transcript text goes only to that provider and to Jev. Local state in `.rhizome/session-memory/` is readable only by the person on macOS and Linux; on Windows it has the checkout's permissions, so tell the person before a first run there.
2. Run `node <skill>/scripts/mine.mjs --harness <h>` with no budget. It spends nothing and prints the model for each role, every provider that will receive transcript text, and the estimated cost. Show these to the person.
3. Ask for a spend limit, then run `mine.mjs --harness <h> --budget <USD>`.
4. If the run refuses because no price is known for a model, choose a priced model with `--model`, or ask the person whether to pass `--allow-unpriced`, which makes the budget count only the costs the provider reports.
5. Exit 3 means the run stopped at its budget. Rerunning resumes it with the same models, and finished calls are not repeated. `--abandon` discards an open run instead.
6. Summarize the run report, then offer review.

Options: `--since`/`--until YYYY-MM-DD` bound the sessions; later runs read only new turns. `--verify-budget N` verifies up to N single-session clusters (default 40), and `--verify-held N` verifies N that earlier runs held back. `--model <role>=<provider>:<model>:<effort>` overrides one role; `references/models.md` explains the roles and defaults.

## Review

1. Check the reviewer: `rzm agent current-user validate` must resolve to a `Person`. If it does not, stop and ask the person to run `rzm agent current-user set "<Person title>"`.
2. `node <skill>/scripts/shards.mjs list` shows pending proposals. Present each one with its tier (A: seen in two or more sessions; B: one session, confirmed in the code), statement, applies-to patterns, and why it matters. `shards.mjs show <cluster>` gives the verification record and evidence; `shards.mjs source <cluster> [n]` shows a cited excerpt, redacted.
3. For each proposal, first ask whether it describes something to fix rather than remember: dead or misleading code, a flaky test, a bad default, a missing check, or an open question. A shard that teaches agents to work around a defect keeps the defect. If the person chooses to fix it, guard against it with a check or test, or ask the team, dismiss the proposal as below with that response and the proposed change as its reason (`--reason "fix: <change>"`), and offer to make the change. Notes that track these responses are planned (SPEC-0124 US2).
4. Otherwise, ask the person to approve, revise, dismiss, or defer:
   - **Approve:** ask whether the statement is fit to publish in this repository, which may be public. Only after a yes, run `shards.mjs approve <cluster> --confirm-publish`.
   - **Revise:** write the changed fields (`kind`, `statement`, `why`, `appliesTo`, `commands`, `target`) to a JSON file outside the repository, show the person the result, then approve with `--edit <file>`. State the behavior agents should follow, not the mistake to avoid; naming a mistake tends to invite it.
   - **Dismiss:** ask for the reason and whether to share the decision with the team (`--shared`, a tracked file) or keep it local (`--local`), then run `shards.mjs dismiss <cluster> --reason "<reason>" --shared|--local`.
   - **Defer:** do nothing; it stays pending.
5. Approval is refused while files the shard depends on have uncommitted changes; ask the person to commit or stash first. New shards and shared decisions are ordinary repository changes for the person to commit.
6. Approval is refused when files the shard depends on changed after verification read them. Re-check the statement against those files, tell the person what you found, and pass `--rechecked` only if it still holds; otherwise revise it.
7. Approval is also refused when the patterns match much of the repository, because the shard would then reach nearly every agent. Propose narrower patterns through `--edit`; pass `--broad` only when the person confirms it applies that widely. When approval returns a `warning` that too many shards apply everywhere, tell the person and offer a pattern or an `AGENTS.md` edit instead.

## Maintain

1. `node <skill>/scripts/shards.mjs check` lists shards that need attention, with reasons: their files changed, their patterns match nothing, their content was edited by hand, or they have no patterns and were verified more than 30 days ago.
2. For each, re-check the statement against the current code and documentation, and tell the person what you found.
3. With the person's choice, pass the `expect` hash from `check`:
   - still true: `shards.mjs renew <path> --expect <hash>`
   - needs new wording or scope: `shards.mjs revise <path> --expect <hash> --edit <file> --confirm-publish`
   - no longer true: `shards.mjs retire <path> --expect <hash> --reason "<reason>"`

## Not yet available

Calibrating model choices (SPEC-0124 US6) is planned but not part of this version. Tell the person so rather than improvising it.
