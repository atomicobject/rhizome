// Deterministic stand-ins for `rzm agent generate`, `evaluate-batch`,
// `harness status`, and `search`, plus synthetic sessions. Answers depend
// only on the prompt, so a resumed run gets what a finished run got.
import { execFileSync } from "node:child_process";
import { mkdirSync, mkdtempSync, symlinkSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";

const lib = new URL("../../templates/starters/compounding/agents/skills/compounding/scripts/lib/", import.meta.url);
const P = await import(new URL("prompts.mjs", lib));
const { completeThrough } = await import(new URL("episodes.mjs", lib));

export const statuses = (over = {}) => [
  { kind: "claude", installed: true, loggedIn: true, version: "2.1.300", models: [{ id: "haiku" }, { id: "sonnet" }, { id: "opus" }], ...over },
  { kind: "codex", installed: false, loggedIn: false, models: [] },
];

const ALIASES = { haiku: "claude-haiku-5-5", sonnet: "claude-sonnet-5-5", opus: "claude-opus-5-5" };
const between = (text, tag) => (text.match(new RegExp(`<${tag}>\\n([\\s\\S]*?)\\n</${tag}>`))?.[1] ?? "").split("\n").filter(Boolean).map((l) => JSON.parse(l));
const topic = (s) => s.match(/Topic (\w+)/)?.[1];

// `word` rewords every statement; `fail(prompt)` makes a call throw;
// `killAfter` kills the process when that many calls have returned;
// `reported` overrides the model a call reports running; `unreported` names
// an alias whose calls report no model; `costUsd` is each call's own total;
// `dropRank` loses every cluster-ranking answer; `routes` maps a topic to the
// response fields its verification returns instead of `remember`.
export function fakeClients({ word = "states", fail = () => false, killAfter = null, reported = null, unreported = null, costUsd = 0, dropRank = false, badVerify = false, rejectVerify = false, routes = {}, status = statuses() } = {}) {
  const calls = { generate: 0, evaluate: 0, items: [], prompts: [] };
  return {
    calls,
    async generate({ provider, model, effort, prompt }) {
      if (killAfter !== null && calls.generate >= killAfter) process.kill(process.pid, "SIGKILL");
      calls.generate++;
      calls.prompts.push({ model, effort, prompt });
      if (fail(prompt)) throw new Error("harness command failed: exit 1: transient");
      const ran = model === unreported ? "" : reported ?? ALIASES[model] ?? model;
      const reply = (output) => ({ provider: provider ?? "claude", model: ran, output, usage: { inputTokens: Math.ceil(prompt.length / 4), outputTokens: 100, costUsd }, toolPolicy: "none" });
      if (prompt.startsWith(P.MAP_PROMPT)) {
        const candidates = between(prompt, "episodes").flatMap((e) => {
          const t = e.text.match(/LEARN (\w+)/)?.[1];
          return t ? [{ kind: "fact", statement: `Topic ${t} ${word} a durable fact.`, why: "It costs time.", appliesTo: [], commands: [], evidence: [e.id], strength: "strong" }] : [];
        });
        return reply({ candidates });
      }
      if (prompt.startsWith(P.GROUP_PROMPT)) {
        const existing = between(prompt, "existing-clusters");
        const byTopic = new Map();
        for (const c of between(prompt, "candidates")) byTopic.set(topic(c.statement), [...(byTopic.get(topic(c.statement)) ?? []), c]);
        const clusters = [...byTopic].map(([t, cs]) => ({ existing: existing.find((x) => topic(x.statement) === t)?.id ?? "", members: cs.map((c) => c.id), kind: "fact", statement: cs[0].statement, appliesTo: [], commands: [], checkable: "code" }));
        return reply({ clusters });
      }
      if (prompt.startsWith(P.PACKET_PREFIX)) {
        const statement = prompt.match(/"statement": "([^"]*)"/)[1];
        // A rejection may still carry the topic's response fields, which mining must drop.
        if (rejectVerify) return reply({ decision: "reject", rejectReason: "single_session", notes: "seen once", ...routes[topic(statement)] });
        // An over-long statement breaks the proposal contract even after repair.
        return reply({ decision: "propose", kind: "fact", statement: badVerify ? statement.padEnd(500, " and more") : statement, why: "It costs time.", appliesTo: [], commands: [], documentation: "none found", verification: [{ path: "README.md", checked: "read it" }], notes: "", response: "remember", responseReason: "A lasting fact.", ...routes[topic(statement)] });
      }
      // A repair returns the proposal unchanged, so a bad answer stays bad.
      if (prompt.startsWith("Revise this verified proposal")) return reply(JSON.parse(prompt.slice(prompt.lastIndexOf("\n\n") + 2)));
      return reply({ ok: true });
    },
    async evaluateBatch({ items }) {
      calls.evaluate++;
      calls.items.push(...items.map((i) => i.id));
      const answer = (s) => (s.passage !== undefined ? { p_reusable: { noul: /LEARN/.test(s.passage) ? 0.9 : 0.1 }, p_surprise: { noul: 0.1 } } : { worth: { noul: 0.8 }, checkable: { noul: /env/.test(s.statement) ? 0.1 : 0.9 } });
      return { items: items.map((i) => (dropRank && i.id.startsWith("k-") ? { id: i.id, status: "uncertain" } : { id: i.id, status: "succeeded", response: { answers: answer(i.state) } })), usage: { input_tokens: 100 * items.length } };
    },
    harnessStatus: async () => status,
    search: async () => "(no results)",
  };
}

