# Model roles

`models.json` (profile version 2) lists each role's candidates in order. A run uses the first one its harness can reach: Claude Code runs reach `claude`, Codex runs reach `codex`, and Jev (`typesafe`) screens in both. A newer version of a listed family runs at the listed effort and is reported as unmeasured. When a family is unavailable, the run falls back to the role's next candidate from the same provider, never to another provider.

Override a role for one mining run with `--model <role>=<provider>:<model>:<effort>`. Calibration proposes repository overrides in `.rhizome/compounding/models.json`; it writes one only after a person approves the diff and runs `calibrate.mjs apply <evidence-file> --confirm`. An override replaces the role's whole list. Apply puts the measured winner first, retains fallback candidates, and leaves other roles intact.

| Role | What it needs | Claude default | Codex default | Evidence |
| --- | --- | --- | --- | --- |
| `decide` | Cheap yes/no judgments over thousands of passages and clusters. | Jev 1.13.0 | Jev 1.13.0 | Passage screening keeps 85% of strong learnings at 30% of the text. |
| `extract` | Recall: find every reusable learning in the screened passages. | Haiku 5.5, xhigh | GPT-6.1 Terra, xhigh | October 30 Codex comparison: Terra found 106 and 108 of 192 on D and 99 of 208 on historical H, versus Sol low's 90/94 and 87. Terra's H extraction cost was 43% lower. Sol low, then Luna xhigh, remain Codex fallbacks. The earlier Haiku evidence is unchanged. |
| `verify` | Judgment: reject what is false, documented, or obvious from the code. | Sonnet 5.5, medium | GPT-6.1 Terra, low | October 30: two 94-case runs produced repeatable useful fix cores, at 62% lower fresh-input cost and 55% less time than Sol low. Scope and edit details still require proposal review. Sol low remains the Codex fallback. Earlier Claude evidence favored Sonnet over its Opus fallback on cost. |
| `synthesize` | Grouping candidates into clusters across runs. | Opus 5.5, medium (provisional) | GPT-6.1 Sol, medium | October 30: Sol preserved important conditions more reliably in the small fidelity audit. Terra medium was cheaper but fidelity parity was not established. The Claude candidate remains provisional. |
| `match` | Calibration only: judging whether a learning matches a label. | Sonnet 5.5, medium | GPT-6.1 Sol, low | October 30: both Sol low repeats found 23 of 25 reference matches with zero false matches among 29 negatives. Terra missed more matches; Luna produced false matches and an invalid repeat. |

Provisional candidates have no measurement behind them yet. Earlier measurements are in the Rhizome repository's compounding effort log. The approved Codex profile version 2 is documented in `docs/performance/compounding-models-2026-10-30.md` in the Rhizome repository. Its historical H window confirms the choice but is not a newly untouched test. Claude defaults and Jev screening are unchanged.

Calibration freezes `match` independently of the candidates under comparison. It never tunes its judge while scoring extraction. The synthetic fixture in `calibration-fixture.json` measures mechanics and relative cost or latency, not quality on the developer's repository. Local override candidates cite the calibration evidence file that justified them.
