# Agent sessions

An agent session carries vault context and cross-invocation content deduplication. Use one session for one conversation or coherent work period. Dedupe also counts content a code-mode script received but did not return; `code-mode.md` covers the explicit reread.

## Start or reuse

If a `sessionId` is already available, reuse it. Otherwise:

```bash
rzm agent start --intent "<specific task>"
```

Known files or directories can seed startup:

```bash
rzm agent start \
  --intent "understand and change the authentication boundary" \
  --file src/auth \
  --file docs/authentication.md
```

`--file` is repeatable, but every seed shares the same packed-context budget. Keep the set small, order the most important seed first, and prefer a directory over a blanket list of its trivial files. Add `--submodule-depth 1` only when immediate child-module docs matter. Normal startup does not need `--profile` or `--ontology`.

Keep ordinary code start minimal. Explicit rich selectors such as `--ontology`, `--profile vault`, context-note selectors, note targets, and graph options add only bounded enrichment read from the existing unified index. Start never crawls, repairs, refreshes, or writes that index. When required enrichment is missing, stale, or incompatible, the response retains the minimal root, ancestor, and target guidance and adds a structured warning whose remediation names the appropriate `rzm index` command.

Pass the result to later commands:

```bash
rzm agent file-context --session-id <id> --file <path> --intent "<task>"
rzm agent semantic-query --session-id <id> --query "<question>"
```

Do not restart merely to change the query. A new session is appropriate when the old id is invalid, the project/vault changed, or the work is intentionally independent.

## Discover current commands

`rzm agent start` returns the code-operation catalog used by `code-mode.md` and a pointer to the detailed CLI surface. Call `rzm agent surface` later only when exact flags, examples, command membership, or capability state are needed. Treat it as authoritative for `rzm agent` plus its sole top-level exception, `note-move`; use current top-level help or project docs for every other command family.
