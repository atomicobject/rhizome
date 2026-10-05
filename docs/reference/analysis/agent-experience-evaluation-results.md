---
type: ReferenceDoc
summary: "Deterministic code-mode parity and combined starter validation pass; the completed bounded model pilot retains infrastructure and adapter confounds."
reference-kind: analysis
last-verified: 2026-09-07
status: active
---

# Agent experience evaluation results

The deterministic code-mode comparison passes. It establishes matched source
content and structural requirement rows across three transports on the recorded
fixture. It establishes no model benefit or uniform latency advantage.

The separate [Luna pilot](agent-experience-evaluation-pilot.md) consumed all four
authorized calls. Three passed deterministic correctness checks; the first was
unusable because of nested sandbox failure. An adapter PATH confound affecting A02 baseline and A01 candidate, and its
later correction prevent a guidance-quality or timing comparison. No run was retried.

## Recorded contract

Source is E revision `6b050f23733856429ca67d8ad2839b0fc55051d0`, binary SHA-256
`e54bf4e41f9de114fa94eb316e4fc026b03c024f05d56d1ce7ee1056cba6f703`.
The [single manifest](../../../scripts/agent-experience-evals/code-mode-manifest.json)
binds the binary to its build receipt, fixture registry, three tasks, three arms,
and first/repeated deterministic calls. It authorizes zero model launches.

| Task | Checked result in all three arms |
| --- | --- |
| Base context for two Python paths | Both current policies and both superseded historical policies are present with identical source hashes; empty equality cannot pass. |
| Requirement coverage | Accepted `REQ-9010` has no trace; accepted `REQ-9011` has source/spec/story/criterion traces. Raw candidate `REQ-9012` remains visible but is excluded from accepted rows. |
| Trivial lookup | Exact target and policy content agree across arms. |
| Source preservation | Every authored source hash is unchanged. Generated artifacts and index state are outside that source set. |
| Invalid source/trace control | Nonzero validation includes the expected issue codes and fixture paths, rather than merely inheriting generated-starter failures. |

“Covered” here means those structural trace fields are populated. It does not
prove implementation, tests, or source truth. The shared coverage recipe is capped
at 200 rows; the fixture exercises three rows and makes no completeness claim for
larger corpora.

## Measurement limits

Direct and batched arms are CLI-mediated. The typed client covers only `files`
and `file_context`; requirement coverage in that arm is explicitly
`hybrid-common-cli`. It must not be interpreted as a typed recipe operation.

Evidence retains setup, client generation, commands, bytes, repeated calls,
response warnings, and source maps. Typed inner timings exclude Node startup,
imports and client construction; outer timings include those costs. The arms run
sequentially in recorded order, with uncontrolled cache/order effects. Preserve
these scopes instead of ranking transports by a single total or estimating tokens
from output characters.

Fresh installs at this older E revision have the generated README findings
recorded in the [fixture validation evidence](agent-experience-evaluation-evidence/fixture-validation.json).
Those historical results remain unchanged. The affected A03/A04/A05 installs
were refreshed against combined staging `1ae34b2b754816df4d39189d4161972df282d483`
(Base, AE, and Domain), using binary SHA-256
`2fec7e22bb3f795ffde061caae1347f0fc0aac7cfec9fbc6e105613427447ea7`.
All three pass ontology, identifier, and broken-link validation with zero issues.
Repeat init preserves guidance hashes, and every authored fixture hash is unchanged.
The [combined install evidence](agent-experience-evaluation-evidence/combined-fixture-validation.json)
records source/build provenance, explicit disabled-embedding setup, raw evidence
hashes, and per-case checks. This refresh makes no model-behavior claim.

## Evidence and reproduction

[Deterministic evidence summary](agent-experience-evaluation-evidence/code-mode-summary.json)
records the actual manifest, invariant checks, source maps and measurement scopes.
The [manual harness README](../../../scripts/agent-experience-evals/README.md)
contains the command and source-build receipt requirements. Full local raw output
is retained with the pilot evidence in Documents through integration review and
30 days afterward. No automatic cleanup or external artifact upload is configured.
