---
type: ReferenceDoc
summary: "Four bounded subscription-backed Luna calls completed; correctness observations, retained infrastructure failure, and adapter confounds prevent an improvement claim."
reference-kind: analysis
last-verified: 2026-09-07
status: active
---

# Agent experience pilot evidence

All four authorized Luna/xhigh launches are consumed, using 153.152 seconds of
the 2400-second campaign limit. The first failed at the infrastructure boundary;
the remaining three passed deterministic correctness checks. The pilot remains
**inconclusive about improvement**: there is no usable A01 baseline, and an
adapter PATH correction changed the environment before the final A02 run.
No run was retried and no alternative model was used.

## Completed subscription continuation

The user explicitly authorized the remaining calls through Codex CLI subscription
authentication. Each continuation preflight confirmed ChatGPT login, the built-in
OpenAI provider, Luna/xhigh, fixture-only discovery, disabled optional surfaces,
native sandbox operation, and 166 protected-path checks. No API keys were passed.

| Run | Seconds | Observed result |
| --- | ---: | --- |
| A01 baseline | 62.136 | Nested sandbox failure; no task changes. Retained below. |
| A02 baseline | 16.760 | Exact one-word README correction; deterministic checks passed. |
| A01 candidate | 59.552 | Correct strict expiration boundary; four visible tests and hidden checks passed. Rhizome-assisted behavior was not established because adapter PATH hid the fixture launcher. |
| A02 candidate | 14.705 | Exact one-word README correction; deterministic checks passed after PATH correction. |

The first two continuation calls used an adapter PATH that omitted `repo/scripts`
and preferred Command Line Tools Python 3.9. A01 candidate reported the missing
Rhizome CLI, found Python 3.14, and verified its fix. Before the final call, PATH
was corrected and native preflight verified shell resolution of the exact fixture
launcher plus Python 3.11 or newer. Both adapter versions and their hashes are
retained. These are evaluator confounds, not evidence of a Rhizome defect.

Both A02 traces made the exact requested edit using two shell commands and one
file edit each. Only A02 candidate supports an observed proportionality result:
its launcher availability was verified, and it used no Rhizome invocation,
session, new note, broad test run, or permission stop. A02 baseline had the same
local trace, but its hidden launcher prevents distinguishing a deliberate local
route from unavailable Rhizome. No general or causal improvement claim follows,
and no human rubric review has been supplied.

Starter-free means no workflow starter was selected. Both arms also installed
the shared `client-harness-builder` and `legacy-codebase-assessor` skills; the
fixture was not limited to the base Rhizome skill alone.

The [continuation summary](agent-experience-evaluation-evidence/continuation/summary.json)
records direct terminal usage, hashes, reservation continuity, source revisions,
and adapter provenance; adjacent per-run events, patches, checks, and final text
provide the trace. Its raw lexical Rhizome-match counter is not a count of CLI
invocations: the A01 trace only read skill text and checked command availability.

## Retained first launch

| Field | Observed result |
| --- | --- |
| Case | A01 constrained expiration bug, baseline |
| Baseline source | `3b93489c4e404981fc4bb193e75c0d70e9eac7a7` |
| Prepared candidate | `d88cbf23a2d2f27c1e61f9ec014ce2e05122dea5`; used by the later continuation |
| Model / reasoning | `gpt-5.6-luna` / `xhigh` |
| Process | Completed normally in 62.136 seconds; task blocked |
| Provider usage | 115,914 input; 97,536 cached input; 2,326 output; 1,404 reasoning output tokens |
| Task mutation | No added, removed or modified fixture sources |
| Hidden checks | Boundary test still fails, as expected for an unchanged fixture |
| Human behavioral score | Pending; no correctness/efficiency comparison is supportable |

Usage comes directly from the terminal `turn.completed` event. Cached and
reasoning counts are reported components, not extra totals to add. The JSONL
stream exposes no executed tool records, while messages and stderr show failed
tool attempts. Therefore zero emitted tool records must not be described as zero
attempted tool calls. No dollar cost is inferred from subscription usage.

## Failure and correction

Zero-call discovery initially proved that the prompt contained only installed
fixture guidance, optional MCP/features were disabled, personal/support reads
were denied, and source/guidance/binary hashes matched. It failed to prove that
Codex could start its own task sandbox inside the additive Darwin sandbox.

The actual launch repeatedly returned `sandbox_apply: Operation not permitted`.
The agent reported that blocker and did not claim a fix or successful tests.
The runner retained the full turn, final response, unchanged diff, failed hidden
checks and usage. Process completion remains distinct from task completion.

