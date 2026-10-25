---
name: session-memory-eval
description: Use when measuring or improving the compounding starter in this repository — changing its prompts, Jev screening questions, thresholds, or model defaults, benchmarking a new model, or rerunning the D or H evaluations.
---

# Session-memory evaluation

This is the developer workflow for changing what the `compounding` starter ships. Users tune their own bindings with the shipped skill's calibrate workflow; this skill changes the defaults everyone gets, so every change needs a measured win over a known baseline.

The contract is [SPEC-0124](../../../docs/specs/technical/session-memory-starter.md). Its "Quality evidence" section governs held-out use. The measurement history is the log of [EFF-2026-10-11-15-21](../../../docs/efforts/2026-10-11-15-21-session-memory-starter.md). The numbers to beat are in [baselines.md](baselines.md).

## Tools and data

- **Bench:** `tools/sessionmemorybench/`. `bench.mjs` runs and scores pipeline stages, and `jevlab.mjs` handles passage screening and question search. Usage is at the top of each script.
- **Same code as the starter:** the bench calls the starter's own stage functions, so a result measures what ships. When a stage changes, change it in the starter template and let the bench pick it up.
- **Local data:** evaluation data lives in `.rhizome/session-memory/eval/`, which Git ignores because it holds transcript-derived text. It exists only on the machine that built it.
  - `D/` is the development window and `H/` the held-out window.
  - `runs/` holds one directory per run.
  - `jevlab/` holds passages, Jev answers, gated episode sets, gold labels, and recall results.
- **New machine:** rebuild the sets with the starter's `sessions.mjs extract` over a recorded window. Record the window and versions in the effort log.

## Running an evaluation

1. **State the question.**
   - Name the single variable you change and the baseline it competes with.
   - Estimate the spend.
   - Get approval before any paid run. Every paid run needs explicit approval, and a run that would exceed its estimate needs approval again.
2. **Develop on D.** Tune only against D. H is a one-shot confirmation for a change already frozen on D. A change tuned after seeing H results needs a new held-out window.
3. **Know the noise floor.**
   - Repeat runs differ: two Haiku xhigh extractions of D found 139 and 136 of 192.
   - When baselines.md has no repeat for the baseline you're comparing against, run it twice first.
   - A difference within the noise floor is a tie.
4. **Run.**
   - Hold everything else fixed: episode set, prompts, matcher model, and gold labels.
   - Give each run its own directory under `runs/`.
5. **Score.**
   - Measure recall against the gold labels with `bench.mjs recall --gold-labels`, keeping the matcher at Sonnet 5.5 medium.
   - Report learnings found, candidates, cost, and minutes.
   - Score verifiers with `bench.mjs compare`.
6. **Decide.** Adopt a change only when:
   - it beats the baseline by more than the noise floor; or
   - it ties and costs at least 25% less, or runs at least 25% faster.

   This is the same rule the calibrate workflow applies.
7. **Record.** Add a dated entry to the current effort's log: question, table, spend, and decision. Quote the person's words for any decision they make.
   - When a default changes, update `models.json` and `references/models.md` in the starter template in the same change. Bump the profile `version`, and point the candidate's `evidence` at the log entry.
   - Update baselines.md to the new numbers.

## Gotchas

- **Credentials:** Jev needs the TypeSafe key from the development 1Password Environment, through `./scripts/with-secrets development`. Run it outside the agent sandbox. Never use the release Environment.
- **Running CLIs:** model calls go through the `claude` and `codex` CLIs, which also need to run outside the sandbox. Never select Opus 5. Codex structured output needs strict schemas.
- **What recall mixes:** recall on gated text mixes two losses, what screening dropped and what extraction missed. To separate them, compare against an unscreened read of the same window.
- **Gold labels:**
  - D gold comes from an unscreened Opus map: 192 strong learnings.
  - H gold comes from an unscreened Haiku xhigh map: 208 strong learnings.
  - Gold built by the model under test favors that model; say so when it applies.
- **Small sets mislead:** 13 judge-accepted learnings are too few to separate variants. Use the strong-learning gold.
- **Transcript text stays local.** Effort entries hold counts, costs, and decisions only.
