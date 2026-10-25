// Evidence-packet verification: the script gathers evidence from the
// repository and the model only reads it. The model may ask for more evidence
// (files, regex searches, Rhizome searches) for a bounded number of rounds.
// Requests become argument arrays, never shell text, so a model can never run
// a command of its own.

import { execFileSync } from "node:child_process";
import { readFileSync, realpathSync, statSync } from "node:fs";
import { join, relative, sep } from "node:path";
import { LEARNING_FIELDS, STATEMENT_MAX, validateLearning } from "./extract.mjs";
import { PACKET_PREFIX, VERIFY_RULES } from "./prompts.mjs";
import { redact } from "./redact.mjs";
import { contentHash } from "./shards.mjs";

const MAX_ROUNDS = 2;
const MAX_REQUESTS = 6;
const FILE_CHARS = 8000;
const SEARCH_CHARS = 3000;
const GREP_CHARS = 2500;

// What a person does about a proposal (SPEC-0124 US2), and the artifact each
// response drafts. A guard rule follows SPEC-0045 US6: a tool class, a
// command prefix or a path glob, the reason, and the alternative.
export const RESPONSES = ["remember", "document", "fix", "guard", "ask"];
export const RESPONSE_FIELDS = {
  response: { enum: RESPONSES },
  responseReason: { type: "string" },
  workaround: { type: "boolean" },
  edit: { type: "object", properties: { document: { type: "string" }, old: { type: "string" }, new: { type: "string" } }, required: ["document", "old", "new"] },
  change: { type: "string" },
  guardRule: { type: "object", properties: { tool: { enum: ["read", "edit", "shell", "patch"] }, command: { type: "string" }, path: { type: "string" }, reason: { type: "string" }, alternative: { type: "string" } }, required: ["tool", "reason", "alternative"] },
  question: { type: "string" },
  owner: { type: "string" },
};

