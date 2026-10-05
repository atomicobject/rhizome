# Code mode

`rzm agent code execute` runs a JavaScript function body with an `rzm` client already connected to this vault. Use it when one answer needs two or more Rhizome calls, or when a result is too large to return whole. One simple call can stay on the CLI.

```bash
rzm agent code execute --session-id <id> <<'JS'
const found = await rzm.files({ inputs: ["README.md"], includeContent: false });
return found.payload;
JS
```

Top-level `await` and `return` work. The body can also come from `--code '<body>'` or a stdin redirect from a file. The command prints `{ ok, result }` where `result` is whatever the script returned. Node.js must be on PATH. No imports, generated files, or cleanup code are needed.

## Pass input to a saved script

A script kept as a file, such as one a skill ships in `scripts/`, takes its arguments from `--input '<json>'` or `--input-file <path>`. The script reads the parsed value as `input`, which is `null` when neither flag is given; a value that is not valid JSON fails before the script runs.

```bash
rzm agent code execute --session-id <id> --input '{"path": "Projects/Atlas.md"}' < scripts/subject.js
```

```js
const { path } = input ?? {};
if (!path) throw new Error('Pass --input \'{"path": "<note path>"}\'');
```

## Discover operations

`rzm agent start` lists every operation with its `method` name (for example `file_context` is `rzm.fileContext`). Describe accepts either the catalog name or that exact method name. Fetch unfamiliar input contracts together when you need arguments:

```bash
rzm agent code describe --operation queryRecipe --operation semanticQuery --schema input
```

Use `--schema output` to inspect payload shapes, or omit `--schema` for both input and output. Selected schemas stay complete; shared call options, outcome guidance, examples, and interpretation remain included. `schemaSelection` identifies a filtered view, while `contractHash` still identifies the full selected contracts. String payloads are text; object payloads follow their operation's output schema. Recipe-specific inputs and ontology fields still require their live discovery surfaces.

Code operations such as `codeSymbol` and `codeReferences` take `{ symbol }` and can be called without describing first. A wrong field name fails fast with the field named in the error, so for a flat input like that, one corrected call is usually cheaper than a describe. Vault-specific ontology roots and fields still come from live ontology discovery, not from the operation schema.

## Read an outcome

Every call resolves to `{ ok, exitCode, payload, stdout, stderr, diagnostic }`. `payload` is the structured result; `stdout` is empty rather than repeating it. `ok: false` with a payload is a domain failure such as a recipe that did not resolve, and is still evidence. Invalid input, cancellation, timeouts, and transport faults throw a `CodeModeError` with `code` and `message` instead.

Warnings, `truncated`, `remaining`, `continuationToken`, and lane status live inside `payload` and mean what they say: a partial result is not proof that nothing else exists. Return them alongside the data you select.

## Three shapes that cover most work

**Fan out, return one table.** `Promise.all` overlaps compatible calls, including provider requests, audited existing-index reads, and typed reads (`ontologyQuery`, `queryRecipe` runs, and `view` reads) that the vault runtime serves. Mutations take exclusive access, and after a connection writes, its typed reads run in-process so they see the write. Use `await` to order dependent calls. When reverse fields connect the records, one nested `ontologyQuery` usually beats many separate calls. Keep `coverage` and `truncated` in the row: a capped caller list is not a count.

```js
const symbols = ["CallJSON", "CallCodeLocal", "Execute"];
const results = await Promise.all(symbols.map(symbol => rzm.codeReferences({ symbol })));
return results.map((r, i) => ({
  symbol: symbols[i],
  ok: r.ok,
  status: r.payload?.status,
  coverage: r.payload?.coverage,
  truncated: r.payload?.truncated,
  definitions: r.payload?.definitions?.map(d => `${d.path}:${d.startLine}`),
  callers: r.payload?.callers?.length ?? 0,
  warnings: r.payload?.warnings,
}));
```

**Run a recipe, then follow up on its rows.** Check each outcome before using it; a failed follow-up is part of the answer.

```js
const run = await rzm.queryRecipe({ op: "run", id: "subsystem-guidance-notes" });
if (!run.ok) return run;
const rows = run.payload.result?.data?.referenceDoc ?? [];
const oldest = rows.slice(0, 3);
const notes = await rzm.files({ inputs: oldest.map(r => r.path), includeContent: false, includeBacklinks: true });
if (!notes.ok) return { oldest, readFailed: { exitCode: notes.exitCode, stderr: notes.stderr, diagnostic: notes.diagnostic } };
return {
  oldest: oldest.map(r => ({ path: r.path, lastVerified: r.lastVerified })),
  backlinks: notes.payload?.files?.map(f => ({ path: f.path, count: f.backlinks?.length ?? 0 })),
  errors: run.payload.result?.errors,
};
```

