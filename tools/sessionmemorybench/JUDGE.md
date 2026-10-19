# Judging session-memory proposals

You are one of several independent judges for proposals produced by the session-memory benchmark (SPEC-0121). Each proposal is a learning mined from past coding-agent sessions in this repository and verified by another model. A person will later review the same proposals; your labels measure the quality of the pipeline before that review.

Work alone. Do not look for other judges' labels. You may read anything in this repository, and you must check claims against the current code yourself. Do not modify files other than your output file.

For each proposal, answer:

- `correct`: `yes`, `no`, or `unsure`. Is the statement true of the repository at HEAD? Check the files.
- `specific`: `yes` or `no`. Is it about this repository rather than general engineering practice?
- `nonObvious`: `yes` or `no`. Would a capable agent who read the files in `appliesTo` still likely miss it?
- `useful`: `0`, `1`, or `2`. 0: it would not change what a future agent does. 1: it saves some time. 2: it prevents a real mistake or saves substantial searching.
- `scoped`: `yes` or `no`. Do the `appliesTo` patterns and `commands` trigger it where an agent doing the relevant task needs it, without firing on many unrelated files?
- `safe`: `yes` or `no`. Is it safe to follow: it never discards someone else's work and agrees with AGENTS.md and docs/engineering/?
- `publishable`: `yes` or `no`. Is it fit to commit to this public repository: no secrets, no private details about people or clients, nothing embarrassing?
- `verdict`: `accept` if you would approve it as written, or with trivial wording edits; otherwise `reject`.
- `reason`: one sentence naming the deciding factor.

Accept only what you would want delivered to every agent that touches those files. A weak accepted proposal is worse than a rejected good one, because it teaches people to ignore the rest.

Each object has this shape, one per proposal in input order: `{"item": "<item>", "correct": ..., "specific": ..., "nonObvious": ..., "useful": ..., "scoped": ..., "safe": ..., "publishable": ..., "verdict": ..., "reason": "..."}`.

## Output

Write one JSON object: `{"proposals": [ ...objects in input order... ]}`. When the input also has an `alreadyDocumented` list (the first development run only), add `"alreadyDocumented": [{"cluster": "<id>", "pointerHelps": "yes" | "no", "reason": "..."}]`.
