import * as fs from "node:fs/promises";
import path from "node:path";
import { hash, inventory, candidateIndex } from "./inventory.mjs";
import { auditQuestions, questionVersion, findingsFor } from "./questions.mjs";
import { focusExclusion, sampleFocus, contextualCandidates } from "./context.mjs";
import { writeReport } from "./report.mjs";

export async function runAudit(rzm, mode, options = {}) {
  if (!["notes", "code"].includes(mode)) throw new Error("Expected notes or code mode");
  const root = path.resolve(options.root || process.cwd());
  const output = path.resolve(
    options.output || process.env.AUDIT_OUTPUT || path.join(root, ".rhizome/audits", mode),
  );
  const execute = options.execute ?? process.env.AUDIT_RUN === "1";
  const model = options.model || process.env.AUDIT_MODEL;
  if (execute && !model) throw new Error("Set AUDIT_MODEL to a pinned Jev version before running");
  const limit = Number(options.limit ?? process.env.AUDIT_LIMIT ?? 0);
  if (!Number.isSafeInteger(limit) || limit < 0)
    throw new Error("AUDIT_LIMIT must be a nonnegative integer");
  const reviewLimit = Number(options.reviewLimit ?? process.env.AUDIT_REVIEW_LIMIT ?? 100);
  if (!Number.isSafeInteger(reviewLimit) || reviewLimit < 1 || reviewLimit > 1000)
    throw new Error("AUDIT_REVIEW_LIMIT must be 1..1000");
  const exploration = options.exploration ?? process.env.AUDIT_EXPLORATION === "1";
  const sampleSeed = options.sampleSeed || process.env.AUDIT_SAMPLE_SEED || "pilot-v1";
  const rootCheck = await rzm.checkPaths({ paths: [{ path: root, isDir: true }] });
  if (
    !rootCheck.ok ||
    rootCheck.payload?.paths?.[0]?.status !== "allowed" ||
    rootCheck.payload.paths[0].path
  ) {
    throw new Error("Run from the selected vault root so inventory and ignore paths agree");
  }
  await fs.mkdir(output, { recursive: true });
  const lock = path.join(output, "running.lock");
  try {
    await fs.mkdir(lock);
  } catch (error) {
    if (error.code === "EEXIST")
      throw new Error(
        `An audit owns ${lock}. If its process died, trash this directory before resuming.`,
      );
    throw error;
  }
  try {
    return await audit(rzm, {
      root,
      output,
      execute,
      model,
      limit,
      mode,
      reviewLimit,
      exploration,
      sampleSeed,
    });
  } finally {
    await fs.rmdir(lock);
  }
}

async function atomicJSON(file, value) {
  await fs.writeFile(file + ".tmp", JSON.stringify(value));
  await fs.rename(file + ".tmp", file);
}
async function readCheckpoint(file) {
  try {
    return JSON.parse(await fs.readFile(file, "utf8"));
  } catch (error) {
    if (error.code === "ENOENT") return undefined;
    throw error;
  }
}