const filler = (n) => `Filler paragraph ${n} describes routine progress on the change without anything a later engineer would reuse, padded to stay a passage of its own.`;

// One turn: a person asks, the agent replies with a learning and filler.
export function turn(i, learn, { reply = true } = {}) {
  const msgs = [{ role: "user", kind: "text", text: `Please look into area ${i} and report back.`, origin: "person", confidence: "likely", basis: ["test"] }];
  msgs.push({ role: "assistant", kind: "tool_call", tool: "Read", reads: [`src/f${i}.go`], edits: [], readOnly: true });
  if (reply) msgs.push({ role: "assistant", kind: "text", text: [`LEARN ${learn}: the loader in area ${i} reads its configuration once at startup, so edits need a restart before they take effect anywhere.`, filler(1), filler(2), filler(3)].join("\n\n") });
  return msgs;
}

export function session(id, turns, start) {
  const messages = turns.flat().map((m, i) => ({ ...m, i, ts: new Date(Date.parse(start) + i * 1000).toISOString() }));
  const s = { harness: "claude", id, kind: "main", title: `Session ${id}`, start, family: id, messages };
  s.completeThrough = completeThrough(s);
  return s;
}

export const SESSIONS = {
  s1: () => session("s1", [turn(1, "alpha"), turn(2, "beta")], "2026-10-10T10:00:00Z"),
  s2: () => session("s2", [turn(3, "alpha"), turn(4, "envgamma")], "2026-10-10T11:00:00Z"),
  s3: () => session("s3", [turn(5, "alpha"), turn(6, "delta")], "2026-10-10T12:00:00Z"),
};

export const fakeDiscover = (list) => () => ({ sessions: list.map((s) => (typeof s === "function" ? s() : s)), report: { matched: list.length } });

// A committed scratch repository with a store package and its doc.
export function scratchRepo() {
  const root = mkdtempSync(join(tmpdir(), "sm-shards-"));
  const git = (...a) => execFileSync("git", a, { cwd: root, stdio: "ignore" });
  git("init", "-q");
  git("config", "user.email", "test@example.com");
  git("config", "user.name", "Test");
  mkdirSync(join(root, "pkg/store"), { recursive: true });
  mkdirSync(join(root, "docs"), { recursive: true });
  writeFileSync(join(root, "pkg/store/db.go"), "package store\n");
  writeFileSync(join(root, "docs/store.md"), "# Store\n");
  git("add", "-A");
  git("commit", "-qm", "init");
  return root;
}

// A committed repository for packet verification to read, with a tracked
// symlink to a directory, as skill links are.
export function tempRepo() {
  const dir = mkdtempSync(join(tmpdir(), "sm-repo-"));
  writeFileSync(join(dir, "README.md"), "# Fixture\n");
  mkdirSync(join(dir, "skills"));
  writeFileSync(join(dir, "skills/a.md"), "a\n");
  // Windows needs the type to link a directory.
  symlinkSync("skills", join(dir, "skilllink"), "dir");
  const git = (...a) => execFileSync("git", ["-C", dir, "-c", "user.email=t@example.com", "-c", "user.name=t", ...a], { stdio: "ignore" });
  git("init", "-q");
  git("add", ".");
  git("commit", "-q", "-m", "init");
  return dir;
}
