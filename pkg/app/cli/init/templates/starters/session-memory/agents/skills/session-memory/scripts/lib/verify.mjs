// Evidence-packet verification: the script gathers evidence from the
// repository and the model only reads it. The model may ask for more evidence
// (files, regex searches, Rhizome searches) for a bounded number of rounds.
// Requests become argument arrays, never shell text, so a model can never run
// a command of its own.

import { execFileSync } from "node:child_process";
import { existsSync, readFileSync, statSync } from "node:fs";
import { join } from "node:path";
import { LEARNING_FIELDS, STATEMENT_MAX, validateLearning } from "./extract.mjs";
import { PACKET_PREFIX, VERIFY_RULES } from "./prompts.mjs";

const MAX_ROUNDS = 2;
const MAX_REQUESTS = 6;
const FILE_CHARS = 8000;
const SEARCH_CHARS = 3000;
const GREP_CHARS = 2500;

export const VERIFY_SCHEMA = {
  type: "object",
  properties: {
    decision: { enum: ["propose", "reject"] },
    rejectReason: { enum: ["false_now", "already_documented", "obvious_from_code", "generic", "preference", "one_off", "plan_not_done", "conflicts_with_policy", "too_vague", "not_repository_specific", "other"] },
    ...LEARNING_FIELDS,
    documentation: { type: "string" },
    verification: { type: "array", items: { type: "object", properties: { path: { type: "string" }, checked: { type: "string" } }, required: ["path", "checked"] } },
    notes: { type: "string" },
  },
  required: ["decision", "verification", "documentation", "notes"],
};

const REQUESTS = {
  type: "array",
  items: {
    type: "object",
    properties: { kind: { enum: ["file", "grep", "search", "docs_for_file"] }, value: { type: "string" }, startLine: { type: "integer" }, endLine: { type: "integer" } },
    required: ["kind", "value"],
  },
};

const tracked = new Map();
function trackedFiles(root) {
  if (!tracked.has(root)) tracked.set(root, new Set(execFileSync("git", ["-C", root, "ls-files"], { encoding: "utf8", maxBuffer: 64 << 20 }).split("\n").filter(Boolean)));
  return tracked.get(root);
}

export function globRegex(glob) {
  let re = "";
  for (let k = 0; k < glob.length; k++) {
    const ch = glob[k];
    if (ch === "*" && glob[k + 1] === "*") {
      k++;
      if (glob[k + 1] === "/") {
        k++;
        re += "(?:.*/)?";
      } else re += ".*";
    } else if (ch === "*") re += "[^/]*";
    else if (ch === "?") re += "[^/]";
    else re += ch.replace(/[.+^${}()|[\]\\]/g, "\\$&");
  }
  return new RegExp(`^${re}$`);
}

const cap = (s, n) => (s.length > n ? `${s.slice(0, n)}\n[... ${s.length - n} more characters]` : s);

// Searches tracked files; long lines are cut so one minified file cannot fill the packet.
function grep(root, args, max) {
  try {
    const out = execFileSync("git", ["-C", root, "grep", "-n", "-I", ...args], { encoding: "utf8", stdio: ["ignore", "pipe", "pipe"], timeout: 20000, maxBuffer: 16 << 20 });
    return cap(out.split("\n").map((l) => l.slice(0, 240)).join("\n"), max);
  } catch (e) {
    if (e.status === 1) return "(no matches)";
    return `(failed: ${String(e.stderr || e.message).split("\n")[0].slice(0, 200)})`;
  }
}

function readFile(root, path, start = 1, end = 0) {
  if (!trackedFiles(root).has(path)) return `(not a tracked file: ${path})`;
  // Tracked symlinks can point at directories or outside the checkout.
  try {
    if (!statSync(join(root, path)).isFile()) return `(not a regular file: ${path})`;
  } catch {
    return `(unreadable: ${path})`;
  }
  const lines = readFileSync(join(root, path), "utf8").split("\n");
  const from = Math.max(1, start);
  const to = end > 0 ? Math.min(end, lines.length) : lines.length;
  const body = lines.slice(from - 1, to).map((l, i) => `${from + i}\t${l}`).join("\n");
  return `${path} (lines ${from}-${to} of ${lines.length})\n${cap(body, FILE_CHARS)}`;
}

async function fulfil(ctx, r) {
  switch (r.kind) {
    case "file":
      return readFile(ctx.root, r.value, r.startLine ?? 1, r.endLine ?? 0);
    case "grep":
      return grep(ctx.root, ["-E", "--max-count", "3", "-e", r.value, "--"], GREP_CHARS);
    case "search":
      return cap(await ctx.search(["-n", "6", "--raw", "--", r.value]), SEARCH_CHARS);
    case "docs_for_file":
      return cap(await ctx.search(["--mode", "docs_for_code", "-n", "6", "--raw", "-f", r.value]), SEARCH_CHARS);
    default:
      return `(unknown request kind ${r.kind})`;
  }
}

