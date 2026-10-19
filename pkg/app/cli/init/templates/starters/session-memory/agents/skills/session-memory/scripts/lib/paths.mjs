// Bounded extraction of repository paths from tool calls. Commands are parsed,
// never executed. Anything outside the recognized forms is ignored.

import { posix } from "node:path";

const READ_COMMANDS = new Set(["cat", "sed", "head", "tail", "nl", "less", "more", "bat", "rg", "grep", "wc", "ls", "find", "tree", "jq", "awk"]);
const EDIT_COMMANDS = new Set(["tee", "touch", "mv", "cp", "rm", "mkdir", "trash"]);

function tokenize(segment) {
  const tokens = [];
  const re = /'([^']*)'|"((?:\\.|[^"\\])*)"|(\S+)/g;
  let m;
  while ((m = re.exec(segment))) tokens.push(m[1] ?? m[2] ?? m[3]);
  return tokens;
}

function looksLikePath(token) {
  if (!token || token.startsWith("-") || token.includes("://") || token.includes("$")) return false;
  if (/[*?{}<>|;&()\[\]]/.test(token)) return false;
  if (token === "." || token === "..") return false;
  return token.includes("/") || /\.[A-Za-z0-9]{1,8}$/.test(token);
}

// Make a path repository-relative. `root` is the session's working directory,
// which for worktree sessions is the repository root.
export function relativize(path, root) {
  if (!path) return null;
  let p = posix.normalize(path.replace(/\\/g, "/"));
  const r = (root ?? "").replace(/\\/g, "/").replace(/\/+$/, "");
  if (p.startsWith("/") || /^[A-Za-z]:\//.test(p)) {
    if (r && (p === r || p.startsWith(r + "/"))) p = p.slice(r.length + 1);
    else {
      const wt = p.match(/^.*?\/(?:\.t3\/worktrees\/[^/]+\/[^/]+|\.codex\/worktrees\/[^/]+\/[^/]+)\/(.*)$/);
      if (!wt) return null;
      p = wt[1];
    }
  }
  p = p.replace(/^\.\//, "").replace(/\/+$/, "");
  if (!p || p.startsWith("../") || p.startsWith("~")) return null;
  return p;
}

const READ_ONLY_GIT = /^(log|show|diff|status|blame|grep|ls-files|rev-parse|branch)$/;

// True when every command in a shell line only reads: no redirects to files
// and no command outside the read-only set.
export function isReadOnlyShell(cmd) {
  const text = String(cmd ?? "");
  if (!text.trim() || />{1,2}\s*[^\s&|]/.test(text.replace(/2>&1|>\s*\/dev\/null/g, ""))) return false;
  for (const segment of text.split(/&&|\|\||;|\||\n/)) {
    const tokens = tokenize(segment.trim());
    if (!tokens.length) continue;
    const [head, sub] = tokens;
    if (head === "cd" || READ_COMMANDS.has(head)) continue;
    if (head === "git" && READ_ONLY_GIT.test(sub ?? "")) continue;
    return false;
  }
  return true;
}

// Paths a shell command reads or edits, with a leading `cd` honored.
export function shellPaths(cmd, root) {
  const reads = new Set();
  const edits = new Set();
  let cwd = root;
  for (const segment of String(cmd ?? "").split(/&&|\|\||;|\||\n/)) {
    const tokens = tokenize(segment.trim());
    if (tokens.length === 0) continue;
    let [head, ...args] = tokens;
    if (head === "cd" && args[0]) {
      const target = args[0];
      cwd = target.startsWith("/") ? target : `${cwd ?? ""}/${target}`;
      continue;
    }
    if (head === "git" || head === "sudo" || head === "command") continue;
    const redirect = segment.match(/>{1,2}\s*([^\s;&|]+)/);
    if (redirect && looksLikePath(redirect[1])) {
      const rel = relativize(resolve(redirect[1], cwd), root);
      if (rel) edits.add(rel);
    }
    const bucket = READ_COMMANDS.has(head) ? reads : EDIT_COMMANDS.has(head) ? edits : null;
    if (!bucket) continue;
    for (const arg of args) {
      if (!looksLikePath(arg)) continue;
      const rel = relativize(resolve(arg, cwd), root);
      if (rel) bucket.add(rel);
    }
  }
  return { reads: [...reads], edits: [...edits] };
}

function resolve(arg, cwd) {
  if (arg.startsWith("/")) return arg;
  if (!cwd) return arg;
  return `${cwd.replace(/\/+$/, "")}/${arg}`;
}

// Paths an apply_patch body touches, including moves and deletes.
export function patchPaths(patch, root) {
  const edits = new Set();
  const re = /^\*\*\* (?:Update File|Add File|Delete File|Move to):\s*(.+)$/gm;
  let m;
  while ((m = re.exec(String(patch ?? "")))) {
    const rel = relativize(m[1].trim(), root);
    if (rel) edits.add(rel);
  }
  return [...edits];
}

// Calls inside a Codex code-mode `exec` script. Arguments may come in any
// order; `workdir` changes where relative paths resolve.
const STRING_LITERAL = String.raw`(\`(?:\\.|[^\`\\])*\`|"(?:\\.|[^"\\])*"|'(?:\\.|[^'\\])*')`;

function decodeLiteral(literal) {
  const body = literal.slice(1, -1);
  if (literal.startsWith("`")) return body;
  try {
    return JSON.parse(`"${literal.startsWith("'") ? body.replace(/\\'/g, "'").replace(/"/g, '\\"') : body}"`);
  } catch {
    return body;
  }
}

function argument(objectText, name) {
  const m = objectText.match(new RegExp(String.raw`\b${name}\s*:\s*` + STRING_LITERAL));
  return m ? decodeLiteral(m[1]) : null;
}

export function codexExecCalls(script) {
  const calls = [];
  const src = String(script ?? "");
  const callRe = /tools\.(exec_command|apply_patch|mcp__t3[_-]code__(?:delegate_task|create_threads|t3_thread_launch|t3_thread_send))\s*\(/g;
  let m;
  while ((m = callRe.exec(src))) {
    // The argument text runs to the next tools.* call; bounded to keep this cheap.
    const next = src.slice(callRe.lastIndex).search(/tools\.[A-Za-z_]+\s*\(/);
    const argsText = src.slice(callRe.lastIndex, next < 0 ? callRe.lastIndex + 20000 : callRe.lastIndex + next);
    if (m[1].startsWith("mcp__")) {
      for (const name of ["task", "message"]) {
        const text = argument(argsText, name);
        if (text) calls.push({ tool: "spawn", text });
      }
    } else if (m[1] === "exec_command") {
      const cmd = argument(argsText, "cmd");
      if (cmd) calls.push({ tool: "exec_command", cmd, workdir: argument(argsText, "workdir") });
    } else {
      let patch = null;
      const lit = argsText.match(new RegExp("^\\s*" + STRING_LITERAL)) ?? argsText.match(new RegExp(String.raw`\b(?:input|patch)\s*:\s*` + STRING_LITERAL));
      if (lit) patch = decodeLiteral(lit[1]);
      else {
        const start = argsText.indexOf("*** Begin Patch");
        if (start >= 0) patch = argsText.slice(start);
      }
      if (patch && !patch.includes("\n") && patch.includes("\\n")) patch = patch.replace(/\\n/g, "\n");
      if (patch) calls.push({ tool: "apply_patch", patch });
    }
  }
  return calls;
}
