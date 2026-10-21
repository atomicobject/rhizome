---
type: EffortNote
id: EFF-2026-10-11-15-21
aliases: [EFF-2026-10-11-15-21]
name: Session memory starter
created-at: 2026-10-11T19:21:34Z
status: active
plan-approved-by: Drew Colthorp
summary: Deliver SPEC-0124. An opt-in session-memory starter mines local Claude Code and Codex transcripts, screens messages with Jev, and records reviewed ContextShard notes that reach agents through approval-gated code anchors.
governing-specs:
  - "[[session-memory-starter]]"
  - "[[init-template-architecture]]"
  - "[[core-template-and-template-dependencies]]"
---

# Session memory starter

## Scope

Drew asked for a starter built on the memory pipeline in [Why AI Coding Agents Need a Memory Pipeline](https://www.youtube.com/watch?v=mAKUL12h1n8). Drew agreed to these decisions on 2026-10-11:

- a skill with bundled scripts, not a new command
- local mining, with only reviewed shards shared through Git
- redaction before extraction, with no transcript text in notes
- the existing Jev integration for screening

Drew also asked for an Astra High review. Round 1 returned "rework" with 20 findings; this plan answers them, and the responses are summarized under Execution Notes. Ideas borrowed from [OptMem](https://github.com/VictorTaelin/OptMem) and [UniiChat](https://gist.github.com/VictorTaelin/91837951a5ce5b38f341ec1ba1df6449):

- treating transcripts as data
- validator-enforced lengths with precise retry messages
- just enough context to resolve references
- opening a cited source ("zoom")
- subagents never writing memory
- asking agents to state discoveries in their replies

Starting facts, verified at `184889cb` unless marked observational:

- **Init.**
  - The starter registry comes from embedded `template.yaml` files.
  - `--addons` accepts only ids that another starter activates by default, and `chooseAddons` disables every default addon left off the list (`first_run_options.go:63`). Existing setups reject `--addons` and other first-run options (`run.go:122`). They support `--eject` and `--restore`.
  - Addons in `workflows.yml` `addons.enabled` survive settings edits.
  - Init copies every skill file and gives files under `scripts/` mode 0755. Embedding skips names that start with `.` or `_`.
  - Init writes no hook configuration.
- **Search and anchors.**
  - Ordinary note search does not filter by status or anchors (`query/execute.go:2063`), so any tracked note is searchable.
  - `NotesForFile` returns every note anchored to a file.
  - Glob anchors are language-agnostic (`anchors/types.go:94`); a Python-keyed glob matches TypeScript (`integration_test.go:148`).
  - Anchor validation checks only symbol anchors (`anchors/validate.go`).
- **Ontology.**
  - Query filters compare values; they cannot apply a stored glob to an input path (`query/root_filters.go:69`).
  - `@requiresWhen` reports issues and does not complete transitions.
  - Inline embedded fields follow the type's property case, which defaults to kebab.
  - `CTX` is taken by `DomainContext`.
- **Code mode.**
  - It runs the system `node` from the vault root with no sandbox.
  - It has no general note-creation or text-generation operation; `noteMove` rewrites links.
  - `rzm.evaluateBatch` takes 1–128 items, a concurrency of 1–32, and an optional pinned `model`, and returns a status per item (`succeeded`, `failed`, `not_started`, or `uncertain`) with summed usage.
  - `scripts/repo-audit/run.mjs` is the checkpoint precedent.
  - Go code-mode tests already run JavaScript under `make check`. No skill-script tests exist.
- **Hooks (vendor docs).**
  - Both harnesses support `PostToolUse` and `SessionStart` (sources include `compact` and `clear`) with `hookSpecificOutput.additionalContext`.
  - Claude Code shows `PreToolUse` context with the tool result. It identifies subagents with `agent_id`, keeps `${CLAUDE_PROJECT_DIR}` at the original root inside worktrees, and caps context at 10,000 characters.
  - Codex gives subagent hooks the parent's `session_id` and distinguishes them only by `turn_id`. It loads project hooks only when the project is trusted and the exact hook definition is trusted by hash; a changed definition is skipped until it is reviewed again. It caps output at about 2,500 tokens.
  - Default hook timeouts are 600 seconds in both.
- **Observational (2026-10-11, to be reproduced by the committed survey in Batch 1).**
  - One week of this repository's sessions was 120 transcript files: 37 Claude Code (18 subagents) and 83 Codex (35 exec, 20 guardian reviews, 15 spawned subagents, 13 interactive).
  - Assistant prose without tool output came to 0.49 MB.
  - Claude Code prompts typed in T3 carry `promptSource: sdk`, as programmatic prompts can.
  - A warm `query-recipe run` took 120–200 ms.

Out of scope: everything listed under SPEC-0124 Non-Goals.

## Spec Set (Frozen)

- [[session-memory-starter|SPEC-0124]], revision 2026-10-11, proposed.
- [[init-template-architecture|SPEC-0039]], revision 2026-05-06, proposed, for starter packaging.
- [[core-template-and-template-dependencies|SPEC-0061]], revision 2026-10-01, active, for the `core` dependency.

## Stories In Scope (Frozen)

- [[session-memory-starter#^SPEC-0124-US1|SPEC-0124 US1 — Mine recent sessions into proposed shards]].
- [[session-memory-starter#^SPEC-0124-US2|SPEC-0124 US2 — Review shards]].
- [[session-memory-starter#^SPEC-0124-US3|SPEC-0124 US3 — Approved shards reach agents working on that code]].
- [[session-memory-starter#^SPEC-0124-US4|SPEC-0124 US4 — Later runs build on earlier decisions]].
- [[session-memory-starter#^SPEC-0124-US5|SPEC-0124 US5 — Stale shards are flagged]].

## Spec Coverage Checklist

- [ ] US1 — families, origin model, episodes, recurrence gate, verification records, redaction, run report: fixtures plus Batches 2–4.
- [ ] US2 — review mode with atomic promotion, publication confirmation, decisions, rescanning.
- [ ] US3 — delivery proven by script tests and live in Claude Code, Codex, and T3 worktrees, plus Rhizome-native lookup through anchors.
- [ ] US4 — checkpointed stages, the local pool, decision suppression, duplicate detection across branches, an idempotent rerun.
- [ ] US5 — fingerprints, verify mode, renew, revise, and retire.

## Plan

### Decisions

1. **D1 — Only approved content is tracked.**
   - Unreviewed data lives in `.rhizome/session-memory/`. The directory is ignored and owner-only, and a lock guards every write. It holds the ledger, episodes, screening results, the candidate pool, proposals, review notes, hook state and cache, diagnostics, and eval data.
   - `docs/context-shards/<kind>-<slug>.md` holds approved `ContextShard` notes. The path is the identity, so there is no sequential identifier; a branch collision on the same slug is a real Git conflict about the same learning.
   - `docs/context-shards/decisions/<learning-hash>--<event-id>.json` holds one shared decision event per file. The learning hash covers the normalized kind and statement; the event ID combines the decider and a timestamp. Concurrent decisions add separate files, and the latest event for a learning wins.
   - There is no status field. Retiring a shard removes its note and records a decision. View-based status edits do not exist.
2. **D2 — Approval is bound to content.**
   - `approved-hash` covers kind, statement, sorted applies-to, and target. The hook delivers a shard only when its current content matches that hash.
   - The guarantee covers hook delivery only. Native search and file context index tracked files as they are. A hand edit is an ordinary reviewed repository change, and verify mode reports the hash mismatch. A test proves both behaviors on a changed summary.
   - Any change to delivered meaning goes through `shards.mjs revise`, which is a new approval. Evidence is frozen at approval. Duplicates found later merge through review: the survivor gets the union of evidence and the other shard is retired.
3. **D3 — Freshness is a fingerprint, not a date.**
   - `verified-fingerprint` is a SHA-256 over `git ls-files -s -z` entries for the matched files, the `where_to_find` target, and the paths in the verification record. A content-addressed fingerprint survives squash merges and branch switches, where a recorded commit would not.
   - Promotion and renewal are refused while any fingerprinted file has uncommitted changes or unresolved stages. That makes the index entries equal to what the reviewer saw.
   - A shard has no fingerprint only when it has no matched files, no target, and no paths in its verification record. Verify lists patternless shards when their verification is older than 30 days, whether or not they have a fingerprint.
   - A changed fingerprint makes the shard eligible for re-verification. Delivery continues, showing `verified-on`.
4. **D4 — One pattern dialect and anchors under `any`.**
   - `applies-to` allows literal paths, `*`, `**`, and `?`. One small JavaScript matcher serves the hook and verify mode.
   - A Go test runs the same case table through the JavaScript matcher and Rhizome's doublestar matcher.
   - The writer derives `code-anchors: {any: [glob: ...]}` from `applies-to`. A Go test proves that `any` glob anchors match Markdown, Go, and JavaScript files.
5. **D5 — The origin model.**
   - Each message records role, transport, origin, confidence, and basis.
   - `confirmed` person: Claude Code CLI interactive prompts.
   - `likely` person: a mid-session reply over the SDK transport in a session not marked as a subagent, exec run, or delegated task.
   - Session-opening messages in SDK sessions are `unknown`.
   - `agent`: Codex exec and subagent sessions, and Claude sidechains.
   - `harness`: injected markers, tool results, `isMeta`, and system prompt sources.
   - Only a `confirmed` correction may be proposed from a single family. Batch 2 measures how many accepted learnings this rule blocks, and Drew decides whether to loosen it.
6. **D6 — Episodes and families.**
   - A family is a root session plus its subagents, spawned workers, and resumed continuations, linked through Codex `parent_thread_id`, Claude subagent directories, and resume metadata.
   - The three episode lenses:
     - Correction: a person message plus the preceding assistant turn.
     - Discovery: an assistant reply plus its files and tool calls.
     - Research: a subagent task with its report, or a main-thread span of at least eight read-only tool calls before a reply.
   - Every episode carries its session's title, opening task, and preceding turn.
   - Recurrence counts distinct families.
7. **D7 — Pipeline stages are checkpointed.**
   - The stages run in order: discover, extract, screen, map, reduce, propose.
   - The session watermark advances only through the end of the last complete turn, and only after that turn's episodes are durably written. An unfinished trailing turn is re-read whole on the next run, so research spans are never split. A test splits a span across two runs.
   - Screening stores a status per item. Failed, not-started, and uncertain items are retried; succeeded items are not.
   - Map batches and the candidate pool are keyed by episode IDs, so replay is idempotent.
   - The session running the skill is excluded: by session ID when the harness exposes it, by any session whose latest person message invokes this skill, and by a 15-minute modification window.
8. **D8 — Judgment runs in two passes.**
   - Map: subagents, when the harness has them, turn episode batches into candidate data returned in their final reply. The main agent validates it with `candidates.mjs` (schema and lengths) and persists it.
   - Reduce: the main agent groups the pool, compares it with approved shards and docs through `rzm.semanticQuery`, and compares it with decisions by loading the decision files directly. It verifies each survivor in the code and records what it checked, then applies the recurrence gate.
   - Candidates held by the recurrence gate are written as `held` proposals, so calibration can show them to Drew separately.
   - The instructions treat transcripts as data, copy identifiers exactly, never overstate progress or certainty, and reject generic advice, personal preferences, plans never carried out, and anything obvious from the code.
9. **D9 — Delivery.**
   - Recipes:
     - `context-shards` returns path, kind, summary, applies-to, target, approved hash, and verification date for every shard; the script filters by path. The script follows continuation offsets until coverage is complete and caches only complete results. A test uses more shards than one page holds.
     - `context-shards-for-run` adds the evidence the reduce pass needs.
   - `deliver.mjs`:
     - Resolves the root from the input `cwd` with `git rev-parse --show-toplevel`, cached per `cwd`.
     - Normalizes touched paths: Claude `file_path`, Codex patch headers including moves and deletes, and bounded `Bash` forms.
     - Matches patterns, checks the approved hash, filters by the dedupe key, and returns at most five shards (about 4,000 characters), rules first.
     - Runs the recipe under a two-second deadline, kills it on timeout, and caches by directory key, publishing only when the key is unchanged.
     - Swallows every error, exits zero, and keeps a bounded diagnostic log.
   - Timing: hook context reaches the model on its next step after the triggering call. Reads are the preventive path and edits the corrective one.
   - Hook events:
     - Claude Code: `PostToolUse` on `Read|Edit|Write|MultiEdit`, keyed by session and `agent_id`. Claude requires a `Read` before an `Edit`, so shards for existing files arrive before the first edit; a new-file `Write` gets them afterward.
     - Codex: `PostToolUse` on `Bash` (recognized read forms) and `apply_patch`, keyed by session and `turn_id`.
     - Both: `SessionStart` and `SubagentStart` deliver shards with no patterns, and `SessionStart` with `compact` or `clear` resets dedupe.
   - An opt-in setting, off by default, refuses the first unread edit of a file with rule shards, once per agent context. It is evaluated in Batch 0 only if the measured first-edit behavior calls for it.
   - Hook commands are `sh -c` wrappers that run the script from the session's own worktree if it exists and otherwise exit 0. Hook `timeout` is 5 seconds.
   - `hooks.mjs install|remove|status` merges marker-identified entries and reports installation, Codex trust, last successful delivery, and recent errors.
10. **D10 — Installation.**
    - `template.yaml` gains `optionalAddon: true`. `--addons` and the desktop setup API accept optional addons; first-run semantics are unchanged, so users list every addon they want. No interactive or desktop UI lists optional addons yet.
    - Existing setups gain `--add-addon <id>` and `--remove-addon <id>` as starter-management options next to `--eject` and `--restore`.
      - Adding moves the id into `addons.enabled` and out of `addons.disabled`.
      - Removing does the reverse for a default addon, and drops an optional addon from `addons.enabled`.
      - Adding an ejected addon is refused with an instruction to use `--restore`.
      - Tests cover each transition.
    - The rerun label lists enabled addons. Interactive and desktop prompts that list optional addons are a follow-up.

### Batches

Each batch ends with its listed evidence recorded in Execution Notes.

- **Batch 0 — Prove the product's riskiest assumptions with a thin slice.**
  - First, a canary: a trivial hook script returning fixed context, installed by hand.
    - Prove it reaches the model in `claude -p`, `codex exec`, and Claude and Codex threads in T3 worktrees, including a fresh subagent in each harness and a Codex hook update that needs re-trust.
    - Record whether the model acts on the context: a read followed by an edit, a first-operation patch, and a new-file write. This decides whether the opt-in refusal setting is needed.
    - **Stop for Drew** if the canary fails in any environment, before building on it.
  - Build D10 with init tests:
    - a first run with `--addons action-items,session-memory`
    - adding and removing the addon in an existing repository while `action-items` is preserved
    - a settings edit that preserves it
    - rerun stability
    - the build, `rzm init`, and `rzm init --check` gate
  - Build the `ContextShard` ontology, the `context-shards` recipe, `shards.mjs promote|retire|check` with atomic writes and expected-hash refusal, D4 with its parity test, and `deliver.mjs` and `hooks.mjs` with script tests.
    - The script tests cover subdirectory `cwd`, paths with spaces, Windows-style paths, patch moves and deletes, `cd` prefixes, two worktrees at once, a parent with two subagents, simultaneous reads, a timeout, a missing script, the size cap, and hash mismatches.
  - Hand-write one approved shard and prove delivery live in five places:
    - `claude -p` in a scratch repository
    - `codex exec` with trusted project and hook, including after a hook update
    - a Claude thread in a T3 worktree
    - a Codex thread in a T3 worktree
    - Rhizome file context
  - Measure hook latency on cache hits, misses, cold starts, and failures. Target: p95 under 150 ms on a hit and under 2.5 s on a miss.
  - **Exit:** delivery works in every place, or each gap is recorded and US3 is adjusted with Drew.
- **Batch 1 — Survey, extraction, and the data split.**
  - Build `sessions.mjs survey`, a committed, content-free structure survey, and record its output.
  - Build `sessions.mjs extract` and `show` with fixtures for every observed record shape. They cover D5 origins (including automated prompts that look like ordinary user messages), D6 families and episodes, redaction before shortening (planted credentials, escaped and multiline values), and D7 stages, with a crash-and-resume test and current-session exclusion.
  - Before any extraction run, split families by root start time into a development window D (2026-09-27 to 2026-10-03) and a held-out window H (2026-10-04 to 2026-10-10). Record the split in `eval/manifest.json`.
- **Batch 2 — Reference run on D, and the go/no-go.**
  - Run map and reduce with draft rules over D, unscreened, producing proposals and separately listed `held` proposals. Record cost, time, and the extraction harness and model.
  - **Stop for Drew** to review the proposals: approve, dismiss with reason, or defer. Record acceptance rate, dismissal reasons, review minutes, and how many accepted items the single-family rule (D5) blocked.
  - **Go/no-go:** at least five accepted proposals and an acceptance rate of at least 40%. Otherwise revise rules and repeat once, or stop. Freeze extraction rules v1.
- **Batch 3 — Screening, tuned on D.**
  - Each threshold run uses its own checkpoints and candidate pool, seeded from the frozen D episode snapshot. Drew's labels never enter a judgment context.
  - Build `screen.js` and its questions. For each threshold, measure per lens how many D-accepted proposals the screened pipeline still produces with sufficient evidence, along with the fraction of episodes kept.
  - Freeze questions, thresholds, and the Jev model.
- **Batch 4 — Held-out evaluation on H.**
  - Run a reference extraction over H with frozen rules, and have Drew label its proposals. Then run the screened pipeline over H with its own checkpoints and pool, seeded from the same frozen H snapshot.
  - Report, per lens, how many accepted proposals the screened pipeline still produces with sufficient evidence. Audit 40 random screened-out episodes for missed learnings. Report the fraction kept, Jev usage, and cost against the reference run.
  - **Ship criterion:** screening keeps at least 90% of accepted proposals. Otherwise ship with wider thresholds or without screening, decided with Drew from the measured tradeoff.
- **Batch 5 — The skill, end to end.**
  - `SKILL.md` with `mine`, `review`, `verify`, and `setup` modes: current-user check, Node check, missing-key path, the run report, and publication confirmation.
  - On this repository: promote the shards Drew approved, run a second mine to prove idempotence, dismiss one shard to prove suppression, simulate a duplicate approved on another branch, and run verify after editing a matched file.
- **Batch 6 — CI, documentation, and closure.**
  - Add Node to the Go test jobs on Linux and Windows. Write the starter guide, the SPEC-0038 entry, the code-intel note, and the changelog.
  - Run independent review, then close the effort.

### Risks

- **Rejection quality decides usefulness.** A few bad shards teach people to ignore the rest. The Batch 2 and Batch 4 reviews measure this on development and held-out data.
- **The origin model may block valuable corrections.** T3 prompts cannot be confirmed as typed by a person, so corrections from T3 need two families. Batch 2 measures what this costs.
- **Codex trust.** Every hook update needs re-trust, and T3 worktrees may need it per path. Batch 0 measures this, and `status` makes it visible.
- **Transcript formats drift.** Unknown record shapes are counted and reported, never guessed, and the survey can be rerun.
- **Prompt injection.** Transcripts contain text written to steer agents. They are treated as data, subagents are read-only, and scripts do all writes.
- **Publication.** Redaction catches credential shapes, not arbitrary private details. The reviewer's publication confirmation is the final gate, and this repository is public.

### Documentation

Starter guide, the SPEC-0038 catalog entry, a code-intel subsystem note on `any`-keyed glob anchors, and a changelog entry.

### Validation

- `go test ./pkg/app/cli/init/... ./pkg/anchors/...`, which includes the Node script and parity tests.
- `make check` before each commit and `make check-full` in CI.
- The build, `rzm init`, and `rzm init --check` gate.
- `./scripts/rzm validate` after ontology and note changes.
- Live delivery checks (Batch 0), with measurements recorded in Execution Notes.

## Original Intended Delivery

Batches 0–6 on one branch, delivered in one PR with this effort's closure. Batch 0 ends with live delivery proven; Batch 2 ends at a go/no-go checkpoint with Drew; Batch 4 decides whether screening ships as tuned.

## Actual Delivered

- [PR #28](https://github.com/atomicobject/rhizome/pull/28) (open): the `session-memory` add-on with mining, review and approval, lookup, and maintenance; `rzm agent generate`; `--add-addon`/`--remove-addon`; anchored notes in Markdown-note file context; the hook dispatcher design in SPEC-0045 US5 (not built); SPEC-0082 US12 (draft). The effort stays active: the hook dispatcher and handler, command-text delivery, calibrate, and M5/M6 remain.

## Execution Notes

- 2026-10-11T19:21:34Z — Effort opened with SPEC-0124 drafted. Plan approval is pending.
- 2026-10-11 — Astra High review, round 1. Verdict: rework, with three blockers and fifteen major findings. Responses:
  - **Approval invariant and accidental commits (blockers 1–2):** only approved content is tracked, there is no status field, and approval is bound to content (D1, D2).
  - **Installing into an existing setup (blocker 3):** D10, tested in Batch 0.
  - **Codex trust:** setup reports install and trust state, tested after a hook update.
  - **Subagent dedupe:** keyed per agent context, reset on compaction (D9).
  - **Deadlines and caching:** D9.
  - **Path cases and rules before edits:** D9, with Batch 0 tests.
  - **Authorship:** the origin model with confidence (D5).
  - **Contaminated evaluation and episode-level recall:** the D/H split and shard-level retention (Batches 1–4).
  - **Research episodes and recurrence:** families and main-thread spans (D6).
  - **Ledger safety:** checkpointed stages (D7).
  - **Team merges:** path identity, per-file decisions, and duplicate merges (D1, D2).
  - **Approval bound to content:** D2.
  - **Freshness baseline:** fingerprints (D3).
  - **Redaction before shortening and the publication gate:** spec and Batch 1.
  - **Path recipe:** the recipe returns all shards and the script filters (D9).
  - **Node and CI:** the spec and Batch 6.
  - **Factual claims:** corrected in Scope.
  - **Anchor experiment:** replaced by the `any` convention (D4).
  - **Batch order:** delivery is proven first, and the data is split before any tuning.
- 2026-10-11 — Astra High review, round 3. Verdict: approve with one change. A patternless shard may skip the fingerprint only when it has no dependencies at all (D3). Applied. No new blockers or major findings.
- 2026-10-11 — Drew approved prototype-first execution with benchmarks, Astra reviews, and independent Opus 5.5 extra-high reviews.
- 2026-10-11 — Jev is blocked. `TYPESAFE_API_KEY` is set in Drew's login shell, but TypeSafe returns HTTP 403 Forbidden, including in `go test -tags live ./pkg/typesafe`. Screening benchmarks wait for a working key.
- 2026-10-11 — Transcript structure survey (content-free, last 14–21 days):
  - Codex uses code mode. 14,212 `exec` calls carry JavaScript that calls `tools.exec_command({cmd})` (17,438), `tools.apply_patch` (1,177, all `*** Begin Patch`), and `tools.write_stdin`.
  - Codex sessions: 122 subagent threads, 57 guardian reviews, 40 interactive roots, and 35 `codex exec` roots.
  - Claude Code tool calls: `Bash` 13,300, `Read` 873, `Write` 253, `Grep` 135, `Edit` 122. 70 of 143 files are subagent transcripts under their parent session's directory.
  - Delivery consequences: Claude Code delivery must also parse `Bash` reads, and the Batch 0 canary must check whether Codex hooks fire for tool calls nested inside a code-mode `exec`.
- 2026-10-11 — Deviation: the development window D is 2026-09-13 to 2026-10-03 (three weeks), not one week. One week held only 37 families. D is 74 families and 311 sessions. H stays 2026-10-04 to 2026-10-10 (101 families, 195 sessions) and is strictly later.
- 2026-10-11 — Batches 1–2 on D, reference run with v0 rules, Opus 5.5 at high effort through `claude -p` (no session persistence):
  - **Extraction:** 1,848 episodes (223 correction, 1,373 discovery, 252 research), with 88 redactions.
  - **Map:** 87 batches, 621 candidates (524 fact, 63 where_to_find, 34 rule), about $37 at list price.
  - **Group:** 444 clusters; 48 eligible (at least two families, or a confirmed correction), 396 held. About $3.40.
  - **Verify** (48 eligible plus 30 sampled held, read-only tools in this worktree): 22 proposed (16 eligible, 6 held) and 56 rejected (35 already documented, 11 false now, 5 obvious from code, 3 generic, 2 other). About $17.50.
  - **Findings:**
    - Facts go stale fast here, so checking against the code is essential.
    - Most frequently recurring friction is already documented, yet agents kept relearning it. The open question is whether pointer shards should deliver existing docs at the file.
    - The recurrence gate blocks some learnings the verifier proposes: 6 of 30 sampled held clusters.
  - **Haiku screening proxy** (evidence retention for the 48 eligible clusters; the method is a stand-in while the Jev key fails):
    - 0.2 threshold: keeps 72% of episodes and 100% of clusters.
    - 0.5 threshold: keeps 56% of episodes and 94% of clusters.
    - Haiku is not selective enough to meet the 35% kept-fraction target.
- 2026-10-11 — D rerun with extractor and rules v1 (runs/D-v1; verification pinned to `184889cb` in a clean checkout):
  - **Volume:** 1,581 episodes (37 families), 596 candidates, 413 clusters (tier A 35, B 327, C 51).
  - **Verified:** all of tier A, 40 seeded from tier B, and 30 seeded from tier C. Proposed: A 11, B 4, C 8. Deduplication took 23 to 21.
  - **Cost at list price:** map $27.52 (down from $37), group $2.79, verify $25.71, dedupe $0.15, about $56 in all.
  - **Blind judges** (shuffled, no counts or tiers): Astra 15 of 21, Opus 15 of 21, both 13 (62%, up from 45% in v0), either 17. Agreement 17 of 21 (κ≈0.53, up from 0.33).
  - **Acceptance by tier, both judges:** A 6 of 11, B 3 of 3, C 4 of 7. Two proposals were flagged unsafe by one judge (restore-from-Git steps). Remaining rejections cite false claims (`-tags fts5` is not required: the vendored shim enables FTS5), command triggers too broad (`git merge`, `gh pr checks`), and an unestablished standing rule (greploop on every push).
  - **Haiku screening with v1 questions:** keeps 59% of episodes at 0.2 and 47% at 0.5, and preserves 12 of 13 both-accepted proposals at the same tier at every threshold from 0.2 to 0.5. The miss is a discovery episode Haiku scored 0. The correction lens is screened hardest (6–10% kept). Haiku cannot reach the 35% kept-fraction target; Jev needs a working key.
  - **Next:** Drew labels the 21 blind proposals (`runs/D-v1/review-for-drew.md`). That is the Batch 2 go/no-go and judge calibration. Rules then freeze and H runs once.
- 2026-10-11 — Independent reviews of the prototype and D results, by Astra high and Opus 5.5 extra-high. Both found the prototype on track and recommended fixes and a v1 rerun of D before freezing rules. Applied in rules v1 and extractor v1:
  - **Family linking.** Delegated T3 tasks and spawned `codex exec`/`claude -p` sessions now join the family that started them, matched by task text or by spawn time. D falls from 74 families to 37 (63 delegated and 21 CLI-spawn links), and H from 101 to 60. v0 recurrence was inflated: 12 of 46 multi-family clusters were one investigation plus its delegates.
  - **Redaction false positives.** 88 of 93 v0 high-entropy redactions were file paths, because the path exemption could never fire. Fixed for paths, word identifiers, and `NAME=/path` values. Quoted, escaped, single-quoted, and YAML-block secret values are now covered. D now has 18 redactions.
  - **Episodes.** Codex `commentary` messages fold into the turn's final answer. Corrections carry the files from the preceding turn, and discoveries carry the person's last message. Research spans accumulate across the turn. Paths are relative to the repository root, not the session's working directory. `entrypoint=cli` no longer counts as proof of a person.
  - **Rules v1** (`tools/sessionmemorybench/prompts.mjs`):
    - one learning per candidate
    - rules need a person's standing instruction
    - no blanket destructive instructions
    - no drifting counts, line numbers, or PR numbers
    - a `commands` trigger alongside `appliesTo`
    - a `checkable` tag
    - the verifier checks clause by clause, provenance, safety (`conflicts_with_policy`), and scope (it counts glob matches)
    - verification runs in a clean checkout pinned to `184889cb`
    - a deduplication pass after verification
  - **Harness.**
    - Checkpoints bind to prompt, model, effort, input, and commit.
    - Provenance is recorded per run.
    - Held clusters are sampled by a seeded content hash. The v0 sample was the first 30 held clusters, which biased the gate comparison.
    - Judges get blind, shuffled input with no counts.
    - Map repair re-sends only faulty candidates.
    - Screening retries failed batches.
  - **Verification tiers in place of the hard gate (open question for Drew):** tier A (two or more families) is always verified; tier B (single-family, checkable in code) is verified up to a budget; tier C (environment or workflow claims seen once) waits for a second family.
- 2026-10-11 — Judge panel on D's 22 proposals, using `tools/sessionmemorybench/JUDGE.md`; each judge verified claims against HEAD independently:
  - **Accepted:** Astra high 14, Opus 5.5 extra-high 13, both 10 (45%), either 17. Verdict agreement 15 of 22.
  - **By gate status:** both judges accepted 7 of 16 eligible proposals and 3 of 6 from the held sample (30 held clusters were verified). The recurrence gate does not predict quality here; at this rate it blocks roughly four times as many accepted learnings as it passes.
  - **Rejection themes:** instructions unsafe under the repository's preserve-unrelated-work rule (blanket `git checkout` of tracked dist files); `appliesTo` far too broad; duplicates the grouper missed; brittle exact counts; a single-session "rule" that is not a standing mandate.
  - **Pointer shards** for documented but recurring friction: Astra 5 of 23, Opus 4, both only 2. Not adopted.
  - **Haiku screening on the 10 both-accepted proposals:** evidence survives for all 10 at thresholds 0.2–0.5. At 0.5 (56% of episodes kept), 9 of 10 reach the same gate outcome.
- 2026-10-11 — Batch 0 canary, using a scratch repository and this worktree:
  - **Claude Code (`claude -p`):** project `PostToolUse` on `Read` delivered context, and `SessionStart` ran. A subagent got `SubagentStart`, and its own `Read` fired `PostToolUse` carrying its `agent_id`.
  - **Codex (`codex exec --dangerously-bypass-hook-trust`):** `PostToolUse` fired for every shell command, reported as `tool_name: Bash` with the command in `tool_input.command`, even in code mode. It carries `turn_id` and no agent ID. Reading the file through `cat` delivered the shard, and the agent followed it.
  - **T3 Claude thread in a worktree:** hooks from `.claude/settings.local.json` ran in a delegated child and in this session, which reloaded them mid-session. The child followed the shard.
  - **T3 Codex thread in a worktree:** the project hook did not run, so trust blocks it. A user-level Codex hook trusted once with `/hooks`, which runs the current repository's delivery script, is the candidate fix. Drew decides; testing it needs an interactive `/hooks` review.
  - **Compliance trial.** Task: append a line to `src/widget.txt`. Hidden rule: add a `CHANGES.md` entry when changing `src/`. Followed (runs):
    - No hook: Sonnet 0/5, Haiku 0/5.
    - Bare rule: Sonnet 3/8, Haiku 5/5, Opus 2/2.
    - Framed shard plus a `CLAUDE.md` trust line: Sonnet 7/8, Haiku 5/5, Opus 2/2.
    - Without delivery the rule is never followed. Framing lifts the most skeptical model from 38% to 88%. (`/tmp/sm-trials.sh`, 2026-10-11.)
  - **Trust framing matters.** A bare injected rule was sometimes flagged as possible prompt injection and not followed. The framed shard plus a `CLAUDE.md`/`AGENTS.md` line naming hook-delivered shards as reviewed guidance did better in the first runs: Sonnet followed it 3/3 with framing and 1/3 without. A larger trial is running. The managed block and delivery format must carry this framing.
- 2026-10-11 — Astra High review, round 2. Verdict: rework. Twelve round 1 findings were resolved and eight partly resolved. It raised one new blocker, seven major, and three minor findings. Responses:
  - **Preventive timing (blocker):** promises now distinguish preventive reads from corrective edits. Batch 0 opens with a canary that measures behavior, plus an opt-in refusal setting if needed.
  - **Hash scope:** limited to hooks, with tests (D2).
  - **Decision matching:** decision files are loaded directly (D8).
  - **Fingerprints:** cover working-tree content via a clean-files rule, the target, and checked paths (D3).
  - **Evaluation isolation:** each run uses its own checkpoints and pool.
  - **Turn-bounded watermark:** D7.
  - **`SubagentStart`:** D9.
  - **Recipe pagination:** D9.
  - **Decision-event identity:** D1.
  - **Addon state transitions:** D10.
  - **Canary-first Batch 0, data-returning subagents, and `held` proposals:** Batch 0 and D8.
- 2026-10-12 — Jev is unblocked. The 403 came from a stale plaintext key exported by Drew's shell profile, which outranks every other source; the user config has no TypeSafe key. The key in the team's 1Password Environment works through `./scripts/with-secrets development`, which needs `RZM_OP_ENVIRONMENT_ID` and `OP_ACCOUNT` set outside Git. `TYPESAFE_LIVE_TEST=1 go test -tags live ./pkg/typesafe` passes, and a synthetic `rzm agent evaluate-batch` returns `jev-1.13.0` scores. The benchmark now calls `rzm` directly and runs under `with-secrets` instead of a login shell.
  - **Jev screening of D with v1 questions** (`D/screen-jev-v1.jsonl`): 1,581 episodes in 38 seconds, none unscored, median score 0.56. Retention of the 13 both-accepted proposals at the same tier, against Haiku at equal kept fractions:

    | Kept | Jev | Haiku | Jev evidence episodes | Haiku evidence episodes |
    | --- | --- | --- | --- | --- |
    | 25% | 10 | 9 | 45 of 82 | 36 of 82 |
    | 35% | 11 | 11 | 61 of 82 | 47 of 82 |
    | 47% | 12 | 12 | 67 of 82 | 58 of 82 |
    | 60% | 13 | 12 | 75 of 82 | 69 of 82 |

    Jev keeps more supporting evidence at every kept fraction and meets the 35% target with 11 of 13. At 35% both lose g364 (a discovery episode); Jev also loses g134 and Haiku g254. These numbers measure retention of learnings mined from unscreened episodes; the end-to-end screened run (Batch 3) is still owed.
- 2026-10-12 — First Jev-gated run on D (`runs/D-v1-jev35`): only episodes Jev scored at least 0.71 (559 of 1,581, 35%) reached the Opus map. Model, effort, verify budget, and seed match D-v1. The threshold was picked from D's own retention numbers, so H is the honest test.

  | | D-v1 (ungated) | D-v1-jev35 (gated) |
  | --- | --- | --- |
  | Map cost | $27.52 | $15.26 |
  | Candidates, clusters | 596, 413 | 378, 291 |
  | Tier A clusters, proposed | 35, 11 | 25, 11 |
  | Proposals after dedupe | 21 | 18 |
  | Both blind judges accept | 13 (62%) | 12 (67%) |
  | Judge agreement | κ≈0.53 | κ≈0.87 |
  | Total cost | about $56 | about $40 |

  - Map cost fell 45% while episodes fell 65%, because the episodes Jev keeps are the longer ones. Verification, whose budget is fixed, is now more than half the cost.
  - Of D-v1's 13 accepted learnings, the gated run proposed 7 again. Five were accepted by both judges. Opus rejected one for its suggested fix, and rejected another as a duplicate of a broader accepted proposal. Three more appeared as clusters but were outside the random tier B verify sample (g2, g54, g391). Three are missing: g134 and g364 were screened out, as retention predicted, and g81 survived screening but was not extracted again.
  - The gated run's 12 accepted proposals include four learnings D-v1 never produced and corrected versions of two D-v1 rejections (FTS5 tags, tracked `dist` files). The four new ones: SPEC numbers from `rzm agent next-id` can collide with other branches; E2E journeys need atomic note saves; E2E fixtures share one vault; the web UI has no subsystem note.
  - Run-to-run variance in mapping and verify sampling is about as large as the gate's effect on which learnings appear. The gate holds quality at about 70% of the cost.
  - Next for Jev: the verify queue. Ranking clusters for verification and checking documentation coverage before an Opus verify would target the remaining cost. The random tier B sample is what left three found learnings unverified.
- 2026-10-12 — Drew asked whether Opus is needed for most stages, given Sonnet 5.5 and Haiku 5.5, and noted that the starter must work in Claude Code, Codex, and T3 Code (Codex models: Sol 6.1 large, Luna 6 small). The benchmark now runs any stage through `claude -p` or `codex exec` (the flags follow `pkg/harness/codex`). Claude cost is computed from token counts at published prices: per million tokens, Opus 5.5 is $4/$20, Sonnet 5.5 $2/$10, and Haiku 5.5 $0.10/$0.50. Claude Code does not price the 5.5 models. Codex runs report tokens only. Jev 1.13 is listed at $0.042 per million input tokens, with output free.
  - **Map on the same 559 gated episodes, medium effort.** Recall is the number of both-judge-accepted learnings a run extracted, decided by a Sonnet 5.5 matcher over candidates citing the same evidence (`runs/map-recall.json`). Gold from D-v1 is independent of every gated run. Gold from D-v1-jev35 favors its own Opus map.

    | Map model | Candidates | D-v1 gold | D-v1-jev35 gold | Cost |
    | --- | --- | --- | --- | --- |
    | Opus 5.5 high | 378 | 9 of 12 | 12 of 12 | $15.26 |
    | Haiku 5.5 | 598 | 9 of 12 | 9 of 12 | $1.26 |
    | Sonnet 5.5 | 233 | 7 of 12 | 8 of 12 | $7.06 |
    | Sol 6.1 | 542 | 6 of 12 | 7 of 12 | 2.3M input tokens |
    | Luna 6 | 235 | 5 of 12 | 6 of 12 | 2.2M input tokens |

  - **Verify on D-v1-jev35's 94 clusters, medium effort.** "Recovered" counts the 12 both-judge-accepted proposals that a model also proposes; Opus's figure is circular because the judges saw only its proposals.

    | Verifier | Agrees with Opus | Proposed | Recovered | Proposed where Opus rejected | Cost | Seconds per cluster |
    | --- | --- | --- | --- | --- | --- | --- |
    | Opus 5.5 high | — | 20 | 12 | — | $22.30 | 29 |
    | Sonnet 5.5 | 81 of 94 | 19 | 7 | 6 | $8.51 | 15 |
    | Sol 6.1 | 77 of 91 (3 failed) | 10 | 6 | 2 | 38M input tokens | 107 |
    | Haiku 5.5 | 76 of 89 (5 failed) | 7 | 5 | 1 | $1.18 | 28 |
    | Luna 6 | 74 of 94 | 6 | 3 | 3 | 26M input tokens | 39 |

    Sonnet's five misses are reasoned rejections that cite the documenting note or code comment, or the single-session evidence. Haiku's misses include tool-permission denials and failed calls. Blind judges rated Sonnet's 17 deduplicated proposals. Both accepted 7 (41%, κ≈0.53), against 12 of 18 (67%) for Opus on the same clusters. That is $1.22 of verify spend per accepted proposal, against $1.86 for Opus, but five fewer accepted learnings. One of the seven is a learning Opus had rejected. Three are rewordings of proposals the judges rejected in Opus's version, so wording moves verdicts.
  - **Reading:** Haiku 5.5 extracts as well as Opus on the independent gold, at about a twelfth of the cost. Verification is where model strength shows: Sonnet 5.5 is the closest cheaper verifier but loses about 40% of accepted learnings. Opus 5.5 at medium or low effort is the next arm to test. The Codex models trail on both stages at medium effort and spend about 29k baseline input tokens per call. One run per arm and 12 gold learnings support model selection for the next run, not a general claim.
- 2026-10-12 — Astra high independent review (`/tmp/sm-review/astra-review.md`; brief with the video transcript and Drew's messages). Verdict: continue; Jev belongs at the center. Findings and responses:
  - **Redaction order (P1, fixed).** Codex progress notes were shortened before redaction, so a cut could split a secret. A second gap: a quoted secret followed by more text on the same line was never redacted. Each note is now redacted whole before it is joined, and a prose-aware quoted-value rule runs after the strict rule. A test goes through `episodesFor`. Regenerated D and H episodes are byte-identical to the files the runs used, so no credential-shaped text reached any model.
  - **35% gate (P1, accepted).** It keeps 11 of 13 reference learnings, below the 90% criterion. Use a wider gate (60% kept all 13 on D) while comparing models.
  - **Fail closed (P1, partly fixed).** Proposals with unresolved contract problems no longer reach review, and Jev is pinned to `jev-1.13.0`. Still open: cluster identity tied to a manifest, per-item screening checkpoints, durable cursors, and content-keyed caching within and across runs.
  - **Stage roles (P2, measured above).** Jev for decisions, a small model for extraction, a mid model for verification, and a large model only for escalations and architecture synthesis.
  - **Harness (P2, open for Drew).** Expose `pkg/harness` generation through Rhizome after it returns usage and the resolved model, matches no-persistence across harnesses, and takes an explicit tool policy. This revises the "no new Go command" decision.
  - **Still unproven:** T3 Codex delivery, preventive command delivery, echo feedback from delivered shards, and a paired task evaluation.
- 2026-10-12 — Drew stated the end goal: "an agent skill with one or more embedded workflows that are highly automated and efficient", with Rhizome agent hooks or at least AGENTS.md/CLAUDE.md guidance on using the memory system. Astra high revised the automation plan (`/tmp/sm-review/astra-plan.md`; brief `/tmp/sm-review/plan-brief.md`). Proposed, pending Drew's approval of the scope changes:
  - **Skill:** one `session-memory` skill with routes for setup/status, mine, review, lookup, and maintain. Shared code-mode stage functions sit behind a dispatcher. Scripts own checkpoints, budgets, and validation; the agent picks the route and explains results; the human approves exact revisions.
  - **Rhizome:** one `generate` operation shared by `rzm agent generate` and `rzm.generate`, backed by `pkg/harness`. It takes a concrete provider, model, and effort; tool policy `none` only; and returns a usage envelope with the resolved model. Codex must prove an enforceable no-tool mode or report it unsupported. Role bindings (`decide`, `extract`, `verify`, `synthesize`) stay in starter config and are frozen per run. T3 stays a host, not a harness kind.
  - **Verification:** a starter workflow over evidence packets, with bounded evidence-request rounds that run no model-supplied commands. A tool-using `agentTask` operation only if that fails to match the current verifier.
  - **Delivery:** a shared lookup selector and managed guidance work in every host first. Optional init-managed hooks follow, per host, after canaries pass. Stale shards are withheld from automatic guidance.
  - **Efficiency:** content-addressed JSON, a result cache separate from an attempt ledger, complete-turn cursors, and cluster identity independent of generated prose. Jev handles screening (about 60%), neighbor equivalence, documentation coverage, and ranking. Targets: $10–15 for a cold run of D, at most $1 per 100 new episodes, and zero calls on unchanged replay.
  - **Milestones:**
    - **M0:** freeze the contract.
    - **M1:** Opus-medium verify, evidence-packet verify, Jev coverage and grouping, Codex permission fixes.
    - **M2:** the generation boundary, with a foundation review.
    - **M3:** replayable mining.
    - **M4:** packaging, review, lookup, and hooks.
    - **M5:** composed D run, then H once.
    - **M6:** paired task evaluation and closure.
- 2026-10-17 — Drew approved the revised plan and the M1 experiment spend (about $10–20): "So let's go with your gut. I approve the approach and the spend." Decisions:
  - **Scope:** one generic `generate` operation, lookup and managed guidance as the delivery baseline with hooks optional per host, and stale shards withheld from hooks and lookup. SPEC-0124 is revised to match.
  - **Verification tiers** replace the two-family gate: tier A always verified, tier B verified up to a budget in Jev's ranked order, tier C held for another family. Review shows each proposal's tier. Drew: "we can always evolve it." Acceptance by tier is reported in every run, and tier B is switched off if it underperforms.
  - `/tmp` was cleared after the 2026-10-12 work, so Astra's review and plan files are gone; the summaries above are the record. The pinned verification checkout was recreated at `184889cb`.
- 2026-10-17 — M1 verification experiments on D-v1-jev35's 94 clusters (`bench.mjs compare`, `runs/verify-compare.json`). "Recovered" counts the 12 both-judge-accepted proposals from the Opus-high run, so it favors that run. "Cost (5m)" reprices token counts at five-minute cache-write rates.

  | Verifier | Mode | Proposed | Agrees with Opus high | Recovered | Cost recorded | Cost (5m) | Seconds per cluster |
  | --- | --- | --- | --- | --- | --- | --- | --- |
  | Opus 5.5 high | tools | 20 | — | 12 | $22.30 | — | 29 |
  | Opus 5.5 medium | tools | 18 | 86 of 94 | 8 | $15.84 | $11.22 | 22 |
  | Sonnet 5.5 medium | tools | 19 | 81 of 94 | 7 | $8.51 | $5.93 | 15 |
  | Sonnet 5.5 medium | packet | 17 | 81 of 94 | 7 | $13.62 | $9.13 | 13 |
  | Haiku 5.5 medium | tools | 7 | 76 of 89 | 5 | $1.18 | $0.71 | 26 |

  - **Packet verification** (`tools/sessionmemorybench/packet.mjs`): the script gathers glob match counts, files and `rg` hits for identifiers in the statement, a Rhizome search, the notes governing the first matched file, and `AGENTS.md`. The model runs no tools; it may request files, regex searches, or Rhizome searches for up to two rounds (1.49 calls per cluster on average). It matches the tool-using Sonnet verifier on recovery (7 of 12) and agreement (81 of 94), so a no-tool `generate` operation can carry verification. It is not cheaper through `claude -p`: each call carries about 9k tokens of Claude Code system prompt and writes its 15–25k-token packet to the cache. A direct API call, a stable prefix that includes `AGENTS.md`, and smaller excerpts are the levers.
  - **Opus medium** recovers one more than Sonnet at about twice the cost, and agrees with Opus high on 86 of 94. The misses on both are reasoned rejections, mostly "already documented". One (g92, "`go test` needs `-tags fts5`") is rejected as false because the vendored shim enables FTS5, which an earlier judge round also found. The gold set itself contains disputed items, so Drew's labels decide between these verifiers.
  - **Cache TTL.** Claude Code 2.1.280 writes the one-hour cache by default, at twice the input price; `FORCE_PROMPT_CACHING_5M=1` switches to 1.25 times. The benchmark now sets it for every call, and the `generate` operation needs the same control.
  - **Search in the verification checkout** failed with "stale search continuation" while its runtime worked through about 40,000 queued embedding jobs with no Voyage key. Verification now runs with the runtime stopped and `RZM_RUNTIME_AUTOSTART=0`. The first Opus-medium attempt ran with broken search and was discarded ($2.59).
  - **Jev ranking** (`bench.mjs rank`, `RANK_QUESTIONS`, evaluated by `tools/sessionmemorybench/rank-eval.mjs`): 704 clusters across D-v1 and D-v1-jev35 in about 6 seconds per run, 390k input tokens (about 2 cents). Two noul questions, with counts and tiers hidden:
    - `checkable` separates the extractor's tier B from tier C with AUC 0.91–0.92, so Jev can assign tiers without the extraction model.
    - `worth` is a weak ranker: AUC 0.62–0.64 against every verifier's decisions and 0.63–0.71 against judge acceptance, with scores bunched between 0.76 and 0.82. Ordering the tier B budget by it is no better than the seeded sample. As a floor it helps a little: skipping verified clusters below 0.75 saves 12 of 105 and 16 of 94 verifies and loses no accepted learning; 0.78 saves about 30% and loses one or two.
    - First attempts failed because the 1Password app had stopped answering the CLI, and because the CLI cannot reach the app from inside the agent sandbox. After Drew restarted 1Password, `with-secrets` ran outside the sandbox.
  - **Spend:** about $32 at list price, over the approved $10–20. Further paid runs wait for Drew.
- 2026-10-17 — Jev lab (`tools/sessionmemorybench/jevlab.mjs`, question sets in `jevlab-sets.mjs`). Drew's framing: Jev is an early recall filter that cuts expensive calls, not the judge of whether a lesson exists. Labels come from D-v1's unscreened Opus map, which read all 1,581 episodes: 619 candidates, 192 rated strong or verified, 23 verifier proposals, 13 accepted by both judges. A learning survives when a kept episode or passage states it. Jev answers are cached per question and state, so each new question set costs a few cents. Secrets come from the keychain through `with-secrets development` (PR #19), with no prompt.
  - **Whole-episode screening plateaus.** Every variant (the v1 lens questions, seven narrow observable questions, permissive questions with and without structured criteria, and logistic combinations with code features, cross-validated by family) keeps 80–86% of strong learnings at 30% of episodes. Message length and tool-call count alone come within a few points of the v1 question.
  - **Episodes are the wrong unit.** By characters kept, which is what extraction costs, keeping 30% of the text holds only 52% of strong learnings, because learnings sit inside long reports of finished work.
  - **Passage screening.** Each paragraph or list item is asked separately (7,965 passages, $0.16 for three questions). Haiku 5.5 labeled which passages state each learning ($0.98; 185 of 192 found). Ranking passages by the better of two questions (`p_reusable`, `p_surprise`) keeps 73% of strong learnings at 20% of the text, 80% at 25%, and 85% at 30%, against 34%, 42%, and 52% for episode screening. The 13 accepted learnings are too few to separate variants.
  - **End to end** (Haiku 5.5 extraction at medium effort; `bench.mjs recall --gold-labels` with a Sonnet 5.5 matcher over the 192 strong-or-better learnings; `jevlab.mjs gate` writes the gated episodes, rebuilding each from its kept passages):

    | Text kept | Gate | Learnings found | Accepted (of 13) | Extraction cost |
    | --- | --- | --- | --- | --- |
    | 20% | passages | 111 (58%) | 6 | $0.64 |
    | 30% | passages | 127 (66%) | 6 | $0.83 |
    | 30% | whole episodes, v1 question | 74 (39%) | 5 | $0.49 |
    | 68% | Jev v1 at 35% of episodes | 136 (71%) | 7 | $1.26 |
    | 68% | the same, Opus 5.5 high | 150 (78%) | 9 | $15.26 |

    At equal text, passage gating finds 72% more learnings than episode gating. At 30% of the text it reaches 93% of the current gate's recall at 68%, for two-thirds of the extraction cost. The extractor now limits recall more than screening does.
  - **Automated question search** (`jevlab.mjs search`, after TypeSafe's feature-discovery cookbook): Sonnet 5.5 reads missed passages and wasted keeps from development families (four of five, split by family hash) and proposes up to four question changes per round. Jev answers each new question over all 7,965 passages, and a change stays only if development recall at 25% of the text rises by at least 0.01. Four rounds cost $2.08 of Jev and $0.32 of Sonnet, and kept five general questions besides the starting two (build and setup gotchas, generated-file handling, deliberate exceptions, a person's directives, shared-state collisions; `jevlab/search-1/state.json`). Screening recall at 25% rose from 0.77 to 0.88 on development families and fell from 0.97 to 0.94 on held-out families.
  - **End to end, split by family** (Haiku extraction at 30% of the text): the searched questions found 119 of 160 development learnings against 103 for the starting two, but 18 of 32 held-out learnings against 24. Whole-episode Jev v1 at 68% found 121 and 15. The held-out set is too small, and extraction too variable, to show that the search generalizes; it may have fit the development families. Keep the two hand-written passage questions until the held-out week (H) gives an independent check, and treat search gains as unproven.
  - **Spend for this round:** about $2.80 of Haiku extraction, $2.40 of Sonnet matching, $2.40 for the search, and $0.70 of other Jev questions, about $8.30 in all.
- 2026-10-18: **Haiku extraction effort.** Drew preferred making Haiku work, at high or xhigh effort but not max, over moving extraction to Sonnet. Same 30% passage set, same 192 strong learnings and Sonnet matcher:

  | Haiku 5.5 effort | Found | Candidates | Map cost | Minutes per batch |
  | --- | --- | --- | --- | --- |
  | medium (two runs) | 127, 125 | 461, 457 | $0.83 each | 0.6 |
  | high | 128 | 565 | $0.91 | 1.2 |
  | xhigh (two runs) | 139, 136 | 588, 606 | $1.31, $1.34 | 2.5 |

  The two medium runs differ by 2, so xhigh's gain of about 11 (9%) is real, and high buys almost nothing. Passage evidence survives screening for about 163 learnings, so xhigh finds about 84% of what the screen leaves reachable. The two xhigh runs together found 152, as many as one Opus 5.5 run over 68% of the text (150, $15.26), for $2.65; a second pass would double the candidates that grouping must read. Use Haiku 5.5 at xhigh for the extract role. Spend: $4.39 of extraction and $1.79 of matching, about $6.20.
- 2026-10-18: **Codex extraction, same setup.** A delegated Opus 5.5 run measured the OpenAI models for the extract role. `models.mjs` now prices gpt-6-luna at $0.10 input, $0.01 cached input, and $0.50 output per million tokens, and gpt-6.1-sol at $2, $0.10, and $10, from the [OpenAI pricing page](https://developers.openai.com/api/docs/pricing). Costs below are API-equivalent; `codex exec` may bill a subscription instead.

  | Setting | Found | Candidates | Cost | Minutes per batch |
  | --- | --- | --- | --- | --- |
  | gpt-6-luna medium | 81 | 191 | $0.13 | 0.3 |
  | gpt-6-luna high | 82 | 194 | $0.14 | 0.3 |
  | gpt-6-luna xhigh (two runs) | 97, 83 | 313, 298 | $0.24, $0.21 | 1.0 |
  | gpt-6.1-sol low | 110 | 350 | $3.05 | 0.4 |
  | gpt-6.1-sol medium | 110 | 383 | $3.26 | 0.4 |

  Every Codex setting trails Haiku 5.5 xhigh (137.5, $1.31).
  - **Luna:** noisy from run to run, about ±7.
  - **Sol 6.1:** low and medium tie. It barely reasons on this task, under 5,000 reasoning tokens per run.
  - **Selectivity:** the Codex models return half as many candidates as Haiku xhigh, which points to selectivity under a prompt tuned on Claude, not to missed reading.
  - **Overhead:** each `codex exec` call carries about 28,000 tokens of harness instructions, most of Sol's input cost. A `generate` path without them would cut Sol to about $1.

  For now, extract with Haiku 5.5 xhigh whenever Claude is available, and with gpt-6.1-sol low as the provisional OpenAI-only default. Spend: $1.92 of Sonnet matching and about $7 API-equivalent of Codex.
- 2026-10-18: **Held-out week H check of screening.** Gold for H came from one unscreened Haiku 5.5 xhigh map of all 1,697 episodes: 902 candidates, 208 rated strong, $2.58 (`runs/H-haiku55-xhigh`, `jevlab/labels-H-haiku55-xhigh.json`). Jev answered all seven searched questions over H's 5,346 passages for $0.17. Each gate kept 30.4% of the characters, and Haiku 5.5 xhigh mapped what it kept:

  | Questions | Episodes kept | Found of 208 | Map cost |
  | --- | --- | --- | --- |
  | `p_reusable` and `p_surprise` (hand-written) | 666 | 135 | $1.01 |
  | The seven from the search | 679 | 132 | $1.05 |

  On a week no question was tuned on, the search's extra questions add nothing, so the two hand-written questions stay. Recall here is measured against the same model reading everything. It therefore combines screening loss with run-to-run extraction variance: on D, two identical xhigh runs agreed on only part of what each found. A second unscreened H map would separate the two (about $3). Spend: $4.64 of Haiku, $0.66 of matching, and $0.17 of Jev, about $5.50.
- 2026-10-18: **Model selection decisions.** Drew approved calibrate as a workflow inside the `session-memory` skill, and starting M2. On fallback he set the rule: "This should operate within the harness the user is already using. However, in a harness where there is access to both models, the model can prioritize amongst what's available." SPEC-0124's model profile now says:
  - A run uses only the providers its harness reaches, so transcript text never moves to another provider. Jev is the exception.
  - A newer catalog version of a candidate's family, with no profile entry of its own, runs at the candidate's effort and is reported as unmeasured.
  - When a family is unavailable, the role falls back to the next candidate from the same provider (Haiku to Sonnet).
- 2026-10-18: **M2 generate operation** (delegated Opus 5.5 high; uncommitted). `Harness.Generate` now returns `GenerateResult{Output, Usage, Model}`, and `rzm agent generate` and `rzm.generate` expose it. Draft `models.json` and `scripts/lib/models.mjs` resolve role bindings under Drew's harness rule.
  - **Claude:** calls pass `--no-session-persistence`, set `FORCE_PROMPT_CACHING_5M=1`, and report the model that ran (`claude-haiku-5-5` for the alias). `--safe-mode` and a one-line `--system-prompt` keep CLAUDE.md, AGENTS.md, and the coding-agent prompt out of each call: a test call fell from 17.4k input tokens and $0.089 to about 1.2k tokens and $0.002. This is unreviewed, and the benchmarks above ran with the full prompt. `rzm harness status` lists only aliases for Claude, so the resolver learns the version from one test call per alias.
  - **Codex:** `codex exec` 0.163 offers shell, sub-agent, and note-writing tools even with every tool feature disabled, so Codex generation now reports itself unsupported, as the spec requires. That leaves Codex-only users with no extract or verify model; decision pending with Drew. Its `--json` usage omits the model that ran, and a call still carries about 17k tokens of instructions.
  - **Checks:** the focused harness, mcp, agentapi, and generate tests pass, as do `make check-fast` and `make build`. `make check` fails only because the in-progress starter template has no `SKILL.md`.
- 2026-10-18: **Lean Claude prompt, one run.** Drew approved keeping `--safe-mode` and the one-line system prompt, subject to a quality check. `SMBENCH_LEAN=1` gives bench calls the same flags. Haiku 5.5 xhigh on the D 30% passage set:

  | Prompt | Found (of 192) | Candidates | Input tokens | Map cost |
  | --- | --- | --- | --- | --- |
  | Claude Code default (two runs) | 139, 136 | 588, 606 | 1.31M | $1.31, $1.34 |
  | Lean | 133 | 772 | 0.74M | $1.41 |

  The lean prompt removes about 14.5k input tokens per call, but those tokens were cache reads that cost Haiku almost nothing, so the run cost slightly more. The difference is the extra output: 30% more candidates, which grouping must also read. Recall is 3 to 6 below the two default runs, against a run-to-run gap of 3, so one run can't settle it. Drew kept the lean prompt without a second run: "Let's just leave the lean prompt in place. It's fine." The saving matters where cache writes or the model are expensive: first calls, short probes, and verification with Sonnet or Opus. Codex's app-server probe also found a working route to a call with no tools at about 1.3k input tokens; the driver is being built (see SPEC-0124's revised `generate` tool policy). Spend: $1.41 of Haiku and $0.56 of matching.
- 2026-10-18: **Codex generation through the app-server** (delegated Opus 5.5 high; uncommitted). This supersedes the M2 entry's "unsupported" result. A probe against a local fake endpoint found that Codex 0.163's model catalog (`use_responses_lite`, `tool_mode`, `multi_agent_version`) brings tools back, and that the backend adds its own note and history tools when the request omits `tools`. The driver now:
  - launches the app-server with tool features and instruction blocks off and a rewritten copy of the model catalog, starts an ephemeral thread with no environments, every configured MCP server disabled, a read-only sandbox, and approval `never`, and runs in an empty temporary directory.
  - fails closed with `ErrUnsupported` before any turn when Codex is older than 0.163.0, the model isn't in the catalog cache, `config/read` or `thread/start` fails, or Codex warns that it ignored one of the overrides.
  - reports usage from `thread/tokenUsage/updated` and the model from `thread/start`, with `toolPolicy: sandboxed` (Claude reports `none`). Codex reports no cost.

  Live: Luna answered in 1,291 input tokens, against about 27,000 through `codex exec`, said it had no tools, and could not create a test file. The global `~/.codex/AGENTS.md` is still sent with each call, and a catalog field a later Codex stops honoring would go unnoticed, so each Codex upgrade needs the probe again. `generate` now defaults its provider to the caller's choice, then `agent.harness`, then the only CLI signed in. Focused Go tests, `make check-fast`, and `make build` pass.
- 2026-10-18: Drew approved M3 and asked to "retain a workflow in this repository for doing further evaluation or improvement work on the memory system, so we can evolve it in future versions of Rhizome." It is the repository-local skill `.agents/skills/session-memory-eval/`, linked into `.claude/skills/`, and it is not shipped. It covers the evaluation steps (develop on D, confirm once on H, noise floor, the adoption rule, recording) and keeps the numbers to beat in `baselines.md`. M3 makes the bench call the starter's own stage functions, so evaluations measure what ships. M3 is delegated to Opus 5.5 high; it builds against synthetic fixtures, and the cold D run waits for review.
- 2026-10-18: **M3 built and run cold on D** (delegated Opus 5.5 high; uncommitted). `mine.mjs` runs as plain Node calling `rzm` subcommands, because code mode caps each call at five minutes. Thirty fixture tests pass, covering cache replay, cursors, crash resume, stable cluster IDs, budget stops, and frozen bindings. The bench now calls the starter's stage functions. A Claude `generate` fix reports the model and full usage when a structured reply takes several requests.
  - **Mechanics, live:** the harness killed the first run at extraction batch 31 of 39; rerunning resumed with no repeated calls. A replay reported `nothing-new` and made no calls. A `--budget 5` run stopped at $4.91 with exit 3.
  - **Cold run:** 1,581 episodes, 695 kept by the passage gate, 740 candidates, 639 clusters (32 tier A, 567 tier B, 40 tier C). Spend $8.53 against a $11.26 estimate: Jev $0.16, Haiku extraction $1.25, Opus grouping $3.39, Sonnet packet verification $3.71. Tier A plus the default 40 tier B gave 4 proposals.
  - **Diagnosis:** most of the 13 learnings both judges accepted in D-v1 were extracted but sat in unverified tier B, because the 40-cluster budget was spent in Jev `worth` order, a ranker measured at AUC 0.62. Verifying 72 more held tier B clusters ($4.91) added 6 proposals. Total: 10 proposals from 144 verified, $13.44, and 3 of the 13 accepted learnings, against 7 of 12 for the Sonnet packet verifier on D-v1-jev35's clusters.
  - **The verifier rejects more:** 57 already documented, 37 obvious from the code, 16 false now. The 10 proposals read well, but several gold-matching clusters were rejected as already documented. Possible causes, none yet measured: the lean prompt, the packet prompt edits (`git grep`, the removed history-rewrite note), cluster statements written by incremental Opus grouping, and fewer cross-family merges (tier A is 32 clusters).

- 2026-10-19: Drew: "The spend has been fine. You can stop asking me for approvals of spend under $15. I think we have the model selection dialed in enough where you can just do what you need to do. Continue. Let's get things committed when you're at a good point." M2 and M3 are committed (22a4c960, 7766c8a2). A first `SKILL.md` was needed for `make check`, which validates every skill template. The secret scanner flagged two synthetic redaction fixtures, which are now built by concatenation.
- 2026-10-19: **Tier B ranking, from the 144 M3 verdicts (no spend).** No cheap cluster feature predicts which tier B clusters verification keeps. For proposed against rejected, `worth` scores AUC 0.39 and `checkable` 0.46. For "not already documented or obvious" against the rest, `worth` scores 0.48. Ranking tier B by `worth` is no better than chance. Most tier B rejections are "already documented" (41 of 112) or "obvious from the code" (32). A pre-verify documentation-coverage check is the next lever, but `rzm search --raw` returns titles, not passages, so it needs more plumbing first.
- 2026-10-19: **M4 delivered: review, lookup, maintain, and packaging.** The packaging is delegated to Opus 5.5 high.
  - **Packaging (D10):** `template.yaml` with `optionalAddon: true`; `--addons` accepts it on a first run; `--add-addon` and `--remove-addon` change an existing setup without touching other choices, and an ejected add-on is refused. SPEC-0038 gains US12. A Go test runs the Node script tests and fails in CI without Node 20. CI installs Node 22 for the Linux and Windows unit jobs. Fixes for Windows in the existing tests: `fileURLToPath`, exit status instead of `SIGKILL`, a typed directory symlink, and drive-letter paths in `relativize`.
  - **Tracked data:** the `ContextShard` ontology, the `context-shards` recipe (offset paging), and a view grouped by kind. `shards.mjs` approves with a publication confirmation and refuses uncommitted files and credential-shaped text. It dismisses into a shared or local decision event, and renews, revises, and retires against the content hash from `check`. Every tracked write is atomic. `lookup.mjs` finds shards by file, command, or task (through `rzm search`) and returns at most five, rules first. It withholds shards whose files changed or whose content no longer matches the approved hash. Mining no longer verifies or proposes a cluster a person reviewed, or a learning that a decision or approved shard covers.
  - **Evidence:** 37 Node tests, including an 18-case glob table that the JavaScript matcher and Rhizome's doublestar both pass, and a dismissal that survives a rebuilt pool. Init and cmd Go tests pass. In a scratch repository, `init --addons action-items,session-memory` installed everything and `init --check` exited 0. `validate all` reported 0 issues. A synthetic proposal was approved, and lookup returned it by file and by task. File context showed it through its glob anchor.
  - **Deviations:** a Markdown file gets no anchor context, because Rhizome treats Markdown as notes, so lookup alone covers shards on Markdown. Lookup reads shard files directly rather than through the recipe; the recipe serves agents and the view. `commands` is a shard field that lookup matches, and the approved hash covers it. A decision suppresses only the same learning hash, so a teammate's reworded duplicate still relies on the verifier finding the approved shard by search. Hooks and calibrate are not built.

- 2026-10-19: **Verifier noise, on D-v1-jev35's 94 clusters** ($20.64). The M3 run raised the question of whether the shipped verifier got stricter. Each run used the shipped Sonnet 5.5 medium packet verifier and seed sm-1 on the same clusters, so only the system prompt changed.

  | Run | Proposed | Agrees with Opus high | Recovered (of 12) | Cost (5m) |
  | --- | --- | --- | --- | --- |
  | M1 packet code, Claude Code system prompt | 17 | 81 | 7 | $9.13 |
  | Shipped, lean prompt | 11 | 75 | 4 | $5.81 |
  | Shipped, lean prompt, repeat | 17 | 77 | 7 | $5.64 |
  | Shipped, Claude Code system prompt | 13 | 79 | 6 | $8.57 |

  - **Reading:** identical runs differ by 3 recovered and 6 proposed, so the first lean run's low recall was noise, not the prompt. The lean prompt stays: same recall within the noise, about 35% cheaper.
  - **For the cold-run yield:** the verifier is not stricter than in M1. The low M3 yield comes from which tier B clusters get verified, and from grouping. Single-run verdicts on borderline clusters are close to a coin flip.
- 2026-10-19: **Astra high review of the hook plan and of Markdown anchors.** Verdict: approve with changes. Findings and responses:
  - **Blocker for the T3 Codex option:** a user-level Codex hook that runs whichever repository's delivery script it finds would run repository code before the repository's own hook is trusted, because Codex loads user hooks in untrusted projects. Rejected as proposed. The alternatives are lookup only in T3 Codex threads, or a user-owned runner that reads shards as data and never runs repository scripts. Drew decides.
  - **Path containment:** `relativize` maps a path in another T3 or Codex worktree onto this repository, which suits transcripts but not delivery. Fixed for lookup with a strict mode; delivery will use it too. The hook build also needs a quote-aware parser for the promised command forms (`cat Makefile` is missed, and an `rg` pattern can be read as a path).
  - **CRLF:** `parseShard` rejected shards with CRLF line endings, so a Windows checkout lost every shard. Fixed and tested.
  - **For the hook build:** synchronous `git` calls cannot honor a JavaScript deadline, so delivery needs bounded subprocesses and batched index reads. Dedupe must filter before the five-shard limit, or delivered shards hide later matches. Dedupe state needs serialized updates and the worktree in its key. On Windows, Claude hooks should launch `node` with an `args` array, and Codex runs hook commands through `cmd.exe /C`.
  - **Timing:** Claude Code `PreToolUse` can add context, but it arrives with the tool result, so reads stay the preventive path.
  - **Decisions:** command-triggered delivery yes, as corrective guidance in event-specific JSON. Hooks are enabled explicitly during add-on setup, and the choice persists through updates; the team setting goes in `.claude/settings.json`.
  - **Freshness wording:** the fingerprint covers committed files, not unstaged edits, and old patternless shards are flagged for maintenance but still delivered. Status text should say so.
  - **Markdown:** file context for a note returns before consulting anchors, though glob anchors match any path. Returning glob-anchored docs for notes is a separate, bounded Rhizome change that moves the note-to-code boundary. Tag, type, and link selectors need their own spec. The spec's claim that Markdown has no file context is corrected.
  - **Drew's decisions:** T3 Codex threads use lookup only. Hooks should be a Rhizome system that installs hook entries and dispatches to the starters that declare handlers, rather than something each starter sets up for itself. Command-triggered delivery is still open; Drew asked why it helps (see the examples in the spec's open question).
- 2026-10-19: **Rhizome hook dispatcher, SPEC-0045 US5.** Drafted, and Astra high reviewed it: approve with changes, no blocker. Changes applied:
  - **Bootstrap:** `rzm hook` takes an early path that skips binary bootstrap and download, and fails open only once it has started. Init checks that the selected binary supports `rzm hook`.
  - **Identity:** the normalized identity is worktree, harness, session, agent ID when present, and turn when present. Session-start and tool-call delivery use separate scopes.
  - **Emitted state:** the dispatcher, not the handler, records emitted item IDs. It records them only after emitting, passes them and the remaining budget to each handler, and prefers a repeat to a lost item across crashes.
  - **Deadline:** one fixed deadline covers the whole invocation. A timed-out handler's process tree is killed, and one handler's failure keeps the others' items.
  - **Shell calls:** recognized shell reads are kept. A shell call with no recognized read starts no handler, because 100 ms on every Bash call would add about 22 minutes over this repository's recent Claude Code sessions, against 2 minutes for Read, Write, and Edit together. Entries narrow tool matchers, and change only when the events or matchers change.
  - **Trust:** handler declarations come only from Rhizome's built-in starter registry. Turning hooks on authorizes enabled starters' handlers; host trust covers the entry, not handler contents.
  - **Host facts:** Claude Code `command` plus `args` spawns without a shell. The Codex Windows field is `commandWindows`, run through `cmd.exe /C`. Codex has a `SubagentStart` event.
  - **T3 Codex:** quality evidence now checks lookup in T3 Codex threads, not hook delivery.
- 2026-10-21: **Context size and resets.** Drew asked whether shards could overwhelm context, whether Rhizome search should help select them, and how sessions handle compaction. Findings and changes:
  - **Size:** each delivery is capped at five shards and about 4,000 characters, and a shard is not repeated in a context. Across the five evaluation runs, 0 of 82 proposals applied everywhere, so session start delivers nothing today. No per-session total cap; one is cheap to add once a repository has hundreds of shards.
  - **Breadth:** against the 7,085 tracked files, three development-run proposals matched 36–43% and the median matched 0.5%. Approval and revision now refuse patterns that match more than 50 files and more than a tenth of tracked files unless the reviewer passes `--broad`. Approving a sixth shard that applies everywhere returns a warning, and session-start delivery names the shards past the limit instead of dropping them.
  - **Search:** lookup already ranks shards with `rzm search` for a task. Delivery on the person's prompt waits for evidence from the composed run of what file-pattern delivery missed.
  - **Resets:** the dispatcher now discards a context's emitted items on compaction or clear. The core template gets a dispatcher handler that tells the agent to start a new Rhizome session after a reset, and the `rhizome` skill says the same for hosts without hooks. Codex's once-per-turn repeat is explained: its subagent hooks carry the parent's session ID and no agent ID.
  - **Spec consistency:** the overview and US3 summary no longer say a canary enables hooks; the person turns them on.
  - **Reset handler moved out (same day):** a core hook would make init offer hooks in every repository for an unmeasured problem, and would not help hosts without hooks. SPEC-0045 AC10 was removed. SPEC-0082 US12 instead has dedupe responses name what they withheld and how to reread it, which works in every host. Drew expects hooks across Rhizome templates soon; core's reset reminder is the first candidate once US12 shows whether it is still needed.
  - **Fixed hook entries:** with hooks spreading across templates, entries narrowed to each starter's tools would change, and Codex would ask for trust again, whenever add-ons change. SPEC-0045 US5 now installs one entry per hook point with no tool filter, and the dispatcher alone decides what runs. `rzm --version` took a median of 30 ms on macOS against 2 ms for an empty process, under 1% of an agent step; the earlier 22-minute estimate assumed 100 ms per call. A platform whose no-match overhead exceeds 50 ms may narrow its filter.
- 2026-10-21: **Merge-readiness review (GPT-6.1 Sol, xhigh).** Verdict: merge after fixes. All six findings were fixed and tested:
  - **Redaction:** CRLF text and quoted values spanning lines were not redacted. Text is now normalized to LF first, and a rule covers multiline quoted values.
  - **Publication:** approval wrote verification notes, and shared dismissals wrote reasons, without a credential scan. Every tracked write now scans its full text; verification notes are redacted and credential-shaped reasons are refused.
  - **Containment:** verification followed tracked symlinks out of the checkout. It now reads only regular tracked files, skips `.env` files, and redacts what it reads.
  - **Lock:** two contenders could both reclaim a dead owner's lock. Reclaiming now happens under a second lock after re-checking the owner, and unlock removes only the caller's lock.
  - **Windows:** owner-only permission assertions now run on Unix only; Windows state inherits the checkout's access control, and the skill says so.
  - **Changelog:** entries added.
  - **Rebase:** onto `main` after #21, which moved file context into `pkg/app/operations` and agent commands onto `vaultCommandOptions`. Anchored notes, `generate`, and the add-on flags were ported.
  - **Mid-effort merge:** release policy allows it. The hook dispatcher and handler, command-text delivery, calibrate, and M5/M6 remain, and the effort stays active.
  - **Round 2** confirmed the lock fix (96 contenders, no overlap) and the rebase port, and found four more, all fixed and tested:
    - Verification checked only the final path component, so a symlinked parent directory escaped the checkout. It now resolves the whole path, requires it inside the checkout, and reads the resolved file.
    - YAML secret blocks stopped at a blank line and missed indentation indicators such as `|2`, and quoted values over 1,000 characters escaped. Blocks now span blank lines, indicators are recognized, and a quoted value that spans lines or never closes within 8,000 characters is redacted whole.
    - Grep output was cut to 240 characters a line before redaction. It is now redacted first.
    - Redaction backtracked on text repeating sensitive names: 1.9 s at 50 KB. Names now start only at token boundaries with bounded affixes, and one-line quoted values are bounded; 200 KB of the worst shapes takes under 200 ms.
- 2026-10-21: **Drew's first labels, and five responses instead of one.** Drew read p01–p07 of `runs/D-v1/review-for-drew.md` and found them "really great", but objected to p18: InlineFieldView is dead code that should be deleted, not documented, and notes like it teach agents to work around problems instead of fixing them. Reviewing all 21 against the code:
  - **False:** p02. The vendored FTS5 shim enables FTS5 without the tag (`//go:build cgo && !fts5`), as an earlier judge round found; verification still proposed it.
  - **Fix instead:** p18, p10 (paths also stale after #21), p19 (a real `select` race in `cancelTurn`), p06 (the tracked bundle is already broken: #19 committed an `index.html` that loads an untracked script), p05, p11, p09.
  - **Guard instead:** p04 and p21 (golden tests with no update flag, outside `make check`), p17, p13, p20.
  - **Document instead:** p15, p01, p08, p07 (13 efforts marked active), p12.
  - **Remember:** p03, p14, p16, and p20 until its guard lands.
  - **Decision (Drew):** the starter becomes a tool for agent effectiveness. SPEC-0124 now routes each finding to remember, document, fix, guard, or ask; adds `AgentFinding` notes with action items, a `documented` decision, and a friction report (US7) that ranks findings by agent cost and checks whether fixes held. SPEC-0045 US6 drafts guard rules that refuse a matching tool call before it runs. Small fixes for p18, p10, p19, p05, p11, p09, p15, p01, and p08 are delegated to Opus 5.5 high on `fix/agent-friction-small`, with a plan only for p06.
  - **Renumbered:** #21 merged `subsystem-refactor.md` as SPEC-0121 while this spec was unmerged, so `rzm validate identifiers` reported a collision after the rebase. This spec is now SPEC-0124; commit messages before this point say SPEC-0121. The frozen judge prompt `tools/sessionmemorybench/JUDGE.md` keeps its old text so cached judgments stay valid.
- 2026-10-21: **Greptile on PR #28: confidence 0, eight comments, all addressed.** AGENTS.md now says Greptile does not review pull requests here on its own; `greptile review --json` runs it from the branch.
  - **Verification inputs:** `AGENTS.md` and search results reached the verifier without containment or redaction. One guarded reader now serves files and `AGENTS.md`, and search output is redacted.
  - **Budget:** a model with no known price reserved $0, so `--budget` could not stop a run. Unpriced bindings are refused unless `--allow-unpriced`. Availability probes ran before any budget check; each is now checked against the budget first, and a budget stop is no longer mistaken for an unavailable model.
  - **Review races:** renew, revise, and retire did not lock, and approval checked before locking. Every change now holds the store lock from its first read to its last write.
  - **Stale approval:** approval fingerprinted today's files though verification read them at its recorded commit. Files that changed since, or a proposal without a commit, now need `--rechecked` after the agent re-checks the statement.
  - **Screening input:** passages cut a paragraph at 700 characters and dropped the rest. Oversized paragraphs now split at word boundaries, which changes screening input for long paragraphs; Jev answers for those passages are no longer cached, and M1's screening numbers were measured with the cut.
  - **Add-on removal:** an add-on that an older setup lists among its workflows stayed installed after `--remove-addon`. It is now dropped from the workflows too.
  - **Docs:** README and the getting-started guide describe `--add-addon`, `--remove-addon`, and `session-memory`. `rzm agent generate` is documented in `pkg/app/mcp/CONTEXT.md`; the README is a getting-started guide and lists no agent commands.
  - **Round 2 (confidence 1, five comments, all fixed):** a shard on `docs/**` went stale by committing itself, so shard notes and decisions no longer count as dependencies. Claude subagent transcripts on Windows were not recognized because of backslash paths. Codex `generate` reported only the last model request's usage, which undercounted spend; it now reports the fresh one-turn thread's total. The harness note says so. The Codex generate test built JSON with unescaped Windows paths. The bench's `passage-labels` failed on map-only runs.
  - **Round 3 (confidence 3, three comments, all fixed):** the availability probe sent a non-strict schema that Codex rejects, so Codex mining could not start. Verification read the working tree but recorded `HEAD`, so a verified edit that was later discarded looked verified; a run now records a commit only when tracked files are clean, and approval otherwise asks for a re-check. A verification that still broke the contract after repair was stored as finished and never retried; it is now kept apart and retried when a later run plans the cluster, at no cost when the packet is unchanged.
  - **Round 4 (confidence 3, two comments, both fixed):** in a small run, screening could drop the single best passage when it alone exceeded the run's share, and the cursor then moved past it; the best passage is now always kept. Session discovery compared Windows transcript paths with Git's forward slashes and skipped those sessions; both sides are now normalized.

## Deviations

None.

## Closure Checklist

- [ ] Spec coverage checklist complete with evidence.
- [ ] SPEC-0124 status and open questions resolved.
- [ ] Documentation plan delivered.
- [ ] Follow-ups recorded.

## Compounding Follow-ups

None yet.

## Status

Active. On 2026-10-11, Drew approved moving forward as a prototype: "move forward in prototype and run some benchmarks to measure efficacy and use Astra and run independent Opus 5.5 extra high reviews to get dialed in." The approved revision is the plan after the Astra round 2 responses.

Prototype scope for this approval:
- Run Batches 1–4 first, as benchmarks of efficacy.
- Use an independent judge panel (Opus 5.5 extra high and Astra high) as a proxy for Drew's labels until Drew reviews the results.
- Prove delivery with a Batch 0 canary.
- Packaging (D10, the full init integration) follows the benchmark results.
