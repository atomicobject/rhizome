---
type: ReferenceDoc
summary: "Guide to the compounding add-on: mining agent sessions into reviewed shards, document edits, and findings; privacy boundaries; TypeSafe; the weekly mining task; hooks and trust; the friction report; and the review workspace."
reference-kind: guide
status: active
---

# Compounding

The `compounding` add-on reads the Claude Code and Codex sessions recorded for this repository and proposes changes that make agents work better here. A person reviews every proposal before anything reaches the repository.

Most friction agents hit comes from something the team can change: dead code that misleads them, a flaky test, a bad default, a rule nobody wrote down. A note that teaches agents to work around such a problem keeps the problem. So each proposal carries one of five responses:

| Response | Becomes | Example |
| --- | --- | --- |
| remember | a context shard in `docs/memory/context-shards/` | "Busy timeout 0 means the 30-second default." |
| document | an edit to the document that should already say it | a missing step in a runbook |
| fix | an agent finding with an action item | dead code agents keep reading |
| guard | an agent finding with an action item for a check, optionally with a guard rule | "run `make vendor-treesitter`, never bare `go mod vendor`" |
| ask | an agent finding holding a question for its owner | conflicting intent between two documents |

Findings live in `docs/memory/agent-findings/`. Their action items use the `#action-item` format, so the action-items queries list them.


Repository evidence includes Markdown guides, schemas, templates, and skills as well as code. A workflow or writing lesson can reach verification when those sources can confirm it. Verification still rejects redundant, obsolete, generic, one-off, or unsupported claims. Finding an existing rule does not prove its delivery works; a proposed discovery or enforcement fix needs evidence of a current gap. Source attribution remains unchanged.

## Set up

```bash
rzm init --add-addon compounding
rzm agent current-user set "<your Person note title>"
```

Compounding is an optional starter installed through `rzm init`. In a terminal, turn it on in the settings menu's Add-ons section (`e` on a first run, `s` on a rerun), which also offers to turn hooks on. In an existing repository, `--add-addon compounding` adds it and preserves the other add-on choices. For a new Agentic Engineering repository, use `rzm init --workflow agentic-engineering --addons action-items,compounding`; `--addons` replaces the first-run default add-on list.

The skill needs Node 20 or later. Approval records the reviewer, so `rzm agent current-user validate` must resolve to a `Person` note.

Ask your agent to use the compounding skill. It runs the module's scripts in `.rhizome/modules/compounding/scripts/` and explains what they return.

## Which sessions it reads

Discovery selects local Claude Code and Codex sessions associated with this repository by their recorded working directory. It includes current Git worktrees. Codex sessions with the same Git origin also qualify, including sessions from another clone or a deleted worktree. Unrelated repositories are excluded unless you explicitly extend the scope with `extraPaths` in `.rhizome/compounding/config.json`. Sessions that scheduled tasks ran, whose working directory is under `.rhizome/task-state/`, are never mined, so mining never reads its own runs.

This selects repository-associated threads rather than classifying each thread's topic. Off-topic discussion inside a matching thread can enter screening. The extraction prompt tells the model to exclude personal style preferences without a repository-specific reason, outside-world facts, and generic advice. It does not read general ChatGPT history.

## What leaves your machine

- **Unreviewed data stays local.** Transcript digests, screened episodes, candidates, proposals, and the run ledger live in `.rhizome/compounding/`, which Git ignores. Mining refuses to run if Git would track that directory. On macOS and Linux it is readable only by you; on Windows it has the checkout's permissions.
- **Transcript text goes to two places only:** the model provider of the harness you run the skill in (Claude through Claude Code, or OpenAI through Codex), and TypeSafe's Jev for screening. A run never moves transcript text to another provider.
- **The repository gets only what you approve.** Before anything is written, every tracked write is scanned for credential-shaped values, and approval asks you to confirm the text is fit to publish. Redaction catches credential shapes, not every private detail, so read what you approve.

## TypeSafe and Jev

Screening asks Jev two cheap questions about each passage of a session and sends only the most promising 30% of the text to the extraction model. That is what makes a run affordable. It needs a TypeSafe credential configured for Rhizome. Without one, the skill says so and can run unscreened over a short window.

