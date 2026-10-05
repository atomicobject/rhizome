# Skill scripts

For a repeated activity that needs several Rhizome reads, a saved code-mode script (`scripts/*.js`) can provide a general orientation, a specific task such as reviewing one subject or preparing a decision, or both. Not every skill needs an orientation brief; write the scripts its agents would otherwise rebuild call by call. Each runs as `rzm agent code execute --session-id <id> --input '<json>' < scripts/<name>.js` and reads its arguments from `input` (see `references/code-mode.md`).

Put mechanics in the script and keep judgment in the skill's prose:

- Paging, coverage, deduplication, and tracking which route reached each record belong in the script. Page until `extensions.typedRoots` reports `complete`, and return the coverage status with the data.
- Prefer one nested typed query over fanned-out recipe or query calls when reverse fields connect the records. A script may still run a saved recipe that answers part of the question.
- Read the type profile from `ontologyReference` (`profile.lifecycleField`, `gapFields`, `summaryField`, and each enum value's `stage`) instead of hardcoding stage or field names, so the script survives schema edits.
- Select content that answers the activity's question, including readable summaries or authored sections and meaningful relationships. Counts, timestamps, and missing fields describe the collection but do not explain its subjects.
- Treat empty optional fields and absent links as context. Report an obligation or problem only when a declared validation rule, policy, or accepted commitment supports it. Do not infer priority or a next action from absence alone.
- Return a compact object an agent can read in one pass. Keep gaps, warnings, and truncation visible; a capped list must never read as a count.
- Measure wall time and output size, and test the script against a fixture or a disposable copy of the vault, including empty and high-volume cases.

SKILL.md names each script, when to run it, its input, and how to read its output, without repeating the query. Shape each script to its activity rather than copying a template. This example shows typed collection mechanics, not a mandatory brief format. Adapt its content and relationships to the activity before shipping it:

```js
// Where one workflow type stands: counts by stage, active records with
// summaries, open validation issues, and recent changes, from typed reads only.
// rzm agent code execute --session-id <id> --input '{"type":"Project"}' < scripts/brief.js
const { type, days = 14 } = input ?? {};
if (!type) throw new Error('Pass --input \'{"type": "<TypeName>"}\'');

// The type profile names the lifecycle, summary, and gap fields, and each enum
// value declares its stage, so nothing here hardcodes this vault's vocabulary.
const [ref, schema] = await Promise.all([rzm.ontologyReference({ type }), rzm.ontologyQuerySchema({ type })]);
if (!ref.ok) return ref;
if (!schema.ok) return schema;
const def = ref.payload.types[0];
const { lifecycleField: life, summaryField, gapFields = [], shape } = def.profile ?? {};
const field = name => def.fields.find(f => f.name === name);
const stageOf = Object.fromEntries((field(life)?.enum?.values ?? []).map(v => [v.name, v.stage]));
const select = name => field(name)?.kind === "section" && !field(name)?.list
  ? `${name} { content }`
  : ["link", "reverse"].includes(field(name)?.kind) ? `${name} { ... on Node { title path } }` : name;
const selection = [...new Set(["title", "path", "updatedAt", "issueCount", life, summaryField, ...gapFields].filter(Boolean))].map(select).join(" ");

// Page until coverage is complete; a capped list is not a count.
const root = schema.payload.roots?.[0]?.name;
if (!root) return { type, error: "no typed root; pass a concrete type" };
const rows = [];
let coverage = { hasMore: true, nextOffset: 0 };
while (coverage?.hasMore) {
  const page = await rzm.ontologyQuery({ query: `{ rows: ${root}(first: 200, offset: ${coverage.nextOffset}) { ${selection} } }` });
  if (!page.ok || page.payload.errors?.length) return page;
  rows.push(...page.payload.data.rows);
  coverage = page.payload.extensions?.typedRoots?.rows;
}

const flagged = rows.filter(r => r.issueCount);
const active = rows.filter(r => stageOf[r[life]] === "active");
const emptyOptionalFields = r => gapFields.filter(f => r[f] == null || r[f] === "" || (Array.isArray(r[f]) && !r[f].length));
const tally = values => values.reduce((counts, v) => ({ ...counts, [v]: (counts[v] ?? 0) + 1 }), {});
const since = Date.now() - days * 864e5;
return {
  type, shape, coverage: coverage?.status ?? "unknown", total: rows.length,
  byStage: life ? tally(rows.map(r => stageOf[r[life]] ?? "unset")) : undefined,
  inMotion: { total: active.length, first: active.slice(0, 10)
    .map(r => ({ title: r.title, path: r.path, [life]: r[life], summary: field(summaryField)?.kind === "section" ? r[summaryField]?.content : r[summaryField], emptyOptionalFields: emptyOptionalFields(r) })) },
  needsAttention: { total: flagged.length, first: flagged.slice(0, 10).map(r => ({ title: r.title, path: r.path, issues: r.issueCount })) },
  recentlyChanged: rows.filter(r => Date.parse(r.updatedAt) >= since)
    .sort((a, b) => b.updatedAt.localeCompare(a.updatedAt)).slice(0, 10)
    .map(r => `${r.updatedAt.slice(0, 10)} ${r.title}`),
};
```
