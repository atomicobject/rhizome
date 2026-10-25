// Evaluates Jev cluster ranking (bench.mjs rank) against verifier decisions and
// both-judge acceptance in judged runs. Run from the repository root.
import { readFileSync, readdirSync } from "node:fs";
const E = ".rhizome/session-memory/eval/runs";
const rj = (f) => JSON.parse(readFileSync(f, "utf8"));
const auc = (pos, neg) => { if (!pos.length || !neg.length) return null; let w = 0; for (const p of pos) for (const n of neg) w += p > n ? 1 : p === n ? 0.5 : 0; return +(w / pos.length / neg.length).toFixed(2); };
for (const [run, verifiers] of [["D-v1", ["D-v1"]], ["D-v1-jev35", ["D-v1-jev35", "verify-opusmed", "verify-sonnet55", "verify-packet-sonnet55"]]]) {
  const rank = new Map(rj(`${E}/${run}/rank.json`).scores.map((s) => [s.cluster, s]));
  const clusters = rj(`${E}/${run}/clusters.json`).clusters;
  const acc = new Set(rj(`${E}/${run}/accepted-both.json`));
  const accIds = new Set(rj(`${E}/${run}/proposals.json`).filter((p) => acc.has(p.cluster)).flatMap((p) => [p.cluster, ...(p.mergedFrom ?? [])]));
  console.log(`\n== ${run}: ${clusters.length} clusters`);
  // checkable vs map tier (B = checkable single-family, C = not)
  const bc = clusters.filter((c) => c.tier !== "A");
  console.log("checkable AUC, tier B vs C:", auc(bc.filter((c) => c.tier === "B").map((c) => rank.get(c.id).checkable), bc.filter((c) => c.tier === "C").map((c) => rank.get(c.id).checkable)));
  for (const v of verifiers) {
    const vs = readdirSync(`${E}/${v}/verify`).map((f) => rj(`${E}/${v}/verify/${f}`)).filter((x) => x.ok);
    const prop = vs.filter((x) => x.result.decision === "propose").map((x) => rank.get(x.cluster).worth);
    const rej = vs.filter((x) => x.result.decision !== "propose").map((x) => rank.get(x.cluster).worth);
    console.log(`worth AUC, ${v} propose vs reject (${prop.length}/${vs.length}):`, auc(prop, rej));
  }
  const verified = readdirSync(`${E}/${run}/verify`).map((f) => f.replace(".json", ""));
  const a = verified.filter((id) => accIds.has(id)).map((id) => rank.get(id).worth);
  const n = verified.filter((id) => !accIds.has(id)).map((id) => rank.get(id).worth);
  console.log(`worth AUC, judge-accepted vs rest among verified (${a.length}/${verified.length}):`, auc(a, n));
  // Budget simulation within verified tier B: top-k by worth vs all
  const tb = verified.filter((id) => clusters.find((c) => c.id === id)?.tier === "B").sort((x, y) => rank.get(y).worth - rank.get(x).worth);
  const accB = tb.filter((id) => accIds.has(id));
  for (const k of [10, 20]) console.log(`tier B verified ${tb.length}, accepted ${accB.length}; Jev top ${k} keeps ${tb.slice(0, k).filter((id) => accIds.has(id)).length}`);
  const ws = [...rank.values()].map((s) => s.worth).sort((x, y) => x - y);
  console.log("worth quartiles:", [0.25, 0.5, 0.75].map((q) => ws[Math.floor(q * ws.length)].toFixed(2)).join(" "));
}
console.log("\n== skip verified clusters below a worth cutoff");
for (const run of ["D-v1", "D-v1-jev35"]) {
  const rank = new Map(rj(`${E}/${run}/rank.json`).scores.map((s) => [s.cluster, s]));
  const acc = new Set(rj(`${E}/${run}/accepted-both.json`));
  const accIds = new Set(rj(`${E}/${run}/proposals.json`).filter((p) => acc.has(p.cluster)).flatMap((p) => [p.cluster, ...(p.mergedFrom ?? [])]));
  const verified = readdirSync(`${E}/${run}/verify`).map((f) => f.replace(".json", ""));
  for (const cut of [0.7, 0.75, 0.78]) {
    const skipped = verified.filter((id) => rank.get(id).worth < cut);
    console.log(run, `cut ${cut}: skips ${skipped.length}/${verified.length} verifies, loses ${skipped.filter((id) => accIds.has(id)).length}/${verified.filter((id) => accIds.has(id)).length} accepted`);
  }
}