The first correction added a nested native sandbox smoke check. It blocked the
three then-unused cells with zero model calls, preserving their budget.
The failure is in the adapter/environment, not evidence against baseline guidance.
A native permission-profile probe allowed normal startup but still included
personal global AGENTS text; that alternative did not meet the discovery contract.
That diagnostic stage left personal configuration untouched and used no bypass.
The later authorized continuation resolved both boundaries as recorded below.
The first failed launch remains in the record and was not replaced.

## Reviewable evidence

- [Four-cell manifest summary and fixture/guidance hashes](agent-experience-evaluation-evidence/pilot-manifest-summary.json)
- [Source build receipt](agent-experience-evaluation-evidence/build-receipt.json)
- [Summary and measured usage](agent-experience-evaluation-evidence/pilot.json)
- [Exact launcher argv, with local paths redacted](agent-experience-evaluation-evidence/command.json)
- [Sanitized raw events](agent-experience-evaluation-evidence/events.jsonl) and [stderr](agent-experience-evaluation-evidence/stderr.txt)
- [Final agent response](agent-experience-evaluation-evidence/final.txt)
- [Hidden checks](agent-experience-evaluation-evidence/checks.json) and [source delta](agent-experience-evaluation-evidence/changes.json)
- [Captured implementation/build provenance](agent-experience-evaluation-evidence/provenance.json)
- [Remaining-cell preflight failures](agent-experience-evaluation-evidence/remaining-preflight.json)

The committed excerpts replace the local home and scratch paths with placeholders;
no task text, failure, or usage values are changed. The summary's event hash refers
to the original raw file, before path redaction. Full local raw evidence is retained
under `Documents/Codex/agent-experience-evaluation-2026-09-07/pilot-results` through
integration review and 30 days afterward; no automatic deletion is configured.

Independent review found and corrected mutation-oracle, provenance, hard-deadline,
partial-event and artifact-error handling defects before this launch. This was a
code review of the harness, not a human evaluation of successful task behavior.
The [plan](agent-experience-evaluation-plan.md) retains the rubric and authority.

## Verified continuation environment

The continuation uses supported native permission profiles with separate clean
Codex homes, carrying only the documented subscription auth cache. Prompt
discovery is checked independently of task permissions; neither proves the other.
Authentication files are excluded from retained and committed evidence.

The native macOS `:minimal` preset grants shared temporary-directory access.
Byte-identical fixtures were therefore relocated outside temporary directories,
preserving their original Git commits, trees, source and guidance hashes. Native
glob denies protect old temporary fixtures, support, oracles and outputs; normal
Git/guidance write protections remain intact. No outer sandbox or bypass is used.

The explicit overlay binds the original manifest, existing ledger checkpoint,
output directory, and remaining launch order. Preflight and execution share the
same permission configuration. The [harness README](../../../scripts/agent-experience-evals/README.md)
describes this path. Raw continuation results and non-secret setup/source evidence
are retained alongside the first launch through integration review and 30 days
afterward. All four slots are consumed; no additional call is authorized here.

## Deterministic fixture readiness

A01/A02 preparation and A06 validation passed. A03–A05 checks used E source
`6b050f23733856429ca67d8ad2839b0fc55051d0`, binary SHA-256
`e54bf4e41f9de114fa94eb316e4fc026b03c024f05d56d1ce7ee1056cba6f703`.
The A03/A04 AE installs each
reported four generated-starter ontology findings; A05 domain reported ten.
Their fixture-specific links and identifiers passed. The [exact issue summary](agent-experience-evaluation-evidence/fixture-validation.json)
retains codes and paths: missing fields on generated README notes and a generated
effort README link to absent engineering guidance. These are existing installed
surface findings, not green full validation; `prepare.py` correctly stops when
full validation fails. These results are revision-specific. The coordinator subsequently reported fixes
in AE `98deb338` and Domain `634c5bd6`, with installed-doc regression evidence.
They must not be reopened as current defects from this older fixture binary.
The affected A03/A04/A05 deterministic install checks now pass with zero issues
against combined staging `1ae34b2b754816df4d39189d4161972df282d483`; the
[separate refresh evidence](agent-experience-evaluation-evidence/combined-fixture-validation.json)
preserves exact build provenance and leaves the historical results intact.
This deterministic refresh is separate from the completed model pilot. No fixture repairs
were applied to shared production templates in this PR.
