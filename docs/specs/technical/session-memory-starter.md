---
type: TechnicalSpec
id: SPEC-0124
aliases:
  - SPEC-0124
  - Session memory starter
summary: "An opt-in session-memory starter mines a developer's local Claude Code and Codex transcripts for what slows agents down: corrections, hard-won facts, and questions agents keep researching. It screens them with Jev, verifies candidates against the current code, and routes each one to the response that removes the friction: a remembered context shard, a documentation edit, a fix, a guardrail, or a question for an owner. Nothing enters the repository until a person approves it. Agents find shards through a lookup workflow and managed guidance, and optional harness hooks deliver them when an agent reads or edits the files they apply to."
spec-status: proposed
last-updated: 2026-10-22
---

# Session memory starter

## Summary

What an agent or a human learns during a coding session is lost when the next session starts with a fresh context window. Humans correct the same mistakes again, and agents spend hundreds of tool calls rediscovering the same facts. The `session-memory` starter turns a developer's past agent sessions into reviewed improvements to how well agents work in the repository.

Not every learning should become a memory. The first labeled run showed that most recurring friction comes from something the team can change: dead code that misleads agents, a flaky test, a bad default, a rule nobody wrote down. A note that teaches agents to work around such a problem preserves it. So each finding is routed to one of five responses, and only lasting facts and deliberate design become remembered shards:

- **Remember:** a lasting fact or deliberate design an agent cannot see from the code becomes a context shard.
- **Document:** a rule that exists but is unwritten, or a document that is wrong, becomes a proposed edit to the canonical document.
- **Fix:** a defect, dead code, a misleading default, or a confusing tool becomes an action item with a proposed fix.
- **Guard:** a rule agents keep breaking that a machine could enforce becomes an action item for a check, a test, or a blocking hook.
- **Ask:** unclear or disputed intent becomes a question for its owner, whose answer becomes a decision, an edit, or a dismissal.

