// Estimates prioritize evidence checks; only evidence verification or a
// person's settled decision removes a learning from this queue.
import { loadFindings } from "./findings.mjs";
import { learningHash, loadDecisions, loadShards } from "./shards.mjs";

export const unrouted = (c) => c.verification?.decision === "propose" && !c.verification.response;
export const due = (c) => !c.verification || unrouted(c) || (c.verification.decision === "reject" && (c.families?.length ?? 0) > (c.verification.families ?? Infinity));

export function verificationQueue(root, store, clusters = store.read("pool/clusters.json", {})) {
  const reviewed = store.read("reviewed.json", {});
  const settled = new Set([...loadDecisions(root, store.dir), ...loadShards(root), ...loadFindings(root)].map((c) => c.learning ?? learningHash(c)));
  return Object.values(clusters).filter((c) => c.members?.length && due(c) && !reviewed[c.id] && (c.recurrence || !settled.has(c.learning ?? learningHash(c))));
}

const quote = (value) => `'${String(value).replaceAll("'", "'\\''")}'`;
export function verificationStatus(root, store, run = null, { reason } = {}) {
  const clusters = Object.values(store.read("pool/clusters.json", {}));
  const queue = verificationQueue(root, store);
  const attempted = run ? clusters.filter((c) => c.verification?.run === run.id || c.verifyProblems?.run === run.id) : [];
  const verified = attempted.filter((c) => c.verification?.run === run.id);
  const rejected = {};
  for (const c of verified.filter((c) => c.verification.decision === "reject")) {
    const why = c.verification.rejectReason ?? "other";
    rejected[why] = (rejected[why] ?? 0) + 1;
  }
  const reasons = {};
  reason ??= run?.status === "stopped" ? run.stoppedReason : undefined;
  const plan = run ? store.read(`runs/${run.id}/verify-plan.json`, null) : null;
  for (const c of queue) {
    const why = c.verifyProblems ? "contract" : plan && !plan.includes(c.id) ? "count_limit" : reason ?? "count_limit";
    reasons[why] = (reasons[why] ?? 0) + 1;
  }
  const harness = run?.bindings?.verify?.provider ?? run?.bindings?.extract?.provider;
  const budget = reason === "budget" ? "<USD>" : run?.budget ?? "<USD>";
  const command = `node .rhizome/modules/compounding/scripts/mine.mjs${harness ? ` --harness ${harness}` : " --harness <claude|codex>"} --budget ${budget} --state ${quote(store.dir)}`;
  return {
    examined: attempted.length,
    proposed: verified.filter((c) => c.verification.decision === "propose").length,
    rejected,
    pending: queue.length,
    complete: !queue.length && !reason,
    deferred: { count: queue.length, reasons },
    contractProblems: attempted.filter((c) => c.verifyProblems?.run === run.id).length,
    next: queue.length || reason ? { command, reason: reasons.contract ? "Repair the verification response or refresh its evidence, then rerun with an approved budget; cached invalid responses stay pending." : reason === "budget" ? "Resume with a larger approved cumulative --budget than the stopped run, or abandon it through the compounding skill." : reason ? `Resolve the ${reason} stop and rerun with an approved budget to continue mining.` : "Rerun with an approved budget to verify the remaining candidates." } : null,
  };
}
