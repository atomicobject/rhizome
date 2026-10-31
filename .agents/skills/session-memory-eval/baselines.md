# Session-memory baselines

These are the numbers a change has to beat. Each row cites its entry in the [effort log](../../../docs/efforts/2026-10-11-15-21-session-memory-starter.md). Replace a row when a measured change wins, and keep the old numbers in the log.

## Current Codex comparison, 2026-10-30

Use these fresh app-server measurements when comparing Codex bindings. The older `codex exec` rows below are historical. Haiku extraction was not retested; Sonnet medium was run as the frozen scoring and verification reference. Full methods, prices, reasoning sweeps, audits, and limitations are in the [model comparison](../../../docs/performance/compounding-models-2026-10-30.md).

| Extraction binding | D found / 192 | Candidates | Extraction cost | Historical H found / 208 |
| --- | --- | --- | --- | --- |
| Sol low | 90, 94 | 257, 261 | $1.325, $1.335 | 87 |
| Luna xhigh | 85, 81 | 310, 316 | $0.169, $0.181 | 80 |
| Terra medium | 102, 98 | 292, 286 | $0.680, $0.675 | not tested |
| Terra xhigh | 106, 108 | 342, 363 | $0.742, $0.763 | 99 |

D repeat spreads are four learnings for Sol low, Luna xhigh, and Terra medium, and two for Terra xhigh. H bindings were frozen on D before opening H in this campaign. H was used by earlier evaluations, so this is historical confirmation rather than a new untouched test. Terra max found 130 in one D run, at $3.419 and 26.7 minutes; it remains an unconfirmed offline option.

Full verification uses 94 cases at frozen evidence commit `184889cbeb2020b8887364602e4139ae6f062233`, with the current response-classifying prompt. These results are not directly comparable to the older verifier table.

| Verifier | Contract-valid proposals | Cost | Minutes |
| --- | ---: | ---: | ---: |
| gpt-6.1-sol low | 0, 1 | $6.441, $6.264 | 11.5, 11.2 |
| gpt-6.1-terra low | 3, 4 | $2.308, $2.183 | 5.1, 5.2 |

The fresh frozen Sonnet medium reference proposes eleven cases, but reference agreement and contract-valid proposals are not factual accuracy or correct-edit rates. Read the content audits before drawing a verification-quality conclusion. Grouping and calibration matching retain Sol medium and Sol low as the preferred bindings. Luna produced material fidelity errors and one invalid matcher repeat. Jev screening remains fixed; Luna was not tested as a replacement gate.

Drew subsequently approved the OpenAI defaults. Shipped profile version 2 uses Terra xhigh for extraction and Terra low for verification, retaining Sol medium for synthesis and Sol low for matching. Claude defaults and Jev screening are unchanged. The campaign used $60.69 API-equivalent, including scoring and superseded attempts, within the approved $75 cap. Raw inputs, individual judgments, outputs, and the pinned evaluation binary remain ignored and local.

## Screening (Jev `jev-1.13.0`, passage questions `p_reusable` and `p_surprise`)

| Set | Text kept | Screening recall (strong learnings whose passage survives) | Cost | Log |
| --- | --- | --- | --- | --- |
| D, 7,965 passages | 30% | 85% | $0.16 for three questions | 2026-10-17 Jev lab |
| D | 25% / 20% | 80% / 73% | same | 2026-10-17 Jev lab |

The automated question search was rejected on H, so the two hand-written questions remain the baseline (2026-10-18).

## Extraction (D 30% passage set, 192 strong gold, Sonnet 5.5 matcher)

| Binding | Prompt | Found | Candidates | Cost | Log |
| --- | --- | --- | --- | --- | --- |
| Haiku 5.5 xhigh (shipped default) | lean, as shipped | 133 (one run) | 772 | $1.41 | 2026-10-18 lean prompt |
| Haiku 5.5 xhigh | Claude Code default | 139, 136 | 588, 606 | $1.31, $1.34 | 2026-10-18 Haiku effort |
| Haiku 5.5 medium | Claude Code default | 127, 125 | ~460 | $0.83 | 2026-10-18 Haiku effort |
| gpt-6.1-sol low (former Codex default) | `codex exec` | 110 | 350 | $3.05 API-equivalent | 2026-10-18 Codex extraction |
| gpt-6-luna xhigh | `codex exec` | 97, 83 | ~300 | ~$0.22 | 2026-10-18 Codex extraction |

- **No repeat of the shipped binding.** It has one run, so run it again before using it as a noise floor.
- **Old Codex costs.** The Codex rows ran through `codex exec`, at about 28k tokens of overhead per call. `generate` now uses the app-server at about 1.3k, so their costs overstate what ships.

On held-out H (208 strong gold from an unscreened Haiku xhigh read), Haiku xhigh at 30% passages found 135 (2026-10-18 H check).

## Verification (D-v1-jev35, 94 clusters, recovery of the 12 learnings both judges accepted from Opus high)

| Verifier | Mode | Recovered | Agrees with Opus high | Cost (5m cache) | Log |
| --- | --- | --- | --- | --- | --- |
| Sonnet 5.5 medium (shipped default), lean prompt | packet | 4, 7 | 75, 77 of 94 | $5.81, $5.64 | 2026-10-19 verifier noise |
| Sonnet 5.5 medium, Claude Code system prompt | packet | 6 | 79 of 94 | $8.57 | 2026-10-19 verifier noise |
| Sonnet 5.5 medium, M1 packet code | packet | 7 | 81 of 94 | $9.13 | 2026-10-17 M1 |
| Sonnet 5.5 medium | tools | 7 | 81 of 94 | $5.93 | 2026-10-17 M1 |
| Opus 5.5 medium | tools | 8 | 86 of 94 | $11.22 | 2026-10-17 M1 |
| Haiku 5.5 medium | tools | 5 | 76 of 89 | $0.71 | 2026-10-17 M1 |

- **Noise floor:** two identical runs of the shipped verifier recovered 4 and 7, and proposed 11 and 17. A verifier change needs to beat that spread, or to recover more over several runs.
- **Lean prompt kept:** the full Claude Code system prompt cost about 50% more, and its recall fell inside the noise.

## End to end (`mine.mjs`, cold D)

| Setting | Proposals | Accepted D-v1 learnings recovered (of 13) | Cost | Log |
| --- | --- | --- | --- | --- |
| Shipped defaults, 32 tier A + 40 tier B verified | 4 | — | $8.53 | 2026-10-18 M3 |
| Plus 72 held tier B verified | 10 | 3 | $13.44 | 2026-10-18 M3 |

## Ranking

The Jev `checkable` question separates tier B from tier C at AUC 0.91–0.92. The `worth` question is a weak ranker, at AUC 0.62–0.71 (2026-10-17 M1).
