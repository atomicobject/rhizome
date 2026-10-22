import { execFileSync } from "node:child_process";
import { readFileSync } from "node:fs";
import { join } from "node:path";
import { approvedHash, learningHash, loadDecisions, loadShards } from "./shards.mjs";
import { writeText } from "./store.mjs";

export function labeledSet(root, store, provider) {
  const episodes = store.read("pool/episodes.json", {});
  const clusters = Object.values(store.read("pool/clusters.json", {}));
  const candidates = store.read("pool/candidates.json", {});
  const reviewed = store.read("reviewed.json", {});
  const labels = [];
  const replay = (item, id, decision, evidence) => {
    const clusterId = item.cluster ?? Object.entries(reviewed).find(([, r]) => r.path === item.path)?.[0];
    const cl = clusters.find((c) => c.id === clusterId) ?? clusters.find((c) => learningHash(c) === learningHash(item));
    const ids = [...new Set(evidence?.length ? evidence : cl?.evidence ?? [])];
    // Retained redacted episodes are the local transcripts. Stubs are not enough.
    if (!ids.length || ids.some((key) => !episodes[key]?.text || episodes[key].harness !== provider)) return;
    labels.push({ id, kind: item.kind, statement: item.statement, appliesTo: item.appliesTo ?? [], commands: item.commands ?? [], target: item.target,
      evidence: ids, decision, variants: (cl?.members ?? []).map((key) => candidates[key]).filter(Boolean).map(({ kind, statement, why, appliesTo, commands, target }) => ({ kind, statement, why, appliesTo, commands, target })) });
  };
  for (const s of loadShards(root)) {
    if (!s.reviewedBy || approvedHash(s) !== s.approvedHash) continue;
    const ids = [...s.evidenceSection.matchAll(/message `([^`]+)`/g)].map((m) => m[1]);
    replay(s, s.path, "propose", ids);
  }
  const approved = labels.length;
  if (approved < 10) return { ...JSON.parse(readFileSync(new URL("../../references/calibration-fixture.json", import.meta.url), "utf8")), localApproved: approved };
  const seen = new Set(labels.map(learningHash));
  for (const d of loadDecisions(root, store.dir)) {
    if (!d.decider || !["dismissed", "retired", "documented"].includes(d.decision) || seen.has(learningHash(d))) continue;
    replay(d, d.learning, "reject");
    seen.add(learningHash(d));
  }
  const ids = new Set(labels.flatMap((l) => l.evidence));
  return { name: "local approved shards and recorded decisions", synthetic: false, localApproved: approved, labels, episodes: [...ids].map((id) => episodes[id]) };
}

// The fixture verifier reads only this synthetic repository, never the user's code.
export function fixtureCheckout(store, id, files) {
  const root = store.path("calibrations", id, "fixture");
  for (const [path, text] of Object.entries(files)) writeText(join(root, path), text);
  execFileSync("git", ["init", "-q", root], { stdio: "pipe" });
  execFileSync("git", ["-C", root, "add", "--", ...Object.keys(files)], { stdio: "pipe" });
  return root;
}
