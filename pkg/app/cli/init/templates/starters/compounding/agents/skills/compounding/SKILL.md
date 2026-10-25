---
name: compounding
description: Use when mining this repository's Claude Code or Codex sessions for reusable project knowledge or recurring agent friction, reviewing or approving the resulting proposals, looking up the context shards that apply to files, a command, or a task, re-verifying shards whose files changed, or reporting where agents lose effort and whether fixes worked.
---

# Compounding

This skill turns the agent sessions recorded for this repository into reviewed improvements that compound: each one removes friction or supplies missing context for every later session. Mining produces local proposals, and each carries a proposed response: fix it, guard against it, or ask its owner, which becomes an agent finding with an action item in `docs/memory/agent-findings/`; document it in an existing document; or remember it as a context shard (a short, verified rule or fact in `docs/memory/context-shards/`). Memory is for human intent and facts an agent needs that documentation will not surface at the right moment, not for working around something the team can fix. A person approves each one before anything reaches the repository.

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
- Approved guard rules refuse a matching tool call before it runs, naming the rule's finding note, its reason, and the alternative. Follow the alternative; never rephrase a command to get past a rule. `rzm init --check` lists the active rules per harness and how often each refused a call.
- When shards do not arrive, or a guard rule does not apply, read the last lines of `.rhizome/hooks/diagnostics.log` and report what it says.

## Mine

1. Pass the harness you are running in: `--harness claude` in Claude Code, `--harness codex` in Codex. Transcript text goes only to that provider and to Jev. Local state in `.rhizome/compounding/` is readable only by the person on macOS and Linux; on Windows it has the checkout's permissions, so tell the person before a first run there.
2. Run `node <skill>/scripts/mine.mjs --harness <h>` with no budget. It spends nothing and prints the model for each role, every provider that will receive transcript text, and the estimated cost; `discovery.inWindow` is the number of sessions it would read. Show these to the person. On a first run that would read more than 200 sessions, ask the person before mining them all, or narrow the window with `--since`.
3. Ask for a spend limit, then run `mine.mjs --harness <h> --budget <USD>`.
4. If the run refuses because no price is known for a model, choose a priced model with `--model`, or ask the person whether to pass `--allow-unpriced`, which makes the budget count only the costs the provider reports.
5. Exit 3 means the run stopped at its budget. Rerunning resumes it with the same models, and finished calls are not repeated. `--abandon` discards an open run instead.
6. Summarize the run report, then offer review.

Options: `--since`/`--until YYYY-MM-DD` bound the sessions; later runs read only new turns. `--verify-budget N` verifies up to N single-session clusters (default 40), and `--verify-held N` verifies N that earlier runs held back. `--model <role>=<provider>:<model>:<effort>` overrides one role; `references/models.md` explains the roles and defaults.

## Review

1. Check the reviewer: `rzm agent current-user validate` must resolve to a `Person`. If it does not, stop and ask the person to run `rzm agent current-user set "<Person title>"`.
2. `node <skill>/scripts/shards.mjs list` shows pending proposals. Present each one with its proposed response and reason first, then its tier (A: seen in two or more sessions; B: one session, confirmed in the code), statement, kind, applies-to patterns, target, and evidence (families, tool calls). `shards.mjs show <cluster>` gives the drafted artifact, verification record, and evidence; `shards.mjs source <cluster> [n]` shows a cited excerpt, redacted. A proposal with `recurrence` is a completed finding whose friction came back after its action item was checked; say so and name the finding. Mining verified it again against the new episodes. When it carries `rejected`, the verifier did not confirm the friction is back: show the reason and the matching episodes (`shards.mjs show`), and let the person choose a response or dismiss it. A reason of `contract` means the verifier's answer was unusable, so nothing was drafted. A proposal whose `response` is null has no proposed response, because the verifier rejected its recurrence or it was verified before responses existed (the next mine run verifies such a proposal again); approving it needs `--response` with the response the person chooses.
3. Ask one question per proposal, with its drafted artifact attached, so the person accepts or redirects rather than writes:
   - **remember:** the statement as it would be delivered.
   - **document:** the document and the exact change (old text, new text).
   - **fix:** the fix sketch (`change`).
   - **guard:** the check (`change`) and, if present, the guard rule (`guardRule`: tool class, the command words or path glob to refuse, reason, alternative), which hooks enforce before the tool call runs once it is approved. Show the reason and alternative verbatim: agents see exactly that text when the rule refuses a call.
   - **ask:** the question and its owner, if known.

   Remember only what an agent cannot learn from the code or documents. A shard that teaches agents to work around a defect keeps the defect, so when a proposal is marked `workaround`, say so.
