# Model roles

`models.json` (profile version 1) lists each role's candidates in order. A run uses the first one its harness can reach: Claude Code runs reach `claude`, Codex runs reach `codex`, and Jev (`typesafe`) screens in both. A newer version of a listed family runs at the listed effort and is reported as unmeasured. When a family is unavailable, the run falls back to the role's next candidate from the same provider, never to another provider.

Override a role for one mining run with `--model <role>=<provider>:<model>:<effort>`. Calibration proposes repository overrides in `.rhizome/compounding/models.json`; it writes one only after a person approves the diff and runs `calibrate.mjs apply <evidence-file> --confirm`. An override replaces the role's whole list. Apply puts the measured winner first, retains fallback candidates, and leaves other roles intact.

| Role | What it needs | Claude default | Codex default | Evidence |
| --- | --- | --- | --- | --- |
| `decide` | Cheap yes/no judgments over thousands of passages and clusters. | Jev 1.13.0 | Jev 1.13.0 | Passage screening keeps 85% of strong learnings at 30% of the text. |
| `extract` | Recall: find every reusable learning in the screened passages. | Haiku 5.5, xhigh | GPT-6.1 Sol, low | Haiku xhigh found 139 and 136 of 192 strong learnings on the development window, and 135 of 208 on the held-out window, for $1.31. Sol low found 110 for $3.05 API-equivalent. GPT-6 Luna at xhigh (97 and 83) is the Codex fallback. |
| `verify` | Judgment: reject what is false, documented, or obvious from the code. | Sonnet 5.5, medium | GPT-6.1 Sol, low (provisional) | Sonnet recovered 7 of the 12 learnings both judges accepted; Opus 5.5 medium, its fallback, recovered 8 at about twice the cost. |
| `synthesize` | Grouping candidates into clusters across runs. | Opus 5.5, medium (provisional) | GPT-6.1 Sol, medium (provisional) | Not yet measured against alternatives. |
| `match` | Calibration only: judging whether a learning matches a label. | Sonnet 5.5, medium | GPT-6.1 Sol, low (provisional) | The matcher for every extraction comparison. |

Provisional candidates have no measurement behind them yet. Measurements and their dates are in the Rhizome repository's compounding effort log.

Calibration freezes `match` independently of the candidates under comparison. It never tunes its judge while scoring extraction. Both providers already have a `match` candidate in profile version 1; the shipped defaults remain unchanged. The synthetic fixture in `calibration-fixture.json` measures mechanics and relative cost or latency, not quality on the developer's repository. Local override candidates cite the calibration evidence file that justified them.