// Identifiers worth looking up: backticked spans and path-like tokens.
function identifiers(texts) {
  const out = new Set();
  for (const t of texts) {
    for (const m of t.matchAll(/`([^`\n]{3,80})`/g)) out.add(m[1].trim());
    for (const m of t.matchAll(/(?<![\w/])((?:[\w.-]+\/)+[\w.-]+|[\w-]+\.(?:go|mjs|js|ts|tsx|md|ya?ml|json|sh|graphql))(?![\w/])/g)) out.add(m[1]);
  }
  return [...out].slice(0, 8);
}

export async function initialPacket(ctx, cl, variants) {
  const files = trackedFiles(ctx.root);
  const sections = [];
  const scope = (cl.appliesTo ?? []).map((g) => {
    const re = globRegex(g);
    const hits = [...files].filter((f) => re.test(f));
    return { glob: g, matches: hits.length, sample: hits.slice(0, 6) };
  });
  sections.push(["appliesTo glob matches among tracked files", JSON.stringify(scope, null, 1)]);
  const texts = [cl.statement, ...variants.map((v) => v.statement), ...(cl.commands ?? [])];
  let opened = 0;
  for (const id of identifiers(texts)) {
    const path = id.replace(/^\.\//, "");
    if (files.has(path) && opened < 3) {
      opened++;
      sections.push([`file ${path}`, readFile(ctx.root, path, 1, 200)]);
    } else if (!files.has(path)) {
      sections.push([`grep -F ${JSON.stringify(id)}`, grep(ctx.root, ["-F", "--max-count", "2", "-e", id, "--"], 1500)]);
    }
  }
  sections.push(["rzm search for the statement", await fulfil(ctx, { kind: "search", value: cl.statement.slice(0, 300) })]);
  const firstFile = scope.flatMap((s) => s.sample)[0];
  if (firstFile) sections.push([`notes governing ${firstFile}`, await fulfil(ctx, { kind: "docs_for_file", value: firstFile })]);
  if (existsSync(join(ctx.root, "AGENTS.md"))) sections.push(["AGENTS.md", cap(readFileSync(join(ctx.root, "AGENTS.md"), "utf8"), 14000)]);
  return sections;
}

const proposalProblems = (r) => (r?.decision === "propose" ? validateLearning(r, { maxBytes: STATEMENT_MAX, candidate: false }) : []);

// Verifies one cluster. `ctx` is { root, search }. Each round is one cached
// call, so a resumed run repeats none that finished.
export async function verifyCluster(caller, ctx, { cl, variants, sources, estimate = 0, tally }) {
  const evidence = await initialPacket(ctx, cl, variants);
  const needSchema = { ...VERIFY_SCHEMA, properties: { ...VERIFY_SCHEMA.properties, decision: { enum: ["propose", "reject", "need_evidence"] }, requests: REQUESTS } };
  const rounds = [];
  let out;
  for (let round = 0; ; round++) {
    const last = round >= MAX_ROUNDS;
    const prompt = `${PACKET_PREFIX}\n\n${VERIFY_RULES}\n\n<candidate>\n${JSON.stringify({ kind: cl.kind, statement: cl.statement, appliesTo: cl.appliesTo, commands: cl.commands, variants }, null, 2)}\n</candidate>\n\n<source-excerpts>\n${sources.map((x) => JSON.stringify(x)).join("\n")}\n</source-excerpts>\n\n<evidence>\n${evidence.map(([h, b]) => `## ${h}\n${b}`).join("\n\n")}\n</evidence>${last ? "\n\nThis is the final round: decide now." : ""}`;
    out = await caller.generate({ role: "verify", stage: "verify", prompt, schema: last ? VERIFY_SCHEMA : needSchema, estimate, tally });
    rounds.push({ promptChars: prompt.length, decision: out?.decision ?? null, requests: out?.requests ?? [] });
    if (out?.decision !== "need_evidence") break;
    for (const r of (out.requests ?? []).slice(0, MAX_REQUESTS)) evidence.push([`requested ${r.kind} ${r.value}${r.startLine ? ` ${r.startLine}-${r.endLine ?? ""}` : ""}`, await fulfil(ctx, r)]);
  }
  let result = { ...out };
  delete result.requests;
  if (proposalProblems(result).length) {
    const fix = await caller.generate({
      role: "verify",
      stage: "verify-repair",
      effort: "low",
      prompt: `Revise this verified proposal to fix the listed problems without changing its meaning, and return the full answer.\nProblems:\n${proposalProblems(result).join("\n")}\n\n${JSON.stringify(result)}`,
      schema: VERIFY_SCHEMA,
      estimate,
      tally,
    });
    if (!proposalProblems(fix).length) result = fix;
  }
  // A proposal that still breaks the output contract never reaches review.
  const problems = proposalProblems(result);
  return { result, rounds, contractProblems: problems };
}
