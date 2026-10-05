---
type: ReferenceDoc
reference-kind: analysis
summary: "Frozen six-run subscription campaign comparing direct CLI use with one-process persistent code mode."
status: active
last-verified: 2026-09-07
---

# Persistent code-mode evaluation

The six launches are complete. See [[persistent-code-mode-evaluation-results|results and integrity correction]] for the observed outcomes and limitations. The campaign definition below remains the predeclared design.

## Claim and design

This campaign implements the six-launch evaluation authorized in
[[persistent-code-mode-delivery-plan]]. It uses `gpt-5.6-luna` with `xhigh`
reasoning through Codex CLI ChatGPT subscription authentication. It uses no API-key
authentication or API billing, retries no run, and has no model fallback.

The design has three paired tasks. Both arms use the same candidate binary,
source revision, installed guidance, authored fixture hashes and Git tree. Each
launch has a separate clean Codex home and execution repository. The only
intended arm difference is the method constraint appended to the task prompt:

| Pair | Fixture | Observable outcome |
| --- | --- | --- |
| P01 | A01 starter-free substantive fix | Exact expiration boundary is invalid, UTC behavior and public API remain stable, focused tests pass. |
| P02 | A03 Agentic Engineering execution/resume | Approved retry limit is implemented, excluded jitter remains absent, and the durable effort records actual verification and handoff. |
| P03 | A05 Domain provenance/coverage | Both source versions and exact locations remain distinct, the 30-day contract stays current, the 14-day proposal stays unresolved, and partial coverage is recorded durably. |

The direct arm requires direct `rzm` commands and permits batching independent
calls in one shell invocation. The persistent arm requires one generated
JavaScript client connected to one `rzm agent code serve` process for Rhizome
operations. Direct commands remain available there only for code-mode discovery,
generation, lifecycle setup and verification without a generated operation.
This is an honest forced-arm comparison. It does not measure natural adoption.

The checked [campaign profile](persistent-code-mode-evaluation.json) fixes the
case order, prompts, methods, model, reasoning, time bounds and six-launch cap.
The original four-run A01/A02 campaign and its consumed ledger remain separate
historical evidence. This campaign does not extend, edit or reuse them.

## Preparation and zero-call preflight

Preparation needs the final executable and an adjacent `build.json` containing
matching `source_sha` and `binary_sha256` fields. The campaign root must be a new
absolute directory outside `/tmp` and `/private/tmp`; Codex 0.153.4's native
minimal profile can read shared temporary directories. The preparer sets
`code.enabled: true` before initial indexing for all three source fixtures, runs
init twice, indexes, validates, commits each starting tree, verifies paired tree
and guidance equality, copies only ChatGPT `auth.json` into six clean homes, and
creates an empty bound ledger. It launches no model.

```sh
python3 scripts/agent-experience-evals/prepare_persistent_campaign.py \
  --binary /absolute/path/to/final/rzm \
  --source-sha 40-character-final-source-sha \
  --root $EVAL_ROOT \
  --auth-source ~/.codex/auth.json
```

Use the returned absolute manifest and environment paths for every check. These
commands launch no model:

```sh
python3 scripts/agent-experience-evals/persistent_runner.py list \
  --manifest $EVAL_ROOT/manifest.json

python3 scripts/agent-experience-evals/persistent_runner.py preflight \
  --manifest $EVAL_ROOT/manifest.json \
  --environment $EVAL_ROOT/environment.json \
  --output $EVAL_ROOT/results

python3 scripts/agent-experience-evals/persistent_runner.py dry-run \
  --manifest $EVAL_ROOT/manifest.json \
  --environment $EVAL_ROOT/environment.json \
  --output $EVAL_ROOT/results
```

Preflight must report `ready: true` for all six runs and `model_calls: 0`. It
checks the exact binary and build receipt, clean Git state, guidance and fixture
hashes, ChatGPT login, Luna/xhigh advertisement, disabled optional surfaces,
native sandbox execution, denied support/personal paths, writable fixture access,
the exact fixture launcher, Python 3.11+, Node 20+, and the method-specific agent
surface. Persistent runs additionally execute zero-call help for
`rzm agent code serve` inside the same native permission profile.

Any preflight failure is a blocker. Fix it before invoking `run`. There is no
API fallback. Do not invoke a different model. Preparation and preflight can be
repeated only before a run reservation; a failed or interrupted evaluated launch
is retained and never retried.

## Authorized launch sequence

Only `persistent_runner.py run` launches an evaluated model. Run one ID at a
time in this fixed order:

1. `p01-direct`
2. `p01-persistent`
3. `p02-direct`
4. `p02-persistent`
5. `p03-direct`
6. `p03-persistent`

For each ID:

```sh
python3 scripts/agent-experience-evals/persistent_runner.py run \
  --manifest $EVAL_ROOT/manifest.json \
  --environment $EVAL_ROOT/environment.json \
  --output $EVAL_ROOT/results \
  --run-id p01-direct
```

Replace only the final ID for the next listed slot. The ledger reserves before
process launch, chains reservation hashes, enforces serial order and refuses a
seventh launch or reused ID. A process error, timeout, interruption or evidence
capture failure remains in the ledger. It does not authorize a replacement.

Each run retains the exact prompt, raw JSONL trace, stderr, final response,
before/after snapshots, patch, added files, deterministic hidden checks, adapter
and build provenance, elapsed wall time, and provider token fields when Codex
emits them. Missing usage is recorded as unavailable; subscription usage is not
converted to dollar cost. Hidden checks establish observable fixture behavior;
the human review still judges correctness, authority, honesty, durable updates,
unnecessary work and efficiency.

After all six runs and review files exist:

```sh
python3 scripts/agent-experience-evals/persistent_runner.py compare \
  --manifest $EVAL_ROOT/manifest.json \
  --output $EVAL_ROOT/results
```

Comparison remains inconclusive without complete evidence, passing deterministic
checks and human review in both arms. Timing is descriptive because six runs do
not support statistical or causal claims.

## Sanitized evidence

Keep raw evidence outside the repository. Before committing excerpts, copy it
through the sanitizer to a new destination. The sanitizer replaces campaign,
fixture, home, executable and user paths and refuses common credential markers
or remaining `/Users/` paths. It never reads or copies the auth cache itself.

```sh
python3 scripts/agent-experience-evals/persistent_sanitize.py \
  --input $EVAL_ROOT/results \
  --output docs/reference/analysis/persistent-code-mode-evaluation-evidence \
  --manifest $EVAL_ROOT/manifest.json \
  --environment $EVAL_ROOT/environment.json
```

Inspect the sanitized diff before staging it. Never commit `environment.json`,
raw homes, auth files, personal config, raw results or prepared repositories.
