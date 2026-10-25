---
type: ReferenceDoc
summary: "Guide to the session-memory add-on: mining agent sessions into reviewed shards, document edits, and findings; privacy boundaries; TypeSafe; hooks and trust; and the friction report."
reference-kind: guide
status: active
---

# Session memory

The `session-memory` add-on reads the Claude Code and Codex sessions recorded for this repository and proposes changes that make agents work better here. A person reviews every proposal before anything reaches the repository.

Most friction agents hit comes from something the team can change: dead code that misleads them, a flaky test, a bad default, a rule nobody wrote down. A note that teaches agents to work around such a problem keeps the problem. So each proposal carries one of five responses:

| Response | Becomes | Example |
| --- | --- | --- |
| remember | a context shard in `docs/memory/context-shards/` | "Busy timeout 0 means the 30-second default." |
| document | an edit to the document that should already say it | a missing step in a runbook |
| fix | an agent finding with an action item | dead code agents keep reading |
| guard | an agent finding with an action item for a check, optionally with a guard rule | "run `make vendor-treesitter`, never bare `go mod vendor`" |
| ask | an agent finding holding a question for its owner | conflicting intent between two documents |

Findings live in `docs/memory/agent-findings/`. Their action items use the `#action-item` format, so the action-items queries list them.

## Set up

```bash
rzm init --add-addon session-memory
rzm agent current-user set "<your Person note title>"
```

The skill needs Node 20 or later. Approval records the reviewer, so `rzm agent current-user validate` must resolve to a `Person` note.

Ask your agent to use the session-memory skill. It runs the scripts below and explains what they return.

## What leaves your machine

- **Unreviewed data stays local.** Transcript digests, screened episodes, candidates, proposals, and the run ledger live in `.rhizome/session-memory/`, which Git ignores. Mining refuses to run if Git would track that directory. On macOS and Linux it is readable only by you; on Windows it has the checkout's permissions.
- **Transcript text goes to two places only:** the model provider of the harness you run the skill in (Claude through Claude Code, or OpenAI through Codex), and TypeSafe's Jev for screening. A run never moves transcript text to another provider.
- **The repository gets only what you approve.** Before anything is written, every tracked write is scanned for credential-shaped values, and approval asks you to confirm the text is fit to publish. Redaction catches credential shapes, not every private detail, so read what you approve.

## TypeSafe and Jev

Screening asks Jev two cheap questions about each passage of a session and sends only the most promising 30% of the text to the extraction model. That is what makes a run affordable. It needs a TypeSafe credential configured for Rhizome. Without one, the skill says so and can run unscreened over a short window.

## Mine

A first run reads the last 7 days of sessions and asks before reading more than 200. Run it without a budget first: it spends nothing, and it shows the model for each role, every provider that will receive transcript text, and the estimated cost. Then give it a spend limit. Cost grows with the number of sessions, so the estimate is the number to check. Later runs read only new turns, and an unchanged rerun makes no model calls.

A run that reaches its budget stops and resumes where it left off.

## Review

The skill presents each proposal with its proposed response and reason first, then the statement, evidence, and drafted artifact. You can approve it, redirect it to another response, revise it, dismiss it (shared with the team or kept local), or defer it.

- A **document** approval applies the edit as an uncommitted change and records a `documented` decision so the learning is not proposed again.
- A **fix**, **guard**, or **ask** approval writes a finding assigned to you, or to another Person you name. While its action item is open, the learning is not proposed again.
- Approval is refused while the files a proposal depends on have uncommitted changes, or when they changed after verification read them. The agent re-checks and asks you before passing `--rechecked`.

New shards, findings, document edits, and shared decisions are ordinary changes for you to commit.

## How shards reach agents

**Lookup works everywhere.** The managed `AGENTS.md` block tells agents to run lookup before working in an unfamiliar area. It returns up to five current shards for files, a command, or a task, rules first, and withholds shards whose files changed since they were verified.

**Hooks deliver automatically, once you turn them on.**

```bash
rzm init --hooks on
```

Init writes one Rhizome entry per hook point into `.claude/settings.json` and `.codex/hooks.json`. With hooks on, agents receive shards with no patterns when a session or subagent starts. After an agent reads, edits, or patches a file, or runs a command a shard names, they receive the shards that apply to it. An agent sees each shard once, until its context is compacted or cleared.

- **Codex asks you to trust each new or changed entry** through `/hooks` in Codex. Until you do, Codex runs no Rhizome hook.
- **Codex threads in T3 Code don't run untrusted project hooks.** They rely on lookup.
- **`rzm init --check`** reports, per harness, whether the entries are installed and current, and when the add-on last delivered context.
- **When shards don't arrive,** `.rhizome/hooks/diagnostics.log` says why. Delivery hooks never block or fail an agent's tool call; only an approved guard rule refuses one, on purpose, and its message names the rule's finding note.

Known limits:
- Codex doesn't tell hooks which directory a command ran in, so a read in another directory may be missed or matched to a same-named file.
- Redelivery to a Claude Code subagent after it compacts is expected but not yet confirmed live.

## Friction report

Ask for the friction report to see open findings and pending proposals ranked by how much agent effort they cost: families of sessions, sessions, and tool calls. Each completed finding is marked `resolved` if the friction has not recurred since its action item was checked, or `recurring` with the sessions that hit it again. A recurring finding comes back for review. The report makes no model calls and shows no transcript text.

## Maintain

Shards go stale when their files change. Maintenance lists the shards that need attention, and for each one the agent re-checks it against the current code and asks you whether to renew, revise, or retire it.

## Calibrate

Calibration compares models for extraction or verification against your own approved shards, or a small synthetic set when you have fewer than ten. It estimates the cost before it runs and proposes a change only when a model clearly does better, or matches quality for at least 25% less cost or time. Nothing changes until you approve the change, and then only your local profile changes.
