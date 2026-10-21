// Synthetic tests for the session-memory skill scripts. Every credential below
// is fabricated and shaped only to exercise a redaction rule.
import assert from "node:assert/strict";
import { test } from "node:test";

const lib = new URL("../../templates/starters/session-memory/agents/skills/session-memory/scripts/lib/", import.meta.url);
const { redact, shorten } = await import(new URL("redact.mjs", lib));
const { shellPaths, patchPaths, codexExecCalls, relativize } = await import(new URL("paths.mjs", lib));

const fake = {
  anthropic: "sk-ant-api03-" + "Ab1".repeat(10),
  openai: "sk-proj-" + "Zz9".repeat(10),
  github: "ghp_" + "a1B2".repeat(9),
  aws: "AKIA" + "ABCDEFGH12345678",
  pem: "-----BEGIN RSA PRIVATE KEY-----\nMIIfake\\nline2\n-----END RSA PRIVATE KEY-----",
  jwt: ["eyJhbGciOiJIUzI1NiJ9", "eyJzdWIiOiIxMjM0NTY3ODkwIn0", "c2lnbmF0dXJlLWZha2U"].join("."),
  entropy: "Qx7Lm2Vb9Rt4Kp8Zw3Nc6Hy1Js5Fd0Ga",
};

test("redacts every planted credential shape", () => {
  const text = [
    `key ${fake.anthropic}`,
    `OPENAI_API_KEY=${fake.openai}`,
    `token: ${fake.github}`,
    `aws ${fake.aws}`,
    fake.pem,
    `Authorization: Bearer ${fake.jwt}`,
    `"client_secret": "hunter2hunter2"`,
    `postgres://admin:s3cretpass@db.example.com/app`,
    `blob ${fake.entropy}`,
  ].join("\n");
  const { text: out, count } = redact(text);
  for (const value of Object.values(fake)) assert.ok(!out.includes(value), `leaked ${value.slice(0, 8)}`);
  assert.ok(!out.includes("hunter2hunter2"));
  assert.ok(!out.includes("s3cretpass"));
  assert.ok(count >= 9);
});

test("redacts escaped values inside JSON strings", () => {
  const { text } = redact(String.raw`{\"api_key\": \"abcdEFGH1234ijkl\"}`);
  assert.ok(!text.includes("abcdEFGH1234ijkl"));
});

test("keeps commit hashes and repository paths", () => {
  const sha = "184889cb0f2d3e4a5b6c7d8e9f0a1b2c3d4e5f60";
  const path = "web/src/features/notesWorkspace/NotesShell2Panel.tsx";
  const { text } = redact(`commit ${sha} touched ${path}`);
  assert.ok(text.includes(sha));
  assert.ok(text.includes(path));
});

test("redaction runs before shortening so a secret in the middle never survives", () => {
  const long = "a".repeat(5000) + ` ${fake.github} ` + "b".repeat(5000);
  const out = shorten(redact(long).text, 1000);
  assert.ok(!out.includes(fake.github.slice(0, 12)));
  assert.ok(out.length <= 1000);
});

test("shell paths: reads, cd prefixes, redirects, and ignored commands", () => {
  const root = "/repo";
  const r = shellPaths("cd pkg/app && sed -n '1,80p' cli/init/run.go | head -5; rg -n foo ../../docs/specs", root);
  assert.deepEqual(r.reads.sort(), ["docs/specs", "pkg/app/cli/init/run.go"]);
  const w = shellPaths("echo hi > notes/out.md; git diff pkg/x.go; curl https://example.com/a.json", root);
  assert.deepEqual(w.edits, ["notes/out.md"]);
  assert.deepEqual(w.reads, []);
});

test("patch paths include adds, moves, and deletes", () => {
  const patch = "*** Begin Patch\n*** Update File: /repo/a/b.go\n*** Move to: a/c.go\n*** Delete File: old.txt\n*** Add File: new dir/x.md\n*** End Patch";
  assert.deepEqual(patchPaths(patch, "/repo").sort(), ["a/b.go", "a/c.go", "new dir/x.md", "old.txt"]);
});

