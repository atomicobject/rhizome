# Session-memory baselines

These are the numbers a change has to beat. Each row cites its entry in the [effort log](../../../docs/efforts/2026-10-11-15-21-session-memory-starter.md). Replace a row when a measured change wins, and keep the old numbers in the log.

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
| gpt-6.1-sol low (Codex default) | `codex exec` | 110 | 350 | $3.05 API-equivalent | 2026-10-18 Codex extraction |
| gpt-6-luna xhigh | `codex exec` | 97, 83 | ~300 | ~$0.22 | 2026-10-18 Codex extraction |

- **No repeat of the shipped binding.** It has one run, so run it again before using it as a noise floor.
- **Old Codex costs.** The Codex rows ran through `codex exec`, at about 28k tokens of overhead per call. `generate` now uses the app-server at about 1.3k, so their costs overstate what ships.

On held-out H (208 strong gold from an unscreened Haiku xhigh read), Haiku xhigh at 30% passages found 135 (2026-10-18 H check).

## Verification (D-v1-jev35, 94 clusters, recovery of the 12 learnings both judges accepted from Opus high)

| Verifier | Mode | Recovered | Agrees with Opus high | Cost (5m cache) | Log |
| --- | --- | --- | --- | --- | --- |
| Sonnet 5.5 medium (shipped default) | packet | 7 | 81 of 94 | $9.13 | 2026-10-17 M1 |
| Sonnet 5.5 medium | tools | 7 | 81 of 94 | $5.93 | 2026-10-17 M1 |
| Opus 5.5 medium | tools | 8 | 86 of 94 | $11.22 | 2026-10-17 M1 |
| Haiku 5.5 medium | tools | 5 | 76 of 89 | $0.71 | 2026-10-17 M1 |

The packet rows ran with Claude Code's full system prompt. Shipped calls use the lean prompt, so packet costs should now be lower.

## End to end (`mine.mjs`, cold D)

| Setting | Proposals | Accepted D-v1 learnings recovered (of 13) | Cost | Log |
| --- | --- | --- | --- | --- |
| Shipped defaults, 32 tier A + 40 tier B verified | 4 | — | $8.53 | 2026-10-18 M3 |
| Plus 72 held tier B verified | 10 | 3 | $13.44 | 2026-10-18 M3 |

## Ranking

The Jev `checkable` question separates tier B from tier C at AUC 0.91–0.92. The `worth` question is a weak ranker, at AUC 0.62–0.71 (2026-10-17 M1).
