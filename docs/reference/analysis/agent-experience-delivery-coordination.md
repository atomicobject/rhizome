---
type: ReferenceDoc
summary: "Active delivery authority, supervisor identities, ownership decisions, and integration status for the agent experience program."
reference-kind: analysis
last-verified: 2026-09-07
status: active
---

# Agent experience delivery coordination

## Current authority

On 2026-09-07 Drew directed the coordinator to oversee the chats, fix review feedback, merge reviewed child PRs into staging, and keep integration/evaluation work moving. He then clarified: “The threads are sitting idle. I want them driving through delivery.” This supersedes the earlier planning-only dispatch and combined-plan waiting checkpoint for the bounded efforts below. Supervisors were resumed with production, verification, feedback, and merge-ready delivery instructions. Routine choices and fixes proceed; material scope/authority changes return to Drew with a recommendation.

The [preparation brief](agent-experience-preparation.md) owns program scope and the [research handoff](agent-experience-research-handoff.md) preserves reasoning and evidence limits. This record owns later dispatch and coordination decisions. Main merge, release, and publication remain separately authorized actions. Approval records must reflect the real user instruction and verified configured identity, never a fabricated earlier approval. Workers freeze validated current scope targets as they proceed.

## Supervisors and PRs

| Area | Real thread ID | Child PR | Staging disposition |
| --- | --- | --- | --- |
| Base | `01a07bd8-ada0-7fb3-ab1a-cb6aafb67691` | 244 (PR #244) | Merged as `9c0e0f41` |
| Agentic Engineering | `01a07bd8-b8e6-7822-a0c7-eb40f73454a3` | 245 (PR #245) | Merged as `45c84cee` |
| Evaluations | `01a07bd8-9cb9-7770-8dd6-eacf77abf0fc` | 246 (PR #246) | Merged as `5023e60c` |
| Code mode | `01a07bd8-d7c3-7912-9800-cb3274be5987` | 247 (PR #247) | Merged as `b36de660` |
| Complex Domain | `01a07bd8-cc39-7a32-a989-d3f2aa392ddf` | 248 (PR #248) | Merged as `1ae34b2b` |

All target `codex/agent-experience-integration`, staging PR #243. On 2026-09-07 the task list omitted these completed planning chats. Session metadata identified the real IDs; `read_thread` confirmed the base chat and `send_message_to_thread` accepted delivery instructions for all five. Use known IDs and current GitHub evidence rather than treating an incomplete task listing as proof of pending startup.

The delivery messages set every supervisor to `gpt-6-astra` / `low`. Worker policy remains Luna `max` for targeted work, Sol `medium` for moderate implementation, and Astra `low` for subtle work. Evaluation is exclusively Luna `xhigh`.

## Reviewed decisions and ownership

- Independent review found no blocking defects in the base and AE plans. Retain their existing skill/phase topology, implement proactive proportional retrieval and precise evidence authority, preserve AE process/local completion, and enrich resume context. Historical baseline findings remain labeled.
- Base owns its planned canonical files, `base_agent_experience_test.go`, and the needed reference-inventory hunk in `helper_templates_test.go`. It is granted SPEC-0080 and SPEC-0039 amendments. Evaluation sends its US6 wording to Base rather than editing the shared spec concurrently.
- AE owns its planned canonical sources and a separate focused test file, SPEC-0038, and SPEC-0063 amendments. Accepted additive hooks are `alignment.additional-context` in alignment.md and `reconciliation.additional-targets` in reconciliation.md. Preserve other existing hooks.
- Domain owns the hook consumers, its planned retained eight skills and bounded recipes, SPEC-0062, and `complex_domain_recipe_behavior_test.go`. It sends SPEC-0063 changes to AE and does not edit AE sources.
- Domain is granted original-delivery reconciliation notes. Reconcile the old effort's actual delivery and gaps before changing its normative assumptions. Preserve incomplete status and concrete follow-up ownership where needed; do not fabricate completion or require unrelated coverage-view delivery merely to unblock independent redesign. This replaces the earlier blanket close-first prerequisite.
- Code mode is granted the narrow existing-catalog/CLI/new agentcode paths and an allocated new TechnicalSpec for compact selective discovery plus a thin typed CLI client. Existing command payloads, warning/ref semantics and minimal startup remain constraints. Semantic writes, persistent service, full-catalog migration, and automatic init/index generation remain excluded.
- A new passive indexed-query API is not a prerequisite for this pilot. Review confirmed current query preparation may update projection state but found no demonstrated workflow need for a new read mode to test the client hypothesis. Use existing commands in disposable fixtures and disclose index/cache effects. A passive-query capability can be proposed separately with evidence; do not silently relax existing source or startup contracts.

## Evaluation evidence

The four authorized Codex subscription launches used `gpt-5.6-luna` / `xhigh` and consumed the complete pilot budget. Both A02 typo cells made the requested one-word edit. The A01 baseline failed because the nested macOS sandbox prevented task execution, so it is not a usable behavior baseline. The A01 candidate fixed the boundary and passed its four tests, but the adapter PATH used by A02 baseline and A01 candidate hid Rhizome and selected an older Python before recovery. The PATH and shell-resolution preflight were corrected before the final A02 launch. These results demonstrate bounded execution and retained evidence; they do not establish a performance or Rhizome-guidance benefit.

The deterministic code-mode comparison passed the three selected workloads across direct, batched, and typed-client arms. Its evidence establishes operation parity within the fixture and documented hybrid coverage; it is not model-behavior or performance evidence. PR #246 carries the runner, fixtures, reports, and final Luna evidence. All 11 CI checks passed at final child head `dba7b7b9` before merge.

A and E originally proposed incompatible code-mode matrices. The coordinator selected the common workload set: base-context gathering, requirement coverage, and a trivial lookup control. Compare direct per-operation calls (label CLI-mediated versus native truthfully), efficient batched CLI/GraphQL, and the typed progressive client. Native MCP/programmatic tools are optional when actually available, not a fabricated baseline or prerequisite. A owns one later bounded run manifest; neither the competing nine-run nor twelve-run proposal is independently authorized now.

Keep the coverage task within E's real selected operations, rather than adding unsupported code/test proof. Permit isolated writable generated-client/task artifacts while protecting note/code sources. Record expected index/cache effects and include first-use generation/setup costs. Compare repeated-use separately. Agree exact adapters, states, caps, and correctness rubric in A's manifest before those follow-up runs.

## Integration status

All five delivery areas are merged into staging in the dependency-aware order recorded above. Their child CI suites passed before merge. The efforts remain active until combined evidence and durable closure records are complete.

Combined staging regeneration is complete: 57 files across 12 affected skill trees match the `.agents` and `.claude` mirrors, all 27 recipes validate, documentation and frozen-scope validation pass, and no overlay slot markers remain. The authorized refresh preserved team-edited starter docs, persisted source fingerprints, and reported no updates on a second run. `make build` and the full `make check` gate passed on the composed source.

The [Fable 5.1 high review](agent-experience-fable-review.md) is complete at `076f6764`: no blocking correctness defect, one corrected evidence interpretation, and three low-severity follow-ups. The coordinator verified the report and added fresh-install evidence at that source; all-merged full checks and the 34 evaluation tests passed. Main merge, release, and effort closure remain outside this staging record.

## Post-review CI follow-up

The staging Ubuntu unit job at `deee3f29` failed `TestGeneratedClientDeadlineQueueConcurrencyAndClose`: its fixed 100 ms observation saw zero operation starts while the client was still starting its describe/operation subprocesses. The other checks, including Windows units, passed; no review threads were open.

The test now uses a bounded peer barrier and a deliberate 300 ms fake startup delay. Both operations must overlap to succeed, and the assertion still requires exactly two operation starts. Thirty race-enabled runs and the full agentcode package passed. A temporary concurrency-one negative control failed as expected, then was restored. The change affects only the test fixture and assertions; runtime behavior is unchanged. The full `make check CHECK_JOBS=3 GO_TEST_PACKAGES=4 GO_TEST_PARALLEL=4` gate and documentation validation passed before committing this fix. Hosted CI must verify the new head; the earlier failure is retained as its cause.

The active heartbeat `coordinate-rhizome-agent-experience` checks every 20 minutes. It resumes idle work within authorization, routes shared decisions, updates this record and efforts, and reports meaningful outcomes or material input needed. It must not reinstate the superseded planning-only stop.
