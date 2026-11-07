#!/usr/bin/env node
// The compounding workspace's actions (SPEC-0124 US9, SPEC-0131). Each
// action runs `workspace.mjs <op>` with one JSON object on stdin and prints
// one JSON value; a refusal exits 2 with its reason as the last stderr line.
//
//   recommendations                 pending proposals and open findings, by response
//   item {ref}                      one recommendation in full
//   prompt {items: [ref]}           one agent prompt for the selected recommendations;
//                                   a proposal may carry the revision shown, `proposal:<cluster>@<revision>`
//   dismiss {cluster, revision, reason} dismisses the reviewed proposal locally
//   hide {path}                     hides a finding in this workspace, locally
//   health                          the friction report and the last mine run
//   shards-check                    shards and guard rules needing attention
//
// A ref is `proposal:<cluster>` or `finding:<path>`. Hiding a finding never
// touches its note or its action item, which the team shares. Run from the
// repository root; every worktree shares one store (lib/state.mjs).

import { readFileSync } from "node:fs";
import { join } from "node:path";
import { pathToFileURL } from "node:url";
import { review, revision } from "./lib/approvals.mjs";
import { loadFindings } from "./lib/findings.mjs";
import { repoRoot, titleOf } from "./lib/shards.mjs";
import { lockOrRefuse, openState } from "./lib/state.mjs";
import { Refused } from "./lib/store.mjs";
import { RESPONSES } from "./lib/verify.mjs";
import { report } from "./report.mjs";
import { run as shards } from "./shards.mjs";

const HIDDEN = "hidden-findings.json";
// The responses in review order; a proposal with no response goes last.
const rank = (r) => (RESPONSES.includes(r) ? RESPONSES.indexOf(r) : RESPONSES.length);

// The publication statement the compounding skill accepts as confirmation.
export const PUBLISH_STATEMENT =
  "The person reviewed these in the workspace and approves publishing them.";

function recommendations(root, store) {
  const proposals = review(store, root).pending.map((p) => ({
    ref: `proposal:${p.cluster}`,
    kind: "proposal",
    response: p.response ?? null,
    title: titleOf(p.statement),
    summary: p.statement,
    cluster: p.cluster,
    tier: p.tier,
    families: p.families,
    status: p.status ?? "pending",
    revision: revision(p),
    ...(p.previous ? { previous: p.previous } : {}),
    ...(p.recurrence ? { recurrence: p.recurrence.finding } : {}),
    ...(p.rejected ? { rejected: p.rejected.reason } : {}),
  }));
  const hidden = store.read(HIDDEN, {});
  // Teammates' findings count too: they come from tracked notes, not local state.
  const findings = loadFindings(root)
    .filter((f) => f.open && ["fix", "guard", "ask"].includes(f.response) && !hidden[f.path])
    .map(findingRow);
  const items = [...proposals, ...findings].sort((a, b) => rank(a.response) - rank(b.response));
  return { items };
}

function findingRow(f) {
  return {
    ref: `finding:${f.path}`,
    kind: "finding",
    response: f.response,
    title: titleOf(f.statement),
    summary: f.statement,
    path: f.path,
    assignee: f.assignee,
    actionItem: f.actionItem,
    families: f.families,
    reviewedOn: f.reviewedOn,
  };
}

function parseRef(ref) {
  const m = String(ref ?? "").match(/^(proposal|finding):(.+)$/);
  if (!m) throw new Refused(`${ref} is not a proposal:<cluster> or finding:<path> reference`);
  return { kind: m[1], id: m[2] };
}

// Any finding or proposal the health report can name, not only current
// recommendations: a completed finding, or an approved proposal.
// `current` says whether it is still a recommendation the quick actions apply to.
function item(root, store, ref, { state, user }) {
  const { kind, id } = parseRef(ref);
  const current = recommendations(root, store).items.some((x) => x.ref === ref);
  if (kind === "finding") {
    const f = loadFindings(root).find((x) => x.path === id);
    if (!f) throw new Refused(`no finding at ${id}`);
    const note = readFileSync(join(root, id), "utf8");
    return {
      ...findingRow(f),
      open: f.open,
      hidden: Boolean(store.read(HIDDEN, {})[id]),
      current,
      note,
    };
  }
  const shown = shards(["show", id], { root, state, user });
  // An approval carries its proposal as approved; a pending proposal is itself.
  const proposal = shown.proposal ?? shown;
  // The cited excerpts of a pending proposal, redacted as `shards.mjs source` gives them.
  const sources = shown.proposal
    ? []
    : (proposal.evidence ?? []).map((_, k) =>
        shards(["source", id, String(k + 1)], { root, state, user }),
      );
  return {
    ref,
    kind: "proposal",
    response: proposal.response ?? null,
    title: titleOf(proposal.statement),
    summary: proposal.statement,
    cluster: id,
    tier: proposal.tier,
    families: proposal.families,
    status: shown.status ?? "pending",
    ...(shown.proposal ? {} : { revision: revision(proposal) }),
    current,
    proposal,
    sources,
  };
}