test("codex code-mode exec scripts expose commands and patches", () => {
  const script = 'text(await tools.exec_command({cmd: "sed -n \'1,40p\' pkg/x.go", workdir: "/repo"}));\nawait tools.apply_patch(`*** Begin Patch\n*** Update File: pkg/x.go\n*** End Patch`)';
  const calls = codexExecCalls(script);
  assert.equal(calls[0].tool, "exec_command");
  assert.match(calls[0].cmd, /sed -n/);
  assert.equal(calls[1].tool, "apply_patch");
  assert.deepEqual(patchPaths(calls[1].patch, "/repo"), ["pkg/x.go"]);
});

test("relativize maps other worktrees of the same repository", () => {
  assert.equal(relativize("/Users/x/.t3/worktrees/rhizome/t3-abc/pkg/a.go", "/Users/x/Developer/rhizome"), "pkg/a.go");
  assert.equal(relativize("/etc/hosts", "/repo"), null);
  assert.equal(relativize("C:\\repo\\pkg\\a.go", "C:\\repo"), "pkg/a.go");
  assert.equal(relativize("D:\\other\\a.go", "C:\\repo"), null);
});

test("redacts quoted values with spaces, escaped quotes, single quotes, and YAML blocks", () => {
  const cases = [
    [`password: "correct horse battery staple"`, "horse battery"],
    [String.raw`{\"client_secret\": \"ab\\\"cd efgh ijkl\"}`, "efgh ijkl"],
    [`API_TOKEN='tok en value 123'`, "en value"],
    [`private_key: |\n  line-one-secret\n  line-two-secret\nnext: ok`, "line-two-secret"],
    [`private_key: |\r\n  line-one-secret\r\n  line-two-secret\r\nnext: ok`, "line-two-secret"],
    [`password: "first line\nsecond-line-secret"`, "second-line-secret"],
    [`api_key = 'first line\nsecond-line-secret'`, "second-line-secret"],
  ];
  for (const [input, leaked] of cases) {
    const { text } = redact(input);
    assert.ok(!text.includes(leaked), `leaked ${leaked} from ${input}`);
  }
  assert.ok(redact(`private_key: |\n  line-one-secret\nnext: ok`).text.includes("next: ok"));
});

test("codex exec calls: any argument order, workdir, and escaped patch text", () => {
  const script = 'await tools.exec_command({workdir: "/repo/pkg", cmd: "cat app/a.go"}); await tools.apply_patch("*** Begin Patch\\n*** Update File: pkg/b.go\\n*** End Patch")';
  const calls = codexExecCalls(script);
  assert.equal(calls[0].cmd, "cat app/a.go");
  assert.equal(calls[0].workdir, "/repo/pkg");
  assert.deepEqual(shellPaths(`cd ${JSON.stringify(calls[0].workdir)} && ${calls[0].cmd}`, "/repo").reads, ["pkg/app/a.go"]);
  assert.deepEqual(patchPaths(calls[1].patch, "/repo"), ["pkg/b.go"]);
});

test("research spans accumulate through a turn despite interim prose", async () => {
  const { episodesFor } = await import(new URL("episodes.mjs", lib));
  const read = (i) => ({ i, role: "assistant", kind: "tool_call", tool: "Read", reads: [`f${i}.go`], edits: [], readOnly: true });
  const prose = (i, text) => ({ i, role: "assistant", kind: "text", text });
  const messages = [
    { i: 0, role: "user", kind: "text", text: "Find out how X works", origin: "person", confidence: "likely" },
    read(1), read(2), read(3), read(4),
    prose(5, "Interim: ".padEnd(260, "x")),
    read(6), read(7), read(8), read(9),
    prose(10, "Finding: ".padEnd(260, "y")),
  ];
  const { episodes } = episodesFor({ harness: "claude", id: "s", kind: "main", title: "", messages }, "s");
  const last = episodes.at(-1);
  assert.equal(last.lens, "research");
  assert.equal(last.toolCalls, 8);
});

