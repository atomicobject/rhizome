import * as fs from "node:fs/promises";
import { buildReviewQueue } from "./triggers.mjs";
import { verifiedReport } from "./reviews.mjs";

export function escapeHTML(value) {
  return String(value ?? "").replace(
    /[&<>"']/g,
    (c) => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" })[c],
  );
}
const page = (title, intro, summary, rows) =>
  `<!doctype html><html lang="en"><meta charset="utf-8"><title>${title}</title><style>body{font:16px system-ui;max-width:1100px;margin:40px auto;padding:0 20px}pre{white-space:pre-wrap;overflow-wrap:anywhere;background:#eee;padding:12px}details{margin:12px 0;padding:12px;border:1px solid #ccc}summary{cursor:pointer}input{padding:8px;width:90%}</style><h1>${title}</h1><p>${intro}</p><nav><a href="report.html">Verified improvements</a> | <a href="candidates.html">Investigation candidates</a></nav><details><summary>Coverage and review status</summary><pre>${escapeHTML(JSON.stringify(summary, null, 2))}</pre></details><label>Filter <input id="filter" type="search"></label>${rows}<script>document.querySelector('#filter').addEventListener('input',e=>{const term=e.target.value.toLowerCase();for(const row of document.querySelectorAll('.issue'))row.hidden=!row.textContent.toLowerCase().includes(term)});</script></html>`;

export async function writeReport(output, summary, findings, options = {}) {
  const metadata = options.metadata || new Map();
  const review = buildReviewQueue(findings, metadata, options.reviewLimit, options.exploration);
  const verified = await verifiedReport(output, review.packets, metadata, options.decisions);
  summary.review = { ...review.summary, verification: verified.summary };
  await fs.writeFile(output + "/summary.json", JSON.stringify(summary, null, 2));
  await fs.writeFile(output + "/candidates.json", JSON.stringify(review.packets, null, 2));
  await fs.writeFile(output + "/findings.json", JSON.stringify(verified.improvements, null, 2));
  await fs.writeFile(
    output + "/review-packets.jsonl",
    verified.pending.map((p) => JSON.stringify(p)).join("\n") +
      (verified.pending.length ? "\n" : ""),
  );
  const rows = review.packets
    .map(
      (p) =>
        `<details class="issue"><summary>${escapeHTML(p.trigger)}: ${p.paths.map(escapeHTML).join(" ↔ ")}</summary><p>${escapeHTML(p.instructions)}</p><p>${p.supportingFindings} supporting signals; ${escapeHTML(p.strengthKind)}: ${p.strength.toFixed(2)}.</p><p>${p.qualifications.map(escapeHTML).join(" ")}</p>${p.evidence.map((e) => e.sources.map((s) => `<p>${escapeHTML(s.path)}:${s.startLine}-${s.endLine}</p><pre>${escapeHTML(JSON.stringify(s.context || s.document, null, 2))}</pre><pre>${escapeHTML(s.content)}</pre>`).join("")).join("")}</details>`,
    )
    .join("\n");
  await fs.writeFile(
    output + "/candidates.html",
    page(
      "Investigation candidates",
      `${review.packets.length} candidates; ${review.summary.omittedByReviewLimit} omitted by the review limit. These are unverified triggers, not confirmed defects.`,
      summary,
      rows,
    ),
  );
  const confirmed = verified.improvements
    .map(
      (p) =>
        `<details class="issue"><summary>${escapeHTML(p.title)}</summary><p>${escapeHTML(p.correction)}</p><p>Edit target: ${escapeHTML(p.editTarget)}</p>${p.reviews.map((r) => `<p>Reviewer: ${escapeHTML(r.reviewer)}; scope: ${escapeHTML(r.scope)}</p><p>${escapeHTML(r.reason)}</p><pre>${escapeHTML(JSON.stringify({ claims: r.claims, evidence: r.evidence }, null, 2))}</pre>`).join("")}</details>`,
    )
    .join("\n");
  await fs.writeFile(
    output + "/report.html",
    page(
      "Verified repository improvements",
      `${verified.improvements.length} confirmed improvements. ${verified.summary.pending + verified.summary.stale} candidates await review; ${verified.summary.unresolved} remain unresolved. Reviewer-confirmed findings still require normal implementation checks.`,
      summary,
      confirmed,
    ),
  );
  return { ...review, summary: summary.review };
}