async function audit(
  rzm,
  { root, output, execute, model, limit, mode, reviewLimit, exploration, sampleSeed },
) {
  const startedAt = new Date().toISOString();
  const { files, chunks } = await inventory(rzm, root, output);
  await atomicJSON(path.join(output, "manifest.json"), { root, startedAt, files });
  const eligible = exploration
    ? chunks
    : chunks.filter(
        (c) => !focusExclusion(c) || c.context?.document.effort || /_test\.go$/.test(c.path),
      );
  const select = candidateIndex(eligible);
  const chunksByPath = new Map();
  for (const chunk of chunks) {
    if (!chunksByPath.has(chunk.path)) chunksByPath.set(chunk.path, []);
    chunksByPath.get(chunk.path).push(chunk);
  }
  const focusChunks = chunks.filter((chunk) => chunk.kind === (mode === "notes" ? "note" : "code"));
  const screened = focusChunks.filter((c) => exploration || !focusExclusion(c));
  const selected = sampleFocus(screened, limit, sampleSeed);
  const checkpointDir = path.join(output, "checkpoints");
  await fs.mkdir(checkpointDir, { recursive: true });
  const summary = {
    mode,
    root,
    model: model || "not selected (dry run)",
    questionVersion,
    exploration,
    sampleSeed,
    startedAt,
    dryRun: !execute,
    filesByStatus: {},
    eligibleFocusChunks: focusChunks.length,
    screenedFocusChunks: screened.length,
    excludedBeforeEvaluation: Object.fromEntries(
      [...new Set(focusChunks.map(focusExclusion).filter(Boolean))].map((reason) => [
        reason,
        exploration ? 0 : focusChunks.filter((c) => focusExclusion(c) === reason).length,
      ]),
    ),
    selectedFocusChunks: selected.length,
    omittedByLimit: screened.length - selected.length,
    completed: 0,
    cached: 0,
    failed: 0,
    uncertain: 0,
    notStarted: 0,
    planned: 0,
    noComparisonEvidence: 0,
    usage: { input_tokens: 0, output_tokens: 0 },
    usageScope:
      "Validated responses only; interrupted or failed attempts may have additional usage.",
    coverage:
      "Entire admitted Markdown/source inventory; focus screening and bounded comparisons are explicit below. Historical/test material can supply evidence. Canonical graph bindings and exhaustive pair coverage are not available.",
  };
  for (const file of files)
    summary.filesByStatus[file.status] = (summary.filesByStatus[file.status] || 0) + 1;
  const findings = [];
  const selectionIds = [];
  await atomicJSON(path.join(output, "selection.json"), selectionIds);
  const reportOptions = {
    metadata: new Map(files.filter((f) => f.metadata).map((f) => [f.path, f.metadata])),
    reviewLimit,
    exploration,
  };
  const preview = [];
  let batch = [],
    batchBytes = 0;
  const account = (packet, result, cached = false) => {
    if (result.status === "succeeded") {
      summary.completed++;
      if (cached) summary.cached++;
      summary.usage.input_tokens += result.response.usage.input_tokens;
      summary.usage.output_tokens += result.response.usage.output_tokens;
      findings.push(
        ...findingsFor(packet, result.response.answers).map((finding) => ({
          ...finding,
          packetId: packet.id,
        })),
      );
    } else if (result.status === "failed") summary.failed++;
    else if (result.status === "not_started") summary.notStarted++;
    else summary.uncertain++;
  };
  const flush = async () => {
    if (!batch.length) return;
    await atomicJSON(path.join(output, "selection.json"), selectionIds);
    const packets = batch;
    batch = [];
    batchBytes = 0;
    // Persist every intent before sending. A crash leaves uncertain records that
    // are never automatically resubmitted, including a lost successful response.
    for (const packet of packets)
      await atomicJSON(path.join(checkpointDir, packet.id + ".json"), {
        packet,
        status: "started",
        startedAt: new Date().toISOString(),
      });
    let results;
    try {
      const reply = await rzm.evaluateBatch(
        { items: packets, concurrency: 8, budgetMs: 25000 },
        { timeoutMs: 30000 },
      );
      if (!reply.ok || !Array.isArray(reply.payload?.items))
        throw new Error("Batch did not return item outcomes");
      results = reply.payload.items;
      if (results.length !== packets.length || results.some((item, i) => item.id !== packets[i].id))
        throw new Error("Batch response correlation mismatch");
    } catch (error) {
      results = packets.map((packet) => ({
        id: packet.id,
        status: "uncertain",
        error: error.message,
      }));
    }
    for (let i = 0; i < packets.length; i++) {
      await atomicJSON(path.join(checkpointDir, packets[i].id + ".json"), {
        packet: packets[i],
        ...results[i],
        finishedAt: new Date().toISOString(),
      });
      account(packets[i], results[i]);
      if ([401, 402, 403].includes(results[i].statusCode))
        summary.stoppedReason = `Provider returned HTTP ${results[i].statusCode}; resolve the account error before resuming.`;
    }
    await writeReport(output, summary, findings, reportOptions);
    console.error(
      `Audit ${mode}: ${summary.completed} completed, ${summary.failed} failed, ${summary.uncertain} uncertain`,
    );
  };
  for (const focus of selected) {
    const state = {
      focus,
      ...contextualCandidates(focus, select(focus), chunksByPath, exploration),
    };
    if (!state.candidates.length) {
      summary.noComparisonEvidence++;
      continue;
    }
    const request = {
      model,
      state,
      questions: auditQuestions(focus, state.candidates, exploration),
    };
    const packet = { id: hash({ questionVersion, ...request }), ...request };
    selectionIds.push(packet.id);
    if (preview.length < 20) preview.push(packet);
    const previous = await readCheckpoint(path.join(checkpointDir, packet.id + ".json"));
    if (previous && previous.status !== "not_started") {
      account(packet, previous, true);
      continue;
    }
    if (!execute) {
      summary.planned++;
      continue;
    }
    const bytes = Buffer.byteLength(JSON.stringify(packet));
    if (bytes > 750000)
      throw new Error(`Packet exceeds transport budget: ${focus.path}:${focus.startLine}`);
    if (batch.length === 8 || batchBytes + bytes > 750000) await flush();
    if (summary.stoppedReason) {
      summary.notStarted++;
      continue;
    }
    batch.push(packet);
    batchBytes += bytes;
  }
  await flush();
  await atomicJSON(path.join(output, "selection.json"), selectionIds);
  await atomicJSON(path.join(output, "sample-packets.json"), preview);
  summary.finishedAt = new Date().toISOString();
  await writeReport(output, summary, findings, reportOptions);
  console.log(JSON.stringify({ output, ...summary }));
  return summary;
}