## Mine

A first run reads the last 7 days of sessions and asks before reading more than 200. Run it without a budget first: it spends nothing, and it shows the model for each role, every provider that will receive transcript text, and the estimated cost. Then give it a spend limit. Cost grows with the number of sessions, so the estimate is the number to check. Later runs read only new turns, and an unchanged rerun makes no model calls.

A run that reaches its budget stops and resumes where it left off.

## The weekly mining task

The add-on ships the scheduled task `compounding/mine` (`.rhizome/modules/compounding/tasks/mine.yml`). It is a person-scope task due every 7 days, and it follows the repository's default mode, which is notify unless `.rhizome/config.yml` says otherwise, so it waits in your Tasks inbox until you run it. `rzm tasks mode compounding/mine auto` runs it in the background instead, and `off` turns it off for you.

Its check makes no model calls. It asks `rzm agent evaluate-batch --check-credentials` whether Jev has a TypeSafe credential, and reads mining state through a dry run. In order, it:

1. needs your attention when Jev has no credential;
2. needs your attention when an open run stopped at its budget, for a credential, or because its model is unavailable, and names the fix;
3. is needed to resume any other open run;
4. is needed when at least five new episodes are waiting;
5. otherwise reports nothing to do.

Its prompt runs `mine.mjs --task-mode` with a fixed $10 budget, then the friction report. Task mode treats that budget as approved and does not ask before reading many sessions. A credential stop exits 4 with its reason, and a budget stop exits 3, which the next run resumes. The run's final message names any stop first and ends with a link to the workspace. Proposals stay in local state; review them with the skill as usual. To give a run a larger budget, mine through the skill instead.

**Test once** runs the checkout's task with fresh compounding state for that attempt. Its proposals, cursors, discovery settings, model choices, and caches stay separate from scheduled mining state. Testing can process sessions that normal mining already read, using the task's usual budget. Test output is retained with the test run rather than added to the normal compounding workspace.

## Review

The skill presents each proposal with its proposed response and reason first, then the statement, evidence, and drafted artifact. You can approve it, redirect it to another response, revise it, dismiss it (shared with the team or kept local), or defer it.

- A **document** approval applies the edit as an uncommitted change and records a `documented` decision so the learning is not proposed again.
- A **fix**, **guard**, or **ask** approval writes a finding assigned to you, or to another Person you name. While its action item is open, the learning is not proposed again.
- Approval is refused while the files a proposal depends on have uncommitted changes, or when they changed after verification read them. The agent re-checks and asks you before passing `--rechecked`.

New shards, findings, document edits, and shared decisions are ordinary changes for you to commit.

## Workspace

The **Compounding** view, under Knowledge in the Notes sidebar, is the quicker way to review. It has three tabs.

- **Recommendations** lists pending proposals and open fix, guard, and ask findings as a checklist, grouped by response, with everything checked. Expand a row to read its draft, evidence, and cited excerpts, or the finding's note. Uncheck what you want to leave for later, then **Copy prompt** and paste it into your agent. The prompt names the items, not their text, and says you reviewed them and approve publishing them. The agent approves each proposal as drafted with the compounding skill, applies the document edits, implements the fixes and guards, checks off the action items it completes, and finishes on one branch for one pull request. Anything a script refuses comes back to you one item at a time.
- **Health** shows the friction report and the last mine run's spend and outcome. Each row opens that item on its own page.
- **Memory** lists the approved context shards and findings, and the shards that need re-verifying.

**Remove** changes only your own review. It dismisses a proposal locally, with a quick reason, or hides a finding from your workspace. A proposal redrafted since you reviewed it must be reviewed again before removal. A teammate still sees both, and hiding a finding never checks off its action item; only finishing the work does. To share a dismissal with the team, dismiss it through the skill with `--shared`.

The workspace also lists findings your teammates approved and committed. Their evidence came from your teammates' sessions and stays on their machines, so those findings show their note but no source excerpts, and their family counts are approximate.

The workspace runs the module's actions in `.rhizome/modules/compounding/actions/`, which need a trusted repository (`rzm trust`).

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