test("high-entropy redaction keeps paths and word identifiers", () => {
  for (const keep of ["/Users/x/.t3/worktrees/rhizome/t3-1405af06/web/src/NotesShell2.tsx", "TestPythonFixture_OntologyQuerySchemaSnapshot2", "docs/efforts/2026-10-11-15-21-session-memory-starter.md"]) {
    assert.ok(redact(`see ${keep} here`).text.includes(keep), keep);
  }
  assert.ok(!redact("blob Qx7Lm2Vb9Rt4Kp8Zw3Nc6Hy1Js5Fd0Ga").text.includes("Qx7Lm2Vb9Rt4"));
});

test("directory paths without extensions are kept", () => {
  for (const keep of ["/Users/someone/.t3/worktrees/rhizome/t3-1405af06/pkg/app/cli", "pkg/app/cli/init/templates/starters/session-memory/agents"]) {
    assert.ok(redact(`cd ${keep} now`).text.includes(keep), keep);
  }
  assert.ok(!redact("k Ab3Xk9Qw2Er7Ty1Ui8Op4As6Df0Gh5Jk3L/mN8bV2cX7zQ1wE5rT9yU3iO7pA2sD6").text.includes("Ab3Xk9Qw2Er7"));
});

test("progress notes are redacted before they are shortened", async () => {
  const { episodesFor } = await import(new URL("episodes.mjs", lib));
  const secret = "Pq7Wn3Lk9" + "Xv2Mb8Rt5Yh1Zc4";
  // The quoted value straddles the point where shortening cuts the notes.
  const commentary = `${"a".repeat(850)} API_TOKEN = "${secret}" ${"b".repeat(3000)}`;
  const messages = [
    { i: 0, role: "user", kind: "text", text: "Fix the build", origin: "person", confidence: "likely" },
    { i: 1, role: "assistant", kind: "text", phase: "commentary", text: commentary },
    { i: 2, role: "assistant", kind: "text", phase: "final_answer", text: "Done. ".padEnd(260, "z") },
  ];
  const { episodes, redactions } = episodesFor({ harness: "codex", id: "s", kind: "main", title: "", messages }, "s");
  const text = episodes.map((e) => e.text).join("\n");
  for (let k = 0; k + 6 <= secret.length; k++) assert.ok(!text.includes(secret.slice(k, k + 6)), `fragment ${secret.slice(k, k + 6)} survived`);
  assert.ok(redactions >= 1);
});

const { mergeProfile, resolve } = await import(new URL("models.mjs", lib));
const shipped = JSON.parse(await (await import("node:fs/promises")).readFile(new URL("../../models.json", lib), "utf8"));
const claudeStatus = { kind: "claude", installed: true, loggedIn: true, version: "2.1.280", models: [{ id: "sonnet", efforts: ["low", "medium", "high", "xhigh", "max"] }, { id: "haiku" }] };
const codexStatus = { kind: "codex", installed: true, loggedIn: true, version: "0.163.0", models: [{ id: "gpt-6.1-sol", efforts: ["low", "medium"] }, { id: "gpt-6.2-sol", efforts: ["low", "medium"] }, { id: "gpt-6-luna", efforts: ["xhigh"] }] };

test("the shipped profile names a measured or provisional candidate per provider for every role", () => {
  for (const [role, list] of Object.entries(shipped.roles)) {
    const providers = role === "decide" ? ["typesafe"] : ["claude", "codex"];
    for (const p of providers) assert.ok(list.some((c) => c.provider === p), `${role} has no ${p} candidate`);
    for (const c of list) assert.ok(c.evidence || c.provisional, `${role} ${c.model} needs evidence or provisional`);
  }
});

test("a Claude harness binds Claude models only, through the family alias", async () => {
  const probed = [];
  const probe = async (r) => (probed.push(r), r.model === "haiku" ? "claude-haiku-5-5" : "claude-sonnet-5-5");
  const { bindings } = await resolve(mergeProfile(shipped), { providers: ["claude"], statuses: [claudeStatus, codexStatus], probe });
  assert.deepEqual([bindings.extract.model, bindings.extract.effort, bindings.extract.choice, bindings.extract.unmeasured], ["claude-haiku-5-5", "xhigh", "default", false]);
  assert.equal(bindings.decide.model, "jev-1.13.0");
  assert.ok(probed.every((r) => r.provider === "claude"));
});