4. Act on the answer:
   - **Approve:** ask whether the text is fit to publish in this repository, which may be public. Only after a yes, run `shards.mjs approve <cluster> --confirm-publish`. To redirect, add `--response remember|document|fix|guard|ask`; when the new response needs an artifact the proposal lacks (`edit`, `change`, or `question`), the script names it, and you write it to an `--edit` file.
     - remember writes the shard.
     - document applies the edit to the named document as an uncommitted change and records a `documented` decision. Show the person `git diff -- <document>`. When the document changed so the edit no longer applies, the script refuses; re-draft `edit` against the current text and pass it with `--edit`. When the document changed since verification read it, or the edit names a document verification did not read, the script refuses even if the old text still occurs: show the person the edit in the context of the current document, and pass `--rechecked` only if it still fits. A document holding a credential-shaped value is refused, because applying the edit would republish it.
     - fix, guard, and ask write one finding note with an action item assigned to the reviewer. `--assignee "<Person title>"` assigns another Person, such as the owner of an ask; the script refuses a name that does not resolve to a `Person` note, so ask the person which Person they meant, or to create the note. Offer to start the fix if the person wants it now.
     - Approving a guard finding with a rule activates the rule when hooks are on, once the note is tracked in Git (the person stages or commits it): then run `rzm init` so it installs the before-tool-call entry, and tell the person that Codex asks them to trust the changed entry with `/hooks`. The rule stays active after its action item is checked, since the guard is the check; deleting the note retires it. A rule edited by hand stops applying until it is revised (see Maintain). In Codex, a path rule refuses a shell read only under an absolute directory, because Codex does not tell the hook where a shell command runs; say so when the person approves a shell path rule.
   - **Revise:** write the changed fields (`kind`, `statement`, `why`, `appliesTo`, `commands`, `target`, and for a response other than remember also `responseReason`, `edit`, `change`, `guardRule`, `question`, `owner`) to a JSON file outside the repository, show the person the result, then approve with `--edit <file>`. State the behavior agents should follow, not the mistake to avoid; naming a mistake tends to invite it.
   - **Dismiss:** ask for the reason and whether to share the decision with the team (`--shared`, a tracked file) or keep it local (`--local`), then run `shards.mjs dismiss <cluster> --reason "<reason>" --shared|--local`.
   - **Defer:** do nothing; it stays pending.
5. New shards, findings, document edits, and shared decisions are ordinary repository changes for the person to commit. A learning with an open finding is not proposed again until its action item is checked.

Approving any response is refused while the files it depends on (those its patterns match, its target, the locations verification checked, and the document an edit changes) have uncommitted changes; ask the person to commit or stash first. It is also refused when those files changed after verification read them. Re-check the proposal against those files, tell the person what you found, and pass `--rechecked` only if it still holds; otherwise revise it.

Approving a remember response, or a guard rule's path, is also refused when the patterns match much of the repository, because the shard would then reach nearly every agent and the rule would refuse nearly every call. Propose narrower patterns through `--edit`; pass `--broad` only when the person confirms it applies that widely. When approval returns a `warning` that too many shards apply everywhere, tell the person and offer a pattern or an `AGENTS.md` edit instead.

## Report

`node <skill>/scripts/report.mjs` shows where agents lose effort. It reads only local state and tracked notes, makes no model calls, and prints no transcript text.

