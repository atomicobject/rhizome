// Synthetic Markdown and workflow evidence goes through the complete miner.
// Model answers are deterministic; these tests prove admission, evidence and
// provenance handling, not the quality of a live model's judgment.
import assert from "node:assert/strict";
import { execFileSync } from "node:child_process";
import { mkdirSync, mkdtempSync, readFileSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { test } from "node:test";
import { fakeClients, scratchRepo } from "./fakes.mjs";

const scripts = new URL("../../templates/starters/compounding/module/scripts/", import.meta.url);
const { mine } = await import(new URL("mine.mjs", scripts));
const P = await import(new URL("lib/prompts.mjs", scripts));
const { writeJson } = await import(new URL("lib/store.mjs", scripts));
const temporary = () => mkdtempSync(join(tmpdir(), "sm-workflow-"));
const read = (state, name) => JSON.parse(readFileSync(join(state, name), "utf8"));
const section = (text, tag) => text.match(new RegExp(`<${tag}>\\n([\\s\\S]*?)\\n</${tag}>`))?.[1] ?? "";
const lines = (text, tag) => section(text, tag).split("\n").filter(Boolean).map((line) => JSON.parse(line));
const label = (statement) => statement.match(/^([a-z_]+):/)[1];

const cases = [
  { id: "concept", kind: "fact", file: "docs/concepts.md", text: "Search titles before creating a concept.", statement: "Search meaning as well as titles before creating concept notes.", response: "document" },
  { id: "links", kind: "fact", file: "tools/links.mjs", text: "export const rewrite = (text, oldTitle, newTitle) => text.replaceAll(oldTitle, newTitle);", statement: "Preserve historical display labels when tools/links.mjs redirects a merged note.", response: "fix" },
  { id: "report", kind: "rule", file: "docs/report.md", text: "Write high-level weekly bullets. Use details to frame the update, not as the update.", statement: "Keep recurring weekly reports at the high level agreed in docs/report.md.", reject: "already_documented", origin: "person", confidence: "likely", lens: "correction" },
  { id: "categories", kind: "rule", file: "docs/categories.md", text: "The previous report's categories are examples. Choose categories for the current work.", statement: "Every weekly report must reuse the previous report's categories.", reject: "conflicts_with_policy", origin: "person", confidence: "likely", lens: "correction" },
  { id: "short_bullets", kind: "rule", file: "docs/bullets.md", text: "Keep this one report's bullets short enough for its audience; include useful context in later reports.", statement: "Every weekly report must use only short bullets without supporting context.", reject: "one_off", origin: "unknown", confidence: "unknown", lens: "correction" },
  { id: "delegated", kind: "rule", file: "docs/delegated.md", text: "No standing instruction establishes a required final section.", statement: "The delegated brief proves the author requires every draft to end with a next-step section.", reject: "preference", origin: "unknown", confidence: "unknown", lens: "correction" },
  { id: "next_step", kind: "rule", file: "schemas/concept.json", text: '{"properties":{"nextStep":{"type":"string"}},"required":["title"]}', statement: "schemas/concept.json makes nextStep optional; do not invent a next step.", reject: "already_documented", origin: "unknown", confidence: "unknown", lens: "correction" },
  { id: "reviewer", kind: "rule", file: "docs/review.md", text: "A reviewer must resolve to a Person note before approving a proposal.", statement: "Require a Person reviewer for the approval workflow in docs/review.md.", reject: "already_documented" },
  { id: "obsolete", kind: "fact", file: "tools/publish.mjs", text: "export const publish = (notes) => notes.filter((note) => note.ready);", statement: "tools/publish.mjs selects status sent and supports a drafts flag.", reject: "false_now" },
  { id: "generic", kind: "fact", file: "docs/general.md", text: "General writing reference.", statement: "Use clear sentences when writing any document.", reject: "generic" },
  { id: "one_off", kind: "rule", file: "docs/draft.md", text: "One draft for a single meeting.", statement: "Use exactly two paragraphs for this one meeting draft.", reject: "one_off" },
  { id: "entrypoint", kind: "fact", file: "docs/entrypoint.md", text: "For concept work, inspect links.", extraFile: "docs/concept-review.md", extraText: "Before creating a concept, search by meaning.", statement: "The concept entrypoint omits the search rule in docs/concept-review.md; link that guide where concept work starts.", response: "document", replacement: "For concept work, read [concept review](concept-review.md), then inspect links." },
  { id: "documented_miss", kind: "fact", file: "docs/linked-entrypoint.md", text: "For concept work, read [concept review](concept-review.md).", extraFile: "docs/concept-review.md", extraText: "Before creating a concept, search by meaning.", statement: "Repeat the search rule from docs/concept-review.md because an earlier agent missed it despite the working entrypoint link.", reject: "already_documented" },
  { id: "environment", kind: "fact", file: "docs/environment.md", text: "No local timing guarantee is recorded.", statement: "The author's laptop always needs five seconds before a search returns.", external: true, reject: "not_repository_specific" },
  { id: "ungrouped", kind: "fact", file: "docs/missing.md", text: "Unclassified evidence.", statement: "The grouper supplied no checkability assessment for this claim.", ungrouped: true, reject: "too_vague" },
];

function fixture(selected = cases) {
  const root = scratchRepo();
  for (const c of selected) {
    mkdirSync(join(root, c.file, ".."), { recursive: true });
    writeFileSync(join(root, c.file), c.text + "\n");
    if (c.extraFile) writeFileSync(join(root, c.extraFile), c.extraText + "\n");
  }
  execFileSync("git", ["add", "."], { cwd: root });
  execFileSync("git", ["commit", "-qm", "synthetic workflow evidence"], { cwd: root });
  const episodes = join(temporary(), "episodes.jsonl");
  const filler = "Routine progress without a reusable conclusion. ".repeat(25);
  writeFileSync(episodes, selected.map((c) => JSON.stringify({
    id: `claude:${c.id}:1`, harness: "claude", session: c.id, family: c.id,
    ts: "2026-10-10T10:00:00Z", lens: c.lens ?? "discovery", origin: c.origin ?? "agent", confidence: c.confidence ?? "confirmed",
    text: [`CASE ${c.id}: ${c.statement}`, filler, filler, filler].join("\n\n"),
    context: { previous: c.lens === "correction" ? "An agent draft needing correction." : "Investigate this workflow." },
    files: { reads: [c.file], edits: [] }, toolCalls: 4,
  })).join("\n") + "\n");
  return { root, episodes };
}

function clients({ legacy = null } = {}) {
  const base = fakeClients();
  const seen = { extracts: [], verification: [], ranks: [], screening: 0 };
  const candidate = (c, id) => ({ kind: c.kind, statement: `${c.id}: ${c.statement} See \`${c.file}\`.`, why: "A later agent would repeat the same observed mistake.", appliesTo: [c.file], commands: [], evidence: [id], strength: "strong" });
  return {
    ...base,
    seen,
    async generate(request) {
      const { prompt, schema } = request;
      const reply = (output) => ({ provider: "claude", model: request.model, output, usage: { inputTokens: 100, outputTokens: 100, costUsd: 0 }, toolPolicy: "none" });
      if (prompt.startsWith(P.MAP_PROMPT)) {
        const episodes = lines(prompt, "episodes");
        seen.extracts.push(...episodes);
        return reply({ candidates: episodes.map((e) => candidate(cases.find((c) => c.id === e.text.match(/CASE (\w+):/)[1]), e.id)) });
      }
      if (prompt.startsWith(P.GROUP_PROMPT)) {
        assert.deepEqual(schema.properties.clusters.items.properties.checkable.enum, ["repository", "external"]);
        return reply({ clusters: lines(prompt, "candidates").filter((c) => !cases.find((entry) => entry.id === label(c.statement)).ungrouped).map((c) => ({
          ...c, members: [c.id], existing: legacy?.id ?? "", checkable: cases.find((entry) => entry.id === label(c.statement)).external ? "external" : "repository",
        })) });
      }
      if (prompt.startsWith(P.PACKET_PREFIX)) {
        const c = JSON.parse(section(prompt, "candidate"));
        const entry = cases.find((entry) => entry.id === label(c.statement));
        seen.verification.push({ id: entry.id, sources: lines(prompt, "source-excerpts"), prompt });
        // The real packet reader must supply the current Markdown/schema/code.
        assert.ok(section(prompt, "evidence").includes(entry.text), `missing current evidence for ${entry.id}`);
        if (entry.extraFile) assert.ok(section(prompt, "evidence").includes(entry.extraText), `missing existing guidance for ${entry.id}`);
        const common = { verification: [{ path: entry.file, checked: entry.text }], documentation: "Checked the current governing source.", notes: "Synthetic decision." };
        if (entry.reject) return reply({ ...common, decision: "reject", rejectReason: entry.reject });
        return reply({ ...common, ...c, decision: "propose", response: entry.response, responseReason: "Repair the observed workflow at its source.",
          ...(entry.response === "document" ? { edit: { document: entry.file, old: entry.text, new: entry.replacement ?? `${entry.text}\nSearch by meaning too, and inspect likely matches.` } } : { change: "Keep original display labels when redirecting links and collapse duplicate destinations." }),
        });
      }
      return base.generate(request);
    },
    async evaluateBatch({ items }) {
      return { items: items.map((item) => {
        const passage = item.state.passage;
        if (passage !== undefined) seen.screening++;
        else seen.ranks.push(item);
        const answers = passage !== undefined
          ? { p_reusable: { noul: passage.startsWith("CASE ") ? 0.99 : 0 }, p_surprise: { noul: 0 } }
          : { worth: { noul: 0.8 }, checkable: { noul: 0.1 } };
        return { id: item.id, status: "succeeded", response: { answers } };
      }), usage: { input_tokens: 100 } };
    },
  };
}

for (const unscreened of [false, true]) test(`${unscreened ? "unscreened fallback" : "Jev ranking"} verifies repository-backed workflows and retains rejection controls`, async () => {
  const f = fixture();
  const state = temporary();
  const fake = clients();
  const result = await mine({ ...f, state, harness: "claude", budget: 5, unscreened, log: () => {} }, fake);
  assert.equal(result.status, "done");
  const clusters = Object.fromEntries(Object.values(read(state, "pool/clusters.json")).map((c) => [label(c.statement), c]));
  assert.deepEqual(fake.seen.verification.map((v) => v.id).sort(), cases.map((c) => c.id).sort());
  assert.deepEqual(read(state, "proposals.json").map((p) => [label(p.statement), p.response]).sort(), [["concept", "document"], ["entrypoint", "document"], ["links", "fix"]]);
  for (const c of cases.filter((c) => c.reject)) assert.equal(clusters[c.id].verification.rejectReason, c.reject);
  for (const id of ["environment", "ungrouped"]) {
    assert.equal(clusters[id].tier, "C");
    assert.equal(clusters[id].verification.decision, "reject");
  }
  // Checkability admits a likely correction without upgrading it to tier A,
  // and preserves the uncertainty of a delegated source all the way through.
  for (const [id, origin, confidence] of [["report", "person", "likely"], ["next_step", "unknown", "unknown"], ["delegated", "unknown", "unknown"]]) {
    assert.equal(clusters[id].tier, unscreened ? "B" : "C");
    assert.equal(fake.seen.extracts.find((e) => e.id === `claude:${id}:1`).confidence, confidence);
    assert.deepEqual(fake.seen.verification.find((v) => v.id === id).sources.map((s) => [s.origin, s.confidence]), [[origin, confidence]]);
  }
  assert.equal(fake.seen.screening > 0, !unscreened);
  assert.equal(fake.seen.ranks.length, unscreened ? 0 : cases.length);
  if (!unscreened) assert.match(fake.seen.ranks[0].questions.checkable.instructions, /documentation/);
});

for (const checkable of ["code", "workflow", "environment"]) test(`saved ${checkable} labels remain usable without inventing new evidence`, async () => {
  const entry = cases[0];
  const f = fixture([entry]);
  const state = temporary();
  const legacy = { id: "k-legacy", seq: 1, members: [], kind: entry.kind, statement: `${entry.id}: ${entry.statement} See \`${entry.file}\`.`, appliesTo: [entry.file], commands: [], checkable };
  writeJson(join(state, "pool/clusters.json"), { [legacy.id]: legacy });
  const fake = clients({ legacy });
  const result = await mine({ ...f, state, harness: "claude", budget: 5, unscreened: true, log: () => {} }, fake);
  assert.equal(result.status, "done");
  const cluster = read(state, "pool/clusters.json")[legacy.id];
  assert.equal(fake.seen.verification.length, 1);
  assert.equal(cluster.tier, "B");
});
