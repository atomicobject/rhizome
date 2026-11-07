import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { createHash } from "node:crypto";
import test from "node:test";

const fixture = new URL("../../../../../../testdata/ao-kb/booking/", import.meta.url);
const read = (path) => readFileSync(new URL(path, fixture), "utf8");

test("real fixture passages distinguish the recorded booking decisions and questions", () => {
  const lines = read("sources/_raw/meetings/clinic-operations-sync.txt").trimEnd().split("\n");
  const cases = [
    ["clinic-patient-books-own-slot", /Patients can choose an available slot/],
    ["reminders-forty-eight-hours", /48 hours before/],
    ["reminders-twenty-four-hours", /24 hours before/],
    ["reminders-two-hours", /two hours before/],
    ["same-day-cancellation-free", /without a fee/],
    ["same-day-cancellation-fee", /pay a fee/],
    ["saturday-hours-through-october", /through October 2026/],
    ["video-visits-approved", /video appointments are approved/],
    ["guardian-question-withdrawn", /account-owning guardian/],
    ["eligibility-question-old", /What eligibility checks/],
    ["eligibility-question-specific", /Must a referral be verified/],
    ["new-waitlist-after-snapshot", /expires after one hour/],
  ];
  const hashes = new Set();
  for (const [slug, meaning] of cases) {
    const claim = read(`assertions/${slug}.md`);
    const [, first, last] = /locator: "L(\d+)-L(\d+)"/.exec(claim);
    const passage = lines.slice(Number(first) - 1, Number(last)).join("\n");
    assert.match(passage, meaning, slug);
    const hash = `sha256:${createHash("sha256").update(passage.trim().split(/\s+/u).join(" ")).digest("hex")}`;
    assert.ok(claim.includes(`quote_hash: "${hash}"`), `${slug}: AO-generated passage hash`);
    hashes.add(hash);
  }
  assert.equal(hashes.size, cases.length, "unrelated claims must not reuse the same passage");
});