- `open` ranks open fix, guard, and ask findings and pending proposals by families, then sessions, then tool calls, with the dates first and last seen. A finding's evidence includes episodes mining matched after it was approved. When an entry has `familiesApproximate`, its family count is an estimate, because this checkout lacks the finding's family IDs (a teammate approved it, or it predates them); show it as `~N (approximate)`.
- `completed` lists findings whose action item is checked, as `resolved` (no matching episode since completion) or `recurring` (with the matching episode ids and dates). `checkedThrough` is the date of the last mine run; when it is older than the completion, offer to mine first. A recurring finding is offered for review again, unless a newer finding for the same learning is still open.

Lead with the top few open items and any recurring findings, then offer review.

## Maintain

1. `node <skill>/scripts/shards.mjs check` lists shards that need attention, with reasons: their files changed, their patterns match nothing, their content was edited by hand, or they have no patterns and were verified more than 30 days ago. It also lists guard findings whose rule was edited after approval or can no longer be read, which the hooks skip until the rule is revised: show the person the rule's text verbatim, then `shards.mjs revise <finding> --expect <hash> --edit <file> --confirm-publish`, where the file holds only `guardRule`. When `check` says the frontmatter itself cannot be read, ask the person to fix it by hand first; revise refuses it.
2. For each, re-check the statement against the current code and documentation, and tell the person what you found.
3. With the person's choice, pass the `expect` hash from `check`:
   - still true: `shards.mjs renew <path> --expect <hash>`
   - needs new wording or scope: `shards.mjs revise <path> --expect <hash> --edit <file> --confirm-publish`
   - no longer true: `shards.mjs retire <path> --expect <hash> --reason "<reason>"`

## Calibrate

Compare extraction or verification models using the harness you are running in:

```sh
node <skill>/scripts/calibrate.mjs --role extract --harness claude
node <skill>/scripts/calibrate.mjs --role extract --harness claude --budget 2
node <skill>/scripts/calibrate.mjs --role verify --harness codex --budget 2 --candidates codex:gpt-6-luna:xhigh
```

1. Run without `--budget` first. Show the estimated cost, label set, and provider to the person. A budget authorizes the model calls; reuse an already approved limit. No transcript text crosses providers.
2. By default, compare the role's profile candidates. `--candidates` takes space- or comma-separated `provider:model:effort` entries. The current model always runs twice, with the second run bypassing the result cache. Extract uses the frozen `match` model to judge learning equivalence. Verify compares decisions with human labels.
3. With at least ten approved shards whose cited episodes remain in local state for this provider, calibrate replays those episodes and any recorded decisions with retained evidence. Otherwise it uses the small, shipped synthetic fixture. The report names the set and label count. Fixture quality can support only cost or latency changes, never a quality-driven change.
4. Review quality, tokens, cost, median seconds per call, and the noise floor. Unmatched generated learnings are unlabeled; only missing human labels count as misses. A diff needs quality above the noise floor, or quality within it with at least 25% less cost or latency than both baseline runs. Cached samples report their original call time and token cost, while `spend` records what this run actually paid. An unknown price stops the run before using that model.
5. A completed report and any proposed diff are saved under `.rhizome/compounding/calibrations/`. Exit 3 means the budget or a model failure stopped the comparison; partial results cannot propose a diff. Rerunning reuses finished calls and repeats the uncached baseline.
   Calls reserve their estimated cost. Providers report actual cost only after a call finishes, so that call can exceed the limit; calibration then stops without proposing a diff.
6. Show the proposed diff and its evidence file to the person. Only after they approve that exact diff, apply it:

```sh
node <skill>/scripts/calibrate.mjs apply .rhizome/compounding/calibrations/<run>/evidence.json --confirm
```

Apply writes only `.rhizome/compounding/models.json`, preserves other roles, and refuses stale evidence if the profile changed. Never edit a profile file on the person's behalf outside this approval step. See `references/models.md` for the role defaults.
