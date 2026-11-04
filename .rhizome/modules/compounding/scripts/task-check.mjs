#!/usr/bin/env node
// The compounding/mine task's check (SPEC-0124 US8). It makes no model
// calls: it asks Rhizome whether Jev screening has a credential, then reads
// mining state through a dry run of mine.mjs, and prints the task check's
// {needed, summary, context}. A problem only a person can fix exits 1 with
// its reason on stderr, which leaves the task needing attention.
//
//   task-check.mjs
//
// Run from the repository root; RZM selects the binary, as for mine.mjs.

import { spawnSync } from "node:child_process";
import { existsSync } from "node:fs";
import { join } from "node:path";
import { fileURLToPath, pathToFileURL } from "node:url";

// Fewer new episodes than this wait for the next week.
export const MIN_EPISODES = 5;

const FIX = {
  budget: "resume it with a larger --budget through the compounding skill, or pass --abandon to drop it",
  credential: "configure a TypeSafe credential for Rhizome, then run the task again; or pass --abandon and mine unscreened over a short window through the compounding skill",
  binding: "resume it once that model is available again, or pass --abandon to start over with the current models",
};

// Every thrown error needs a person. credentials() returns {configured,
// source}; dryRun() returns mine.mjs's dry-run result. Checked in order:
// credential, a run a person must unblock, a run to resume, new episodes.
export async function check({ credentials, dryRun }) {
  const cred = await credentials();
  if (!cred.configured) throw new Error(`Jev screening has no TypeSafe credential, so mining cannot screen sessions; configure one for Rhizome, or mine unscreened over a short window through the compounding skill`);
  const r = await dryRun();
  // A dry run reports a frozen binding that is unavailable now as a stop. A
  // run saved as stopped for a credential or a binding that is available
  // again resumes; only a budget stop still needs a person.
  const stop = r.status === "stopped" ? { run: r.run, reason: r.reason, message: r.message } : r.open?.status === "stopped" && r.open.stoppedReason === "budget" ? { run: r.open.run, reason: "budget", message: r.open.stoppedMessage } : null;
  if (stop && FIX[stop.reason]) throw new Error(`mining run ${stop.run} stopped (${stop.reason}): ${stop.message}; ${FIX[stop.reason]}`);
  if (r.status === "stopped") throw new Error(`mining stopped (${r.reason}): ${r.message}`);
  if (r.open) return { needed: true, summary: `resume mining run ${r.open.run}`, context: `Run ${r.open.run} is ${r.open.status}${r.open.stoppedReason ? ` (${r.open.stoppedReason}: ${r.open.stoppedMessage})` : ""}; mining resumes it with its frozen bindings.` };
  if (r.verification?.pending > 0) return { needed: true, summary: `${r.verification.pending} clusters await evidence verification`, context: "Mining has unfinished verification even without new session episodes. Rerun mining with an approved budget; Health shows the store and the remaining work." };
  const n = r.episodes ?? 0;
  if (n >= MIN_EPISODES) return { needed: true, summary: `${n} new session episodes to mine`, context: `Discovery found ${r.discovery?.inWindow ?? "?"} sessions in the window; ${n} episodes are new.` };
  return { needed: false, summary: n ? `only ${n} new session episodes; waiting for ${MIN_EPISODES}` : "no new session episodes" };
}

function run(command, args, root) {
  const r = spawnSync(command, args, { cwd: root, encoding: "utf8", stdio: ["ignore", "pipe", "inherit"] });
  if (r.error) throw r.error;
  return r;
}

const isMain = process.argv[1] && import.meta.url === pathToFileURL(process.argv[1]).href;
if (isMain) {
  let root = process.cwd();
  const git = spawnSync("git", ["rev-parse", "--show-toplevel"], { encoding: "utf8" });
  if (git.status === 0) root = git.stdout.trim();
  const rzm = process.env.RZM || (existsSync(join(root, "bin/rzm")) ? join(root, "bin/rzm") : "rzm");
  const mine = fileURLToPath(new URL("mine.mjs", import.meta.url));
  try {
    const result = await check({
      credentials() {
        const r = run(rzm, ["agent", "evaluate-batch", "--check-credentials"], root);
        if (r.status !== 0) throw new Error(`rzm agent evaluate-batch --check-credentials exited ${r.status}; this check needs a Rhizome release that has it`);
        return JSON.parse(r.stdout);
      },
      dryRun() {
        // A dry run exits 3 when it reports a stop.
        const r = run(process.execPath, [mine], root);
        if (r.status !== 0 && r.status !== 3) throw new Error(`mine.mjs dry run exited ${r.status}`);
        return JSON.parse(r.stdout);
      },
    });
    process.stdout.write(JSON.stringify(result) + "\n");
  } catch (err) {
    process.stderr.write(`${err.message}\n`);
    process.exitCode = 1;
  }
}