The design follows the pipeline Boundary and HumanLayer described in [Why AI Coding Agents Need a Memory Pipeline](https://www.youtube.com/watch?v=mAKUL12h1n8). That pipeline uses three lenses: what agents found out, what humans corrected, and what agents kept researching. Cheap classifiers screen messages before a stronger model judges them, a learning is kept only when it recurs, and each one cites the sessions behind it. The episode left two problems open: making repeated runs incremental, and noticing when a memory goes stale. This spec answers both and adds a third requirement the episode assumed: an approved learning has to reach the agent at the moment it matters.

The starter has two halves with a hard boundary between them:

- **Everything unreviewed stays on the developer's machine.** Transcript digests, screened episodes, candidates, and proposals live in ignored local state under `.rhizome/session-memory/`.
- **The repository holds only what a person approved.** Each approved learning is a `ContextShard` note in `docs/memory/context-shards/`, and the repository also records decisions not to keep a learning, so teammates are not asked again. A tracked shard is in force by definition. Retiring it removes the note and records the decision.

Approved shards reach agents in two layers. The baseline works in every host, including T3 Code: a `lookup` workflow returns the current shards for a task, files, or a command, and managed `AGENTS.md`/`CLAUDE.md` guidance tells agents when to call it. Each shard also declares a glob `code-anchors` entry, so Rhizome's own file context and search find it. The second layer is optional push delivery. Claude Code and Codex both run project hooks around tool calls and accept context from them ([Claude Code hooks](https://code.claude.com/docs/en/hooks), [Codex hooks](https://learn.chatgpt.com/docs/hooks)), so a delivery hook can add the shards for a file when an agent reads or edits it. Hooks stay off until the person turns them on, and they leave draft only after delivery is confirmed live in each host.

The starter builds on [[init-template-architecture]], [[core-template-and-template-dependencies]], [[saved-query-recipes]], [[code-mode-skill-scripts]], and [[code-intel|code anchors]]. It is one `session-memory` skill whose workflows are Node scripts that call `rzm` subcommands. They are not code-mode scripts, because code mode caps each call at five minutes and extraction and grouping calls run longer. Scripts own transcript parsing, model calls, checkpoints, budgets, validation, local state, note writing, and delivery. The agent picks the workflow and explains results, and a person approves what is published. Decisions run on Jev through the existing integration (`rzm agent evaluate-batch`). Extraction and verification run through one new Rhizome operation, `generate`, with each model role bound to a concrete model per run.

## Goals

- Recover learnings that would otherwise be lost: human corrections, hard-won facts about the code, and questions agents keep researching.
- Remove friction at its source. Route each finding to the response that ends it, and remember only what an agent cannot learn from the code or its documents.
- Show where agents lose the most effort, and whether a fix removed the friction it targeted.
- Propose only learnings that are true of the current code, not already documented, not obvious from reading the code, and supported by independent evidence.
- Keep a human in charge. Nothing the pipeline produces enters the repository until a person approves it, and hooks deliver a shard only while its content matches what was approved. A hand edit to a tracked shard is an ordinary repository change, reviewed like any other file, and verify mode flags it.
- Get approved shards to agents in Claude Code, Codex, and T3 Code before consequential work: through lookup and guidance everywhere, and through hooks where a host supports them.
- Make repeated runs incremental and crash-safe, and let decisions made by one developer spare teammates the same review.
- Detect when the files behind an approved shard have changed, so it is re-verified rather than trusted.
- Keep cost low: Jev for decisions, the cheapest model that holds quality for each role, and no model calls when nothing changed. Prove with held-out measurement that screening does not lose learnings people accept.

## Non-Goals

- Sharing transcripts or unreviewed candidates between developers, or any central store. Recurrence is therefore counted per developer.
- Approving, dismissing, or retiring shards automatically, or choosing a finding's response without a person.
- Editing `AGENTS.md`, `CLAUDE.md`, skills, documentation, or code automatically. The skill proposes such edits, and a person applies or approves them.
- Filing findings in an external tracker. Findings stay in the repository as notes with action items.
- Scheduled or background runs.
- A review UI or a new storage engine. Review happens in the skill's conversation. The only new `rzm` surface is the generic `generate` operation.
- Mining or delivery for harnesses other than Claude Code and Codex.
- Detecting staleness by replaying sessions against newer models.
- Enabling the starter by default.
- Changing a model binding without a person's approval.

## User Stories

### US1 - Mine recent sessions into proposals

- id:: ^SPEC-0124-US1
- summary:: A developer runs the session-memory skill and gets local proposals drawn from their recent Claude Code and Codex sessions for this repository, each supported by independent evidence and checked against the current code.
- status:: ready

#### Acceptance Criteria

- The skill finds the developer's sessions for this repository, including subagent transcripts and sessions from current and deleted worktrees. It excludes the session running the skill, and reports sessions read, skipped, and unrecognized, with reasons.
- Every message records its role, transport, origin (`person`, `agent`, `harness`, or `unknown`), and how confident the origin is (`confirmed`, `likely`, or `unknown`), with the evidence for each. Only `person` messages feed the corrections lens.
- Sessions are grouped into families: a root session together with its subagents, resumed continuations, spawned workers, and sessions it delegated through tools or started through a CLI (`codex exec`, `claude -p`). Recurrence counts distinct families, never transcript files.
- Candidates are verified by tier. Tier A (two or more families, or a correction whose origin is `confirmed` person) is always verified. Tier B (one family, a claim the code can confirm) is verified up to a per-run budget, in the order Jev ranks them. Tier C (one family, a claim the code cannot confirm, such as environment, workflow, or behavior) is held until another family supports it.
- Review shows each proposal's tier, so a reviewer knows when a learning was seen once and confirmed only in the code.
- Each proposal has a kind (`rule`, `fact`, or `where_to_find`), a statement of at most 400 bytes, the reason it matters, the files it applies to, evidence references, and a verification record naming the code locations checked and the documentation searched.
- Verification also proposes a response: `remember`, `document`, `fix`, `guard`, or `ask`, with a one-sentence reason. A proposal that only helps agents work around something the team could change is marked as such and gets `fix`, `guard`, or `document` instead of `remember`. A `document` proposal names the document to change and drafts the edit; a `fix` or `guard` proposal names the code or check to change and sketches the change; an `ask` proposal names the decision and, when the evidence shows one, its owner.
- A proposal whose evidence contradicts the current code or its tools is rejected as false, even when several sessions repeated it.
- A candidate that is false for the current code, already documented, or obvious from reading the files it applies to is not proposed. When the answer is already documented, the candidate becomes a `where_to_find` proposal whose target is that document.
- Credential-shaped values are redacted before any text is truncated, written to disk, or sent to TypeSafe. No transcript text appears in tracked files.
- The run reports Jev usage, episodes kept versus read, and candidates proposed, merged, held for more evidence, suppressed by a recorded decision, or rejected, each with a reason.
  verification:: synthetic fixtures for every observed transcript record shape, plus measured runs over this repository (Batches 2–4).

### US2 - Review and promote

- id:: ^SPEC-0124-US2
- summary:: A developer reviews proposals in the skill's review mode, accepts or changes each one's proposed response, and approves, revises, dismisses, or defers it. Approving a `remember` proposal writes a tracked shard; approving any other response writes a tracked finding with an action item.
- status:: ready

#### Acceptance Criteria

- Review presents each proposal with its proposed response and reason first, then its statement, kind, applies-to patterns, target, evidence summary (families, tool calls spent), and verification record. On request, it opens a cited source message, redacted, from the local transcript.
- The reviewer can change the response. Review asks one question per proposal and attaches the drafted edit, fix sketch, or question, so the common case is accepting or redirecting, not writing.
- Approving a `document` response applies the drafted edit to the named document as an ordinary working-tree change for the person to commit, and records the outcome so the learning is not proposed again.
- Approving a `fix`, `guard`, or `ask` response writes one `AgentFinding` note under `docs/memory/agent-findings/`, holding the response, statement, reason, evidence summary, the proposed change or question, and one action item. The action item is assigned to the reviewer unless the reviewer names another `Person`. The learning is not proposed again while the action item is open.
- Approving asks the reviewer to confirm that the statement is fit to publish in this repository. It then writes the `ContextShard` note in one atomic step, with reviewer, date, approved-content hash, verification fingerprint, and derived `code-anchors`.
- Approval and renewal are refused while any fingerprinted file has uncommitted changes or unresolved merge stages, so the fingerprint always describes committed content the reviewer could see.
- Approval and revision are refused when the applies-to patterns match more than 50 tracked files and more than a tenth of them, unless the reviewer confirms the breadth. A broad shard reaches nearly every agent; in the development runs, three proposals matched 36–43% of tracked files while the median matched 0.5%.
- Dismissing records a decision with its reason. The reviewer chooses whether the decision is shared through the repository or kept local.
- Deferring keeps the proposal local for a later review.
- Text is scanned for credentials again before anything is written to a tracked file.

### US3 - Approved shards reach agents

- id:: ^SPEC-0124-US3
- summary:: An agent in Claude Code, Codex, or T3 Code finds the approved shards for its task, files, or command through the lookup workflow and managed guidance. Where the person turned hooks on, they also deliver a file's shards when the agent reads or edits it, without repeating a shard the agent still has.
- status:: ready

#### Acceptance Criteria

- Lookup takes a task description, file paths, or a command and returns the matching current shards, rules first, within a size budget. It works without hooks in every host.
- The managed `AGENTS.md`/`CLAUDE.md` block tells agents to run lookup before editing files or running commands that shards may cover.
- Hooks are optional. They are installed through the Rhizome hook dispatcher only when the person turns them on, and status reports which hosts have them.

The rest of this story applies where hooks are installed.

- Hook context reaches the model on its next step after the tool call that triggered it; it cannot change a tool call the model has already generated. Reads are therefore the preventive path, and edits are the corrective one.
- Claude Code: a `Read`, `Edit`, `Write`, or `MultiEdit` of a file an approved shard applies to adds that shard to the context of the agent or subagent that made the call. Claude Code requires a read before an edit, so shards for an existing file arrive before the agent edits it. For a new file written without a read, they arrive after the write, and the agent can correct it. The same shard revision is not added again in that agent's context until a compaction or clear.
- Codex: once the project and the hook definition are trusted, a recognized read in a `Bash` command adds the shards for the files it reads. The recognized forms are `cat`, `sed -n`, `head`, `tail`, `nl`, and `rg`/`grep` with file arguments, optionally after a `cd`. A folder argument, as in `rg -n TODO pkg/store`, adds the shards for the tracked files beneath it. An `apply_patch` adds them after the patch. Codex's hook payload names the session's `cwd` but not the directory a command ran in. Relative paths resolve against the hook's `cwd`, plus any `cd` in the command; a call that ran in another working directory may be missed or attributed to a same-named file under that `cwd`. A read that names no existing file is dropped to reduce noise. When the payload carries an agent ID, a shard is added at most once in that agent's context, as in Claude Code. Without one, it is added at most once per turn, a wider repeat, because the turn is the narrowest scope that never hides a shard from a subagent that shares its parent's session ID.
- Batch 0 measures what agents actually do after an edit that came before any delivery: a first-operation patch and a new-file write. If agents break rules there, a setting that refuses the first unread edit of a file with rule shards, once per agent context, is evaluated with Drew.
- Only shards whose content still matches their approved-content hash, and that are not stale (US5), are delivered by hooks or returned by lookup. Delivered text names the shard file and the date it was last verified.
- Delivery adds at most five shards (about 4,000 characters) per call, rules first. A compacted subagent's next tool call, which also brings the shards that apply everywhere, selects from those and the call's own shards together within the same limit, rules first; file shards that do not fit arrive with later calls. A hook never blocks or fails a tool call. Every error, including a timeout, results in no output, an exit status of zero, and a bounded local diagnostic entry.
- Setup reports, for each harness, whether the hook is installed, whether Codex trusts the project and the hook, and when delivery last succeeded.
- An approved shard with no applies-to pattern and no command applies everywhere. It is delivered at session start and at subagent start, and again after a compaction or clear (to a compacted subagent, with its next tool call, where the host reports the subagent's compaction), and the skill also proposes an `AGENTS.md` edit for it. When more than five apply everywhere, the delivery says how many more there are and gives the lookup command, with `--skip`, that shows exactly those, and approving the sixth warns the reviewer.
- Rhizome file context for a code file or a Markdown note returns its approved shards through their glob code anchors, as does search for a code file. Unlike lookup and hooks, file context does not withhold stale shards; each shown shard names its verification date.

### US4 - Later runs and teammates build on earlier decisions

- id:: ^SPEC-0124-US4
- summary:: Later runs process only new messages, keep single-sighting candidates until they recur, and never re-propose what any teammate already approved or dismissed without materially new evidence.
- status:: ready

#### Acceptance Criteria

- Each pipeline stage records its progress, and an interrupted run resumes without losing or duplicating work. Jev items that did not finish or whose outcome is uncertain are retried; items that succeeded are not.
- A tier C candidate, and a tier B candidate beyond the run's budget, is held in the local pool. A later run can verify it when another family supports it or budget allows.
- The reduce pass loads every recorded decision and every `AgentFinding`, tracked and local, directly from their files and compares candidates against them. It finds approved shards and documentation through semantic search. A candidate that matches an approved shard, a decision, or a finding whose action item is open is not proposed. A candidate whose new evidence contradicts a dismissal reason is proposed together with that reason.
- Two developers who approve the same learning on different branches produce a duplicate that the next run detects and offers to merge. Each decision event is its own file, named by learning and event, so concurrent decisions add separate files instead of editing the same one. The latest decision for a learning wins.
- A second run over the same window changes nothing.

### US5 - Stale shards are re-verified

- id:: ^SPEC-0124-US5
- summary:: When the files behind an approved shard change or disappear, the skill's verify mode surfaces the shard for re-verification, and the reviewer renews, revises, or retires it.
- status:: ready

#### Acceptance Criteria

- Each shard records a verification fingerprint over its matched files, its `where_to_find` target, and the locations in its verification record. Verify mode lists shards whose fingerprint changed, whose patterns match no file, whose content no longer matches the approved-content hash, or whose derived code anchors drifted. A shard has no fingerprint only when it has no matched files, no target, and no checked locations. A shard with no patterns is listed once its verification is older than 30 days.
- For each listed shard, the agent re-checks the statement against the code. The reviewer then renews it (new fingerprint and date), revises it (a new approval), or retires it (the note is removed and a decision recorded).
- A changed fingerprint marks a shard stale. Stale shards stay in the repository but are withheld from hooks and lookup until they are renewed or revised.

### US6 - Keep model choices current

- id:: ^SPEC-0124-US6
- summary:: A developer runs calibrate for one role. The workflow measures the current binding against the other available candidates and proposes a profile change, which the developer can approve or decline.
- status:: ready

#### Acceptance Criteria

- Calibrate measures one role over a labeled set. When at least 10 approved shards still have local transcripts, the set is the developer's own: the approved shards and recorded decisions, replaying the episodes and clusters their evidence cites. Otherwise it is a shipped synthetic fixture. The report names the set and its label count. Quality measured on the fixture can only support changes driven by cost or latency.
- Calibrate estimates its cost before any model call and stops at its budget.
- For each candidate, the report shows quality, input and output tokens, cost where a price is known, and median seconds per call. For extract, quality is the labeled learnings found, as judged by the frozen `match` binding. For verify, it is agreement with the recorded human decisions. Candidates that match no label are reported as unlabeled, never as misses.
- The current binding runs twice, the second time without the result cache, and the difference is reported as the noise floor.
- A profile diff is proposed only in two cases: a candidate beats the current binding by more than the noise floor, or it matches within the noise floor for at least 25% less cost or latency. The diff names its evidence file, and nothing is written until the developer approves it.
  verification:: a fixture run in CI with a fake `generate`; one live run per provider recorded in the effort.

### US7 - See where agents lose effort, and whether fixes worked

- id:: ^SPEC-0124-US7
- summary:: A developer asks for a friction report and sees open findings and unreviewed proposals ranked by the agent effort they cost, plus completed fixes and whether the friction came back.
- status:: ready

#### Acceptance Criteria

- The report ranks open `fix`, `guard`, and `ask` findings and pending proposals by evidence: families, sessions, and tool calls spent, with the dates first and last seen.
- For each finding whose action item is complete, later runs check whether new episodes still match its learning, and the report marks it `resolved` (no recurrence since completion) or `recurring`, with the episodes that matched. A recurring finding is offered for review again.
- The report reads only local state and tracked notes, makes no model calls, and contains no transcript text.
  verification:: a fixture with an open, a resolved, and a recurring finding.

## Requirements

### Packaging and installation

- The starter MUST be named `session-memory`, install from `pkg/app/cli/init/templates/starters/session-memory/**`, require `core`, and be marked as an optional addon that no starter activates by default.
- A first run MUST accept it in `--addons`.
- An existing configured repository MUST be able to add or remove it without changing any other addon choice, and the change MUST survive later init reruns and settings edits. Adding moves the id into `addons.enabled` and out of `addons.disabled`. Adding an ejected addon is refused with an instruction to use `--restore`.
- It MUST install a template-owned ontology file, query recipes, a view of approved shards, the `session-memory` skill and its scripts, and a managed `AGENTS.md` block. The block routes session-memory work to the skill, tells agents when to run lookup, and asks agents to state, in one sentence in their reply, any non-obvious fact an investigation turned up, because mining reads replies and drops tool output.

### Skill and model calls

- The skill MUST expose workflows for setup and status, mine, review, lookup, maintain, and calibrate through one dispatcher over shared stage scripts.
- Mine MUST be resumable and MUST stop at its spend budget. Setup MUST estimate a run's cost before it starts.
- Model roles (`decide`, `extract`, `verify`, `synthesize`, and `match` for calibration) MUST be bound through the model profile, frozen per run, and recorded in the run's provenance. `decide` uses Jev.
- Rhizome MUST provide one `generate` operation, as both `rzm agent generate` and `rzm.generate`, backed by `pkg/harness`. It takes a provider, model, effort, prompt, and optional output schema; allows no tools; persists no session; and returns the output, token usage, and the model that actually ran. A provider whose CLI cannot remove every tool MAY run only when its configuration removes the tools it can, including sub-agents and note writing, and a read-only sandbox with no network and no approvals bounds the rest. Otherwise it MUST report itself unsupported rather than run with broader permissions. Codex qualifies only through its app-server. The result MUST name the policy that applied: `none` or `sandboxed`. When the caller names no provider, `generate` MUST use the user's `agent.harness` setting, or else the only provider installed and signed in, and otherwise fail and name the choices.
- Every model result MUST be cached by the hash of its prompt, model, effort, schema, and input, separately from a ledger of attempts, so an unchanged rerun makes no model calls.
- Skill scripts MUST be dependency-free Node ES modules for Node 20 or later. Setup MUST check the Node version.
- Script tests and fixtures MUST NOT be installed downstream. A Go test MUST run them, read their files so Go's test cache sees changes, and fail rather than skip in CI when Node is missing. CI MUST provide Node to the Go test jobs on Linux and Windows.
- Verification MUST include the build, `rzm init`, and `rzm init --check` gate from `docs/engineering/quality-gates.md`.

### Model profile

- Each role MUST resolve from an ordered list of candidates. Each candidate names a provider (`claude`, `codex`, or `typesafe`), a model family and the version it was measured at (for example Haiku 5.5), and the provider's own effort value.
- A run MUST use only the providers the harness running the skill can reach, and MUST NOT move transcript text to another provider. In Claude Code that means `claude`; in Codex, `codex`. Only a harness that reaches both MAY choose between them, in profile order. Jev (`typesafe`) is the exception, because screening sends it passages in every harness.
- The run MUST take its provider from, in order: the harness the calling agent names (the skill tells the agent to pass the harness it is running in); the user's Rhizome agent setting (`agent.harness`, which the desktop agent chat already uses); and the only provider installed and signed in. Otherwise it MUST stop and ask which to use. Only a local profile listing both providers lets a run choose between them.
- When the provider's catalog has a newer version of a candidate's family than the profile names, and the profile has no entry for that version, the resolver MUST use the newest version at the candidate's effort, and report it as unmeasured.
- When no version of a candidate's family is available, the resolver MUST fall back to the role's next candidate from the same provider (for example Haiku to Sonnet), never to another provider's.
- The starter MUST ship its default profile as `models.json` in the `session-memory` skill. The profile has a `version`, and each candidate has either a reference to the measurement that chose it or a `provisional` flag. A change to the shipped profile MUST update `references/models.md` in the same change. A test MUST check that every role has a candidate for each provider, or an explicit `none` with a reason.
- A developer MAY override the profile in `.rhizome/session-memory/models.json`. A run MAY override a role with `--model <role>=<provider>:<model>:<effort>`. A run flag takes precedence over the local file, and the local file over the shipped profile. An override replaces a role's whole candidate list.
- When a run starts, the resolver MUST select, for each role, the first candidate that meets three conditions:
  - Its provider is reachable from the running harness, and `rzm harness status` reports it as installed and signed in.
  - Its model is in that provider's catalog, or a one-call probe with a minimal schema succeeds. Probe results are cached by provider, model, effort, and CLI version.
  - Its effort is listed for that model, when the catalog lists efforts.
- When it skips a candidate, the run MUST report the substitution and the reason in the cost estimate and the run report. If no candidate for `extract` or `verify` is available, the run MUST stop before any model call and name the role and what was missing.
- The resolved bindings MUST be written to the run manifest when the run starts, and a resumed run MUST use them. If a frozen model becomes unavailable, the run MUST stop in a resumable state rather than switch models.
- Provenance MUST record the following for each role:
  - the profile version and the hash of each profile file read
  - the requested and the reported model
  - the CLI version
  - how the candidate was chosen (`default`, `local`, `flag`, `newer-version`, or `fallback`)
  - each skipped candidate and its reason
- Setup and status MUST show the resolved binding for every role and name every provider that will receive transcript text.
- The skill MUST NOT write a profile file except to apply a diff that a person approved in the calibrate workflow.

### Tracked data

- A `ContextShard` note MUST live in `docs/memory/context-shards/`, named by kind and a slug, with no sequential identifier.
- Its frontmatter MUST hold:
  - `kind`
  - `summary` (the statement)
  - `applies-to` (patterns)
  - `commands` (literal command text that lookup matches; optional)
  - `target` (an existing document; required for `where_to_find`)
  - `reviewed-by` (a `Person`) and `reviewed-on` (`Date`)
  - `approved-hash`, over kind, statement, sorted applies-to, sorted commands, and target
  - `verified-on` (`Date`) and `verified-fingerprint`
  - `families` (`Int`) and `tool-calls` (`Int`)
  - `code-anchors` derived from `applies-to` under the `any` language key
- Its body MUST hold `## Why it matters`, a `## Verification` list of checked locations, and an `## Evidence` list whose items name the harness, session, message, date, lens, and tool calls. Evidence is frozen at approval and includes no transcript text.
- `applies-to` patterns MUST use one dialect: literal paths, `*`, `**`, and `?`. A test MUST prove that the delivery script's matcher and Rhizome's anchor matcher agree on a shared case table.
- An `AgentFinding` note MUST live in `docs/memory/agent-findings/`, named by response and a slug, with frontmatter holding the response (`fix`, `guard`, or `ask`), `summary` (the statement), `learning-hash`, `applies-to`, `reviewed-by`, `reviewed-on`, `families`, and `tool-calls`. Its body MUST hold `## Why it matters`, `## Proposed change` (or `## Question`), `## Evidence` in the shard's evidence form, and one action item written as an `#action-item` line with `assignee::`, as the `action-items` starter defines it. The line is written the same way when that add-on is disabled.
- A decision event MUST be one JSON file under `docs/memory/context-shards/decisions/`, named `<learning-hash>--<event-id>.json`, holding the statement, kind, applies-to, decision (`dismissed`, `retired`, or `documented`), reason, decider, and date. A `documented` decision names the document it changed. Decisions are not notes, so search does not surface them; the skill reads them directly.
- Every tracked write MUST go through `shards.mjs` as one atomic replacement, and MUST be refused when the file's content differs from the hash in the change plan.

### Local data and extraction

- All unreviewed data MUST live under `.rhizome/session-memory/`, created with owner-only permissions on macOS and Linux. On Windows, which has no owner-only mode bits, it inherits the checkout's access control, and the skill tells the person before a first run there. A lock MUST guard every write.
- Session discovery MUST match by recorded `cwd` and, for Codex, by `git.repository_url`. It MUST cover the repository root, current worktrees, and configured path patterns for deleted worktrees, and MUST skip Codex guardian-review sessions.
- Episodes MUST share one representation that carries:
  - family and lineage
  - the lenses it may serve
  - origin and confidence
  - redacted, middle-shortened text
  - context: the session title, the opening task, and the preceding turn
  - files read or edited
  - tool calls spent
- Research episodes MUST include main-thread investigation spans as well as subagent tasks and reports, and pass through the same screening.
- The extraction watermark MUST advance only through the end of the last complete turn: a person message through the assistant's final reply before the next person message. An unfinished trailing turn is re-read on the next run, so no episode is split across runs.
- Redaction MUST run on full text before shortening and cover escaped and multiline values. The scripts MUST NOT read `.env` files, Rhizome credential configuration, or credential environment variables.
- Extraction MUST treat transcript content as data, never as instructions. Only the agent running the skill MAY write local state or tracked files. Subagents return candidate data in their final reply, and the main agent validates and persists it.
- The candidate validator MUST enforce the schema and length limits itself and return a precise retry message.
- Matching against approved shards and documentation MUST use semantic search, and matching against decisions MUST compare the loaded decision files.

### Screening

- Screening is the gate in front of extraction: only episodes it keeps reach the extraction model. A decision model makes this affordable at every run; an extraction model reading every episode does not.
- Screening MUST use `rzm.evaluateBatch` with a pinned Jev model, explicit thresholds, and per-item checkpoints, following `scripts/repo-audit/run.mjs`.
- Screening MUST score passages, not whole episodes. A passage is a paragraph or list item of at most 700 characters, with short pieces joined. The screen keeps the highest-scoring passages up to a share of the characters, 30% by default, and the extraction model sees only the kept passages of each episode. Jev asks two questions per passage: whether it states something reusable about the project, and whether it reports a surprise or a root cause. Passage screening found 72% more learnings than whole-episode screening at the same share of text, and a held-out check rejected adding more questions.
- If no TypeSafe credential is available, the skill MUST say so and MAY run unscreened over a window small enough to fit.

### Delivery

- Hook delivery MUST run as this starter's handler under the Rhizome hook dispatcher ([[agent-surface-integration-modes#^SPEC-0045-US5]]), which owns harness settings, payload parsing, path containment, deadlines, Windows launching, emitted-item state, and status. The handler covers session start, subagent start, and read, edit, and patch tool calls, plus shell calls with recognized reads. After a shell command whose text starts with an approved shard's command prefix, it delivers that shard; the handler publishes its current prefixes so the dispatcher starts it for no other shell call.
- The handler MUST select shards with the same function as lookup: current shards only, rules first, at most five shards and about 4,000 characters, or the dispatcher's remaining allowance if smaller. On a tool call marked `startDue`, one selection draws from the shards that apply everywhere and the call's own together, so a file rule comes before facts that apply everywhere. Item IDs are shard revisions (path and content hash).
- A folder path MUST match a shard when a tracked file beneath it, read from the Git index rather than the filesystem, matches the shard's patterns. A folder holding more than 50 tracked files and more than a tenth of them, the breadth approval questions, MUST match nothing, so a search over most of the repository does not select every shard. Lookup's `--files` treats a folder the same way.
- It MUST leave out the shard revisions the dispatcher reports as already emitted before applying its limits, so delivered shards never hide later matches.
- It MUST read shard files directly rather than through `rzm`. It MUST cache parsed shards only after a consistent read, keyed by the shard directory's listing and modification times, and MUST check eligibility (approved hash and fingerprint) against the current Git index on every call. Every `git` call MUST have a timeout inside the handler's deadline.
- Shards that apply everywhere MUST be delivered on session start and subagent start, including a session start after a compaction or clear and a subagent's next tool call after it compacts, as far as the host reports that compaction ([[agent-surface-integration-modes#^SPEC-0045-US5-AC6]]: verified in Codex's source, expected but not yet verified live in Claude Code). When more than the limit apply, the delivery MUST end with one line naming how many more apply and the lookup command, at the installed script's absolute path so it runs from any directory, quoted so the agent's shell takes it literally (for Claude Code on Windows, which may run Git Bash or PowerShell, one form for each, labelled), and with `--skip`, that returns the ones not delivered.
- Freshness covers the current Git index: an unstaged edit to a dependency does not withhold a shard. Patternless shards older than 30 days are flagged by `check` for maintenance and are still delivered.
- T3 Code Codex threads, where project hooks are not trusted, rely on lookup and managed guidance. No user-level hook runs repository scripts.

### Quality evidence

- Before any tuning, sessions MUST be split by family into a development window and a later held-out window, and the split recorded.
- Extraction rules, Jev questions, thresholds, and every model identifier (Jev, and the harness and model that extract and judge) and the model profile version MUST be frozen before the held-out evaluation.
- Each reference, screened, and threshold run MUST use its own checkpoints and candidate pool, seeded from the same frozen episode snapshot, and reference labels MUST be kept out of every judgment context.
- The held-out evaluation MUST measure, per lens, how many shards a reviewer accepted from an unscreened reference run that the screened pipeline still produces with sufficient evidence. It MUST also audit a sample of screened-out episodes and report the fraction kept, Jev usage, and the reference-run cost.
- The effort MUST record acceptance rates, dismissal reasons, and review time.
- The composed evaluation run MUST record, for each task, the relevant approved shards that file-pattern delivery missed and whether task search would have found them. Delivery on the person's prompt through search is considered only on that evidence.
- The effort MUST measure hook latency on cache hits, misses, cold starts, and failures; confirm hook delivery live in Claude Code, Codex, and Claude threads in T3 Code worktrees, including a GUI-launched host; and confirm lookup in T3 Code Codex threads.
- Every transcript measurement MUST be reproducible from a committed, content-free survey command, with its window and versions recorded.

## Open questions

None open. Decided:

- **Verification method (after M1):** scripts gather evidence packets, with up to two model-requested evidence rounds, and run no model-supplied commands. The packet verifier matched the tool-using verifier's recovered learnings (7 of 12) and agreement (81 of 94).
- **Delivery on command text (Drew, 2026-10-22):** the hook handler delivers after a shell command whose text starts with an approved shard's command prefix. The dispatcher starts the handler for a shell call only when the command matches a prefix in the list the handler last published, so other shell calls start nothing.
- **Tracked locations (Drew, 2026-10-22):** everything the starter tracks lives under `docs/memory/`: shards in `docs/memory/context-shards/<kind>-<slug>.md`, decisions in `docs/memory/context-shards/decisions/`, and findings in `docs/memory/agent-findings/<response>-<slug>.md`.
- **Guard enforcement (Drew, 2026-10-22):** an approved `guard` finding may carry a guard rule, which the dispatcher enforces before the tool call runs where the host allows it ([[agent-surface-integration-modes#^SPEC-0045-US6]]).
- **Mining window (Drew, 2026-10-22):** the last 7 days by default, and the first run asks before reading more than 200 sessions.
- **Publication:** this repository is public, so dogfood shards, findings, and shared decisions are committed only after Drew approves each one as fit to publish.

## Documentation plan

- A starter guide under the installed `docs/reference/guides/` explains the review loop, hook setup and trust, privacy boundaries, and TypeSafe usage.
- `docs/specs/product/init-starter-workflow.md` gains the optional addon and the flags for adding or removing an addon in an existing setup.
- The code-intel subsystem note records the `any`-keyed glob convention for context shards.
- `references/models.md` in the skill records each role's needs, its current defaults, and the evidence for them.