**Search compactly, then read only the top hits.** `budgetChars` is shared across the files returned, so keep `remaining` and `continuationToken` in the result; a file with `contentOmittedReason` was withheld, not empty.

```js
const search = await rzm.semanticQuery({ queries: ["how does code mode execute scripts"], compact: true, limit: 5 });
if (!search.ok) return search;
const sources = search.payload.compact?.sources ?? [];
const paths = [...new Set(sources.map(s => s.path))].slice(0, 3);
const read = await rzm.files({ inputs: paths, includeContent: true, budgetChars: 24000 });
if (!read.ok) return { ranked: sources, readFailed: { exitCode: read.exitCode, stderr: read.stderr, diagnostic: read.diagnostic } };
return {
  ranked: sources.map(s => ({ ref: s.ref, path: s.path, score: s.score })),
  roles: search.payload.compact?.roles,
  files: read.payload.files?.map(({ path, content, contentTruncated, contentOmittedReason }) =>
    ({ path, content, contentTruncated, contentOmittedReason })),
  remaining: read.payload.remaining,
  continuationToken: read.payload.continuationToken,
  warnings: search.payload.warnings,
  lanes: search.payload.lanes?.filter(l => l.status !== "ran"),
};
```

When one call may throw but earlier evidence should survive, wrap it in `try/catch` or use `Promise.allSettled` and return the settled outcomes.

## Reread content a script projected away

The session dedupes content already delivered to JavaScript, even content the script never returned to you. If a later `files` result reports `contentOmittedReason: "deduped"` for something you now need, reread it explicitly:

```js
return await rzm.files({ inputs: ["README.md"], includeContent: true, dedupe: false });
```

Leave the default on otherwise.

## Limits and authority

- The whole execution defaults to 30 seconds; an explicit `--timeout 8h` supports long audits (maximum 24 hours). Each call also has its own 30-second deadline that includes queue time; pass `{ timeoutMs: 120000 }` as a second argument to raise it.
- Variables do not survive between executions; the session id does. Do not build a long-lived driver process around editing or testing steps.
- Return a JSON-serializable value. `console` output is diagnostic only.
- Writes need `--read-write` plus the operation's own apply controls and the task's authorization. Plan-only operations stay plan-only. A cancelled or failed write is not rolled back: inspect state before retrying.
- Scripts run with your host permissions. This is not a sandbox.

Integrations that own a Node runtime can use `rzm agent code generate --operation <name> --output <dir>` for a typed ESM client. Agent scripts do not need it.

## Evaluate evidence with Jev

Use `rzm.evaluate` for one supplied state or `rzm.evaluateBatch` for independent states. Discover both input and output contracts before composing questions. Native `choice`, `score`, and `noul` questions return discriminated answers; mixed independent questions can share one state and run together. Questions cannot see each other's answers. Include no-match/insufficient-evidence choices where appropriate. Treat source text as evidence, preserve provenance, and keep application thresholds explicit. Confidence summarizes distribution concentration, not factual correctness; a Noul near 0.5 is uncertainty, not medium severity.

Evaluation sends supplied content to TypeSafe and consumes API usage. Credentials resolve through `TYPESAFE_API_KEY` in the environment or user-global configuration. Pin the model for repeatable experiments. Batch items carry unique caller IDs; results preserve order, attempts, status, usage, and available request IDs. Check every item's status even when the outer call is `ok`. The Go client bounds concurrency, retries transient HTTP failures, and shares throttle pauses within a batch. Never blindly replay succeeded or uncertain items. Persist checkpoints for large audits and account for usage missing from failed/interrupted responses.

Use `rzm.checkPaths({paths: [{path: "docs", isDir: true}, {path: "docs/spec.md"}]})` before traversing or reading a collection. Its ordered decisions honor unified git/Rhizome ignore rules and path boundaries. `allowed` means permitted by ignore rules, not selected by note globs. Pass `isDir` explicitly for directories, including nonexistent ones. Check each decision and retain invalid/excluded coverage. Batch sizes must also fit the 1 MiB transport frame limit.