test("a newer unmeasured version runs at the measured effort", async () => {
  const probe = async (r) => (r.model === "haiku" ? "claude-haiku-5-7" : "claude-sonnet-5-5");
  const { bindings } = await resolve(mergeProfile(shipped), { providers: ["claude"], statuses: [claudeStatus], probe });
  assert.deepEqual([bindings.extract.model, bindings.extract.effort, bindings.extract.choice, bindings.extract.unmeasured], ["claude-haiku-5-7", "xhigh", "newer-version", true]);
});

test("an unavailable family falls back within the provider, never across", async () => {
  const probe = async (r) => {
    if (r.model === "haiku") throw new Error("model not found");
    return "claude-sonnet-5-5";
  };
  const { bindings } = await resolve(mergeProfile(shipped), { providers: ["claude"], statuses: [claudeStatus, codexStatus], probe });
  assert.deepEqual([bindings.extract.provider, bindings.extract.model, bindings.extract.choice], ["claude", "claude-sonnet-5-5", "fallback"]);
  assert.match(bindings.extract.skipped[0].reason, /model not found/);
});

test("a Codex harness picks the newest catalog version, and a missing role stops the run", async () => {
  const { bindings } = await resolve(mergeProfile(shipped), { providers: ["codex"], statuses: [codexStatus], probe: async (r) => r.model });
  assert.deepEqual([bindings.extract.model, bindings.extract.effort, bindings.extract.choice], ["gpt-6.2-sol", "low", "newer-version"]);
  const flagged = mergeProfile(shipped, null, ["verify=claude:claude-opus-5-5:medium"]);
  await assert.rejects(resolve(flagged, { providers: ["codex"], statuses: [codexStatus], probe: async (r) => r.model }), /no model for verify/);
});

test("verification packets read only regular, non-credential files and redact what they read", async (t) => {
  if (process.platform === "win32") t.skip("symlinks need privileges on Windows");
  const { execFileSync } = await import("node:child_process");
  const { mkdtempSync, symlinkSync, writeFileSync } = await import("node:fs");
  const { tmpdir } = await import("node:os");
  const { join } = await import("node:path");
  const { initialPacket } = await import(new URL("verify.mjs", lib));
  const outside = join(mkdtempSync(join(tmpdir(), "sm-outside-")), "notes.txt");
  writeFileSync(outside, "outside-marker\n");
  const root = mkdtempSync(join(tmpdir(), "sm-verify-"));
  writeFileSync(join(root, "store.go"), `package store // ${fake.github}\n`);
  writeFileSync(join(root, ".env"), "env-marker\n");
  symlinkSync(outside, join(root, "link.txt"));
  execFileSync("git", ["init", "-q"], { cwd: root });
  execFileSync("git", ["add", "-A"], { cwd: root });
  const cl = { statement: "See `store.go`, `link.txt`, and `.env`.", appliesTo: [] };
  const packet = JSON.stringify(await initialPacket({ root, search: async () => "" }, cl, []));
  assert.ok(packet.includes("package store"));
  assert.ok(!packet.includes(fake.github));
  assert.ok(!packet.includes("outside-marker"));
  assert.ok(!packet.includes("env-marker"));
});

test("a dead owner's lock is taken over, a live one is not, and unlock removes only its own lock", async () => {
  const { mkdirSync, mkdtempSync, writeFileSync, existsSync } = await import("node:fs");
  const { hostname, tmpdir } = await import("node:os");
  const { join } = await import("node:path");
  const { openStore } = await import(new URL("store.mjs", lib));
  const dir = mkdtempSync(join(tmpdir(), "sm-lock-"));
  mkdirSync(join(dir, "lock"));
  writeFileSync(join(dir, "lock/owner.json"), JSON.stringify({ pid: 2 ** 22 + 7, host: hostname(), at: "2026-01-01T00:00:00Z", token: "dead" }));
  const a = openStore(dir);
  const b = openStore(dir);
  a.lock();
  assert.throws(() => b.lock(), new RegExp(`locked by pid ${process.pid}`));
  b.unlock();
  assert.ok(existsSync(join(dir, "lock")), "another store's unlock left the lock alone");
  a.unlock();
  assert.ok(!existsSync(join(dir, "lock")));
});