export const VERIFY_SCHEMA = {
  type: "object",
  properties: {
    decision: { enum: ["propose", "reject"] },
    rejectReason: { enum: ["false_now", "already_documented", "obvious_from_code", "generic", "preference", "one_off", "plan_not_done", "conflicts_with_policy", "too_vague", "not_repository_specific", "other"] },
    ...LEARNING_FIELDS,
    ...RESPONSE_FIELDS,
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

// Searches tracked files. Git only nominates candidate files; each is then
// read through trackedText and searched after redaction, so a match can never
// show text the protected reader would withhold. Long lines are cut so one
// minified file cannot fill the packet.
// ponytail: the model's pattern runs in-process with no timeout; a
// pathological pattern can stall this run. Move matching to a child process
// with a timeout if that is ever observed.
function grep(ctx, pattern, { fixed = false, perFile, max }) {
  let re;
  try {
    re = new RegExp(fixed ? pattern.replace(/[.*+?^${}()|[\]\\]/g, "\\$&") : pattern);
  } catch {
    return "(failed: invalid pattern)";
  }
  let candidates;
  try {
    candidates = execFileSync("git", ["-C", ctx.root, "grep", "-l", "-z", "-I", fixed ? "-F" : "-E", "-e", pattern, "--", ":(exclude,glob)**/.env*"], { encoding: "utf8", stdio: ["ignore", "pipe", "pipe"], timeout: 20000, maxBuffer: 16 << 20 }).split("\0").filter(Boolean);
  } catch (e) {
    if (e.status === 1) return "(no matches)";
    return `(failed: ${String(e.stderr || e.message).split("\n")[0].slice(0, 200)})`;
  }
  const out = [];
  const from = [];
  let chars = 0;
  for (const path of candidates) {
    const { text, hash } = trackedText(ctx, path);
    if (!text) continue;
    let hits = 0;
    for (const [i, line] of text.split("\n").entries()) {
      if (!re.test(line)) continue;
      const hit = `${path}:${i + 1}:${line.slice(0, 240)}`;
      out.push(hit);
      from.push([path, hash]);
      chars += hit.length + 1;
      if (++hits >= perFile) break;
    }
    if (chars > max) break;
  }
  // Only a hit that survives the final cut was shown.
  const joined = out.join("\n");
  let end = -1;
  out.forEach((hit, i) => {
    end += hit.length + 1;
    if (end <= max) shown(ctx, ...from[i]);
  });
  return out.length ? cap(joined, max) : "(no matches)";
}

// The first hash of each file whose text the verifier was shown, so approval
// is bound to that text even if the file changes mid-run. Search results are
// not file reads: a document seen only there has no hash, and approval asks
// the reviewer to re-check it.
const shown = (ctx, path, hash) => ctx.reads && !ctx.reads.has(path) && ctx.reads.set(path, hash);

// The redacted text of a tracked regular file inside the checkout and the
// hash of its raw content, or a parenthesized reason it was not read.
const CREDENTIAL_FILE = /(^|\/)\.env(\.|$)/;
function trackedText(ctx, path) {
  const { root, exclude } = ctx;
  if (exclude?.(path)) return { skipped: `(withheld calibration label: ${path})` };
  if (!trackedFiles(root).has(path)) return { skipped: `(not a tracked file: ${path})` };
  if (CREDENTIAL_FILE.test(path)) return { skipped: `(credential files are not read: ${path})` };
  // A tracked symlink, or a symlinked parent directory, can point outside the
  // checkout or at a credential file, so the fully resolved path is checked too.
  try {
    const top = realpathSync(root);
    const real = realpathSync(join(root, path));
    if (!real.startsWith(top + sep)) return { skipped: `(outside the checkout: ${path})` };
    if (exclude?.(relative(top, real).split(sep).join("/"))) return { skipped: `(withheld calibration label: ${path})` };
    if (CREDENTIAL_FILE.test(relative(top, real).split(sep).join("/"))) return { skipped: `(credential files are not read: ${path})` };
    if (!statSync(real).isFile()) return { skipped: `(not a regular file: ${path})` };
    const raw = readFileSync(real, "utf8");
    return { text: redact(raw).text, hash: contentHash(raw) };
  } catch {
    return { skipped: `(unreadable: ${path})` };
  }
}

function readFile(ctx, path, start = 1, end = 0) {
  const { text, hash, skipped } = trackedText(ctx, path);
  if (skipped) return skipped;
  const lines = text.split("\n");
  const from = Math.max(1, start);
  const to = end > 0 ? Math.min(end, lines.length) : lines.length;
  // A range past the end shows no text.
  if (to >= from) shown(ctx, path, hash);
  const body = lines.slice(from - 1, to).map((l, i) => `${from + i}\t${l}`).join("\n");
  return `${path} (lines ${from}-${to} of ${lines.length})\n${cap(body, FILE_CHARS)}`;
}

async function searchEvidence(ctx, args) {
  if (!ctx.exclude) return cap(redact(await ctx.search(args)).text, SEARCH_CHARS);
  // Calibration labels must not become proof that their own learning is documented.
  const result = await ctx.search(args.map((a) => a === "--raw" ? "--json" : a));
  if (result.startsWith("(failed:")) return result;
  let sources;
  try {
    sources = JSON.parse(result).sources;
    if (sources === null) sources = [];
  } catch {
    throw new Error("calibration search requires structured sources to withhold labels");
  }
  if (!Array.isArray(sources)) throw new Error("calibration search returned no sources array");
  const paths = [...new Set(sources.map((s) => s.result?.Path || s.result?.NoteID).filter((p) => typeof p === "string" && !ctx.exclude(p)))];
  return cap(paths.map((p) => readFile(ctx, p, 1, 200)).join("\n\n") || "(no matches outside calibration labels)", SEARCH_CHARS);
}

async function fulfil(ctx, r) {
  switch (r.kind) {
    case "file":
      return readFile(ctx, r.value, r.startLine ?? 1, r.endLine ?? 0);
    case "grep":
      return grep(ctx, r.value, { perFile: 3, max: GREP_CHARS });
    case "search":
      return searchEvidence(ctx, ["-n", "6", "--raw", "--", r.value]);
    case "docs_for_file":
      return searchEvidence(ctx, ["--mode", "docs_for_code", "-n", "6", "--raw", "-f", r.value]);
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
  const files = new Set([...trackedFiles(ctx.root)].filter((p) => !ctx.exclude?.(p)));
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
      sections.push([`file ${path}`, readFile(ctx, path, 1, 200)]);
    } else if (!files.has(path)) {
      sections.push([`grep -F ${JSON.stringify(id)}`, grep(ctx, id, { fixed: true, perFile: 2, max: 1500 })]);
    }
  }
  sections.push(["rzm search for the statement", await fulfil(ctx, { kind: "search", value: cl.statement.slice(0, 300) })]);
  const firstFile = scope.flatMap((s) => s.sample)[0];
  if (firstFile) sections.push([`notes governing ${firstFile}`, await fulfil(ctx, { kind: "docs_for_file", value: firstFile })]);
  const agents = trackedText(ctx, "AGENTS.md");
  if (agents.text) {
    shown(ctx, "AGENTS.md", agents.hash);
    sections.push(["AGENTS.md", cap(agents.text, 14000)]);
  }
  return sections;
}

const filled = (v) => typeof v === "string" && v.trim() !== "";

// Problems with a proposal's response and its drafted artifact, each phrased
// as the fix to make. Review applies the same checks to a reviewer's change.
export function responseProblems(r) {
  const problems = [];
  if (!RESPONSES.includes(r.response)) return [`response must be one of ${RESPONSES.join(", ")}`];
  if (!filled(r.responseReason)) problems.push("responseReason must be one sentence on why this response fits");
  if (r.response === "document") {
    if (!filled(r.edit?.document)) problems.push("a document response needs edit.document, the repository path of the document to change");
    if (!filled(r.edit?.old)) problems.push("a document response needs edit.old, text that occurs exactly once in the document");
    if (typeof r.edit?.new !== "string" || r.edit.new === r.edit?.old) problems.push("a document response needs edit.new, the text that replaces edit.old");
  }
  if ((r.response === "fix" || r.response === "guard") && !filled(r.change)) problems.push(`a ${r.response} response needs change, a sketch of the ${r.response === "fix" ? "fix" : "check, test, or hook"}`);
  if (r.response === "ask" && !filled(r.question)) problems.push("an ask response needs question, the decision its owner must make");
  if (r.guardRule) {
    if (r.response !== "guard") problems.push("only a guard response may carry guardRule");
    problems.push(...guardRuleProblems(r.guardRule));
  }
  return problems;
}

// The agent sees a rule's reason and alternative verbatim, so each is short.
export const GUARD_TEXT_MAX = 300;
const WRAPPERS = new Set(["env", "command", "builtin", "exec", "time", "nohup", "sudo"]);
const assignment = (w) => /^[^=/.-][^=/.-]*=/.test(w);
// The hook dispatcher evaluates only plain simple commands (SPEC-0045 US6
// AC3): a literal command word that is no reserved word and no bash or zsh
// builtin but cd, true, false, echo, and pwd. Its notPlain set in
// guard_shell.go is the same list.
const PLAIN_NAME = /^[A-Za-z0-9._/+][A-Za-z0-9._/+-]*$/;
const NOT_PLAIN = new Set(`
	! [[ ]] { } case coproc do done elif else end esac fi for foreach function if in nocorrect repeat select then time until while
	. : alias bg bind break builtin caller command compgen complete compopt continue declare dirs disown enable eval exec exit
	export fc fg getopts hash help history jobs kill let local logout mapfile popd printf pushd read readarray readonly return
	set shift shopt source suspend times trap type typeset ulimit umask unalias unset wait test [
	- autoload bindkey bye chdir compadd comparguments compcall compctl compdescribe compfiles compgroups compquote compset
	comptags comptry compvalues disable echotc echoti emulate float functions getln integer limit log nameref noglob print
	private pushln r rehash sched setopt ttyctl unfunction unhash unlimit unsetopt vared whence where which zcompile zformat
	zle zmodload zparseopts zregexparse zstyle
	cap clone getcap setcap pcre_compile pcre_match pcre_study strftime sysopen sysread sysseek syswrite zsystem zselect zpty
	zsocket ztcp zprof zcurses zstat zgetattr zsetattr zdelattr zlistattr ztie zuntie zgdbmpath zftp
	env sudo doas nohup nice timeout xargs sh bash zsh dash ksh fish
`.trim().split(/\s+/));
const plainCommandWord = (w) => PLAIN_NAME.test(w) && !NOT_PLAIN.has(w.replace(/^.*\//, "").toLowerCase().replace(/\.exe$/, ""));

// Problems with a guard rule the hook dispatcher would skip (SPEC-0045 US6),
// or whose text is too long to show an agent.
export function guardRuleProblems(g) {
  const problems = [];
  if (!["read", "edit", "shell", "patch"].includes(g.tool)) problems.push("guardRule.tool must be read, edit, shell, or patch");
  if (filled(g.command) === filled(g.path)) problems.push("guardRule needs exactly one of command (a command prefix) or path (a glob using *, **, ?)");
  if (filled(g.command) && g.tool !== "shell") problems.push("guardRule.command needs tool shell, since only a shell call has command words; a read, edit, or patch rule needs a path");
  if (filled(g.command)) {
    const all = g.command.trim().split(/\s+/);
    const words = [...all];
    while (words.length && (WRAPPERS.has(words[0]) || assignment(words[0]))) words.shift();
    // Matching splits a call at its operators and drops redirects, so a rule
    // holding one could never match.
    if (/[;&|<>()\n]/.test(g.command)) problems.push("guardRule.command must be one simple command, without shell operators or redirects");
    else if (/['"\\$`]/.test(g.command)) problems.push("guardRule.command must be plain words, without quotes or expansions");
    else if (!words.length || !plainCommandWord(words[0])) problems.push("guardRule.command must name a literal command word, not a glob such as * or a builtin such as eval");
    // The dispatcher never evaluates a command with an unquoted glob or a
    // leading ~, so a rule with one could match only a quoted word.
    else if (all.some((w) => /[*?[]|^~/.test(w))) problems.push("guardRule.command may not use a glob (*, ?, [) or a leading ~ in any word");
  }
  if (filled(g.path) && /^\/|(^|\/)\.{1,2}(\/|$)|\/\/|\/$/.test(g.path.trim())) problems.push("guardRule.path must be a repository-relative glob, without a leading /, ./, or ..");
  // The dispatcher's matcher would expand braces, classes, and escapes that
  // the breadth check reads literally, so only the shard dialect is allowed.
  if (filled(g.path) && /[{}[\]\\]|^!/.test(g.path.trim())) problems.push("guardRule.path may use only literal names, *, **, and ?, not {}, [], \\, or a leading !");
  // The dispatcher's YAML parser rejects these, or reads U+0085, U+2028, and
  // U+2029 as line breaks, which change the text.
  if ([g.command, g.path, g.reason, g.alternative].some((v) => /[\u007f-\u009f\u2028\u2029\ufffe\uffff]|\p{Cs}/u.test(v ?? ""))) problems.push("guardRule text may not contain U+007F to U+009F, U+2028, U+2029, U+FFFE, U+FFFF, or an unpaired surrogate");
  if (!filled(g.reason) || !filled(g.alternative)) problems.push("guardRule needs reason and alternative");
  else if ([...g.reason].length > GUARD_TEXT_MAX || [...g.alternative].length > GUARD_TEXT_MAX) problems.push(`guardRule reason and alternative must each be at most ${GUARD_TEXT_MAX} characters`);
  return problems;
}

// A drafted document edit must apply to the document as it is now. Edits
// are drafted against LF text, as verification reads it, so a document with
// CRLF line endings is matched as LF and keeps CRLF in the result.
// ponytail: a document mixing both endings is matched as it is.
export function applyEdit(text, edit) {
  if (text == null) return { problems: [`edit.document ${edit.document} is not a tracked file`] };
  const crlf = text.includes("\r\n") && !/(^|[^\r])\n/.test(text);
  const lf = (s) => (crlf ? s.replace(/\r\n/g, "\n") : s);
  const body = lf(text);
  const n = body.split(lf(edit.old)).length - 1;
  if (n !== 1) return { problems: [`edit.old occurs ${n} times in ${edit.document}; it must occur exactly once`] };
  const after = body.replace(lf(edit.old), () => lf(edit.new));
  return { problems: [], text: crlf ? after.replace(/\n/g, "\r\n") : after };
}

export const editProblems = (text, edit) => applyEdit(text, edit).problems;

const proposalProblems = (ctx, r) => {
  if (r?.decision !== "propose") return [];
  const problems = [...validateLearning(r, { maxBytes: STATEMENT_MAX, candidate: false }), ...responseProblems(r)];
  if (r.response === "document" && !problems.length) problems.push(...editProblems(trackedText(ctx, r.edit.document).text, r.edit));
  return problems;
};

// Verifies one cluster. `base` is { root, search }. Each round is one cached
// call, so a resumed run repeats none that finished.
export async function verifyCluster(caller, base, { cl, variants, sources, estimate = 0, tally }) {
  const ctx = { ...base, reads: new Map() };
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
  if (proposalProblems(ctx, result).length) {
    const fix = await caller.generate({
      role: "verify",
      stage: "verify-repair",
      effort: "low",
      prompt: `Revise this verified proposal to fix the listed problems without changing its meaning, and return the full answer.\nProblems:\n${proposalProblems(ctx, result).join("\n")}\n\n${JSON.stringify(result)}`,
      schema: VERIFY_SCHEMA,
      estimate,
      tally,
    });
    if (!proposalProblems(ctx, fix).length) result = fix;
  }
  // A proposal that still breaks the output contract never reaches review.
  const problems = proposalProblems(ctx, result);
  // Approval is bound to the document as the verifier was first shown it, not
  // as the edit check reads it after the answer arrives.
  if (!problems.length && result.decision === "propose" && result.response === "document") result.documentHash = ctx.reads.get(result.edit.document);
  return { result, rounds, contractProblems: problems };
}