// The prompt names references, never their content, so the agent reads each
// one where it lives. A view sends each proposal as `proposal:<cluster>@<revision>`,
// the revision the person saw; a redraft since refuses, and the prompt pins
// that revision so approving refuses a later one too.
function prompt(root, store, refs) {
  if (!Array.isArray(refs) || !refs.length) throw new Refused("select at least one recommendation");
  const listed = new Map(recommendations(root, store).items.map((x) => [x.ref, x]));
  const asked = refs.map((r) => String(r).match(/^(proposal:[^@]+)@(.+)$/) ?? [r, r, null]);
  const gone = asked.filter(([, ref]) => !listed.has(ref)).map(([r]) => r);
  if (gone.length)
    throw new Refused(`no longer recommendations: ${gone.join(", ")}; refresh the workspace`);
  const changed = asked.filter(([, ref, rev]) => rev && listed.get(ref).revision !== rev).map(([, ref]) => ref);
  if (changed.length)
    throw new Refused(`redrafted since you reviewed them: ${changed.join(", ")}; refresh the workspace and review them again`);
  const chosen = [...new Set(asked.map(([, ref]) => ref))].map((r) => listed.get(r));
  const line = (x) =>
    `- \`${x.ref}\`${x.response ? ` (${x.response})` : ""}${x.revision ? `, revision \`${x.revision}\`` : ""}`;
  const proposals = chosen.filter((x) => x.kind === "proposal");
  const findings = chosen.filter((x) => x.kind === "finding");
  const out = [
    "Use the compounding skill to act on these recommendations from the compounding workspace.",
    "",
    PUBLISH_STATEMENT,
  ];
  if (proposals.length)
    out.push(
      "",
      "Proposals: read each with `node .rhizome/modules/compounding/scripts/shards.mjs show <cluster>`, then approve it as drafted with `shards.mjs approve <cluster> --confirm-publish --revision <revision> --head <head>`, using the revision listed here and the head `show` printed. If `show` prints a different revision, the draft changed after the person reviewed it: do not approve it; show them what changed and ask. Apply each document edit an approval makes. A proposal with no response needs the person to choose one, so ask them.",
      ...proposals.map(line),
    );
  if (findings.length) out.push("", "Findings: read each note.", ...findings.map(line));
  out.push(
    "",
    "Implement each selected fix and guard, including those the approvals above create, and check off each action item you complete. For an ask, put the question to its owner and leave its action item open.",
    "When a script refuses an item, bring its reason back to me one item at a time, and continue with the rest.",
    "Finish with every change on one branch, ready for one pull request.",
  );
  return { prompt: out.join("\n") };
}

function hide(root, store, path) {
  if (!path || typeof path !== "string") throw new Refused("pass the finding's path");
  lockOrRefuse(store);
  try {
    if (!loadFindings(root).some((f) => f.path === path))
      throw new Refused(`no finding at ${path}`);
    store.write(HIDDEN, { ...store.read(HIDDEN, {}), [path]: { at: new Date().toISOString() } });
    return { hidden: path };
  } finally {
    store.unlock();
  }
}

// The friction report with references the workspace can open. Mining state
// comes from the same report/store as the CLI, including unfinished work.
function health(root, store, state) {
  const r = report({ root, state });
  const ref = (x) => (x.proposal ? `proposal:${x.proposal}` : `finding:${x.finding}`);
  return {
    open: r.open.map((x) => ({ ref: ref(x), ...x })),
    completed: r.completed.map((x) => ({ ref: ref(x), ...x })),
    ...r.mining,
  };
}

// `state` and `user` stand in for the repository store and the configured
// current user in tests.
export function workspace(op, input = {}, { root = repoRoot(), state, user } = {}) {
  const store = openState(root, { state });
  switch (op) {
    case "recommendations":
      return recommendations(root, store);
    case "item":
      return item(root, store, input.ref, { state, user });
    case "prompt":
      return prompt(root, store, input.items);
    case "dismiss":
      if (!input.cluster || !input.reason || typeof input.reason !== "string")
        throw new Refused("pass the proposal's cluster and a reason");
      if (typeof input.revision !== "string" || !input.revision)
        throw new Refused("pass the proposal revision you reviewed");
      return shards(["dismiss", input.cluster, "--reason", input.reason, "--revision", input.revision, "--local"], {
        root,
        state,
        user,
      });
    case "hide":
      return hide(root, store, input.path);
    case "health":
      return health(root, store, state);
    case "shards-check":
      return shards(["check"], { root, state, user });
    default:
      throw new Refused(
        "usage: workspace.mjs recommendations|item|prompt|dismiss|hide|health|shards-check (see the header)",
      );
  }
}

if (process.argv[1] && import.meta.url === pathToFileURL(process.argv[1]).href) {
  try {
    const text = readFileSync(0, "utf8").trim();
    console.log(JSON.stringify(workspace(process.argv[2], text ? JSON.parse(text) : {})));
  } catch (err) {
    console.error(err instanceof Refused ? err.message : err.stack);
    process.exitCode = err instanceof Refused ? 2 : 1;
  }
}
